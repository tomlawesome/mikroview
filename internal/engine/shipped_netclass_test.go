// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeNetClass is internal/detect/netclass_test.go's helper of the same
// name, in NetClassLookup's terms.
type fakeNetClass struct {
	matches map[string]netclass.Class
}

func newFakeNetClass() *fakeNetClass {
	return &fakeNetClass{matches: make(map[string]netclass.Class)}
}

func (f *fakeNetClass) setMatch(ip string, c netclass.Class) { f.matches[ip] = c }

func (f *fakeNetClass) LookupClass(ip string) (bool, string, string) {
	c, ok := f.matches[ip]
	if !ok || !c.Matched {
		return false, "", ""
	}
	return true, string(c.Category), c.Label
}

func torMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryTor, Label: "Tor exit nodes"}
}

func vpnMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryVPN, Label: "X4BNet VPN"}
}

func datacenterMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryDatacenter, Label: "X4BNet datacenter"}
}

func privacyRelayMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryPrivacyRelay, Label: "iCloud Private Relay"}
}

func newShippedNetClassDefinition(t *testing.T, fs *flags.Store, nc NetClassLookup) *netClassDefinition {
	t.Helper()
	def := Definition{
		ID:          "netclass",
		Name:        "Network class reinforcement",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      Params{"torFloor": reputation.TorExitNodeFloor, "vpnFloor": netclassVPNFloor},
		ParamSchema: NetClassParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{
		NetClass: nc,
		Flags:    FlagsConfidenceFloorRaiser(fs),
	})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(netclass): %v", err)
	}
	d := built.(*netClassDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

// inboundEvt is an external source reaching a LAN destination -- the one
// direction this definition looks at.
func inboundEvt(srcIP string, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: "192.168.1.10", DstPort: 22, ReceivedAt: at}
}

func seededPortScanFlag(t *testing.T, fs *flags.Store, ip string, at time.Time) {
	t.Helper()
	fs.AddWithDetail(flags.TypePortScan, ip, "seeded", 10, flags.Evidence{}, "", at)
}

func TestShippedNetClassTorReinforcesToTheTorFloor(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.9"
	now := time.Now()
	seededPortScanFlag(t, fs, ip, now)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())
	newShippedNetClassDefinition(t, fs, nc).Evaluate(inboundEvt(ip, now))

	f := findFlag(fs, ip, flags.TypePortScan)
	if f.ReputationFloor == nil || *f.ReputationFloor != reputation.TorExitNodeFloor {
		t.Errorf("ReputationFloor = %v, want %d (Tor exit)", f.ReputationFloor, reputation.TorExitNodeFloor)
	}
}

func TestShippedNetClassVPNReinforcesToTheVPNFloor(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.9"
	now := time.Now()
	seededPortScanFlag(t, fs, ip, now)

	nc := newFakeNetClass()
	nc.setMatch(ip, vpnMatch())
	newShippedNetClassDefinition(t, fs, nc).Evaluate(inboundEvt(ip, now))

	f := findFlag(fs, ip, flags.TypePortScan)
	if f.ReputationFloor == nil || *f.ReputationFloor != netclassVPNFloor {
		t.Errorf("ReputationFloor = %v, want %d (commercial VPN exit)", f.ReputationFloor, netclassVPNFloor)
	}
}

// TestShippedNetClassDatacenterAndPrivacyRelayNeverReinforce pins the two
// categories that deliberately contribute nothing: datacenter space is
// too broad to mean anything, and privacy relays exist specifically to
// identify traffic that must never read as suspicious.
func TestShippedNetClassDatacenterAndPrivacyRelayNeverReinforce(t *testing.T) {
	for name, class := range map[string]netclass.Class{
		"datacenter":    datacenterMatch(),
		"privacy relay": privacyRelayMatch(),
	} {
		t.Run(name, func(t *testing.T) {
			fs := newTestFlagsStore(t)
			ip := "198.51.100.9"
			now := time.Now()
			seededPortScanFlag(t, fs, ip, now)

			nc := newFakeNetClass()
			nc.setMatch(ip, class)
			newShippedNetClassDefinition(t, fs, nc).Evaluate(inboundEvt(ip, now))

			if f := findFlag(fs, ip, flags.TypePortScan); f.ReputationFloor != nil {
				t.Errorf("expected %s to contribute no floor, got %d", name, *f.ReputationFloor)
			}
		})
	}
}

// TestShippedNetClassRaisesNoFlagOfItsOwn pins #114's explicit non-goal:
// a classified but otherwise quiet source must produce nothing at all.
func TestShippedNetClassRaisesNoFlagOfItsOwn(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.9"
	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())

	newShippedNetClassDefinition(t, fs, nc).Evaluate(inboundEvt(ip, time.Now()))

	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a classified but quiet source to raise nothing, got %+v", got)
	}
}

// TestShippedNetClassIsDirectionGated pins the refinement #114's research
// called the highest-value one: only inbound (public source, private
// destination) is classified.
func TestShippedNetClassIsDirectionGated(t *testing.T) {
	cases := map[string]store.Event{
		"internal source":        {SrcIP: "192.168.1.50", DstIP: "192.168.1.10", DstPort: 22},
		"external destination":   {SrcIP: "198.51.100.9", DstIP: "203.0.113.5", DstPort: 22},
		"no destination address": {SrcIP: "198.51.100.9", DstPort: 22},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			fs := newTestFlagsStore(t)
			now := time.Now()
			e.ReceivedAt = now
			seededPortScanFlag(t, fs, e.SrcIP, now)

			nc := newFakeNetClass()
			nc.setMatch(e.SrcIP, torMatch())
			newShippedNetClassDefinition(t, fs, nc).Evaluate(e)

			if f := findFlag(fs, e.SrcIP, flags.TypePortScan); f.ReputationFloor != nil {
				t.Errorf("expected no reinforcement for %s, got floor %d", name, *f.ReputationFloor)
			}
		})
	}
}

func TestShippedNetClassNilLookupIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.9"
	now := time.Now()
	seededPortScanFlag(t, fs, ip, now)

	newShippedNetClassDefinition(t, fs, nil).Evaluate(inboundEvt(ip, now)) // must not panic

	if f := findFlag(fs, ip, flags.TypePortScan); f.ReputationFloor != nil {
		t.Errorf("expected no netclass source configured to be inert, got floor %d", *f.ReputationFloor)
	}
}

func TestShippedNetClassDoesNotReinforceUnrelatedTargetTypes(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.9"
	now := time.Now()
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 20, now)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())
	newShippedNetClassDefinition(t, fs, nc).Evaluate(inboundEvt(ip, now))

	f := findFlag(fs, "port 22", flags.TypeDistributedBruteForce)
	if f.Confidence == nil || *f.Confidence != 20 {
		t.Errorf("expected distributed_brute_force's confidence to stay untouched at 20, got %v", f.Confidence)
	}
}

func TestShippedNetClassDeclaresReinforcementOrder(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedNetClassDefinition(t, fs, newFakeNetClass())

	o, ok := Evaluated(d).(Ordered)
	if !ok {
		t.Fatal("netclass must implement Ordered -- its reinforcement pass is meaningless before the definitions it reinforces")
	}
	if got := o.EvaluationOrder(); got != ReinforcementOrder {
		t.Errorf("EvaluationOrder = %d, want ReinforcementOrder (%d)", got, ReinforcementOrder)
	}
}

// TestShippedNetClassReinforcesAFlagRaisedByTheSameEvent is the second
// half of the end-to-end ordering re-pin this port owes (see
// TestShippedKnownBadIPReinforcesAFlagRaisedByTheSameEvent for the
// first). Both reinforcement definitions declare the same rank, so both
// need proving against a real flag-raiser rather than only against the
// chassis's own fakes -- a rank declared but not honoured for one of them
// would be invisible otherwise.
//
// Registration order is again deliberately adverse: netclass registers
// first, and sorts before "port_scan" by ID, so a chassis ignoring
// Ordered would evaluate it before the definition it reinforces and this
// test would fail.
func TestShippedNetClassReinforcesAFlagRaisedByTheSameEvent(t *testing.T) {
	fs := newTestFlagsStore(t)
	ip := "198.51.100.88"

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())
	reinforcer := newShippedNetClassDefinition(t, fs, nc)
	portScan := newShippedPortScanDefinition(t, fs, Scope{})

	eng := New()
	eng.Register(reinforcer) // registered FIRST, deliberately
	eng.Register(portScan)

	t0 := time.Now()
	// One short of port_scan's shipped threshold of 15 distinct ports.
	for i := 0; i < 14; i++ {
		e := inboundEvt(ip, t0.Add(time.Duration(i)*time.Second))
		e.DstPort = 1000 + i
		evaluateThroughEngine(t, eng, e)
	}
	if f := findFlag(fs, ip, flags.TypePortScan); f != nil {
		t.Fatalf("setup: expected no port_scan flag below the threshold, got %+v", f)
	}

	// The fifteenth event crosses port_scan's threshold; netclass must see
	// the flag it just raised.
	last := inboundEvt(ip, t0.Add(14*time.Second))
	last.DstPort = 1014
	evaluateThroughEngine(t, eng, last)

	ps := findFlag(fs, ip, flags.TypePortScan)
	if ps == nil {
		t.Fatal("expected port_scan to fire on the fifteenth distinct port")
	}
	if ps.ReputationFloor == nil || *ps.ReputationFloor != reputation.TorExitNodeFloor {
		t.Fatalf("port_scan's flag was not reinforced by the same event's Tor classification: ReputationFloor = %v, want %d -- netclass ran before the definition it reinforces",
			ps.ReputationFloor, reputation.TorExitNodeFloor)
	}
}

// TestShippedNetClassIsNonReplayable pins the declaration: this
// definition has no emissions, so there is no count a receipt could
// report.
func TestShippedNetClassIsNonReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedNetClassDefinition(t, fs, newFakeNetClass())

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify netclass")
	}
	if receiptCapable {
		t.Fatal("expected netclass to declare itself non-replayable")
	}
	if reason == "" {
		t.Error("a non-replayable declaration with no reason is the thing the contract exists to prevent")
	}
}
