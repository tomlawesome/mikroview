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

// TestCharacterizationPortScanFiresAtDefaultConfigScale moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedPortScanFiresAtDefaultConfigScale (issue #405: port_scan is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect -- see shipped_declarative.go's buildPortScanDefinition).
// Every pinned value carried over unchanged.

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

// TestCharacterizationDistributedBruteForceRequiresDistinctSources used
// to document a deliberate divergence from the migration plan's summary
// table, which listed "countRing per key" for
// observeDistributedBruteForce -- the detector's entire point is
// *distinct* source IPs hammering one port, so it needed a
// distinctRing[string] instead, the same guarantee
// TestDistributedBruteForceIgnoresRepeatsFromSameSource pinned in
// distributed_brute_force_test.go; moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedDistributedBruteForceIgnoresRepeatsFromSameSource (issue
// #405: distributed_brute_force is now a shipped declarative definition
// evaluated by internal/engine, not internal/detect -- see
// shipped_declarative.go's buildDistributedBruteForceDefinition). Every
// pinned value carried over unchanged.

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
//
// port_scan's own characterization (firing boundary, flag fields,
// re-fire/clear/revive, and its Hosts/Classification scope pins) moved to
// internal/engine/shipped_declarative_test.go's TestShippedPortScan_*
// series (issue #405: port_scan is now a shipped declarative definition
// evaluated by internal/engine, not internal/detect -- see
// shipped_declarative.go's buildPortScanDefinition). Every pinned value
// carried over unchanged.

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
//
// critical_port's own characterization moved to
// internal/engine/shipped_declarative_test.go (issue #405: critical_port is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect -- see shipped_declarative.go's
// buildCriticalPortDefinition). The boundary/fields/confidence/re-fire/
// clear/revive pin that used to live here as
// TestCharacterizationCriticalPort_FieldsRefireClearRevive is now
// TestShippedCriticalPort_FieldsRefireClearRevive; the #379 known-wrong
// Detail-naming pin that used to live here as
// TestCharacterizationCriticalPort_DetailNamesOnlyTheLastPort is now
// TestShippedCriticalPort_DetailNamesTheSetOfPortsTouched -- renamed, not
// just moved, because #379 landed as part of this same port: the Detail
// string now names the accumulated set of ports touched across the
// counted attempts instead of only the single triggering event's port,
// and Evidence.Ports is now populated (it used to be the zero value).
// Per the original pin's own instruction ("this pin should be *updated*
// to match the corrected wording, not deleted to make the diff
// quieter"), the engine-side test was updated in place to match #379's
// fix rather than deleted and rewritten from scratch.

// ---------------------------------------------------------------------------
// 4. distributed_brute_force
// ---------------------------------------------------------------------------

// TestCharacterizationDistributedBruteForce_FieldsRefireClearRevive
// used to pin distributed_brute_force's boundary at DefaultConfig's real
// 10-distinct-sources/5-minute threshold against a critical port, its
// flag fields, and its re-fire/clear/revive cycle; moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedDistributedBruteForce_FieldsRefireClearRevive (issue #405:
// distributed_brute_force is now a shipped declarative definition
// evaluated by internal/engine, not internal/detect -- see
// shipped_declarative.go's buildDistributedBruteForceDefinition). Every
// pinned value carried over unchanged; also covers what
// TestDistributedBruteForceFlagsManyDistinctSources and
// TestDistributedBruteForceEvidenceCapturesSourceHosts pinned in the
// now-deleted distributed_brute_force_test.go.

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
//
// rule_spike's own characterization moved to
// internal/engine/shipped_rule_spike_test.go (issue #405: rule_spike is now
// a shipped programmatic definition evaluated by internal/engine, not
// internal/detect -- see shipped_rule_spike.go). primeRuleSpikeConstantBaseline
// moved with it, unchanged in behaviour.
//
// TestCharacterizationRuleSpike_FieldsRefireClearRevive did not move like
// the rest of this section -- it does not survive. This is #405's third
// and last sanctioned characterization diff, and it implements #368: the
// old rule_spike primed its EMA baseline from the very first reading, which
// was measured over a window that had only just started existing and so
// read artificially low (here, a baseline of 0.0 after a single sample),
// and the window's own ordinary fill then satisfied "5x baseline" with no
// actual change in traffic -- exactly 12 hits in a 60s window, the MinRate
// floor, not a real spike. That boundary -- "a flag at exactly 12 hits in
// the window" -- was this test's whole point, and it no longer exists to
// pin: internal/engine's Baseline (baseline.go) refuses to prime inside the
// first window it observes, and gates firing on a declared BaselineFloor,
// so a rule's first dozen-odd hits ever (or after a restart with no
// persisted state) now produce silence instead of a false spike. See
// internal/engine/shipped_rule_spike_test.go's
// TestShippedRuleSpikeNoFalseSpikeInsideTheFirstWindow (#368's own scenario,
// closed) and TestShippedRuleSpikeNoFalseSpikeOnRestart (#368's restart
// half, both the warm-resume-from-state and cold-restart shapes) for what
// replaces this pin. Alongside those two, and deliberately not left as the
// only rule_spike coverage, is
// TestShippedRuleSpikeFlagsWellAboveOwnBaseline: #368's fix must not have
// bought its silence by making the detector inert, so a genuine, large
// departure from a rule's own established baseline is pinned to still
// fire. This test's re-fire/clear/revive sequence (Count incrementing on a
// second crossing, Clear, then revival as a fresh active flag) is not
// separately re-pinned for rule_spike on the engine side.

// ---------------------------------------------------------------------------
// 8. repeated_drops
// ---------------------------------------------------------------------------
//
// repeated_drops' own characterization moved to
// internal/engine/shipped_declarative_test.go (issue #405: repeated_drops is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect -- see shipped_declarative.go's
// buildRepeatedDropsDefinition). The boundary/fields/confidence/re-fire/
// clear/revive pin that used to live here as
// TestCharacterizationRepeatedDrops_FieldsRefireClearRevive is now
// TestShippedRepeatedDrops_FieldsRefireClearRevive; the NAT-evidence pin
// that used to live here as
// TestCharacterizationRepeatedDrops_EvidenceCapturesNAT is now
// TestShippedRepeatedDrops_EvidenceCapturesNAT, moved unchanged.
//
// TestCharacterizationRepeatedDrops_DetailNamesOnlyTheLastDestination is
// now TestShippedRepeatedDrops_DetailCarriesTheDestinationSetAsEvidence --
// renamed, not just moved, because #379 landed as part of this same port:
// the Detail string no longer names a single destination address (it
// names only the destination port, which is a key component and so true
// of every counted attempt), and the distinct destination set -- which
// used to be nowhere -- now rides in Evidence.Hosts, following
// dest_spread's pattern. Per the original pin's own instruction ("this
// pin should be *updated*, not deleted, when that lands"), the
// engine-side test was updated in place to match #379's fix rather than
// deleted and rewritten from scratch.

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

// TestCharacterizationScope_HostsAllow used to pin the Hosts axis under
// ListModeAllow at critical_port's DefaultConfig scale; moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortScope_HostsAllow (issue #405: critical_port is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect). Every pinned value carried over unchanged.

// TestCharacterizationScope_HostsModeDeny used to pin the HostsMode axis
// under ListModeDeny at critical_port's DefaultConfig scale (5/5m) --
// itself already a retarget from port_scan (see the comment this test
// used to carry). With critical_port's own move to internal/engine (issue
// #405), there is no internal/detect detector left with an obvious,
// convenient reason to re-host this axis case, so it isn't retargeted a
// second time: the Hosts-deny axis is already covered by
// internal/engine/shipped_declarative_test.go's
// TestShippedPortScanScope_HostsModeDeny (not a critical_port-named test --
// port_scan's own HostsMode pin, which this test was itself standing in
// for before critical_port took over that role).

// TestCharacterizationScope_PortsAllow used to pin the Ports axis under
// ListModeAllow at repeated_drops' real DefaultConfig scale (10/15m);
// moved to internal/engine/shipped_declarative_test.go's
// TestShippedRepeatedDropsScope_PortsAllow (issue #405: repeated_drops is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect). Every pinned value carried over unchanged.

// TestCharacterizationScope_PortsModeDeny used to pin the PortsMode axis
// under ListModeDeny at critical_port's DefaultConfig scale, restricting
// the *effective* subset of Config.CriticalPorts a scoped instance reacts
// to; moved to internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortScope_PortsModeDeny (issue #405: critical_port is
// now a shipped declarative definition evaluated by internal/engine, not
// internal/detect). Every pinned value carried over unchanged.

// TestCharacterizationScope_Classification pins the Classification axis,
// at activity_spike's real DefaultConfig scale -- per settings.go's
// per-detector field usage table, ActivitySpike's Hosts/HostsMode +
// Classification restrict which source IPs are tracked at all.
// Previously exercised via port_scan (a plain threshold, so a firing
// scenario was one loop); port_scan's own Classification pin moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedPortScanScope_Classification (issue #405). Retargeted, not
// deleted, so internal/detect's own scope-axis coverage stays complete;
// activity_spike's EMA baseline needs a warm-up-then-spike shape (see
// TestActivitySpikeFlagsGenuineDeviationFromHostsOwnBaseline) rather than
// a single loop to actually fire.
func TestCharacterizationScope_Classification(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 2
	cfg.ActivitySpikeWindow = time.Second
	cfg.HostActivityMultiplier = 3
	cfg.HostActivityWarmupSamples = 20
	seed := DefaultSettingsMap()
	seed[DetectorActivitySpike] = Settings{Enabled: true, Scope: Scope{Classification: store.ScopeInternal}}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)
	now := time.Now()

	warmThenSpike := func(ip string, start time.Time) {
		tick := time.Duration(0)
		for i := 0; i < 25; i++ {
			base := start.Add(tick)
			d.Observe(evt(ip, 100, base))
			d.Observe(evt(ip, 101, base.Add(10*time.Millisecond)))
			tick += 2 * time.Second
		}
		spikeBase := start.Add(tick)
		for i := 0; i < 10; i++ {
			d.Observe(evt(ip, 200+i, spikeBase.Add(time.Duration(i)*10*time.Millisecond)))
		}
	}

	// An external source: never even tracked under Classification=Internal
	// (asActive gates observeScanAndSpike before any window is created),
	// so warming it up and spiking it the same way a real deviation would
	// still never flags.
	extIP := "203.0.113.9"
	warmThenSpike(extIP, now)
	if got := flagOfType(t, fs, flags.TypeActivitySpike); got != nil {
		t.Fatalf("expected an external source to never flag under Classification=Internal, got %+v", got)
	}

	// An internal (LAN) source still flags normally.
	warmThenSpike("192.168.1.77", now.Add(time.Minute))
	if got := flagOfType(t, fs, flags.TypeActivitySpike); got == nil {
		t.Fatal("expected an internal source to still flag under Classification=Internal")
	}
}

// TestCharacterizationScope_RulesAllow and TestCharacterizationScope_
// RulesModeDeny used to pin the Rules axis (ListModeAllow and ListModeDeny)
// at rule_spike's real DefaultConfig scale. rule_spike is now a shipped
// programmatic definition evaluated by internal/engine, not internal/detect
// (issue #405 -- see shipped_rule_spike.go), so both moved with it: the
// surviving rules-axis coverage is
// internal/engine/shipped_rule_spike_test.go's
// TestShippedRuleSpikeRespectsRulesDenylist.

// TestCharacterizationScope_AxesCombineWithAND used to prove #44's model
// -- multiple active Scope axes on one detector combine with AND, not OR
// -- using critical_port's Hosts+Ports axes together at DefaultConfig
// scale; moved to internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortScope_AxesCombineWithAND (issue #405:
// critical_port is now a shipped declarative definition evaluated by
// internal/engine, not internal/detect). Every pinned value carried over
// unchanged.
