// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SaveTimeout bounds every backend Load/Save call this package's
// write-behind path performs -- both the synchronous load Open runs
// before a *WriteBehind exists at all, and every asynchronous save the
// writer goroutine performs afterward. One constant rather than the five
// near-identical 5-second consts every adopting store used to carry
// (internal/flags, internal/rules, internal/device, and, inconsistently,
// no deadline at all in internal/detect/internal/watchlist -- #380's
// first item): a store's persist call is a small JSON document, so 5s is
// generous for ordinary latency and short enough that a genuinely stuck
// backend degrades to a logged failure rather than an indefinite hang.
// See OpenWriteBehind and (*WriteBehind).attempt for where it's applied.
//
// A var, not a const, so a test can shrink it to prove a stuck backend
// is actually bounded without waiting out 5 real seconds -- the same
// "var rather than const so tests can shrink it" convention every
// interval/threshold in this codebase already follows (e.g.
// flags.persistMinInterval, engine.queueSize).
var SaveTimeout = 5 * time.Second

// WriteBehind is the one place mikroview's small JSON-document stores
// get persistence off their hot path, per issue #400.
//
// # Write-behind
//
// MarkDirty hands off an already-encoded snapshot and returns
// immediately -- it never touches the backend. A single writer goroutine
// (started by OpenWriteBehind, stopped by Close) coalesces however many
// MarkDirty calls land between its own save attempts into one
// SaveWithRetry call: a caller marking dirty ten times in a row while an
// attempt is in flight still produces one write of the latest state, not
// ten. This is what makes it safe for a store's own mutating methods
// (flags.Store.Add, rules.Store.Touch, ...) to call MarkDirty while
// still holding their own lock -- see this type's own callers for the
// "lock covers the in-memory mutation and an encode/snapshot, nothing
// past that" contract issue #400 asks for.
//
// # Deadline
//
// Every backend call this type makes -- Open's initial Load, and every
// subsequent Save the writer goroutine attempts -- carries a SaveTimeout
// deadline. No call here ever runs under context.Background() with no
// bound, which is #380's first item: a stalled backend used to be able
// to block whichever goroutine called persistLocked (the single ingest
// goroutine, for three of the five stores this now backs) forever.
//
// # Rate limiting and back-off are the same mechanism
//
// MinInterval (see WriteBehind.interval) is stamped *after* an attempt
// completes, not before it starts -- the fix for #377's reproduced
// defect. The old shape recorded "last persist" the moment the interval
// check passed and then performed the write, so a write that itself ran
// long (the exact stuck-backend case #377 is about) never actually
// bought any back-off: by the time it returned, the interval had already
// elapsed, and the very next call attempted again. Stamping after means
// the next attempt is always at least MinInterval of *wall-clock time
// after the previous one *finished*, whether it succeeded or not -- so a
// sustained outage costs one attempt per MinInterval window, not one per
// event, which is what closes #377 across every adopter.
//
// Stated honestly: this does not make an individual attempt any faster.
// A MarkDirty call that lands outside the back-off window still has to
// wait for the writer goroutine to pick it up and, if the backend is
// genuinely stuck, for that attempt to run out its SaveTimeout deadline
// before the next one can start. What write-behind removes is that wait
// being on the *caller's* goroutine -- the ingest path, an HTTP handler,
// whatever called MarkDirty -- not the wait itself. The residual latency
// is real and belongs to the writer goroutine, which is precisely the
// point of moving it there.
//
// # Fail-closed load
//
// OpenWriteBehind is built on Open (#378), not a reimplementation of it:
// a document that exists but cannot be loaded or parsed produces a
// *StartupError and no *WriteBehind at all. There is therefore no object
// a caller could ever call MarkDirty on for a store whose load failed --
// the failure mode #378 closed (a live, writable backend attached to a
// store built around a decode that never happened) is structurally
// unreachable here, not merely avoided by convention. See
// TestOpenWriteBehindNeverAttachesToAFailedLoad.
//
// # Shutdown
//
// Close stops the writer goroutine and performs one last save of
// whatever is still dirty, bounded by SaveTimeout, before returning --
// see Close's own doc comment.
type WriteBehind struct {
	name     string
	backend  Backend
	timeout  time.Duration
	interval time.Duration

	onSaveError func(msg string)
	onConflict  func(msg string)

	mu         sync.Mutex
	version    int64
	payload    []byte
	dirty      bool
	generation uint64
	// forceNow, set by Flush, tells run to skip the interval wait on its
	// very next attempt -- see Flush's own doc comment for why this
	// routes through the single writer goroutine instead of attempting
	// independently.
	forceNow bool

	wake  chan struct{}
	stopc chan struct{}
	donec chan struct{}
	once  sync.Once
}

// WriteBehindOptions configures OpenWriteBehind. MinInterval is
// required (see WriteBehind's doc comment for what it governs);
// OnSaveError/OnConflict are optional hooks a store uses to log a
// failure with its own component logger and wording, mirroring what
// persistLocked used to do inline before this type existed -- kept as
// caller hooks rather than this package taking a logging dependency
// itself, the same "keeps this package free of logging policy" reason
// SaveWithRetry's own doc comment already gives for returning
// `conflicted` instead of logging it.
type WriteBehindOptions struct {
	MinInterval time.Duration
	OnSaveError func(msg string)
	OnConflict  func(msg string)
}

// OpenWriteBehind loads b's document under the same fail-closed contract
// Open documents (#378) and, on success, returns a *WriteBehind wired to
// b -- or, if b is nil, a nil *WriteBehind, matching the "persistence
// not configured" convention every adopting store already has (every
// method here is a safe no-op on a nil receiver, mirroring
// engine.Engine.Enqueue's own nil-receiver convention).
//
// decode is called exactly as Open documents: only when a document
// already exists, so the caller can populate whatever it closed over.
// name is used both for Open's StartupError (e.g. "the flags store")
// and, once the store is live, for the save-failure/conflict messages
// this type formats once and hands to opts.OnSaveError/OnConflict --
// consolidating wording that used to be duplicated, with minor
// variations, across every one of this package's five adopters.
func OpenWriteBehind(ctx context.Context, b Backend, name string, opts WriteBehindOptions, decode func([]byte) error) (wb *WriteBehind, existed bool, err error) {
	loadCtx, cancel := context.WithTimeout(ctx, SaveTimeout)
	defer cancel()
	version, existed, err := Open(loadCtx, b, name, decode)
	if err != nil {
		return nil, false, err
	}
	if b == nil {
		return nil, existed, nil
	}

	w := &WriteBehind{
		name:        name,
		backend:     b,
		timeout:     SaveTimeout,
		interval:    opts.MinInterval,
		onSaveError: opts.OnSaveError,
		onConflict:  opts.OnConflict,
		version:     version,
		wake:        make(chan struct{}, 1),
		stopc:       make(chan struct{}),
		donec:       make(chan struct{}),
	}
	go w.run()
	return w, existed, nil
}

// MarkDirty records payload as the latest snapshot to persist and wakes
// the writer goroutine -- never blocks on the backend, never blocks at
// all beyond a small internal mutex. Safe to call while the caller holds
// its own lock (see this type's own doc comment). A nil receiver is a
// no-op, matching "persistence not configured".
func (w *WriteBehind) MarkDirty(payload []byte) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.payload = payload
	w.dirty = true
	w.generation++
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Flush forces the writer goroutine to attempt now, skipping whatever is
// left of MinInterval's wait, and blocks until whatever was dirty when
// it was called has been persisted (successfully or not) or ctx expires
// -- the synchronous checkpoint a test (or a caller that genuinely needs
// to know the current state has reached the backend, e.g. immediately
// before -backup reads the same file from a separate process) needs,
// without tearing down the writer goroutine the way Close does.
//
// Deliberately routed through the single writer goroutine (a forceNow
// flag it checks) rather than performing its own independent attempt:
// two attempts racing the same backend concurrently is not what "one
// writer goroutine serialises its own backend I/O" (this type's own
// doc comment) promises, and would show up as spurious ErrConflict
// retries under SaveWithRetry -- see
// TestWriteBehindFlushDoesNotRaceTheWriterGoroutine.
//
// A nil receiver, or one with nothing dirty, returns immediately. Not
// meant for the hot path -- see MarkDirty for that.
func (w *WriteBehind) Flush(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	dirty := w.dirty
	if dirty {
		w.forceNow = true
	}
	w.mu.Unlock()
	if !dirty {
		return nil
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}

	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		w.mu.Lock()
		stillDirty := w.dirty
		w.mu.Unlock()
		if !stillDirty {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Close stops the writer goroutine, performs one final attempt if
// anything is still dirty (ignoring MinInterval, the same as Flush), and
// waits for that to finish or ctx to expire before returning -- the join
// primitive main's shutdown uses so a change made right before shutdown
// is not silently dropped the way an ordinary MinInterval-debounced
// write could be. Idempotent: a second Close is a no-op that returns
// nil. A nil receiver is a no-op.
//
// Lifecycle note: unlike engine.Engine's explicit Run(ctx)/Done()
// (started by the caller, observed by the caller), WriteBehind starts
// its writer goroutine itself in OpenWriteBehind -- a store needs
// persistence live from the moment it's constructed, there is no
// "registered but not yet running" phase to model. Close is the one
// piece of that lifecycle a caller does need to observe, so it mirrors
// Done()'s join without needing a separate channel for it.
func (w *WriteBehind) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.once.Do(func() { close(w.stopc) })
	select {
	case <-w.donec:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the single writer goroutine: attempts a save whenever something
// is dirty, spaced by at least w.interval between attempt completions
// (see this type's own doc comment for why that's stamped after, not
// before), and exits once stopc is closed -- performing one last attempt
// on the way out if anything is still dirty.
func (w *WriteBehind) run() {
	defer close(w.donec)

	var lastAttempt time.Time
	for {
		w.mu.Lock()
		dirty := w.dirty
		w.mu.Unlock()

		if !dirty {
			select {
			case <-w.wake:
				continue
			case <-w.stopc:
				return
			}
		}

		w.mu.Lock()
		force := w.forceNow
		w.forceNow = false
		w.mu.Unlock()

		if !force && !lastAttempt.IsZero() {
			if wait := w.interval - time.Since(lastAttempt); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-w.wake:
					// A Flush call armed forceNow and nudged wake to cut
					// this wait short -- loop back around to pick up
					// force on the next iteration rather than consuming
					// this timer's remaining wait for nothing.
					timer.Stop()
					continue
				case <-w.stopc:
					timer.Stop()
					w.attempt(context.Background())
					return
				}
			}
		}

		w.attempt(context.Background())
		lastAttempt = time.Now()
	}
}

// attempt performs one SaveWithRetry against whatever payload was
// current when it started, under a SaveTimeout deadline derived from
// parent. It clears dirty only if nothing marked a newer generation
// dirty while this attempt was in flight -- so a MarkDirty that lands
// mid-attempt is never lost, even though its bytes arrived too late for
// this particular Save.
func (w *WriteBehind) attempt(parent context.Context) {
	w.mu.Lock()
	payload := w.payload
	expect := w.version
	gen := w.generation
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, w.timeout)
	defer cancel()
	version, conflicted, err := SaveWithRetry(ctx, w.backend, payload, expect)
	if err != nil {
		if w.onSaveError != nil {
			w.onSaveError(fmt.Sprintf(
				"writing %s to %s failed: %v -- this change exists only in memory and will be lost on restart",
				w.name, w.backend.Describe(), err))
		}
		return
	}
	if conflicted && w.onConflict != nil {
		w.onConflict(fmt.Sprintf(
			"%s was modified by another process while this change was pending (%s); this change was applied on top",
			w.name, w.backend.Describe()))
	}

	w.mu.Lock()
	w.version = version
	if w.generation == gen {
		w.dirty = false
		// A Flush that arrived while this attempt was in flight is
		// satisfied by it: nothing newer is dirty, so Flush returns as
		// soon as it sees dirty clear. Disarm forceNow here too --
		// otherwise it stays armed for the next unrelated MarkDirty,
		// which then skips MinInterval (#941).
		w.forceNow = false
	}
	w.mu.Unlock()
}
