// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

// evalQueueSize mirrors internal/detect.observeQueueSize's own sizing
// reasoning exactly -- a generous burst absorber, not a guarantee against
// sustained overload (nothing bounded can be one). Kept the same size as
// detection's queue for consistency, not because evaluation is expected
// to be equally expensive per event; per-match cost here (an fsync) is
// far higher than a detector's, but matches are rare by this feature's
// own premise (#243 section 4), so the common case is a fast "no entry
// matched" pass, much cheaper than a detector's per-event work.
const evalQueueSize = 4096

// evalQueueDropLogInterval mirrors
// internal/detect.observeQueueDropLogInterval's reasoning: rate-limit
// the overload log line itself, since logging every drop would add load
// during exactly the condition being reported.
const evalQueueDropLogInterval = 30 * time.Second

// Evaluator asynchronously matches ingested events against the current
// entry set and records matches to a matchlog.Store.
//
// This exists, rather than evaluating inline on the ingest goroutine
// that calls Enqueue, for the same reason internal/detect.Detector does
// -- documented directly on mikroview's ingest goroutine's own doc
// comment: "a slow or backed-up detection pass must never delay store
// insertion or WebSocket broadcast." A watchlist match is materially
// more expensive per hit than a detector's own work (matchlog.Append
// fsyncs), and issue #221 already showed what an inline, unbounded-cost
// path on that single goroutine does under sustained real-world load
// (event ring/live view/detection all degraded until the queue was
// found and fixed) -- this queue exists so the same failure mode cannot
// recur here.
type Evaluator struct {
	entries  *Store
	matchLog matchlog.Store
	queue    chan store.Event

	dropped          atomic.Uint64
	lastDropLogNanos atomic.Int64
}

// NewEvaluator constructs an Evaluator. matchLog may be nil -- see
// Enqueue.
func NewEvaluator(entries *Store, matchLog matchlog.Store) *Evaluator {
	return &Evaluator{entries: entries, matchLog: matchLog, queue: make(chan store.Event, evalQueueSize)}
}

// Enqueue hands e off to the evaluation-worker goroutine (see Run)
// without ever blocking the caller -- a non-blocking select/default
// send, dropping e if the queue is full. A dropped event is still
// stored/broadcast/detected normally; only watchlist evaluation for that
// one event is skipped.
//
// A nil matchLog (the match log failed to open at startup -- see
// main.go) makes Enqueue itself a no-op rather than queuing work Run
// could never complete: there is nowhere for a match to be recorded, so
// there is no point spending queue capacity or a goroutine wakeup on it.
func (ev *Evaluator) Enqueue(e store.Event) {
	if ev == nil || ev.matchLog == nil {
		return
	}
	select {
	case ev.queue <- e:
	default:
		ev.recordDropped()
	}
}

func (ev *Evaluator) recordDropped() {
	total := ev.dropped.Add(1)
	now := time.Now().UnixNano()
	last := ev.lastDropLogNanos.Load()
	if now-last < int64(evalQueueDropLogInterval) {
		return
	}
	if ev.lastDropLogNanos.CompareAndSwap(last, now) {
		persistLog.Warn(fmt.Sprintf("watchlist evaluation queue full -- %d event(s) dropped from evaluation so far (still stored/broadcast/detected normally)", total))
	}
}

// Run drains the queue, evaluating each event against every entry, until
// ctx is done. Meant to run in its own goroutine, separate from whatever
// goroutine calls Enqueue.
func (ev *Evaluator) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ev.queue:
			ev.evaluateRecovered(e)
		}
	}
}

// evaluateRecovered isolates panic recovery to a single event rather
// than Run's whole lifetime -- recover only unwinds as far as the
// nearest deferring function, so a defer in Run itself would end the
// entire evaluation goroutine for good on the first bad event, the same
// reasoning ingestOneRecovered's own doc comment (main.go) gives for its
// identical shape.
func (ev *Evaluator) evaluateRecovered(e store.Event) {
	defer logging.Recover(persistLog)
	for _, entry := range ev.entries.entriesSnapshot() {
		tuple, outcome := Match(entry, e)
		switch outcome {
		case Violation:
			if err := ev.matchLog.Append(entry.ID, tuple, e, e.ReceivedAt); err != nil {
				// ErrCapacityReached is the expected, already-documented
				// steady state once a deployment's match log fills (#243
				// section 3: refused, not silently overwritten) -- surfaced
				// here rather than swallowed, since from this point on
				// every further genuinely-new match for this entry is
				// silently lost until the operator acts.
				persistLog.Warn(fmt.Sprintf("recording a match for entry %q failed: %v", entry.ID, err))
			}
		case Observed:
			// An inverted entry still observing -- record the candidate
			// destination, fire nothing (#243 section 5).
			ev.entries.RecordObservation(entry.ID, tuple.DestIP, tuple.Port, e.ReceivedAt)
		}
	}
}
