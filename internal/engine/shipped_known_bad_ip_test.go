// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeKnownBadIPs is internal/detect/known_bad_ip_test.go's helper of the
// same name: an explicit fixed match set, so a test controls matches
// deterministically without a real Spamhaus/Emerging Threats fetch.
type fakeKnownBadIPs struct {
	matches map[string][2]string // ip -> {label, cidr}
}

func newFakeKnownBadIPs() *fakeKnownBadIPs {
	return &fakeKnownBadIPs{matches: make(map[string][2]string)}
}

func (f *fakeKnownBadIPs) setMatch(ip, label, cidr string) {
	f.matches[ip] = [2]string{label, cidr}
}

func (f *fakeKnownBadIPs) MatchIP(ip string) (label, cidr string, ok bool) {
	m, ok := f.matches[ip]
	return m[0], m[1], ok
}

func newShippedKnownBadIPDefinition(t *testing.T, fs *flags.Store, bl KnownBadIPLookup) *knownBadIPDefinition {
	t.Helper()
	def := Definition{
		ID:          "known_bad_ip",
		Name:        "Known bad IP",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      Params{"confidence": knownBadIPConfidence},
		ParamSchema: KnownBadIPParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{
		KnownBad: bl,
		Flags:    FlagsConfidenceFloorRaiser(fs),
	})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(known_bad_ip): %v", err)
	}
	d := built.(*knownBadIPDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func findFlag(fs *flags.Store, target string, typ flags.Type) *flags.Flag {
	for _, f := range fs.List() {
		if f.Target == target && f.Type == typ {
			f := f
			return &f
		}
	}
	return nil
}

func TestShippedKnownBadIPRaisesFlagOnMatch(t *testing.T) {
	fs := newTestFlagsStore(t)
	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", "Spamhaus DROP", "198.51.100.0/24")
	d := newShippedKnownBadIPDefinition(t, fs, bl)

	e := psEvtCountry("198.51.100.4", "NL", 22, time.Now())
	d.Evaluate(e)

	f := findFlag(fs, "198.51.100.4", flags.TypeKnownBadIP)
	if f == nil {
		t.Fatal("expected a known_bad_ip flag to be raised")
	}
	if f.Confidence == nil || *f.Confidence != knownBadIPConfidence {
		t.Errorf("Confidence = %v, want %d", f.Confidence, knownBadIPConfidence)
	}
	if want := "matches Spamhaus DROP (198.51.100.0/24)"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "NL" {
		t.Errorf("Country = %q, want the source's", f.Country)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}
}

func TestShippedKnownBadIPNoFlagWithoutMatch(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedKnownBadIPDefinition(t, fs, newFakeKnownBadIPs()) // no matches configured

	d.Evaluate(psEvt("198.51.100.4", 22, time.Now()))
	if f := findFlag(fs, "198.51.100.4", flags.TypeKnownBadIP); f != nil {
		t.Errorf("expected no known_bad_ip flag, got %+v", f)
	}
}

// TestShippedKnownBadIPSkippedForInternalSource pins the public-source
// guard as the thing that actually enforces "a private address is never
// matched", rather than trusting feed content to never contain one.
func TestShippedKnownBadIPSkippedForInternalSource(t *testing.T) {
	fs := newTestFlagsStore(t)
	bl := newFakeKnownBadIPs()
	bl.setMatch("192.168.1.50", "Spamhaus DROP", "192.168.0.0/16")
	d := newShippedKnownBadIPDefinition(t, fs, bl)

	d.Evaluate(psEvt("192.168.1.50", 22, time.Now()))
	if f := findFlag(fs, "192.168.1.50", flags.TypeKnownBadIP); f != nil {
		t.Errorf("expected no known_bad_ip flag for an internal source, got %+v", f)
	}
}

func TestShippedKnownBadIPNilLookupIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedKnownBadIPDefinition(t, fs, nil)

	d.Evaluate(psEvt("198.51.100.4", 22, time.Now())) // must not panic
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no blocklist configured to be inert, got %+v", got)
	}
}

// TestShippedKnownBadIPReinforcesPreviouslyRaisedFlag is
// internal/detect/known_bad_ip_test.go's
// TestKnownBadIPReinforcesPreviouslyRaisedFlagOnLaterEvent: a flag
// raised on an earlier event is reinforced when a later event from the
// same source matches.
func TestShippedKnownBadIPReinforcesPreviouslyRaisedFlag(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.4"
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	if f := findFlag(fs, ip, flags.TypeLowSlowScan); f == nil || f.ReputationFloor != nil {
		t.Fatalf("setup: expected a seeded flag with no floor yet, got %+v", f)
	}

	bl := newFakeKnownBadIPs()
	bl.setMatch(ip, "Spamhaus DROP", "198.51.100.0/24")
	d := newShippedKnownBadIPDefinition(t, fs, bl)
	d.Evaluate(psEvt(ip, 22, now.Add(time.Second)))

	f := findFlag(fs, ip, flags.TypeLowSlowScan)
	if f.ReputationFloor == nil || *f.ReputationFloor != knownBadIPConfidence {
		t.Errorf("expected the low_slow_scan flag's ReputationFloor to be reinforced to %d, got %v", knownBadIPConfidence, f.ReputationFloor)
	}
	if f.Confidence == nil || *f.Confidence < knownBadIPConfidence {
		t.Errorf("expected its Confidence to be at least %d after reinforcement, got %v", knownBadIPConfidence, f.Confidence)
	}
}

// TestShippedKnownBadIPDoesNotReinforceUnrelatedTargetTypes pins the
// exclusion reinforcedFlagTypes exists for: distributed_brute_force's
// target is a port label, not a source address, so a blocklist match on
// some source must never touch it.
func TestShippedKnownBadIPDoesNotReinforceUnrelatedTargetTypes(t *testing.T) {
	fs := newTestFlagsStore(t)
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 20, time.Now())

	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", "Spamhaus DROP", "198.51.100.0/24")
	d := newShippedKnownBadIPDefinition(t, fs, bl)
	d.Evaluate(psEvt("198.51.100.4", 22, time.Now()))

	f := findFlag(fs, "port 22", flags.TypeDistributedBruteForce)
	if f == nil {
		t.Fatal("expected the pre-existing flag to still exist")
	}
	if f.Confidence == nil || *f.Confidence != 20 {
		t.Errorf("expected distributed_brute_force's confidence to stay untouched at 20, got %v", f.Confidence)
	}
}

// TestShippedKnownBadIPDeclaresReinforcementOrder pins the declaration
// itself. The end-to-end consequence is pinned separately, below.
func TestShippedKnownBadIPDeclaresReinforcementOrder(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedKnownBadIPDefinition(t, fs, newFakeKnownBadIPs())

	o, ok := Evaluated(d).(Ordered)
	if !ok {
		t.Fatal("known_bad_ip must implement Ordered -- its reinforcement pass is meaningless before the definitions it reinforces")
	}
	if got := o.EvaluationOrder(); got != ReinforcementOrder {
		t.Errorf("EvaluationOrder = %d, want ReinforcementOrder (%d)", got, ReinforcementOrder)
	}
}

// TestShippedKnownBadIPReinforcesAFlagRaisedByTheSameEvent is the
// end-to-end re-pin this port owes.
//
// internal/detect proved this by raising a flag with one detector and
// reinforcing it with another inside a single Observe call, which was
// possible because both were function calls in one function. As detectors
// moved to the engine one at a time, internal/detect ran out of
// cheap-to-trigger source-keyed flag-raisers and its version of this test
// had to be narrowed to a pre-seeded flag -- explicitly recorded at the
// time as an unpreserved pin, to be restored once known_bad_ip itself
// ported. This is that restoration.
//
// It is deliberately driven through a real Engine with two real shipped
// definitions rather than through the chassis-level ordering test (which
// uses purpose-built fakes and proves only that the sort is applied).
// What is under test here is the composition: that port_scan's flag, from
// THIS event, already exists by the time known_bad_ip's reinforcement
// pass looks for it. Registration order is deliberately reversed against
// evaluation order, so a chassis that ignored Ordered would fail this.
func TestShippedKnownBadIPReinforcesAFlagRaisedByTheSameEvent(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.77"

	bl := newFakeKnownBadIPs()
	bl.setMatch(ip, "Spamhaus DROP", "198.51.100.0/24")
	reinforcer := newShippedKnownBadIPDefinition(t, fs, bl)
	portScan := newShippedPortScanDefinition(t, fs, Scope{})

	eng := New()
	eng.Register(reinforcer) // registered FIRST, deliberately
	eng.Register(portScan)

	t0 := time.Now()
	// One event short of port_scan's shipped threshold (15 distinct
	// ports): no port_scan flag yet, so known_bad_ip's pass has nothing
	// of port_scan's to reinforce.
	for i := 0; i < 14; i++ {
		evaluateThroughEngine(t, eng, psEvt(ip, 1000+i, t0.Add(time.Duration(i)*time.Second)))
	}
	if f := findFlag(fs, ip, flags.TypePortScan); f != nil {
		t.Fatalf("setup: expected no port_scan flag below the threshold, got %+v", f)
	}

	// The fifteenth event crosses port_scan's threshold AND matches the
	// blocklist. Both definitions act on this one event; the reinforcement
	// must see the flag the other one just raised.
	evaluateThroughEngine(t, eng, psEvt(ip, 1014, t0.Add(14*time.Second)))

	ps := findFlag(fs, ip, flags.TypePortScan)
	if ps == nil {
		t.Fatal("expected port_scan to fire on the fifteenth distinct port")
	}
	if ps.ReputationFloor == nil || *ps.ReputationFloor != knownBadIPConfidence {
		t.Fatalf("port_scan's flag was not reinforced by the same event's blocklist match: ReputationFloor = %v, want %d -- known_bad_ip ran before the definition it reinforces",
			ps.ReputationFloor, knownBadIPConfidence)
	}
	if ps.Confidence == nil || *ps.Confidence < knownBadIPConfidence {
		t.Errorf("port_scan's Confidence = %v, want at least the reinforced floor %d", ps.Confidence, knownBadIPConfidence)
	}
	if kb := findFlag(fs, ip, flags.TypeKnownBadIP); kb == nil {
		t.Error("expected known_bad_ip to also raise its own flag on the same event")
	}
}

// evaluateThroughEngine drives one event through a real Engine and waits
// for its evaluation goroutine to have processed it -- the ordering under
// test is Engine.evaluateEvent's, so these tests must not call Evaluate
// on the definitions directly.
func evaluateThroughEngine(t *testing.T, eng *Engine, e store.Event) {
	t.Helper()
	eng.evaluateEvent(e)
}
