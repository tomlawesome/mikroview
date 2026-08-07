// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
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

func TestDistributedBruteForceConfidenceScalesWithOvershoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 5
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	now := time.Now()

	justOver, fs := newTestDetector(t, cfg)
	for i := 0; i < 5; i++ {
		justOver.Observe(evt(fmt.Sprintf("198.51.100.%d", i+1), 22, now))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	wellOver, fs2 := newTestDetector(t, cfg)
	for i := 0; i < 15; i++ {
		wellOver.Observe(evt(fmt.Sprintf("198.51.100.%d", i+1), 22, now))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

func TestDistributedBruteForceEvidenceCapturesSourceHosts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 3
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	ips := []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"}
	for i, ip := range ips {
		d.Observe(evt(ip, 22, now.Add(time.Duration(i)*time.Second)))
	}

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected one flag, got %+v", list)
	}
	if got := list[0].Evidence.Hosts; len(got) != 3 {
		t.Errorf("expected the evidence to list all 3 distinct source hosts, got %v", got)
	}
}
