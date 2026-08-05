package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// emaAlpha weights how much each new EPS reading moves the baseline --
// a slow-moving average (2% weight per sample) so one genuine spike
// doesn't immediately drag the baseline up and mask itself, while still
// adapting over many samples to a real, sustained change in normal
// traffic levels.
const emaAlpha = 0.02

// GlobalSpikeDetector compares the store's current events-per-second
// figure against a slow exponential-moving-average baseline of itself,
// raising a flag when current activity is a large multiple of that
// baseline. Checked periodically (see main.go), not per-event, since
// "spike relative to normal" is a property of a rate over time, not of
// any single event.
type GlobalSpikeDetector struct {
	cfg      Config
	fs       *flags.Store
	settings *SettingsStore
	baseline float64
	variance float64
	primed   bool
	// sampleCount backs the confidence score's history component (see
	// emaConfidence) -- capped at GlobalSpikeWarmupSamples, same
	// warmup-then-cap pattern as sourceWindow.sampleCount.
	sampleCount int
}

// NewGlobalSpikeDetector constructs a detector that's always enabled --
// see NewGlobalSpikeDetectorWithSettings for on/off control (issue #44;
// global spike is network-wide, not keyed by anything per-source, so it
// has no scope, only Enabled). Kept for callers and existing tests that
// don't need that control.
func NewGlobalSpikeDetector(cfg Config, fs *flags.Store) *GlobalSpikeDetector {
	return NewGlobalSpikeDetectorWithSettings(cfg, fs, AllEnabledSettingsStore())
}

func NewGlobalSpikeDetectorWithSettings(cfg Config, fs *flags.Store, settings *SettingsStore) *GlobalSpikeDetector {
	return &GlobalSpikeDetector{cfg: cfg, fs: fs, settings: settings}
}

// Check updates the baseline with the current EPS reading and raises a
// TypeGlobalSpike flag if current activity is at least
// Config.GlobalSpikeMultiplier times the baseline (and at least
// GlobalSpikeMinEPS, so "3 events/s vs a baseline of 0.5" doesn't fire
// on essentially idle traffic). The very first call only primes the
// baseline -- there's nothing to compare against yet.
func (g *GlobalSpikeDetector) Check(currentEPS float64, now time.Time) {
	if !g.settings.Get(DetectorGlobalSpike).Enabled {
		// Marks the baseline stale rather than leaving the EMA running
		// on data nobody asked to watch: re-enabling re-primes on the
		// next reading instead of instantly comparing against whatever
		// the baseline happened to be when it was switched off.
		g.primed = false
		g.sampleCount = 0
		return
	}

	if !g.primed {
		g.baseline = currentEPS
		g.variance = 0
		g.primed = true
		g.sampleCount = 1
		return
	}

	prevBaseline := g.baseline
	// The firing condition itself is unchanged from before confidence
	// scoring existed (issue #59): only the confidence attached to a
	// flag that already fires is new, not when it fires.
	if currentEPS >= g.cfg.GlobalSpikeMinEPS && prevBaseline > 0 && currentEPS >= prevBaseline*g.cfg.GlobalSpikeMultiplier {
		z := emaZScore(currentEPS, prevBaseline, g.variance)
		confidence := emaConfidence(z, g.sampleCount, g.cfg.GlobalSpikeWarmupSamples)
		g.fs.AddWithConfidence(flags.TypeGlobalSpike, "global",
			fmt.Sprintf("%.1f events/s vs a baseline of %.1f (based on %d samples, %.1fσ above normal)", currentEPS, prevBaseline, g.sampleCount, z),
			confidence, now)
	}

	g.baseline, g.variance = emaUpdate(currentEPS, g.baseline, g.variance)
	if g.sampleCount < g.cfg.GlobalSpikeWarmupSamples {
		g.sampleCount++
	}
}
