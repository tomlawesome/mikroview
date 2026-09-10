// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func newShippedActivitySpikeDefinition(t *testing.T, fs *flags.Store, params Params, scope Scope) *activitySpikeDefinition {
	t.Helper()
	full := Params{
		"threshold":               200,
		"window":                  (60 * time.Second).String(),
		"baselineMultiplier":      3.0,
		"warmupSamples":           20,
		"vpnInterfaces":           []string{},
		"vpnConfidenceMultiplier": 1.5,
		"updateCadence":           "perEvent",
		"baselineFloorDuration":   time.Duration(0).String(),
	}
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          "activity_spike",
		Name:        "Activity spike",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Scope:       scope,
		Params:      full,
		ParamSchema: ActivitySpikeParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(activity_spike): %v", err)
	}
	d := built.(*activitySpikeDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func asFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeActivitySpike {
			return &f
		}
	}
	return nil
}

// TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig retires
// TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig
// (#420's finding, previously pinned rather than fixed): the owner
// decision recorded on #420 (2026-08-22) is the freeze/bucket redesign
// this file now implements, and this is its fail-first replacement --
// run against the pre-redesign code it fails exactly as the old pin
// asserted (no flag; see the old pin's own git history for that run),
// and against the redesign it passes.
//
// At DefaultConfig's real values, the old per-event EMA fold-in could
// never lag the observed rate by more than 1/emaAlpha = 50 events (see
// baseline.go's emaAlpha), so by the time a ramp reached the 200-event
// threshold the baseline had already caught up to within ~50 of it and
// the 3x multiplier condition was unreachable. The freeze this file adds
// (checkBaseline) breaks that chase: once the ramp's z-score first
// clears emaMinZ -- which happens early, while the baseline is still
// close to the source's genuine pre-ramp normal -- the baseline stops
// moving entirely, so the gap the 3x condition needs opens up as the
// ramp continues past it.
func TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.4"
	t0 := time.Now()

	// 2,000 events from one source, all inside the window, so the live
	// count ramps continuously to well past the 200 threshold.
	for i := 0; i < 2000; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: t0.Add(time.Duration(i) * 10 * time.Millisecond)})
	}

	got := asFlagOfType(fs)
	if got == nil {
		t.Fatal("expected a continuous ramp to fire at DefaultConfig once the freeze fix is in place -- see this test's own doc comment for why")
	}
	if got.Target != ip {
		t.Errorf("Target = %q, want %q", got.Target, ip)
	}
	if got.Confidence == nil || *got.Confidence <= 0 {
		t.Errorf("Confidence = %v, want a positive score", got.Confidence)
	}

	// The mechanism, not just the symptom: the baseline the ramp fired
	// against stayed near the source's early, pre-ramp normal --
	// nowhere close to "caught up" the way the retired pin proved it
	// used to.
	snap, ok := d.baselines.snapshot(ip, t0.Add(2000*10*time.Millisecond))
	if !ok {
		t.Fatal("expected a baseline to exist for the ramping source")
	}
	if snap.Value > 50 {
		t.Errorf("baseline = %.1f after a 2,000-event ramp, want it frozen near the source's early normal (far below the old ~1,900+ catch-up point)", snap.Value)
	}
}

// TestShippedActivitySpike_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's test of the same name,
// moved. Every pinned value is unchanged.
//
// Like the original, it drives the baseline check directly rather than
// through the event path, because of the test above: no input through
// Evaluate can present these values at DefaultConfig's real thresholds.
// internal/detect's pins had the same shape for the same reason (see
// TestActivitySpikeNeverFiresBeforeMinimumSampleFloor), and
// activitySpikeDefinition keeps the seam deliberately so they move
// across testing the same thing.
func TestShippedActivitySpike_FieldsRefireClearRevive(t *testing.T) {
	ip := "198.51.100.4"
	now := time.Now()

	warm := func(d *activitySpikeDefinition) {
		for i := 0; i < 25; i++ {
			d.checkBaseline(ip, "FR", "", 1, now.Add(time.Duration(i)*time.Second))
		}
	}

	// The below-floor probe gets its own definition: a reading is folded
	// in unconditionally afterwards, so probing 199 on the instance that
	// pins the 200 boundary would perturb the zero-variance baseline that
	// pin depends on.
	probeFS := newTestFlagsStore(t)
	probe := newShippedActivitySpikeDefinition(t, probeFS, nil, Scope{})
	warm(probe)
	probe.checkBaseline(ip, "FR", "", 199, now.Add(25*time.Second))
	if got := asFlagOfType(probeFS); got != nil {
		t.Fatalf("expected no flag at rate=199 (threshold 200 is an absolute floor), got %+v", got)
	}

	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})
	warm(d)
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag from the steady warm-up phase, got %+v", got)
	}

	d.checkBaseline(ip, "FR", "", 200, now.Add(25*time.Second))
	f := asFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag at exactly rate=200")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if f.Country != "FR" {
		t.Errorf("Country = %q, want FR", f.Country)
	}
	if want := "200 events in 1m0s vs a baseline of 1.0 for this host (based on 20 samples, 6.0σ above normal)"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 || f.Evidence.NAT != nil {
		t.Errorf("Evidence = %+v, want the zero value (activity_spike carries no structured evidence)", f.Evidence)
	}
	if f.Confidence == nil || *f.Confidence != 100 {
		t.Errorf("Confidence at the boundary = %v, want 100 (zero-variance warm-up collapses emaZScore to its capped ceiling)", f.Confidence)
	}
	if f.Count != 1 {
		t.Errorf("Count = %d, want 1", f.Count)
	}

	// Re-fire.
	d.checkBaseline(ip, "FR", "", 200, now.Add(27*time.Second))
	f2 := asFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence <= 0 || *f2.Confidence > 100 {
		t.Errorf("Confidence after re-fire = %v, want a value in (0, 100]", f2.Confidence)
	}

	// Clear + revive.
	if _, ok := fs.SetVerdict(f2.ID, flags.VerdictChecked, "operator", now.Add(28*time.Second)); !ok {
		t.Fatal("expected Clear to succeed")
	}
	reviveAt := now.Add(29 * time.Second)
	d.checkBaseline(ip, "FR", "", 200, reviveAt)
	f3 := asFlagOfType(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if !f3.FirstSeen.Equal(reviveAt) {
		t.Errorf("FirstSeen after revival = %v, want %v", f3.FirstSeen, reviveAt)
	}
}

// TestShippedActivitySpikeNeverFiresBeforeMinimumSampleFloor is
// internal/detect/detect_test.go's test of the same name: the
// hostActivityMinSamples floor, which is a hard floor independent of the
// operator-set warmupSamples.
func TestShippedActivitySpikeNeverFiresBeforeMinimumSampleFloor(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, Params{"threshold": 2, "warmupSamples": 20}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()

	d.checkBaseline(ip, "", "", 1, now) // primes
	for i := 0; i < hostActivityMinSamples-2; i++ {
		d.checkBaseline(ip, "", "", 100, now.Add(time.Duration(i+1)*time.Second))
	}
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag below the %d-sample floor, got %+v", hostActivityMinSamples, got)
	}
}

// TestShippedActivitySpikeStillFiresWhenWarmupSamplesBelowFloor is
// internal/detect/detect_test.go's test of the same name: warmupSamples
// controls confidence scoring, not firing eligibility, so setting it
// below hostActivityMinSamples must not permanently disable the
// detector.
func TestShippedActivitySpikeStillFiresWhenWarmupSamplesBelowFloor(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, Params{"threshold": 2, "warmupSamples": 2}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()

	d.checkBaseline(ip, "", "", 1, now) // primes
	for i := 0; i < hostActivityMinSamples; i++ {
		d.checkBaseline(ip, "", "", 100, now.Add(time.Duration(i+1)*time.Second))
	}
	if got := asFlagOfType(fs); got == nil {
		t.Fatal("expected the detector to still fire once the sample floor is reached, despite a lower warmupSamples")
	}
}

// TestShippedActivitySpikeIgnoresEstablishedTraffic is
// internal/detect/detect_test.go's TestScanAndSpikeIgnoreEstablishedTraffic,
// moved (its port_scan half went with that detector in slice 1): a busy
// server's ordinary return traffic on a stateful accept rule must not
// trip a threshold meant to catch new activity.
func TestShippedActivitySpikeIgnoresEstablishedTraffic(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, Params{"threshold": 3, "window": time.Minute.String()}, Scope{})
	now := time.Now()

	for i := 0; i < 200; i++ {
		d.Evaluate(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 40000 + i,
			ConnState: "established", ReceivedAt: now.Add(time.Duration(i) * time.Millisecond)})
	}
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected established traffic never to reach the window, got %+v", got)
	}
	if d.counts.Len() != 0 {
		t.Errorf("expected established traffic to create no per-source state at all, got %d key(s)", d.counts.Len())
	}
}

// TestShippedActivitySpikeVPNBoostsConfidence pins the #105 confidence
// boost and its Detail suffix on this definition, which is one of the
// two places internal/detect applied it.
func TestShippedActivitySpikeVPNBoostsConfidence(t *testing.T) {
	ip := "198.51.100.4"
	now := time.Now()
	warm := func(d *activitySpikeDefinition, iface string) {
		for i := 0; i < 25; i++ {
			d.checkBaseline(ip, "", iface, 1, now.Add(time.Duration(i)*time.Second))
		}
	}

	// A partial-confidence spike, so a boost has room to show. The
	// zero-variance warm-up above would pin confidence at 100 and hide
	// it, so this one uses a lower multiplier and a smaller overshoot.
	plainFS := newTestFlagsStore(t)
	plain := newShippedActivitySpikeDefinition(t, plainFS,
		Params{"threshold": 2, "warmupSamples": 100}, Scope{})
	warm(plain, "ether1")
	plain.checkBaseline(ip, "", "ether1", 10, now.Add(25*time.Second))
	pf := asFlagOfType(plainFS)
	if pf == nil || pf.Confidence == nil {
		t.Fatalf("expected a scored flag from the LAN interface, got %+v", pf)
	}

	vpnFS := newTestFlagsStore(t)
	vpn := newShippedActivitySpikeDefinition(t, vpnFS,
		Params{"threshold": 2, "warmupSamples": 100, "vpnInterfaces": []string{"wireguard*"}}, Scope{})
	warm(vpn, "wireguard1")
	vpn.checkBaseline(ip, "", "wireguard1", 10, now.Add(25*time.Second))
	vf := asFlagOfType(vpnFS)
	if vf == nil || vf.Confidence == nil {
		t.Fatalf("expected a scored flag from the VPN interface, got %+v", vf)
	}

	if *vf.Confidence <= *pf.Confidence {
		t.Errorf("expected a VPN-tagged interface to score more confidently: lan=%d vpn=%d", *pf.Confidence, *vf.Confidence)
	}
	want := " -- arrived via VPN interface \"wireguard1\", scored more confidently as an already-authenticated remote peer"
	if len(vf.Detail) < len(want) || vf.Detail[len(vf.Detail)-len(want):] != want {
		t.Errorf("Detail = %q, want it to end with the VPN explanation %q", vf.Detail, want)
	}
	if pf.Detail == vf.Detail {
		t.Error("expected the VPN suffix to be absent from the LAN-interface flag")
	}
}

// TestShippedActivitySpikeScope_Classification is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_Classification, moved: this definition is
// where that axis was pinned once port_scan left internal/detect.
func TestShippedActivitySpikeScope_Classification(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "window": time.Second.String(), "warmupSamples": 20},
		Scope{Classification: store.ScopeInternal})
	now := time.Now()

	// An external source: never tracked at all under Classification=Internal.
	for i := 0; i < 50; i++ {
		d.Evaluate(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: now.Add(time.Duration(i) * time.Millisecond)})
	}
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected an external source to never flag under Classification=Internal, got %+v", got)
	}
	if _, ok := d.counts.Get("198.51.100.4"); ok {
		t.Error("expected an out-of-scope source to create no state at all")
	}
}

// TestShippedActivitySpikeIsReplayable pins the classification, and --
// since the owner decision on #420 rejected "replay stays on the old
// arithmetic" as its own kind of dishonesty -- pins replay/live
// equivalence: replaying the same continuous ramp
// TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig proves
// fires live must also fire here, against the same corpus, through the
// same activitySpikeCheck. A replay that still reported zero on this
// exact scenario would be the product contradicting itself: "fires,
// but only if you actually run it" is not a claim replay-with-receipts
// is allowed to make once the live detector genuinely fires.
func TestShippedActivitySpikeIsReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})

	receiptCapable, reason, ok := Replayability(d)
	if !ok || !receiptCapable {
		t.Fatalf("Replayability = (%v, %q, %v), want a replayable classification", receiptCapable, reason, ok)
	}

	// 50ms spacing (not TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig's
	// 10ms): the corpus itself must span longer than the window
	// (DefaultConfig's 60s) or Replay declines outright (see the span<window
	// check below) -- 2,000 * 50ms = 100s clears that, 2,000 * 10ms's 20s
	// would not. Still the same shape of scenario: a continuous
	// single-source ramp with no gaps.
	t0 := time.Now()
	var events []store.Event
	for i := 0; i < 2000; i++ {
		events = append(events, store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 80,
			ConnState: "new", ReceivedAt: t0.Add(time.Duration(i) * 50 * time.Millisecond)})
	}
	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Receipt == nil {
		t.Fatalf("expected a receipt over a corpus longer than the window, got %+v", res)
	}
	if res.Receipt.EmissionCount() == 0 {
		t.Fatal("EmissionCount = 0, want a non-zero count -- this is the same continuous ramp that fires live at DefaultConfig (TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig); replay running different arithmetic than live is exactly what #420's owner decision rejected")
	}
	samples := res.Receipt.Sample()
	if len(samples) == 0 {
		t.Fatal("expected at least one sample emission on the receipt")
	}
	if s := samples[0]; s.Target != "198.51.100.4" || s.Detail == "" {
		t.Errorf("sample[0] = %+v, want a populated Target/Detail", s)
	}

	// A tighter multiplier only ever needed z-score alone to bind before
	// (see the old pin this replaced): now that entry is z-score-gated
	// and independent of the multiplier, loosening it further changes
	// nothing about whether this ramp fires -- it already does.
	res2, err := d.Replay(fakeCorpus{events: events}, Params{"baselineMultiplier": 1.05})
	if err != nil {
		t.Fatalf("Replay (candidate): %v", err)
	}
	if res2.Receipt == nil {
		t.Fatalf("expected a receipt for the candidate sweep, got %+v", res2)
	}
	if res2.Receipt.EmissionCount() == 0 {
		t.Error("EmissionCount = 0 with baselineMultiplier=1.05, want a non-zero count -- a looser multiplier must not turn off a firing that already happens at the shipped multiplier")
	}
}

// TestShippedActivitySpikeFiresThroughEvaluateAtASmallerScale is
// internal/detect/detect_test.go's
// TestActivitySpikeFlagsGenuineDeviationFromHostsOwnBaseline, moved --
// and it is the one activity_spike test that must go through Evaluate
// rather than checkBaseline, because everything else on this definition
// either asserts "never fires" or drives the seam directly. Without it,
// nothing would prove the Evaluate -> CountRing -> checkBaseline wiring
// produces a flag end to end, and the port could have left that path
// broken with every other pin still green.
//
// It runs at a smaller scale than DefaultConfig for the reason #420
// documents: at shipped values the lag ceiling makes the 3x condition
// unreachable through this path. At a threshold of 2 the same ceiling is
// not binding -- a baseline of ~1 need only be outrun by a count of ~4,
// far inside the ~50-event lag the EMA can carry -- so the wiring is
// demonstrable even though the shipped configuration's is not. That
// difference is #420's whole substance, and it is why this test exists
// beside TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig
// rather than instead of it.
func TestShippedActivitySpikeFiresThroughEvaluateAtASmallerScale(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "window": time.Second.String(), "warmupSamples": 5}, Scope{})
	ip := "198.51.100.4"
	t0 := time.Now()

	// Warm-up: one event per window, spaced two windows apart, so every
	// reading is exactly 1 and the EMA's variance stays at zero.
	for i := 0; i < 8; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: t0.Add(time.Duration(i) * 2 * time.Second)})
	}
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected steady one-per-window traffic never to fire, got %+v", got)
	}

	// A genuine burst: several events inside one window, so the live
	// count outruns the baseline this source established for itself.
	burst := t0.Add(20 * time.Second)
	for i := 0; i < 6; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: burst.Add(time.Duration(i) * 100 * time.Millisecond)})
	}

	f := asFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a genuine deviation from the host's own baseline to fire through Evaluate")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if f.Confidence == nil || *f.Confidence <= 0 {
		t.Errorf("Confidence = %v, want a positive score", f.Confidence)
	}
}

// TestShippedActivitySpikeScope_ClassificationStillFiresInScope is the
// other half of internal/detect's TestCharacterizationScope_Classification:
// the out-of-scope direction alone can be satisfied by a definition that
// never fires at all, so the in-scope direction is what makes that pin
// mean something.
func TestShippedActivitySpikeScope_ClassificationStillFiresInScope(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "window": time.Second.String(), "warmupSamples": 5},
		Scope{Classification: store.ScopeInternal})
	ip := "192.168.1.77"
	t0 := time.Now()

	for i := 0; i < 8; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: t0.Add(time.Duration(i) * 2 * time.Second)})
	}
	burst := t0.Add(20 * time.Second)
	for i := 0; i < 6; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: burst.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	if got := asFlagOfType(fs); got == nil {
		t.Fatal("expected an internal source to still fire under Classification=Internal")
	}
}

// TestShippedActivitySpikeDisabledIsInert pins the enable/disable gate
// for this definition specifically. The gate itself lives on the shared
// programmaticBase.active, and global_spike and rule_spike each pin their
// own -- but "it relies on a shared helper two siblings test" is exactly
// the reasoning that lets a definition quietly stop honouring it (by
// checking state before calling active, say), so it is pinned per
// definition rather than per helper.
func TestShippedActivitySpikeDisabledIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "window": time.Second.String(), "warmupSamples": 5}, Scope{})
	d.def.Enabled = false
	ip := "198.51.100.4"
	t0 := time.Now()

	for i := 0; i < 8; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: t0.Add(time.Duration(i) * 2 * time.Second)})
	}
	burst := t0.Add(20 * time.Second)
	for i := 0; i < 6; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: burst.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("expected a disabled definition never to fire, got %+v", got)
	}
	if d.counts.Len() != 0 {
		t.Errorf("expected a disabled definition to accumulate no state at all, got %d key(s)", d.counts.Len())
	}
}

// TestShippedActivitySpikeVPNUnsetLeavesConfidenceUnchanged is
// internal/detect/vpn_test.go's
// TestActivitySpikeConfidenceIdenticalRegardlessOfInterfaceWhenVPNInterfacesUnset,
// moved: with vpnInterfaces unset -- the shipped default -- the
// interface name must make no difference at all to the score or the
// sentence. This is the backward-compatibility promise VPNInterfaces'
// own doc comment makes ("empty matches no interface, so every existing
// deployment's confidence scoring is completely unchanged until this is
// explicitly configured"), pinned through this definition's wiring
// rather than only through the helper it calls.
func TestShippedActivitySpikeVPNUnsetLeavesConfidenceUnchanged(t *testing.T) {
	ip := "198.51.100.4"
	now := time.Now()
	score := func(iface string) *flags.Flag {
		fs := newTestFlagsStore(t)
		d := newShippedActivitySpikeDefinition(t, fs,
			Params{"threshold": 2, "warmupSamples": 100}, Scope{}) // vpnInterfaces stays empty
		for i := 0; i < 25; i++ {
			d.checkBaseline(ip, "", iface, 1, now.Add(time.Duration(i)*time.Second))
		}
		d.checkBaseline(ip, "", iface, 10, now.Add(25*time.Second))
		f := asFlagOfType(fs)
		if f == nil || f.Confidence == nil {
			t.Fatalf("expected a scored flag for interface %q, got %+v", iface, f)
		}
		return f
	}

	lan := score("ether1")
	wg := score("wireguard1")
	if *lan.Confidence != *wg.Confidence {
		t.Errorf("with vpnInterfaces unset, confidence differed by interface: ether1=%d wireguard1=%d",
			*lan.Confidence, *wg.Confidence)
	}
	if lan.Detail != wg.Detail {
		t.Errorf("with vpnInterfaces unset, Detail differed by interface:\n  ether1     = %q\n  wireguard1 = %q", lan.Detail, wg.Detail)
	}
}

// TestShippedActivitySpikeFreezeHoldsBaselineDuringCandidateSpike is the
// #420 redesign's central freeze property (design item 4): once a
// reading's z-score against the applicable baseline clears emaMinZ,
// every subsequent reading during that same candidate spike must leave
// the baseline being measured against completely unmoved, not merely
// slowed down.
func TestShippedActivitySpikeFreezeHoldsBaselineDuringCandidateSpike(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "warmupSamples": 100}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()

	// Warm up a steady, zero-variance baseline at rate=1.
	for i := 0; i < 25; i++ {
		d.checkBaseline(ip, "", "", 1, now.Add(time.Duration(i)*time.Second))
	}
	before, ok := d.baselines.snapshot(ip, now.Add(25*time.Second))
	if !ok || before.Value != 1 {
		t.Fatalf("expected a warmed-up baseline of exactly 1.0, got %+v (ok=%v)", before, ok)
	}

	// A sustained candidate spike: several elevated readings in a row,
	// each one individually enough to have moved a non-frozen EMA
	// baseline. The value/variance/samples the spike is measured
	// against must not change across any of them.
	for i := 0; i < 10; i++ {
		d.checkBaseline(ip, "", "", 10, now.Add(time.Duration(26+i)*time.Second))
		snap, ok := d.baselines.snapshot(ip, now.Add(time.Duration(26+i)*time.Second))
		if !ok {
			t.Fatalf("iteration %d: expected the baseline to still exist", i)
		}
		if snap.Value != before.Value || snap.Variance != before.Variance || snap.Samples != before.Samples {
			t.Fatalf("iteration %d: baseline moved during a candidate spike -- before=%+v after=%+v", i, before, snap)
		}
	}
}

// TestShippedActivitySpikeBackstopForcesOneFoldInAndReconverges is the
// #420 redesign's backstop (design item 4): a freeze held continuously
// past activitySpikeFreezeBackstop*window forces exactly one fold-in,
// then re-evaluates and re-freezes if the plateau is still elevated.
// Held over many such cycles, the baseline must trend toward the
// plateau's own rate -- spot-checked by direction, not an exact value,
// per #420's own instruction (an exact EMA trajectory is an
// implementation detail, not the property that matters).
func TestShippedActivitySpikeBackstopForcesOneFoldInAndReconverges(t *testing.T) {
	fs := newTestFlagsStore(t)
	window := time.Second // ParamSchema's window floor is 1s (durationBound(time.Second))
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "window": window.String(), "warmupSamples": 100}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()
	const plateau = 50.0

	for i := 0; i < 25; i++ {
		d.checkBaseline(ip, "", "", 1, now.Add(time.Duration(i)*time.Second))
	}
	entryAt := now.Add(25 * time.Second)
	d.checkBaseline(ip, "", "", plateau, entryAt) // entry: freeze engages
	afterEntry, ok := d.baselines.snapshot(ip, entryAt)
	if !ok || afterEntry.Samples != 25 {
		t.Fatalf("expected the entry reading itself to be excluded from folding (samples still 25), got %+v (ok=%v)", afterEntry, ok)
	}

	// Repeated plateau readings well inside the backstop window (5x the
	// 1s window = 5s) must not fold at all -- still frozen, still no
	// backstop trigger.
	for i, at := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		d.checkBaseline(ip, "", "", plateau, entryAt.Add(at))
		snap, _ := d.baselines.snapshot(ip, entryAt.Add(at))
		if snap.Samples != 25 {
			t.Fatalf("plateau reading %d (t+%s): expected no fold-in yet (samples still 25), got samples=%d", i, at, snap.Samples)
		}
	}

	// The reading that crosses the backstop threshold forces exactly one
	// fold-in.
	backstopAt := entryAt.Add(5500 * time.Millisecond)
	d.checkBaseline(ip, "", "", plateau, backstopAt)
	afterBackstop, ok := d.baselines.snapshot(ip, backstopAt)
	if !ok || afterBackstop.Samples != 26 {
		t.Fatalf("expected exactly one forced fold-in at the backstop (samples=26), got %+v (ok=%v)", afterBackstop, ok)
	}
	if afterBackstop.Value <= afterEntry.Value {
		t.Fatalf("expected the backstop's forced fold-in to move the baseline toward the plateau, got %.4f (was %.4f)", afterBackstop.Value, afterEntry.Value)
	}

	// Many more backstop cycles, spaced comfortably past the 5s backstop
	// window each time: the baseline must keep trending toward the
	// plateau rate, never away from it.
	last := afterBackstop.Value
	at := backstopAt
	for cycle := 0; cycle < 20; cycle++ {
		at = at.Add(5500 * time.Millisecond)
		d.checkBaseline(ip, "", "", plateau, at)
		snap, _ := d.baselines.snapshot(ip, at)
		if snap.Value < last {
			t.Fatalf("cycle %d: baseline regressed (%.4f -> %.4f), want monotonic convergence toward the plateau", cycle, last, snap.Value)
		}
		last = snap.Value
	}
	if last <= afterBackstop.Value {
		t.Fatalf("expected the baseline to have moved further toward the plateau over 20 more cycles: after first backstop=%.4f, after 20 more=%.4f", afterBackstop.Value, last)
	}
	if last >= plateau {
		t.Fatalf("baseline = %.4f overshot the plateau rate %.1f -- an EMA fold-in can approach but never exceed a constant target", last, plateau)
	}
}

// TestShippedActivitySpikeThawResumesNormalFoldInWithNoJump is the
// #420 redesign's primary thaw path (design item 4): once the rate
// drops back under the absolute threshold, fold-in resumes immediately,
// as one ordinary small EMA step -- not a jump to the just-observed
// value, and not a lingering freeze.
func TestShippedActivitySpikeThawResumesNormalFoldInWithNoJump(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 10, "warmupSamples": 100}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()

	for i := 0; i < 25; i++ {
		d.checkBaseline(ip, "", "", 1, now.Add(time.Duration(i)*time.Second))
	}
	entryAt := now.Add(25 * time.Second)
	d.checkBaseline(ip, "", "", 50, entryAt) // entry: freeze engages, rate=50 >= threshold=10
	frozen, ok := d.baselines.snapshot(ip, entryAt)
	if !ok || frozen.Samples != 25 || frozen.Value != 1 {
		t.Fatalf("expected the entry reading excluded from folding (samples=25, value=1), got %+v (ok=%v)", frozen, ok)
	}

	// rate=5 is below the absolute threshold (10) -- thaw, and this
	// reading itself folds in normally (the ordinary per-event cadence
	// resumes on the very reading that thaws).
	thawAt := entryAt.Add(time.Second)
	d.checkBaseline(ip, "", "", 5, thawAt)
	thawed, ok := d.baselines.snapshot(ip, thawAt)
	if !ok {
		t.Fatal("expected the baseline to still exist after thaw")
	}
	if thawed.Samples != 26 {
		t.Fatalf("expected exactly one fold-in on the thawing reading (samples=26), got samples=%d", thawed.Samples)
	}
	wantValue, wantVariance := emaUpdate(5, frozen.Value, frozen.Variance)
	if thawed.Value != wantValue || thawed.Variance != wantVariance {
		t.Fatalf("expected an ordinary emaUpdate(5, %.4f, %.4f) step = (%.4f, %.4f), got (%.4f, %.4f)",
			frozen.Value, frozen.Variance, wantValue, wantVariance, thawed.Value, thawed.Variance)
	}
	if thawed.Value >= 5 {
		t.Fatalf("baseline = %.4f jumped to (or past) the thawing reading's own value 5 -- want a small alpha-weighted step, not a jump", thawed.Value)
	}

	// Fold-in keeps resuming normally afterward -- not a one-off.
	d.checkBaseline(ip, "", "", 1, thawAt.Add(time.Second))
	afterNext, ok := d.baselines.snapshot(ip, thawAt.Add(time.Second))
	if !ok || afterNext.Samples != 27 {
		t.Fatalf("expected normal fold-in to continue after thaw (samples=27), got %+v (ok=%v)", afterNext, ok)
	}
}

// TestShippedActivitySpikeImmatureBucketFallsBackToGlobalEMA is design
// item 3's "mature bucket, else warm-up fallback": a source with no
// hour-bucket history yet (the ordinary case for every source until it
// has lived through at least one full prior day at some hour) must be
// judged against the fallback EMA, exactly as before the #420 redesign.
func TestShippedActivitySpikeImmatureBucketFallsBackToGlobalEMA(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 2, "warmupSamples": 20}, Scope{})
	ip := "198.51.100.4"
	now := time.Now()

	for i := 0; i < 25; i++ {
		d.checkBaseline(ip, "", "", 1, now.Add(time.Duration(i)*time.Second))
	}
	fireAt := now.Add(25 * time.Second)
	d.checkBaseline(ip, "", "", 10, fireAt)

	f := asFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag from the fallback path")
	}
	if want := "10 events in 1m0s vs a baseline of 1.0 for this host (based on 20 samples, 6.0σ above normal)"; f.Detail != want {
		t.Errorf("Detail = %q, want the fallback-style template %q (not the mature-bucket template, which names an hour and a day count)", f.Detail, want)
	}

	key := activityBucketKey(ip, fireAt.Hour())
	if _, ok := d.buckets.snapshot(key, fireAt); ok {
		t.Error("expected no hour-bucket state to exist at all for a source that has never lived through a day boundary")
	}
}

// TestShippedActivitySpike_BucketLearnsAcrossNights is the backup-shape
// scenario the #420 redesign exists for (design item 1/3): sustained
// elevated traffic recurring at the same hour on two simulated calendar
// days, with time driven entirely through event timestamps (never a
// wall-clock read). Night one has no same-hour history yet, so it is
// judged against the fallback EMA and fires. By night two, the hour's
// own bucket has learned night one's peak and become the applicable
// baseline -- night two's identical traffic is that bucket's own normal,
// so it does not fire.
func TestShippedActivitySpike_BucketLearnsAcrossNights(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 20, "window": (5 * time.Second).String(), "warmupSamples": 50}, Scope{})
	ip := "198.51.100.4"

	// A fixed, explicit UTC day-one 22:00 -- every subsequent timestamp
	// is derived from this by simple addition, never time.Now().
	night1 := time.Date(2024, 3, 1, 22, 0, 0, 0, time.UTC)
	daytime := night1.Add(-10 * time.Hour) // 12:00 the same day

	// Daytime baseline: ordinary, low, steady traffic at a different
	// hour, spaced past the window so each reading is a clean rate=1 --
	// what night one's spike is judged against.
	for i := 0; i < 10; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: daytime.Add(time.Duration(i) * 6 * time.Second)})
	}

	burst := func(start time.Time) {
		for i := 0; i < 30; i++ {
			d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
				ReceivedAt: start.Add(time.Duration(i) * 100 * time.Millisecond)})
		}
	}

	// Night one: sustained elevated traffic at 22:00 -- no history for
	// this hour yet, judged against the fallback EMA, fires.
	burst(night1)
	if got := asFlagOfType(fs); got == nil {
		t.Fatal("expected night one's spike to fire against the fallback EMA")
	}
	if _, ok := d.buckets.snapshot(activityBucketKey(ip, 22), night1); ok {
		t.Error("expected the hour-22 bucket to still not exist mid-way through night one (it only ever folds at the next day's rollover)")
	}

	// Thaw before the day rolls over, still within hour 22 on day one --
	// otherwise rollHourBucket would see the source still frozen at the
	// day boundary and withhold night one's fold-in entirely (see that
	// function's own doc comment on why an active freeze withholds it).
	d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
		ReceivedAt: night1.Add(10 * time.Second)})

	fs.SetVerdict(asFlagOfType(fs).ID, flags.VerdictChecked, "operator", night1.Add(11*time.Second))

	// Night two: the identical burst, 24 hours later. The first event of
	// this burst is also the first hour-22 event since the day rolled
	// over, so it is what triggers rollHourBucket to fold night one's
	// peak (30) into the bucket before this same event is judged.
	night2 := night1.Add(24 * time.Hour)
	burst(night2)

	bucketSnap, ok := d.buckets.snapshot(activityBucketKey(ip, 22), night2)
	if !ok || !bucketSnap.Ready {
		t.Fatalf("expected the hour-22 bucket to be mature by night two, got %+v (ok=%v)", bucketSnap, ok)
	}
	if bucketSnap.Value != 30 {
		t.Errorf("hour-22 bucket value = %.1f, want 30 (night one's peak windowed rate)", bucketSnap.Value)
	}
	// asFlagOfType returns night one's flag regardless -- clearing does
	// not delete it, only marks it Cleared (see the FieldsRefireClearRevive
	// pin above for that same revive-on-refire shape). Not firing on
	// night two means that flag stays cleared, with no new firing to
	// revive it: Count/LastSeen must be exactly what night one left them
	// at, not moved forward by any night-two firing.
	got := asFlagOfType(fs)
	if got == nil {
		t.Fatal("expected night one's (cleared) flag to still exist")
	}
	if !got.Cleared {
		t.Fatalf("expected night two's identical traffic to be judged against the now-mature hour-22 bucket and NOT fire (which would have revived the cleared flag), got %+v", got)
	}
	if got.Count != 11 || !got.LastSeen.Equal(night1.Add(2900*time.Millisecond)) {
		t.Fatalf("expected the flag untouched by night two (Count=11, LastSeen=night one's last burst event), got Count=%d LastSeen=%v", got.Count, got.LastSeen)
	}
}

// newActivitySpikeWithStateAndParams is newShippedActivitySpikeDefinition
// and newActivitySpikeWithState (shipped_export_test.go) combined: a
// StateStore-backed definition with operator-set params, needed here so
// two restarted definitions can share both a store and the small
// window/threshold TestShippedActivitySpike_BucketLearnsAcrossNights
// uses to make a bucket mature quickly.
func newActivitySpikeWithStateAndParams(t *testing.T, fs *flags.Store, state *StateStore, params Params) *activitySpikeDefinition {
	t.Helper()
	full := Params{
		"threshold":               200,
		"window":                  (60 * time.Second).String(),
		"baselineMultiplier":      3.0,
		"warmupSamples":           20,
		"vpnInterfaces":           []string{},
		"vpnConfidenceMultiplier": 1.5,
		"updateCadence":           "perEvent",
		"baselineFloorDuration":   time.Duration(0).String(),
	}
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          "activity_spike",
		Name:        "Activity spike",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      full,
		ParamSchema: ActivitySpikeParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{State: state})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(activity_spike): %v", err)
	}
	d := built.(*activitySpikeDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

// TestShippedActivitySpike_BucketResumesAfterRestart is issue #902: a
// restart used to lose a persisted, Ready hour bucket until the next
// day's own rollover happened to touch it. rollHourBucket's prevDay==""
// branch -- "first time this hour has ever been seen for this source"
// -- is both the true first-ever case and every first-since-restart
// case (a fresh process's in-memory state always starts there), but it
// used to return without ever consulting the StateStore, so a bucket an
// earlier process had already learned sat invisible for up to a day.
//
// Drives one definition against a real StateStore across a day boundary
// until the hour-22 bucket for one source is Ready and persisted, then
// builds a second, fresh definition against the same store (the
// restart) and evaluates a single event for that same source and hour:
// with the fix (buckets.resume in that branch), this first post-restart
// event already sees the resumed bucket, rather than needing a day
// boundary of its own.
func TestShippedActivitySpike_BucketResumesAfterRestart(t *testing.T) {
	withEagerBaselinePersist(t)
	state, err := OpenStateStoreWithBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	params := Params{"threshold": 20, "window": (5 * time.Second).String(), "warmupSamples": 50}

	fs1 := newTestFlagsStore(t)
	d1 := newActivitySpikeWithStateAndParams(t, fs1, state, params)
	ip := "198.51.100.9"

	night1 := time.Date(2024, 3, 1, 22, 0, 0, 0, time.UTC)
	burst := func(d *activitySpikeDefinition, start time.Time) {
		for i := 0; i < 30; i++ {
			d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
				ReceivedAt: start.Add(time.Duration(i) * 100 * time.Millisecond)})
		}
	}

	burst(d1, night1)
	if got := asFlagOfType(fs1); got == nil {
		t.Fatal("expected night one's spike to fire against the fallback EMA")
	}
	// Thaw before the day rolls over, same as
	// TestShippedActivitySpike_BucketLearnsAcrossNights -- otherwise
	// rollHourBucket would see the source still frozen at the day
	// boundary and withhold night one's fold-in.
	d1.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
		ReceivedAt: night1.Add(10 * time.Second)})

	// Day two: one event at hour 22 folds night one's peak (30) into the
	// hour-22 bucket and persists it -- still on d1, the process that
	// learned it.
	night2 := night1.Add(24 * time.Hour)
	d1.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new", ReceivedAt: night2})

	key := activityBucketKey(ip, 22)
	snap, ok := d1.buckets.snapshot(key, night2)
	if !ok || !snap.Ready || snap.Value != 30 {
		t.Fatalf("test setup: expected a Ready hour-22 bucket with Value=30 before the restart, got %+v (ok=%v)", snap, ok)
	}
	if _, ok := state.Get("activity_spike#hourly", key); !ok {
		t.Fatal("test setup: expected the hour-22 bucket to be persisted before the restart")
	}

	// The restart: a brand-new definition, against the same StateStore,
	// with nothing yet in its own in-memory buckets.
	fs2 := newTestFlagsStore(t)
	d2 := newActivitySpikeWithStateAndParams(t, fs2, state, params)
	if _, ok := d2.buckets.snapshot(key, night2); ok {
		t.Fatal("test setup: expected the restarted definition to start with no in-memory bucket state")
	}

	// A single event for the same source at the same hour -- this
	// definition's first-ever sight of hour 22, exactly the buggy branch.
	restartAt := night2.Add(1 * time.Minute)
	d2.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new", ReceivedAt: restartAt})

	resumed, ok := d2.buckets.snapshot(key, restartAt)
	if !ok || !resumed.Ready {
		t.Fatalf("expected the first event after a restart to resume the persisted, Ready hour-22 bucket immediately, got %+v (ok=%v)", resumed, ok)
	}
	if resumed.Value != 30 {
		t.Errorf("resumed hour-22 bucket value = %.1f, want 30 (night one's peak, learned before the restart)", resumed.Value)
	}
}
