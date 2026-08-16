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

// withQueueSize/withDrainTimeout shrink the package-level tuning vars
// for the duration of a test -- same convention as
// internal/detect.maxTrackedSources: a var rather than a const purely so
// tests can shrink it, restored via t.Cleanup so tests never leak state
// into each other.
func withQueueSize(t *testing.T, n int) {
	t.Helper()
	orig := queueSize
	queueSize = n
	t.Cleanup(func() { queueSize = orig })
}

func withDrainTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := drainTimeout
	drainTimeout = d
	t.Cleanup(func() { drainTimeout = orig })
}

// ---- queue / backpressure ----

func TestEnqueueDropsOnFullQueueAndCounts(t *testing.T) {
	withQueueSize(t, 2)
	e := New()

	// Fill the queue without a consumer running -- Run is never started
	// in this test, so nothing ever drains it.
	e.Enqueue(evt("198.51.100.1"))
	e.Enqueue(evt("198.51.100.2"))
	if got := e.Dropped(); got != 0 {
		t.Fatalf("Dropped() = %d before any overflow, want 0", got)
	}

	const extra = 5
	for i := 0; i < extra; i++ {
		e.Enqueue(evt("198.51.100.3"))
	}
	if got := e.Dropped(); got != extra {
		t.Fatalf("Dropped() = %d, want %d", got, extra)
	}
}

func TestEnqueueNeverBlocksOnFullQueue(t *testing.T) {
	withQueueSize(t, 1)
	e := New()
	e.Enqueue(evt("198.51.100.1")) // fills the size-1 queue

	done := make(chan struct{})
	go func() {
		e.Enqueue(evt("198.51.100.2")) // must drop, not block
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full queue instead of dropping")
	}
	if got := e.Dropped(); got != 1 {
		t.Fatalf("Dropped() = %d, want 1", got)
	}
}

// ---- lifecycle ----

func TestRunDeliversEnqueuedEventsToDefinitions(t *testing.T) {
	e := New()
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	// Stop and join before returning -- an unjoined Run goroutine would
	// outlive this test and could still be inside drain() reading
	// drainTimeout when a later test's withDrainTimeout writes it,
	// which is exactly the cross-test data race this guards against.
	defer func() {
		cancel()
		<-e.Done()
	}()

	const n = 10
	for i := 0; i < n; i++ {
		e.Enqueue(evt("198.51.100.1"))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.calls.Load() == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("definition saw %d of %d events", d.calls.Load(), n)
}

func TestRunClosesDonePromptlyWhenQueueAlreadyEmpty(t *testing.T) {
	withDrainTimeout(t, 2*time.Second) // a large bound the test must NOT have to wait out
	e := New()

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	cancel()

	select {
	case <-e.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("Done() did not close promptly for an already-empty queue")
	}
}

func TestRunDrainsQueuedEventsOnShutdownWithinBound(t *testing.T) {
	withDrainTimeout(t, 500*time.Millisecond)
	e := New()
	d := &fakeDef{id: "d1", kind: "declarative"}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())

	const n = 5
	for i := 0; i < n; i++ {
		e.Enqueue(evt("198.51.100.1"))
	}

	go e.Run(ctx)
	cancel() // cancel immediately -- Run must still drain what's queued

	select {
	case <-e.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within the drain bound")
	}
	if got := d.calls.Load(); got != n {
		t.Fatalf("definition saw %d of %d queued events drained on shutdown", got, n)
	}
}

func TestRunStopsWithinDrainTimeoutUnderSustainedBacklog(t *testing.T) {
	const drainBound = 100 * time.Millisecond
	withDrainTimeout(t, drainBound)
	e := New()
	// A definition too slow (5ms/event) to drain a 4096-deep backlog
	// (~20s unbounded) within a 100ms bound -- proves drain() actually
	// stops instead of running until the queue empties no matter how
	// long that takes.
	d := &fakeDef{id: "slow", kind: "declarative", delay: 5 * time.Millisecond}
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	const n = 4096
	for i := 0; i < n; i++ {
		e.Enqueue(evt("198.51.100.1"))
	}
	go e.Run(ctx)
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
	e := New()
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
	e := New()
	d := &fakeDef{id: "d1", kind: "declarative"}
	d.shouldPanic.Store(true)
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	// See TestRunDeliversEnqueuedEventsToDefinitions for why this joins
	// rather than just cancelling.
	defer func() {
		cancel()
		<-e.Done()
	}()

	const n = 20
	for i := 0; i < n; i++ {
		e.Enqueue(evt("198.51.100.1"))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(e.queue) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Run appears to have stopped consuming after a panic -- %d events still queued", len(e.queue))
}

func TestThreeConsecutivePanicsFaultDefinitionAndSkipIt(t *testing.T) {
	e := New()
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
	e := New()
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
	e := New()
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
