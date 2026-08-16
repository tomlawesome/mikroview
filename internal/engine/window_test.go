// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"
)

func TestBucketSpanForFloorsShortWindows(t *testing.T) {
	if got := bucketSpanFor(10 * time.Second); got != minBucketSpan {
		t.Errorf("expected a short window to floor at minBucketSpan, got %v", got)
	}
	if got := bucketSpanFor(3 * time.Hour); got != 3*time.Minute {
		t.Errorf("expected bucketSpanFor(3h) = 3m, got %v", got)
	}
}

func TestCountRingAddAndCount(t *testing.T) {
	r := NewCountRing(time.Minute)
	now := time.Now()

	for i := 0; i < 5; i++ {
		r.Add(now.Add(time.Duration(i)*time.Second), true)
	}
	if got := r.Count(now.Add(4*time.Second), time.Minute); got != 5 {
		t.Errorf("expected 5 events counted, got %d", got)
	}
}

func TestCountRingRatioTracksTrueVsTotal(t *testing.T) {
	r := NewCountRing(time.Minute)
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
	r := NewCountRing(10 * time.Second)
	now := time.Now()

	r.Add(now, true)
	later := now.Add(21 * time.Second) // past the window and past the bucket's own reset point
	r.Add(later, true)
	r.Add(later.Add(time.Second), true)

	if got := r.Count(later.Add(time.Second), 10*time.Second); got != 2 {
		t.Errorf("expected only the 2 recent events counted, got %d", got)
	}
}

func TestDistinctRingCountAndValues(t *testing.T) {
	r := NewDistinctRing[int](time.Minute)
	now := time.Now()

	for _, p := range []int{22, 80, 443, 22, 80} { // 22/80 repeated
		r.Add(now, p)
	}
	if got := r.Count(now, time.Minute, nil); got != 3 {
		t.Errorf("expected 3 distinct ports, got %d", got)
	}
	values := r.Values(now, time.Minute, nil)
	if len(values) != 3 {
		t.Errorf("expected 3 distinct values, got %d: %v", len(values), values)
	}
	for _, p := range []int{22, 80, 443} {
		if _, ok := values[p]; !ok {
			t.Errorf("expected port %d in values, got %v", p, values)
		}
	}
}

func TestDistinctRingFilterAppliesAtQueryTime(t *testing.T) {
	r := NewDistinctRing[int](time.Minute)
	now := time.Now()
	r.Add(now, 22)
	r.Add(now, 8080)

	onlyLow := func(p int) bool { return p < 1024 }
	if got := r.Count(now, time.Minute, onlyLow); got != 1 {
		t.Errorf("expected filter to restrict to 1 distinct value, got %d", got)
	}
}

func TestDistinctRingCapsPerBucket(t *testing.T) {
	r := NewDistinctRing[int](time.Minute)
	now := time.Now()
	for p := 0; p < maxDistinctPerBucket+50; p++ {
		r.Add(now, p)
	}
	if got := r.Count(now, time.Minute, nil); got != maxDistinctPerBucket {
		t.Errorf("expected distinct count capped at %d, got %d", maxDistinctPerBucket, got)
	}
}

func TestDistinctRingValuesSharesNoBackingArrayAcrossCalls(t *testing.T) {
	r := NewDistinctRing[int](time.Minute)
	now := time.Now()
	r.Add(now, 22)

	first := r.Values(now, time.Minute, nil)
	first[9999] = struct{}{} // mutate the caller's copy
	second := r.Values(now, time.Minute, nil)
	if _, ok := second[9999]; ok {
		t.Error("Values returned a map sharing storage with a previous call's result")
	}
}
