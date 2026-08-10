// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

// fakeNetClass is a netClassLookup that matches an explicit fixed set of
// IPs -- same "inject a fake" approach known_bad_ip_test.go's
// fakeKnownBadIPs establishes for knownBadIPLookup.
type fakeNetClass struct {
	matches map[string]netclass.Class
}

func newFakeNetClass() *fakeNetClass {
	return &fakeNetClass{matches: make(map[string]netclass.Class)}
}

func (f *fakeNetClass) setMatch(ip string, c netclass.Class) {
	f.matches[ip] = c
}

func (f *fakeNetClass) Lookup(ip string) netclass.Class {
	return f.matches[ip]
}

func torMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryTor, Label: "Tor exit nodes"}
}

func vpnMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryVPN, Label: "X4BNet VPN"}
}

func datacenterMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryDatacenter, Label: "AWS", Detail: "eu-west-1"}
}

func privacyRelayMatch() netclass.Class {
	return netclass.Class{Matched: true, Category: netclass.CategoryPrivacyRelay, Label: "Apple Private Relay"}
}

func TestNetClassReinforcesTorMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised")
	}
	if psFlag.ReputationFloor == nil || *psFlag.ReputationFloor != reputation.TorExitNodeFloor {
		t.Errorf("expected ReputationFloor to be reinforced to %d (Tor), got %v", reputation.TorExitNodeFloor, psFlag.ReputationFloor)
	}
}

func TestNetClassReinforcesVPNMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", vpnMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised")
	}
	if psFlag.ReputationFloor == nil || *psFlag.ReputationFloor != netclassVPNFloor {
		t.Errorf("expected ReputationFloor to be reinforced to %d (VPN), got %v", netclassVPNFloor, psFlag.ReputationFloor)
	}
}

// TestNetClassDatacenterNeverReinforces is the direct consequence of
// #114's research: a datacenter match alone covers >10% of routable
// IPv4, so it stays display-only -- CategoryDatacenter must never reach
// RaiseConfidenceFloor, regardless of direction.
func TestNetClassDatacenterNeverReinforces(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", datacenterMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised (behaviorally, independent of netclass)")
	}
	if psFlag.ReputationFloor != nil {
		t.Errorf("expected no ReputationFloor from a datacenter match, got %v", psFlag.ReputationFloor)
	}
}

// TestNetClassPrivacyRelayNeverReinforces is the other never-reinforces
// category, for the opposite reason to datacenter: this category exists
// specifically to identify traffic that must never read as suspicious
// (Apple Private Relay / Cloudflare WARP -- ordinary consumer traffic).
func TestNetClassPrivacyRelayNeverReinforces(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", privacyRelayMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised (behaviorally, independent of netclass)")
	}
	if psFlag.ReputationFloor != nil {
		t.Errorf("expected no ReputationFloor from a Private Relay match, got %v", psFlag.ReputationFloor)
	}
}

// TestNetClassNeverRaisesItsOwnFlag is #114's explicit non-goal, checked
// directly: a Tor match on a source that never crosses any behavioral
// threshold must produce no flag of any kind. RaiseConfidenceFloor
// no-ops against a target fs doesn't already know about, so this is
// really a test of that contract holding at this call site too.
func TestNetClassNeverRaisesItsOwnFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	d.Observe(evt("198.51.100.4", 443, time.Now()))

	if got := fs.List(); len(got) != 0 {
		t.Errorf("expected zero flags from a lone classified-but-otherwise-quiet event, got %+v", got)
	}
}

// TestNetClassSkippedForOutboundTraffic is #114's highest-value
// refinement, checked directly: a LAN source reaching a classified
// public destination is outbound, and must contribute nothing --
// regardless of category, even Tor.
func TestNetClassSkippedForOutboundTraffic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	// Internal-recon/outbound-anomaly need a LAN source reaching many
	// distinct destinations to raise on their own; kept high so the
	// event below can't accidentally cross either threshold and
	// contaminate the result -- this test is purely about netclass
	// itself never firing outbound, not about those detectors.
	cfg.OutboundAnomalyThreshold = 1000
	cfg.InternalReconThreshold = 1000

	nc := newFakeNetClass()
	// The classified address is now the DESTINATION, not the source.
	nc.setMatch("203.0.113.9", torMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	e := evt("192.168.1.50", 443, time.Now())
	e.DstIP = "203.0.113.9"
	d.Observe(e)

	if got := fs.List(); len(got) != 0 {
		t.Errorf("expected zero flags from outbound traffic to a classified destination, got %+v", got)
	}
}

// TestNetClassSkippedWhenDestinationIsAlsoPublic guards the other half
// of the direction gate: a classified public source reaching a public
// destination (not this LAN) is not "arriving here" either.
func TestNetClassSkippedWhenDestinationIsAlsoPublic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		e := evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second))
		e.DstIP = "203.0.113.9" // also public, not this LAN
		d.Observe(e)
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised behaviorally")
	}
	if psFlag.ReputationFloor != nil {
		t.Errorf("expected no reinforcement when the destination is also public, got %v", psFlag.ReputationFloor)
	}
}

// TestNetClassConfidenceNeverDecreases is the invariant #114's owner
// asked to have as a test, not a comment: reinforcing with a lower floor
// after a higher one is already set must never lower the flag's
// confidence. Exercised directly at this package's call site, on top of
// whatever RaiseConfidenceFloor itself already guarantees.
func TestNetClassConfidenceNeverDecreases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch()) // higher floor (reputation.TorExitNodeFloor)

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil || psFlag.Confidence == nil || *psFlag.Confidence != reputation.TorExitNodeFloor {
		t.Fatalf("expected Confidence == %d after the Tor match, got %+v", reputation.TorExitNodeFloor, psFlag)
	}

	// Reclassify the same source as VPN (a lower floor) and observe
	// again -- the already-raised flag's confidence must not drop.
	nc.setMatch("198.51.100.4", vpnMatch())
	d.Observe(evt("198.51.100.4", 200, now.Add(10*time.Second)))

	psFlag = findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag.Confidence == nil || *psFlag.Confidence < reputation.TorExitNodeFloor {
		t.Errorf("Confidence dropped from %d to %v after a lower-floor reclassification -- RaiseConfidenceFloor must never lower a score", reputation.TorExitNodeFloor, psFlag.Confidence)
	}
}

// TestNetClassRespectsPermanentExclusion proves #114's whitelisting
// requirement is already satisfied by the existing #111 mechanism, with
// no new one-off suppression path needed: once a (type, target) pair is
// permanently excluded, no flag is raised for it even when the source
// is both classified (Tor) and behaviorally crosses the threshold in the
// same events.
func TestNetClassRespectsPermanentExclusion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch())

	d, fs := newTestDetector(t, cfg)
	d.WithNetClass(nc)
	fs.Exclude(flags.TypePortScan, "198.51.100.4")

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	if f := findFlag(fs, "198.51.100.4", flags.TypePortScan); f != nil {
		t.Errorf("expected the excluded pair to stay unflagged despite a Tor match, got %+v", f)
	}
}

// TestNetClassDoesNotReinforceUnrelatedTargetTypes mirrors known_bad_ip_
// test.go's equivalent: a flag type whose target is not a plain source
// IP must never be touched just because this source IP happens to be
// classified.
func TestNetClassDoesNotReinforceUnrelatedTargetTypes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 1000
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 20, time.Now())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	nc := newFakeNetClass()
	nc.setMatch("198.51.100.4", torMatch())
	d.WithNetClass(nc)

	d.Observe(evt("198.51.100.4", 22, time.Now()))

	f := findFlag(fs, "port 22", flags.TypeDistributedBruteForce)
	if f == nil {
		t.Fatal("expected the pre-existing flag to still exist")
	}
	if f.Confidence == nil || *f.Confidence != 20 {
		t.Errorf("expected TypeDistributedBruteForce's confidence to stay untouched at 20, got %v", f.Confidence)
	}
}
