package detect

import (
	"fmt"
	"math"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// hostActivityMinZ is the minimum "standard deviations above this host's
// own normal" before a reading is even considered -- below this, normal
// variance in a host's own traffic is a perfectly ordinary explanation,
// not an anomaly worth a human's attention.
const hostActivityMinZ = 2.0

// hostActivityFullConfidenceZ is the deviation at which the confidence
// score's deviation component maxes out. Chosen well above hostActivityMinZ
// so confidence actually scales across the range a real flag can occur
// in, rather than every flag reading close to 100%.
const hostActivityFullConfidenceZ = 6.0

// hostActivityMinSamples is a hard floor: a host with fewer prior
// observations than this can never raise a flag, no matter how extreme
// the first few readings look. A baseline built from 1-2 samples isn't a
// baseline -- there's nothing to have deviated from yet.
const hostActivityMinSamples = 5

// checkHostActivityBaseline is the per-host counterpart to
// GlobalSpikeDetector (network-wide) and observeRuleRate (per rule): an
// EMA baseline of this specific source's own event rate, so a host
// that's always busy is judged against its own normal rather than one
// fixed threshold applied to every host equally. Unlike those two, it
// also tracks a rolling variance so it can express *how* unusual a
// reading is (a z-score) rather than just "over the line" -- which is
// what makes a meaningful confidence score possible: a small deviation
// backed by a long history and a huge deviation backed by three samples
// should not read as equally trustworthy.
//
// currentRate is spikeCount from the caller's already-computed window
// (see observeScanAndSpike) -- events in ActivitySpikeWindow, reused
// rather than recomputed.
func (d *Detector) checkHostActivityBaseline(w *sourceWindow, srcIP string, currentRate int, now time.Time) {
	rate := float64(currentRate)

	if !w.primed {
		w.baseline = rate
		w.variance = 0
		w.primed = true
		w.sampleCount = 1
		return
	}

	prevBaseline := w.baseline
	stddev := math.Sqrt(w.variance)

	var z float64
	switch {
	case stddev > 0:
		z = (rate - prevBaseline) / stddev
	case rate > prevBaseline:
		// Perfectly steady baseline so far (variance still 0) and this
		// reading is above it -- genuinely unusual for this host, but
		// with no variance estimate yet to size it against. Capped well
		// below hostActivityFullConfidenceZ so a tiny sample count still
		// can't reach full deviation-confidence on its own; sampleCount's
		// own gate below is what actually holds this back.
		z = hostActivityFullConfidenceZ
	default:
		z = 0
	}

	if w.sampleCount >= hostActivityMinSamples &&
		z >= hostActivityMinZ &&
		rate >= float64(d.cfg.ActivitySpikeThreshold) &&
		prevBaseline > 0 &&
		rate >= prevBaseline*d.cfg.HostActivityMultiplier {

		historyConfidence := math.Min(1, float64(w.sampleCount)/float64(d.cfg.HostActivityWarmupSamples))
		deviationConfidence := math.Min(1, math.Max(0, (z-hostActivityMinZ)/(hostActivityFullConfidenceZ-hostActivityMinZ)))
		confidence := int(math.Round(historyConfidence * deviationConfidence * 100))

		detail := fmt.Sprintf(
			"%d events in %s vs a baseline of %.1f for this host (based on %d samples, %.1fσ above normal)",
			currentRate, d.cfg.ActivitySpikeWindow, prevBaseline, w.sampleCount, z,
		)
		d.fs.AddWithConfidence(flags.TypeActivitySpike, srcIP, detail, confidence, now)
	}

	// EMA update: standard exponentially-weighted mean/variance -- same
	// alpha as GlobalSpikeDetector/observeRuleRate, applied after the
	// check above so the flag (if any) compares against the baseline as
	// it stood *before* this reading, not after.
	diff := rate - w.baseline
	incr := emaAlpha * diff
	w.baseline += incr
	w.variance = (1 - emaAlpha) * (w.variance + diff*incr)
	if w.sampleCount < d.cfg.HostActivityWarmupSamples {
		w.sampleCount++
	}
}
