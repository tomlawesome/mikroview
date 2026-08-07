// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"
)

func TestBucketSpanForFloorsShortWindows(t *testing.T) {
	// A 10s window divided into 60 buckets would be ~166ms each --
	// floored to minBucketSpan instead, so short windows don't collapse
	// to sub-second buckets no detector in this package needs.
	if got := bucketSpanFor(10 * time.Second); got != minBucketSpan {
		t.Errorf("expected a short window to floor at minBucketSpan, got %v", got)
	}
	// A window comfortably above 60*minBucketSpan divides evenly.
	if got := bucketSpanFor(3 * time.Hour); got != 3*time.Minute {
		t.Errorf("expected bucketSpanFor(3h) = 3m, got %v", got)
	}
}

func TestCountRingAddAndCount(t *testing.T) {
	r := newCountRing(time.Minute)
	now := time.Now()

	for i := 0; i < 5; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), true)
	}
	if got := r.Count(now.Add(4*time.Second), time.Minute); got != 5 {
		t.Errorf("expected 5 events counted, got %d", got)
	}
}

func TestCountRingRatioTracksTrueVsTotal(t *testing.T) {
	r := newCountRing(time.Minute)
	now := time.Now()

	for i := 0; i < 10; i++ {
		isTrue := i%2 == 0 // 5 true, 5 false
		r.Add(now.Add(time.Duration(i)*time.Second), isTrue)
	}
	trueCount, total := r.Ratio(now.Add(9*time.Second), time.Minute)
	if total != 10 {
		t.Errorf("expected total 10, got %d", total)
	}
	if trueCount != 5 {
		t.Errorf("expected trueCount 5, got %d", trueCount)
	}
}

func TestCountRingExcludesEventsOutsideWindow(t *testing.T) {
	r := newCountRing(10 * time.Second)
	now := time.Now()

	r.Add(now, true)
	later := now.Add(21 * time.Second) // well past the 10s window, and past the bucket's own reset point
	r.Add(later, true)
	r.Add(later.Add(time.Second), true)

	if got := r.Count(later.Add(time.Second), 10*time.Second); got != 2 {
		t.Errorf("expected the first, now-stale event excluded, got count %d", got)
	}
}

func TestCountRingBucketResetsLazily(t *testing.T) {
	// Same bucket slot reused a full ring-rotation later must not carry
	// over the earlier count -- the lazy-reset-on-Add behavior mirrored
	// from internal/store/ring.go's secBuckets.
	r := newCountRing(time.Second) // span floors to 1s regardless
	now := time.Now().Truncate(time.Second)

	r.Add(now, true)
	if got := r.Count(now, time.Second); got != 1 {
		t.Fatalf("expected 1 immediately after Add, got %d", got)
	}

	// Advance by a full windowBucketCount*span rotation so the same
	// array index is reused for a new, unrelated second.
	muchLater := now.Add(windowBucketCount * time.Second)
	if got := r.Count(muchLater, time.Second); got != 0 {
		t.Errorf("expected the stale bucket to read as empty after rotation, got %d", got)
	}
	r.Add(muchLater, true)
	if got := r.Count(muchLater, time.Second); got != 1 {
		t.Errorf("expected only the new Add to be counted after reset, got %d", got)
	}
}

func TestDistinctRingAddCountValues(t *testing.T) {
	r := newDistinctRing[int](time.Minute)
	now := time.Now()

	for _, p := range []int{22, 80, 22, 443, 80} {
		r.Add(now, p)
	}
	if got := r.Count(now, time.Minute, nil); got != 3 {
		t.Errorf("expected 3 distinct ports, got %d", got)
	}
	values := r.Values(now, time.Minute, nil)
	if len(values) != 3 {
		t.Errorf("expected Values to return the same 3 distinct ports, got %v", values)
	}
	for _, p := range []int{22, 80, 443} {
		if _, ok := values[p]; !ok {
			t.Errorf("expected %d in Values, got %v", p, values)
		}
	}
}

func TestDistinctRingFilterAppliesAtQueryTime(t *testing.T) {
	// Mirrors a live SettingsStore.Set narrowing a detector's scope:
	// the same stored samples read differently depending on the filter
	// passed to *this* call, not what was true when Add ran.
	r := newDistinctRing[int](time.Minute)
	now := time.Now()
	for _, p := range []int{22, 80, 443} {
		r.Add(now, p)
	}

	allowOnly80 := func(p int) bool { return p == 80 }
	if got := r.Count(now, time.Minute, allowOnly80); got != 1 {
		t.Errorf("expected the filter to narrow the count to 1, got %d", got)
	}
	if got := r.Count(now, time.Minute, nil); got != 3 {
		t.Errorf("expected an unfiltered query to still see all 3, got %d", got)
	}
}

func TestDistinctRingExcludesValuesOutsideWindow(t *testing.T) {
	r := newDistinctRing[string](10 * time.Second)
	now := time.Now()

	r.Add(now, "1.1.1.1")
	later := now.Add(21 * time.Second)
	r.Add(later, "2.2.2.2")

	got := r.Values(later, 10*time.Second, nil)
	if len(got) != 1 {
		t.Fatalf("expected the stale value to have aged out, got %v", got)
	}
	if _, ok := got["2.2.2.2"]; !ok {
		t.Errorf("expected the recent value to still be present, got %v", got)
	}
}

func TestDistinctRingBucketResetClearsPriorValues(t *testing.T) {
	r := newDistinctRing[string](time.Second)
	now := time.Now().Truncate(time.Second)

	r.Add(now, "1.1.1.1")
	muchLater := now.Add(windowBucketCount * time.Second)
	if got := r.Count(muchLater, time.Second, nil); got != 0 {
		t.Fatalf("expected the stale bucket's value to be gone after rotation, got %d", got)
	}
	r.Add(muchLater, "2.2.2.2")
	values := r.Values(muchLater, time.Second, nil)
	if len(values) != 1 {
		t.Fatalf("expected only the post-reset value, got %v", values)
	}
	if _, ok := values["1.1.1.1"]; ok {
		t.Errorf("expected the pre-reset value to be gone, got %v", values)
	}
}

func TestDistinctRingCapsPerBucket(t *testing.T) {
	// maxDistinctPerBucket is a memory ceiling, not a detector threshold
	// -- confirm it actually bounds bucket growth rather than being
	// purely aspirational.
	r := newDistinctRing[int](time.Minute)
	now := time.Now()
	for i := 0; i < maxDistinctPerBucket+50; i++ {
		r.Add(now, i)
	}
	if got := r.Count(now, time.Minute, nil); got != maxDistinctPerBucket {
		t.Errorf("expected the bucket to cap at %d distinct values, got %d", maxDistinctPerBucket, got)
	}
}

func TestDistinctRingCountIsAllocationFree(t *testing.T) {
	r := newDistinctRing[int](time.Minute)
	now := time.Now()
	for i := 0; i < 20; i++ {
		r.Add(now, i)
	}

	allocs := testing.AllocsPerRun(100, func() {
		r.Count(now, time.Minute, nil)
	})
	if allocs != 0 {
		t.Errorf("expected Count to be allocation-free on the hot path, got %.1f allocs/op", allocs)
	}
}
