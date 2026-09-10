// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/decommission"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/store"
)

var decommCreated = time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)

// decommStore is a DecommissionStore entirely under test control, so
// this file exercises the engine half rather than re-testing
// internal/decommission's own state machine (which has its own tests).
type decommStore struct {
	watches []decommission.Watch
	// recorded is every (src, dst, at) RecordTraffic was called with, in
	// order -- the assertion surface for "did the engine hand the
	// observation over at all, and with which end of the event".
	recorded []decommObservation
	swept    []time.Time
}

type decommObservation struct {
	src, dst string
	at       time.Time
	obs      decommission.Observation
}

func (s *decommStore) Active() []decommission.Watch { return s.watches }

func (s *decommStore) RecordTraffic(srcIP, dstIP string, at time.Time, obs decommission.Observation) []decommission.Watch {
	s.recorded = append(s.recorded, decommObservation{src: srcIP, dst: dstIP, at: at, obs: obs})
	var hit []decommission.Watch
	for i := range s.watches {
		if s.watches[i].Matches(srcIP, dstIP) {
			s.watches[i].RecordSighting(srcIP, dstIP, at, obs)
			s.watches[i].RecordTraffic(at)
			hit = append(hit, s.watches[i])
		}
	}
	return hit
}

func (s *decommStore) Sweep(now time.Time) []decommission.Watch {
	s.swept = append(s.swept, now)
	return nil
}

func decommWatch(cidr string) decommission.Watch {
	return decommission.Watch{
		ID:          "watch-" + cidr,
		CIDR:        cidr,
		Name:        "lab",
		CreatedAt:   decommCreated,
		CleanWindow: 6 * time.Hour,
		Covered:     true,
	}
}

func decommEvent(src, dst string, at time.Time) store.Event {
	return store.Event{SrcIP: src, DstIP: dst, DstPort: 443, ReceivedAt: at, Time: at}
}

// buildDecommission wires a set over st and captures its emissions.
func buildDecommission(st DecommissionStore) (*DecommissionWatches, *[]RoutedEmission) {
	var out []RoutedEmission
	x := NewDecommissionWatches(st)
	x.OnRoutedEmission = func(r RoutedEmission) { out = append(out, r) }
	return x, &out
}

func TestDecommissionWatchesEvaluatesBothDirections(t *testing.T) {
	// "ANY traffic to or from it" is the ratified rule, and it is the
	// reason this definition is programmatic at all: a declarative
	// condition set is AND across fields, so source-in-CIDR OR
	// destination-in-CIDR cannot be expressed as one.
	for _, tc := range []struct {
		name     string
		src, dst string
		wantHit  bool
	}{
		{"a device still speaking from the dead range", "192.0.2.7", "10.0.0.1", true},
		{"a rule still steering packets at the dead range", "10.0.0.1", "192.0.2.7", true},
		{"unrelated traffic", "10.0.0.1", "10.0.0.2", false},
	} {
		st := &decommStore{watches: []decommission.Watch{decommWatch("192.0.2.0/24")}}
		x, emissions := buildDecommission(st)
		x.Evaluate(decommEvent(tc.src, tc.dst, decommCreated.Add(time.Hour)))

		if got := len(*emissions); (got > 0) != tc.wantHit {
			t.Errorf("%s: produced %d emissions, wantHit=%v", tc.name, got, tc.wantHit)
		}
		if !tc.wantHit && len(st.recorded) != 0 {
			t.Errorf("%s: the store was consulted for an event outside every watched range", tc.name)
		}
	}
}

func TestDecommissionViolationRoutesUnderTheWatchsOwnEnvelope(t *testing.T) {
	// Each watch routes under its own id, so a straggler lands in the
	// match log as that watch's finding rather than as the set's.
	w := decommWatch("192.0.2.0/24")
	w.LastKnown = []decommission.Straggler{{
		Address: "192.0.2.7",
		Name:    "garage-cam",
		SeenAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	st := &decommStore{watches: []decommission.Watch{w}}
	x, emissions := buildDecommission(st)

	x.Evaluate(decommEvent("192.0.2.7", "10.0.0.1", decommCreated.Add(time.Hour)))

	if len(*emissions) != 1 {
		t.Fatalf("produced %d emissions, want 1", len(*emissions))
	}
	got := (*emissions)[0]
	if got.Expectation == nil {
		t.Fatal("the emission did not route to the match log -- a decommission watch is expectation intent")
	}
	if got.Expectation.EntryID != w.ID {
		t.Errorf("routed under entry %q, want the watch's own id %q", got.Expectation.EntryID, w.ID)
	}
	if got.Expectation.Target != "192.0.2.7" {
		t.Errorf("target is %q, want the straggler %q", got.Expectation.Target, "192.0.2.7")
	}
	if want := `traffic from 192.0.2.7 -- was "garage-cam" until 2026-08-01`; got.Expectation.Detail != want {
		t.Errorf("detail is %q, want %q -- the enrichment did not reach the finding", got.Expectation.Detail, want)
	}
}

func TestDecommissionNamesTheEndInsideTheDeadRange(t *testing.T) {
	// Where only the destination is inside the range, the straggler is
	// the destination: reporting the live host that spoke to it would
	// name the wrong device.
	st := &decommStore{watches: []decommission.Watch{decommWatch("192.0.2.0/24")}}
	x, emissions := buildDecommission(st)

	x.Evaluate(decommEvent("10.0.0.1", "192.0.2.9", decommCreated.Add(time.Hour)))

	if len(*emissions) != 1 {
		t.Fatalf("produced %d emissions, want 1", len(*emissions))
	}
	if got := (*emissions)[0].Expectation.Target; got != "192.0.2.9" {
		t.Errorf("target is %q, want the address inside the retired range", got)
	}
}

func TestDecommissionObservationUsesArrivalTimeNotTheRoutersClock(t *testing.T) {
	// A skewed router must not be able to shorten or extend a
	// decommission -- the clean window is mikroview's claim about what
	// it saw and when it saw it.
	st := &decommStore{watches: []decommission.Watch{decommWatch("192.0.2.0/24")}}
	x, _ := buildDecommission(st)

	e := decommEvent("192.0.2.7", "10.0.0.1", decommCreated.Add(time.Hour))
	e.Time = decommCreated.Add(-72 * time.Hour) // a router with a wrong clock
	x.Evaluate(e)

	if len(st.recorded) != 1 {
		t.Fatalf("the store took %d observations, want 1", len(st.recorded))
	}
	if !st.recorded[0].at.Equal(e.ReceivedAt) {
		t.Errorf("observation stamped %s, want the arrival time %s", st.recorded[0].at, e.ReceivedAt)
	}
}

func TestDecommissionTickSweepsTheCleanWindow(t *testing.T) {
	// The clean window's expiry is the retirement, and nothing arrives
	// to notice it -- which is why this is a Ticked definition.
	st := &decommStore{watches: []decommission.Watch{decommWatch("192.0.2.0/24")}}
	x, _ := buildDecommission(st)

	if x.TickInterval() <= 0 {
		t.Fatal("the sweep declares no interval, so it would never run")
	}
	x.Tick(decommCreated.Add(9 * time.Hour))
	if len(st.swept) != 1 {
		t.Errorf("Tick swept %d times, want 1", len(st.swept))
	}
}

func TestDecommissionWatchesIsSafeWithNoStore(t *testing.T) {
	// A deployment with no persistence still registers the whole engine;
	// this one simply evaluates nothing.
	x := NewDecommissionWatches(nil)
	x.Evaluate(decommEvent("192.0.2.7", "10.0.0.1", decommCreated))
	x.Tick(decommCreated)
	if x.Len() != 0 {
		t.Errorf("a set over no store holds %d watches, want 0", x.Len())
	}
}

// --- the receipt behind the offer ---------------------------------------

func TestReplayDecommissionCountsWhatTheWatchWouldHaveCaught(t *testing.T) {
	// #485's ratified sentence: the offer arrives with "in the last N
	// hours this would have caught 3". This is that number.
	base := decommCreated
	events := []store.Event{
		decommEvent("192.0.2.7", "10.0.0.1", base),                     // from the dead range
		decommEvent("10.0.0.1", "192.0.2.9", base.Add(time.Minute)),    // to the dead range
		decommEvent("10.0.0.1", "10.0.0.2", base.Add(2*time.Minute)),   // unrelated
		decommEvent("192.0.2.200", "8.8.8.8", base.Add(3*time.Minute)), // from the dead range
		decommEvent("192.0.3.7", "10.0.0.1", base.Add(4*time.Minute)),  // the adjacent range
	}

	result, err := ReplayDecommission("192.0.2.1/24", fakeCorpus{events: events})
	if err != nil {
		t.Fatalf("ReplayDecommission: %v", err)
	}
	if result.Receipt == nil {
		t.Fatalf("no receipt: %+v", result.Decline)
	}
	if got := result.Receipt.EmissionCount(); got != 3 {
		t.Errorf("the receipt claims %d stragglers, want 3", got)
	}
	// The span travels with the count: a receipt that omits the period
	// it was measured over invites the reader to supply a flattering
	// one.
	win := result.Receipt.Window()
	if win.EventCount() != len(events) {
		t.Errorf("the receipt's span covers %d events, want the whole corpus (%d)", win.EventCount(), len(events))
	}
	if win.Duration() != 4*time.Minute {
		t.Errorf("the receipt's span is %s, want 4m", win.Duration())
	}
}

func TestReplayDecommissionDeclinesOnAnEmptyCorpus(t *testing.T) {
	// "Nothing would have been caught" and "there was nothing to catch
	// it in" are different answers, and reporting the first for the
	// second would be a confident zero about a question nobody asked.
	result, err := ReplayDecommission("192.0.2.0/24", fakeCorpus{})
	if err != nil {
		t.Fatalf("ReplayDecommission: %v", err)
	}
	if result.Receipt != nil {
		t.Errorf("an empty corpus produced a receipt claiming %d stragglers", result.Receipt.EmissionCount())
	}
	if result.Decline == nil {
		t.Fatal("an empty corpus produced neither a receipt nor a decline")
	}
}

func TestReplayDecommissionRefusesARangeItCannotRead(t *testing.T) {
	if _, err := ReplayDecommission("not-a-range", fakeCorpus{}); err == nil {
		t.Error("a value that is not a range was replayed")
	}
}

// --- could a straggler have been seen at all ----------------------------

func TestDecommissionCoverage(t *testing.T) {
	rng := "192.0.2.0/24"
	for _, tc := range []struct {
		name  string
		rules map[string][]ingest.FilterRule
		want  CoverageState
	}{
		{
			name:  "nothing pushed at all",
			rules: nil,
			want:  CoverageUnknown,
		},
		{
			name:  "a logging rule with no address condition catches everything",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true}}},
			want:  CoverageOK,
		},
		{
			name:  "a logging rule scoped to the range itself",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "192.0.2.0/24"}}},
			want:  CoverageOK,
		},
		{
			name: "a logging rule scoped to part of the range still covers it",
			// The overlap case a containment test would answer
			// backwards: 192.0.2.200/32 is inside the range, so a
			// straggler at that address would be seen.
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "192.0.2.200/32"}}},
			want:  CoverageOK,
		},
		{
			name:  "a logging rule scoped to the range as a destination",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, DstAddress: "192.0.2.0/25"}}},
			want:  CoverageOK,
		},
		{
			name:  "a logging rule covering an address range that spans it",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "192.0.1.0-192.0.3.255"}}},
			want:  CoverageOK,
		},
		{
			name:  "rules pushed, none of them logging",
			rules: map[string][]ingest.FilterRule{"r1": {{SrcAddress: "192.0.2.0/24"}, {DstAddress: "0.0.0.0/0"}}},
			want:  CoverageNoLogging,
		},
		{
			name:  "logging rules that cannot touch this range",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "198.51.100.0/24", DstAddress: "203.0.113.0/24"}}},
			want:  CoverageNoLogging,
		},
		{
			name: "one router logs it even though another does not",
			rules: map[string][]ingest.FilterRule{
				"r1": {{Log: true, SrcAddress: "198.51.100.0/24", DstAddress: "203.0.113.0/24"}},
				"r2": {{Log: true, SrcAddress: "192.0.2.0/24"}},
			},
			want: CoverageOK,
		},
		{
			name: "a rule with no destination condition logs into the range whatever its source says",
			// Either end is enough: a rule matching sources outside
			// 10/8, with no destination condition at all, still logs
			// traffic arriving at the dead range from anywhere else.
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "!10.0.0.0/8"}}},
			want:  CoverageOK,
		},
		{
			name: "a negated condition on both ends is unanswerable, not a negative",
			// Over a range, "everything outside 10/8" overlaps almost
			// every prefix and excludes a few. Unknown errs towards
			// refusing to claim quiet, which is the safe direction here:
			// the cost of a wrong "nothing is logging this" is a
			// decommission that never retires.
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "!10.0.0.0/8", DstAddress: "!10.0.0.0/8"}}},
			want:  CoverageUnknown,
		},
		{
			name:  "a disabled rule is not evidence either way",
			rules: map[string][]ingest.FilterRule{"r1": {{Log: true, SrcAddress: "192.0.2.0/24", Disabled: true}}},
			want:  CoverageUnknown,
		},
	} {
		if got := DecommissionCoverage(rng, tc.rules); got != tc.want {
			t.Errorf("%s: coverage is %q, want %q", tc.name, got, tc.want)
		}
	}
}
