package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type ruleWindow struct {
	hits         *countRing // replaces a []time.Time hit list, sized to RuleSpikeWindow
	lastActivity time.Time
	baseline     float64 // EMA of events/sec for this rule
	variance     float64
	primed       bool
	// sampleCount backs the confidence score's history component (see
	// emaConfidence) -- capped at RuleSpikeWarmupSamples, same warmup-
	// then-cap pattern as sourceWindow.sampleCount.
	sampleCount int
}

// observeRuleRate tracks each rule's own hit rate against a slow-moving
// baseline of itself -- same EMA technique as GlobalSpikeDetector, but
// per rule label instead of network-wide, so a rule that's normally
// almost silent suddenly firing a lot is visible even when it's nowhere
// near large enough to move the network-wide total. Often the first
// sign of either a new attack pattern or a misconfiguration, since a
// rule "lighting up" after being quiet is inherently unusual regardless
// of absolute volume.
func (d *Detector) observeRuleRate(e store.Event, now time.Time) {
	w, ok := d.ruleWindows[e.RuleLabel]
	if !ok {
		if len(d.ruleWindows) >= maxTrackedSources {
			evictOldestByActivity(d.ruleWindows)
		}
		w = &ruleWindow{hits: newCountRing(d.cfg.RuleSpikeWindow)}
		d.ruleWindows[e.RuleLabel] = w
	}
	w.lastActivity = now
	w.hits.Add(now, true)

	currentRate := float64(w.hits.Count(now, d.cfg.RuleSpikeWindow)) / d.cfg.RuleSpikeWindow.Seconds()

	if !w.primed {
		w.baseline = currentRate
		w.variance = 0
		w.primed = true
		w.sampleCount = 1
		return
	}

	prevBaseline := w.baseline
	// The firing condition itself is unchanged from before confidence
	// scoring existed (issue #59): only the confidence attached to a
	// flag that already fires is new, not when it fires.
	if currentRate >= d.cfg.RuleSpikeMinRate && prevBaseline > 0 && currentRate >= prevBaseline*d.cfg.RuleSpikeMultiplier {
		z := emaZScore(currentRate, prevBaseline, w.variance)
		confidence := emaConfidence(z, w.sampleCount, d.cfg.RuleSpikeWarmupSamples)
		d.fs.AddWithConfidence(flags.TypeRuleSpike, e.RuleLabel,
			fmt.Sprintf("%.1f hits/s vs a baseline of %.1f for this rule (based on %d samples, %.1fσ above normal)", currentRate, prevBaseline, w.sampleCount, z),
			confidence, now)
	}

	w.baseline, w.variance = emaUpdate(currentRate, w.baseline, w.variance)
	if w.sampleCount < d.cfg.RuleSpikeWarmupSamples {
		w.sampleCount++
	}
}

func (w *ruleWindow) lastActivityTime() time.Time { return w.lastActivity }
