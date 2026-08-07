// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// lowSlowCfg returns a Config tuned for fast, deterministic tests --
// small thresholds and short durations instead of DefaultConfig's
// hours-scale defaults -- with every other detector's threshold pushed
// out of reach so only low_slow_scan can fire.
func lowSlowCfg() Config {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	cfg.RepeatedDropsThreshold = 1000
	cfg.LowSlowScanWindow = 2 * time.Hour
	cfg.LowSlowScanPortThreshold = 5
	cfg.LowSlowScanHostThreshold = 5
	cfg.LowSlowScanMinObservation = 10 * time.Minute
	cfg.LowSlowScanDropRatio = 0.8
	cfg.LowSlowScanBaselineMultiplier = 2
	return cfg
}

func lowSlowEvt(srcIP, dstIP string, dstPort int, action store.Action, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, Action: action, ReceivedAt: at, ConnState: "new"}
}

// feedPacedScan feeds n events, one every step, each touching a distinct
// (port, host) pair -- the shape a real low-and-slow scanner produces --
// starting at t0. Returns the time of the last event.
func feedPacedScan(d *Detector, srcIP string, n int, step time.Duration, action store.Action, t0 time.Time) time.Time {
	last := t0
	for i := 0; i < n; i++ {
		last = t0.Add(time.Duration(i) * step)
		d.Observe(lowSlowEvt(srcIP, fmt.Sprintf("192.168.50.%d", i+1), 10000+i, action, last))
	}
	return last
}

func findLowSlowFlag(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		if f.Type == flags.TypeLowSlowScan {
			return &f
		}
	}
	return nil
}

func TestLowSlowScanFiresWhenAllAxesClear(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	f := findLowSlowFlag(fs)
	if f == nil {
		t.Fatalf("expected a low_slow_scan flag, got %+v", fs.List())
	}
	if f.Target != "203.0.113.9" {
		t.Errorf("expected target to be the source IP, got %q", f.Target)
	}
	if f.Confidence == nil {
		t.Errorf("expected a confidence score, got nil")
	}
	if len(f.Evidence.Ports) < cfg.LowSlowScanPortThreshold {
		t.Errorf("expected at least %d ports in evidence, got %v", cfg.LowSlowScanPortThreshold, f.Evidence.Ports)
	}
	if len(f.Evidence.Hosts) < cfg.LowSlowScanHostThreshold {
		t.Errorf("expected at least %d hosts in evidence, got %v", cfg.LowSlowScanHostThreshold, f.Evidence.Hosts)
	}
}

func TestLowSlowScanRequiresPortBreadth(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	// Many distinct hosts, but always the same port -- a horizontal
	// probe on one known service, not a scan.
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		d.Observe(lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 22, store.ActionDrop, at))
	}
	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected no flag with port breadth stuck at 1, got %+v", f)
	}
}

func TestLowSlowScanRequiresHostBreadth(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	// Many distinct ports, but always the same host -- a vertical scan
	// on one already-known host, deliberately out of scope for this
	// detector (the existing fast port_scan detector's territory).
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		d.Observe(lowSlowEvt("203.0.113.9", "192.168.50.1", 10000+i, store.ActionDrop, at))
	}
	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected no flag with host breadth stuck at 1, got %+v", f)
	}
}

func TestLowSlowScanRequiresDropRatio(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionAccept, t0)

	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected no flag when traffic is mostly accepted, got %+v", f)
	}
}

func TestLowSlowScanRequiresMinimumObservation(t *testing.T) {
	cfg := lowSlowCfg()
	cfg.LowSlowScanMinObservation = time.Hour
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	// Same breadth/drop-ratio pattern as the firing test, but compressed
	// into a few seconds -- well short of the one-hour observation floor.
	feedPacedScan(d, "203.0.113.9", 8, time.Second, store.ActionDrop, t0)

	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected no flag before the minimum observation floor is met, got %+v", f)
	}
}

func TestLowSlowScanIgnoresUntrackableConnState(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		e := lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, at)
		e.ConnState = "established"
		d.Observe(e)
	}
	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected established/return traffic to be ignored, got %+v", f)
	}
}

func TestLowSlowScanRespectsHostScope(t *testing.T) {
	cfg := lowSlowCfg()
	seed := DefaultSettingsMap()
	seed[DetectorLowSlowScan] = Settings{
		Enabled: true,
		Scope:   Scope{Hosts: []string{"198.51.100.1"}, HostsMode: ListModeAllow},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected a source outside the allowed hosts list to never flag, got %+v", f)
	}
}

func TestLowSlowScanDisabledByDefaultToggle(t *testing.T) {
	cfg := lowSlowCfg()
	seed := DefaultSettingsMap()
	seed[DetectorLowSlowScan] = Settings{Enabled: false}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if f := findLowSlowFlag(fs); f != nil {
		t.Fatalf("expected a disabled detector to never flag, got %+v", f)
	}
}

func TestLowSlowScanCarriesCountry(t *testing.T) {
	cfg := lowSlowCfg()
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	for i := 0; i < 8; i++ {
		at := t0.Add(time.Duration(i) * 5 * time.Minute)
		e := lowSlowEvt("203.0.113.9", fmt.Sprintf("192.168.50.%d", i+1), 10000+i, store.ActionDrop, at)
		e.SrcCountry = "NL"
		d.Observe(e)
	}

	f := findLowSlowFlag(fs)
	if f == nil {
		t.Fatalf("expected a flag to fire, got %+v", fs.List())
	}
	if f.Country != "NL" {
		t.Errorf("expected Country to be threaded through, got %q", f.Country)
	}
}

func TestLowSlowScanTriggersReputationLookup(t *testing.T) {
	cfg := lowSlowCfg()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	d := New(cfg, fs)
	fake := newFakeReputation()
	d.WithReputation(fake)

	t0 := time.Now()
	feedPacedScan(d, "203.0.113.9", 8, 5*time.Minute, store.ActionDrop, t0)

	if findLowSlowFlag(fs) == nil {
		t.Fatalf("expected a flag to fire before reputation is even relevant, got %+v", fs.List())
	}
	ip := expectStarted(t, fake.started)
	if ip != "203.0.113.9" {
		t.Errorf("expected the reputation lookup to target the source IP, got %q", ip)
	}
}
