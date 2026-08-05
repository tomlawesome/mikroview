package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func newTestDetector(t *testing.T, cfg Config) (*Detector, *flags.Store) {
	t.Helper()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, fs), fs
}

func evt(srcIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: "192.168.1.1", DstPort: dstPort, ReceivedAt: at}
}

// evtState builds an event with an explicit ConnState -- mirrors evt()'s
// shape (see below) for the connState-filtering tests, since evt() always
// leaves ConnState at its zero value ("").
func evtState(srcIP string, dstPort int, connState string, at time.Time) store.Event {
	e := evt(srcIP, dstPort, at)
	e.ConnState = connState
	return e
}

func TestScanAndSpikeIgnoreEstablishedTraffic(t *testing.T) {
	// Reproduces the false-positive pattern a busy server produces when a
	// RouterOS ruleset logs both directions of an established connection:
	// many "established" events with distinct ports (the *client's*
	// varying ephemeral port) and high volume must not trip the port-scan
	// or activity-spike detectors, even though a "new"-only version of the
	// same traffic would.
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.ActivitySpikeThreshold = 3
	cfg.PortScanWindow = time.Minute
	cfg.ActivitySpikeWindow = time.Minute
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for port := 1; port <= 5; port++ {
		d.Observe(evtState("192.168.1.10", port, "established", now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected established-state traffic to never trip port-scan/activity-spike, got %+v", fs.List())
	}

	// The same volume of "new" traffic still flags as before -- confirms
	// this is a state filter, not an accidental threshold change.
	for port := 1; port <= 5; port++ {
		d.Observe(evtState("192.168.1.11", port, "new", now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) == 0 {
		t.Fatalf("expected new-state traffic to still trip port-scan/activity-spike")
	}
}

func TestPortScanFlagsAtThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 5
	cfg.PortScanWindow = time.Minute
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for port := 1; port <= 4; port++ {
		d.Observe(evt("203.0.113.9", port, now))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below threshold, got %+v", fs.List())
	}

	d.Observe(evt("203.0.113.9", 5, now))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypePortScan || list[0].Target != "203.0.113.9" {
		t.Fatalf("expected a port_scan flag for 203.0.113.9, got %+v", list)
	}
}

func TestPortScanIgnoresSamplesOutsideWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.PortScanWindow = 10 * time.Second
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(evt("203.0.113.9", 1, now))
	d.Observe(evt("203.0.113.9", 2, now.Add(20*time.Second))) // outside the 10s window from the first
	d.Observe(evt("203.0.113.9", 3, now.Add(21*time.Second)))

	if len(fs.List()) != 0 {
		t.Fatalf("expected the first sample to have aged out, got %+v", fs.List())
	}
}

func TestActivitySpikeFlagsAtThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 5
	cfg.ActivitySpikeWindow = time.Minute
	// keep the port-scan threshold high so it doesn't also fire and
	// muddy this test's assertion
	cfg.PortScanThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 5; i++ {
		// same dest port every time -- a burst of activity, not a scan
		d.Observe(evt("198.51.100.4", 443, now.Add(time.Duration(i)*time.Second)))
	}

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeActivitySpike || list[0].Target != "198.51.100.4" {
		t.Fatalf("expected an activity_spike flag for 198.51.100.4, got %+v", list)
	}
}

func TestReFiringUpdatesExistingFlagInPlace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.PortScanWindow = time.Hour
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(evt("203.0.113.9", 1, now))
	d.Observe(evt("203.0.113.9", 2, now))
	d.Observe(evt("203.0.113.9", 3, now)) // crosses the threshold
	d.Observe(evt("203.0.113.9", 4, now.Add(time.Second)))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected re-firing to update one flag in place, not create another, got %d: %+v", len(list), list)
	}
	if list[0].Count < 2 {
		t.Errorf("expected Count to reflect multiple firings, got %d", list[0].Count)
	}
}

func TestCriticalPortFlagsOnlyForExternalSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CriticalPortThreshold = 3
	cfg.CriticalPortWindow = time.Minute
	cfg.CriticalPorts = []int{22}
	// keep the other detectors from also firing on the same traffic
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(evt("192.168.1.50", 22, now.Add(time.Duration(i)*time.Second))) // private, should never flag
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag for a private-source critical-port attempt, got %+v", fs.List())
	}

	for i := 0; i < 3; i++ {
		d.Observe(evt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeCriticalPort || list[0].Target != "198.51.100.4" {
		t.Fatalf("expected a critical_port flag for the external source only, got %+v", list)
	}
}

func TestCriticalPortIgnoresNonCriticalPorts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CriticalPortThreshold = 1
	cfg.CriticalPorts = []int{22}
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	d.Observe(evt("198.51.100.4", 80, time.Now()))
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag for a non-critical port, got %+v", fs.List())
	}
}

func TestEvictsOldestSourceWhenOverCap(t *testing.T) {
	orig := maxTrackedSources
	maxTrackedSources = 2
	defer func() { maxTrackedSources = orig }()

	cfg := DefaultConfig()
	d, _ := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(evt("1.1.1.1", 1, now))
	d.Observe(evt("2.2.2.2", 1, now.Add(time.Second)))
	if len(d.perSource) != 2 {
		t.Fatalf("expected 2 tracked sources, got %d", len(d.perSource))
	}

	// third distinct source should evict the least-recently-active one (1.1.1.1)
	d.Observe(evt("3.3.3.3", 1, now.Add(2*time.Second)))
	if len(d.perSource) != 2 {
		t.Fatalf("expected eviction to hold the tracked-source count at the cap, got %d", len(d.perSource))
	}
	if _, ok := d.perSource["1.1.1.1"]; ok {
		t.Error("expected the least-recently-active source (1.1.1.1) to be evicted")
	}
}
