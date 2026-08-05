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

func newTestDetectorWithSettings(t *testing.T, cfg Config, byName map[DetectorName]Settings) (*Detector, *flags.Store) {
	t.Helper()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := OpenSettingsStore("", byName)
	if err != nil {
		t.Fatal(err)
	}
	return NewWithSettings(cfg, fs, settings), fs
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

func TestActivitySpikeIgnoresSteadyBaselineTraffic(t *testing.T) {
	// A host with a low but perfectly steady rate should never flag, no
	// matter how long it keeps going -- this is exactly the false-positive
	// pattern (a naturally busy host) the per-host baseline replaced the
	// old fixed threshold to fix.
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 2
	cfg.ActivitySpikeWindow = time.Second
	cfg.PortScanThreshold = 1000

	d, fs := newTestDetector(t, cfg)
	now := time.Now()
	tick := time.Duration(0)
	for i := 0; i < 30; i++ {
		base := now.Add(tick)
		d.Observe(evt("198.51.100.4", 100, base))
		d.Observe(evt("198.51.100.4", 101, base.Add(10*time.Millisecond)))
		tick += 2 * time.Second
	}

	if len(fs.List()) != 0 {
		t.Fatalf("expected steady baseline traffic to never flag, got %+v", fs.List())
	}
}

func TestActivitySpikeFlagsGenuineDeviationFromHostsOwnBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 2
	cfg.ActivitySpikeWindow = time.Second
	cfg.PortScanThreshold = 1000
	cfg.HostActivityMultiplier = 3
	cfg.HostActivityWarmupSamples = 20

	d, fs := newTestDetector(t, cfg)
	ip := "198.51.100.4"
	now := time.Now()
	tick := time.Duration(0)

	// Warm up a steady baseline of ~2 events/window, spaced more than
	// ActivitySpikeWindow apart so each tick's window doesn't accumulate
	// into the next.
	for i := 0; i < 25; i++ {
		base := now.Add(tick)
		d.Observe(evt(ip, 100, base))
		d.Observe(evt(ip, 101, base.Add(10*time.Millisecond)))
		tick += 2 * time.Second
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected the warm-up phase itself to never flag, got %+v", fs.List())
	}

	// A genuine spike: well above the floor and several times the
	// established baseline, all within one window.
	spikeBase := now.Add(tick)
	for i := 0; i < 10; i++ {
		d.Observe(evt(ip, 200+i, spikeBase.Add(time.Duration(i)*10*time.Millisecond)))
	}

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeActivitySpike || list[0].Target != ip {
		t.Fatalf("expected an activity_spike flag for %s, got %+v", ip, list)
	}
	if list[0].Confidence == nil || *list[0].Confidence <= 0 || *list[0].Confidence > 100 {
		t.Fatalf("expected a confidence score in (0, 100], got %+v", list[0].Confidence)
	}
}

func TestActivitySpikeNeverFiresBeforeMinimumSampleFloor(t *testing.T) {
	// Calls checkHostActivityBaseline directly with a hand-controlled
	// rate, sidestepping observeScanAndSpike's cumulative-window counting
	// (where a tight burst's rate climbs with every call regardless of
	// sampleCount, making "no flag from a cold start" otherwise ambiguous
	// to assert by hand). Feeds the same extreme, easily-threshold-
	// clearing reading repeatedly -- proves the hard floor, not just that
	// nothing happened to be extreme enough yet.
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 1
	cfg.HostActivityMultiplier = 2
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.9"
	now := time.Now()

	d.checkHostActivityBaseline(w, ip, 1, now) // primes: sampleCount=1

	for i := 0; i < hostActivityMinSamples-1; i++ {
		d.checkHostActivityBaseline(w, ip, 100, now.Add(time.Duration(i+1)*time.Second))
		if len(fs.List()) != 0 {
			t.Fatalf("expected no flag while sampleCount < hostActivityMinSamples (call %d), got %+v", i+2, fs.List())
		}
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

func TestPortScanRespectsHostsDenylist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.PortScanWindow = time.Minute
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorPortScan] = Settings{
		Enabled: true,
		Scope:   Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	for port := 1; port <= 5; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected the denylisted source to never flag, got %+v", fs.List())
	}

	for port := 1; port <= 5; port++ {
		d.Observe(evt("203.0.113.10", port, now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) != 1 {
		t.Fatalf("expected a non-denylisted source to still flag, got %+v", fs.List())
	}
}

func TestPortScanPortsScopeRestrictsCountedPortsOnly(t *testing.T) {
	// Denylisting one port excludes it from the port-scan distinct-count,
	// but the underlying event still counts toward activity-spike's
	// total -- proving Ports/PortsMode narrows what port-scan counts,
	// not which events are tracked at all (see Scope's doc comment).
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.PortScanWindow = time.Minute
	cfg.ActivitySpikeThreshold = 3
	cfg.ActivitySpikeWindow = time.Minute
	cfg.HostActivityWarmupSamples = 1000 // keep the baseline detector from also firing

	seed := DefaultSettingsMap()
	seed[DetectorPortScan] = Settings{
		Enabled: true,
		Scope:   Scope{Ports: []int{9999}, PortsMode: ListModeDeny},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	for port := 1; port <= 3; port++ {
		d.Observe(evt("203.0.113.9", 9999, now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected a denylisted port to never count toward the distinct-port total, got %+v", fs.List())
	}
}

func TestPortScanAndActivitySpikeToggleIndependently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 3
	cfg.PortScanWindow = time.Minute
	cfg.ActivitySpikeThreshold = 3
	cfg.ActivitySpikeWindow = time.Minute
	cfg.HostActivityWarmupSamples = 1

	seed := DefaultSettingsMap()
	seed[DetectorActivitySpike] = Settings{Enabled: false}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	for port := 1; port <= 5; port++ {
		d.Observe(evt("203.0.113.9", port, now.Add(time.Duration(port)*time.Millisecond)))
	}

	list := fs.List()
	sawPortScan, sawActivitySpike := false, false
	for _, f := range list {
		switch f.Type {
		case flags.TypePortScan:
			sawPortScan = true
		case flags.TypeActivitySpike:
			sawActivitySpike = true
		}
	}
	if !sawPortScan {
		t.Error("expected port_scan to still fire while enabled")
	}
	if sawActivitySpike {
		t.Error("expected activity_spike to never fire while disabled, even on the same shared window")
	}
}

func TestEveryDetectorDisabledEntirelySuppressesItsFlagType(t *testing.T) {
	nameToType := map[DetectorName]flags.Type{
		DetectorPortScan:              flags.TypePortScan,
		DetectorActivitySpike:         flags.TypeActivitySpike,
		DetectorCriticalPort:          flags.TypeCriticalPort,
		DetectorDistributedBruteForce: flags.TypeDistributedBruteForce,
		DetectorOutboundAnomaly:       flags.TypeOutboundAnomaly,
		DetectorInternalRecon:         flags.TypeInternalRecon,
		DetectorRuleSpike:             flags.TypeRuleSpike,
		DetectorRepeatedDrops:         flags.TypeRepeatedDrops,
	}

	for name, flagType := range nameToType {
		t.Run(string(name), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.PortScanThreshold = 2
			cfg.ActivitySpikeThreshold = 2
			cfg.HostActivityWarmupSamples = 1
			cfg.CriticalPortThreshold = 2
			cfg.CriticalPorts = []int{22}
			cfg.DistributedBruteForceThreshold = 2
			cfg.OutboundAnomalyThreshold = 2
			cfg.InternalReconThreshold = 2
			cfg.RuleSpikeMultiplier = 2
			cfg.RuleSpikeMinRate = 0
			cfg.RepeatedDropsThreshold = 2

			seed := DefaultSettingsMap()
			seed[name] = Settings{Enabled: false}
			d, fs := newTestDetectorWithSettings(t, cfg, seed)

			now := time.Now()
			// A barrage designed to trip every detector at once: distinct
			// external sources hitting a critical port (critical_port +
			// distributed_brute_force), an internal source touching many
			// internal and external destinations (internal_recon +
			// outbound_anomaly), a rule firing repeatedly (rule_spike),
			// and refused attempts against a local service
			// (repeated_drops) -- plus enough distinct ports/volume from
			// one source for port_scan/activity_spike.
			for i := 0; i < 10; i++ {
				t := now.Add(time.Duration(i) * time.Millisecond)
				scanner := store.Event{SrcIP: "198.51.100.50", DstIP: "192.168.1.1", DstPort: 1000 + i, ReceivedAt: t, RuleLabel: "r1"}
				d.Observe(scanner)

				bruteForceSrc := store.Event{SrcIP: "198.51.100." + string(rune('1'+i%9)), DstIP: "192.168.1.1", DstPort: 22, ReceivedAt: t}
				d.Observe(bruteForceSrc)

				internalScanner := store.Event{SrcIP: "192.168.1.50", DstIP: "192.168.1." + string(rune('1'+i%9)), DstPort: 80, ReceivedAt: t}
				d.Observe(internalScanner)
				outbound := store.Event{SrcIP: "192.168.1.50", DstIP: "203.0.113." + string(rune('1'+i%9)), DstPort: 443, ReceivedAt: t}
				d.Observe(outbound)

				drop := store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 8080, ReceivedAt: t, Action: store.ActionDrop}
				d.Observe(drop)
			}

			for _, f := range fs.List() {
				if f.Type == flagType {
					t.Fatalf("expected %s to never fire while %s is disabled, got %+v", flagType, name, f)
				}
			}
		})
	}
}
