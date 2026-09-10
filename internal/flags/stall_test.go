// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// fakeClock is a persist.Clock a test drives by hand -- no sleeps, so
// none of the timing flakiness #941 traces to (internal/logging's own
// fakeClock, internal/logging/limiter_test.go, is the same "now advances
// only when told to" idea; this one also fakes the timer run's back-off
// wait blocks on, since unlike Limiter, WriteBehind's run loop actually
// waits rather than just comparing timestamps). All fakeTimer state is
// guarded by the clock's own mutex, since Stop (called from the writer
// goroutine) and advance (called from the test goroutine) touch the same
// timer concurrently.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiting []*fakeTimer
}

type fakeTimer struct {
	clock    *fakeClock
	deadline time.Time
	c        chan time.Time
	fired    bool
	stopped  bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) persist.ClockTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, deadline: c.now.Add(d), c: make(chan time.Time, 1)}
	c.waiting = append(c.waiting, t)
	return t
}

// advance moves the fake clock forward by d and fires, in deadline
// order, every pending timer whose deadline the new time has reached --
// exactly what a real clock would do over that span, without spending
// any real wall-clock time doing it. Timers already fired or stopped are
// dropped rather than kept, so pending (below) reflects only what's
// genuinely still waiting.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	var remaining []*fakeTimer
	for _, t := range c.waiting {
		if t.stopped || t.fired {
			continue
		}
		if !t.deadline.After(c.now) {
			t.fired = true
			t.c <- c.now
			continue
		}
		remaining = append(remaining, t)
	}
	c.waiting = remaining
}

// pending reports how many timers are currently registered and still
// waiting (neither fired nor stopped) -- a test's synchronisation point
// for "the writer goroutine has finished handling its last attempt and
// settled into its next wait" (see waitForPendingTimer), needed because
// otherwise advance can race ahead of the writer stamping lastAttempt
// from this same clock, pushing the next deadline a whole window later
// than the test expects.
func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.waiting {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	already := t.fired
	t.stopped = true
	return !already
}

// stallingSaveBackend blocks every Save call until released -- the same
// "stall until armed/released" shape internal/auth's own stallingBackend
// uses (internal/auth/stall_test.go), kept as its own small copy per
// this codebase's "each package keeps its own small private copy"
// convention rather than shared across packages.
type stallingSaveBackend struct {
	mu       sync.Mutex
	release  chan struct{}
	version  int64
	inFlight int
	maxIn    int
}

func newStallingSaveBackend() *stallingSaveBackend {
	return &stallingSaveBackend{release: make(chan struct{})}
}

func (b *stallingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *stallingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxIn {
		b.maxIn = b.inFlight
	}
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		b.mu.Lock()
		b.inFlight--
		b.mu.Unlock()
		return 0, ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight--
	b.version++
	return b.version, nil
}

func (b *stallingSaveBackend) Close() error     { return nil }
func (b *stallingSaveBackend) Describe() string { return "stalling test backend" }

func (b *stallingSaveBackend) maxInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxIn
}

// TestAddDoesNotBlockOnAStuckBackend is #400's central proof for this
// store, one of #377's three named copies: Add used to hold s.mu across
// persistLocked's own backend call, so a stalled backend blocked every
// Add -- and, transitively, every read taking s.mu.RLock, since a
// writer waiting on a stuck Lock starves later readers behind it -- for
// as long as the backend stayed stuck. With persist.WriteBehind, Add
// only ever encodes and hands the snapshot to the writer goroutine; the
// backend call happens entirely off this path.
func TestAddDoesNotBlockOnAStuckBackend(t *testing.T) {
	b := newStallingSaveBackend()
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	// The first Add's own write attempts immediately and blocks the
	// writer goroutine inside Save -- exactly the state a stuck backend
	// leaves the store in.
	s.Add(TypePortScan, "203.0.113.1", "first", time.Now())

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Add(TypeActivitySpike, "203.0.113.2", "still responsive", time.Now())
			s.List()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Add/List blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if got := b.maxInFlight(); got > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1 -- the writer goroutine must serialise its own backend I/O", got)
	}
}

// failingSaveBackend always fails Save, for the back-off proof below.
type failingSaveBackend struct {
	mu    sync.Mutex
	saves int
}

func (b *failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (b *failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.saves++
	b.mu.Unlock()
	return 0, errors.New("backend unavailable")
}
func (b *failingSaveBackend) Close() error     { return nil }
func (b *failingSaveBackend) Describe() string { return "failing test backend" }
func (b *failingSaveBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

// waitForSaveCount polls (real time, bounded) until b has recorded want
// attempts, or fails the test -- synchronising with the writer goroutine
// picking up a fake-clock advance, not measuring the property under
// test. The property itself (exactly one attempt per advanced window)
// is asserted by the caller once this returns.
func waitForSaveCount(t *testing.T, b *failingSaveBackend, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.count() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d save attempts, got %d", want, b.count())
}

// waitForPendingTimer polls (real time, bounded) until the writer
// goroutine has registered its next back-off wait on clk -- see
// fakeClock.pending's own doc comment for why advancing before this
// point races the writer stamping lastAttempt from the same clock.
func waitForPendingTimer(t *testing.T, clk *fakeClock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clk.pending() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for the writer to register its back-off timer")
}

// TestSustainedFailuresCostOneAttemptPerBackoffWindow is #377's proof
// for this store: a permanently failing backend under a sustained
// stream of Add calls (a real re-fire burst, the scenario
// persistMinInterval exists for) must not be attempted once per event.
//
// #941: this used to drive the back-off window with a real time.Sleep
// and infer how many windows "must" have passed from the elapsed wall
// time, with a slack of 2 to cover a starved runner sleeping longer than
// asked. That slack still wasn't enough under three queued pipelines on
// the shared runner (pipeline 480, job 3832: 6 attempts against a
// computed ceiling of 5). Rather than raise the slack again, this now
// drives persist.WriteBehind's own back-off clock (see
// persistClock/persist.Clock) by hand: no real sleep, no elapsed-time
// arithmetic, and the assertion is exact -- one attempt per window
// advanced, never more, regardless of how loaded the runner is.
func TestSustainedFailuresCostOneAttemptPerBackoffWindow(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = time.Second
	defer func() { persistMinInterval = orig }()

	clk := newFakeClock()
	persistClock = clk
	defer func() { persistClock = nil }()

	b := &failingSaveBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	now := time.Now()
	for i := 0; i < 500; i++ {
		s.Add(TypePortScan, "203.0.113.9", "re-fire", now)
	}

	// The first Add's own MarkDirty wakes the writer straight into its
	// first attempt -- no wait, since lastAttempt starts zero.
	waitForSaveCount(t, b, 1)

	// Three back-off windows, advanced by the test rather than slept
	// through: exactly one further attempt per window, no slack needed.
	// waitForPendingTimer first, so the writer has already stamped
	// lastAttempt and registered its next wait from the clock as it
	// stands right now -- advancing before that races the writer's own
	// read of "now" (see fakeClock.pending).
	for window := 2; window <= 4; window++ {
		waitForPendingTimer(t, clk)
		clk.advance(persistMinInterval)
		waitForSaveCount(t, b, window)
	}

	// Give the writer goroutine a brief real moment to prove it does NOT
	// attempt again on its own before the next advance -- this is
	// synchronisation slack for the scheduler, not slack in the
	// assertion: the count checked below must be exact.
	time.Sleep(20 * time.Millisecond)
	if saves := b.count(); saves != 4 {
		t.Errorf("500 re-fires against a permanently failing backend produced %d attempts over 3 back-off windows (plus the first immediate one), want exactly 4 -- one per window, no slack", saves)
	}
}
