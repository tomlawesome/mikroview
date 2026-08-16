// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
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

func TestOutboundAnomalyConfidenceHigherOverVPNInterfaceThanIdenticalLAN(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 5
	cfg.OutboundAnomalyWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 2
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	externals := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5", "203.0.113.6"}

	const lanIP, vpnIP = "192.168.1.60", "192.168.1.61"
	for i, dst := range externals {
		t := now.Add(time.Duration(i) * time.Second)
		d.Observe(vpnTaggedEvt(lanIP, dst, "ether1", t))
		d.Observe(vpnTaggedEvt(vpnIP, dst, "wireguard1", t))
	}

	var lanConf, vpnConf *int
	for _, f := range fs.List() {
		if f.Type != flags.TypeOutboundAnomaly {
			continue
		}
		switch f.Target {
		case lanIP:
			lanConf = f.Confidence
		case vpnIP:
			vpnConf = f.Confidence
		}
	}
	if lanConf == nil || vpnConf == nil {
		t.Fatalf("expected both sources to raise outbound_anomaly, got %+v", fs.List())
	}

	wantLAN := overshootConfidence(len(externals), cfg.OutboundAnomalyThreshold)
	wantVPN := d.vpnBoostConfidence(wantLAN, "wireguard1")
	if *lanConf != wantLAN {
		t.Errorf("LAN confidence = %d, want %d", *lanConf, wantLAN)
	}
	if *vpnConf != wantVPN {
		t.Errorf("VPN confidence = %d, want %d", *vpnConf, wantVPN)
	}
	if *vpnConf <= *lanConf {
		t.Fatalf("expected VPN-interface confidence (%d) to exceed identical LAN-interface confidence (%d)", *vpnConf, *lanConf)
	}
}

func TestInternalReconConfidenceHigherOverVPNInterfaceThanIdenticalLAN(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 5
	cfg.InternalReconWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	cfg.VPNInterfaces = []string{"wireguard*"}
	cfg.VPNConfidenceMultiplier = 2
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	internals := []string{"192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5", "192.168.1.6", "192.168.1.7"}

	const lanIP, vpnIP = "192.168.1.70", "192.168.1.71"
	for i, dst := range internals {
		t := now.Add(time.Duration(i) * time.Second)
		d.Observe(vpnTaggedEvt(lanIP, dst, "ether1", t))
		d.Observe(vpnTaggedEvt(vpnIP, dst, "wireguard1", t))
	}

	var lanConf, vpnConf *int
	for _, f := range fs.List() {
		if f.Type != flags.TypeInternalRecon {
			continue
		}
		switch f.Target {
		case lanIP:
			lanConf = f.Confidence
		case vpnIP:
			vpnConf = f.Confidence
		}
	}
	if lanConf == nil || vpnConf == nil {
		t.Fatalf("expected both sources to raise internal_recon, got %+v", fs.List())
	}

	wantLAN := overshootConfidence(len(internals), cfg.InternalReconThreshold)
	wantVPN := d.vpnBoostConfidence(wantLAN, "wireguard1")
	if *lanConf != wantLAN {
		t.Errorf("LAN confidence = %d, want %d", *lanConf, wantLAN)
	}
	if *vpnConf != wantVPN {
		t.Errorf("VPN confidence = %d, want %d", *vpnConf, wantVPN)
	}
	if *vpnConf <= *lanConf {
		t.Fatalf("expected VPN-interface confidence (%d) to exceed identical LAN-interface confidence (%d)", *vpnConf, *lanConf)
	}
}

func TestDestSpreadConfidenceIdenticalRegardlessOfInterfaceWhenVPNInterfacesUnset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 5
	cfg.OutboundAnomalyWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	// VPNInterfaces deliberately left unset (the default).
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	externals := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5", "203.0.113.6"}

	const plainIP, wgLookalikeIP = "192.168.1.80", "192.168.1.81"
	for i, dst := range externals {
		t := now.Add(time.Duration(i) * time.Second)
		d.Observe(vpnTaggedEvt(plainIP, dst, "ether1", t))
		d.Observe(vpnTaggedEvt(wgLookalikeIP, dst, "wireguard1", t))
	}

	var plainConf, wgConf *int
	for _, f := range fs.List() {
		if f.Type != flags.TypeOutboundAnomaly {
			continue
		}
		switch f.Target {
		case plainIP:
			plainConf = f.Confidence
		case wgLookalikeIP:
			wgConf = f.Confidence
		}
	}
	if plainConf == nil || wgConf == nil {
		t.Fatalf("expected both sources to raise outbound_anomaly, got %+v", fs.List())
	}
	if *plainConf != *wgConf {
		t.Errorf("expected identical confidence with VPNInterfaces unset regardless of InInterface, got LAN-iface=%d wireguard-iface=%d", *plainConf, *wgConf)
	}
}
