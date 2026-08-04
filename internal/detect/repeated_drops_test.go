package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func dropEvt(srcIP, dstIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, Action: store.ActionDrop, ReceivedAt: at}
}

func TestRepeatedDropsFlagsAtThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 5
	cfg.RepeatedDropsWindow = time.Minute
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 4; i++ {
		d.Observe(dropEvt("203.0.113.9", "192.168.1.50", 25565, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below threshold, got %+v", fs.List())
	}

	d.Observe(dropEvt("203.0.113.9", "192.168.1.50", 25565, now.Add(5*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeRepeatedDrops || list[0].Target != "203.0.113.9 -> port 25565" {
		t.Fatalf("expected a repeated_drops flag keyed by source+port, got %+v", list)
	}
}

func TestRepeatedDropsIgnoresAcceptedTraffic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 5; i++ {
		e := dropEvt("203.0.113.9", "192.168.1.50", 25565, now.Add(time.Duration(i)*time.Second))
		e.Action = store.ActionAccept
		d.Observe(e)
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected accepted traffic to never trigger repeated_drops, got %+v", fs.List())
	}
}

func TestRepeatedDropsIgnoresExternalDestinations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 5; i++ {
		// dst is public -- not a locally-hosted service
		d.Observe(dropEvt("192.168.1.50", "203.0.113.9", 443, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected drops against an external destination to be ignored, got %+v", fs.List())
	}
}

func TestRepeatedDropsTracksEachPortIndependently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 2; i++ {
		d.Observe(dropEvt("203.0.113.9", "192.168.1.50", 25565, now.Add(time.Duration(i)*time.Second)))
		d.Observe(dropEvt("203.0.113.9", "192.168.1.50", 8080, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected 2+2 split across two ports to stay below the threshold=3 for either, got %+v", fs.List())
	}
}
