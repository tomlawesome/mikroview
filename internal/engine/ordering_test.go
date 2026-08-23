// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// recordingDef records the order it was evaluated in, into a shared log.
type recordingDef struct {
	id    string
	rank  int
	log   *[]string
	mu    *sync.Mutex
	ticks int
	every time.Duration
}

func (d *recordingDef) ID() string   { return d.id }
func (d *recordingDef) Kind() string { return string(KindProgrammatic) }
func (d *recordingDef) Evaluate(store.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	*d.log = append(*d.log, d.id)
}

type orderedDef struct{ *recordingDef }

func (d orderedDef) EvaluationOrder() int { return d.rank }

type tickingDef struct{ *recordingDef }

func (d tickingDef) Tick(time.Time)              { d.ticks++ }
func (d tickingDef) TickInterval() time.Duration { return d.every }

// TestEvaluationOrderIsDeterministicAndRespectsOrdered is the invariant
// #405's reinforcement definitions depend on, and the reason
// Engine.evaluateEvent iterates a sorted slice rather than a map.
//
// internal/detect got this for free: known_bad_ip's and netclass's
// RaiseConfidenceFloor passes were literally the last two statements of
// Observe, so every flag any other detector raised for that same event
// already existed by the time they ran. Once each detector is a separate
// registration, Go's randomized map iteration would make that ordering
// true only sometimes -- and the symptom would be a confidence floor
// silently not applied on some events, since RaiseConfidenceFloor no-ops
// on a target it does not know about yet. Nothing would error.
func TestEvaluationOrderIsDeterministicAndRespectsOrdered(t *testing.T) {
	var log []string
	var mu sync.Mutex
	eng := New()

	// Registered in an order that is neither the id order nor the rank
	// order, so neither can be satisfied by accident.
	eng.Register(orderedDef{&recordingDef{id: "reinforce-b", rank: ReinforcementOrder, log: &log, mu: &mu}})
	eng.Register(&recordingDef{id: "raiser-z", log: &log, mu: &mu})
	eng.Register(orderedDef{&recordingDef{id: "reinforce-a", rank: ReinforcementOrder, log: &log, mu: &mu}})
	eng.Register(&recordingDef{id: "raiser-a", log: &log, mu: &mu})

	eng.evaluateEvent(store.Event{SrcIP: "203.0.113.9", ReceivedAt: time.Now()})

	want := []string{"raiser-a", "raiser-z", "reinforce-a", "reinforce-b"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("evaluation order = %v, want %v (rank first, then id)", log, want)
	}
}

// TestEvaluationOrderIsStableAcrossManyEvents is the half a single
// assertion cannot prove: Go's map iteration is randomized *per range*,
// so an order that happens to be right once says nothing. This asserts
// the same order every time, over enough events that a map-ordered
// implementation would essentially certainly have differed.
func TestEvaluationOrderIsStableAcrossManyEvents(t *testing.T) {
	var log []string
	var mu sync.Mutex
	eng := New()
	for _, id := range []string{"d", "b", "e", "a", "c"} {
		eng.Register(&recordingDef{id: id, log: &log, mu: &mu})
	}

	const events = 50
	for i := 0; i < events; i++ {
		eng.evaluateEvent(store.Event{SrcIP: "203.0.113.9", ReceivedAt: time.Now()})
	}

	want := []string{"a", "b", "c", "d", "e"}
	for i := 0; i < events; i++ {
		got := log[i*len(want) : (i+1)*len(want)]
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("event %d evaluated in order %v, want %v", i, got, want)
		}
	}
}

// TestRegisterKeepsTheOrderCurrent pins that the sorted order is
// maintained by Register rather than computed per event -- a definition
// registered later still lands in its right place.
func TestRegisterKeepsTheOrderCurrent(t *testing.T) {
	var log []string
	var mu sync.Mutex
	eng := New()
	eng.Register(&recordingDef{id: "c", log: &log, mu: &mu})
	eng.Register(orderedDef{&recordingDef{id: "reinforce", rank: ReinforcementOrder, log: &log, mu: &mu}})
	eng.evaluateEvent(store.Event{SrcIP: "203.0.113.9", ReceivedAt: time.Now()})

	log = nil
	eng.Register(&recordingDef{id: "a", log: &log, mu: &mu})
	eng.evaluateEvent(store.Event{SrcIP: "203.0.113.9", ReceivedAt: time.Now()})

	want := []string{"a", "c", "reinforce"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("evaluation order after a later Register = %v, want %v", log, want)
	}
}

// TestTickHonoursEachDefinitionsOwnInterval pins why Ticked declares its
// cadence rather than inheriting the driver's: internal/detect ran these
// checks on three separate tickers (10s, 1m, an operator-configured
// stale-rule sweep), and folding them onto one would silently retune
// global_spike's EMA, whose baseline advances exactly one sample per
// tick.
func TestTickHonoursEachDefinitionsOwnInterval(t *testing.T) {
	var log []string
	var mu sync.Mutex
	fast := tickingDef{&recordingDef{id: "fast", log: &log, mu: &mu, every: 10 * time.Second}}
	slow := tickingDef{&recordingDef{id: "slow", log: &log, mu: &mu, every: time.Minute}}
	eng := New()
	eng.Register(fast)
	eng.Register(slow)

	base := time.Now()
	// A driver ticking every 10s for 60s: 7 calls at t=0,10,...,60.
	for i := 0; i <= 6; i++ {
		eng.Tick(base.Add(time.Duration(i) * 10 * time.Second))
	}

	// Both are due on the first call (lastTick zero), then fast runs on
	// every subsequent call and slow only once the minute has elapsed.
	if fast.ticks != 7 {
		t.Errorf("fast ticked %d times, want 7 (once per 10s driver call)", fast.ticks)
	}
	if slow.ticks != 2 {
		t.Errorf("slow ticked %d times, want 2 (once at registration, once a minute later)", slow.ticks)
	}
}

// TestTickSkipsFaultedDefinitions pins that the tick path shares the
// evaluate path's fault gate rather than having its own -- a definition
// the engine has stopped evaluating is stopped, not stopped-except-on-a-timer.
func TestTickSkipsFaultedDefinitions(t *testing.T) {
	eng := New()
	d := &panickingTicker{}
	eng.Register(d)

	base := time.Now()
	for i := 0; i < faultThreshold+3; i++ {
		eng.Tick(base.Add(time.Duration(i) * time.Hour))
	}

	if d.calls != faultThreshold {
		t.Errorf("ticked %d times, want %d (stopped once faulted)", d.calls, faultThreshold)
	}
	faults := eng.Faults()
	if len(faults) != 1 || faults[0].DefinitionID != "panicky" {
		t.Fatalf("Faults() = %+v, want one fault for panicky", faults)
	}
}

type panickingTicker struct{ calls int }

func (d *panickingTicker) ID() string                  { return "panicky" }
func (d *panickingTicker) Kind() string                { return string(KindProgrammatic) }
func (d *panickingTicker) Evaluate(store.Event)        {}
func (d *panickingTicker) TickInterval() time.Duration { return time.Minute }
func (d *panickingTicker) Tick(time.Time) {
	d.calls++
	panic("boom")
}

// TestTickOnNilEngineIsANoOp mirrors Enqueue's own nil-receiver
// contract, so a caller that did not construct an engine can still wire
// the ticker unconditionally.
func TestTickOnNilEngineIsANoOp(t *testing.T) {
	var eng *Engine
	eng.Tick(time.Now())
}
