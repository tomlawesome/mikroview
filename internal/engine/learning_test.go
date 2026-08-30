// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// --- learningStateFrom: the shared reduction every LearningReporter
// implementation above (rule_spike, off_hours, low_slow_scan directly;
// global_spike and activity_spike via their own hand-built maps) funnels
// through. Exercised here against synthetic baselineLearning input so
// the four states #639 requires are pinned independently of any one
// definition's own arithmetic.

func TestLearningStateFromNoKeysObserved(t *testing.T) {
	floor := BaselineFloor{MinDuration: 14 * 24 * time.Hour, MinSamples: 14}
	state := learningStateFrom(floor, map[string]baselineLearning{})
	if state.Floor != floor {
		t.Fatalf("Floor = %+v, want %+v (floor is statically known even with zero keys)", state.Floor, floor)
	}
	if state.Keys != 0 || state.Ready != 0 {
		t.Fatalf("Keys/Ready = %d/%d, want 0/0", state.Keys, state.Ready)
	}
	if state.Nearest != nil {
		t.Fatalf("Nearest = %+v, want nil (nothing observed to be furthest along)", state.Nearest)
	}
}

func TestLearningStateFromKeysObservedNoneReady(t *testing.T) {
	floor := BaselineFloor{MinSamples: 14}
	keys := map[string]baselineLearning{
		"a": {ready: false, observedFor: time.Hour, samples: 3},
		"b": {ready: false, observedFor: 2 * time.Hour, samples: 9},
	}
	state := learningStateFrom(floor, keys)
	if state.Keys != 2 || state.Ready != 0 {
		t.Fatalf("Keys/Ready = %d/%d, want 2/0", state.Keys, state.Ready)
	}
	if state.Nearest == nil {
		t.Fatal("Nearest = nil, want the furthest-along not-ready key")
	}
	// "b" (9/14 samples) is further along than "a" (3/14).
	if state.Nearest.Samples != 9 {
		t.Fatalf("Nearest.Samples = %d, want 9 (the furthest-along key, not just any key)", state.Nearest.Samples)
	}
}

func TestLearningStateFromMixed(t *testing.T) {
	floor := BaselineFloor{MinSamples: 14}
	keys := map[string]baselineLearning{
		"ready1": {ready: true, samples: 14},
		"ready2": {ready: true, samples: 20},
		"cold":   {ready: false, observedFor: time.Hour, samples: 1},
	}
	state := learningStateFrom(floor, keys)
	if state.Keys != 3 {
		t.Fatalf("Keys = %d, want 3", state.Keys)
	}
	if state.Ready != 2 {
		t.Fatalf("Ready = %d, want 2 (numbers, not a blended state)", state.Ready)
	}
	if state.Nearest == nil || state.Nearest.Samples != 1 {
		t.Fatalf("Nearest = %+v, want the one not-ready key (samples=1)", state.Nearest)
	}
}

func TestLearningStateFromAllReady(t *testing.T) {
	floor := BaselineFloor{MinSamples: 5}
	keys := map[string]baselineLearning{
		"a": {ready: true, samples: 5},
		"b": {ready: true, samples: 8},
		"c": {ready: true, samples: 50},
	}
	state := learningStateFrom(floor, keys)
	if state.Keys != 3 || state.Ready != 3 {
		t.Fatalf("Keys/Ready = %d/%d, want 3/3", state.Keys, state.Ready)
	}
	if state.Nearest != nil {
		t.Fatalf("Nearest = %+v, want nil -- every observed key is ready", state.Nearest)
	}
}

// TestLearningStateFromNearestIsDeterministicUnderTies pins that a tie in
// floorProgress resolves the same way regardless of Go's randomized map
// iteration order, by running the reduction many times over the same
// input and requiring the same winner every time.
func TestLearningStateFromNearestIsDeterministicUnderTies(t *testing.T) {
	floor := BaselineFloor{MinSamples: 10}
	keys := map[string]baselineLearning{
		"z-key": {ready: false, samples: 5},
		"a-key": {ready: false, samples: 5},
		"m-key": {ready: false, samples: 5},
	}
	first := learningStateFrom(floor, keys)
	if first.Nearest == nil {
		t.Fatal("Nearest = nil, want a tie-broken winner")
	}
	for i := 0; i < 20; i++ {
		got := learningStateFrom(floor, keys)
		if *got.Nearest != *first.Nearest {
			t.Fatalf("run %d: Nearest = %+v, want the same tie-broken %+v every time", i, got.Nearest, first.Nearest)
		}
	}
}

func TestFloorProgressNeitherDimensionSetReturnsOne(t *testing.T) {
	if got := floorProgress(BaselineFloor{}, 0, 0); got != 1 {
		t.Fatalf("floorProgress(zero floor) = %v, want 1", got)
	}
}

func TestFloorProgressIsTheMinimumOfBoundDimensions(t *testing.T) {
	floor := BaselineFloor{MinDuration: 10 * time.Second, MinSamples: 10}
	// Duration is 90% there, samples only 20% there -- the binding
	// constraint (BaselineFloor.cleared's own conjunction) is the smaller.
	got := floorProgress(floor, 9*time.Second, 2)
	if got != 0.2 {
		t.Fatalf("floorProgress = %v, want 0.2 (the smaller of the two ratios)", got)
	}
}

// --- baselineSet.learning: the aggregate read every keyed definition's
// Learning method is built on.

func TestBaselineSetLearningNoKeysObserved(t *testing.T) {
	s := newBaselineSet("def", 0, BaselineFloor{MinSamples: 5}, UpdatePerEvent, nil)
	got := s.learning(time.Now())
	if len(got) != 0 {
		t.Fatalf("learning() = %v, want empty map for a set nothing has ever touched", got)
	}
}

func TestBaselineSetLearningReflectsRealBaselines(t *testing.T) {
	s := newBaselineSet("def", 0, BaselineFloor{MinSamples: 3}, UpdatePerEvent, nil)
	now := time.Now()
	// "ready": three readings clears MinSamples: 3.
	s.reading("ready", now, 1)
	s.reading("ready", now.Add(time.Second), 1)
	s.reading("ready", now.Add(2*time.Second), 1)
	// "cold": one reading, nowhere near the floor.
	s.reading("cold", now, 1)

	got := s.learning(now.Add(3 * time.Second))
	if len(got) != 2 {
		t.Fatalf("learning() has %d keys, want 2", len(got))
	}
	ready, ok := got["ready"]
	if !ok || !ready.ready {
		t.Fatalf("key \"ready\" = %+v, ok=%v, want ready=true", ready, ok)
	}
	if ready.samples != 3 {
		t.Fatalf("key \"ready\" samples = %d, want 3", ready.samples)
	}
	cold, ok := got["cold"]
	if !ok || cold.ready {
		t.Fatalf("key \"cold\" = %+v, ok=%v, want ready=false", cold, ok)
	}
	if cold.samples != 1 {
		t.Fatalf("key \"cold\" samples = %d, want 1", cold.samples)
	}
}

// --- Per-definition LearningReporter wiring: one state-progression test
// per definition, from zero keys through to ready, plus activity_spike's
// merge behaviour, which is the one definition with genuinely bespoke
// logic in this file (its own Learning, not a bare
// learningStateFrom(d.baselines.floor, d.baselines.learning(now))).

func TestShippedRuleSpikeLearning(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})

	state, ok := d.Learning(time.Now())
	if !ok {
		t.Fatal("Learning() ok = false, want true -- rule_spike has a warm-up concept")
	}
	if state.Keys != 0 {
		t.Fatalf("before any traffic, Keys = %d, want 0", state.Keys)
	}
	if state.Floor.MinDuration != 60*time.Second {
		t.Fatalf("Floor.MinDuration = %s, want the definition's own 60s window", state.Floor.MinDuration)
	}

	// Steady traffic well past the window and warmup should clear Ready.
	last := primeRuleSpikeConstantBaseline(d, "D|input-def|", 25, time.Now())
	state, ok = d.Learning(last)
	if !ok {
		t.Fatal("Learning() ok = false after traffic, want true")
	}
	if state.Keys != 1 {
		t.Fatalf("Keys = %d, want 1 (one rule label observed)", state.Keys)
	}
	if state.Ready != 1 {
		t.Fatalf("Ready = %d, want 1 (25 steady readings clears the floor)", state.Ready)
	}
	if state.Nearest != nil {
		t.Fatalf("Nearest = %+v, want nil -- the one observed key is ready", state.Nearest)
	}
}

func TestShippedOffHoursLearning(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, Params{"minSampleDays": 3}, Scope{})

	if state, ok := d.Learning(time.Now()); !ok || state.Keys != 0 {
		t.Fatalf("before any traffic: state=%+v ok=%v, want Keys=0, ok=true", state, ok)
	}

	base := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		at := base.AddDate(0, 0, day)
		d.Evaluate(store.Event{SrcIP: "203.0.113.5", ReceivedAt: at})
	}
	// One more day's worth of events to roll the third day's count into
	// the baseline (off_hours folds the *previous* day on rollover).
	rollover := base.AddDate(0, 0, 3)
	d.Evaluate(store.Event{SrcIP: "203.0.113.5", ReceivedAt: rollover})

	state, ok := d.Learning(rollover)
	if !ok {
		t.Fatal("Learning() ok = false, want true")
	}
	if state.Keys == 0 {
		t.Fatal("Keys = 0, want at least the one (source, hour) key touched above")
	}
	if state.Floor.MinSamples != 3 {
		t.Fatalf("Floor.MinSamples = %d, want 3", state.Floor.MinSamples)
	}
}

func TestShippedLowSlowScanLearning(t *testing.T) {
	sink := func(RoutedEmission) {}
	d := newShippedLowSlowScanDefinition(t, sink, lowSlowTestParams(), Scope{}, true)

	if state, ok := d.Learning(time.Now()); !ok || state.Keys != 0 {
		t.Fatalf("before any traffic: state=%+v ok=%v, want Keys=0, ok=true", state, ok)
	}

	now := time.Now()
	d.Evaluate(store.Event{SrcIP: "203.0.113.7", DstIP: "192.0.2.1", DstPort: 80, ReceivedAt: now, Action: store.ActionAccept})

	state, ok := d.Learning(now)
	if !ok {
		t.Fatal("Learning() ok = false, want true")
	}
	if state.Keys != 1 {
		t.Fatalf("Keys = %d, want 1", state.Keys)
	}
	if state.Ready != 0 {
		t.Fatalf("Ready = %d, want 0 -- minObservation has not elapsed yet", state.Ready)
	}
	if state.Nearest == nil {
		t.Fatal("Nearest = nil, want the one not-yet-ready key")
	}

	// Past minObservation (10m in lowSlowTestParams), the key clears Ready.
	later := now.Add(11 * time.Minute)
	d.Evaluate(store.Event{SrcIP: "203.0.113.7", DstIP: "192.0.2.1", DstPort: 80, ReceivedAt: later, Action: store.ActionAccept})
	state, ok = d.Learning(later)
	if !ok {
		t.Fatal("Learning() ok = false, want true")
	}
	if state.Ready != 1 {
		t.Fatalf("Ready = %d, want 1 once minObservation has elapsed", state.Ready)
	}
}

func TestShippedGlobalSpikeLearningNoBaselineYet(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedGlobalSpikeDefinition(t, fs, &fixedRate{eps: 1}, nil)

	state, ok := d.Learning(time.Now())
	if !ok {
		t.Fatal("Learning() ok = false, want true -- global_spike has a warm-up concept")
	}
	if state.Keys != 0 {
		t.Fatalf("Keys = %d, want 0 before the first Tick (no baseline yet)", state.Keys)
	}
	// The floor is statically known even with zero keys (#639's own
	// fresh-install requirement, restated for the single-baseline case).
	if state.Floor != d.floor {
		t.Fatalf("Floor = %+v, want the definition's own %+v", state.Floor, d.floor)
	}
}

func TestShippedGlobalSpikeLearningAfterTick(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedGlobalSpikeDefinition(t, fs, &fixedRate{eps: 1}, nil)

	now := time.Now()
	d.Tick(now)

	state, ok := d.Learning(now)
	if !ok {
		t.Fatal("Learning() ok = false, want true")
	}
	if state.Keys != 1 {
		t.Fatalf("Keys = %d, want 1 -- one Tick primes the single global baseline", state.Keys)
	}
}

// TestShippedActivitySpikeLearningMergesFallbackAndBuckets is the one
// definition-specific test this issue calls out by name: activity_spike
// tracks two baselineSets, and a source must read as "ready" the moment
// *either* its fallback or one of its hour buckets clears its own floor
// -- see activitySpikeCheck's own useBucket rule, and this file's
// Learning doc comment for why keys/ready are counted per source rather
// than per (source, hour) bucket.
func TestShippedActivitySpikeLearningMergesFallbackAndBuckets(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, Params{}, Scope{})

	if state, ok := d.Learning(time.Now()); !ok || state.Keys != 0 {
		t.Fatalf("before any traffic: state=%+v ok=%v, want Keys=0, ok=true", state, ok)
	}

	// Directly seed one source's bucket as ready (activityBucketMinDays)
	// without ever giving the fallback baseline hostActivityMinSamples
	// (5) readings of its own -- the merge must still report this source
	// ready, because activitySpikeCheck would use the bucket as the
	// applicable baseline the instant it clears.
	now := time.Now()
	d.baselines.reading("203.0.113.9", now, 1) // one fallback reading only
	key := activityBucketKey("203.0.113.9", now.Hour())
	d.buckets.reading(key, now, 10) // one bucket reading, at MinSamples: 1

	state, ok := d.Learning(now)
	if !ok {
		t.Fatal("Learning() ok = false, want true")
	}
	if state.Keys != 1 {
		t.Fatalf("Keys = %d, want 1 -- one source, merged across both sets", state.Keys)
	}
	if state.Ready != 1 {
		t.Fatalf("Ready = %d, want 1 -- the bucket cleared its own floor even though the fallback has not", state.Ready)
	}
	if state.Nearest != nil {
		t.Fatalf("Nearest = %+v, want nil -- the merged source is ready", state.Nearest)
	}
}

// --- The engine accessor itself.

func TestEngineLearningUnknownDefinition(t *testing.T) {
	e := New()
	if _, ok := e.Learning("no-such-id", time.Now()); ok {
		t.Fatal("Learning() ok = true for an unregistered id, want false")
	}
}

func TestEngineLearningNilEngine(t *testing.T) {
	var e *Engine
	if _, ok := e.Learning("anything", time.Now()); ok {
		t.Fatal("Learning() ok = true on a nil *Engine, want false")
	}
}

// TestEngineLearningNoWarmupConcept covers a definition that implements
// Evaluated but not LearningReporter -- the "definition with no warm-up
// concept" state #639 requires the API to omit entirely. globalSpike's
// own sibling detectors (known_bad_ip, netclass, ...) are declarative,
// so a minimal hand-rolled Evaluated stands in here rather than building
// a whole declarative definition just to prove the type assertion fails.
type noLearningDefinition struct{ id string }

func (n *noLearningDefinition) ID() string           { return n.id }
func (n *noLearningDefinition) Kind() string         { return "programmatic" }
func (n *noLearningDefinition) Evaluate(store.Event) {}

func TestEngineLearningNoWarmupConcept(t *testing.T) {
	e := New()
	e.Register(&noLearningDefinition{id: "no-warmup"})
	if _, ok := e.Learning("no-warmup", time.Now()); ok {
		t.Fatal("Learning() ok = true for a definition with no warm-up concept, want false")
	}
}

// TestEngineLearningRaceSafeAgainstConcurrentBaselineUpdates is the -race
// proof issue #639 requires for the live accessor: Engine.Learning reads
// a registered rule_spike definition's baseline while the evaluation
// goroutine concurrently folds new readings into it via Evaluate. Must
// never trip the race detector. Run with `go test -race`.
func TestEngineLearningRaceSafeAgainstConcurrentBaselineUpdates(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	e := New()
	e.Register(d)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			label := fmt.Sprintf("rule-%d", i%4)
			d.Evaluate(ruleEvt(label, start.Add(time.Duration(i)*time.Second)))
			i++
		}
	}()

	for i := 0; i < 500; i++ {
		if _, ok := e.Learning("rule_spike", time.Now()); !ok {
			t.Fatal("Learning() ok = false for a registered definition, want true")
		}
	}
	close(stop)
	wg.Wait()
}
