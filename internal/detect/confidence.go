package detect

import "math"

// thresholdOvershootCeiling is how many multiples of a detector's own
// threshold correspond to 100% confidence for the plain threshold-
// crossing detectors below -- confidence here measures "how far over
// the line" an observed count is, not history or statistical deviation
// (see host_baseline.go's z-score approach for that, used only where a
// real EMA baseline exists). Just-crossed reads as low confidence;
// comfortably past it reads as high -- a self-hoster should read a 15%
// confidence flag very differently from a 95% one, even though both
// crossed the same line.
const thresholdOvershootCeiling = 3.0

// overshootConfidence scores how far count is past threshold: 0 (just
// crossed) to 100 (at or beyond thresholdOvershootCeiling times
// threshold). Only ever called once count >= threshold already holds
// (the detector's own firing condition), so count < threshold is not a
// case this needs to handle.
func overshootConfidence(count, threshold int) int {
	if threshold <= 0 {
		// No meaningful "over the line" concept to measure against --
		// treat as maximally confident rather than dividing by zero.
		return 100
	}
	ratio := float64(count-threshold) / float64(threshold) / (thresholdOvershootCeiling - 1)
	return int(math.Round(math.Min(1, math.Max(0, ratio)) * 100))
}
