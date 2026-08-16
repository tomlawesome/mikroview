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

// TestCharacterizationLowSlowScanFiresAtDefaultWindowScale moved to
// internal/engine/shipped_low_slow_scan_test.go's
// TestShippedLowSlowScanFiresAtDefaultWindowScale (issue #405:
// low_slow_scan is now a shipped programmatic definition evaluated by
// internal/engine -- see shipped_low_slow_scan.go). The hours-scale
// window this test exists to exercise (bucketSpanFor(3h) == 3m) is
// unchanged: internal/engine's DistinctRing/CountRing are
// internal/detect's own rings, lifted.

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
//
// activity_spike's own characterization moved to
// internal/engine/shipped_activity_spike_test.go (issue #405:
// activity_spike is now a shipped programmatic definition evaluated by
// internal/engine, not internal/detect -- see shipped_activity_spike.go).
//
// TestCharacterizationActivitySpike_ContinuousRampNeverFiresAtDefaultConfig
// is now TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig,
// and TestCharacterizationActivitySpike_FieldsRefireClearRevive is now
// TestShippedActivitySpike_FieldsRefireClearRevive -- both moved unchanged.
// Every pinned value carried over unchanged: the rate=199/200 boundary,
// the byte-for-byte Detail string "200 events in 1m0s vs a baseline of
// 1.0 for this host (based on 20 samples, 6.0σ above normal)",
// Confidence=100, the empty Evidence, and Country=FR.
//
// #420 (this detector is structurally unfireable through the ordinary
// event path at shipped defaults) is NOT fixed by this move -- it is
// pinned rather than fixed. The port reproduces the old arithmetic
// exactly (the same per-event baseline fold-in, the same 1/emaAlpha lag
// ceiling), and #420 stays open with the remedy still an open design
// decision. See shipped_activity_spike.go's own doc comment and #420
// itself.

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

// TestCharacterizationLowSlowScan_FieldsRefireClearRevive moved to
// internal/engine/shipped_low_slow_scan_test.go's
// TestShippedLowSlowScan_FieldsRefireClearRevive (issue #405). Every
// pinned value carried over unchanged: the boundary landing exactly on
// portThreshold=8, Target, the hand-verifiable Confidence of 0 (the
// weakest-clearing axis, overshootConfidence(8,8), is what bounds it),
// the byte-for-byte Detail prefix, the σ-tail shape, the 8-port/8-host
// Evidence, and the re-fire/clear/revive sequence.

// ---------------------------------------------------------------------------
// 10. off_hours_activity
// ---------------------------------------------------------------------------

// TestCharacterizationOffHours_FieldsRefireClearRevive moved to
// internal/engine/shipped_off_hours_test.go's
// TestShippedOffHours_FieldsRefireClearRevive (issue #405: off_hours is
// now a shipped programmatic definition evaluated by internal/engine,
// not internal/detect -- see shipped_off_hours.go). Every pinned value
// carried over unchanged: the boundary landing exactly on minCount=5
// after a 14-day warm-up, Target, Country=GB, the Detail
// prefix/middle/σ-tail shape, the empty Evidence, Count=1, and the
// re-fire/clear/revive sequence.
//
// off_hours is also the one detector in this port that changed *kind*,
// not just location, and that is worth recording here rather than only
// in shipped_off_hours.go: docs/decisions/evaluation-engine.md section 2
// and #405's own plan both listed off_hours among the detectors expected
// to become shipped *declarative* definitions -- conditions plus a
// window plus a threshold, expressed as data. Actually porting it showed
// that expectation was wrong. off_hours carries twenty-four independent
// EMA baselines per source, one per clock hour, each advancing exactly
// once per calendar day, and it fires on a z-score computed against that
// hour's own history, gated by how many distinct prior days that hour
// has been observed on. There is no window whose contents are being
// counted, and the thing being compared is a per-hour daily mean, not an
// event count -- that is not conditions-plus-a-count under any honest
// reading. Making it declarative would have meant either inventing a
// per-hour-of-day counting mode nothing else in the condition language
// needs, or weakening the statistic in exactly the way
// OffHoursMinSampleDays exists to prevent (see
// TestShippedOffHoursNeverFiresWithoutEstablishedSampleDays). So it
// stayed programmatic instead, alongside host_baseline, global_spike,
// rule_spike and the other definitions the ADR already carves out for
// the same reason: some of what this product does cannot honestly be a
// form.

// ---------------------------------------------------------------------------
// 11. global_spike
// ---------------------------------------------------------------------------
//
// global_spike's own characterization moved to
// internal/engine/shipped_global_spike_test.go (issue #405: global_spike is
// now a shipped programmatic Ticked definition evaluated by internal/engine,
// not internal/detect -- see shipped_global_spike.go). Unlike every other
// detector this file covers, global_spike was never driven through
// Observe(): internal/detect ran it off its own ticker in main.go, polling
// store.Store.EventsPerSecond on an interval, and that ticker is exactly
// what Engine.Tick has replaced. warmGlobalSpike (the constant-1eps
// zero-variance warm-up helper) moved with it, unchanged in behaviour.
//
// TestCharacterizationGlobalSpike_FieldsRefireClearRevive is now
// TestShippedGlobalSpike_FieldsRefireClearRevive; every pinned value carried
// over unchanged -- the 4x-multiplier/5-EPS-floor/20-sample-warmup boundary,
// the byte-for-byte Detail string "5.0 events/s vs a baseline of 1.0 (based
// on 20 samples, 6.0σ above normal)", Confidence=100, and the empty Evidence
// and empty Country.
//
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

// TestCharacterizationScope_Classification pinned the Classification
// axis at activity_spike's real DefaultConfig scale; moved to
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpikeScope_Classification (issue #405:
// activity_spike is now a shipped programmatic definition evaluated by
// internal/engine, not internal/detect).
//
// Only half of this test's pin carried over. The external-source half
// (an out-of-scope source is never even tracked under
// Classification=Internal, let alone flagged) moved unchanged. The
// internal-source half -- that a warmed-then-spiked *in-scope* source
// still flags normally -- did not move with it: like every other
// Evaluate()-driven activity_spike test on the engine side (see the
// activity_spike section above), demonstrating a real fire through the
// ordinary event path runs into #420, and
// TestShippedActivitySpikeScope_Classification only exercises the
// never-flags direction. Flagged in this port's report rather than
// silently dropped.

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
