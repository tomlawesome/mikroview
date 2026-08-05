package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestDistributedBruteForceFlagsManyDistinctSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 5
	cfg.DistributedBruteForceWindow = time.Minute
	cfg.CriticalPorts = []int{22}
	// keep unrelated detectors quiet so they don't muddy this assertion
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 4; i++ {
		src := []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4"}[i]
		d.Observe(evt(src, 22, now))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below the distinct-source threshold, got %+v", fs.List())
	}

	d.Observe(evt("198.51.100.5", 22, now))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeDistributedBruteForce || list[0].Target != "port 22" {
		t.Fatalf("expected a distributed_brute_force flag targeting the port, got %+v", list)
	}
}

func TestDistributedBruteForceIgnoresRepeatsFromSameSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 3
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 10; i++ {
		d.Observe(evt("198.51.100.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected repeats from one source to never cross a *distinct*-source threshold, got %+v", fs.List())
	}
}

func TestDistributedBruteForceIgnoresPrivateSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 2
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(evt("192.168.1.10", 22, now))
	d.Observe(evt("192.168.1.11", 22, now))
	if len(fs.List()) != 0 {
		t.Fatalf("expected private sources to be excluded (matches critical-port's own external-only scope), got %+v", fs.List())
	}
}

func TestDistributedBruteForceRespectsPortsScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 3
	cfg.CriticalPorts = []int{22, 23}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorDistributedBruteForce] = Settings{
		Enabled: true,
		Scope:   Scope{Ports: []int{22}, PortsMode: ListModeDeny},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	for i := 0; i < 3; i++ {
		src := []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"}[i]
		d.Observe(evt(src, 22, now)) // denylisted port
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected the denylisted port to never flag, got %+v", fs.List())
	}

	for i := 0; i < 3; i++ {
		src := []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"}[i]
		d.Observe(evt(src, 23, now))
	}
	if len(fs.List()) != 1 {
		t.Fatalf("expected the non-denylisted port to still flag, got %+v", fs.List())
	}
}
