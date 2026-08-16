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

// TestScanAndSpikeIgnoreEstablishedTraffic moved to
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpikeIgnoresEstablishedTraffic (issue #405:
// activity_spike is now a shipped programmatic definition evaluated by
// internal/engine, not internal/detect -- see shipped_activity_spike.go).
// port_scan's own half of this guarantee moved earlier still, onto
// internal/engine's shipped declarative definition -- see
// internal/engine/shipped_declarative_test.go for its counterpart there.
// Every pinned value carried over unchanged.

// TestActivitySpikeIgnoresSteadyBaselineTraffic and
// TestActivitySpikeFlagsGenuineDeviationFromHostsOwnBaseline moved to
// internal/engine (issue #405: activity_spike is now a shipped
// programmatic definition evaluated by internal/engine, not
// internal/detect -- see shipped_activity_spike.go).
//
// The first (a low, perfectly steady rate never flags, however long it
// continues) is covered by
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpike_ContinuousRampNeverFiresAtDefaultConfig: that
// test's own doc comment states the structural "never fires" result
// holds for any single-episode ramp shape, and a flat/steady rate is the
// zero-slope case of that same shape -- the mechanism (the baseline
// chasing the rate closely enough that rate < baseline*multiplier never
// releases) is the one this test pinned too, just demonstrated at a
// different config scale (this test used a small custom
// threshold/window to get a boundary reachable within a short test,
// rather than DefaultConfig's real 200/3x).
//
// The second (a genuine, sharp deviation from an established baseline
// does flag, with a valid confidence score) is only partially covered.
// TestShippedActivitySpike_FieldsRefireClearRevive pins the same
// boundary/fields/confidence/re-fire/clear/revive behaviour this test
// exercised, but -- like the pre-existing
// TestActivitySpikeNeverFiresBeforeMinimumSampleFloor pattern this test
// followed -- it drives checkBaseline directly rather than through
// Observe()/Evaluate(), because #420 means no input through the ordinary
// event path can reach a firing state at DefaultConfig's real
// thresholds. What this test additionally proved -- that a real burst of
// events run through the actual Observe()/Evaluate() ring-and-baseline
// path (not a hand-fed rate) can still produce a fire, at a non-default
// but plausible config -- has no engine-side counterpart: every
// Evaluate()-driven activity_spike test on the engine side either
// asserts no flag (the ramp test above, the established-traffic test,
// and the Classification-scope test's external-source half) or drives
// checkBaseline directly. Flagged in this port's report rather than
// silently dropped.

// TestActivitySpikeNeverFiresBeforeMinimumSampleFloor moved to
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpikeNeverFiresBeforeMinimumSampleFloor (issue
// #405). Every pinned value carried over unchanged.

// TestActivitySpikeStillFiresWhenWarmupSamplesBelowFloor moved to
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpikeStillFiresWhenWarmupSamplesBelowFloor (issue
// #405). Every pinned value carried over unchanged.

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
		// DetectorActivitySpike, DetectorCriticalPort,
		// DetectorDistributedBruteForce, DetectorRepeatedDrops and
		// DetectorRuleSpike are deliberately absent here now: all five
		// moved to internal/engine (issue #405) -- critical_port,
		// distributed_brute_force and repeated_drops as shipped
		// declarative definitions, activity_spike and rule_spike as
		// shipped programmatic ones (their own baseline/history-floor
		// logic didn't fit the declarative shape the other three share) --
		// so internal/detect no longer evaluates any of them. Their own
		// enable/disable pins now live in
		// internal/engine/shipped_declarative_test.go's
		// TestShippedCriticalPortDisabledIsInert and its
		// TestShippedDistributedBruteForce*/TestShippedRepeatedDrops*
		// counterparts (generically, every ported declarative definition's
		// disabled-definition contract is the same one
		// TestShippedCriticalPortDisabledIsInert pins). The two
		// programmatic ones share their own single gate instead
		// (programmaticBase.active, which every shipped programmatic
		// definition's Evaluate checks before doing anything else):
		// rule_spike's half is
		// internal/engine/shipped_rule_spike_test.go's
		// TestShippedRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives
		// and global_spike's is
		// internal/engine/shipped_global_spike_test.go's
		// TestShippedGlobalSpikeDisabledNeverFires; activity_spike does not
		// have its own dedicated disabled-definition test alongside them
		// (see this port's report).
		DetectorOutboundAnomaly: flags.TypeOutboundAnomaly,
		DetectorInternalRecon:   flags.TypeInternalRecon,
	}

	for name, flagType := range nameToType {
		t.Run(string(name), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.PortScanThreshold = 2
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

// TestActivitySpikeCarriesCountry moved to internal/engine: covered by
// the Country=="FR" assertion in
// internal/engine/shipped_activity_spike_test.go's
// TestShippedActivitySpike_FieldsRefireClearRevive (issue #405). Every
// pinned value carried over unchanged.
