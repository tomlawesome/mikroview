// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

func mustOpenMatchLog(t *testing.T, capacity int) *matchlog.FileStore {
	t.Helper()
	ml, err := matchlog.Open(filepath.Join(t.TempDir(), "matchlog.jsonl"), capacity)
	if err != nil {
		t.Fatalf("matchlog.Open: %v", err)
	}
	t.Cleanup(func() { ml.Close() })
	return ml
}

func TestNilEvaluatorEnqueueIsSafeNoop(t *testing.T) {
	var ev *Evaluator
	ev.Enqueue(baseEvent()) // must not panic
}

func TestEvaluatorWithNilMatchLogEnqueueIsNoop(t *testing.T) {
	entries := mustOpenStore(t)
	ev := NewEvaluator(entries, nil)
	ev.Enqueue(baseEvent()) // must not panic, and must not block or queue anything
	if len(ev.queue) != 0 {
		t.Errorf("queue has %d items, want 0 -- a nil matchLog means Enqueue should be a pure no-op", len(ev.queue))
	}
}

// The core end-to-end path: Enqueue -> Run -> Match -> matchLog.Append,
// entirely off the caller's goroutine.
func TestEvaluatorRecordsAMatchEndToEnd(t *testing.T) {
	entries := mustOpenStore(t)
	if err := entries.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	ml := mustOpenMatchLog(t, 10)
	ev := NewEvaluator(entries, ml)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ev.Run(ctx)

	ev.Enqueue(baseEvent())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ml.Stats().Count == 1 {
			var got []matchlog.Record
			_ = ml.Query(context.Background(), matchlog.Query{Source: matchlog.Identity{MAC: baseEvent().SrcMAC}}, func(r matchlog.Record) bool {
				got = append(got, r)
				return true
			})
			if len(got) != 1 || got[0].EntryID != "e1" {
				t.Fatalf("recorded match is wrong: %+v", got)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no match was recorded within the deadline")
}

// The Observed path end to end: an inverted entry still observing must
// record a candidate via Store.RecordObservation, not a matchlog record
// -- confirms the Evaluator actually wires Match's Observed outcome to
// the entry store, not just that Match decides Observed in isolation
// (already covered in match_test.go) or that RecordObservation itself
// works in isolation (invert_test.go).
func TestEvaluatorRecordsAnObservationEndToEnd(t *testing.T) {
	entries := mustOpenStore(t)
	if err := entries.Upsert(Entry{
		ID: "e1", Invert: true, Observing: true,
		Source: matchlog.Identity{MAC: baseEvent().SrcMAC},
	}); err != nil {
		t.Fatal(err)
	}
	ml := mustOpenMatchLog(t, 10)
	ev := NewEvaluator(entries, ml)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ev.Run(ctx)

	ev.Enqueue(baseEvent())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e, _ := entries.Get("e1")
		if len(e.Observed) == 1 {
			if e.Observed[0].DestIP != baseEvent().DstIP || e.Observed[0].Port != baseEvent().DstPort {
				t.Fatalf("wrong observation recorded: %+v", e.Observed[0])
			}
			if got := ml.Stats().Count; got != 0 {
				t.Errorf("matchlog Stats().Count = %d, want 0 -- observing must not write a matchlog record", got)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no observation was recorded within the deadline")
}

// A non-matching event must not produce a record -- confirms Run
// actually calls Match rather than recording everything it sees.
func TestEvaluatorDoesNotRecordANonMatch(t *testing.T) {
	entries := mustOpenStore(t)
	if err := entries.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	ml := mustOpenMatchLog(t, 10)
	ev := NewEvaluator(entries, ml)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ev.Run(ctx)

	e := baseEvent()
	e.DstPort = 443 // not in entry's Ports
	ev.Enqueue(e)

	// No positive signal for "nothing happened" -- wait out a window
	// long enough that a wrongly-recorded match would have landed, then
	// assert the store is still empty.
	time.Sleep(100 * time.Millisecond)
	if got := ml.Stats().Count; got != 0 {
		t.Errorf("Stats().Count = %d, want 0 for a non-matching event", got)
	}
}

// Enqueue must never block the caller -- a full queue drops rather than
// waiting, the entire reason this type exists (see its doc comment on
// issue #221). Verified by filling the queue with a Run that is not yet
// draining it, then confirming a further Enqueue returns immediately.
func TestEnqueueDropsRatherThanBlocksWhenFull(t *testing.T) {
	entries := mustOpenStore(t)
	ml := mustOpenMatchLog(t, evalQueueSize+10)
	ev := NewEvaluator(entries, ml)
	// Deliberately never call Run -- the queue fills and stays full.

	for i := 0; i < evalQueueSize; i++ {
		ev.Enqueue(baseEvent())
	}
	if got := ev.dropped.Load(); got != 0 {
		t.Fatalf("dropped = %d before the queue was even full, want 0", got)
	}

	done := make(chan struct{})
	go func() {
		ev.Enqueue(baseEvent()) // the queue is now full; this must not block
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full queue instead of dropping")
	}
	if got := ev.dropped.Load(); got != 1 {
		t.Errorf("dropped = %d, want 1", got)
	}
}

// panicOnAppendMatchLog is a minimal matchlog.Store whose Append always
// panics, so evaluateRecovered's recover() can be verified to actually
// protect Run rather than just being present and untested.
type panicOnAppendMatchLog struct{}

func (panicOnAppendMatchLog) Append(string, matchlog.Tuple, store.Event, time.Time) error {
	panic("simulated panic from Append")
}
func (panicOnAppendMatchLog) Query(context.Context, matchlog.Query, func(matchlog.Record) bool) error {
	return nil
}
func (panicOnAppendMatchLog) Stats() matchlog.Stats { return matchlog.Stats{} }
func (panicOnAppendMatchLog) Close() error          { return nil }

// A panic evaluating one event must not kill the Run goroutine for every
// event after it -- the same reasoning ingestOneRecovered's own
// panic-recovery has, and the same failure mode a missing recover() here
// would silently reproduce (Run's for-select loop simply stops, and
// every event enqueued afterwards is dropped with no further log line,
// since even recordDropped never runs once Run itself is dead).
func TestEvaluatorSurvivesAPanicDuringEvaluation(t *testing.T) {
	entries := mustOpenStore(t)
	if err := entries.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(entries, panicOnAppendMatchLog{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ev.Run(ctx)

	ev.Enqueue(baseEvent()) // this evaluation panics inside Append

	// Prove Run is still alive: enqueue a second event and confirm it
	// still gets *attempted* (still panics, since every Append does) --
	// there is no clean success signal available with this fake, so
	// this poll just needs the goroutine to still be consuming from the
	// queue rather than having exited. A generous, bounded wait; if Run
	// died, the queue fills and TestEnqueueDropsRatherThanBlocksWhenFull
	// above already proves Enqueue would still not hang here either way.
	deadline := time.Now().Add(500 * time.Millisecond)
	for i := 0; i < 100 && time.Now().Before(deadline); i++ {
		ev.Enqueue(baseEvent())
		time.Sleep(time.Millisecond)
	}
	if got := len(ev.queue); got == evalQueueSize {
		t.Error("the queue filled up, suggesting Run stopped draining it after the panic")
	}
}

// BenchmarkEvaluateRecovered pins evaluateRecovered's per-event cost
// against entry count, direct rather than through Enqueue/Run so the
// number reflects the evaluation itself rather than channel scheduling.
// Entries deliberately never match the benchmarked event (port 2222 vs
// the event's port 22) -- this measures the common "no entry matched"
// case entriesSnapshot's own doc comment cites, not matchLog.Append's
// fsync cost.
func BenchmarkEvaluateRecovered(b *testing.B) {
	for _, n := range []int{10, 100, 500, 1000, 2000, 5000} {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			entries, err := Open(filepath.Join(b.TempDir(), "watchlist.json"))
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
			for i := 0; i < n; i++ {
				if err := entries.Upsert(Entry{ID: fmt.Sprintf("e%d", i), Ports: []int{2222}}); err != nil {
					b.Fatalf("Upsert: %v", err)
				}
			}
			ml, err := matchlog.Open(filepath.Join(b.TempDir(), "matchlog.jsonl"), 10)
			if err != nil {
				b.Fatalf("matchlog.Open: %v", err)
			}
			b.Cleanup(func() { ml.Close() })

			ev := NewEvaluator(entries, ml)
			e := baseEvent()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ev.evaluateRecovered(e)
			}
		})
	}
}
