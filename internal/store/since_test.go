// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// insertN fills s with n events one millisecond apart, returning the time
// of the first one so a caller can reason about ReceivedAt if it needs to.
func insertN(s *Store, n int) time.Time {
	now := time.Now()
	for i := 0; i < n; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Millisecond), "core", ActionAccept))
	}
	return now
}

func sameIDs(got []Event, want ...uint64) bool {
	g := ids(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// TestSinceReturnsEverythingAfterTheCursorInOrder is the contract the
// engine's cursor rests on: ascending ID, no gaps, no repeats. Ascending
// matters as much as completeness -- detectors are stateful over a
// sequence, so replaying a burst backwards is not the same evidence.
func TestSinceReturnsEverythingAfterTheCursorInOrder(t *testing.T) {
	s := New(100, time.Hour)
	insertN(s, 10)

	events, oldest, newest := s.Since(0, 100)
	if !sameIDs(events, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10) {
		t.Fatalf("Since(0, 100) returned IDs %v, want 1..10 ascending", ids(events))
	}
	if oldest != 1 || newest != 10 {
		t.Errorf("held range = (%d, %d), want (1, 10)", oldest, newest)
	}

	events, _, _ = s.Since(7, 100)
	if !sameIDs(events, 8, 9, 10) {
		t.Errorf("Since(7, 100) returned IDs %v, want 8, 9, 10", ids(events))
	}
}

// TestSinceStopsAtMax pins the batch bound -- Run reads in batches
// precisely so one call cannot hold the read lock (and therefore the
// ingest writer) for an unbounded copy.
func TestSinceStopsAtMax(t *testing.T) {
	s := New(100, time.Hour)
	insertN(s, 50)

	events, _, newest := s.Since(0, 8)
	if !sameIDs(events, 1, 2, 3, 4, 5, 6, 7, 8) {
		t.Fatalf("Since(0, 8) returned IDs %v, want the first eight", ids(events))
	}
	// A truncated batch still reports the full held range: that is how the
	// caller knows to come back for more rather than reading a short batch
	// as "caught up".
	if newest != 50 {
		t.Errorf("newestHeld = %d, want 50 even though the batch stopped at 8", newest)
	}
}

// TestSinceOnAnEmptyStoreReportsTheNextIDAsOldestHeld covers the case a
// cursor reader meets on every quiet second: nothing to do, and no reason
// to think anything was missed. oldestHeld = nextID + 1 is what keeps the
// caller's "did eviction pass me" test (oldestHeld > cursor + 1) false
// here instead of needing an emptiness special case.
func TestSinceOnAnEmptyStoreReportsTheNextIDAsOldestHeld(t *testing.T) {
	s := New(100, time.Hour)

	events, oldest, newest := s.Since(0, 100)
	if len(events) != 0 {
		t.Fatalf("Since on an empty store returned %d event(s), want none", len(events))
	}
	if oldest != 1 || newest != 0 {
		t.Errorf("held range = (%d, %d), want (1, 0) -- oldest held is the next arrival", oldest, newest)
	}

	insertN(s, 3)
	events, oldest, newest = s.Since(3, 100)
	if len(events) != 0 {
		t.Fatalf("Since(3) with three events held returned %d event(s), want none", len(events))
	}
	if oldest != 1 || newest != 3 {
		t.Errorf("held range = (%d, %d), want (1, 3)", oldest, newest)
	}
}

// TestSinceReportsTheGapWhenTheCursorHasBeenEvicted is the loss signal
// itself: the ring wrapped past events the reader never reached, and the
// only honest thing left to say is which ones are gone.
func TestSinceReportsTheGapWhenTheCursorHasBeenEvicted(t *testing.T) {
	s := New(10, time.Hour)
	insertN(s, 30) // IDs 21..30 survive; 1..20 have been overwritten

	events, oldest, newest := s.Since(5, 100)
	if oldest != 21 || newest != 30 {
		t.Fatalf("held range = (%d, %d), want (21, 30)", oldest, newest)
	}
	if !sameIDs(events, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30) {
		t.Errorf("Since(5, 100) returned IDs %v, want the surviving 21..30", ids(events))
	}
	// 6..20 are not reported as skipped by Since itself -- it returns what
	// is held and the range it came from, and leaves counting the gap to
	// the caller, which is the only side that knows where its cursor was.
}

// TestSinceAtTheNewestHeldEventReturnsNothing is the caught-up steady
// state, which must not be confused with the evicted case above.
func TestSinceAtTheNewestHeldEventReturnsNothing(t *testing.T) {
	s := New(10, time.Hour)
	insertN(s, 10)

	events, oldest, newest := s.Since(10, 100)
	if len(events) != 0 {
		t.Fatalf("Since(10) at the newest held ID returned %d event(s), want none", len(events))
	}
	if oldest != 1 || newest != 10 {
		t.Errorf("held range = (%d, %d), want (1, 10)", oldest, newest)
	}
}

// TestSinceAfterResetReportsTheWholeBufferGone -- Reset empties the ring
// without rewinding nextID (see its doc comment), so a reader holding a
// cursor from before it must be able to see that everything between the
// cursor and the next arrival is unreachable.
func TestSinceAfterResetReportsTheWholeBufferGone(t *testing.T) {
	s := New(100, time.Hour)
	insertN(s, 40)
	s.Reset()

	events, oldest, newest := s.Since(12, 100)
	if len(events) != 0 {
		t.Fatalf("Since after Reset returned %d event(s), want none", len(events))
	}
	if oldest != 41 || newest != 40 {
		t.Fatalf("held range = (%d, %d), want (41, 40) -- nothing held, next arrival is 41", oldest, newest)
	}

	insertN(s, 2) // IDs 41, 42
	events, oldest, newest = s.Since(12, 100)
	if !sameIDs(events, 41, 42) {
		t.Errorf("Since(12) after Reset returned IDs %v, want the post-reset 41, 42", ids(events))
	}
	if oldest != 41 || newest != 42 {
		t.Errorf("held range = (%d, %d), want (41, 42)", oldest, newest)
	}
}

// TestSinceAfterShrinkingResizeReportsTheEvictedGap -- a resize down
// evicts oldest-first exactly as an ordinary wrap does, so it must read
// the same way to a cursor holder.
func TestSinceAfterShrinkingResizeReportsTheEvictedGap(t *testing.T) {
	s := New(100, time.Hour)
	insertN(s, 60)

	if kept, evicted := s.Resize(10); kept != 10 || evicted != 50 {
		t.Fatalf("Resize(10) kept %d evicted %d, want 10 and 50", kept, evicted)
	}

	events, oldest, newest := s.Since(3, 100)
	if oldest != 51 || newest != 60 {
		t.Fatalf("held range = (%d, %d), want (51, 60)", oldest, newest)
	}
	if !sameIDs(events, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60) {
		t.Errorf("Since(3, 100) after the shrink returned IDs %v, want 51..60", ids(events))
	}

	// Growing keeps everything, so the same cursor sees no new gap.
	s.Resize(200)
	insertN(s, 1) // ID 61
	events, oldest, _ = s.Since(60, 100)
	if !sameIDs(events, 61) || oldest != 51 {
		t.Errorf("after growing: Since(60) = %v with oldestHeld %d, want [61] and 51", ids(events), oldest)
	}
}

// TestSinceReadsAcrossTheRingWrap catches the off-by-capacity the O(1)
// start position could hide: with the newest events split either side of
// the buffer's end, the walk has to come back round to slot zero.
func TestSinceReadsAcrossTheRingWrap(t *testing.T) {
	s := New(8, time.Hour)
	insertN(s, 13) // head has wrapped; IDs 6..13 survive, starting mid-buffer

	events, _, _ := s.Since(8, 100)
	if !sameIDs(events, 9, 10, 11, 12, 13) {
		t.Fatalf("Since(8) across the wrap returned IDs %v, want 9..13", ids(events))
	}
	for i, e := range events {
		if e.DeviceID != "core" {
			t.Fatalf("event %d came back as %+v, want a real stored event", i, e)
		}
	}
}

// TestSinceRejectsANonPositiveMax -- a caller asking for no events gets
// none rather than the whole buffer, and still gets the held range, which
// is what makes Since(cursor, 1)-style peeking safe to write.
func TestSinceRejectsANonPositiveMax(t *testing.T) {
	s := New(100, time.Hour)
	insertN(s, 5)

	events, oldest, newest := s.Since(0, 0)
	if len(events) != 0 {
		t.Fatalf("Since with max 0 returned %d event(s), want none", len(events))
	}
	if oldest != 1 || newest != 5 {
		t.Errorf("held range = (%d, %d), want (1, 5)", oldest, newest)
	}
}
