// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeDef is a minimal Evaluated used to drive the chassis without any
// real detection/expectation logic -- exactly what this issue's tests
// need, since #398 builds no definition kinds at all.
type fakeDef struct {
	id   string
	kind string

	// delay, if non-zero, is set once before the definition is ever
	// evaluated (never mutated concurrently with Evaluate) -- it
	// simulates a definition too slow to finish draining a backlog
	// within drainTimeout, for TestRunStopsWithinDrainTimeoutUnderSustainedBacklog.
	delay time.Duration

	calls       atomic.Int64
	shouldPanic atomic.Bool
}

func (f *fakeDef) ID() string   { return f.id }
func (f *fakeDef) Kind() string { return f.kind }

func (f *fakeDef) Evaluate(e store.Event) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.shouldPanic.Load() {
		panic("fakeDef: intentional test panic")
	}
}

func evt(srcIP string) store.Event {
	return store.Event{SrcIP: srcIP, ReceivedAt: time.Now()}
}

// withBatchSize/withDrainTimeout shrink the package-level tuning vars
// for the duration of a test -- same convention as
// internal/detect.maxTrackedSources: a var rather than a const purely so
// tests can shrink it, restored via t.Cleanup so tests never leak state
// into each other.
func withBatchSize(t *testing.T, n int) {
	t.Helper()
	orig := batchSize
	batchSize = n
	t.Cleanup(func() { batchSize = orig })
}

// newEngineOnStore builds an engine reading from its own ring, which is
// how main.go builds the real one -- the store is the engine's source of
// events now, so a test that drives evaluation drives it through a store.
func newEngineOnStore(t *testing.T, capacity int) (*Engine, *store.Store) {
	t.Helper()
	st := store.New(capacity, time.Hour)
	return New(st), st
}

// storeAndNudge stores n events and rings the doorbell after each,
// exactly as main.go's ingest goroutine does.
func storeAndNudge(e *Engine, st *store.Store, n int) {
	for i := 0; i < n; i++ {
		st.Insert(evt("198.51.100.1"))
		e.Nudge()
	}
}

// waitFor polls until cond holds, failing the test if it never does --
// the engine evaluates on its own goroutine, so every "it got there"
// assertion in this file is eventually-true rather than immediate.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func withDrainTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := drainTimeout
	drainTimeout = d
	t.Cleanup(func() { drainTimeout = orig })
}

// ---- cursor / nudge ----

// TestNudgeNeverBlocksHoweverOftenItIsRung is the property that replaced
// the old queue's drop policy: ingest must never wait on evaluation, and
// with nothing consuming the doorbell there is still nothing to wait for.
func TestNudgeNeverBlocksHoweverOftenItIsRung(t *testing.T) {
	e, st := newEngineOnStore(t, 100)

	done := make(chan struct{})
	go func() {
		defer close(done)
		storeAndNudge(e, st, 1000) // Run is never started here: nothing answers
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Nudge blocked with no engine running, want a non-blocking doorbell")
	}
	// The property worth guaranteeing here is exactly the one this test
	// is named for: Nudge never blocked, however many times it was rung
	// with nothing answering. It is not true that nothing was lost --
	// the store's own capacity is 100, so it evicted 900 of these 1000
	// events from its ring long before any engine could have reached
	// them; TestOutrunCountsWhatTheRingWrappedPast is what actually
	// exercises that loss being counted.
	if _, oldestHeld, newestHeld := st.Since(0, 0); newestHeld-oldestHeld+1 != 100 {
		t.Fatalf("store held %d event(s) after 1000 inserts into a 100-capacity ring, want exactly the 100 it can hold", newestHeld-oldestHeld+1)
	}
}

// TestRunEvaluatesEveryStoredEventAndAdvancesTheCursor is the core of
// #1109: what is evaluated is what the store holds past the cursor, and
// the cursor ends up on the newest event rather than anywhere short of
// it.
func TestRunEvaluatesEveryStoredEventAndAdvancesTheCursor(t *testing.T) {
	e, st := newEngineOnStore(t, 1000)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer func() {
		cancel()
		<-e.Done()
	}()
	waitForRunning(t, e)

	const n = 10
	storeAndNudge(e, st, n)

	waitFor(t, "every stored event to be evaluated", func() bool { return d.calls.Load() == n })
	if got := e.cursor.Load(); got != uint64(n) {
		t.Fatalf("cursor = %d after %d events, want %d", got, n, n)
	}
	behind, _, outrun := e.Lag()
	if behind != 0 || outrun != 0 {
		t.Fatalf("Lag() = (behind %d, outrun %d) once caught up, want (0, 0)", behind, outrun)
	}
}

// TestOneNudgeCatchesUpOnAWholeBurst is what makes the doorbell safe: a
// nudge that arrives while the engine is busy is coalesced away, so
// "caught up" has to mean the store is empty past the cursor, not that
// every ring has been answered individually. Nudged exactly once for a
// thousand events, across several batch boundaries.
func TestOneNudgeCatchesUpOnAWholeBurst(t *testing.T) {
	withBatchSize(t, 64)
	e, st := newEngineOnStore(t, 2000)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer func() {
		cancel()
		<-e.Done()
	}()
	waitForRunning(t, e)

	const n = 1000
	for i := 0; i < n; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	e.Nudge()

	waitFor(t, "one nudge to cover the whole burst", func() bool { return d.calls.Load() == n })
}

// TestRunDoesNotReplayAStoreItInherited -- the cursor starts at the
// store's newest ID, so a warm restart (a restored ring, or an engine
// started after ingest) evaluates what arrives next rather than raising
// the whole retention window again.
func TestRunDoesNotReplayAStoreItInherited(t *testing.T) {
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)
	for i := 0; i < 5; i++ {
		st.Insert(evt("198.51.100.1"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer func() {
		cancel()
		<-e.Done()
	}()
	waitForRunning(t, e)

	storeAndNudge(e, st, 1)
	waitFor(t, "the one new event to be evaluated", func() bool { return d.calls.Load() == 1 })
	if _, _, outrun := e.Lag(); outrun != 0 {
		t.Fatalf("outrun = %d for a store the engine simply started level with, want 0", outrun)
	}
	if got := d.calls.Load(); got != 1 {
		t.Fatalf("definition saw %d events, want only the one that arrived after Run started", got)
	}
}

// ---- outrun: the only loss left ----

// TestOutrunCountsWhatTheRingWrappedPast is the replacement for the old
// queue-full drop count, and the reason it is a different measure: it
// takes a flood big enough to overrun the whole retention window, not one
// big enough to overrun 4096 queue slots.
func TestOutrunCountsWhatTheRingWrappedPast(t *testing.T) {
	e, st := newEngineOnStore(t, 10)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	for i := 0; i < 30; i++ {
		st.Insert(evt("198.51.100.1")) // IDs 21..30 survive
	}
	e.evaluateBatch(context.Background(), time.Time{})

	if got := d.calls.Load(); got != 10 {
		t.Fatalf("definition saw %d events, want the 10 the ring still held", got)
	}
	behind, _, outrun := e.Lag()
	if outrun != 20 {
		t.Fatalf("outrun = %d, want the 20 events evicted before the engine reached them", outrun)
	}
	if behind != 0 {
		t.Fatalf("behind = %d after catching up on what survived, want 0", behind)
	}
}

// TestForgetStartsLevelAfterADeliberateReset -- the test-only reset
// route empties the store on purpose and tells the engine so; the
// engine then starts level with the store and carries no outrun, rather
// than counting the wipe as a flood (which TestOutrunCountsAStoreReset
// shows is what a bare Reset looks like from here).
func TestForgetStartsLevelAfterADeliberateReset(t *testing.T) {
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	for i := 0; i < 10; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	e.cursor.Store(3) // 1..3 evaluated, 4..10 not yet
	e.outrun.Store(5) // residue from an earlier flood
	st.Reset()
	e.Forget()
	for i := 0; i < 2; i++ {
		st.Insert(evt("198.51.100.1")) // IDs 11, 12
	}
	e.evaluateBatch(context.Background(), time.Time{})

	if behind, _, outrun := e.Lag(); outrun != 0 || behind != 0 {
		t.Fatalf("Lag() = (behind %d, outrun %d) after Forget, want (0, 0)", behind, outrun)
	}
	if got := d.calls.Load(); got != 2 {
		t.Fatalf("definition saw %d events, want the 2 stored after the reset", got)
	}
}

// TestForgetRendezvousesWithARunningEngine exercises the branch
// runOnEvaluationGoroutine takes when Run is actively driving evaluation
// (e.running == true): Forget must hand its work to Run's tasks channel
// and wait for it there, rather than running inline -- the branch
// TestForgetStartsLevelAfterADeliberateReset never reaches, since it
// never starts Run at all. Forget's own doc comment says this rendezvous
// is what stops a Reset from racing a batch in flight and writing an
// older cursor back over the reset one, so the case worth proving is
// Run genuinely busy in catchUp when Forget is called, not merely alive.
func TestForgetRendezvousesWithARunningEngine(t *testing.T) {
	e, st := newEngineOnStore(t, 4096)
	d := &fakeDef{id: "slow", kind: "declarative", delay: 2 * time.Millisecond}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer func() {
		cancel()
		<-e.Done()
	}()
	waitForRunning(t, e)

	storeAndNudge(e, st, 200)
	waitFor(t, "evaluation to be underway", func() bool { return d.calls.Load() > 0 })

	st.Reset()
	e.Forget()

	if behind, _, outrun := e.Lag(); behind != 0 || outrun != 0 {
		t.Fatalf("Lag() = (behind %d, outrun %d) after Forget on a running engine, want (0, 0)", behind, outrun)
	}
}

// TestOutrunCountsAStoreReset -- Reset empties the ring without rewinding
// its IDs (see store.Store.Reset), so events the engine had not reached
// are gone exactly as an eviction would leave them, and must be counted
// the same way rather than silently skipped.
func TestOutrunCountsAStoreReset(t *testing.T) {
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	for i := 0; i < 10; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	e.cursor.Store(3) // 1..3 evaluated, 4..10 not yet
	st.Reset()
	for i := 0; i < 2; i++ {
		st.Insert(evt("198.51.100.1")) // IDs 11, 12
	}
	e.evaluateBatch(context.Background(), time.Time{})

	if _, _, outrun := e.Lag(); outrun != 7 {
		t.Fatalf("outrun = %d after a Reset over 7 unevaluated events, want 7", outrun)
	}
	if got := d.calls.Load(); got != 2 {
		t.Fatalf("definition saw %d events, want the 2 stored after the Reset", got)
	}
}

// TestOutrunCountsAShrinkingResize -- lowering store.maxMemory evicts
// oldest-first, the same direction an ordinary wrap does, so it reads as
// the same loss to a cursor that was behind the new capacity.
func TestOutrunCountsAShrinkingResize(t *testing.T) {
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	for i := 0; i < 60; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	e.cursor.Store(3)
	if kept, evicted := st.Resize(10); kept != 10 || evicted != 50 {
		t.Fatalf("Resize(10) kept %d evicted %d, want 10 and 50", kept, evicted)
	}
	e.evaluateBatch(context.Background(), time.Time{})

	if _, _, outrun := e.Lag(); outrun != 47 {
		t.Fatalf("outrun = %d after shrinking past 47 unevaluated events, want 47", outrun)
	}
	if got := d.calls.Load(); got != 10 {
		t.Fatalf("definition saw %d events, want the 10 that survived the shrink", got)
	}

	// Growing loses nothing, so it adds nothing to the count.
	st.Resize(200)
	for i := 0; i < 5; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	e.evaluateBatch(context.Background(), time.Time{})
	if _, _, outrun := e.Lag(); outrun != 47 {
		t.Fatalf("outrun = %d after growing the ring, want it unchanged at 47", outrun)
	}
}

// ---- lag ----

// TestLagReportsHowFarBehindAndHowLate covers what /api/stats serves: a
// backlog is late, not lost, and the two numbers say so in the terms the
// Engine Room readout uses.
func TestLagReportsHowFarBehindAndHowLate(t *testing.T) {
	e, st := newEngineOnStore(t, 100)

	if behind, seconds, outrun := e.Lag(); behind != 0 || seconds != 0 || outrun != 0 {
		t.Fatalf("Lag() on a fresh engine = (%d, %v, %d), want all zero", behind, seconds, outrun)
	}

	old := store.Event{SrcIP: "198.51.100.1", ReceivedAt: time.Now().Add(-4 * time.Second)}
	st.Insert(old)
	for i := 0; i < 2; i++ {
		st.Insert(evt("198.51.100.1"))
	}

	behind, seconds, _ := e.Lag()
	if behind != 3 {
		t.Fatalf("behind = %d with three unevaluated events, want 3", behind)
	}
	if seconds < 3 || seconds > 60 {
		t.Fatalf("behindSeconds = %v, want roughly the 4s age of the oldest unevaluated event", seconds)
	}

	e.evaluateBatch(context.Background(), time.Time{})
	behind, seconds, _ = e.Lag()
	if behind != 0 || seconds != 0 {
		t.Fatalf("Lag() = (behind %d, %v s) once caught up, want (0, 0)", behind, seconds)
	}
}

// TestLagReportsAlreadyEvictedEventsAsOutrunBeforeAnyBatchRuns -- outrun
// used to be counted lazily, inside evaluateBatch, so an engine that had
// never run a batch (the state Lag() can be called in at any time, e.g.
// from /api/stats before Run's goroutine has read anything) reported
// everything between its cursor and the store's newest event as "behind"
// -- recoverable -- even the part the ring had already evicted for good.
// Lag() must draw the same line evaluateBatch draws: events at or after
// the oldest survivor are behind, everything older than that, back to the
// cursor, is outrun.
func TestLagReportsAlreadyEvictedEventsAsOutrunBeforeAnyBatchRuns(t *testing.T) {
	e, st := newEngineOnStore(t, 10)

	for i := 0; i < 30; i++ {
		st.Insert(evt("198.51.100.1")) // IDs 21..30 survive, cursor is still 0
	}

	behind, _, outrun := e.Lag()
	if outrun != 20 {
		t.Fatalf("outrun = %d before any batch ran, want the 20 events evicted past the cursor", outrun)
	}
	if behind != 10 {
		t.Fatalf("behind = %d before any batch ran, want the 10 events the store still holds", behind)
	}
}

// TestLagNeverDoubleCountsAConcurrentGap pins the race Lag()'s own doc
// comment promises against: evaluateBatch's gap branch used to add to
// e.outrun and store e.cursor as two separate, unlocked writes, with
// nothing to stop a concurrent Lag() call from loading a cursor that
// hadn't moved yet paired with an outrun that already had -- adding the
// same one-time loss to both instead of counting it once.
//
// The real race window between those two writes is a handful of
// instructions wide, so a plain concurrent-stress version of this test
// would pass on an unfixed engine essentially every run -- proving
// nothing, for the same reason this package's own
// TestMemoryCorpusReplayReportsTruncatedWhenCursorIsEvicted rejected
// timing races (#501, #744). afterOutrunIncrementForTest (engine.go)
// forces the window open deterministically instead: it fires from
// inside evaluateBatch's locked update, between the outrun increment and
// the cursor store, and blocks there until this test releases it, so
// Lag() is given every chance to run while the pair is (or, on the fixed
// engine, would be) only half-updated.
func TestLagNeverDoubleCountsAConcurrentGap(t *testing.T) {
	const capacity = 10
	e, st := newEngineOnStore(t, capacity)

	for i := 0; i < 50; i++ {
		st.Insert(evt("198.51.100.1")) // IDs 41..50 survive, cursor is still 0
	}
	const wantOutrun = 40 // the true one-time loss: IDs 1..40, evicted before the cursor ever reached them

	entered := make(chan struct{})
	release := make(chan struct{})
	e.afterOutrunIncrementForTest = func() {
		close(entered)
		<-release
	}

	batchDone := make(chan struct{})
	go func() {
		defer close(batchDone)
		e.evaluateBatch(context.Background(), time.Time{})
	}()
	<-entered // evaluateBatch is mid-update, parked in the hook

	lagDone := make(chan struct{})
	var outrun uint64
	go func() {
		defer close(lagDone)
		_, _, outrun = e.Lag()
	}()

	// No signal exists for "a goroutine is now blocked trying to take
	// e.mu" -- that is precisely the fixed engine's behaviour under
	// test, not something it can announce -- so this is a deliberate
	// sleep, not a poll, giving Lag() time to reach the lock before the
	// hook (and therefore evaluateBatch's own update) is allowed to
	// finish.
	time.Sleep(50 * time.Millisecond)
	close(release)

	<-batchDone
	<-lagDone

	if outrun != wantOutrun {
		t.Fatalf("Lag() outrun = %d for a concurrent evaluateBatch update, want %d -- the true one-time loss counted once, not the same gap added twice", outrun, wantOutrun)
	}
}

// TestLagIsNilSafe -- /api/stats holds the engine behind a narrow
// interface that is commonly nil (see api.Server.Evaluation), and the
// nil-receiver convention Nudge and Tick follow applies here too.
func TestLagIsNilSafe(t *testing.T) {
	var e *Engine
	if behind, seconds, outrun := e.Lag(); behind != 0 || seconds != 0 || outrun != 0 {
		t.Fatalf("Lag() on a nil engine = (%d, %v, %d), want all zero", behind, seconds, outrun)
	}
	e.Nudge() // must not panic either
}

// ---- lifecycle ----

func TestRunClosesDonePromptlyWhenAlreadyCaughtUp(t *testing.T) {
	withDrainTimeout(t, 2*time.Second) // a large bound the test must NOT have to wait out
	e, _ := newEngineOnStore(t, 100)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	cancel()

	select {
	case <-e.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("Done() did not close promptly for an engine with nothing to evaluate")
	}
}

func TestRunDrainsTheBacklogOnShutdownWithinBound(t *testing.T) {
	withDrainTimeout(t, 500*time.Millisecond)
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	waitForRunning(t, e)

	// Stored but never announced, so the backlog is still there when ctx
	// is cancelled -- drain has to go and look rather than rely on having
	// been told.
	const n = 5
	for i := 0; i < n; i++ {
		st.Insert(evt("198.51.100.1"))
	}
	cancel()

	select {
	case <-e.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within the drain bound")
	}
	if got := d.calls.Load(); got != n {
		t.Fatalf("definition saw %d of %d backlogged events drained on shutdown", got, n)
	}
}

func TestRunStopsWithinDrainTimeoutUnderSustainedBacklog(t *testing.T) {
	const drainBound = 100 * time.Millisecond
	withDrainTimeout(t, drainBound)
	e, st := newEngineOnStore(t, 4096)
	// A definition too slow (5ms/event) to catch up on a 4096-deep
	// backlog (~20s unbounded) within a 100ms bound -- proves ctx
	// cancellation reaches Run's select, and drain's own bound, promptly
	// even while Run is genuinely deep in catchUp, not only once it
	// finishes the whole backlog on its own.
	//
	// storeAndNudge, not a bare Insert loop, is what gets Run there in
	// the first place: a nudge is what main.go's ingest goroutine sends
	// after every store.Insert (see storeAndNudge), and it is what
	// actually routes Run into catchUp rather than leaving it idle on
	// its select until ctx.Done() and straight into drain(), which
	// proves nothing about catchUp at all.
	d := &fakeDef{id: "slow", kind: "declarative", delay: 5 * time.Millisecond}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	waitForRunning(t, e)
	const n = 4096
	storeAndNudge(e, st, n)
	cancel()

	start := time.Now()
	select {
	case <-e.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within a bounded time under sustained backlog")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Run took %s to stop, want roughly drainTimeout (%s)", elapsed, drainBound)
	}
	if got := d.calls.Load(); got >= n {
		t.Fatalf("definition was evaluated %d of %d times, want the bound to have cut the drain short", got, n)
	}
}

// ---- panic isolation ----

func TestEvaluateContainsAPanicWithoutCrashing(t *testing.T) {
	e := New(nil)
	d := &fakeDef{id: "d1", kind: "declarative"}
	d.shouldPanic.Store(true)
	e.Register(d)

	// Reaching the end of this test at all is the proof: an unrecovered
	// panic here would take down the whole `go test` process, not just
	// fail an assertion -- same shape as
	// internal/detect/panic_recovery_test.go's
	// TestObserveRecoveredSwallowsPanic.
	e.evaluateEvent(evt("198.51.100.1"))

	if d.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", d.calls.Load())
	}
	if faults := e.Faults(); len(faults) != 0 {
		t.Fatalf("a single panic must not fault the definition, got %+v", faults)
	}
}

func TestRunSurvivesPanickingDefinitions(t *testing.T) {
	e, st := newEngineOnStore(t, 100)
	d := &fakeDef{id: "d1", kind: "declarative"}
	d.shouldPanic.Store(true)
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	// Stop and join before returning -- an unjoined Run goroutine would
	// outlive this test and could still be inside drain() reading
	// drainTimeout when a later test's withDrainTimeout writes it, which
	// is exactly the cross-test data race this guards against.
	defer func() {
		cancel()
		<-e.Done()
	}()
	waitForRunning(t, e)

	const n = 20
	storeAndNudge(e, st, n)

	// Caught up, not merely alive: the cursor reaching the newest event
	// is what proves the panics did not stall the loop.
	waitFor(t, "the cursor to reach the newest event after a run of panics", func() bool {
		behind, _, _ := e.Lag()
		return behind == 0
	})
}

func TestThreeConsecutivePanicsFaultDefinitionAndSkipIt(t *testing.T) {
	e := New(nil)
	d := &fakeDef{id: "flaky", kind: "programmatic"}
	d.shouldPanic.Store(true)
	e.Register(d)

	for i := 0; i < faultThreshold; i++ {
		e.evaluateEvent(evt("198.51.100.1"))
	}

	faults := e.Faults()
	if len(faults) != 1 {
		t.Fatalf("Faults() = %+v, want exactly one fault", faults)
	}
	f := faults[0]
	if f.DefinitionID != "flaky" || f.Kind != "programmatic" {
		t.Fatalf("fault = %+v, want id=flaky kind=programmatic", f)
	}
	if f.Reason == "" {
		t.Fatal("fault Reason is empty, want an operator-readable explanation")
	}
	if f.At.IsZero() {
		t.Fatal("fault At is zero, want the time the fault was raised")
	}

	// Faulted means skipped: further events must not reach Evaluate.
	callsBeforeMore := d.calls.Load()
	e.evaluateEvent(evt("198.51.100.1"))
	if got := d.calls.Load(); got != callsBeforeMore {
		t.Fatalf("Evaluate was called %d more time(s) after faulting, want 0", got-callsBeforeMore)
	}
}

func TestSuccessfulEvaluationResetsConsecutivePanicCount(t *testing.T) {
	e := New(nil)
	d := &fakeDef{id: "flaky", kind: "programmatic"}
	e.Register(d)

	d.shouldPanic.Store(true)
	e.evaluateEvent(evt("198.51.100.1"))
	e.evaluateEvent(evt("198.51.100.1")) // 2 consecutive panics, one short of faultThreshold

	d.shouldPanic.Store(false)
	e.evaluateEvent(evt("198.51.100.1")) // success -- resets the streak

	d.shouldPanic.Store(true)
	e.evaluateEvent(evt("198.51.100.1"))
	e.evaluateEvent(evt("198.51.100.1")) // 2 more consecutive panics, still short of faultThreshold

	if faults := e.Faults(); len(faults) != 0 {
		t.Fatalf("Faults() = %+v, want none -- the intervening success should have reset the streak", faults)
	}
}

func TestClearFaultReArmsDefinition(t *testing.T) {
	e := New(nil)
	d := &fakeDef{id: "flaky", kind: "programmatic"}
	d.shouldPanic.Store(true)
	e.Register(d)

	for i := 0; i < faultThreshold; i++ {
		e.evaluateEvent(evt("198.51.100.1"))
	}
	if faults := e.Faults(); len(faults) != 1 {
		t.Fatalf("setup: Faults() = %+v, want one fault before ClearFault", faults)
	}

	if ok := e.ClearFault("flaky"); !ok {
		t.Fatal("ClearFault(\"flaky\") = false, want true for a genuinely faulted id")
	}
	if faults := e.Faults(); len(faults) != 0 {
		t.Fatalf("Faults() = %+v, want none after ClearFault", faults)
	}

	// Re-armed: the definition is evaluated again (still panicking, but
	// not yet re-faulted since the streak reset to zero).
	d.shouldPanic.Store(false)
	callsBefore := d.calls.Load()
	e.evaluateEvent(evt("198.51.100.1"))
	if got := d.calls.Load(); got != callsBefore+1 {
		t.Fatalf("calls = %d, want %d -- ClearFault should have re-armed evaluation", got, callsBefore+1)
	}

	if ok := e.ClearFault("no-such-id"); ok {
		t.Fatal("ClearFault on an unknown id returned true, want false")
	}
	if ok := e.ClearFault("flaky"); ok {
		t.Fatal("ClearFault on an already-unfaulted id returned true, want false")
	}
}
