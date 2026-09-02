// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fixedRate is an EventRateSource a test sets directly, standing in for
// store.Store.EventsPerSecond -- the same "inject the reading" shape
// internal/detect's GlobalSpikeDetector.Check(eps, now) had, which is
// what made its own tests hand-derivable.
type fixedRate struct{ eps float64 }

func (r *fixedRate) EventsPerSecond() float64 { return r.eps }

func newShippedGlobalSpikeDefinition(t *testing.T, fs *flags.Store, rate EventRateSource, state *StateStore) *globalSpikeDefinition {
	t.Helper()
	def := Definition{
		ID:      "global_spike",
		Name:    "Global spike",
		Intent:  IntentDetection,
		Kind:    KindProgrammatic,
		Enabled: true,
		Params: Params{
			"multiplier":            4.0,
			"minEPS":                5.0,
			"warmupSamples":         20,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": time.Duration(0).String(),
		},
		ParamSchema: GlobalSpikeParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{Rate: rate, State: state})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(global_spike): %v", err)
	}
	d := built.(*globalSpikeDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func gsFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeGlobalSpike {
			return &f
		}
	}
	return nil
}

// warmGlobalSpike is internal/detect/characterization_test.go's helper of
// the same name: n readings of exactly 1 eps, one second apart, which
// collapses the EMA's variance to exactly zero so the boundary that
// follows has a fully hand-derivable confidence.
func warmGlobalSpike(d *globalSpikeDefinition, rate *fixedRate, n int, from time.Time) {
	rate.eps = 1
	for i := 0; i < n; i++ {
		d.Tick(from.Add(time.Duration(i) * time.Second))
	}
}

// TestShippedGlobalSpike_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationGlobalSpike_FieldsRefireClearRevive, moved. Every
// pinned value -- the boundary, the byte-for-byte Detail string, the
// confidence, the empty Evidence and Country -- is unchanged.
//
// The below-MinEPS probe keeps its own separately-warmed definition, for
// the reason the original states: a reading is folded into the baseline
// unconditionally afterwards, so probing eps=4 on the instance that goes
// on to pin the eps=5 boundary would perturb the zero-variance baseline
// that pin depends on.
func TestShippedGlobalSpike_FieldsRefireClearRevive(t *testing.T) {
	now := time.Now()

	probeFS := newTestFlagsStore(t)
	probeRate := &fixedRate{}
	probe := newShippedGlobalSpikeDefinition(t, probeFS, probeRate, nil)
	warmGlobalSpike(probe, probeRate, 25, now)
	probeRate.eps = 4 // below minEPS(5)
	probe.Tick(now.Add(25 * time.Second))
	if got := gsFlagOfType(probeFS); got != nil {
		t.Fatalf("expected no flag at eps=4 (below minEPS=5), got %+v", got)
	}

	fs := newTestFlagsStore(t)
	rate := &fixedRate{}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)
	warmGlobalSpike(d, rate, 25, now)
	rate.eps = 5 // minEPS(5) and 5x baseline(1) both clear
	d.Tick(now.Add(25 * time.Second))

	f := gsFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag at eps=5")
	}
	if f.Target != "global" {
		t.Errorf("Target = %q, want %q", f.Target, "global")
	}
	if want := "5.0 events/s vs a baseline of 1.0 (based on 20 samples, 6.0σ above normal)"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 100 {
		t.Errorf("Confidence = %v, want 100 (zero-variance warm-up)", f.Confidence)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 || f.Evidence.NAT != nil {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}
	if f.Country != "" {
		t.Errorf("Country = %q, want empty (global_spike has no per-event source to attribute a country to)", f.Country)
	}

	// Re-fire.
	d.Tick(now.Add(27 * time.Second))
	f2 := gsFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}

	// Clear + revive.
	if _, ok := fs.SetVerdict(f2.ID, flags.VerdictChecked, "operator", now.Add(28*time.Second)); !ok {
		t.Fatal("expected Clear to succeed")
	}
	d.Tick(now.Add(29 * time.Second))
	f3 := gsFlagOfType(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// TestShippedGlobalSpikeFirstReadingOnlyPrimes is
// internal/detect/global_spike_test.go's
// TestGlobalSpikeFirstCallOnlyPrimesBaseline: there is nothing to
// compare the very first reading against, so however large it is, it
// only primes.
//
// Note this is deliberately NOT #368's priming gate: this definition's
// primeWindow is zero, because its reading is an accurate instantaneous
// rate rather than a still-filling window's, so priming on the first
// reading is correct here and was wrong for rule_spike. See
// baselineSet.primeWindow.
func TestShippedGlobalSpikeFirstReadingOnlyPrimes(t *testing.T) {
	fs := newTestFlagsStore(t)
	rate := &fixedRate{eps: 5000}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)

	d.Tick(time.Now())
	if got := gsFlagOfType(fs); got != nil {
		t.Fatalf("expected the first reading only to prime, got %+v", got)
	}
}

// TestShippedGlobalSpikeIgnoresLowAbsoluteVolume is
// internal/detect/global_spike_test.go's test of the same name: minEPS
// is an absolute floor on top of the multiplier, so a quiet network
// going from very quiet to slightly less quiet does not fire.
func TestShippedGlobalSpikeIgnoresLowAbsoluteVolume(t *testing.T) {
	fs := newTestFlagsStore(t)
	rate := &fixedRate{}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)
	now := time.Now()

	rate.eps = 0.2
	for i := 0; i < 25; i++ {
		d.Tick(now.Add(time.Duration(i) * time.Second))
	}
	rate.eps = 4 // 20x the baseline, but under minEPS
	d.Tick(now.Add(25 * time.Second))

	if got := gsFlagOfType(fs); got != nil {
		t.Fatalf("expected a rate under minEPS never to fire however large the multiple, got %+v", got)
	}
}

// TestShippedGlobalSpikeBaselineAdapts is
// internal/detect/global_spike_test.go's
// TestGlobalSpikeBaselineAdaptsSoRepeatedNormalTrafficStopsFlagging: a
// sustained new normal is learned rather than flagged forever, which is
// the entire reason this is an EMA and not a fixed threshold.
func TestShippedGlobalSpikeBaselineAdapts(t *testing.T) {
	fs := newTestFlagsStore(t)
	rate := &fixedRate{}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)
	now := time.Now()

	warmGlobalSpike(d, rate, 25, now)
	rate.eps = 50
	// The first crossing fires; a long sustained plateau at the same
	// level must eventually stop being a spike.
	for i := 0; i < 400; i++ {
		d.Tick(now.Add(time.Duration(25+i) * time.Second))
	}
	f := gsFlagOfType(fs)
	if f == nil {
		t.Fatal("expected the initial jump to fire")
	}
	countAfterPlateau := f.Count

	for i := 0; i < 100; i++ {
		d.Tick(now.Add(time.Duration(425+i) * time.Second))
	}
	if got := gsFlagOfType(fs); got.Count != countAfterPlateau {
		t.Errorf("the baseline never caught up: Count still rising (%d -> %d) after a long plateau at a constant rate",
			countAfterPlateau, got.Count)
	}
}

// TestShippedGlobalSpikeDisabledNeverFires and
// TestShippedGlobalSpikeReenableRePrimes are
// internal/detect/global_spike_test.go's tests of the same names. The
// second is the behaviour that deliberately differs from rule_spike's,
// for the reason internal/detect recorded: this definition is handed an
// accurate current rate on every reading, so re-priming after a gap
// gives it a correct baseline immediately, where rule_spike's ring-based
// rate would read its own refill as a spike.
func TestShippedGlobalSpikeDisabledNeverFires(t *testing.T) {
	fs := newTestFlagsStore(t)
	rate := &fixedRate{}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)
	d.def.Enabled = false
	now := time.Now()

	rate.eps = 1
	for i := 0; i < 25; i++ {
		d.Tick(now.Add(time.Duration(i) * time.Second))
	}
	rate.eps = 500
	d.Tick(now.Add(25 * time.Second))

	if got := gsFlagOfType(fs); got != nil {
		t.Fatalf("expected a disabled definition never to fire, got %+v", got)
	}
}

func TestShippedGlobalSpikeReenableRePrimes(t *testing.T) {
	fs := newTestFlagsStore(t)
	rate := &fixedRate{}
	d := newShippedGlobalSpikeDefinition(t, fs, rate, nil)
	now := time.Now()

	warmGlobalSpike(d, rate, 25, now)

	// Off while traffic climbs to a new sustained level.
	d.def.Enabled = false
	rate.eps = 100
	for i := 0; i < 10; i++ {
		d.Tick(now.Add(time.Duration(25+i) * time.Second))
	}

	// Back on: the first reading after re-enabling re-primes against the
	// new level rather than comparing it to the stale baseline of 1.
	d.def.Enabled = true
	d.Tick(now.Add(40 * time.Second))
	if got := gsFlagOfType(fs); got != nil {
		t.Fatalf("expected re-enabling to re-prime rather than fire immediately, got %+v", got)
	}
	d.Tick(now.Add(41 * time.Second))
	if got := gsFlagOfType(fs); got != nil {
		t.Fatalf("expected steady traffic at the new level not to fire once re-primed, got %+v", got)
	}
}

// TestShippedGlobalSpikeIsReplayable pins the classification and the
// reconstruction Replay performs -- see its own doc comment for why
// rebuilding the reading from the corpus's timestamps is the honest
// answer rather than a claim that the live counter was re-observed.
func TestShippedGlobalSpikeIsReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedGlobalSpikeDefinition(t, fs, &fixedRate{}, nil)

	receiptCapable, reason, ok := Replayability(d)
	if !ok || !receiptCapable {
		t.Fatalf("Replayability = (%v, %q, %v), want a replayable classification", receiptCapable, reason, ok)
	}

	t0 := time.Now()
	var events []store.Event
	// A quiet stretch, then a burst far above it.
	for i := 0; i < 300; i++ {
		events = append(events, store.Event{SrcIP: "203.0.113.9", ReceivedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	burst := t0.Add(300 * time.Second)
	for i := 0; i < 600; i++ {
		events = append(events, store.Event{SrcIP: "203.0.113.9", ReceivedAt: burst.Add(time.Duration(i) * 100 * time.Millisecond)})
	}

	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Receipt == nil {
		t.Fatalf("expected a receipt over a corpus longer than the sampling interval, got %+v", res)
	}
	if res.Receipt.EmissionCount() == 0 {
		t.Error("expected a tenfold jump in rate to have fired at least once in the replay")
	}

	// A corpus shorter than one sampling interval has no reading in it.
	res2, err := d.Replay(fakeCorpus{events: events[:3]}, nil)
	if err != nil {
		t.Fatalf("Replay (short corpus): %v", err)
	}
	if res2.Decline == nil {
		t.Fatalf("expected a Decline on a corpus shorter than the sampling interval, got %+v", res2)
	}
}

// TestShippedGlobalSpikeConfidenceGrowsWithSampleHistory is
// internal/detect/global_spike_test.go's test of the same name: an
// identical spike reads as more confident when more history backs the
// baseline it deviates from. That is emaConfidence's history term doing
// its job, and it is worth pinning on this side of the port because the
// sample count reaching it changed shape -- Baseline.Samples is uncapped
// where internal/detect's own counter was capped at warmupSamples, and
// the definition caps it only for display. If those two ever diverged,
// this is the assertion that would notice.
func TestShippedGlobalSpikeConfidenceGrowsWithSampleHistory(t *testing.T) {
	now := time.Now()

	earlyFS := newTestFlagsStore(t)
	earlyRate := &fixedRate{}
	early := newShippedGlobalSpikeDefinition(t, earlyFS, earlyRate, nil)
	earlyRate.eps = 10
	// Prime plus a handful of steady readings: some history, but far
	// short of the 20-sample warm-up.
	for i := 0; i < 4; i++ {
		early.Tick(now.Add(time.Duration(i) * time.Second))
	}
	earlyRate.eps = 60
	early.Tick(now.Add(4 * time.Second))
	ef := gsFlagOfType(earlyFS)
	if ef == nil || ef.Confidence == nil {
		t.Fatalf("expected one scored flag from the early spike, got %+v", ef)
	}

	lateFS := newTestFlagsStore(t)
	lateRate := &fixedRate{}
	late := newShippedGlobalSpikeDefinition(t, lateFS, lateRate, nil)
	lateRate.eps = 10
	// The same spike, but with a full warm-up of steady history first.
	for i := 0; i < 20; i++ {
		late.Tick(now.Add(time.Duration(i) * time.Second))
	}
	lateRate.eps = 60
	late.Tick(now.Add(20 * time.Second))
	lf := gsFlagOfType(lateFS)
	if lf == nil || lf.Confidence == nil {
		t.Fatalf("expected one scored flag from the late spike, got %+v", lf)
	}

	if *lf.Confidence <= *ef.Confidence {
		t.Errorf("expected more sample history to read as more confident for an equivalent spike: early=%d, late=%d",
			*ef.Confidence, *lf.Confidence)
	}
}
