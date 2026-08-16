// SPDX-License-Identifier: AGPL-3.0-only

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

// TestScanAndSpikeIgnoreEstablishedTraffic reproduces the false-positive
// pattern a busy server produces when a RouterOS ruleset logs both
// directions of an established connection: many "established" events
// (the *client's* varying ephemeral port, high volume) must not trip
// activity-spike, even though a "new"-only version of the same traffic
// would. port_scan's own half of this guarantee (it shared
// isTrackableConnState/observeScanAndSpike's filter) moved with it onto
// internal/engine's shipped declarative definition (issue #405) -- see
// internal/engine/shipped_declarative_test.go for its counterpart there.
func TestScanAndSpikeIgnoreEstablishedTraffic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 3
	cfg.ActivitySpikeWindow = time.Minute
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for port := 1; port <= 5; port++ {
		d.Observe(evtState("192.168.1.10", port, "established", now.Add(time.Duration(port)*time.Millisecond)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected established-state traffic to never trip activity-spike, got %+v", fs.List())
	}
	if _, ok := d.perSource["192.168.1.10"]; ok {
		t.Fatal("expected established-state traffic to never even touch per-source window state")
	}

	// The same volume of "new" traffic is recorded (proving this is a
	// state filter, not an accidental threshold change) -- activity_spike
	// itself needs a primed EMA baseline to actually fire, which its own
	// dedicated tests (TestActivitySpikeFlagsGenuineDeviationFromHostsOwnBaseline
	// and friends) already cover; this test only pins the connState gate.
	for port := 1; port <= 5; port++ {
		d.Observe(evtState("192.168.1.11", port, "new", now.Add(time.Duration(port)*time.Millisecond)))
	}
	w, ok := d.perSource["192.168.1.11"]
	if !ok || w.spikes.Count(now.Add(6*time.Millisecond), cfg.ActivitySpikeWindow) != 5 {
		t.Fatalf("expected new-state traffic to still be recorded into the per-source window")
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

	d.checkHostActivityBaseline(w, ip, "", "", 1, now) // primes: sampleCount=1

	for i := 0; i < hostActivityMinSamples-1; i++ {
		d.checkHostActivityBaseline(w, ip, "", "", 100, now.Add(time.Duration(i+1)*time.Second))
		if len(fs.List()) != 0 {
			t.Fatalf("expected no flag while sampleCount < hostActivityMinSamples (call %d), got %+v", i+2, fs.List())
		}
	}
}

func TestActivitySpikeStillFiresWhenWarmupSamplesBelowFloor(t *testing.T) {
	// A plausible operator tuning attempt -- "trust it faster" -- used
	// to cap sampleCount at HostActivityWarmupSamples even when that
	// value sat below hostActivityMinSamples, so the firing gate
	// (sampleCount >= hostActivityMinSamples) could never pass again:
	// lowering the warmup to detect *sooner* silently disabled detection
	// entirely, permanently, for every host. Proves the counter still
	// climbs to the hard floor regardless of a lower warmup setting.
	cfg := DefaultConfig()
	cfg.ActivitySpikeThreshold = 1
	cfg.HostActivityMultiplier = 2
	cfg.HostActivityWarmupSamples = 2 // below hostActivityMinSamples (5)
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.10"
	now := time.Now()

	d.checkHostActivityBaseline(w, ip, "", "", 1, now) // primes: sampleCount=1

	for i := 0; i < hostActivityMinSamples; i++ {
		d.checkHostActivityBaseline(w, ip, "", "", 100, now.Add(time.Duration(i+1)*time.Second))
	}

	if len(fs.List()) != 1 {
		t.Fatalf("expected activity_spike to fire once sampleCount reaches hostActivityMinSamples despite a lower HostActivityWarmupSamples, got %+v", fs.List())
	}
}

// TestReFiringUpdatesExistingFlagInPlace moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortReFiringUpdatesExistingFlagInPlace (issue #405:
// critical_port is now a shipped declarative definition evaluated by
// internal/engine, not internal/detect -- see shipped_declarative.go's
// buildCriticalPortDefinition). Every pinned value carried over unchanged.

// TestCriticalPortFlagsOnlyForExternalSources moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortFlagsOnlyForExternalSources (issue #405). Every
// pinned value carried over unchanged.

// TestCriticalPortIgnoresNonCriticalPorts moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortIgnoresNonCriticalPorts (issue #405). Every
// pinned value carried over unchanged.

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

func TestEveryDetectorDisabledEntirelySuppressesItsFlagType(t *testing.T) {
	nameToType := map[DetectorName]flags.Type{
		DetectorActivitySpike: flags.TypeActivitySpike,
		// DetectorCriticalPort, DetectorDistributedBruteForce,
		// DetectorRepeatedDrops and DetectorRuleSpike are deliberately
		// absent here now: all four moved to internal/engine (issue #405)
		// -- the first three as shipped declarative definitions, rule_spike
		// as a shipped programmatic one (its own baseline/history-floor
		// logic didn't fit the declarative shape the other three share) --
		// so internal/detect no longer evaluates any of them. Their own
		// enable/disable pins now live in
		// internal/engine/shipped_declarative_test.go's
		// TestShippedCriticalPortDisabledIsInert and its
		// TestShippedDistributedBruteForce*/TestShippedRepeatedDrops*
		// counterparts (generically, every ported declarative definition's
		// disabled-definition contract is the same one
		// TestShippedCriticalPortDisabledIsInert pins), and rule_spike's in
		// internal/engine/shipped_rule_spike_test.go's
		// TestShippedRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives,
		// which toggles Enabled false then true and asserts nothing fires
		// during the off period.
		DetectorOutboundAnomaly: flags.TypeOutboundAnomaly,
		DetectorInternalRecon:   flags.TypeInternalRecon,
	}

	for name, flagType := range nameToType {
		t.Run(string(name), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.PortScanThreshold = 2
			cfg.ActivitySpikeThreshold = 2
			cfg.HostActivityWarmupSamples = 1
			cfg.OutboundAnomalyThreshold = 2
			cfg.InternalReconThreshold = 2

			seed := DefaultSettingsMap()
			seed[name] = Settings{Enabled: false}
			d, fs := newTestDetectorWithSettings(t, cfg, seed)

			now := time.Now()
			// A barrage designed to trip every detector this test still
			// evaluates at once: an internal source touching many internal
			// and external destinations (internal_recon + outbound_anomaly),
			// and enough distinct ports/volume from one source for
			// port_scan/activity_spike. (Older revisions of this barrage
			// also included a rule firing repeatedly for rule_spike and a
			// distinct-external-sources-hitting-a-critical-port block for
			// critical_port/distributed_brute_force, and a refused-attempt
			// block for repeated_drops -- removed alongside their nameToType
			// entries above, since all four detectors moved to
			// internal/engine and no longer evaluate anything this test
			// observes. scanner's RuleLabel is vestigial now that
			// rule_spike is gone from this test, but harmless, so it is
			// left as-is rather than reshaping an event literal for no
			// behavioural gain.)
			for i := 0; i < 10; i++ {
				t := now.Add(time.Duration(i) * time.Millisecond)
				scanner := store.Event{SrcIP: "198.51.100.50", DstIP: "192.168.1.1", DstPort: 1000 + i, ReceivedAt: t, RuleLabel: "r1"}
				d.Observe(scanner)

				internalScanner := store.Event{SrcIP: "192.168.1.50", DstIP: "192.168.1." + string(rune('1'+i%9)), DstPort: 80, ReceivedAt: t}
				d.Observe(internalScanner)
				outbound := store.Event{SrcIP: "192.168.1.50", DstIP: "203.0.113." + string(rune('1'+i%9)), DstPort: 443, ReceivedAt: t}
				d.Observe(outbound)
			}

			for _, f := range fs.List() {
				if f.Type == flagType {
					t.Fatalf("expected %s to never fire while %s is disabled, got %+v", flagType, name, f)
				}
			}
		})
	}
}

// TestCriticalPortConfidenceScalesWithOvershoot moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortConfidenceScalesWithOvershoot (issue #405). Every
// pinned value carried over unchanged.

// evtCountry is evt() with an explicit SrcCountry, for tests asserting
// a flag's Country field is threaded through from the triggering event.
func evtCountry(srcIP, country string, dstPort int, at time.Time) store.Event {
	e := evt(srcIP, dstPort, at)
	e.SrcCountry = country
	return e
}

// TestCriticalPortCarriesCountry moved to
// internal/engine/shipped_declarative_test.go's
// TestShippedCriticalPortCarriesCountry (issue #405). Every pinned value
// carried over unchanged.

func TestActivitySpikeCarriesCountry(t *testing.T) {
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

	for i := 0; i < 25; i++ {
		base := now.Add(tick)
		d.Observe(evtCountry(ip, "FR", 100, base))
		d.Observe(evtCountry(ip, "FR", 101, base.Add(10*time.Millisecond)))
		tick += 2 * time.Second
	}
	spikeBase := now.Add(tick)
	for i := 0; i < 10; i++ {
		d.Observe(evtCountry(ip, "FR", 200+i, spikeBase.Add(time.Duration(i)*10*time.Millisecond)))
	}

	list := fs.List()
	if len(list) != 1 || list[0].Country != "FR" {
		t.Fatalf("expected Country to be threaded through activity_spike, got %+v", list)
	}
}
