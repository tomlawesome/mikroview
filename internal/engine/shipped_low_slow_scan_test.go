// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// lowSlowDefaultParams mirrors internal/detect.DefaultConfig()'s
// low_slow_scan block exactly -- the shipped defaults every pinned value
// in this file is measured at.
func lowSlowDefaultParams() Params {
	return Params{
		"window":             (3 * time.Hour).String(),
		"portThreshold":      8,
		"hostThreshold":      5,
		"minObservation":     (45 * time.Minute).String(),
		"dropRatio":          0.8,
		"baselineMultiplier": 3.0,
		"updateCadence":      "perEvent",
	}
}

// lowSlowTestParams is internal/detect/low_slow_scan_test.go's lowSlowCfg:
// the same defaults with smaller thresholds and shorter durations, so the
// non-characterization tests below stay fast and deterministic.
func lowSlowTestParams() Params {
	p := lowSlowDefaultParams()
	p["window"] = (2 * time.Hour).String()
	p["portThreshold"] = 5
	p["hostThreshold"] = 5
	p["minObservation"] = (10 * time.Minute).String()
	p["baselineMultiplier"] = 2.0
	return p
}

func newShippedLowSlowScanDefinition(t *testing.T, sink func(RoutedEmission), params Params, scope Scope, enabled bool) *lowSlowScanDefinition {
	t.Helper()
	full := lowSlowDefaultParams()
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          "low_slow_scan",
		Name:        "Low & slow scan",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     enabled,
		Scope:       scope,
		Params:      full,
		ParamSchema: LowSlowScanParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(low_slow_scan): %v", err)
	}
	d := built.(*lowSlowScanDefinition)
	d.SetSink(sink)
	return d
}

func lsFlag(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeLowSlowScan {
			return &f
		}
	}
	return nil
}

// lowSlowEvt is internal/detect/low_slow_scan_test.go's helper of the
// same name.
func lowSlowEvt(srcIP, dstIP string, dstPort int, action store.Action, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, Action: action, ReceivedAt: at, ConnState: "new"}
}

// feedPacedScan is internal/detect/low_slow_scan_test.go's helper of the
// same name: n events, one every step, each touching a distinct
// (port, host) pair -- the shape a real low-and-slow scanner produces.
func feedPacedScan(d *lowSlowScanDefinition, srcIP string, n int, step time.Duration, action store.Action, t0 time.Time) time.Time {
	last := t0
	for i := 0; i < n; i++ {
		last = t0.Add(time.Duration(i) * step)
		d.Evaluate(lowSlowEvt(srcIP, fmt.Sprintf("192.168.50.%d", i+1), 10000+i, action, last))
	}
	return last
}

func TestShippedLowSlowScanFiresWhenAllAxesClear(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	f := lsFlag(fs)
	if f == nil {
		t.Fatalf("expected a low_slow_scan flag, got %+v", fs.List())
	}
	if f.Target != "203.0.113.9" {
		t.Errorf("expected target to be the source IP, got %q", f.Target)
	}
	if f.Confidence == nil {
		t.Errorf("expected a confidence score, got nil")
	}
	if len(f.Evidence.Ports) < 5 {
		t.Errorf("expected at least 5 ports in evidence, got %v", f.Evidence.Ports)
	}
	if len(f.Evidence.Hosts) < 5 {
		t.Errorf("expected at least 5 hosts in evidence, got %v", f.Evidence.Hosts)
	}
}

func TestShippedLowSlowScanRequiresPortBreadth(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	// Many distinct hosts, but always the same port -- a horizontal probe
	// on one known service, not a scan.
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		d.Evaluate(lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 22, store.ActionDrop, at))
	}
	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected no flag with port breadth stuck at 1, got %+v", f)
	}
}

func TestShippedLowSlowScanRequiresHostBreadth(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	// Many distinct ports, but always the same host -- a vertical scan on
	// one already-known host, deliberately port_scan's territory.
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		d.Evaluate(lowSlowEvt("203.0.113.9", "192.168.50.1", 10000+i, store.ActionDrop, at))
	}
	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected no flag with host breadth stuck at 1, got %+v", f)
	}
}

func TestShippedLowSlowScanRequiresDropRatio(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionAccept, t0)

	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected no flag when traffic is mostly accepted, got %+v", f)
	}
}

func TestShippedLowSlowScanRequiresMinimumObservation(t *testing.T) {
	fs := newTestFlagsStore(t)
	params := lowSlowTestParams()
	params["minObservation"] = time.Hour.String()
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), params, Scope{}, true)

	t0 := time.Now()
	// Same breadth/drop-ratio pattern as the firing test, compressed into
	// a few seconds -- well short of the one-hour observation floor.
	feedPacedScan(d, "203.0.113.9", 8, time.Second, store.ActionDrop, t0)

	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected no flag before the minimum observation floor is met, got %+v", f)
	}
}

func TestShippedLowSlowScanIgnoresUntrackableConnState(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		e := lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, at)
		e.ConnState = "established"
		d.Evaluate(e)
	}
	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected established/return traffic to be ignored, got %+v", f)
	}
}

func TestShippedLowSlowScanRespectsHostScope(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(),
		Scope{Hosts: []string{"198.51.100.1"}, HostsMode: ListModeAllow}, true)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected a source outside the allowed hosts list to never flag, got %+v", f)
	}
}

func TestShippedLowSlowScanDisabledToggle(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, false)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if f := lsFlag(fs); f != nil {
		t.Fatalf("expected a disabled definition to never flag, got %+v", f)
	}
}

func TestShippedLowSlowScanCarriesCountry(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		e := lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, at)
		e.SrcCountry = "NL"
		d.Evaluate(e)
	}

	f := lsFlag(fs)
	if f == nil {
		t.Fatalf("expected a flag to fire, got %+v", fs.List())
	}
	if f.Country != "NL" {
		t.Errorf("expected Country to be threaded through, got %q", f.Country)
	}
}

// TestShippedLowSlowScanTriggersReputationLookup is
// internal/detect/low_slow_scan_test.go's
// TestLowSlowScanTriggersReputationLookup: the single-address reputation
// lookup internal/detect fired through maybeCheckReputation now runs
// through ReputationSink, which is what main.go wires onto this
// definition (it is not in shippedGroupReputationIDs).
func TestShippedLowSlowScanTriggersReputationLookup(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()
	defer close(fake.release)
	d := newShippedLowSlowScanDefinition(t, ReputationSink(fs, fake, DefaultReputationPolicy()), lowSlowTestParams(), Scope{}, true)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if lsFlag(fs) == nil {
		t.Fatalf("expected a flag to fire before reputation is even relevant, got %+v", fs.List())
	}
	select {
	case ip := <-fake.started:
		if ip != "203.0.113.9" {
			t.Errorf("expected the reputation lookup to target the source IP, got %q", ip)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a reputation lookup to be started for the new episode")
	}
}

// TestShippedLowSlowScanFiresAtDefaultWindowScale is
// internal/detect/characterization_test.go's
// TestCharacterizationLowSlowScanFiresAtDefaultWindowScale, moved
// unchanged: the three rings at the real 3-hour window, where
// bucketSpanFor(3h) == 3m.
func TestShippedLowSlowScanFiresAtDefaultWindowScale(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), nil, Scope{}, true)

	t0 := time.Now()
	// One paced, mostly-refused attempt every ~13 minutes -- comfortably
	// past the 45-minute observation floor and past a 3-minute bucket span
	// by the time enough samples accumulate to clear the port/host breadth
	// thresholds (8/5).
	feedPacedScan(d, "203.0.113.9", 8+2, 13*time.Minute, store.ActionDrop, t0)

	if lsFlag(fs) == nil {
		t.Fatalf("expected a low_slow_scan flag at default-config scale, got %+v", fs.List())
	}
}

// TestShippedLowSlowScan_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationLowSlowScan_FieldsRefireClearRevive, moved. Every
// pinned value is unchanged: the boundary landing exactly on
// portThreshold=8, Target, the hand-verifiable Confidence of 0 (the
// weakest axis is overshootConfidence(8,8)==0), the byte-for-byte Detail
// prefix, the σ tail's shape, the 8-port/8-host Evidence, and the
// re-fire/clear/revive sequence.
// lowSlowRampEvents builds n events from srcIP, spaced step apart
// starting at t0, each touching a brand-new distinct destination port and
// host -- port/host breadth grows by exactly 2 every event, the fastest
// this definition's own Replay loop can grow it (one port, one host per
// event), which is what
// TestShippedLowSlowScanReplaySampleKeepsMostRecent needs to keep the
// per-source baseline's z-score clearing emaMinZ for a good long run
// before the EMA catches up and firing tails off.
func lowSlowRampEvents(srcIP string, n int, step time.Duration, t0 time.Time) []store.Event {
	events := make([]store.Event, n)
	for i := 0; i < n; i++ {
		events[i] = lowSlowEvt(srcIP, fmt.Sprintf("10.9.0.%d", i+1), 20000+i, store.ActionDrop, t0.Add(time.Duration(i)*step))
	}
	return events
}

// TestShippedLowSlowScanReplaySampleKeepsMostRecent pins issue #860 for
// the low-slow-scan replay path. Unlike this file's other definitions,
// low_slow_scan's Replay gates firing on a z-score
// (before.ZScore < emaMinZ) that no candidate override can bypass, and a
// single source's own EMA baseline catches up with a sustained breadth
// ramp after a few dozen events regardless of how fast the ramp climbs --
// so this test cannot lean on "one long burst" the way the global-spike
// and rule-spike siblings do.
//
// Instead it uses two sources, srcA then srcB, each ramping port/host
// breadth independently (per-source state, so neither's baseline sees the
// other). It measures each source's own fire count from a solo Replay
// first -- not a hand-derived constant, since the exact EMA cutoff point
// is a property of the baseline math, not of this test -- then checks the
// combined replay's sample against those measured counts: the sample
// must be made up of all of srcB's (the later source's) fires plus only
// the tail of srcA's, never the other way around. The old first-N
// behaviour would keep all of srcA's fires (it fired first) and only as
// much of srcB's as fits after that -- the opposite split.
func TestShippedLowSlowScanReplaySampleKeepsMostRecent(t *testing.T) {
	params := Params{
		"window":             (5 * time.Minute).String(),
		"portThreshold":      3,
		"hostThreshold":      3,
		"minObservation":     (30 * time.Second).String(),
		"dropRatio":          0.5,
		"baselineMultiplier": 1.0,
	}
	step := 5 * time.Second
	n := 70

	solo := func(srcIP string, t0 time.Time) int {
		fs := newTestFlagsStore(t)
		d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), params, Scope{}, true)
		res, err := d.Replay(fakeCorpus{events: lowSlowRampEvents(srcIP, n, step, t0)}, nil)
		if err != nil {
			t.Fatalf("Replay (solo %s): %v", srcIP, err)
		}
		if res.Receipt == nil {
			t.Fatalf("Replay (solo %s) declined unexpectedly: %+v", srcIP, res.Decline)
		}
		return res.Receipt.EmissionCount()
	}

	t0 := time.Now().Add(-2 * time.Hour)
	eventsA := lowSlowRampEvents("203.0.113.21", n, step, t0)
	tB := eventsA[len(eventsA)-1].ReceivedAt.Add(step)
	eventsB := lowSlowRampEvents("203.0.113.22", n, step, tB)

	firesA := solo("203.0.113.21", t0)
	firesB := solo("203.0.113.22", tB)
	if firesA+firesB <= replaySampleBound {
		t.Fatalf("test setup produces only %d+%d=%d fires, want more than replaySampleBound=%d", firesA, firesB, firesA+firesB, replaySampleBound)
	}

	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), params, Scope{}, true)
	var events []store.Event
	events = append(events, eventsA...)
	events = append(events, eventsB...)
	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay (combined): %v", err)
	}
	if res.Receipt == nil {
		t.Fatalf("Replay (combined) declined unexpectedly: %+v", res.Decline)
	}
	if got, want := res.Receipt.EmissionCount(), firesA+firesB; got != want {
		t.Fatalf("combined EmissionCount() = %d, want firesA+firesB = %d (the two sources' baselines are independent, so replaying them together should not change either's count)", got, want)
	}

	sample := res.Receipt.Sample()
	if len(sample) != replaySampleBound {
		t.Fatalf("len(Sample()) = %d, want exactly replaySampleBound=%d", len(sample), replaySampleBound)
	}
	wantSrcBCount := firesB
	if wantSrcBCount > replaySampleBound {
		wantSrcBCount = replaySampleBound
	}
	wantSrcACount := replaySampleBound - wantSrcBCount

	var gotACount, gotBCount int
	seenB := false
	for i, s := range sample {
		switch s.Target {
		case "203.0.113.21":
			gotACount++
			if seenB {
				t.Fatalf("sample[%d] is srcA (%s) after an srcB entry -- sample must stay chronological, and srcA is entirely earlier than srcB", i, s.At)
			}
		case "203.0.113.22":
			gotBCount++
			seenB = true
		default:
			t.Fatalf("sample[%d].Target = %q, want srcA or srcB", i, s.Target)
		}
	}
	if gotACount != wantSrcACount || gotBCount != wantSrcBCount {
		t.Fatalf("sample holds %d srcA + %d srcB entries, want %d srcA + %d srcB -- sample must keep all of the later source's (srcB) emissions before trimming into the earlier source's (srcA), not the reverse",
			gotACount, gotBCount, wantSrcACount, wantSrcBCount)
	}
}

func TestShippedLowSlowScan_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), nil, Scope{}, true)
	ip := "203.0.113.9"
	t0 := time.Now()

	// 7 steps: portCount==hostCount==7 < portThreshold(8) -- breadth not
	// cleared yet, so no flag regardless of anything else.
	feedPacedScan(d, ip, 7, 10*time.Minute, store.ActionDrop, t0)
	if got := lsFlag(fs); got != nil {
		t.Fatalf("expected no flag at portCount=hostCount=7, got %+v", got)
	}

	// The 8th step clears every axis at once.
	last := t0.Add(7 * 10 * time.Minute)
	d.Evaluate(lowSlowEvt(ip, "192.168.50.8", 10007, store.ActionDrop, last))
	f := lsFlag(fs)
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
	d.Evaluate(lowSlowEvt(ip, "192.168.50.9", 10008, store.ActionDrop, t0.Add(8*10*time.Minute)))
	f2 := lsFlag(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}

	// Clear + revive.
	if _, ok := fs.SetVerdict(f2.ID, flags.VerdictChecked, "operator", t0.Add(9*10*time.Minute)); !ok {
		t.Fatal("expected Clear to succeed")
	}
	d.Evaluate(lowSlowEvt(ip, "192.168.50.10", 10009, store.ActionDrop, t0.Add(10*10*time.Minute)))
	f3 := lsFlag(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// TestShippedLowSlowScanReplayDeclinesOnShortCorpus pins the Decline this
// definition owes a corpus that could not possibly have let it fire: the
// in-memory event ring holds minutes, and nothing under the 45-minute
// observation floor is eligible however broad it looks.
func TestShippedLowSlowScanReplayDeclinesOnShortCorpus(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), nil, Scope{}, true)

	t0 := time.Now()
	var events []store.Event
	for i := 0; i < 20; i++ {
		events = append(events, lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, t0.Add(time.Duration(i)*time.Second)))
	}

	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Decline == nil {
		t.Fatalf("expected a Decline over a 19-second corpus, got %+v", res)
	}
	if res.Receipt != nil {
		t.Errorf("expected no Receipt alongside a Decline, got %+v", res.Receipt)
	}
}

// TestShippedLowSlowScanReplayProducesReceiptOverLongEnoughCorpus is the
// other half: a corpus that does span the window and the observation
// floor gets a real count, and replaying does not disturb the live
// definition's own state.
func TestShippedLowSlowScanReplayProducesReceiptOverLongEnoughCorpus(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedLowSlowScanDefinition(t, FlagsSink(fs), nil, Scope{}, true)

	t0 := time.Now().Add(-4 * time.Hour)
	var events []store.Event
	for i := 0; i < 20; i++ {
		events = append(events, lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, t0.Add(time.Duration(i)*11*time.Minute)))
	}

	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Decline != nil {
		t.Fatalf("expected a Receipt over a 3h29m corpus, got Decline %+v", res.Decline)
	}
	if res.Receipt == nil {
		t.Fatal("expected a Receipt")
	}
	if res.Receipt.EmissionCount() == 0 {
		t.Errorf("expected the paced-scan corpus to produce at least one emission, got 0")
	}
	if got := len(fs.List()); got != 0 {
		t.Errorf("Replay raised %d live flag(s) -- it must never touch the live sink", got)
	}
	if d.tracks.Len() != 0 {
		t.Errorf("Replay mutated the live definition's tracked sources (%d) -- it must use call-local state only", d.tracks.Len())
	}
}
