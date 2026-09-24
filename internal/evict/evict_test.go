// SPDX-License-Identifier: AGPL-3.0-only

package evict

import (
	"testing"
	"time"
)

// TestBatchNeverReturnsZero: Batch says how many entries a shed removes,
// and 0 would mean a full map that has just overflowed makes no
// progress at all -- the very next key would overflow again.
func TestBatchNeverReturnsZero(t *testing.T) {
	for _, n := range []int{-5, -1, 0, 1, 2, 7, 8, 9, 15, 16, 100, 800, 8000} {
		if got := Batch(n); got < 1 {
			t.Errorf("Batch(%d) = %d, want >= 1", n, got)
		}
	}
}

// TestBatchIsOneEighth pins the arithmetic itself, not just the "never
// zero" floor: BatchFraction is the amortisation knob, and this table
// catches a change to the divisor as directly as a change to the floor.
func TestBatchIsOneEighth(t *testing.T) {
	tests := []struct{ n, want int }{
		{0, 1}, {1, 1}, {7, 1}, {8, 1}, {9, 1}, {15, 1}, {16, 2}, {800, 100}, {8000, 1000},
	}
	for _, tt := range tests {
		if got := Batch(tt.n); got != tt.want {
			t.Errorf("Batch(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

// TestTargetIsOneBatchBelowLimit pins the property the package exists
// for. Evicting back to exactly limit leaves the map full, so the very
// next new key overflows again and pays for another full sort -- the
// 87x slowdown recorded in the package doc. Target must land strictly
// below limit, by exactly one batch, so that many new keys are free
// before the map is full again.
func TestTargetIsOneBatchBelowLimit(t *testing.T) {
	for _, limit := range []int{2, 8, 9, 16, 100, 800, 8000} {
		target := Target(limit)
		batch := Batch(limit)
		if target != limit-batch {
			t.Errorf("Target(%d) = %d, want %d (limit - Batch(limit) = %d - %d)", limit, target, limit-batch, limit, batch)
		}
		if target >= limit {
			t.Errorf("Target(%d) = %d, not below limit -- the next new key would overflow immediately, reproducing the eviction-to-exactly-the-cap bug", limit, target)
		}
	}
}

// TestTargetFloorsAtOne: for a small enough limit, limit - Batch(limit)
// is 0 or negative. Target must still return a usable positive size --
// 0 would make DownTo remove everything, and a negative size makes no
// sense as a map size at all.
func TestTargetFloorsAtOne(t *testing.T) {
	for _, limit := range []int{-5, -1, 0, 1} {
		if got := Target(limit); got != 1 {
			t.Errorf("Target(%d) = %d, want 1", limit, got)
		}
	}
}

// TestDownToNegativeTargetRemovesEverything: a negative target is
// clamped to 0 rather than left to underflow the removal count.
func TestDownToNegativeTargetRemovesEverything(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	when := func(v int) time.Time { return time.Unix(int64(v), 0) }

	removed := DownTo(m, -3, when)
	if removed != 3 {
		t.Errorf("DownTo with negative target removed %d, want 3", removed)
	}
	if len(m) != 0 {
		t.Errorf("map has %d entries left, want 0", len(m))
	}
}

// TestDownToTargetAboveLenRemovesNothing: a target already at or above
// the map's size means there is nothing to shed.
func TestDownToTargetAboveLenRemovesNothing(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	when := func(v int) time.Time { return time.Unix(int64(v), 0) }

	removed := DownTo(m, 10, when)
	if removed != 0 {
		t.Errorf("DownTo with target above len(m) removed %d, want 0", removed)
	}
	if len(m) != 2 {
		t.Errorf("map has %d entries left, want 2 (unchanged)", len(m))
	}
}

// TestDownToRemovesLeastRecentlyActive: under a cap the entries worth
// keeping are the ones still active, so the shed must read activity
// through when and remove the stalest entries first, not arbitrary
// ones a map range happens to visit.
func TestDownToRemovesLeastRecentlyActive(t *testing.T) {
	base := time.Now()
	m := map[string]time.Time{
		"oldest": base,
		"middle": base.Add(time.Hour),
		"newest": base.Add(2 * time.Hour),
	}
	when := func(v time.Time) time.Time { return v }

	removed := DownTo(m, 1, when)
	if removed != 2 {
		t.Fatalf("removed %d, want 2", removed)
	}
	if _, ok := m["newest"]; !ok {
		t.Error("DownTo removed the most-recently-active entry; it must remove the stalest ones first")
	}
	if _, ok := m["oldest"]; ok {
		t.Error("DownTo kept the stalest entry; it must be the first one removed")
	}
	if len(m) != 1 {
		t.Errorf("map has %d entries left, want 1", len(m))
	}
}

// TestShedLeavesHeadroomForABatch ties Batch, Target and DownTo
// together end to end: shedding down to Target(limit) after crossing
// limit must leave exactly Batch(limit) entries of headroom, so that
// many more insertions land free before a caller needs to shed again.
// That is the amortisation the package promises -- a sort every n/8
// insertions instead of a scan on every one -- and is exactly what an
// eviction-to-exactly-the-cap implementation fails to provide.
func TestShedLeavesHeadroomForABatch(t *testing.T) {
	const limit = 800
	m := make(map[int]time.Time, limit+1)
	now := time.Now()
	for i := 0; i <= limit; i++ { // one past the cap, forcing a shed
		m[i] = now.Add(time.Duration(i) * time.Millisecond)
	}

	batch := Batch(limit)
	target := Target(limit)
	when := func(v time.Time) time.Time { return v }

	DownTo(m, target, when)

	after := len(m)
	if after != target {
		t.Fatalf("map holds %d entries after the shed, want %d", after, target)
	}
	headroom := limit - after
	if headroom != batch {
		t.Fatalf("shed left %d entries of headroom, want %d (Batch(%d)) -- the next batch of new keys would trigger another shed before filling it",
			headroom, batch, limit)
	}

	// Filling exactly the headroom must land the map back at limit,
	// not force a second shed partway through.
	for i := 0; i < headroom; i++ {
		m[limit+1+i] = now.Add(time.Hour)
	}
	if len(m) != limit {
		t.Fatalf("map holds %d entries after filling the headroom, want %d", len(m), limit)
	}
}
