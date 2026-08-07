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

func TestKnownBadIPReinforcesSameEventPortScanFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 1000

	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})

	d, fs := newTestDetector(t, cfg)
	d.WithKnownBadIPs(bl)

	now := time.Now()
	// The 3rd event in this same call both crosses PortScanThreshold
	// (raising TypePortScan) and matches the blocklist (raising
	// TypeKnownBadIP) -- observeKnownBadIP runs last within that same
	// Observe call, so it must still see and reinforce the
	// just-raised TypePortScan flag, not only ones raised on an
	// earlier event.
	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 100+i, now.Add(time.Duration(i)*time.Second)))
	}

	psFlag := findFlag(fs, "198.51.100.4", flags.TypePortScan)
	if psFlag == nil {
		t.Fatal("expected a TypePortScan flag to have been raised")
	}
	if psFlag.ReputationFloor == nil || *psFlag.ReputationFloor != knownBadIPConfidence {
		t.Errorf("expected TypePortScan's ReputationFloor to be reinforced to %d by the same-event blocklist match, got %v", knownBadIPConfidence, psFlag.ReputationFloor)
	}
	if psFlag.Confidence == nil || *psFlag.Confidence < knownBadIPConfidence {
		t.Errorf("expected TypePortScan's Confidence to be at least %d after reinforcement, got %v", knownBadIPConfidence, psFlag.Confidence)
	}
}

func TestKnownBadIPReinforcesPreviouslyRaisedFlagOnLaterEvent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	d, fs := newTestDetector(t, cfg)
	// No knownBad matcher attached yet -- raise TypeCriticalPort purely
	// behaviorally first, with no reinforcement in play.
	now := time.Now()
	d.Observe(evt("198.51.100.4", 22, now))

	cpFlag := findFlag(fs, "198.51.100.4", flags.TypeCriticalPort)
	if cpFlag == nil {
		t.Fatal("expected a TypeCriticalPort flag from the first event")
	}
	if cpFlag.ReputationFloor != nil {
		t.Fatalf("expected no floor yet, got %v", cpFlag.ReputationFloor)
	}

	// Now attach a matcher and observe a second event from the same
	// source -- the pre-existing TypeCriticalPort flag must be
	// reinforced.
	bl := newFakeKnownBadIPs()
	bl.setMatch("198.51.100.4", blocklist.MatchResult{Source: blocklist.SourceSpamhausDROP, Label: "Spamhaus DROP", Range: "198.51.100.0/24"})
	d.WithKnownBadIPs(bl)
	d.Observe(evt("198.51.100.4", 22, now.Add(time.Second)))

	cpFlag = findFlag(fs, "198.51.100.4", flags.TypeCriticalPort)
	if cpFlag.ReputationFloor == nil || *cpFlag.ReputationFloor != knownBadIPConfidence {
		t.Errorf("expected TypeCriticalPort's ReputationFloor to be reinforced to %d, got %v", knownBadIPConfidence, cpFlag.ReputationFloor)
	}
}

func TestKnownBadIPDoesNotReinforceUnrelatedTargetTypes(t *testing.T) {
	// distributed_brute_force's target is a port label ("port 22"), not
	// this source IP -- RaiseConfidenceFloor must never touch it just
	// because this source IP happens to be on the blocklist.
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
