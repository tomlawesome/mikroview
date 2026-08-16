// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/blocklist"
	"github.com/tomlawesome/mikroview/internal/flags"
)

// fakeKnownBadIPs is a knownBadIPLookup that matches an explicit fixed
// set of IPs -- lets tests control matches deterministically without a
// real internal/blocklist fetch, same "inject a fake" approach
// reputation_test.go's fakeReputation already establishes for
// reputationLookup.
type fakeKnownBadIPs struct {
	matches map[string]blocklist.MatchResult
}

func newFakeKnownBadIPs() *fakeKnownBadIPs {
	return &fakeKnownBadIPs{matches: make(map[string]blocklist.MatchResult)}
}

func (f *fakeKnownBadIPs) setMatch(ip string, m blocklist.MatchResult) {
	f.matches[ip] = m
}

func (f *fakeKnownBadIPs) Match(ip string) (blocklist.MatchResult, bool) {
	m, ok := f.matches[ip]
	return m, ok
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

func TestKnownBadIPRaisesFlagOnMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})

	d, fs := newTestDetector(t, cfg)
	d.WithKnownBadIPs(bl)

	d.Observe(evt("198.51.100.4", 22, time.Now()))

	f := findFlag(fs, "198.51.100.4", flags.TypeKnownBadIP)
	if f == nil {
		t.Fatal("expected a TypeKnownBadIP flag to be raised")
	}
	if f.Confidence == nil || *f.Confidence != knownBadIPConfidence {
		t.Errorf("Confidence = %v, want %d", f.Confidence, knownBadIPConfidence)
	}
	if f.Detail == "" {
		t.Error("expected a non-empty Detail describing the match")
	}
}

func TestKnownBadIPNoFlagWithoutMatch(t *testing.T) {
	cfg := DefaultConfig()
	bl := newFakeKnownBadIPs() // no matches configured

	d, fs := newTestDetector(t, cfg)
	d.WithKnownBadIPs(bl)

	d.Observe(evt("198.51.100.4", 22, time.Now()))

	if f := findFlag(fs, "198.51.100.4", flags.TypeKnownBadIP); f != nil {
		t.Errorf("expected no TypeKnownBadIP flag, got %+v", f)
	}
}

func TestKnownBadIPSkippedForInternalSource(t *testing.T) {
	cfg := DefaultConfig()
	bl := newFakeKnownBadIPs()
	// Even if the fake were (incorrectly) configured to match a private
	// address, observeKnownBadIP's isPublic guard must still block it --
	// a real blocklist would never contain a private range, but the
	// guard is what actually enforces that, not feed content alone.
	bl.setMatch("192.168.1.50", blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "192.168.0.0/16"})

	d, fs := newTestDetector(t, cfg)
	d.WithKnownBadIPs(bl)

	d.Observe(evt("192.168.1.50", 22, time.Now()))

	if f := findFlag(fs, "192.168.1.50", flags.TypeKnownBadIP); f != nil {
		t.Errorf("expected no TypeKnownBadIP flag for an internal source, got %+v", f)
	}
}

// TestKnownBadIPReinforcesSameEventCriticalPortFlag and
// TestKnownBadIPReinforcesPreviouslyRaisedFlagOnLaterEvent used
// critical_port, then activity_spike, purely as a convenient,
// cheap-to-trigger flag-raiser for a public source IP -- neither test is
// actually about either detector's own behaviour, just about
// observeKnownBadIP's RaiseConfidenceFloor reinforcement path finding
// *some* already-raised, source-IP-keyed flag. Now that activity_spike has
// also moved to internal/engine (issue #405) and internal/detect no
// longer evaluates it, the two remaining reinforceable types
// (low_slow_scan, off_hours_activity) both need an elaborate multi-signal
// or multi-day setup to fire behaviorally, and known_bad_ip/netclass are
// themselves due to port onto internal/engine shortly, at which point
// these tests move there anyway -- so chasing another live detector here
// isn't worth it. Both tests below pre-seed a flags.Store entry directly
// (flags.TypeLowSlowScan is still a plausible source-IP-keyed target --
// see knownBadReinforcedTypes) rather than raising one behaviorally;
// observeKnownBadIP runs unconditionally at the end of Observe for any
// public source, so the reinforcement path itself is exercised exactly as
// before. Every ReputationFloor/Confidence assertion is unchanged; only
// how the flag got there is.

// TestKnownBadIPReinforcesAnAlreadyRaisedFlag is
// TestKnownBadIPReinforcesSameEventActivitySpikeFlag, retargeted per this
// file's header comment above -- renamed because it can no longer prove
// what its old name claimed.
//
// The original test's real point was narrower than "a match reinforces an
// existing flag" (TestKnownBadIPReinforcesPreviouslyRaisedFlagOnLaterEvent,
// below, already proves that): it proved that a flag raised by *another
// detector during the same Observe call* is already visible to
// observeKnownBadIP's reinforcement pass, not just one raised on an
// earlier call -- see Observe's own doc comment on why observeKnownBadIP
// runs last. internal/detect can no longer produce that same-call
// ordering for a public source at any reasonable test cost (every
// detector still live here either doesn't apply to a bare source-IP
// target or needs a multi-event/multi-day setup that would no longer be
// happening within one Observe call from a single triggering event).
// That half of the pin is not preserved by this test -- it is now
// exercised by internal/engine's Ordered contract instead
// (ReinforcementOrder, engine.go, pinned end-to-end by
// internal/engine/ordering_test.go's
// TestEvaluationOrderIsDeterministicAndRespectsOrdered), and will be
// re-pinned for known_bad_ip specifically once known_bad_ip and netclass
// themselves port onto the engine. Flagged in this port's report as an
// unpreserved pin rather than silently narrowed.
func TestKnownBadIPReinforcesAnAlreadyRaisedFlag(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	bl := newFakeKnownBadIPs()
	bl.setMatch(ip, blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())
	d.WithKnownBadIPs(bl)
	d.Observe(evt(ip, 22, now))

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to still exist")
	}
	if asFlag.ReputationFloor == nil || *asFlag.ReputationFloor != knownBadIPConfidence {
		t.Errorf("expected TypeLowSlowScan's ReputationFloor to be reinforced to %d, got %v", knownBadIPConfidence, asFlag.ReputationFloor)
	}
	if asFlag.Confidence == nil || *asFlag.Confidence < knownBadIPConfidence {
		t.Errorf("expected TypeLowSlowScan's Confidence to be at least %d after reinforcement, got %v", knownBadIPConfidence, asFlag.Confidence)
	}
}

func TestKnownBadIPReinforcesPreviouslyRaisedFlagOnLaterEvent(t *testing.T) {
	cfg := DefaultConfig()
	ip := "198.51.100.4"

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Raised with no reinforcement in play yet, mirroring the original
	// test's "behaviorally first, matcher attached later" shape.
	fs.AddWithDetail(flags.TypeLowSlowScan, ip, "seeded", 10, flags.Evidence{}, "", now)

	d := NewWithSettings(cfg, fs, AllEnabledSettingsStore())

	asFlag := findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag == nil {
		t.Fatal("expected the seeded TypeLowSlowScan flag to exist")
	}
	if asFlag.ReputationFloor != nil {
		t.Fatalf("expected no floor yet, got %v", asFlag.ReputationFloor)
	}

	// Now attach a matcher and observe a later event from the same
	// source -- the pre-existing TypeLowSlowScan flag must be reinforced.
	bl := newFakeKnownBadIPs()
	bl.setMatch(ip, blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})
	d.WithKnownBadIPs(bl)
	d.Observe(evt(ip, 22, now.Add(time.Second)))

	asFlag = findFlag(fs, ip, flags.TypeLowSlowScan)
	if asFlag.ReputationFloor == nil || *asFlag.ReputationFloor != knownBadIPConfidence {
		t.Errorf("expected TypeLowSlowScan's ReputationFloor to be reinforced to %d, got %v", knownBadIPConfidence, asFlag.ReputationFloor)
	}
}

func TestKnownBadIPDoesNotReinforceUnrelatedTargetTypes(t *testing.T) {
	// distributed_brute_force's target is a port label ("port 22"), not
	// this source IP -- RaiseConfidenceFloor must never touch it just
	// because this source IP happens to be on the blocklist.
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
	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})
	d.WithKnownBadIPs(bl)

	d.Observe(evt("198.51.100.4", 22, time.Now()))

	f := findFlag(fs, "port 22", flags.TypeDistributedBruteForce)
	if f == nil {
		t.Fatal("expected the pre-existing flag to still exist")
	}
	if f.Confidence == nil || *f.Confidence != 20 {
		t.Errorf("expected TypeDistributedBruteForce's confidence to stay untouched at 20, got %v", f.Confidence)
	}
}
