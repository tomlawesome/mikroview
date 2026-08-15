// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file holds two generations of characterization coverage.
//
// The first three tests below are the original, small set written for
// the countRing/distinctRing migration (issue #76): not exhaustive
// re-tests of every detector, just enough to confirm a few detectors
// still fire in the same general shape at realistic, non-test-shrunk
// config scale, and to call out one deliberate divergence from a
// literal reading of that migration's plan. Left unchanged.
//
// Everything below that section is #397's growth of this file into the
// full safety net the evaluation-engine ADR (docs/decisions/
// evaluation-engine.md) makes a precondition of porting internal/detect
// onto the shared engine chassis: every detector in AllDetectorNames, at
// DefaultConfig() scale, with its firing boundary, its emitted flag's
// fields, its re-fire/clear/count behaviour, its confidence scoring, its
// scope filtering and its enable/disable behaviour all pinned as
// executable fact rather than prose. Nothing here changes product
// behaviour -- see the ADR's "Migration" and "Costs, stated" sections
// for why this has to be true before the port, not after.
//
// Two conventions, stated once here rather than repeated at every call
// site:
//
//  1. "DefaultConfig() scale" means the detector *under test* keeps its
//     real DefaultConfig() thresholds/windows unchanged. Other
//     detectors sharing the same window state are sometimes given an
//     unreachable threshold (never a *smaller* one) purely to keep them
//     from co-firing on the same traffic and confusing the assertion --
//     that is isolation, not shrinking.
//  2. Every detector in this package computes its firing boundary from
//     either a plain integer threshold (overshootConfidence) or an
//     EMA baseline (emaConfidence/emaZScore -- confidence.go/
//     ema_confidence.go). The former's Detail strings are entirely
//     integer/duration formatting, so they are pinned byte-for-byte.
//     The latter's Detail strings embed a %.1f-formatted baseline and
//     z-score whose exact digits are a function of this test's own
//     chosen input sequence, not a product contract -- those are pinned
//     by exact template wording plus an integer/count field (still
//     byte-for-byte on the part that would actually change if the
//     wording changed) and a bounds/format check on the floating part.
//     Where a scenario collapses the EMA's variance to exactly zero
//     (a perfectly constant warm-up feed -- see emaZScore's stddev==0
//     branch), the resulting confidence is a deterministic integer and
//     is pinned exactly.

// TestCharacterizationPortScanFiresAtDefaultConfigScale exercises
// observeScanAndSpike's port-scan half with DefaultConfig's real
// PortScanWindow/PortScanThreshold (60s/15) rather than a test-shrunk
// config, spreading events across the full window so the migration
// touches most of the ring's 60 one-second buckets (bucketSpanFor(60s)
// == minBucketSpan == 1s) -- the shape a real slow-burst scan produces.
func TestCharacterizationPortScanFiresAtDefaultConfigScale(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 1000 // isolate port_scan
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for port := 1; port < cfg.PortScanThreshold; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*3*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below threshold, got %+v", fs.List())
	}

	d.Observe(evt("203.0.113.9", cfg.PortScanThreshold, now.Add(time.Duration(cfg.PortScanThreshold)*3*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypePortScan || list[0].Target != "203.0.113.9" {
		t.Fatalf("expected a port_scan flag at threshold, got %+v", list)
	}
}

// TestCharacterizationLowSlowScanFiresAtDefaultWindowScale runs
// observeLowSlowScan's three rings (ports/hosts/drops) at
// DefaultConfig's real 3-hour LowSlowScanWindow, where
// bucketSpanFor(3h) == 3m -- a much coarser bucket than the port-scan
// test above, exercising the case this migration cares most about
// (hours-scale windows that would be expensive to linear-rescan on
// every event).
func TestCharacterizationLowSlowScanFiresAtDefaultWindowScale(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	cfg.RepeatedDropsThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	// One paced, mostly-refused attempt every ~13 minutes -- comfortably
	// spaced past LowSlowScanMinObservation (45m) and past a 3-minute
	// bucket span by the time enough samples accumulate to clear the
	// port/host breadth thresholds (8/5).
	feedPacedScan(d, "203.0.113.9", cfg.LowSlowScanPortThreshold+2, 13*time.Minute, store.ActionDrop, t0)

	if findLowSlowFlag(fs) == nil {
		t.Fatalf("expected a low_slow_scan flag at default-config scale, got %+v", fs.List())
	}
}

// TestCharacterizationDistributedBruteForceRequiresDistinctSources
// documents a deliberate divergence from the migration plan's summary
// table, which listed "countRing per key" for
// observeDistributedBruteForce. The detector's entire point is
// *distinct* source IPs hammering one port (as opposed to
// critical-port's "one source hitting it repeatedly") -- a countRing
// only tracks a raw event count, so swapping in one there would make a
// single source's retries alone cross the threshold, collapsing the
// distinction the detector exists to draw. portSources therefore uses
// distinctRing[string] instead (see distributed_brute_force.go), which
// is what TestDistributedBruteForceIgnoresRepeatsFromSameSource in
// distributed_brute_force_test.go already pins down; this test re-states
// the same guarantee here, next to the rest of this migration's
// characterization coverage, for anyone comparing this change against
// the plan's table.
func TestCharacterizationDistributedBruteForceRequiresDistinctSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 5
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	// 20 repeats from a single source must never cross a threshold of 5
	// distinct sources.
	for i := 0; i < 20; i++ {
		d.Observe(evt("198.51.100.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected repeats from one source alone to never flag, got %+v", fs.List())
	}

	// The 5th distinct source crosses it.
	for i := 0; i < 5; i++ {
		d.Observe(evt(fmt.Sprintf("198.51.100.%d", i+2), 22, now))
	}
	if len(fs.List()) != 1 {
		t.Fatalf("expected 5 distinct sources to flag, got %+v", fs.List())
	}
}

// ============================================================================
// #397: full-suite characterization at DefaultConfig() scale.
// ============================================================================

// flagOfType returns the one flag of type typ in fs, or nil. Fails the
// test if there is more than one, since every scenario below is built
// so at most one target/type pair is ever in play.
func flagOfType(t *testing.T, fs *flags.Store, typ flags.Type) *flags.Flag {
	t.Helper()
	var found *flags.Flag
	for _, f := range fs.List() {
		f := f
		if f.Type == typ {
			if found != nil {
				t.Fatalf("expected at most one %s flag, got at least two: %+v", typ, fs.List())
			}
			found = &f
		}
	}
	return found
}

// countTypedFlags counts how many flags of typ are currently in fs
// (active or cleared) -- for tests that deliberately produce more than
// one (the clear+revive step, mid-transition).
func countTypedFlags(fs *flags.Store, typ flags.Type) int {
	n := 0
	for _, f := range fs.List() {
		if f.Type == typ {
			n++
		}
	}
	return n
}

// isZeroEvidence reports whether ev is the zero value -- flags.Evidence
// contains slice fields, so it isn't comparable with ==.
func isZeroEvidence(ev flags.Evidence) bool {
	return len(ev.Ports) == 0 && len(ev.Hosts) == 0 && ev.NAT == nil
}

// pub3/lan3 build IPs with a fixed-width final octet (100-125) so
// sort.Strings' lexical order matches numeric order -- makes an evidence
// list's expected sorted/capped content predictable by construction
// rather than needing net.ParseIP-unfriendly zero-padding (Go's
// net.ParseIP rejects leading zeros in a dotted-quad octet).
func pub3(n int) string { return fmt.Sprintf("203.0.113.%d", 100+n) }
func lan3(n int) string { return fmt.Sprintf("192.168.2.%d", 100+n) }

// ---------------------------------------------------------------------------
// 1. port_scan
// ---------------------------------------------------------------------------

// TestCharacterizationPortScan_FieldsRefireClearRevive pins port_scan's
// firing boundary at DefaultConfig's real threshold/window (15/60s), the
// exact shape of the flag it raises, and its re-fire/clear/revive
// behaviour across a second crossing.
func TestCharacterizationPortScan_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // PortScanThreshold=15, PortScanWindow=60s
	d, fs := newTestDetector(t, cfg)
	ip := "203.0.113.9"
	t0 := time.Now()

	// 14 distinct ports: must not fire.
	for port := 1; port <= 14; port++ {
		d.Observe(evtCountry(ip, "DE", port, t0.Add(time.Duration(port-1)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypePortScan); got != nil {
		t.Fatalf("expected no flag at 14 distinct ports, got %+v", got)
	}

	// The 15th distinct port crosses the threshold.
	d.Observe(evtCountry(ip, "DE", 15, t0.Add(14*time.Second)))
	f := flagOfType(t, fs, flags.TypePortScan)
	if f == nil {
		t.Fatal("expected a port_scan flag at exactly 15 distinct ports")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if want := "15 distinct destination ports in 1m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "DE" {
		t.Errorf("Country = %q, want DE", f.Country)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}
	wantPorts := make([]int, 15)
	for i := range wantPorts {
		wantPorts[i] = i + 1
	}
	if fmt.Sprint(f.Evidence.Ports) != fmt.Sprint(wantPorts) {
		t.Errorf("Evidence.Ports = %v, want %v", f.Evidence.Ports, wantPorts)
	}
	if f.Count != 1 {
		t.Errorf("Count = %d, want 1", f.Count)
	}

	// Re-fire: a 16th distinct port within the same window updates the
	// flag in place (Count increments, still one flag, confidence tracks
	// the new overshoot).
	d.Observe(evtCountry(ip, "DE", 16, t0.Add(15*time.Second)))
	f2 := flagOfType(t, fs, flags.TypePortScan)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 3 {
		t.Errorf("Confidence after re-fire = %v, want 3 (overshootConfidence(16,15))", f2.Confidence)
	}

	// Clear it, then feed a 17th distinct port still inside the same
	// window: the flag revives -- isNew again, Count resets to 1,
	// FirstSeen moves to the reviving event's time.
	if !fs.Clear(f2.ID, t0.Add(15500*time.Millisecond)) {
		t.Fatal("expected Clear to succeed on the active flag")
	}
	reviveAt := t0.Add(16 * time.Second)
	d.Observe(evtCountry(ip, "DE", 17, reviveAt))
	f3 := flagOfType(t, fs, flags.TypePortScan)
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
	// The ring itself never forgot the earlier ports -- Add doesn't know
	// about flags.Store's clear state -- so the revived episode's count
	// (and Detail) reflects all 17 distinct ports still inside the
	// window, not just the one revival event.
	if want := "17 distinct destination ports in 1m0s"; f3.Detail != want {
		t.Errorf("Detail after revival = %q, want %q", f3.Detail, want)
	}
	if f3.Confidence == nil || *f3.Confidence != 7 {
		t.Errorf("Confidence after revival = %v, want 7 (overshootConfidence(17,15))", f3.Confidence)
	}
	if countTypedFlags(fs, flags.TypePortScan) != 1 {
		t.Errorf("expected exactly one port_scan flag (revival updates in place, not a second one), got %d", countTypedFlags(fs, flags.TypePortScan))
	}
}

// ---------------------------------------------------------------------------
// 2. activity_spike
// ---------------------------------------------------------------------------

// indexOf is strings.Index without importing strings solely for one call
// site -- kept local and tiny.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// assertFloatSigmaTail asserts that s is exactly a %.1f-formatted
// (possibly multi-digit) float followed by "σ" and wantTail -- the
// shape every EMA-derived Detail string in this package ends in. Kept
// as one shared helper (rather than one regex literal per call site)
// so the shape itself, not just its digits, is pinned consistently.
func assertFloatSigmaTail(t *testing.T, s, wantTail string) {
	t.Helper()
	re := regexp.MustCompile(`^-?\d+\.\d+σ` + regexp.QuoteMeta(wantTail) + `$`)
	if !re.MatchString(s) {
		t.Errorf("tail = %q, want to match ^-?\\d+\\.\\d+σ%s$", s, wantTail)
	}
}

// TestCharacterizationActivitySpike_ContinuousRampNeverFiresAtDefaultConfig
// is a genuine surprise this characterization pass found, not a bug this
// issue fixes: at DefaultConfig's real values (ActivitySpikeThreshold=200,
// HostActivityMultiplier=3, emaAlpha=0.02), a single source's traffic
// ramping continuously up through Observe can never actually raise
// activity_spike, no matter how far past the threshold it goes.
//
// Why: checkHostActivityBaseline runs on *every* event, not once per
// window, and unconditionally folds the current reading into the EMA
// baseline immediately afterwards (see host_baseline.go). Because
// spikeCount only ever changes by +1 per call (Observe adds exactly one
// event to the window before checkHostActivityBaseline reads it), the
// gap between the live rate and the baseline chasing it is bounded by
// the EMA's own lag ceiling, 1/emaAlpha = 50 -- provably, however long
// the ramp continues (see emaUpdate's recurrence: the lag between rate
// and baseline for a unit-slope input converges to 1/alpha and never
// exceeds it). So by the time the absolute floor (200) is reached, the
// baseline is at best ~150 -- three times that is >= 450, far past any
// rate a live window can be reporting at that same moment. The 2000-event
// ramp below is well past the threshold and still never fires, which is
// the point: this is not a slow warm-up artifact, it is a structural
// ceiling that holds for any single-episode ramp shape.
//
// This is worth flagging, not fixing here: it means the shipped default
// (HostActivityMultiplier=3 alongside ActivitySpikeThreshold=200) makes
// this detector effectively unfireable via the exact "one source's
// traffic climbing steadily" scenario its own doc comment describes as
// the target. See this file's companion test below, which pins the
// firing boundary/fields/confidence formula by calling
// checkHostActivityBaseline directly (as the pre-existing
// TestActivitySpikeNeverFiresBeforeMinimumSampleFloor already does) --
// the only way to reach that state at all with these real thresholds,
// since Observe's ring can never present it with a same-call jump larger
// than 1.
func TestCharacterizationActivitySpike_ContinuousRampNeverFiresAtDefaultConfig(t *testing.T) {
	cfg := DefaultConfig() // ActivitySpikeThreshold=200, HostActivityMultiplier=3, WarmupSamples=20
	d, fs := newTestDetector(t, cfg)
	ip := "198.51.100.4"
	t0 := time.Now()

	// 2000 events on one port (ten times the absolute floor), spaced
	// closely enough to stay inside one 60s window.
	for i := 0; i < 2000; i++ {
		d.Observe(evt(ip, 443, t0.Add(time.Duration(i)*10*time.Millisecond)))
	}
	if got := flagOfType(t, fs, flags.TypeActivitySpike); got != nil {
		t.Fatalf("expected activity_spike to never fire from a continuous single-source ramp at DefaultConfig, got %+v", got)
	}
	w := d.perSource[ip]
	if gap := 2000 - w.baseline; gap < 45 || gap > 55 {
		t.Errorf("baseline/rate gap = %.2f, want close to the 1/emaAlpha=50 lag ceiling this test's doc comment describes", gap)
	}
}

// TestCharacterizationActivitySpike_FieldsRefireClearRevive pins the
// boundary/fields/confidence/re-fire/clear/revive behaviour of
// checkHostActivityBaseline itself, called directly (bypassing Observe's
// ring -- see the companion test above for why that ring can never
// actually present these values at DefaultConfig's real thresholds).
// This mirrors the pre-existing direct-call pattern
// TestActivitySpikeNeverFiresBeforeMinimumSampleFloor/
// TestActivitySpikeStillFiresWhenWarmupSamplesBelowFloor already use in
// detect_test.go. Feeding a constant currentRate=1 during warm-up keeps
// the EMA's variance at exactly zero (see emaZScore's stddev==0 branch),
// so the boundary values below are fully deterministic.
func TestCharacterizationActivitySpike_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // ActivitySpikeThreshold=200, HostActivityMultiplier=3, WarmupSamples=20
	ip := "198.51.100.4"
	now := time.Now()

	warm := func(d *Detector) *sourceWindow {
		w := &sourceWindow{}
		for i := 0; i < 25; i++ {
			d.checkHostActivityBaseline(w, ip, "FR", "", 1, now.Add(time.Duration(i)*time.Second))
		}
		return w
	}

	// The below-floor probe (rate=199) uses its own Detector: like every
	// other EMA-based detector in this file, checkHostActivityBaseline
	// unconditionally folds its reading into the baseline afterwards, so
	// probing on the same instance that goes on to pin the rate=200
	// boundary would perturb the zero-variance baseline that pin depends
	// on.
	probe, probeFS := newTestDetector(t, cfg)
	pw := warm(probe)
	probe.checkHostActivityBaseline(pw, ip, "FR", "", 199, now.Add(25*time.Second))
	if got := flagOfType(t, probeFS, flags.TypeActivitySpike); got != nil {
		t.Fatalf("expected no flag at rate=199 (ActivitySpikeThreshold=200 is an absolute floor), got %+v", got)
	}

	d, fs := newTestDetector(t, cfg)
	w := warm(d)
	if got := flagOfType(t, fs, flags.TypeActivitySpike); got != nil {
		t.Fatalf("expected no flag from the steady warm-up phase, got %+v", got)
	}

	d.checkHostActivityBaseline(w, ip, "FR", "", 200, now.Add(25*time.Second))
	f := flagOfType(t, fs, flags.TypeActivitySpike)
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
	if !isZeroEvidence(f.Evidence) {
		t.Errorf("Evidence = %+v, want the zero value (activity_spike carries no structured evidence)", f.Evidence)
	}
	if f.Confidence == nil || *f.Confidence != 100 {
		t.Errorf("Confidence at the boundary = %v, want 100 (zero-variance warm-up collapses emaZScore to its capped ceiling)", f.Confidence)
	}
	if f.Count != 1 {
		t.Errorf("Count = %d, want 1", f.Count)
	}

	// Re-fire: state past this point is no longer variance-zero, so only
	// structural properties are asserted (see this file's header comment).
	d.checkHostActivityBaseline(w, ip, "FR", "", 200, now.Add(27*time.Second))
	f2 := flagOfType(t, fs, flags.TypeActivitySpike)
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
	d.checkHostActivityBaseline(w, ip, "FR", "", 200, now.Add(29*time.Second))
	f3 := flagOfType(t, fs, flags.TypeActivitySpike)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if countTypedFlags(fs, flags.TypeActivitySpike) != 1 {
		t.Errorf("expected exactly one activity_spike flag, got %d", countTypedFlags(fs, flags.TypeActivitySpike))
	}
}

// ---------------------------------------------------------------------------
// 3. critical_port
// ---------------------------------------------------------------------------

// TestCharacterizationCriticalPort_FieldsRefireClearRevive pins
// critical_port's boundary at DefaultConfig's real 5-attempts/5-minute
// threshold against port 22 (one of DefaultConfig's real CriticalPorts).
func TestCharacterizationCriticalPort_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // CriticalPortThreshold=5, CriticalPortWindow=5m, CriticalPorts includes 22
	d, fs := newTestDetector(t, cfg)
	ip := "198.51.100.4"
	t0 := time.Now()

	for i := 0; i < 4; i++ {
		d.Observe(evtCountry(ip, "RU", 22, t0.Add(time.Duration(i)*30*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got != nil {
		t.Fatalf("expected no flag at 4 attempts, got %+v", got)
	}

	d.Observe(evtCountry(ip, "RU", 22, t0.Add(4*30*time.Second)))
	f := flagOfType(t, fs, flags.TypeCriticalPort)
	if f == nil {
		t.Fatal("expected a flag at exactly 5 attempts")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if want := "5 attempts against port 22 in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "RU" {
		t.Errorf("Country = %q, want RU", f.Country)
	}
	if !isZeroEvidence(f.Evidence) {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}

	// Re-fire.
	d.Observe(evtCountry(ip, "RU", 22, t0.Add(5*30*time.Second)))
	f2 := flagOfType(t, fs, flags.TypeCriticalPort)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 10 {
		t.Errorf("Confidence after re-fire = %v, want 10 (overshootConfidence(6,5))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(6*30*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	reviveAt := t0.Add(7 * 30 * time.Second)
	d.Observe(evtCountry(ip, "RU", 22, reviveAt))
	f3 := flagOfType(t, fs, flags.TypeCriticalPort)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if !f3.FirstSeen.Equal(reviveAt) {
		t.Errorf("FirstSeen after revival = %v, want %v", f3.FirstSeen, reviveAt)
	}
	if want := "7 attempts against port 22 in 5m0s"; f3.Detail != want {
		t.Errorf("Detail after revival = %q, want %q", f3.Detail, want)
	}
	if f3.Confidence == nil || *f3.Confidence != 20 {
		t.Errorf("Confidence after revival = %v, want 20 (overshootConfidence(7,5))", f3.Confidence)
	}
}

// TestCharacterizationCriticalPort_DetailNamesOnlyTheLastPort pins
// #379's known-wrong behaviour: criticalHits is keyed by source IP only
// (see detect.go's criticalHits map), so the count that crosses the
// threshold can span attempts against *several different* critical
// ports from the same source -- but the Detail string names only
// e.DstPort, the single port of the triggering event. This test feeds
// attempts against two different critical ports from one source and
// pins today's Detail, which names only the last one touched even
// though the count includes both. #379 is expected to fix this
// (aggregating per-port or naming the set touched); when it does, this
// pin should be *updated* to match the corrected wording, not deleted
// to make the diff quieter -- the point of pinning a known-wrong
// behaviour is so the fix shows up as an intentional, reviewed change to
// this test, not a silent one.
func TestCharacterizationCriticalPort_DetailNamesOnlyTheLastPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CriticalPorts = []int{22, 23} // two distinct critical ports
	cfg.CriticalPortThreshold = 5
	d, fs := newTestDetector(t, cfg)
	ip := "198.51.100.7"
	t0 := time.Now()

	// 3 attempts against port 22, then 2 against port 23 -- 5 total
	// against the *source*, spanning two ports.
	for i := 0; i < 3; i++ {
		d.Observe(evt(ip, 22, t0.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 2; i++ {
		d.Observe(evt(ip, 23, t0.Add(time.Duration(3+i)*time.Second)))
	}

	f := flagOfType(t, fs, flags.TypeCriticalPort)
	if f == nil {
		t.Fatal("expected a flag once the combined count across both ports reaches the threshold")
	}
	// KNOWN-WRONG, pinned per #379: names only port 23 (the last event's
	// port), even though 3 of the 5 counted attempts were against port
	// 22.
	if want := "5 attempts against port 23 in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q (today's known-wrong single-port naming -- see #379)", f.Detail, want)
	}
}

// ---------------------------------------------------------------------------
// 4. distributed_brute_force
// ---------------------------------------------------------------------------

// TestCharacterizationDistributedBruteForce_FieldsRefireClearRevive
// pins distributed_brute_force's boundary at DefaultConfig's real
// 10-distinct-sources/5-minute threshold against a critical port.
func TestCharacterizationDistributedBruteForce_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // DistributedBruteForceThreshold=10, Window=5m
	d, fs := newTestDetector(t, cfg)
	t0 := time.Now()

	for i := 0; i < 9; i++ {
		d.Observe(evt(fmt.Sprintf("198.51.100.%d", 100+i), 22, t0.Add(time.Duration(i)*10*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeDistributedBruteForce); got != nil {
		t.Fatalf("expected no flag at 9 distinct sources, got %+v", got)
	}

	d.Observe(evt(fmt.Sprintf("198.51.100.%d", 100+9), 22, t0.Add(9*10*time.Second)))
	f := flagOfType(t, fs, flags.TypeDistributedBruteForce)
	if f == nil {
		t.Fatal("expected a flag at exactly 10 distinct sources")
	}
	if f.Target != "port 22" {
		t.Errorf("Target = %q, want %q", f.Target, "port 22")
	}
	if want := "10 distinct source IPs in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}
	wantHosts := make([]string, 10)
	for i := range wantHosts {
		wantHosts[i] = fmt.Sprintf("198.51.100.%d", 100+i)
	}
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(sortedStrings(wantHosts)) {
		t.Errorf("Evidence.Hosts = %v, want %v", f.Evidence.Hosts, sortedStrings(wantHosts))
	}

	// Re-fire: 11th distinct source.
	d.Observe(evt(fmt.Sprintf("198.51.100.%d", 110), 22, t0.Add(10*10*time.Second)))
	f2 := flagOfType(t, fs, flags.TypeDistributedBruteForce)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 5 {
		t.Errorf("Confidence after re-fire = %v, want 5 (overshootConfidence(11,10))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(11*10*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Observe(evt("198.51.100.111", 22, t0.Add(12*10*time.Second)))
	f3 := flagOfType(t, fs, flags.TypeDistributedBruteForce)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 10 {
		t.Errorf("Confidence after revival = %v, want 10 (overshootConfidence(12,10))", f3.Confidence)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 5. outbound_anomaly
// ---------------------------------------------------------------------------

// TestCharacterizationOutboundAnomaly_FieldsRefireClearRevive pins
// outbound_anomaly's boundary at DefaultConfig's real
// 25-distinct-destinations/5-minute threshold, including the
// maxEvidenceHosts=20 cap (evidence.go) on the flag's Evidence.Hosts.
func TestCharacterizationOutboundAnomaly_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // OutboundAnomalyThreshold=25, Window=5m
	d, fs := newTestDetector(t, cfg)
	src := "192.168.1.50" // LAN source
	t0 := time.Now()

	for i := 0; i < 24; i++ {
		d.Observe(store.Event{SrcIP: src, DstIP: pub3(i), DstPort: 443, ReceivedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	if got := flagOfType(t, fs, flags.TypeOutboundAnomaly); got != nil {
		t.Fatalf("expected no flag at 24 distinct external destinations, got %+v", got)
	}

	d.Observe(store.Event{SrcIP: src, DstIP: pub3(24), DstPort: 443, ReceivedAt: t0.Add(24 * time.Second)})
	f := flagOfType(t, fs, flags.TypeOutboundAnomaly)
	if f == nil {
		t.Fatal("expected a flag at exactly 25 distinct external destinations")
	}
	if f.Target != src {
		t.Errorf("Target = %q, want %q", f.Target, src)
	}
	if want := "25 distinct external destinations in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}
	if len(f.Evidence.Hosts) != 20 {
		t.Fatalf("Evidence.Hosts length = %d, want 20 (maxEvidenceHosts cap)", len(f.Evidence.Hosts))
	}
	wantAll := make([]string, 25)
	for i := range wantAll {
		wantAll[i] = pub3(i)
	}
	wantCapped := sortedStrings(wantAll)[:20]
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(wantCapped) {
		t.Errorf("Evidence.Hosts = %v, want %v (sorted, capped at 20)", f.Evidence.Hosts, wantCapped)
	}

	// Re-fire.
	d.Observe(store.Event{SrcIP: src, DstIP: pub3(25), DstPort: 443, ReceivedAt: t0.Add(25 * time.Second)})
	f2 := flagOfType(t, fs, flags.TypeOutboundAnomaly)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 2 {
		t.Errorf("Confidence after re-fire = %v, want 2 (overshootConfidence(26,25))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(26*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Observe(store.Event{SrcIP: src, DstIP: pub3(26), DstPort: 443, ReceivedAt: t0.Add(27 * time.Second)})
	f3 := flagOfType(t, fs, flags.TypeOutboundAnomaly)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 4 {
		t.Errorf("Confidence after revival = %v, want 4 (overshootConfidence(27,25))", f3.Confidence)
	}
}

// ---------------------------------------------------------------------------
// 6. internal_recon
// ---------------------------------------------------------------------------

// TestCharacterizationInternalRecon_FieldsRefireClearRevive pins
// internal_recon's boundary at DefaultConfig's real
// 10-distinct-destinations/60-second threshold.
func TestCharacterizationInternalRecon_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // InternalReconThreshold=10, Window=60s
	d, fs := newTestDetector(t, cfg)
	src := "192.168.1.50"
	t0 := time.Now()

	for i := 0; i < 9; i++ {
		d.Observe(store.Event{SrcIP: src, DstIP: lan3(i), DstPort: 445, ReceivedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	if got := flagOfType(t, fs, flags.TypeInternalRecon); got != nil {
		t.Fatalf("expected no flag at 9 distinct internal destinations, got %+v", got)
	}

	d.Observe(store.Event{SrcIP: src, DstIP: lan3(9), DstPort: 445, ReceivedAt: t0.Add(9 * time.Second)})
	f := flagOfType(t, fs, flags.TypeInternalRecon)
	if f == nil {
		t.Fatal("expected a flag at exactly 10 distinct internal destinations")
	}
	if f.Target != src {
		t.Errorf("Target = %q, want %q", f.Target, src)
	}
	if want := "10 distinct internal destinations in 1m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}
	wantHosts := make([]string, 10)
	for i := range wantHosts {
		wantHosts[i] = lan3(i)
	}
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(sortedStrings(wantHosts)) {
		t.Errorf("Evidence.Hosts = %v, want %v", f.Evidence.Hosts, sortedStrings(wantHosts))
	}

	// internal_recon's own AddWithDetail call ignores its isNew return
	// (see dest_spread.go -- no maybeCheckGroupReputation for this one),
	// but re-fire/clear/revival still go through flags.Store the same as
	// every other detector.
	d.Observe(store.Event{SrcIP: src, DstIP: lan3(10), DstPort: 445, ReceivedAt: t0.Add(10 * time.Second)})
	f2 := flagOfType(t, fs, flags.TypeInternalRecon)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 5 {
		t.Errorf("Confidence after re-fire = %v, want 5 (overshootConfidence(11,10))", f2.Confidence)
	}

	if !fs.Clear(f2.ID, t0.Add(11*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Observe(store.Event{SrcIP: src, DstIP: lan3(11), DstPort: 445, ReceivedAt: t0.Add(12 * time.Second)})
	f3 := flagOfType(t, fs, flags.TypeInternalRecon)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 10 {
		t.Errorf("Confidence after revival = %v, want 10 (overshootConfidence(12,10))", f3.Confidence)
	}
}

// ---------------------------------------------------------------------------
// 7. rule_spike
// ---------------------------------------------------------------------------

// primeRuleSpikeConstantBaseline feeds n ticks of exactly one event to
// rule at a fixed 65-second cadence (> the default 60s RuleSpikeWindow,
// so each tick reads a constant rate of 1/60 events/sec) -- collapses
// the EMA's variance to exactly zero, the same trick used above for
// activity_spike/global_spike, so the boundary event that follows has a
// deterministic confidence instead of one this test would have to
// hand-derive. Returns the time of the last warm-up tick.
func primeRuleSpikeConstantBaseline(d *Detector, rule string, n int, from time.Time) time.Time {
	tick := from
	for i := 0; i < n; i++ {
		d.Observe(ruleEvt(rule, tick))
		tick = tick.Add(65 * time.Second)
	}
	return tick.Add(-65 * time.Second)
}

// TestCharacterizationRuleSpike_FieldsRefireClearRevive pins rule_spike's
// boundary at DefaultConfig's real 5x-multiplier/0.2-events-per-sec/60s
// config. MinRate (12 events in the 60s window) is the binding
// constraint here, not the multiplier, since the primed baseline is
// tiny -- see primeRuleSpikeConstantBaseline's doc comment. Unlike
// activity_spike/global_spike's boundary pins above, this one cannot
// hold the EMA's variance at exactly zero right up to the boundary: to
// reach a window count of 12, the window must actually hold 11 prior
// hits, and every one of observeRuleRate's calls unconditionally folds
// its reading into the baseline immediately afterwards (see
// rule_spike.go) -- so the 11 hits leading up to the boundary event
// necessarily perturb it first. The Detail/Confidence values below are
// therefore captured from an actual run rather than hand-derived, the
// same way every %.1f-formatted EMA field elsewhere in this file is
// pinned (see this file's header comment).
func TestCharacterizationRuleSpike_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // RuleSpikeMultiplier=5, RuleSpikeMinRate=0.2, Window=60s, WarmupSamples=20
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	rule := "wan-in"

	probe, probeFS := newTestDetector(t, cfg)
	probeLast := primeRuleSpikeConstantBaseline(probe, rule, 25, time.Now())
	probeBurst := probeLast.Add(65 * time.Second)
	for i := 0; i < 11; i++ {
		probe.Observe(ruleEvt(rule, probeBurst.Add(time.Duration(i)*time.Second)))
	}
	if got := flagOfType(t, probeFS, flags.TypeRuleSpike); got != nil {
		t.Fatalf("expected no flag at 11 hits in the window (11/60=0.183 < MinRate 0.2), got %+v", got)
	}

	d, fs := newTestDetector(t, cfg)
	last := primeRuleSpikeConstantBaseline(d, rule, 25, time.Now())
	burstStart := last.Add(65 * time.Second)
	for i := 0; i < 11; i++ {
		d.Observe(ruleEvt(rule, burstStart.Add(time.Duration(i)*time.Second)))
	}
	d.Observe(ruleEvt(rule, burstStart.Add(11*time.Second)))
	f := flagOfType(t, fs, flags.TypeRuleSpike)
	if f == nil {
		t.Fatal("expected a flag at exactly 12 hits in the window (12/60=0.2 == MinRate)")
	}
	if f.Target != rule {
		t.Errorf("Target = %q, want %q", f.Target, rule)
	}
	wantPrefix := "0.2 hits/s vs a baseline of 0.0 for this rule (based on 20 samples, "
	if len(f.Detail) < len(wantPrefix) || f.Detail[:len(wantPrefix)] != wantPrefix {
		t.Errorf("Detail = %q, want prefix %q", f.Detail, wantPrefix)
	}
	assertFloatSigmaTail(t, f.Detail[len(wantPrefix):], " above normal)")
	if f.Confidence == nil || *f.Confidence <= 0 || *f.Confidence > 100 {
		t.Errorf("Confidence at the boundary = %v, want a value in (0, 100]", f.Confidence)
	}

	// Re-fire and clear/revive: only structural properties asserted from
	// here on -- the EMA's variance is no longer exactly zero once the
	// burst itself has updated it, so the float fields are no longer a
	// clean hand-derivable pin (see this file's header comment).
	d.Observe(ruleEvt(rule, burstStart.Add(12*time.Second)))
	f2 := flagOfType(t, fs, flags.TypeRuleSpike)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}

	if !fs.Clear(f2.ID, burstStart.Add(13*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	// The prior episode's own hits left the baseline too elevated for a
	// same-scale 12-hit burst to clear 5x-baseline again immediately
	// (the window is capped at DefaultConfig's real 60 buckets/60s, so a
	// continued burst plateaus rather than outrunning the baseline the
	// way it would with a taller window) -- so this settles the rule
	// back to a low, quiet baseline the same way the original warm-up
	// did (spaced ticks, constant rate), then repeats the same 12-hit
	// burst shape already proven to fire above. This is revival of the
	// same (Type, Target) flag, not a new one -- flags.Store dedupes by
	// (Type, Target), so the earlier episode's cleared record is what
	// gets revived.
	resettleLast := primeRuleSpikeConstantBaseline(d, rule, 25, burstStart.Add(time.Minute))
	revive := resettleLast.Add(65 * time.Second)
	for i := 0; i < 12; i++ {
		d.Observe(ruleEvt(rule, revive.Add(time.Duration(i)*time.Second)))
	}
	f3 := flagOfType(t, fs, flags.TypeRuleSpike)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// ---------------------------------------------------------------------------
// 8. repeated_drops
// ---------------------------------------------------------------------------

// TestCharacterizationRepeatedDrops_FieldsRefireClearRevive pins
// repeated_drops' boundary at DefaultConfig's real
// 10-attempts/15-minute threshold.
func TestCharacterizationRepeatedDrops_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // RepeatedDropsThreshold=10, Window=15m
	d, fs := newTestDetector(t, cfg)
	src, dstIP, dstPort := "203.0.113.9", "192.168.1.1", 8080
	t0 := time.Now()

	dropEvt := func(at time.Time) store.Event {
		return store.Event{SrcIP: src, DstIP: dstIP, DstPort: dstPort, Action: store.ActionDrop, SrcCountry: "NL", ReceivedAt: at}
	}

	for i := 0; i < 9; i++ {
		d.Observe(dropEvt(t0.Add(time.Duration(i) * time.Minute)))
	}
	if got := flagOfType(t, fs, flags.TypeRepeatedDrops); got != nil {
		t.Fatalf("expected no flag at 9 attempts, got %+v", got)
	}

	d.Observe(dropEvt(t0.Add(9 * time.Minute)))
	f := flagOfType(t, fs, flags.TypeRepeatedDrops)
	if f == nil {
		t.Fatal("expected a flag at exactly 10 attempts")
	}
	wantTarget := "203.0.113.9 -> port 8080"
	if f.Target != wantTarget {
		t.Errorf("Target = %q, want %q", f.Target, wantTarget)
	}
	wantDetail := "10 attempts against 192.168.1.1:8080 dropped in 15m0s -- check whether this port is meant to be open"
	if f.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", f.Detail, wantDetail)
	}
	if f.Country != "NL" {
		t.Errorf("Country = %q, want NL", f.Country)
	}
	if f.Evidence.NAT != nil {
		t.Errorf("Evidence.NAT = %+v, want nil (no NAT fields set on the triggering event)", f.Evidence.NAT)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}

	// Re-fire.
	d.Observe(dropEvt(t0.Add(10 * time.Minute)))
	f2 := flagOfType(t, fs, flags.TypeRepeatedDrops)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 5 {
		t.Errorf("Confidence after re-fire = %v, want 5 (overshootConfidence(11,10))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(11*time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Observe(dropEvt(t0.Add(12 * time.Minute)))
	f3 := flagOfType(t, fs, flags.TypeRepeatedDrops)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 10 {
		t.Errorf("Confidence after revival = %v, want 10 (overshootConfidence(12,10))", f3.Confidence)
	}
}

// TestCharacterizationRepeatedDrops_EvidenceCapturesNAT pins the
// Evidence.NAT branch (only ever exercised when the *triggering* event
// itself carries NAT fields -- see observeRepeatedDrops).
func TestCharacterizationRepeatedDrops_EvidenceCapturesNAT(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 2
	d, fs := newTestDetector(t, cfg)
	t0 := time.Now()

	d.Observe(store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: t0})
	d.Observe(store.Event{
		SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop,
		NatIP: "10.0.0.5", NatPort: 51820, NatRaw: "dst-nat(10.0.0.5:51820)",
		ReceivedAt: t0.Add(time.Minute),
	})

	f := flagOfType(t, fs, flags.TypeRepeatedDrops)
	if f == nil {
		t.Fatal("expected a flag")
	}
	if f.Evidence.NAT == nil {
		t.Fatal("expected Evidence.NAT to be set")
	}
	if f.Evidence.NAT.IP != "10.0.0.5" || f.Evidence.NAT.Port != 51820 || f.Evidence.NAT.Raw != "dst-nat(10.0.0.5:51820)" {
		t.Errorf("Evidence.NAT = %+v, want {IP:10.0.0.5 Port:51820 Raw:dst-nat(10.0.0.5:51820)}", f.Evidence.NAT)
	}
}

// TestCharacterizationRepeatedDrops_DetailNamesOnlyTheLastDestination
// pins #379's known-wrong behaviour on repeated_drops' side: the
// (srcIP, dstPort) key does not include dstIP (see dropPairKey), so the
// same source repeatedly hitting one port across *several different*
// destination IPs collapses into a single counter -- but the Detail
// string names only e.DstIP, the triggering event's destination.
// Pinned as today's behaviour; #379 is expected to change it (naming the
// set of destinations, or keying per-destination), and this pin should
// be *updated*, not deleted, when that lands.
func TestCharacterizationRepeatedDrops_DetailNamesOnlyTheLastDestination(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 4
	d, fs := newTestDetector(t, cfg)
	t0 := time.Now()

	dests := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4"}
	for i, dst := range dests {
		d.Observe(store.Event{SrcIP: "203.0.113.9", DstIP: dst, DstPort: 8080, Action: store.ActionDrop, ReceivedAt: t0.Add(time.Duration(i) * time.Minute)})
	}

	f := flagOfType(t, fs, flags.TypeRepeatedDrops)
	if f == nil {
		t.Fatal("expected a flag once the combined count across all four destinations reaches the threshold")
	}
	// KNOWN-WRONG, pinned per #379: names only 192.168.1.4 (the last
	// event's destination), even though 3 of the 4 counted attempts
	// targeted other IPs.
	want := "4 attempts against 192.168.1.4:8080 dropped in 15m0s -- check whether this port is meant to be open"
	if f.Detail != want {
		t.Errorf("Detail = %q, want %q (today's known-wrong single-destination naming -- see #379)", f.Detail, want)
	}
}

// ---------------------------------------------------------------------------
// 9. low_slow_scan
// ---------------------------------------------------------------------------

// TestCharacterizationLowSlowScan_FieldsRefireClearRevive pins
// low_slow_scan's boundary at DefaultConfig's real thresholds
// (PortThreshold=8, HostThreshold=5, MinObservation=45m,
// DropRatio=0.8, BaselineMultiplier=3, Window=3h). feedPacedScan (see
// low_slow_scan_test.go, same package) gives each step a distinct port
// AND a distinct host, so port/host breadth grow together and
// PortThreshold(8) -- the higher of the two -- is what actually gates
// the boundary. overshootConfidence(portCount, PortThreshold) is 0 right
// at that boundary and is provably the minimum of the four axis
// confidences combined (see observeLowSlowScan), which is what makes
// the boundary event's Confidence a clean, hand-verifiable 0 despite the
// EMA baseline/z-score terms not being round numbers.
func TestCharacterizationLowSlowScan_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 100000
	cfg.ActivitySpikeThreshold = 100000
	cfg.RepeatedDropsThreshold = 100000
	d, fs := newTestDetector(t, cfg)
	ip := "203.0.113.9"
	t0 := time.Now()

	// 7 steps: portCount=hostCount=7 < PortThreshold(8) -- breadth not
	// cleared yet, so no flag regardless of anything else.
	feedPacedScan(d, ip, 7, 10*time.Minute, store.ActionDrop, t0)
	if got := flagOfType(t, fs, flags.TypeLowSlowScan); got != nil {
		t.Fatalf("expected no flag at portCount=hostCount=7, got %+v", got)
	}

	// The 8th step clears every axis at once.
	last := t0.Add(7 * 10 * time.Minute)
	d.Observe(lowSlowEvt(ip, "192.168.50.8", 10007, store.ActionDrop, last))
	f := flagOfType(t, fs, flags.TypeLowSlowScan)
	if f == nil {
		t.Fatal("expected a flag at exactly portCount=hostCount=8")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence at the boundary = %v, want 0 (portConfidence=overshootConfidence(8,8)=0 is the minimum of the four axis confidences)", f.Confidence)
	}
	wantDetailPrefix := "8 distinct ports, 8 distinct hosts over 3h0m0s (100% drop/reject, "
	if len(f.Detail) < len(wantDetailPrefix) || f.Detail[:len(wantDetailPrefix)] != wantDetailPrefix {
		t.Errorf("Detail = %q, want prefix %q", f.Detail, wantDetailPrefix)
	}
	assertFloatSigmaTail(t, f.Detail[len(wantDetailPrefix):], " above this source's normal breadth)")
	if len(f.Evidence.Ports) != 8 || len(f.Evidence.Hosts) != 8 {
		t.Errorf("Evidence = %+v, want 8 ports and 8 hosts", f.Evidence)
	}

	// Re-fire.
	d.Observe(lowSlowEvt(ip, "192.168.50.9", 10008, store.ActionDrop, t0.Add(8*10*time.Minute)))
	f2 := flagOfType(t, fs, flags.TypeLowSlowScan)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(9*10*time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Observe(lowSlowEvt(ip, "192.168.50.10", 10009, store.ActionDrop, t0.Add(10*10*time.Minute)))
	f3 := flagOfType(t, fs, flags.TypeLowSlowScan)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// ---------------------------------------------------------------------------
// 10. off_hours_activity
// ---------------------------------------------------------------------------

// TestCharacterizationOffHours_FieldsRefireClearRevive pins
// off_hours_activity's boundary at DefaultConfig's real
// OffHoursMinSampleDays(14)/MinCount(5)/window(23:00-06:00). 14 distinct
// prior days of one event/day at 03:00 establish the baseline; day 15's
// events then arrive one at a time, and since checkOffHoursActivity only
// folds a day's baseline contribution on the *next* day's first event
// (see off_hours.go), the baseline/variance are fixed for the whole of
// day 15 -- count is the only thing moving, so the search below finds
// day 15's exact boundary event deterministically rather than this test
// hand-deriving it.
func TestCharacterizationOffHours_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // OffHoursMinSampleDays=14, OffHoursMinCount=5, window 23:00-06:00
	d, fs := newTestDetector(t, cfg)
	ip := "198.51.100.30"

	for i := 0; i < 14; i++ {
		d.Observe(evtCountry(ip, "GB", 100, offHoursAt(2024, time.March, 1+i, 3)))
	}
	if got := flagOfType(t, fs, flags.TypeOffHoursActivity); got != nil {
		t.Fatalf("expected no flag from the 14-day steady warm-up, got %+v", got)
	}

	day15 := offHoursAt(2024, time.March, 15, 3)
	var boundary int
	for i := 1; i <= cfg.OffHoursMinCount+5; i++ {
		d.Observe(evtCountry(ip, "GB", 100+i, day15.Add(time.Duration(i)*time.Millisecond)))
		if flagOfType(t, fs, flags.TypeOffHoursActivity) != nil {
			boundary = i
			break
		}
	}
	if boundary == 0 {
		t.Fatalf("expected a flag to fire within %d events on day 15, got none; flags=%+v", cfg.OffHoursMinCount+5, fs.List())
	}
	if boundary != cfg.OffHoursMinCount {
		t.Errorf("boundary event = %d, want %d (OffHoursMinCount is the binding gate given this warm-up's tiny baseline)", boundary, cfg.OffHoursMinCount)
	}

	f := flagOfType(t, fs, flags.TypeOffHoursActivity)
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if f.Country != "GB" {
		t.Errorf("Country = %q, want GB", f.Country)
	}
	wantPrefix := fmt.Sprintf("%d events at 03:00 vs a baseline of ", boundary)
	if len(f.Detail) < len(wantPrefix) || f.Detail[:len(wantPrefix)] != wantPrefix {
		t.Errorf("Detail = %q, want prefix %q", f.Detail, wantPrefix)
	}
	wantMid := " for this host at this hour (14 days of history, "
	if idx := indexOf(f.Detail, wantMid); idx < 0 {
		t.Errorf("Detail = %q, want to contain %q", f.Detail, wantMid)
	} else {
		assertFloatSigmaTail(t, f.Detail[idx+len(wantMid):], " above normal)")
	}
	if f.Confidence == nil || *f.Confidence <= 0 || *f.Confidence > 100 {
		t.Errorf("Confidence = %v, want a value in (0, 100]", f.Confidence)
	}
	if !isZeroEvidence(f.Evidence) {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}

	// Re-fire: same day, count keeps climbing.
	d.Observe(evtCountry(ip, "GB", 200, day15.Add(time.Duration(boundary+1)*time.Millisecond)))
	f2 := flagOfType(t, fs, flags.TypeOffHoursActivity)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}

	// Clear + revive: a burst on a later day inside the window.
	if !fs.Clear(f2.ID, day15.Add(time.Hour)) {
		t.Fatal("expected Clear to succeed")
	}
	day16 := offHoursAt(2024, time.March, 16, 3)
	for i := 1; i <= boundary; i++ {
		d.Observe(evtCountry(ip, "GB", 300+i, day16.Add(time.Duration(i)*time.Millisecond)))
	}
	f3 := flagOfType(t, fs, flags.TypeOffHoursActivity)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active on day 16, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// ---------------------------------------------------------------------------
// 11. global_spike
// ---------------------------------------------------------------------------

// warmGlobalSpike feeds n constant readings of 1 eps to g -- primes
// baseline=1 and, because the reading never changes, keeps variance at
// exactly zero throughout (emaUpdate's diff term is 0 on every call
// after the first), the same trick used above for activity_spike/
// rule_spike. sampleCount caps at GlobalSpikeWarmupSamples(20) well
// before n=25 warm-up calls complete.
func warmGlobalSpike(g *GlobalSpikeDetector, n int, from time.Time) {
	for i := 0; i < n; i++ {
		g.Check(1, from.Add(time.Duration(i)*time.Second))
	}
}

// TestCharacterizationGlobalSpike_FieldsRefireClearRevive pins
// global_spike's boundary at DefaultConfig's real 4x-multiplier/5-EPS
// floor/20-sample-warmup config. Check(eps, now) takes eps directly with
// no window/ring behind it, so a constant warm-up feed collapses the
// EMA's variance to exactly zero -- here every field at the boundary is
// a clean, fully hand-derivable value. The below-MinEPS probe uses its
// own, separately-warmed detector: Check unconditionally folds every
// reading into the baseline afterwards (see global_spike.go), so probing
// eps=4 on the same instance that goes on to pin the eps=5 boundary
// would perturb the zero-variance baseline that pin depends on.
func TestCharacterizationGlobalSpike_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // GlobalSpikeMultiplier=4, GlobalSpikeMinEPS=5, WarmupSamples=20
	now := time.Now()

	probe, probeFS := newTestGlobalSpike(t, cfg)
	warmGlobalSpike(probe, 25, now)
	probe.Check(4, now.Add(25*time.Second)) // below MinEPS(5)
	if got := flagOfType(t, probeFS, flags.TypeGlobalSpike); got != nil {
		t.Fatalf("expected no flag at eps=4 (below GlobalSpikeMinEPS=5), got %+v", got)
	}

	g, fs := newTestGlobalSpike(t, cfg)
	warmGlobalSpike(g, 25, now)
	g.Check(5, now.Add(25*time.Second)) // MinEPS(5) and 5x baseline(1) both clear
	f := flagOfType(t, fs, flags.TypeGlobalSpike)
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
	if !isZeroEvidence(f.Evidence) {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}
	if f.Country != "" {
		t.Errorf("Country = %q, want empty (global_spike has no per-event source to attribute a country to)", f.Country)
	}

	// Re-fire.
	g.Check(5, now.Add(27*time.Second))
	f2 := flagOfType(t, fs, flags.TypeGlobalSpike)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, now.Add(28*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	g.Check(5, now.Add(29*time.Second))
	f3 := flagOfType(t, fs, flags.TypeGlobalSpike)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// ---------------------------------------------------------------------------
// 12. device_silence
// ---------------------------------------------------------------------------

// TestCharacterizationDeviceSilence_FieldsRefireClearRevive pins
// device_silence's boundary at DefaultConfig's real 15-minute
// DeviceStaleAfter (TestDeviceSilenceFiresExactlyAtTheThreshold in
// device_silence_test.go already pins the fire/no-fire boundary itself;
// this test adds the Detail/Evidence/Confidence field pin and the
// clear+revive step that file doesn't cover).
func TestCharacterizationDeviceSilence_FieldsRefireClearRevive(t *testing.T) {
	cfg := DefaultConfig() // DeviceStaleAfter=15m
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1", Configured: true,
			FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-15 * time.Minute), EventCount: 500},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)
	d.Check(now)

	f := flagOfType(t, fs, flags.TypeDeviceSilence)
	if f == nil {
		t.Fatal("expected a flag exactly at the 15-minute threshold")
	}
	if f.Target != "core" {
		t.Errorf("Target = %q, want %q", f.Target, "core")
	}
	wantDetail := "Core Router has sent no syslog for 15m0s, exceeding the 15m0s staleness threshold"
	if f.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", f.Detail, wantDetail)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}
	if !isZeroEvidence(f.Evidence) {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}

	// Re-fire: further past the threshold.
	laterDevices := fakeDeviceLister{
		{ID: "core", Name: "Core Router", Configured: true, LastSeen: now.Add(-15 * time.Minute)},
	}
	d2 := NewDeviceSilenceDetectorWithSettings(cfg, fs, AllEnabledSettingsStore(), laterDevices)
	d2.Check(now.Add(30 * time.Minute)) // elapsed=45m -> overshootConfidence(2700,900)=100
	f2 := flagOfType(t, fs, flags.TypeDeviceSilence)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 100 {
		t.Errorf("Confidence after re-fire = %v, want 100 (overshootConfidence(2700,900))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, now.Add(31*time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	d3 := NewDeviceSilenceDetectorWithSettings(cfg, fs, AllEnabledSettingsStore(), laterDevices)
	d3.Check(now.Add(32 * time.Minute))
	f3 := flagOfType(t, fs, flags.TypeDeviceSilence)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// ---------------------------------------------------------------------------
// Scope: one case per Settings.Scope axis, plus the #44 AND-together case.
// ---------------------------------------------------------------------------

// TestCharacterizationScope_HostsAllow pins the Hosts axis under
// ListModeAllow, at critical_port's real DefaultConfig scale.
func TestCharacterizationScope_HostsAllow(t *testing.T) {
	cfg := DefaultConfig()
	seed := DefaultSettingsMap()
	seed[DetectorCriticalPort] = Settings{Enabled: true, Scope: Scope{Hosts: []string{"198.51.100.4"}, HostsMode: ListModeAllow}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second))) // not on the allow list
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got != nil {
		t.Fatalf("expected a host outside the allow list to never flag, got %+v", got)
	}

	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.4", 22, now.Add(time.Duration(i)*30*time.Second))) // allow-listed
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got == nil {
		t.Fatal("expected the allow-listed host to still flag")
	}
}

// TestCharacterizationScope_HostsModeDeny pins the HostsMode axis under
// ListModeDeny, at port_scan's real DefaultConfig scale (15/60s).
func TestCharacterizationScope_HostsModeDeny(t *testing.T) {
	cfg := DefaultConfig()
	seed := DefaultSettingsMap()
	seed[DetectorPortScan] = Settings{Enabled: true, Scope: Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	for port := 1; port <= 20; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*time.Second))) // denied
	}
	if got := flagOfType(t, fs, flags.TypePortScan); got != nil {
		t.Fatalf("expected the denylisted host to never flag even at 20 distinct ports, got %+v", got)
	}

	for port := 1; port <= 15; port++ {
		d.Observe(evt("203.0.113.10", port, now.Add(time.Duration(port)*time.Second))) // not denied
	}
	if got := flagOfType(t, fs, flags.TypePortScan); got == nil {
		t.Fatal("expected a non-denylisted host to still flag at threshold")
	}
}

// TestCharacterizationScope_PortsAllow pins the Ports axis under
// ListModeAllow, at repeated_drops' real DefaultConfig scale (10/15m).
func TestCharacterizationScope_PortsAllow(t *testing.T) {
	cfg := DefaultConfig()
	seed := DefaultSettingsMap()
	seed[DetectorRepeatedDrops] = Settings{Enabled: true, Scope: Scope{Ports: []int{8080}, PortsMode: ListModeAllow}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	for i := 0; i < 10; i++ {
		d.Observe(store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 9090, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Minute)}) // not on the allow list
	}
	if got := flagOfType(t, fs, flags.TypeRepeatedDrops); got != nil {
		t.Fatalf("expected a non-allowed port to never flag even at 10 attempts, got %+v", got)
	}

	for i := 0; i < 10; i++ {
		d.Observe(store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Minute)}) // allow-listed
	}
	if got := flagOfType(t, fs, flags.TypeRepeatedDrops); got == nil {
		t.Fatal("expected the allow-listed port to still flag at threshold")
	}
}

// TestCharacterizationScope_PortsModeDeny pins the PortsMode axis under
// ListModeDeny, at critical_port's real DefaultConfig scale, restricting
// the *effective* subset of Config.CriticalPorts this scoped instance
// reacts to (Scope doc comment, settings.go).
func TestCharacterizationScope_PortsModeDeny(t *testing.T) {
	cfg := DefaultConfig() // CriticalPorts includes both 22 and 23
	seed := DefaultSettingsMap()
	seed[DetectorCriticalPort] = Settings{Enabled: true, Scope: Scope{Ports: []int{23}, PortsMode: ListModeDeny}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.4", 23, now.Add(time.Duration(i)*30*time.Second))) // denied port
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got != nil {
		t.Fatalf("expected the denylisted port to never count toward the threshold, got %+v", got)
	}

	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second))) // not denied
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got == nil {
		t.Fatal("expected a non-denylisted critical port to still flag at threshold")
	}
}

// TestCharacterizationScope_Classification pins the Classification axis,
// at port_scan's real DefaultConfig scale (15/60s) -- per settings.go's
// per-detector field usage table, PortScan's Hosts/HostsMode +
// Classification restrict which source IPs are tracked at all.
func TestCharacterizationScope_Classification(t *testing.T) {
	cfg := DefaultConfig()
	seed := DefaultSettingsMap()
	seed[DetectorPortScan] = Settings{Enabled: true, Scope: Scope{Classification: store.ScopeInternal}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	// An external source: never tracked at all under Classification=Internal,
	// no matter how many distinct ports it touches.
	for port := 1; port <= 20; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypePortScan); got != nil {
		t.Fatalf("expected an external source to never flag under Classification=Internal, got %+v", got)
	}

	// An internal (LAN) source still flags normally at threshold.
	for port := 1; port <= 15; port++ {
		d.Observe(evt("192.168.1.77", port, now.Add(time.Duration(port)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypePortScan); got == nil {
		t.Fatal("expected an internal source to still flag under Classification=Internal")
	}
}

// TestCharacterizationScope_RulesAllow pins the Rules axis under
// ListModeAllow, at rule_spike's real DefaultConfig scale.
func TestCharacterizationScope_RulesAllow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	seed := DefaultSettingsMap()
	seed[DetectorRuleSpike] = Settings{Enabled: true, Scope: Scope{Rules: []string{"wan-in"}, RulesMode: ListModeAllow}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	// "other-rule" is not on the allow list -- never even tracked,
	// regardless of volume.
	last := primeRuleSpikeConstantBaseline(d, "other-rule", 5, time.Now())
	burst := last.Add(65 * time.Second)
	for i := 0; i < 20; i++ {
		d.Observe(ruleEvt("other-rule", burst.Add(time.Duration(i)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeRuleSpike); got != nil {
		t.Fatalf("expected a non-allowed rule to never flag, got %+v", got)
	}

	// "wan-in" is allow-listed and behaves normally.
	last2 := primeRuleSpikeConstantBaseline(d, "wan-in", 25, burst.Add(time.Minute))
	burst2 := last2.Add(65 * time.Second)
	for i := 0; i < 12; i++ {
		d.Observe(ruleEvt("wan-in", burst2.Add(time.Duration(i)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeRuleSpike); got == nil {
		t.Fatal("expected the allow-listed rule to still flag")
	}
}

// TestCharacterizationScope_RulesModeDeny pins the RulesMode axis under
// ListModeDeny, at rule_spike's real DefaultConfig scale.
func TestCharacterizationScope_RulesModeDeny(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	seed := DefaultSettingsMap()
	seed[DetectorRuleSpike] = Settings{Enabled: true, Scope: Scope{Rules: []string{"noisy-rule"}, RulesMode: ListModeDeny}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	last := primeRuleSpikeConstantBaseline(d, "noisy-rule", 5, time.Now())
	burst := last.Add(65 * time.Second)
	for i := 0; i < 20; i++ {
		d.Observe(ruleEvt("noisy-rule", burst.Add(time.Duration(i)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeRuleSpike); got != nil {
		t.Fatalf("expected the denylisted rule to never flag, got %+v", got)
	}

	last2 := primeRuleSpikeConstantBaseline(d, "wan-in", 25, burst.Add(time.Minute))
	burst2 := last2.Add(65 * time.Second)
	for i := 0; i < 12; i++ {
		d.Observe(ruleEvt("wan-in", burst2.Add(time.Duration(i)*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeRuleSpike); got == nil {
		t.Fatal("expected a non-denylisted rule to still flag")
	}
}

// TestCharacterizationScope_AxesCombineWithAND proves #44's model --
// multiple active Scope axes on one detector combine with AND, not OR --
// using critical_port's Hosts+Ports axes together at DefaultConfig
// scale.
func TestCharacterizationScope_AxesCombineWithAND(t *testing.T) {
	cfg := DefaultConfig() // CriticalPorts includes both 21 and 22
	seed := DefaultSettingsMap()
	seed[DetectorCriticalPort] = Settings{
		Enabled: true,
		Scope: Scope{
			Hosts: []string{"198.51.100.4"}, HostsMode: ListModeAllow,
			Ports: []int{22}, PortsMode: ListModeAllow,
		},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	// Matches the host axis but not the ports axis -- port 21 is a real
	// critical port, but not in the ports allow-list.
	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.4", 21, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got != nil {
		t.Fatalf("expected host-only match (wrong port) to never flag, got %+v", got)
	}

	// Matches the ports axis but not the hosts axis.
	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got != nil {
		t.Fatalf("expected port-only match (wrong host) to never flag, got %+v", got)
	}

	// Matches both axes.
	for i := 0; i < 5; i++ {
		d.Observe(evt("198.51.100.4", 22, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := flagOfType(t, fs, flags.TypeCriticalPort); got == nil {
		t.Fatal("expected a match on both axes together to flag")
	}
}
