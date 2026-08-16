// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingBackend is an in-memory Backend that counts Save calls and can
// be told to block every call (Load and Save alike) until released --
// the same "stall until armed/released" shape internal/auth's own
// stallingBackend uses, kept local to this package rather than shared
// across packages (each package keeps its own small test double, the
// precedent internal/detect's own doc comment on isPublic already sets).
type countingBackend struct {
	mu      sync.Mutex
	payload []byte
	version int64

	saves       int
	loads       int
	blockSave   bool
	release     chan struct{}
	entered     chan struct{} // signals "a Save just started blocking" -- see waitUntilBlocked
	inFlight    int
	maxInFlight int
	sawDeadline bool
}

func newCountingBackend() *countingBackend {
	return &countingBackend{release: make(chan struct{}), entered: make(chan struct{}, 1)}
}

// waitUntilBlocked waits for a Save call to actually reach its blocking
// wait, rather than assuming a fixed sleep is long enough -- the same
// "assert the behaviour, not the speed of the machine" lesson
// flags.TestPersistLockedRateLimitsWrites' own doc comment records.
func (b *countingBackend) waitUntilBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Save never reached its blocking wait")
	}
}

func (b *countingBackend) Load(ctx context.Context) (Snapshot, error) {
	b.mu.Lock()
	b.loads++
	snap := Snapshot{Payload: b.payload, Version: b.version, Exists: b.version != 0}
	b.mu.Unlock()
	return snap, nil
}

func (b *countingBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	if expect != b.version {
		b.mu.Unlock()
		return 0, ErrConflict
	}
	block := b.blockSave
	if _, ok := ctx.Deadline(); ok {
		b.sawDeadline = true
	}
	b.inFlight++
	if b.inFlight > b.maxInFlight {
		b.maxInFlight = b.inFlight
	}
	b.mu.Unlock()

	if block {
		select {
		case b.entered <- struct{}{}:
		default:
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			b.mu.Lock()
			b.inFlight--
			b.mu.Unlock()
			return 0, ctx.Err()
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight--
	b.saves++
	b.payload = payload
	b.version++
	return b.version, nil
}

func (b *countingBackend) Close() error     { return nil }
func (b *countingBackend) Describe() string { return "counting test backend" }

func (b *countingBackend) stats() (saves, loads, maxInFlight int, sawDeadline bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves, b.loads, b.maxInFlight, b.sawDeadline
}

func (b *countingBackend) setBlocking(blocking bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blockSave = blocking
}

// waitForSaves polls until b has recorded at least n saves, rather than
// sleeping a fixed duration and hoping the writer goroutine has caught
// up -- same "assert the behaviour, not the speed of the machine"
// reasoning as waitUntilBlocked above.
func waitForSaves(t *testing.T, b *countingBackend, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if saves, _, _, _ := b.stats(); saves >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("saves never reached %d", n)
}

// failingBackend always fails Save, so a caller can prove back-off
// behaviour without needing a real stall.
type failingBackend struct {
	mu    sync.Mutex
	saves int
}

func (b *failingBackend) Load(ctx context.Context) (Snapshot, error) { return Snapshot{}, nil }
func (b *failingBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.saves++
	b.mu.Unlock()
	return 0, errors.New("backend unavailable")
}
func (b *failingBackend) Close() error     { return nil }
func (b *failingBackend) Describe() string { return "failing test backend" }
func (b *failingBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

// unreadableBackend always fails Load -- the "document exists but can't
// be read" case #378's Open refuses to build a store around.
type unreadableBackend struct{}

func (unreadableBackend) Load(ctx context.Context) (Snapshot, error) {
	return Snapshot{}, errors.New("disk on fire")
}
func (unreadableBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("must never be called")
}
func (unreadableBackend) Close() error     { return nil }
func (unreadableBackend) Describe() string { return "unreadable test backend" }

// TestWriteBehindMarkDirtyNeverBlocksOnAStuckBackend proves the central
// write-behind claim: a caller marking state dirty gets control back
// immediately even while the writer goroutine is stuck inside a Save
// call that never returns on its own -- the "reads/evaluation continuing
// while a save is stuck" proof #400 requires.
func TestWriteBehindMarkDirtyNeverBlocksOnAStuckBackend(t *testing.T) {
	b := newCountingBackend()
	b.setBlocking(true)

	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		// Closing (not just flipping the flag) unblocks every Save
		// currently parked on <-b.release, including one already
		// in-flight when this cleanup runs -- setBlocking(false) alone
		// only affects the *next* call, since an in-flight Save already
		// captured the old value.
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		wb.Close(ctx)
	}()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			wb.MarkDirty([]byte(`{"n":1}`))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("MarkDirty blocked -- a stuck backend must never reach the caller")
	}
}

// TestWriteBehindSaveDeadline proves every Save the writer goroutine
// performs carries a deadline -- #380's first item -- by blocking the
// backend forever and confirming the context it receives has one, and
// that the writer eventually gives up and retries rather than hanging.
func TestWriteBehindSaveDeadline(t *testing.T) {
	b := newCountingBackend()
	b.setBlocking(true)

	restore := SaveTimeout
	SaveTimeout = 50 * time.Millisecond
	defer func() { SaveTimeout = restore }()

	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		wb.Close(ctx)
	}()
	wb.MarkDirty([]byte(`{"n":1}`))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, sawDeadline := b.stats(); sawDeadline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no Save call ever carried a context deadline")
}

// TestWriteBehindBackoffCostsOneAttemptPerWindow is the #377 proof: a
// backend that fails every call must not be attempted once per MarkDirty
// -- a sustained outage costs one attempt per back-off window.
func TestWriteBehindBackoffCostsOneAttemptPerWindow(t *testing.T) {
	b := &failingBackend{}
	interval := 200 * time.Millisecond

	var errs int
	var mu sync.Mutex
	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{
		MinInterval: interval,
		OnSaveError: func(string) { mu.Lock(); errs++; mu.Unlock() },
	}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		wb.Close(ctx)
	}()

	// A burst of 500 events during less than one back-off window --
	// simulating a sustained-rate hot path hitting a stalled backend.
	start := time.Now()
	for i := 0; i < 500; i++ {
		wb.MarkDirty([]byte(`{"n":1}`))
	}
	elapsed := time.Since(start)
	if elapsed >= interval {
		t.Fatalf("test setup: marking 500 events took %v, want well under the %v window", elapsed, interval)
	}

	time.Sleep(interval / 2)
	saves := b.count()
	if saves > 1 {
		t.Errorf("500 events inside one back-off window produced %d attempts, want at most 1", saves)
	}

	// Give it a few more windows and confirm attempts stay bounded to
	// "roughly one per window", not "one per event".
	time.Sleep(3 * interval)
	saves = b.count()
	if saves > 6 {
		t.Errorf("got %d attempts over ~4 back-off windows against a permanently failing backend -- want roughly one per window, not one per event", saves)
	}
	if saves < 2 {
		t.Errorf("got %d attempts -- expected the writer to keep retrying across windows, not give up", saves)
	}
}

// TestWriteBehindStampsAfterNotBefore is #377's defect, reproduced
// directly: an attempt that itself takes longer than MinInterval must
// not let the next attempt start immediately once it returns -- the old
// "stamp lastPersist, then write" shape did exactly that, because by the
// time a slow write returned, the interval had already elapsed.
func TestWriteBehindStampsAfterNotBefore(t *testing.T) {
	b := newCountingBackend()
	interval := 100 * time.Millisecond

	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: interval}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		wb.Close(ctx)
	}()

	// First attempt blocks for well over one interval -- the hold itself
	// (not just the eventual Save call) has to outlast interval, or a
	// "stamp before the write" bug and a "stamp after" fix are
	// indistinguishable: released too quickly, neither implementation
	// has had time for the interval to elapse during the block, so both
	// would (correctly, for the wrong reason) debounce the second write.
	// Held past the interval, the two implementations disagree: a
	// before-stamp already has the interval elapsed by release time (the
	// clock started at attempt *start*), so it fires the second write
	// immediately; an after-stamp starts its clock at release and still
	// owes a full interval.
	b.setBlocking(true)
	wb.MarkDirty([]byte(`{"n":1}`))
	b.waitUntilBlocked(t) // deterministic: wait for the writer to actually be inside Save, not a fixed sleep
	time.Sleep(2 * interval)
	longAttemptStart := time.Now()
	b.release <- struct{}{} // release exactly once -- the first attempt
	// Immediately mark dirty again -- if the stamp happened before the
	// (slow) write instead of after, this would be free to fire right
	// away since the interval already elapsed during the block.
	b.setBlocking(false)
	wb.MarkDirty([]byte(`{"n":2}`))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if saves, _, _, _ := b.stats(); saves >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	saves, _, _, _ := b.stats()
	if saves < 2 {
		t.Fatal("second write never landed")
	}
	if gap := time.Since(longAttemptStart); gap < interval {
		t.Errorf("second attempt landed only %v after the first was released, want at least MinInterval (%v) -- "+
			"the rate limiter must stamp after the write completes, not before it starts", gap, interval)
	}
}

// TestOpenWriteBehindNeverAttachesToAFailedLoad is #400's critical
// invariant, proven directly: a document that exists but can't be
// loaded must produce no *WriteBehind at all, so there is no object a
// caller could ever MarkDirty against for a store in that state.
func TestOpenWriteBehindNeverAttachesToAFailedLoad(t *testing.T) {
	wb, existed, err := OpenWriteBehind(context.Background(), unreadableBackend{}, "the test store", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error {
		t.Fatal("decode must never be called when Load itself failed")
		return nil
	})
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("OpenWriteBehind error = %v, want a *StartupError", err)
	}
	if existed {
		t.Error("existed = true on a failed load")
	}
	if wb != nil {
		t.Fatal("OpenWriteBehind returned a non-nil *WriteBehind against a failed load -- " +
			"a queued MarkDirty could now write over data nothing has actually confirmed is stale")
	}
}

// TestOpenWriteBehindDecodeFailureAlsoRefuses covers the second half of
// Open's fail-closed contract: a document that loads fine but doesn't
// parse must equally produce no *WriteBehind.
func TestOpenWriteBehindDecodeFailureAlsoRefuses(t *testing.T) {
	b := newCountingBackend()
	b.payload = []byte(`not json`)
	b.version = 1

	wb, _, err := OpenWriteBehind(context.Background(), b, "the test store", WriteBehindOptions{MinInterval: time.Millisecond}, func(data []byte) error {
		return errors.New("bad json")
	})
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("OpenWriteBehind error = %v, want a *StartupError", err)
	}
	if wb != nil {
		t.Fatal("OpenWriteBehind returned a non-nil *WriteBehind against a decode failure")
	}
}

// TestWriteBehindCloseFlushesFinalDirtyState proves Close performs a
// final save of whatever is still dirty (bounded by the deadline)
// before returning, rather than dropping it -- the "no lost final
// write" half of #400's shutdown contract.
func TestWriteBehindCloseFlushesFinalDirtyState(t *testing.T) {
	b := newCountingBackend()
	// A long interval, so ordinary debounce would not have flushed this
	// write on its own within the test's lifetime.
	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: time.Hour}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}

	wb.MarkDirty([]byte(`{"final":true}`))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wb.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	saves, _, _, _ := b.stats()
	if saves == 0 {
		t.Error("Close returned without ever saving the pending dirty state")
	}
	if string(b.payload) != `{"final":true}` {
		t.Errorf("final persisted payload = %q, want the last MarkDirty payload", b.payload)
	}
}

// TestWriteBehindFlushForcesAnAttemptNow proves Flush -- the test/-backup
// checkpoint primitive -- bypasses MinInterval without stopping the
// writer goroutine.
func TestWriteBehindFlushForcesAnAttemptNow(t *testing.T) {
	b := newCountingBackend()
	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: time.Hour}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		wb.Close(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// The very first MarkDirty ever, on a freshly-opened WriteBehind,
	// attempts immediately regardless of MinInterval -- there is no
	// prior attempt to debounce against, matching every adopting store's
	// existing "the first write happens immediately (empty lastPersist)"
	// contract (see flags.TestPersistLockedRateLimitsWrites). Waited out
	// here (rather than asserted on) so it doesn't race with the
	// deliberate Flush below.
	wb.MarkDirty([]byte(`{"n":1}`))
	waitForSaves(t, b, 1)

	// A second MarkDirty would ordinarily be debounced for a full hour;
	// Flush must force it through immediately regardless.
	wb.MarkDirty([]byte(`{"n":2}`))
	if err := wb.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if saves, _, _, _ := b.stats(); saves != 2 {
		t.Errorf("saves after Flush = %d, want 2", saves)
	}

	// A third Flush with nothing dirty must not attempt again.
	if err := wb.Flush(ctx); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if saves, _, _, _ := b.stats(); saves != 2 {
		t.Errorf("saves after a no-op Flush = %d, want still 2", saves)
	}
}

// TestWriteBehindFlushDoesNotRaceTheWriterGoroutine proves Flush routes
// through the single writer goroutine rather than attempting
// independently: calling Flush concurrently with the writer's own
// natural attempt (both racing to persist the very first MarkDirty on a
// freshly-opened WriteBehind) must never produce two Save calls, and
// must never provoke an ErrConflict retry against a backend nothing else
// is touching.
func TestWriteBehindFlushDoesNotRaceTheWriterGoroutine(t *testing.T) {
	b := newCountingBackend()
	wb, _, err := OpenWriteBehind(context.Background(), b, "test store", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		wb.Close(ctx)
	}()

	// The first-ever MarkDirty attempts immediately on the writer
	// goroutine (see this file's other tests) -- calling Flush in the
	// same instant used to spawn a second, independent attempt() racing
	// it for the same backend write.
	wb.MarkDirty([]byte(`{"n":1}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wb.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if saves, _, _, _ := b.stats(); saves != 1 {
		t.Errorf("saves = %d, want exactly 1 -- Flush must not race the writer goroutine's own attempt", saves)
	}
}

// TestWriteBehindNilIsANoop mirrors engine.Engine's own nil-receiver
// convention: every method here must be safe to call on a nil
// *WriteBehind, so a store with no backend configured doesn't need its
// own nil check at every call site.
func TestWriteBehindNilIsANoop(t *testing.T) {
	var wb *WriteBehind
	wb.MarkDirty([]byte(`{}`))
	if err := wb.Flush(context.Background()); err != nil {
		t.Errorf("Flush on nil = %v, want nil", err)
	}
	if err := wb.Close(context.Background()); err != nil {
		t.Errorf("Close on nil = %v, want nil", err)
	}
}

// TestOpenWriteBehindWithNilBackendIsInMemoryOnly matches every store's
// existing "empty path -> in-memory only" contract: a nil Backend is not
// an error, and produces a nil *WriteBehind rather than one wired to
// nothing.
func TestOpenWriteBehindWithNilBackendIsInMemoryOnly(t *testing.T) {
	wb, existed, err := OpenWriteBehind(context.Background(), nil, "test store", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error {
		t.Fatal("decode must never be called against a nil backend")
		return nil
	})
	if err != nil {
		t.Fatalf("OpenWriteBehind: %v", err)
	}
	if existed {
		t.Error("existed = true against a nil backend")
	}
	if wb != nil {
		t.Error("expected a nil *WriteBehind for a nil backend")
	}
}
