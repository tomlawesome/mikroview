package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type ruleWindow struct {
	samples      []time.Time
	lastActivity time.Time
	baseline     float64 // EMA of events/sec for this rule
	primed       bool
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
			d.evictOldestRuleWindow()
		}
		w = &ruleWindow{}
		d.ruleWindows[e.RuleLabel] = w
	}
	w.lastActivity = now
	w.samples = append(w.samples, now)

	cutoff := now.Add(-d.cfg.RuleSpikeWindow)
	i := 0
	for i < len(w.samples) && w.samples[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		w.samples = w.samples[i:]
	}

	currentRate := float64(len(w.samples)) / d.cfg.RuleSpikeWindow.Seconds()

	if !w.primed {
		w.baseline = currentRate
		w.primed = true
		return
	}

	if currentRate >= d.cfg.RuleSpikeMinRate && w.baseline > 0 && currentRate >= w.baseline*d.cfg.RuleSpikeMultiplier {
		d.fs.Add(flags.TypeRuleSpike, e.RuleLabel,
			fmt.Sprintf("%.1f hits/s vs a baseline of %.1f for this rule", currentRate, w.baseline), now)
	}

	w.baseline = emaAlpha*currentRate + (1-emaAlpha)*w.baseline
}

func (d *Detector) evictOldestRuleWindow() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, w := range d.ruleWindows {
		if first || w.lastActivity.Before(oldest) {
			oldestKey, oldest, first = k, w.lastActivity, false
		}
	}
	if oldestKey != "" {
		delete(d.ruleWindows, oldestKey)
	}
}
