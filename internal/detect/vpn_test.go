// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// --- isVPNInterface: exact match, prefix/glob match, no match ---

func TestIsVPNInterfaceMatching(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		iface    string
		want     bool
	}{
		{"exact match", []string{"wireguard1"}, "wireguard1", true},
		{"exact no match, different suffix", []string{"wireguard1"}, "wireguard2", false},
		{"prefix/glob match", []string{"wireguard*"}, "wireguard5", true},
		{"prefix/glob match against the base name itself", []string{"wireguard*"}, "wireguard", true},
		{"glob doesn't match an unrelated interface", []string{"wireguard*"}, "ether1", false},
		{"no match, unrelated pattern list", []string{"ether1", "bridge1"}, "wireguard1", false},
		{"multiple patterns, second one matches", []string{"ovpn*", "wireguard1"}, "wireguard1", true},
		{"empty pattern list never matches", nil, "wireguard1", false},
		{"empty interface never matches, even a wildcard pattern", []string{"*"}, "", false},
		{"malformed glob pattern degrades to no-match, not a panic", []string{"[wireguard"}, "wireguard1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVPNInterface(tc.patterns, tc.iface); got != tc.want {
				t.Errorf("isVPNInterface(%v, %q) = %v, want %v", tc.patterns, tc.iface, got, tc.want)
			}
		})
	}
}

// --- vpnBoostConfidence: the multiplier mechanism itself ---

func TestVPNBoostConfidenceAppliesMultiplierAndClampsTo100(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 2
	d, _ := newTestDetector(t, cfg)

	if got := d.vpnBoostConfidence(30, "wireguard1"); got != 60 {
		t.Errorf("vpnBoostConfidence(30, matching iface) = %d, want 60", got)
	}
	if got := d.vpnBoostConfidence(60, "wireguard1"); got != 100 {
		t.Errorf("vpnBoostConfidence(60, matching iface) = %d, want 100 (clamped)", got)
	}
}

func TestVPNBoostConfidenceLeavesNonMatchingInterfaceUnchanged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 2
	d, _ := newTestDetector(t, cfg)

	for _, iface := range []string{"ether1", "bridge1", ""} {
		if got := d.vpnBoostConfidence(42, iface); got != 42 {
			t.Errorf("vpnBoostConfidence(42, %q) = %d, want 42 unchanged (non-matching interface)", iface, got)
		}
	}
}

func TestVPNBoostConfidenceNonPositiveMultiplierTreatedAsNoBoost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 0
	d, _ := newTestDetector(t, cfg)
	if got := d.vpnBoostConfidence(42, "wireguard1"); got != 42 {
		t.Errorf("vpnBoostConfidence with multiplier<=0 = %d, want 42 (treated as 1x)", got)
	}
}

func TestVPNBoostConfidenceNeverLowersConfidence(t *testing.T) {
	// A multiplier in (0,1) would otherwise reduce confidence -- see
	// vpnBoostConfidence's doc comment: a misconfigured value must never
	// make a VPN-sourced anomaly read as less alarming than an identical
	// LAN-sourced one.
	cfg := DefaultConfig()
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 0.5
	d, _ := newTestDetector(t, cfg)
	if got := d.vpnBoostConfidence(42, "wireguard1"); got != 42 {
		t.Errorf("vpnBoostConfidence with multiplier<1 = %d, want 42 (never lowered)", got)
	}
}

// --- Backward compatibility: empty VPNInterfaces (the default) is a no-op ---

func TestVPNBoostConfidenceEmptyInterfacesLeavesEveryScoreUnchanged(t *testing.T) {
	d, _ := newTestDetector(t, DefaultConfig()) // VPNInterfaces unset
	for _, iface := range []string{"wireguard1", "wireguard*", "ether1", ""} {
		if got := d.vpnBoostConfidence(55, iface); got != 55 {
			t.Errorf("vpnBoostConfidence(55, %q) with VPNInterfaces unset = %d, want 55 unchanged", iface, got)
		}
	}
}

// --- checkHostActivityBaseline (TypeActivitySpike): direct VPN-vs-LAN comparison ---
//
// TestActivitySpikeConfidenceHigherOverVPNInterfaceThanIdenticalLAN and
// TestActivitySpikeConfidenceIdenticalRegardlessOfInterfaceWhenVPNInterfacesUnset
// moved to internal/engine (issue #405: activity_spike is now a shipped
// programmatic definition evaluated by internal/engine, not
// internal/detect -- see shipped_activity_spike.go), since both drove
// checkHostActivityBaseline directly against sourceWindow fields
// (baseline/variance/primed/sampleCount) that moved with it.
//
// The first is now
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpikeVPNBoostsConfidence, though not pinned quite as
// exhaustively: it asserts the VPN-tagged interface scores strictly
// higher than an identical LAN one and pins the Detail suffix, but
// (unlike this test) does not separately recompute the expected LAN
// confidence from emaZScore/emaConfidence by hand and assert exact
// equality.
//
// The second has no engine-side counterpart at all -- no
// activity_spike test on the engine side pins that, with VPNInterfaces
// left unset on one definition instance, two different interface names
// (including one that superficially looks like a VPN interface) produce
// byte-identical confidence. The underlying mechanism (vpnBoostConfidence
// treating an empty pattern list as a no-op for any interface) is still
// covered generically by this file's own
// TestVPNBoostConfidenceEmptyInterfacesLeavesEveryScoreUnchanged, which
// exercises internal/detect's still-live copy of that function directly
// (used by dest_spread's two halves) -- not activity_spike's wiring to
// it. Flagged in this port's report rather than silently dropped.

// --- observeDestSpread (TypeOutboundAnomaly/TypeInternalRecon): direct VPN-vs-LAN comparison ---

func vpnTaggedEvt(srcIP, dstIP, iface string, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, InInterface: iface, ReceivedAt: at}
}

// TestOutboundAnomalyConfidenceHigherOverVPNInterfaceThanIdenticalLAN
// moved to internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyVPNBoostsConfidenceAndNamesTheInterface
// (issue #405).

// TestInternalReconConfidenceHigherOverVPNInterfaceThanIdenticalLAN
// moved to internal/engine/shipped_dest_spread_test.go's
// TestShippedInternalReconVPNBoostsConfidence (issue #405).

// TestDestSpreadConfidenceIdenticalRegardlessOfInterfaceWhenVPNInterfacesUnset
// moved to internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyConfidenceIdenticalWhenVPNInterfacesUnset
// (issue #405).
