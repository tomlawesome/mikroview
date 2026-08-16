// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// withMaxTrackedKeys shrinks maxTrackedKeys for the duration of a test --
// same convention as engine.withQueueSize/withDrainTimeout and
// internal/detect.maxTrackedSources: a var rather than a const purely so
// tests don't need thousands of distinct keys to exercise eviction.
func withMaxTrackedKeys(t *testing.T, n int) {
	t.Helper()
	orig := maxTrackedKeys
	maxTrackedKeys = n
	t.Cleanup(func() { maxTrackedKeys = orig })
}

func TestKeyedGetOrCreateReturnsSameValueForSameKey(t *testing.T) {
	k := NewKeyed[*int]()
	now := time.Now()
	calls := 0
	newValue := func() *int { calls++; v := 0; return &v }

	a := k.GetOrCreate("x", now, newValue)
	b := k.GetOrCreate("x", now, newValue)
	if a != b {
		t.Fatal("GetOrCreate returned different values for the same key")
	}
	if calls != 1 {
		t.Fatalf("newValue called %d times, want 1", calls)
	}
}

func TestKeyedGetReturnsFalseForUnknownKeyWithoutCreating(t *testing.T) {
	k := NewKeyed[*int]()
	if _, ok := k.Get("missing"); ok {
		t.Fatal("Get reported a value for a key that was never created")
	}
	if k.Len() != 0 {
		t.Fatalf("Len() = %d after a Get on a missing key, want 0 -- Get must not create", k.Len())
	}
}

func TestKeyedGetDoesNotUpdateLastActivity(t *testing.T) {
	withMaxTrackedKeys(t, 4)
	k := NewKeyed[*int]()
	now := time.Now()
	newValue := func() *int { v := 0; return &v }

	// Key "old" is created first (and so is least-recently-active by
	// GetOrCreate's own bookkeeping) -- repeated Get calls on it, spaced
	// out in time, must NOT count as activity, or it would never be the
	// eviction target below.
	k.GetOrCreate("old", now, newValue)
	for i := 0; i < 10; i++ {
		k.Get("old")
	}

	k.GetOrCreate("b", now.Add(time.Second), newValue)
	k.GetOrCreate("c", now.Add(2*time.Second), newValue)
	k.GetOrCreate("d", now.Add(3*time.Second), newValue)
	// Pushes the map to maxTrackedKeys+1, forcing an eviction batch.
	k.GetOrCreate("e", now.Add(4*time.Second), newValue)

	if _, ok := k.Get("old"); ok {
		t.Error("expected \"old\" to be evicted as least-recently-active despite intervening Get calls")
	}
}

func TestKeyedEvictsLeastRecentlyActiveOnceOverCap(t *testing.T) {
	withMaxTrackedKeys(t, 4)
	k := NewKeyed[*int]()
	now := time.Now()
	newValue := func() *int { v := 0; return &v }

	for i := 0; i < 4; i++ {
		k.GetOrCreate(fmt.Sprintf("k%d", i), now.Add(time.Duration(i)*time.Second), newValue)
	}
	if k.Len() != 4 {
		t.Fatalf("Len() = %d, want 4 before overflow", k.Len())
	}

	// One more key overflows the cap -- internal/evict.Batch(4) == 1, so
	// exactly the oldest ("k0") should go.
	k.GetOrCreate("k4", now.Add(4*time.Second), newValue)

	if _, ok := k.Get("k0"); ok {
		t.Error("expected the least-recently-active key (k0) to be evicted")
	}
	for _, key := range []string{"k1", "k2", "k3", "k4"} {
		if _, ok := k.Get(key); !ok {
			t.Errorf("expected %q to survive eviction, it did not", key)
		}
	}
}

func TestKeyedForEachVisitsEveryEntry(t *testing.T) {
	k := NewKeyed[*int]()
	now := time.Now()
	for i := 0; i < 5; i++ {
		i := i
		k.GetOrCreate(fmt.Sprintf("k%d", i), now, func() *int { return &i })
	}

	seen := make(map[string]bool)
	k.ForEach(func(key string, v *int) bool {
		seen[key] = true
		return true
	})
	if len(seen) != 5 {
		t.Fatalf("ForEach visited %d entries, want 5", len(seen))
	}
}

func TestKeyedForEachStopsWhenFnReturnsFalse(t *testing.T) {
	k := NewKeyed[*int]()
	now := time.Now()
	for i := 0; i < 5; i++ {
		v := i
		k.GetOrCreate(fmt.Sprintf("k%d", i), now, func() *int { return &v })
	}

	visited := 0
	k.ForEach(func(key string, v *int) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Fatalf("ForEach visited %d entries after fn returned false, want 1", visited)
	}
}

// TestKeyedSnapshotIsADeepCopy is the non-concurrent half of the
// copy-on-read proof: mutating the value handed back by Snapshot must
// never be visible through GetOrCreate, and vice versa.
func TestKeyedSnapshotIsADeepCopy(t *testing.T) {
	k := NewKeyed[*EvidenceSet]()
	now := time.Now()
	live := k.GetOrCreate("src", now, NewEvidenceSet)
	live.AddPort(22)

	snap := k.Snapshot(func(v *EvidenceSet) *EvidenceSet {
		c := NewEvidenceSet()
		for _, p := range v.Ports() {
			c.AddPort(p)
		}
		return c
	})

	live.AddPort(443) // mutate the live value after the snapshot was taken

	if got := snap["src"].Ports(); len(got) != 1 || got[0] != 22 {
		t.Fatalf("snapshot Ports() = %v, want [22] -- it must not see the later AddPort(443)", got)
	}
	if got := live.Ports(); len(got) != 2 {
		t.Fatalf("live Ports() = %v, want 2 entries", got)
	}
}

// TestKeyedSnapshotRaceSafeAgainstConcurrentWrites is the -race proof
// for the copy-on-read contract Keyed.Snapshot and EvidenceSet's own
// read API state: reading via Snapshot while the evaluation goroutine
// concurrently calls GetOrCreate and mutates the returned *EvidenceSet
// must never trip the race detector. Run with `go test -race`.
func TestKeyedSnapshotRaceSafeAgainstConcurrentWrites(t *testing.T) {
	k := NewKeyed[*EvidenceSet]()
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
			key := fmt.Sprintf("10.0.0.%d", i%16)
			ev := k.GetOrCreate(key, time.Now(), NewEvidenceSet)
			ev.AddPort(i % 100)
			ev.AddHost(fmt.Sprintf("203.0.113.%d", i%20))
			ev.AddLabel(fmt.Sprintf("r%d", i%5))
			i++
		}
	}()

	clone := func(v *EvidenceSet) *EvidenceSet {
		c := NewEvidenceSet()
		for _, p := range v.Ports() {
			c.AddPort(p)
		}
		for _, h := range v.Hosts() {
			c.AddHost(h)
		}
		for _, l := range v.Labels() {
			c.AddLabel(l)
		}
		return c
	}
	for i := 0; i < 500; i++ {
		_ = k.Snapshot(clone)
	}
	close(stop)
	wg.Wait()
}
