// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// TestQueryBeforeIDPagesWithoutOverlapOrGaps pins the correctness half of
// issue #759's fix: chaining Query calls by feeding each page's oldest
// returned ID back in as the next call's BeforeID must visit every held
// event exactly once, with no page-boundary overlap (the old Until-based
// design needed a caller-side dedup map precisely because it could not
// make this guarantee -- BeforeID is an exclusive bound, so it doesn't
// need one).
func TestQueryBeforeIDPagesWithoutOverlapOrGaps(t *testing.T) {
	const n = 137 // deliberately not a multiple of the page size below
	const page = 20
	s := New(n, time.Hour)
	now := time.Now()
	for i := 0; i < n; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Millisecond), "core", ActionAccept))
	}

	seen := make(map[uint64]int)
	var beforeID uint64
	for {
		q := Query{Limit: page}
		if beforeID != 0 {
			q.BeforeID = beforeID
		}
		res := s.Query(q)
		if len(res.Events) == 0 {
			break
		}
		for _, e := range res.Events {
			seen[e.ID]++
		}
		if !res.HasMore {
			break
		}
		beforeID = res.Events[0].ID // oldest event in this page, per Query's oldest-first contract
	}

	if len(seen) != n {
		t.Fatalf("saw %d distinct IDs across the paged walk, want %d", len(seen), n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("ID %d seen %d times, want exactly once", id, count)
		}
	}
}

// TestQueryBeforeIDStaleCursorAfterEvictionReturnsEmpty pins what happens
// when a BeforeID cursor names an event that has since been evicted from
// the ring -- the case issue #759 asks to be handled deliberately, not by
// accident. Eviction only ever removes from the oldest end, so if the
// cursor's own event is gone, every event older than it (everything the
// next page would have asked for) is gone too: the correct answer is an
// empty page, not stale data and not a panic from an out-of-range ring
// computation. But an empty page is not the same fact as "there was
// nothing left to read" -- CursorEvicted must say which one this is, so
// a caller (MemoryCorpus.Replay) can tell a short result from a complete
// one instead of the two being silently indistinguishable.
func TestQueryBeforeIDStaleCursorAfterEvictionReturnsEmpty(t *testing.T) {
	const capacity = 10
	s := New(capacity, time.Hour)
	now := time.Now()
	for i := 0; i < capacity; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Millisecond), "core", ActionAccept))
	}
	// A cursor mid-buffer, as MemoryCorpus.Replay would be holding
	// between two pages of a slow, paused-and-resumed pass.
	staleCursor := uint64(5)

	// Evict everything currently held (including staleCursor's own event)
	// by wrapping the ring with a full capacity's worth of new inserts.
	for i := 0; i < capacity; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(capacity+i)*time.Millisecond), "core", ActionAccept))
	}

	res := s.Query(Query{BeforeID: staleCursor, Limit: 100})
	if len(res.Events) != 0 {
		t.Fatalf("got %d events for a cursor whose own event was evicted, want 0", len(res.Events))
	}
	if res.HasMore {
		t.Fatalf("HasMore = true for a fully-evicted cursor, want false -- nothing left for a caller to page through")
	}
	if !res.CursorEvicted {
		t.Fatalf("CursorEvicted = false for a cursor whose own event was evicted, want true -- an empty page here means data was lost to eviction, not that history genuinely ended")
	}
}

// TestQueryBeforeIDScanCostDoesNotGrowWithDepth pins corpus.go:154's own
// stated invariant ("at most one page's worth of scan time") at the level
// where issue #759's fix actually lives: a Query call resuming via
// BeforeID must examine roughly Limit ring entries regardless of how deep
// in history the cursor sits, not entries proportional to that depth --
// the cost the old Until-cursor design paid, since Until can only skip
// newer entries one at a time while still starting its walk from the
// newest held event every call.
//
// This asserts on a scan-entry *count*, via queryScanHook, rather than
// wall-clock timing: #501 and #744 already showed a timing-based bound
// flakes on a loaded CI runner even when the property it's checking
// genuinely holds, and the whole point of this test is to not be issue
// #744 the third time around.
func TestQueryBeforeIDScanCostDoesNotGrowWithDepth(t *testing.T) {
	const n = 20_000
	const limit = 50
	s := New(n, time.Hour)
	now := time.Now()
	for i := 0; i < n; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Millisecond), "core", ActionAccept))
	}

	scanCount := func(q Query) int {
		count := 0
		orig := queryScanHook
		queryScanHook = func() { count++ }
		defer func() { queryScanHook = orig }()
		s.Query(q)
		return count
	}

	// wantScanned is limit+1, not limit: Query peeks one entry past the
	// limit to know whether to report HasMore -- pre-existing behavior,
	// unrelated to BeforeID (the same peek happens on an unfiltered
	// Since/Until-only query). The property this test exists to pin is
	// that the peek count stays flat across cursor depths, not that it
	// equals limit exactly.
	const wantScanned = limit + 1
	cases := map[string]uint64{
		"shallow": uint64(n),     // cursor one step behind the newest event
		"mid":     uint64(n / 2), // cursor halfway through history
		"deep":    uint64(200),   // deep in history, but still far enough from the oldest held event that a full page is available
	}
	for name, beforeID := range cases {
		got := scanCount(Query{BeforeID: beforeID, Limit: limit})
		if got != wantScanned {
			t.Errorf("%s cursor (BeforeID=%d) scanned %d ring entries, want exactly %d -- scan cost must not grow with cursor depth", name, beforeID, got, wantScanned)
		}
	}
}
