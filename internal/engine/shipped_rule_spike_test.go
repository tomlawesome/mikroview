// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// newShippedRuleSpikeDefinition builds rule_spike at DefaultConfig's real
// values (multiplier 5, minRate 0.2/s, window 60s, warmup 20 -- see
// internal/detect.DefaultConfig), wired to raise into fs the way main.go
// wires every detection-intent definition.
func newShippedRuleSpikeDefinition(t *testing.T, fs *flags.Store, deps ShippedDeps, scope Scope) *ruleSpikeDefinition {
	t.Helper()
	def := Definition{
		ID:      "rule_spike",
		Name:    "Rule spike",
		Intent:  IntentDetection,
		Kind:    KindProgrammatic,
		Enabled: true,
		Scope:   scope,
		Params: Params{
			"multiplier":            5.0,
			"minRate":               0.2,
			"window":                (60 * time.Second).String(),
			"warmupSamples":         20,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": time.Duration(0).String(),
		},
		ParamSchema: RuleSpikeParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, deps)
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(rule_spike): %v", err)
	}
	d := built.(*ruleSpikeDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func ruleEvt(rule string, at time.Time) store.Event {
	return store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", RuleLabel: rule, ReceivedAt: at}
}

func rsFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeRuleSpike {
			return &f
		}
	}
	return nil
}

// primeRuleSpikeConstantBaseline is
// internal/detect/characterization_test.go's helper of the same name,
// moved: n ticks of exactly one event at a fixed 65-second cadence (>
// the 60s window, so each tick reads a constant 1/60 events/sec),
// collapsing the EMA's variance to exactly zero. Returns the time of the
// last warm-up tick.
func primeRuleSpikeConstantBaseline(d *ruleSpikeDefinition, rule string, n int, from time.Time) time.Time {
	tick := from
	for i := 0; i < n; i++ {
		d.Evaluate(ruleEvt(rule, tick))
		tick = tick.Add(65 * time.Second)
	}
	return tick.Add(-65 * time.Second)
}

// TestShippedRuleSpikeNoFalseSpikeInsideTheFirstWindow is #368's own
// scenario, and the test that closes it. This is the LAST of #405's three
// sanctioned characterization diffs: internal/detect raised a flag here
// and the engine does not.
//
// #368, verbatim: "With DefaultConfig() (RuleSpikeWindow 60s, multiplier
// 5, MinRate 0.2/s, warmup 20), feed a perfectly steady 5 events/second
// under a single rule label D|input-def|. At event 12 -- 2.2 seconds in
// -- a rule_spike flag is raised: detail='0.2 hits/s vs a baseline of 0.0
// for this rule (based on 11 samples, 4.0σ above normal)', confidence
// 27%. Traffic never changed."
//
// It never changes here either, and nothing fires, because
// Baseline.Reading refuses to prime inside the first window: the reading
// that used to become a baseline of 0.0167/s is now discarded for
// priming purposes entirely, so there is no near-zero baseline for the
// window's own fill to read as 5x.
func TestShippedRuleSpikeNoFalseSpikeInsideTheFirstWindow(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	rule := "D|input-def|"

	// A perfectly steady 5 events/second for the whole first window.
	t0 := time.Now()
	for i := 0; i < 5*60; i++ {
		d.Evaluate(ruleEvt(rule, t0.Add(time.Duration(i)*200*time.Millisecond)))
	}

	if got := rsFlagOfType(fs); got != nil {
		t.Fatalf("steady traffic inside the first window raised a spike -- #368's exact scenario: %+v", got)
	}
}

// TestShippedRuleSpikeNoFalseSpikeOnRestart is #368's other half: the
// defect recurred on every restart, because ruleWindows was in-memory
// only and NewWithSettings rebuilt it cold. Both restart shapes are
// pinned, because which one a deployment gets depends on whether it has
// engine-state persistence configured:
//
//   - with a StateStore, the baseline resumes warm from the persisted
//     value, so the rule is judged against its real history immediately
//     rather than spending another window blind;
//   - without one, the baseline starts cold and the history floor
//     guarantees warm-up silence instead.
//
// Neither fires. That is the point: the old behaviour fired in both.
func TestShippedRuleSpikeNoFalseSpikeOnRestart(t *testing.T) {
	rule := "D|input-def|"
	steady := func(d *ruleSpikeDefinition, from time.Time, seconds int) time.Time {
		for i := 0; i < 5*seconds; i++ {
			d.Evaluate(ruleEvt(rule, from.Add(time.Duration(i)*200*time.Millisecond)))
		}
		return from.Add(time.Duration(seconds) * time.Second)
	}

	t.Run("with persisted state, the baseline resumes warm", func(t *testing.T) {
		state, err := OpenStateStoreWithBackend(nil)
		if err != nil {
			t.Fatal(err)
		}
		deps := ShippedDeps{State: state}

		fs1 := newTestFlagsStore(t)
		first := newShippedRuleSpikeDefinition(t, fs1, deps, Scope{})
		t0 := time.Now()
		next := steady(first, t0, 180) // three windows of steady traffic
		if got := rsFlagOfType(fs1); got != nil {
			t.Fatalf("steady traffic raised a spike before the restart: %+v", got)
		}
		if _, ok := state.Get("rule_spike", rule); !ok {
			t.Fatal("expected the warm baseline to have reached the state store")
		}

		// Restart: a brand-new definition against the same store.
		fs2 := newTestFlagsStore(t)
		second := newShippedRuleSpikeDefinition(t, fs2, deps, Scope{})
		steady(second, next, 120)
		if got := rsFlagOfType(fs2); got != nil {
			t.Fatalf("steady traffic raised a spike after a restart -- #368's recurrence: %+v", got)
		}
	})

	t.Run("without persisted state, the floor gives warm-up silence", func(t *testing.T) {
		fs1 := newTestFlagsStore(t)
		first := newShippedRuleSpikeDefinition(t, fs1, ShippedDeps{}, Scope{})
		t0 := time.Now()
		next := steady(first, t0, 180)

		fs2 := newTestFlagsStore(t)
		second := newShippedRuleSpikeDefinition(t, fs2, ShippedDeps{}, Scope{})
		steady(second, next, 120)
		if got := rsFlagOfType(fs2); got != nil {
			t.Fatalf("steady traffic raised a spike after a cold restart: %+v", got)
		}
	})
}

// TestShippedRuleSpikeFlagsWellAboveOwnBaseline is
// internal/detect/rule_spike_test.go's test of the same name: a genuine,
// large departure from a rule's own established rate still fires. #368's
// fix must not have bought its silence by making the detector inert --
// that is the failure mode a history floor is most likely to introduce,
// so it is pinned right next to the two tests that assert the silence.
func TestShippedRuleSpikeFlagsWellAboveOwnBaseline(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	rule := "wan-in"

	// Establish a real, quiet baseline over many windows.
	last := primeRuleSpikeConstantBaseline(d, rule, 25, time.Now())

	// Then a burst far above it, starting a clear window later.
	burst := last.Add(65 * time.Second)
	for i := 0; i < 60; i++ {
		d.Evaluate(ruleEvt(rule, burst.Add(time.Duration(i)*time.Second)))
	}

	f := rsFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a genuine spike well above the rule's own baseline to fire")
	}
	if f.Target != rule {
		t.Errorf("Target = %q, want %q", f.Target, rule)
	}
	if !strings.Contains(f.Detail, "for this rule (based on") || !strings.HasSuffix(f.Detail, "σ above normal)") {
		t.Errorf("Detail = %q, want internal/detect's exact sentence shape", f.Detail)
	}
	if !strings.Contains(f.Detail, " hits/s vs a baseline of ") {
		t.Errorf("Detail = %q, missing the rate-vs-baseline clause", f.Detail)
	}
	if f.Confidence == nil || *f.Confidence <= 0 || *f.Confidence > 100 {
		t.Errorf("Confidence = %v, want a value in (0, 100]", f.Confidence)
	}
	// internal/detect raised this through AddWithConfidence, which
	// carries no country and no evidence -- an emission about a rule
	// label has neither.
	if f.Country != "" {
		t.Errorf("Country = %q, want empty (the target is a rule label, not a source)", f.Country)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 {
		t.Errorf("Evidence = %+v, want empty", f.Evidence)
	}
}

// TestShippedRuleSpikeIgnoresLowAbsoluteRate is
// internal/detect/rule_spike_test.go's test of the same name: minRate is
// an absolute floor on top of the multiplier, so a rule going from
// almost-never to still-almost-never does not fire however large the
// ratio.
func TestShippedRuleSpikeIgnoresLowAbsoluteRate(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	rule := "quiet"

	last := primeRuleSpikeConstantBaseline(d, rule, 25, time.Now())
	// Six hits in the window: 0.1/s, well under minRate 0.2, but many
	// times the ~0.0167/s baseline.
	burst := last.Add(65 * time.Second)
	for i := 0; i < 6; i++ {
		d.Evaluate(ruleEvt(rule, burst.Add(time.Duration(i)*time.Second)))
	}
	if got := rsFlagOfType(fs); got != nil {
		t.Fatalf("expected a rate under minRate never to fire however large the multiple, got %+v", got)
	}
}

// TestShippedRuleSpikeTracksEachRuleIndependently is
// internal/detect/rule_spike_test.go's test of the same name.
func TestShippedRuleSpikeTracksEachRuleIndependently(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})

	t0 := time.Now()
	quietLast := primeRuleSpikeConstantBaseline(d, "quiet", 25, t0)
	primeRuleSpikeConstantBaseline(d, "busy", 25, t0)

	// Only "busy" bursts.
	burst := quietLast.Add(65 * time.Second)
	for i := 0; i < 60; i++ {
		d.Evaluate(ruleEvt("busy", burst.Add(time.Duration(i)*time.Second)))
	}

	f := rsFlagOfType(fs)
	if f == nil {
		t.Fatal("expected the bursting rule to fire")
	}
	if f.Target != "busy" {
		t.Errorf("Target = %q, want busy -- one rule's burst must not be attributed to another", f.Target)
	}
	if len(fs.List()) != 1 {
		t.Errorf("expected exactly one flag, got %+v", fs.List())
	}
}

// TestShippedRuleSpikeRespectsRulesDenylist is
// internal/detect/rule_spike_test.go's TestRuleSpikeRespectsRulesDenylist,
// moved: Scope's rule axis, which is the axis this definition keys on.
func TestShippedRuleSpikeRespectsRulesDenylist(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{},
		Scope{Rules: []string{"noisy"}, RulesMode: ListModeDeny})

	last := primeRuleSpikeConstantBaseline(d, "noisy", 25, time.Now())
	burst := last.Add(65 * time.Second)
	for i := 0; i < 60; i++ {
		d.Evaluate(ruleEvt("noisy", burst.Add(time.Duration(i)*time.Second)))
	}
	if got := rsFlagOfType(fs); got != nil {
		t.Fatalf("expected a denylisted rule never to fire, got %+v", got)
	}
}

// TestShippedRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives is
// internal/detect/rule_spike_test.go's test of the same name -- #267
// finding 17's proposal, measured and rejected. A disabled definition
// touches nothing, and re-enabling it must not re-prime against a
// partially-refilled ring, because the refill back to normal traffic
// would then read as a spike. Under #368's floor the same guarantee
// holds for a different reason as well, which is worth having both of.
func TestShippedRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	rule := "wan-in"

	last := primeRuleSpikeConstantBaseline(d, rule, 25, time.Now())

	// Off for a while, during which traffic continues.
	d.def.Enabled = false
	off := last.Add(65 * time.Second)
	for i := 0; i < 120; i++ {
		d.Evaluate(ruleEvt(rule, off.Add(time.Duration(i)*time.Second)))
	}

	// Back on, at the same ordinary rate it was always running at.
	d.def.Enabled = true
	on := off.Add(130 * time.Second)
	for i := 0; i < 25; i++ {
		d.Evaluate(ruleEvt(rule, on.Add(time.Duration(i)*65*time.Second)))
	}

	if got := rsFlagOfType(fs); got != nil {
		t.Fatalf("expected no false spike from ordinary traffic after a disable/enable cycle, got %+v", got)
	}
}

// TestShippedRuleSpikeIsReplayable pins the classification and the
// two-window Decline that follows from a baseline that cannot be
// borrowed: a corpus shorter than two windows contains no judgement this
// definition would have made, so it declines rather than reporting zero.
func TestShippedRuleSpikeIsReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})

	receiptCapable, reason, ok := Replayability(d)
	if !ok || !receiptCapable {
		t.Fatalf("Replayability = (%v, %q, %v), want a replayable classification", receiptCapable, reason, ok)
	}

	t0 := time.Now()
	short := make([]store.Event, 0, 30)
	for i := 0; i < 30; i++ {
		short = append(short, ruleEvt("wan-in", t0.Add(time.Duration(i)*time.Second)))
	}
	res, err := d.Replay(fakeCorpus{events: short}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Decline == nil {
		t.Fatalf("expected a Decline on a corpus shorter than two windows, got %+v", res)
	}
	if !strings.Contains(res.Decline.Reason, "prime a baseline") {
		t.Errorf("Decline reason does not say why: %q", res.Decline.Reason)
	}
}

// TestShippedRuleSpike_RefireClearRevive carries forward the half of
// internal/detect's TestCharacterizationRuleSpike_FieldsRefireClearRevive
// that #368 does NOT change: what happens on a second crossing of the
// same episode.
//
// #368 moves the firing boundary (that is the sanctioned diff), so the
// old test's exact hit counts and %.1f-formatted EMA fields cannot carry
// over -- they were captured from a run of the code this port replaces.
// The lifecycle around the boundary is untouched by #368 and is pinned
// here so it is not silently lost with the numbers: a second crossing
// updates the same flag in place rather than raising another, Clear
// works, and a later crossing revives the same (Type, Target) record
// with Count reset and FirstSeen moved, which is flags.Store's
// dedupe-by-(Type,Target) lifecycle rather than anything rule_spike does.
func TestShippedRuleSpike_RefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	rule := "wan-in"

	last := primeRuleSpikeConstantBaseline(d, rule, 25, time.Now())
	burst := last.Add(65 * time.Second)

	// Walk the burst one event at a time so the exact first crossing is
	// observed rather than assumed -- the boundary itself is #368's to
	// move, so this test refuses to hard-code it.
	var first *flags.Flag
	var firstAt time.Time
	for i := 0; i < 60 && first == nil; i++ {
		at := burst.Add(time.Duration(i) * time.Second)
		d.Evaluate(ruleEvt(rule, at))
		if f := rsFlagOfType(fs); f != nil {
			first, firstAt = f, at
		}
	}
	if first == nil {
		t.Fatal("expected the burst to cross at some point")
	}
	if first.Count != 1 {
		t.Errorf("Count on the first crossing = %d, want 1", first.Count)
	}

	// Re-fire: the next event in the same burst updates the same flag in
	// place rather than creating a second one.
	d.Evaluate(ruleEvt(rule, firstAt.Add(time.Second)))
	f2 := rsFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.ID != first.ID {
		t.Errorf("re-firing created a second flag (%s -> %s), want the same one updated in place", first.ID, f2.ID)
	}
	if len(fs.List()) != 1 {
		t.Errorf("expected exactly one flag after a re-fire, got %+v", fs.List())
	}

	// Clear, then keep the burst going: the same record revives rather
	// than a new one appearing beside it.
	clearAt := firstAt.Add(2 * time.Second)
	if _, ok := fs.SetVerdict(f2.ID, flags.VerdictChecked, "operator", clearAt); !ok {
		t.Fatal("expected Clear to succeed on the active flag")
	}
	reviveAt := clearAt.Add(time.Second)
	d.Evaluate(ruleEvt(rule, reviveAt))
	f3 := rsFlagOfType(fs)
	if f3 == nil {
		t.Fatal("expected the flag to revive")
	}
	if f3.Cleared {
		t.Error("expected the revived flag to no longer be Cleared")
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1 (revival resets Count)", f3.Count)
	}
	if !f3.FirstSeen.Equal(reviveAt) {
		t.Errorf("FirstSeen after revival = %v, want %v (revival resets FirstSeen)", f3.FirstSeen, reviveAt)
	}
	if len(fs.List()) != 1 {
		t.Errorf("expected the revival to reuse the one record, got %+v", fs.List())
	}
}
