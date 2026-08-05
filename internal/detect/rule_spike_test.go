package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func ruleEvt(rule string, at time.Time) store.Event {
	return store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", RuleLabel: rule, ReceivedAt: at}
}

func TestRuleSpikeFirstWindowOnlyPrimesBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(ruleEvt("no-torrent", now))
	if len(fs.List()) != 0 {
		t.Fatalf("expected the first observation to only prime the baseline, got %+v", fs.List())
	}
}

func TestRuleSpikeFlagsWellAboveOwnBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuleSpikeWindow = time.Minute
	cfg.RuleSpikeMultiplier = 4
	cfg.RuleSpikeMinRate = 0.05
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	// prime a low baseline: a handful of hits in the first window
	for i := 0; i < 3; i++ {
		d.Observe(ruleEvt("no-torrent", now.Add(time.Duration(i)*10*time.Second)))
	}

	// next window: a burst well above 4x that baseline
	next := now.Add(cfg.RuleSpikeWindow + time.Second)
	for i := 0; i < 20; i++ {
		d.Observe(ruleEvt("no-torrent", next.Add(time.Duration(i)*time.Millisecond)))
	}

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeRuleSpike || list[0].Target != "no-torrent" {
		t.Fatalf("expected a rule_spike flag for the rule, got %+v", list)
	}
}

// TestRuleSpikeFlagsCarryConfidence covers issue #59: same z-score-
// against-EMA-baseline confidence host_baseline.go established, applied
// to rule_spike -- without changing when a flag fires (see the other
// tests in this file for that -- unchanged).
func TestRuleSpikeFlagsCarryConfidence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuleSpikeWindow = time.Minute
	cfg.RuleSpikeMultiplier = 4
	cfg.RuleSpikeMinRate = 0.05
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(ruleEvt("no-torrent", now.Add(time.Duration(i)*10*time.Second)))
	}

	next := now.Add(cfg.RuleSpikeWindow + time.Second)
	for i := 0; i < 20; i++ {
		d.Observe(ruleEvt("no-torrent", next.Add(time.Duration(i)*time.Millisecond)))
	}

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag, got %+v", list)
	}
	if list[0].Confidence == nil {
		t.Fatal("expected the rule_spike flag to carry a confidence score, got nil")
	}
	if *list[0].Confidence < 0 || *list[0].Confidence > 100 {
		t.Errorf("expected confidence in [0, 100], got %d", *list[0].Confidence)
	}
}

func TestRuleSpikeIgnoresLowAbsoluteRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuleSpikeWindow = time.Minute
	cfg.RuleSpikeMultiplier = 2
	cfg.RuleSpikeMinRate = 1.0 // 60/min floor
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(ruleEvt("quiet-rule", now)) // primes baseline near 0

	next := now.Add(cfg.RuleSpikeWindow + time.Second)
	for i := 0; i < 3; i++ { // 3x the "baseline" but nowhere near MinRate
		d.Observe(ruleEvt("quiet-rule", next.Add(time.Duration(i)*time.Second)))
	}

	if len(fs.List()) != 0 {
		t.Fatalf("expected low absolute rate to be ignored regardless of the multiplier, got %+v", fs.List())
	}
}

func TestRuleSpikeTracksEachRuleIndependently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuleSpikeWindow = time.Minute
	cfg.RuleSpikeMultiplier = 3
	cfg.RuleSpikeMinRate = 0.05
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000
	d, fs := newTestDetector(t, cfg)

	now := time.Now()
	d.Observe(ruleEvt("rule-a", now))
	d.Observe(ruleEvt("rule-b", now))

	next := now.Add(cfg.RuleSpikeWindow + time.Second)
	// only rule-a spikes
	for i := 0; i < 10; i++ {
		d.Observe(ruleEvt("rule-a", next.Add(time.Duration(i)*time.Millisecond)))
	}
	d.Observe(ruleEvt("rule-b", next))

	list := fs.List()
	if len(list) != 1 || list[0].Target != "rule-a" {
		t.Fatalf("expected only rule-a to be flagged, got %+v", list)
	}
}

func TestRuleSpikeRespectsRulesDenylist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuleSpikeWindow = time.Minute
	cfg.RuleSpikeMultiplier = 3
	cfg.RuleSpikeMinRate = 0.05
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	seed := DefaultSettingsMap()
	seed[DetectorRuleSpike] = Settings{
		Enabled: true,
		Scope:   Scope{Rules: []string{"noisy-rule"}, RulesMode: ListModeDeny},
	}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	now := time.Now()
	d.Observe(ruleEvt("noisy-rule", now))
	next := now.Add(cfg.RuleSpikeWindow + time.Second)
	for i := 0; i < 10; i++ {
		d.Observe(ruleEvt("noisy-rule", next.Add(time.Duration(i)*time.Millisecond)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected the denylisted rule to never flag, got %+v", fs.List())
	}
}
