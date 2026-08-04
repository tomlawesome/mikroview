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
	baseline float64
	primed   bool
}

func NewGlobalSpikeDetector(cfg Config, fs *flags.Store) *GlobalSpikeDetector {
	return &GlobalSpikeDetector{cfg: cfg, fs: fs}
}

// Check updates the baseline with the current EPS reading and raises a
// TypeGlobalSpike flag if current activity is at least
// Config.GlobalSpikeMultiplier times the baseline (and at least
// GlobalSpikeMinEPS, so "3 events/s vs a baseline of 0.5" doesn't fire
// on essentially idle traffic). The very first call only primes the
// baseline -- there's nothing to compare against yet.
func (g *GlobalSpikeDetector) Check(currentEPS float64, now time.Time) {
	if !g.primed {
		g.baseline = currentEPS
		g.primed = true
		return
	}

	if currentEPS >= g.cfg.GlobalSpikeMinEPS && g.baseline > 0 && currentEPS >= g.baseline*g.cfg.GlobalSpikeMultiplier {
		g.fs.Add(flags.TypeGlobalSpike, "global",
			fmt.Sprintf("%.1f events/s vs a baseline of %.1f", currentEPS, g.baseline), now)
	}

	g.baseline = emaAlpha*currentEPS + (1-emaAlpha)*g.baseline
}
