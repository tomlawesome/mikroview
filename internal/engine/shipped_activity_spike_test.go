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

// TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig is
// internal/detect/characterization_test.go's test of the same name,
// moved: #420's finding, pinned rather than fixed.
//
// At DefaultConfig's real values the baseline folds in the current
// reading on every event, and the window's live count can only change by
// +1 per call, so the baseline can never lag the observed rate by more
// than 1/emaAlpha = 50. Firing needs the rate to clear both the absolute
// threshold (200) and 3x the baseline, and by the time any single-source
// ramp reaches 200 the baseline has caught up to within ~50 -- around
// 150+ -- so the 3x condition cannot be satisfied.
//
// #405 ports this detector as-is and #420 stays open: the remedy is a
// design decision (freeze the fold-in during a candidate spike, fold in
// per window instead of per event, or retune the multiplier/alpha
// relationship), not something a port is licensed to pick. This test is
// what would notice if the port had quietly picked one.
func TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig(t *testing.T) {
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

	if got := asFlagOfType(fs); got != nil {
		t.Fatalf("a continuous ramp fired at DefaultConfig -- #420 says it structurally cannot, so either the arithmetic changed or #420 is fixed; either way this pin needs updating deliberately: %+v", got)
	}

	// The lag ceiling itself, so the reason is pinned and not just the
	// symptom: the baseline lands within ~50 of the final count.
	snap, ok := d.baselines.snapshot(ip, t0.Add(2000*10*time.Millisecond))
	if !ok {
		t.Fatal("expected a baseline to exist for the ramping source")
	}
	if snap.Value < 1900 {
		t.Errorf("baseline = %.1f after a 2,000-event ramp, want it to have caught up to within ~50 (the 1/emaAlpha lag ceiling #420 derives)", snap.Value)
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
	if !fs.Clear(f2.ID, now.Add(28*time.Second)) {
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

// TestShippedActivitySpikeIsReplayable pins the classification. The
// receipt over an ordinary corpus is expected to be zero at shipped
// defaults, which is #420's finding restated as a number rather than a
// failure -- and is the instrument #420 asks for to demonstrate a fix.
func TestShippedActivitySpikeIsReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})

	receiptCapable, reason, ok := Replayability(d)
	if !ok || !receiptCapable {
		t.Fatalf("Replayability = (%v, %q, %v), want a replayable classification", receiptCapable, reason, ok)
	}

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
	if res.Receipt.EmissionCount() != 0 {
		t.Errorf("EmissionCount = %d, want 0 -- #420 says this detector cannot fire at shipped defaults, so a non-zero replay means the arithmetic moved", res.Receipt.EmissionCount())
	}

	// And the finding replay makes visible that #420's issue body does
	// not: loosening the multiplier all the way to 1.05 still produces
	// nothing. The multiplier is not the only thing binding -- the
	// z-score floor (emaMinZ) binds independently, because a baseline
	// that folds in every reading tracks the rate closely enough that
	// the deviation never reaches two standard deviations either.
	//
	// That matters for whoever takes #420 on: it rules out "retune the
	// multiplier" as a sufficient fix on its own, and points at the two
	// remedies #420 lists that change the *cadence* of the fold-in
	// rather than the thresholds around it. Pinned here rather than left
	// as a note, because a future change that made a multiplier sweep
	// start working would be exactly the signal that the cadence
	// question had been answered.
	res2, err := d.Replay(fakeCorpus{events: events}, Params{"baselineMultiplier": 1.05})
	if err != nil {
		t.Fatalf("Replay (candidate): %v", err)
	}
	if res2.Receipt == nil {
		t.Fatalf("expected a receipt for the candidate sweep, got %+v", res2)
	}
	if res2.Receipt.EmissionCount() != 0 {
		t.Errorf("EmissionCount = %d with baselineMultiplier=1.05, want 0 -- if a multiplier sweep now fires, the per-event fold-in cadence has changed and #420's analysis needs revisiting", res2.Receipt.EmissionCount())
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
