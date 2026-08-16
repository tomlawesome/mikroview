// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

// findFlag was internal/detect/known_bad_ip_test.go's helper; that file
// moved to internal/engine with its detector (issue #405), so the helper
// lives with the last tests still using it.
func findFlag(fs *flags.Store, target string, typ flags.Type) *flags.Flag {
	for _, f := range fs.List() {
		if f.Target == target && f.Type == typ {
			f := f
			return &f
		}
	}
	return nil
}

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

// The tests below used critical_port, then activity_spike, purely as a
// convenient, cheap-to-trigger flag-raiser for a public source IP -- none
// of them are actually about either detector's own behaviour, just about
// observeNetClass's RaiseConfidenceFloor reinforcement path finding *some*
// already-raised, source-IP-keyed flag. Now that activity_spike has also
// moved to internal/engine (issue #405) and internal/detect no longer
// evaluates it, the remaining reinforceable types (low_slow_scan,
// off_hours_activity) both need an elaborate multi-signal or multi-day
// setup to fire behaviorally, and known_bad_ip/netclass are themselves
// due to port onto internal/engine shortly, at which point these tests
// move there anyway -- so chasing another live detector here isn't worth
// it (see known_bad_ip_test.go's own header comment, which took the same
// call). Every test below that only needs "a reinforceable flag already
// exists for this source" pre-seeds a flags.Store entry directly
// (flags.TypeLowSlowScan, still a plain-source-IP-keyed member of
// knownBadReinforcedTypes) instead of raising one behaviorally;
// observeNetClass runs unconditionally at the end of Observe for a public
// source reaching a private destination, so the reinforcement path itself
// is exercised exactly as before. Every ReputationFloor/Confidence
// assertion is unchanged; only how the flag got there is.

func TestNetClassReinforcesTorMatch(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist")
	}
	if asFlag.ReputationFloor == nil || *asFlag.ReputationFloor != reputation.TorExitNodeFloor {
		t.Errorf("expected ReputationFloor to be reinforced to %d (Tor), got %v", reputation.TorExitNodeFloor, asFlag.ReputationFloor)
	}
}

func TestNetClassReinforcesVPNMatch(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, vpnMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist")
	}
	if asFlag.ReputationFloor == nil || *asFlag.ReputationFloor != netclassVPNFloor {
		t.Errorf("expected ReputationFloor to be reinforced to %d (VPN), got %v", netclassVPNFloor, asFlag.ReputationFloor)
	}
}

// TestNetClassDatacenterNeverReinforces is the direct consequence of
// #114's research: a datacenter match alone covers >10% of routable
// IPv4, so it stays display-only -- CategoryDatacenter must never reach
// RaiseConfidenceFloor, regardless of direction.
func TestNetClassDatacenterNeverReinforces(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, datacenterMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist (independent of netclass)")
	}
	if asFlag.ReputationFloor != nil {
		t.Errorf("expected no ReputationFloor from a datacenter match, got %v", asFlag.ReputationFloor)
	}
}

// TestNetClassPrivacyRelayNeverReinforces is the other never-reinforces
// category, for the opposite reason to datacenter: this category exists
// specifically to identify traffic that must never read as suspicious
// (Apple Private Relay / Cloudflare WARP -- ordinary consumer traffic).
func TestNetClassPrivacyRelayNeverReinforces(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, privacyRelayMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist (independent of netclass)")
	}
	if asFlag.ReputationFloor != nil {
		t.Errorf("expected no ReputationFloor from a Private Relay match, got %v", asFlag.ReputationFloor)
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
// destination (not this LAN) is not "arriving here" either. Retargeted
// per this file's header comment -- the seeded flag's existence doesn't
// depend on DstIP at all, so overriding it to a second public address
// here only affects observeNetClass's direction gate, not whether the
// flag exists.
func TestNetClassSkippedWhenDestinationIsAlsoPublic(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)

	e := evt(ip, 22, now)
	e.DstIP = "203.0.113.9" // also public, not this LAN
	d.Observe(e)

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist")
	}
	if asFlag.ReputationFloor != nil {
		t.Errorf("expected no reinforcement when the destination is also public, got %v", asFlag.ReputationFloor)
	}
}

// TestNetClassConfidenceNeverDecreases is the invariant #114's owner
// asked to have as a test, not a comment: reinforcing with a lower floor
// after a higher one is already set must never lower the flag's
// confidence. Exercised directly at this package's call site, on top of
// whatever RaiseConfidenceFloor itself already guarantees. Retargeted
// per this file's header comment.
func TestNetClassConfidenceNeverDecreases(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch()) // higher floor (reputation.TorExitNodeFloor)

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil || asFlag.Confidence == nil || *asFlag.Confidence != reputation.TorExitNodeFloor {
		t.Fatalf("expected Confidence == %d after the Tor match, got %+v", reputation.TorExitNodeFloor, asFlag)
	}

	// Reclassify the same source as VPN (a lower floor) and observe
	// again -- the already-raised flag's confidence must not drop.
	nc.setMatch(ip, vpnMatch())
	d.Observe(evt(ip, 200, now.Add(time.Second)))

	asFlag = findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag.Confidence == nil || *asFlag.Confidence < reputation.TorExitNodeFloor {
		t.Errorf("Confidence dropped from %d to %v after a lower-floor reclassification -- RaiseConfidenceFloor must never lower a score", reputation.TorExitNodeFloor, asFlag.Confidence)
	}
}

// TestNetClassRespectsPermanentExclusion proves #114's whitelisting
// requirement is already satisfied by the existing #111 mechanism, with
// no new one-off suppression path needed: once a (type, target) pair is
// permanently excluded, no flag is raised for it even when the source is
// classified (Tor). Retargeted per this file's header comment -- the
// original also proved the pair stays unflagged when the source
// simultaneously crosses a behavioral threshold in the same events; that
// half is not preserved here (flags.Store.add already refuses to create
// *any* flag, seeded or behavioral, for an excluded (type, target), so
// there is nothing left to pre-seed once Exclude has been called first,
// and re-seeding before excluding would just be testing Exclude() against
// an existing entry, not this call site). What remains -- that
// observeNetClass's RaiseConfidenceFloor call does not itself resurrect
// or create a flag for an excluded target -- is still the same guarantee
// RaiseConfidenceFloor's own doc comment states generally.
func TestNetClassRespectsPermanentExclusion(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.Exclude(flags.TypeLowSlowScan, ip)

	nc := newFakeNetClass()
	nc.setMatch(ip, torMatch())

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithNetClass(nc)
	d.Observe(evt(ip, 22, time.Now()))

	if f := findFlag(fs, ip, flags.TypeLowSlowScan); f != nil {
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
