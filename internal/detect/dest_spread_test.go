// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func lanEvt(srcIP, dstIP string, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: 443, ReceivedAt: at}
}

func lanEvtState(srcIP, dstIP, connState string, at time.Time) store.Event {
	e := lanEvt(srcIP, dstIP, at)
	e.ConnState = connState
	return e
}

func TestInternalReconIgnoresEstablishedTraffic(t *testing.T) {
	// The database-server false positive reported in mikroview#35/#36:
	// a busy server's established-connection return traffic to many
	// distinct clients must not look like network recon.
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 3
	cfg.OutboundAnomalyThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		d.Observe(lanEvtState("192.168.1.20", "192.168.1."+string(rune('0'+i)), "established", now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected established-state traffic to never trip internal-recon, got %+v", fs.List())
	}
}

func TestOutboundAnomalyFlagsManyDistinctExternalDestinations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 5
	cfg.OutboundAnomalyWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	externals := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4"}
	for i, dst := range externals {
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below the distinct-destination threshold, got %+v", fs.List())
	}

	d.Observe(lanEvt("192.168.1.50", "203.0.113.5", now.Add(5*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeOutboundAnomaly || list[0].Target != "192.168.1.50" {
		t.Fatalf("expected an outbound_anomaly flag for the LAN source, got %+v", list)
	}
}

func TestInternalReconFlagsManyDistinctInternalDestinations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 5
	cfg.InternalReconWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 1; i <= 4; i++ {
		d.Observe(lanEvt("192.168.1.50", "192.168.1."+string(rune('0'+i)), now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below the distinct-destination threshold, got %+v", fs.List())
	}

	d.Observe(lanEvt("192.168.1.50", "192.168.1.99", now.Add(10*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeInternalRecon || list[0].Target != "192.168.1.50" {
		t.Fatalf("expected an internal_recon flag for the LAN source, got %+v", list)
	}
}

func TestDestSpreadIgnoresExternalSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 2
	cfg.InternalReconThreshold = 2
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(lanEvt("203.0.113.9", "192.168.1.1", now))
	d.Observe(lanEvt("203.0.113.9", "192.168.1.2", now))
	if len(fs.List()) != 0 {
		t.Fatalf("expected an external source's destination spread to never be tracked, got %+v", fs.List())
	}
}

func TestDestSpreadClassifiesEachDestinationIndependently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 3
	cfg.InternalReconThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	// mix of internal and external destinations from the same LAN source --
	// neither should cross its own threshold on its own
	d.Observe(lanEvt("192.168.1.50", "192.168.1.5", now))
	d.Observe(lanEvt("192.168.1.50", "192.168.1.6", now))
	d.Observe(lanEvt("192.168.1.50", "203.0.113.1", now))
	d.Observe(lanEvt("192.168.1.50", "203.0.113.2", now))

	if len(fs.List()) != 0 {
		t.Fatalf("expected 2 internal + 2 external destinations to stay below both (threshold=3) thresholds, got %+v", fs.List())
	}
}

func TestOutboundAnomalyRespectsHostsScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorOutboundAnomaly] = Settings{
		Enabled: true,
		Scope:   Scope{Hosts: []string{"192.168.1.50"}, HostsMode: ListModeAllow},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	for i, dst := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		d.Observe(lanEvt("192.168.1.99", dst, now.Add(time.Duration(i)*time.Second))) // not in the allowlist
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected a source outside the allowlist to never flag, got %+v", fs.List())
	}

	for i, dst := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 1 {
		t.Fatalf("expected the allowlisted source to still flag, got %+v", fs.List())
	}
}

func TestOutboundAnomalyAndInternalReconToggleIndependently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 2
	cfg.InternalReconThreshold = 2
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorInternalRecon] = Settings{Enabled: false}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	d.Observe(lanEvt("192.168.1.50", "203.0.113.1", now))
	d.Observe(lanEvt("192.168.1.50", "203.0.113.2", now))
	d.Observe(lanEvt("192.168.1.50", "192.168.1.5", now))
	d.Observe(lanEvt("192.168.1.50", "192.168.1.6", now))

	sawOutbound, sawRecon := false, false
	for _, f := range fs.List() {
		switch f.Type {
		case flags.TypeOutboundAnomaly:
			sawOutbound = true
		case flags.TypeInternalRecon:
			sawRecon = true
		}
	}
	if !sawOutbound {
		t.Error("expected outbound_anomaly to still fire while enabled")
	}
	if sawRecon {
		t.Error("expected internal_recon to never fire while disabled, even on the same shared window")
	}
}

func TestOutboundAnomalyConfidenceScalesWithOvershoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 5
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	now := time.Now()

	justOver, fs := newTestDetector(t, cfg)
	for i := 1; i <= 5; i++ {
		justOver.Observe(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	wellOver, fs2 := newTestDetector(t, cfg)
	for i := 1; i <= 15; i++ {
		wellOver.Observe(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

func TestInternalReconConfidenceScalesWithOvershoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 5
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	now := time.Now()

	justOver, fs := newTestDetector(t, cfg)
	for i := 1; i <= 5; i++ {
		justOver.Observe(lanEvt("192.168.1.50", fmt.Sprintf("192.168.1.%d", i+10), now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	wellOver, fs2 := newTestDetector(t, cfg)
	for i := 1; i <= 15; i++ {
		wellOver.Observe(lanEvt("192.168.1.50", fmt.Sprintf("192.168.1.%d", i+10), now.Add(time.Duration(i)*time.Second)))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

func TestOutboundAnomalyAndInternalReconEvidenceCapturesDestinations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 3
	cfg.InternalReconThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	externals := []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"}
	internals := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for i, dst := range externals {
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}
	for i, dst := range internals {
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}

	var outbound, recon *flags.Flag
	list := fs.List()
	for i := range list {
		switch list[i].Type {
		case flags.TypeOutboundAnomaly:
			outbound = &list[i]
		case flags.TypeInternalRecon:
			recon = &list[i]
		}
	}
	if outbound == nil || len(outbound.Evidence.Hosts) != 3 {
		t.Errorf("expected outbound_anomaly evidence to list the 3 external destinations, got %+v", outbound)
	}
	if recon == nil || len(recon.Evidence.Hosts) != 3 {
		t.Errorf("expected internal_recon evidence to list the 3 internal destinations, got %+v", recon)
	}
}
