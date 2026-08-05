package detect

import "testing"

func TestEmaZScoreZeroVarianceAboveBaseline(t *testing.T) {
	z := emaZScore(10, 5, 0)
	if z != emaFullConfidenceZ {
		t.Errorf("emaZScore with zero variance and rate > baseline = %v, want emaFullConfidenceZ (%v)", z, emaFullConfidenceZ)
	}
}

func TestEmaZScoreZeroVarianceAtOrBelowBaseline(t *testing.T) {
	if z := emaZScore(5, 5, 0); z != 0 {
		t.Errorf("emaZScore at baseline with zero variance = %v, want 0", z)
	}
	if z := emaZScore(3, 5, 0); z != 0 {
		t.Errorf("emaZScore below baseline with zero variance = %v, want 0", z)
	}
}

func TestEmaZScoreWithVariance(t *testing.T) {
	// stddev = 2, rate is 3 stddev above baseline
	z := emaZScore(16, 10, 4)
	if z != 3 {
		t.Errorf("emaZScore(16, 10, variance=4) = %v, want 3", z)
	}
}

func TestEmaConfidenceScalesWithSampleCountAndZ(t *testing.T) {
	// Full history, z at the ceiling -> full confidence.
	if c := emaConfidence(emaFullConfidenceZ, 20, 20); c != 100 {
		t.Errorf("expected full confidence at full history + max z, got %d", c)
	}
	// Full history, z at the floor -> zero confidence.
	if c := emaConfidence(emaMinZ, 20, 20); c != 0 {
		t.Errorf("expected zero confidence at the z floor, got %d", c)
	}
	// Below the z floor -> clamped to zero, not negative.
	if c := emaConfidence(0, 20, 20); c != 0 {
		t.Errorf("expected confidence clamped to zero below the z floor, got %d", c)
	}
	// Partial history caps confidence even at max z.
	if c := emaConfidence(emaFullConfidenceZ, 5, 20); c != 25 {
		t.Errorf("expected 25%% confidence at 5/20 warmup samples with max z, got %d", c)
	}
	// More samples than warmup still caps at full history-confidence
	// (1, not >1) rather than overshooting.
	if c := emaConfidence(emaFullConfidenceZ, 40, 20); c != 100 {
		t.Errorf("expected sampleCount beyond warmupSamples to still cap at 100, got %d", c)
	}
}

func TestEmaUpdateMovesTowardReading(t *testing.T) {
	baseline, variance := emaUpdate(20, 10, 0)
	if baseline <= 10 || baseline >= 20 {
		t.Errorf("expected the updated baseline to move toward the reading without jumping straight to it, got %v", baseline)
	}
	if variance <= 0 {
		t.Errorf("expected variance to become positive after a reading that deviates from the prior baseline, got %v", variance)
	}
}
