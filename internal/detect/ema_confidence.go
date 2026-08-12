// SPDX-License-Identifier: AGPL-3.0-only

package detect

import "math"

// emaMinZ/emaFullConfidenceZ: shared across every detector built on a
// per-source/per-rule/network-wide EMA baseline (checkHostActivityBaseline,
// GlobalSpikeDetector.Check, observeRuleRate) -- one tuning, one formula,
// rather than a separate copy per detector. emaMinZ is the deviation
// floor host_baseline.go's own firing gate uses (see
// checkHostActivityBaseline); emaFullConfidenceZ is where the confidence
// score's deviation component maxes out, chosen well above emaMinZ so
// confidence actually scales across the range a real flag can occur in
// rather than every flag reading close to 100%.
const (
	emaMinZ            = 2.0
	emaFullConfidenceZ = 6.0
)

// emaZScore reports how many standard deviations above baseline rate
// is, given the EMA's running variance.
func emaZScore(rate, baseline, variance float64) float64 {
	stddev := math.Sqrt(variance)
	switch {
	case stddev > 0:
		return (rate - baseline) / stddev
	case rate > baseline:
		// Perfectly steady baseline so far (variance still 0) and this
		// reading is above it -- genuinely unusual, but with no variance
		// estimate yet to size it against. Capped at emaFullConfidenceZ
		// rather than +Inf; emaConfidence's own sampleCount/warmupSamples
		// term is what actually keeps a tiny sample count from reading as
		// fully confident on this alone.
		return emaFullConfidenceZ
	default:
		return 0
	}
}

// emaConfidence turns a z-score and how much history backs the
// baseline (sampleCount vs warmupSamples) into a 0-100 confidence
// score -- a small deviation backed by a long history and a huge
// deviation backed by a handful of samples don't read as equally
// trustworthy. Doesn't decide *whether* to fire: callers keep their own
// existing firing condition (a threshold/multiplier check) unchanged;
// this only scores confidence once that condition already holds, so a
// low return value here just means "crossed the line, but not
// statistically unusual for this baseline's own volatility."
func emaConfidence(z float64, sampleCount, warmupSamples int) int {
	// warmupSamples comes from operator configuration and was divided by
	// unguarded. Zero gives 0/0 = NaN, and NaN survives both Min and
	// Round to land in Flag.Confidence as an arbitrary integer -- a
	// number an analyst reads as a judgement about how sure the detector
	// is. Treating a non-positive warmup as "no warmup required" is the
	// honest reading of the setting: nothing to wait for means full
	// history confidence immediately. See #285.
	historyConfidence := 1.0
	if warmupSamples > 0 {
		historyConfidence = math.Min(1, float64(sampleCount)/float64(warmupSamples))
	}
	deviationConfidence := math.Min(1, math.Max(0, (z-emaMinZ)/(emaFullConfidenceZ-emaMinZ)))
	return int(math.Round(historyConfidence * deviationConfidence * 100))
}

// emaUpdate advances an EMA baseline+variance by one reading -- the
// standard exponentially-weighted mean/variance update, shared by every
// detector using this technique. Callers apply this *after* computing
// z/confidence for the current reading, so a flag (if any) compares
// against the baseline as it stood before this reading, not after.
func emaUpdate(rate, baseline, variance float64) (newBaseline, newVariance float64) {
	diff := rate - baseline
	incr := emaAlpha * diff
	newBaseline = baseline + incr
	newVariance = (1 - emaAlpha) * (variance + diff*incr)
	return newBaseline, newVariance
}
