// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

func newTestGlobalSpike(t *testing.T, cfg Config) (*GlobalSpikeDetector, *flags.Store) {
	t.Helper()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return NewGlobalSpikeDetector(cfg, fs), fs
}

func TestGlobalSpikeFirstCallOnlyPrimesBaseline(t *testing.T) {
	cfg := DefaultConfig()
	g, fs := newTestGlobalSpike(t, cfg)

	g.Check(500, time.Now()) // absurdly high, but it's the first reading
	if len(fs.List()) != 0 {
		t.Fatalf("expected the first Check() to only prime the baseline, got %+v", fs.List())
	}
}

func TestGlobalSpikeFlagsWellAboveBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5
	g, fs := newTestGlobalSpike(t, cfg)

	now := time.Now()
	g.Check(10, now) // primes baseline at 10

	g.Check(60, now.Add(time.Second)) // 6x baseline, well above minimum
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeGlobalSpike || list[0].Target != "global" {
		t.Fatalf("expected a global_spike flag, got %+v", list)
	}
}

func TestGlobalSpikeIgnoresLowAbsoluteVolume(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 2
	cfg.GlobalSpikeMinEPS = 5
	g, fs := newTestGlobalSpike(t, cfg)

	now := time.Now()
	g.Check(0.5, now) // baseline primed near-idle

	// 4x baseline, but still below GlobalSpikeMinEPS -- shouldn't fire
	g.Check(2, now.Add(time.Second))
	if len(fs.List()) != 0 {
		t.Fatalf("expected low absolute volume to be ignored regardless of the multiplier, got %+v", fs.List())
	}
}

func TestGlobalSpikeBaselineAdaptsSoRepeatedNormalTrafficStopsFlagging(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5
	g, fs := newTestGlobalSpike(t, cfg)

	now := time.Now()
	g.Check(10, now) // baseline = 10

	// sustained higher traffic for many samples should pull the baseline
	// up until it's no longer flagged as a spike relative to itself
	for i := 0; i < 500; i++ {
		g.Check(40, now.Add(time.Duration(i+1)*time.Second))
	}

	before := len(fs.List())
	g.Check(40, now.Add(501*time.Second))
	after := len(fs.List())
	if after > before {
		t.Errorf("expected the baseline to have adapted to sustained 40eps traffic, but a new flag was raised (before=%d, after=%d)", before, after)
	}
}

func TestGlobalSpikeDisabledNeverFires(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	seed := DefaultSettingsMap()
	seed[DetectorGlobalSpike] = Settings{Enabled: false}
	settings, err := OpenSettingsStore("", seed)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGlobalSpikeDetectorWithSettings(cfg, fs, settings)

	now := time.Now()
	g.Check(10, now)
	g.Check(1000, now.Add(time.Second)) // would trivially spike if enabled
	if len(fs.List()) != 0 {
		t.Fatalf("expected a disabled global-spike detector to never fire, got %+v", fs.List())
	}
}

// TestGlobalSpikeFlagsCarryConfidence covers issue #59: the flag itself
// should carry a non-nil confidence score (the same z-score-against-EMA
// approach host_baseline.go uses), without changing when a flag fires
// (see the other tests in this file for that -- unchanged).
func TestGlobalSpikeFlagsCarryConfidence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5
	cfg.GlobalSpikeWarmupSamples = 20
	g, fs := newTestGlobalSpike(t, cfg)

	now := time.Now()
	g.Check(10, now) // primes baseline at 10

	g.Check(60, now.Add(time.Second)) // 6x baseline, well above minimum
	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag, got %+v", list)
	}
	if list[0].Confidence == nil {
		t.Fatal("expected the global_spike flag to carry a confidence score, got nil")
	}
	if *list[0].Confidence < 0 || *list[0].Confidence > 100 {
		t.Errorf("expected confidence in [0, 100], got %d", *list[0].Confidence)
	}
}

// TestGlobalSpikeConfidenceGrowsWithSampleHistory confirms the history
// component actually moves the score: an identical relative spike
// should read as more confident once the baseline has more samples
// behind it, all else equal.
func TestGlobalSpikeConfidenceGrowsWithSampleHistory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5
	cfg.GlobalSpikeWarmupSamples = 20
	g, fs := newTestGlobalSpike(t, cfg)

	now := time.Now()
	g.Check(10, now) // primes baseline, sampleCount=1

	// A handful of steady readings to build some variance/history
	// without letting the baseline drift far from 10.
	for i := 0; i < 3; i++ {
		g.Check(10, now.Add(time.Duration(i+1)*time.Second))
	}
	g.Check(60, now.Add(4*time.Second)) // spike with a short history
	early := fs.List()
	if len(early) != 1 || early[0].Confidence == nil {
		t.Fatalf("expected one confident flag from the early spike, got %+v", early)
	}
	earlyConfidence := *early[0].Confidence

	// Fresh detector, same spike, but with a much longer steady history
	// first.
	g2, fs2 := newTestGlobalSpike(t, cfg)
	g2.Check(10, now)
	for i := 0; i < 19; i++ {
		g2.Check(10, now.Add(time.Duration(i+1)*time.Second))
	}
	g2.Check(60, now.Add(20*time.Second))
	late := fs2.List()
	if len(late) != 1 || late[0].Confidence == nil {
		t.Fatalf("expected one confident flag from the late spike, got %+v", late)
	}
	lateConfidence := *late[0].Confidence

	if lateConfidence <= earlyConfidence {
		t.Errorf("expected more sample history to read as more confident for an equivalent spike: early=%d, late=%d", earlyConfidence, lateConfidence)
	}
}

func TestGlobalSpikeReenableRePrimesRatherThanFlaggingImmediately(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GlobalSpikeMultiplier = 4
	cfg.GlobalSpikeMinEPS = 5

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	seed := DefaultSettingsMap()
	settings, err := OpenSettingsStore("", seed)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGlobalSpikeDetectorWithSettings(cfg, fs, settings)

	now := time.Now()
	g.Check(10, now) // primes baseline at 10

	settings.Set(DetectorGlobalSpike, Settings{Enabled: false})
	g.Check(1000, now.Add(time.Second)) // no-op while disabled

	settings.Set(DetectorGlobalSpike, Settings{Enabled: true})
	// First call after re-enabling should only re-prime, not immediately
	// compare 1000 against the stale baseline of 10.
	g.Check(1000, now.Add(2*time.Second))
	if len(fs.List()) != 0 {
		t.Fatalf("expected the first reading after re-enabling to only re-prime, got %+v", fs.List())
	}
}
