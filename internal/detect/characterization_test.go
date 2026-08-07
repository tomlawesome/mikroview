// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file holds a small number of characterization tests for the
// countRing/distinctRing migration (issue #76): not exhaustive re-tests
// of every detector (the ~30 existing tests in this package already
// cover firing conditions in detail and are unchanged by this
// migration), just enough to confirm a few detectors still fire in the
// same general shape at realistic, non-test-shrunk config scale, and to
// call out the one deliberate, documented divergence from a literal
// reading of the migration plan.

// TestCharacterizationPortScanFiresAtDefaultConfigScale exercises
// observeScanAndSpike's port-scan half with DefaultConfig's real
// PortScanWindow/PortScanThreshold (60s/15) rather than a test-shrunk
// config, spreading events across the full window so the migration
// touches most of the ring's 60 one-second buckets (bucketSpanFor(60s)
// == minBucketSpan == 1s) -- the shape a real slow-burst scan produces.
func TestCharacterizationPortScanFiresAtDefaultConfigScale(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 1000 // isolate port_scan
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for port := 1; port < cfg.PortScanThreshold; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*3*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below threshold, got %+v", fs.List())
	}

	d.Observe(evt("203.0.113.9", cfg.PortScanThreshold, now.Add(time.Duration(cfg.PortScanThreshold)*3*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypePortScan || list[0].Target != "203.0.113.9" {
		t.Fatalf("expected a port_scan flag at threshold, got %+v", list)
	}
}

// TestCharacterizationLowSlowScanFiresAtDefaultWindowScale runs
// observeLowSlowScan's three rings (ports/hosts/drops) at
// DefaultConfig's real 3-hour LowSlowScanWindow, where
// bucketSpanFor(3h) == 3m -- a much coarser bucket than the port-scan
// test above, exercising the case this migration cares most about
// (hours-scale windows that would be expensive to linear-rescan on
// every event).
func TestCharacterizationLowSlowScanFiresAtDefaultWindowScale(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	cfg.RepeatedDropsThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	t0 := time.Now()
	// One paced, mostly-refused attempt every ~13 minutes -- comfortably
	// spaced past LowSlowScanMinObservation (45m) and past a 3-minute
	// bucket span by the time enough samples accumulate to clear the
	// port/host breadth thresholds (8/5).
	feedPacedScan(d, "203.0.113.9", cfg.LowSlowScanPortThreshold+2, 13*time.Minute, store.ActionDrop, t0)

	if findLowSlowFlag(fs) == nil {
		t.Fatalf("expected a low_slow_scan flag at default-config scale, got %+v", fs.List())
	}
}

// TestCharacterizationDistributedBruteForceRequiresDistinctSources
// documents a deliberate divergence from the migration plan's summary
// table, which listed "countRing per key" for
// observeDistributedBruteForce. The detector's entire point is
// *distinct* source IPs hammering one port (as opposed to
// critical-port's "one source hitting it repeatedly") -- a countRing
// only tracks a raw event count, so swapping in one there would make a
// single source's retries alone cross the threshold, collapsing the
// distinction the detector exists to draw. portSources therefore uses
// distinctRing[string] instead (see distributed_brute_force.go), which
// is what TestDistributedBruteForceIgnoresRepeatsFromSameSource in
// distributed_brute_force_test.go already pins down; this test re-states
// the same guarantee here, next to the rest of this migration's
// characterization coverage, for anyone comparing this change against
// the plan's table.
func TestCharacterizationDistributedBruteForceRequiresDistinctSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 5
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	// 20 repeats from a single source must never cross a threshold of 5
	// distinct sources.
	for i := 0; i < 20; i++ {
		d.Observe(evt("198.51.100.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected repeats from one source alone to never flag, got %+v", fs.List())
	}

	// The 5th distinct source crosses it.
	for i := 0; i < 5; i++ {
		d.Observe(evt(fmt.Sprintf("198.51.100.%d", i+2), 22, now))
	}
	if len(fs.List()) != 1 {
		t.Fatalf("expected 5 distinct sources to flag, got %+v", fs.List())
	}
}
