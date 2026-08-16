// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"sync"
	"testing"
	"time"
)

// ---- BaselineFloor: the three declarable shapes ----

// TestBaselineFloorSampleCountOnlyShape mirrors
// internal/detect.hostActivityMinSamples (host_baseline.go): a floor
// expressed purely as a sample count, no duration component.
func TestBaselineFloorSampleCountOnlyShape(t *testing.T) {
	floor := BaselineFloor{MinSamples: 5}
	if floor.cleared(0, 4) {
		t.Error("floor cleared with 4 samples against MinSamples: 5")
	}
	if !floor.cleared(0, 5) {
		t.Error("floor not cleared with 5 samples against MinSamples: 5, despite zero elapsed duration")
	}
	// A huge duration alone must not substitute for samples.
	if floor.cleared(365*24*time.Hour, 4) {
		t.Error("floor cleared on duration alone -- MinSamples must still gate it")
	}
}

// TestBaselineFloorDurationOnlyShape mirrors
// internal/detect.Config.LowSlowScanMinObservation (low_slow_scan.go): a
// floor expressed purely as a duration since first-seen, no sample-count
// component.
func TestBaselineFloorDurationOnlyShape(t *testing.T) {
	floor := BaselineFloor{MinDuration: 45 * time.Minute}
	if floor.cleared(44*time.Minute, 1_000_000) {
		t.Error("floor cleared 1 minute short of MinDuration, however many samples")
	}
	if !floor.cleared(45*time.Minute, 0) {
		t.Error("floor not cleared at exactly MinDuration, despite zero samples")
	}
}

// TestBaselineFloorBothShape mirrors
// internal/detect.Config.OffHoursMinSampleDays (off_hours.go): a floor
// whose "distinct prior days of history" requirement is inherently both
// a sample count and, because each sample is a calendar day, a
// wall-clock duration that has to have elapsed for that many samples to
// even be possible -- BaselineFloor expresses this as both dimensions
// set, and both must independently clear.
func TestBaselineFloorBothShape(t *testing.T) {
	floor := BaselineFloor{MinDuration: 14 * 24 * time.Hour, MinSamples: 14}
	if floor.cleared(20*24*time.Hour, 13) {
		t.Error("floor cleared with enough duration but too few samples")
	}
	if floor.cleared(10*24*time.Hour, 20) {
		t.Error("floor cleared with enough samples but too little duration")
	}
	if !floor.cleared(14*24*time.Hour, 14) {
		t.Error("floor not cleared when both dimensions exactly meet their minimum")
	}
}

// ---- priming and firing gating ----

func TestBaselineDoesNotPrimeBeforeAFullyObservedWindow(t *testing.T) {
	const window = time.Minute
	b := NewBaseline(window, BaselineFloor{}, UpdatePerEvent)
	now := time.Now()

	snap := b.Reading(now, 1)
	if snap.Primed {
		t.Fatal("Baseline primed on the very first reading -- priming must wait for a fully-observed window")
	}
	snap = b.Reading(now.Add(30*time.Second), 5)
	if snap.Primed {
		t.Fatal("Baseline primed before window elapsed since firstSeen")
	}

	// This call's returned snapshot reflects state going *into* the
	// call -- still unprimed, since priming happens as a result of this
	// call once observedFor >= window.
	snap = b.Reading(now.Add(window), 5)
	if snap.Primed {
		t.Fatal("Reading's returned snapshot must reflect pre-call state, not the priming this call just performed")
	}
	next := b.Reading(now.Add(window+time.Second), 5)
	if !next.Primed {
		t.Fatal("Baseline never primed even once a full window had elapsed")
	}
}

func TestBaselineFireFalseWhenNotReadyRegardlessOfZScore(t *testing.T) {
	// A floor that can never clear during this test (MinSamples very
	// high) -- even an extreme, obviously-anomalous reading must not
	// fire, because Ready gates Fire unconditionally.
	const window = time.Second
	b := NewBaseline(window, BaselineFloor{MinSamples: 1000}, UpdatePerEvent)
	now := time.Now()

	// Prime with a steady low value, then feed one huge outlier.
	b.Reading(now, 1)
	for i := 1; i <= 5; i++ {
		b.Reading(now.Add(time.Duration(i)*window), 1)
	}
	snap := b.Reading(now.Add(6*window), 100000)
	if snap.Ready {
		t.Fatal("snapshot reports Ready despite the floor's MinSamples being nowhere close to cleared")
	}
	if snap.Fire(emaMinZ) {
		t.Fatal("Fire reported true despite Ready being false -- the floor must refuse the firing condition, not just discourage it")
	}
}

func TestBaselineFireTrueOnceReadyAndZScoreClearsMinZ(t *testing.T) {
	const window = time.Second
	b := NewBaseline(window, BaselineFloor{MinSamples: 3}, UpdatePerEvent)
	now := time.Now()

	// Prime, then feed several identical readings so variance stays at
	// 0 and z-score is deterministic (see emaZScore's stddev==0 branch).
	b.Reading(now, 10)
	for i := 1; i <= 4; i++ {
		b.Reading(now.Add(time.Duration(i)*window), 10)
	}
	// A reading well above the steady baseline, with variance still 0,
	// scores emaZScore's capped 6.0 -- comfortably above emaMinZ.
	snap := b.Reading(now.Add(5*window), 500)
	if !snap.Ready {
		t.Fatal("snapshot not Ready after 5 primed readings against MinSamples: 3")
	}
	if !snap.Fire(emaMinZ) {
		t.Fatalf("Fire reported false for snapshot %+v, want true", snap)
	}
}

// ---- #368 reproduction ----

// TestBaselineDoesNotFalselyFireInsideFirstWindowFromColdStart reproduces
// #368 directly against Baseline: perfectly steady traffic under one
// rule label, from a cold start, must never read as a spike merely
// because the rolling-window count is still filling up. The naive fix
// (gate *firing* on a sample-count floor, prime on the first reading
// regardless) still seeds the wrong baseline and just moves the false
// emission to the moment the floor clears -- Baseline.Reading's
// window-aware priming (see its own doc comment) is what actually
// closes this, not the floor alone.
func TestBaselineDoesNotFalselyFireInsideFirstWindowFromColdStart(t *testing.T) {
	const window = 60 * time.Second
	const eventSpacing = 3 * time.Second // steady, unremarkable traffic
	const multiplier = 3.0               // mirrors HostActivityMultiplier's role
	const minZ = emaMinZ

	ring := NewCountRing(window)
	baseline := NewBaseline(window, BaselineFloor{MinSamples: 5}, UpdatePerEvent)

	start := time.Now()
	fired := false
	for elapsed := time.Duration(0); elapsed < 4*window; elapsed += eventSpacing {
		now := start.Add(elapsed)
		ring.Add(now, true)
		// The reading a real per-event detector would compute: events
		// in the trailing window as of now -- mechanically low near a
		// cold start purely because the window hasn't finished filling,
		// exactly the artifact #368 is about.
		reading := float64(ring.Count(now, window))

		snap := baseline.Reading(now, reading)
		if snap.Fire(minZ) && snap.Value > 0 && reading >= snap.Value*multiplier {
			fired = true
			t.Logf("false fire at elapsed=%s: reading=%.0f baseline=%.2f z=%.2f samples=%d",
				elapsed, reading, snap.Value, snap.ZScore, snap.Samples)
		}
	}
	if fired {
		t.Fatal("baseline fired on perfectly steady traffic from a cold start -- reproduces #368")
	}
}

// TestBaselineNeverPrimesOnAnArtificiallyLowFirstReading is the same
// scenario's more targeted assertion: the specific priming bug #368
// found was seeding value from a reading taken before a full window had
// elapsed. Confirm directly that the eventual prime reflects
// steady-state traffic, not the near-zero count from event 1.
func TestBaselineNeverPrimesOnAnArtificiallyLowFirstReading(t *testing.T) {
	const window = 60 * time.Second
	const eventSpacing = 3 * time.Second
	ring := NewCountRing(window)
	baseline := NewBaseline(window, BaselineFloor{}, UpdatePerEvent)

	start := time.Now()
	var now time.Time
	for elapsed := time.Duration(0); elapsed <= window; elapsed += eventSpacing {
		now = start.Add(elapsed)
		ring.Add(now, true)
		baseline.Reading(now, float64(ring.Count(now, window)))
	}
	// One more call to read back what priming actually seeded --
	// Reading returns the state as of *before* the call, so this is
	// what the loop's last call (the one that crossed the window
	// boundary and primed) left behind.
	after := baseline.Reading(now.Add(eventSpacing), float64(ring.Count(now.Add(eventSpacing), window)))
	// The steady-state count for 3s spacing over a 60s window is ~20 --
	// a prime anywhere near that (not near 1, event 1's reading) proves
	// the cold-start artifact was discarded rather than seeded.
	if !after.Primed {
		t.Fatal("baseline never primed even after the window fully elapsed")
	}
	if after.Value < 10 {
		t.Fatalf("baseline primed at %.2f -- looks seeded from a near-empty early window, not a full one", after.Value)
	}
}

// ---- cadence is declarable, not fixed ----

func TestBaselineCadenceIsDeclaredAndIntrospectable(t *testing.T) {
	perEvent := NewBaseline(time.Minute, BaselineFloor{}, UpdatePerEvent)
	if got := perEvent.Cadence(); got != UpdatePerEvent {
		t.Errorf("Cadence() = %v, want UpdatePerEvent", got)
	}
	perWindow := NewBaseline(time.Minute, BaselineFloor{}, UpdatePerWindow)
	if got := perWindow.Cadence(); got != UpdatePerWindow {
		t.Errorf("Cadence() = %v, want UpdatePerWindow", got)
	}
}

// TestBaselineSupportsPerWindowCadence proves UpdatePerWindow is a real,
// usable path -- Reading called once per completed window (rather than
// once per event) still primes and gates identically. This is the shape
// #420's eventual fix needs (see UpdateCadence's doc comment); this
// issue only has to keep it possible, not implement it.
func TestBaselineSupportsPerWindowCadence(t *testing.T) {
	const window = time.Minute
	b := NewBaseline(window, BaselineFloor{MinSamples: 2}, UpdatePerWindow)
	start := time.Now()

	// One reading per completed window, not per event.
	b.Reading(start, 10)                      // window 0: does not prime (0 < window elapsed)
	first := b.Reading(start.Add(window), 12) // window 1: primes
	if first.Primed {
		t.Fatal("expected the window-1 call's *before* snapshot to reflect pre-prime state")
	}
	second := b.Reading(start.Add(2*window), 11) // window 2
	if !second.Primed {
		t.Fatal("expected Baseline primed by the second per-window reading")
	}
}

// ---- copy-on-read / concurrency ----

// TestBaselineSnapshotRaceSafeAgainstConcurrentReadings is a -race proof
// that Baseline's read API (Snapshot) is safe to call from any goroutine
// while the evaluation goroutine concurrently calls Reading. Run with
// `go test -race`.
func TestBaselineSnapshotRaceSafeAgainstConcurrentReadings(t *testing.T) {
	b := NewBaseline(time.Second, BaselineFloor{MinSamples: 2}, UpdatePerEvent)
	start := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			b.Reading(start.Add(time.Duration(i)*time.Second), float64(i%50))
			i++
		}
	}()

	for i := 0; i < 500; i++ {
		_ = b.Snapshot(time.Now())
	}
	close(stop)
	wg.Wait()
}
