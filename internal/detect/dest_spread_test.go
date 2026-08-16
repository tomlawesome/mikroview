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

// TestOutboundAnomalyFlagsManyDistinctExternalDestinations moved to
// internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyFlagsManyDistinctExternalDestinations (issue
// #405: outbound_anomaly is now a shipped programmatic definition
// evaluated by internal/engine -- see shipped_dest_spread.go).

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

// TestOutboundAnomalyRespectsHostsScope moved to
// internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyRespectsHostsScope (issue #405).

// TestOutboundAnomalyAndInternalReconToggleIndependently used to prove
// the two detectors were independently toggleable while sharing one
// destWindow. The sharing is gone: outbound_anomaly moved to
// internal/engine (issue #405, see shipped_dest_spread.go), and each
// direction is now its own definition with its own enablement -- so
// independence is structural rather than something a test has to
// establish. What survives here is the half internal/detect still
// evaluates: a disabled internal_recon fires nothing.
// internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyDisabledIsInert is its counterpart.
func TestInternalReconDisabledFiresNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 2
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorInternalRecon] = Settings{Enabled: false}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	d.Observe(lanEvt("192.168.1.50", "192.168.1.5", now))
	d.Observe(lanEvt("192.168.1.50", "192.168.1.6", now))

	for _, f := range fs.List() {
		if f.Type == flags.TypeInternalRecon {
			t.Error("expected internal_recon to never fire while disabled")
		}
	}
}

// TestOutboundAnomalyConfidenceScalesWithOvershoot moved to
// internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomalyConfidenceScalesWithOvershoot (issue #405),
// pinned value for value.

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

// TestOutboundAnomalyAndInternalReconEvidenceCapturesDestinations kept
// only its internal_recon half here; outbound_anomaly's is covered by
// internal/engine/shipped_dest_spread_test.go's
// TestShippedOutboundAnomaly_FieldsRefireClearRevive, which pins the
// evidence set exactly (sorted, capped at maxEvidenceHosts) rather than
// only its length.
func TestInternalReconEvidenceCapturesDestinations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalReconThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	internals := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for i, dst := range internals {
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}

	var recon *flags.Flag
	list := fs.List()
	for i := range list {
		if list[i].Type == flags.TypeInternalRecon {
			recon = &list[i]
		}
	}
	if recon == nil || len(recon.Evidence.Hosts) != 3 {
		t.Errorf("expected internal_recon evidence to list the 3 internal destinations, got %+v", recon)
	}
}
