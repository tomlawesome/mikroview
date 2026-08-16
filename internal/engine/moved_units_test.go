// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/store"
)

// This file is the unit-level coverage that moved here with the code it
// tests when internal/detect was deleted (issue #405): its
// confidence_test.go, ema_confidence_test.go and the pure-function
// halves of its vpn_test.go and settings_test.go. Every case is the one
// that package pinned, rewritten only where the function it calls
// changed shape (vpnBoostConfidence takes its patterns and multiplier as
// arguments here rather than reading them off a Detector).
//
// They are kept as direct unit tests rather than folded into the shipped
// definitions' own tests, for the same reason internal/detect kept them
// separate: a definition's test proves the definition's behaviour, and
// when one of these formulas is wrong every such test fails at once
// without any of them saying which formula it was.

// --- overshootConfidence (was internal/detect/confidence_test.go) -----

func TestOvershootConfidence(t *testing.T) {
	cases := []struct {
		name      string
		count     int
		threshold int
		want      int
	}{
		{"exactly at threshold", 5, 5, 0},
		{"at the ceiling (3x threshold)", 15, 5, 100},
		{"beyond the ceiling clamps to 100", 100, 5, 100},
		{"halfway to the ceiling", 10, 5, 50},
		{"threshold of zero treated as maximally confident", 3, 0, 100},
		{"negative threshold treated as maximally confident", 3, -1, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := overshootConfidence(tc.count, tc.threshold); got != tc.want {
				t.Errorf("overshootConfidence(%d, %d) = %d, want %d", tc.count, tc.threshold, got, tc.want)
			}
		})
	}
}

// --- the EMA formulas (was internal/detect/ema_confidence_test.go) ----

func TestEmaZScoreZeroVarianceAboveBaseline(t *testing.T) {
	if z := emaZScore(10, 5, 0); z != emaFullConfidenceZ {
		t.Errorf("emaZScore with zero variance and rate > baseline = %v, want emaFullConfidenceZ (%v)", z, emaFullConfidenceZ)
	}
}

func TestEmaZScoreZeroVarianceAtOrBelowBaseline(t *testing.T) {
	if z := emaZScore(5, 5, 0); z != 0 {
		t.Errorf("emaZScore at baseline with zero variance = %v, want 0", z)
	}
	if z := emaZScore(3, 5, 0); z != 0 {
		t.Errorf("emaZScore below baseline with zero variance = %v, want 0", z)
	}
}

func TestEmaZScoreWithVariance(t *testing.T) {
	// stddev = 2, rate is 3 stddev above baseline.
	if z := emaZScore(16, 10, 4); z != 3 {
		t.Errorf("emaZScore(16, 10, variance=4) = %v, want 3", z)
	}
}

func TestEmaConfidenceScalesWithSampleCountAndZ(t *testing.T) {
	if c := emaConfidence(emaFullConfidenceZ, 20, 20); c != 100 {
		t.Errorf("expected full confidence at full history + max z, got %d", c)
	}
	if c := emaConfidence(emaMinZ, 20, 20); c != 0 {
		t.Errorf("expected zero confidence at the z floor, got %d", c)
	}
	if c := emaConfidence(0, 20, 20); c != 0 {
		t.Errorf("expected confidence clamped to zero below the z floor, got %d", c)
	}
	if c := emaConfidence(emaFullConfidenceZ, 5, 20); c != 25 {
		t.Errorf("expected 25%% confidence at 5/20 warmup samples with max z, got %d", c)
	}
	if c := emaConfidence(emaFullConfidenceZ, 40, 20); c != 100 {
		t.Errorf("expected sampleCount beyond warmupSamples to still cap at 100, got %d", c)
	}
}

func TestEmaUpdateMovesTowardReading(t *testing.T) {
	baseline, variance := emaUpdate(20, 10, 0)
	if baseline <= 10 || baseline >= 20 {
		t.Errorf("expected the updated baseline to move toward the reading without jumping straight to it, got %v", baseline)
	}
	if variance <= 0 {
		t.Errorf("expected variance to become positive after a deviating reading, got %v", variance)
	}
}

// --- the VPN helpers (was internal/detect/vpn_test.go) ----------------

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

func TestVPNBoostConfidenceAppliesMultiplierAndClampsTo100(t *testing.T) {
	wg := []string{"wireguard*"}
	if got := vpnBoostConfidence(30, wg, 2, "wireguard1"); got != 60 {
		t.Errorf("vpnBoostConfidence(30, matching iface) = %d, want 60", got)
	}
	if got := vpnBoostConfidence(60, wg, 2, "wireguard1"); got != 100 {
		t.Errorf("vpnBoostConfidence(60, matching iface) = %d, want 100 (clamped)", got)
	}
}

func TestVPNBoostConfidenceLeavesNonMatchingInterfaceUnchanged(t *testing.T) {
	wg := []string{"wireguard*"}
	for _, iface := range []string{"ether1", "bridge1", ""} {
		if got := vpnBoostConfidence(42, wg, 2, iface); got != 42 {
			t.Errorf("vpnBoostConfidence(42, %q) = %d, want 42 unchanged (non-matching interface)", iface, got)
		}
	}
}

func TestVPNBoostConfidenceNonPositiveMultiplierTreatedAsNoBoost(t *testing.T) {
	if got := vpnBoostConfidence(42, []string{"wireguard*"}, 0, "wireguard1"); got != 42 {
		t.Errorf("vpnBoostConfidence with multiplier<=0 = %d, want 42 (treated as 1x)", got)
	}
}

// TestVPNBoostConfidenceNeverLowersConfidence: a multiplier in (0,1)
// would otherwise reduce confidence -- a misconfigured value must never
// make a VPN-sourced anomaly read as less alarming than an identical
// LAN-sourced one.
func TestVPNBoostConfidenceNeverLowersConfidence(t *testing.T) {
	if got := vpnBoostConfidence(42, []string{"wireguard*"}, 0.5, "wireguard1"); got != 42 {
		t.Errorf("vpnBoostConfidence with multiplier<1 = %d, want 42 (never lowered)", got)
	}
}

func TestVPNBoostConfidenceEmptyInterfacesLeavesEveryScoreUnchanged(t *testing.T) {
	for _, iface := range []string{"wireguard1", "wireguard*", "ether1", ""} {
		if got := vpnBoostConfidence(55, nil, 1.5, iface); got != 55 {
			t.Errorf("vpnBoostConfidence(55, %q) with no patterns = %d, want 55 unchanged", iface, got)
		}
	}
}

// --- the scope matchers (was internal/detect/settings_test.go) --------

func TestScopeMatchesSourceAllowDenyEmptyAndCIDR(t *testing.T) {
	cases := []struct {
		name string
		sc   Scope
		ip   string
		want bool
	}{
		{"empty list always matches", Scope{}, "203.0.113.9", true},
		{"allow list, hit", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeAllow}, "203.0.113.9", true},
		{"allow list, miss", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeAllow}, "203.0.113.10", false},
		{"deny list, hit excluded", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny}, "203.0.113.9", false},
		{"deny list, miss admitted", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny}, "203.0.113.10", true},
		{"CIDR allow, inside", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeAllow}, "203.0.113.200", true},
		{"CIDR allow, outside", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeAllow}, "198.51.100.1", false},
		{"CIDR deny, inside excluded", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeDeny}, "203.0.113.200", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopeMatchesSource(tc.sc, tc.ip); got != tc.want {
				t.Errorf("scopeMatchesSource(%+v, %q) = %v, want %v", tc.sc, tc.ip, got, tc.want)
			}
		})
	}
}

func TestScopeMatchesPortAndRuleAllowDeny(t *testing.T) {
	allow := Scope{Ports: []int{22, 3389}, PortsMode: ListModeAllow}
	if !matchesList(allow.Ports, allow.PortsMode, 22) {
		t.Error("expected 22 to match the allow list")
	}
	if matchesList(allow.Ports, allow.PortsMode, 80) {
		t.Error("expected 80 to not match the allow list")
	}

	deny := Scope{Ports: []int{22}, PortsMode: ListModeDeny}
	if matchesList(deny.Ports, deny.PortsMode, 22) {
		t.Error("expected 22 to be excluded by the deny list")
	}
	if !matchesList(deny.Ports, deny.PortsMode, 80) {
		t.Error("expected 80 to be admitted by the deny list")
	}

	rules := Scope{Rules: []string{"r13"}, RulesMode: ListModeDeny}
	if matchesList(rules.Rules, rules.RulesMode, "r13") {
		t.Error("expected r13 to be excluded")
	}
	if !matchesList(rules.Rules, rules.RulesMode, "r14") {
		t.Error("expected r14 to be admitted")
	}
}

func TestScopeMatchesClassificationAnyInternalExternal(t *testing.T) {
	cases := []struct {
		scope store.Scope
		ip    string
		want  bool
	}{
		{store.ScopeAny, "192.168.1.10", true},
		{store.ScopeAny, "203.0.113.9", true},
		{store.ScopeInternal, "192.168.1.10", true},
		{store.ScopeInternal, "203.0.113.9", false},
		{store.ScopeExternal, "203.0.113.9", true},
		{store.ScopeExternal, "192.168.1.10", false},
		{store.ScopeInternal, "not-an-ip", false},
	}
	for _, tc := range cases {
		sc := Scope{Classification: tc.scope}
		if got := scopeMatchesSource(sc, tc.ip); got != tc.want {
			t.Errorf("classification %q, ip %q: got %v, want %v", tc.scope, tc.ip, got, tc.want)
		}
	}
}

// TestIsLegacyDetectorID is internal/detect/settings_test.go's
// TestIsValidDetectorName -- the same question, asked of the list that
// replaced AllDetectorNames.
func TestIsLegacyDetectorID(t *testing.T) {
	if !IsLegacyDetectorID("port_scan") {
		t.Error("expected port_scan to be valid")
	}
	if IsLegacyDetectorID("not_a_real_detector") {
		t.Error("expected an unknown name to be invalid")
	}
	if !IsShippedDefinitionID("known_bad_ip") {
		t.Error("expected known_bad_ip to be in the shipped catalogue")
	}
	if IsLegacyDetectorID("known_bad_ip") {
		t.Error("expected known_bad_ip to be outside the legacy twelve -- see LegacyDetectorIDs")
	}
}
