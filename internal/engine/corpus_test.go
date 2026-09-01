// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func corpusEvent(t time.Time, srcIP string) store.Event {
	return store.Event{Time: t, ReceivedAt: t, SrcIP: srcIP, DstIP: "10.0.0.1", DstPort: 22, Action: store.ActionDrop}
}

func TestMemoryCorpusReplayEmptyStore(t *testing.T) {
	s := store.New(100, time.Hour)
	c := NewMemoryCorpus(s)

	visited := 0
	w := c.Replay(func(store.Event) { visited++ })

	if visited != 0 {
		t.Fatalf("visited = %d, want 0", visited)
	}
	if w.Count != 0 || !w.Start.IsZero() || !w.End.IsZero() || w.Truncated {
		t.Fatalf("unexpected CorpusWindow for empty store: %+v", w)
	}
}

func TestMemoryCorpusReplayVisitsEveryEventInChronologicalOrder(t *testing.T) {
	s := store.New(1000, 24*time.Hour)
	base := time.Now().Add(-time.Hour)
	const n = 50
	for i := 0; i < n; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Second), fmt.Sprintf("10.1.0.%d", i)))
	}

	c := NewMemoryCorpus(s)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if len(got) != n {
		t.Fatalf("visited %d events, want %d", len(got), n)
	}
	if w.Count != n {
		t.Fatalf("CorpusWindow.Count = %d, want %d", w.Count, n)
	}
	if w.Truncated {
		t.Fatalf("CorpusWindow.Truncated = true, want false")
	}
	for i := 1; i < len(got); i++ {
		if got[i].ReceivedAt.Before(got[i-1].ReceivedAt) {
			t.Fatalf("events not in chronological order at index %d: %s before %s", i, got[i].ReceivedAt, got[i-1].ReceivedAt)
		}
	}
	if !w.Start.Equal(got[0].ReceivedAt) || !w.End.Equal(got[len(got)-1].ReceivedAt) {
		t.Fatalf("CorpusWindow start/end (%s, %s) do not match visited events' actual span (%s, %s)",
			w.Start, w.End, got[0].ReceivedAt, got[len(got)-1].ReceivedAt)
	}
}

// TestMemoryCorpusReplayPaginatesAcrossMultiplePages shrinks
// corpusPageSize far below the event count, forcing Replay to walk
// several pages via its BeforeID cursor -- pinning that pages don't
// overlap or drop events (each ID visited exactly once) and that
// reassembly into chronological order works, not just the trivial
// single-page case.
func TestMemoryCorpusReplayPaginatesAcrossMultiplePages(t *testing.T) {
	origPageSize := corpusPageSize
	corpusPageSize = 7
	t.Cleanup(func() { corpusPageSize = origPageSize })

	s := store.New(1000, 24*time.Hour)
	base := time.Now().Add(-time.Hour)
	const n = 103 // deliberately not a multiple of the shrunk page size
	for i := 0; i < n; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Second), fmt.Sprintf("10.2.0.%d", i)))
	}

	c := NewMemoryCorpus(s)
	seen := make(map[uint64]int)
	var got []store.Event
	w := c.Replay(func(e store.Event) {
		seen[e.ID]++
		got = append(got, e)
	})

	if w.Count != n {
		t.Fatalf("CorpusWindow.Count = %d, want %d", w.Count, n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("event id %d visited %d times, want exactly once", id, count)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].ID <= got[i-1].ID {
			t.Fatalf("events not strictly increasing by ID at index %d: %d then %d", i, got[i-1].ID, got[i].ID)
		}
	}
}

// TestMemoryCorpusReplayTruncatesAtMaxCorpusEvents shrinks
// maxCorpusEvents and pins that Replay (a) stops rather than walking
// the whole ring, (b) reports Truncated, and (c) keeps the most recent
// events (the tail of history) rather than the oldest -- the useful
// direction to truncate for a replay that cares about recent behaviour.
func TestMemoryCorpusReplayTruncatesAtMaxCorpusEvents(t *testing.T) {
	origMax := maxCorpusEvents
	origPageSize := corpusPageSize
	maxCorpusEvents = 10
	corpusPageSize = 4
	t.Cleanup(func() {
		maxCorpusEvents = origMax
		corpusPageSize = origPageSize
	})

	s := store.New(1000, 24*time.Hour)
	base := time.Now().Add(-time.Hour)
	const n = 37
	for i := 0; i < n; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Second), fmt.Sprintf("10.3.0.%d", i)))
	}

	c := NewMemoryCorpus(s)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if !w.Truncated {
		t.Fatalf("CorpusWindow.Truncated = false, want true (n=%d > maxCorpusEvents=%d)", n, maxCorpusEvents)
	}
	if w.Count != maxCorpusEvents || len(got) != maxCorpusEvents {
		t.Fatalf("Count/visited = %d/%d, want exactly maxCorpusEvents=%d", w.Count, len(got), maxCorpusEvents)
	}
	// The kept events must be the most recent maxCorpusEvents (IDs
	// n-maxCorpusEvents+1 .. n), not the oldest.
	wantFirstID := uint64(n - maxCorpusEvents + 1)
	if got[0].ID != wantFirstID {
		t.Fatalf("oldest kept event ID = %d, want %d (the most recent %d events, not the oldest)", got[0].ID, wantFirstID, maxCorpusEvents)
	}
	if got[len(got)-1].ID != uint64(n) {
		t.Fatalf("newest kept event ID = %d, want %d", got[len(got)-1].ID, n)
	}
}

// TestMemoryCorpusReplayReportsTruncatedWhenCursorIsEvicted pins the
// eviction-during-replay case issue #759 named but didn't originally
// surface to a caller: BeforeID pages don't overlap or drop events among
// what the ring still holds when each page runs, but that guarantee says
// nothing about an event the ring evicts *between* two pages, under fast
// ingest, while Replay isn't holding the lock. When that happens, the
// events the lost page would have returned are gone for good, and Replay
// must report that via CorpusWindow.Truncated rather than stopping
// silently the same way it would at a genuine, uncontended end of
// history -- collapsing the two would mean a caller can never tell "I
// read everything currently retained" from "the ring evicted the rest
// while I was reading it."
//
// afterReplayPageForTest deterministically forces the eviction to land
// between exactly the two pages this test cares about, rather than
// relying on a timing race between concurrent goroutines -- the same
// reason internal/store's queryScanHook pins a scan count instead of a
// wall-clock duration: see that field's own doc comment for why a timing
// race is the wrong tool here (#501, #744).
func TestMemoryCorpusReplayReportsTruncatedWhenCursorIsEvicted(t *testing.T) {
	origPageSize := corpusPageSize
	corpusPageSize = 2
	t.Cleanup(func() { corpusPageSize = origPageSize })

	const capacity = 6
	s := store.New(capacity, time.Hour)
	base := time.Now().Add(-time.Minute)
	for i := 0; i < capacity; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Second), fmt.Sprintf("10.4.0.%d", i)))
	}

	var evicted bool
	afterReplayPageForTest = func() {
		if evicted {
			return
		}
		evicted = true
		// Wrap the ring with a full capacity's worth of new events,
		// evicting everything the first page read -- including the
		// event it handed the next page as a resume cursor -- before
		// that next page runs.
		for i := 0; i < capacity; i++ {
			s.Insert(corpusEvent(base.Add(time.Duration(capacity+i)*time.Second), fmt.Sprintf("10.4.1.%d", i)))
		}
	}
	t.Cleanup(func() { afterReplayPageForTest = nil })

	c := NewMemoryCorpus(s)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if !w.Truncated {
		t.Fatalf("CorpusWindow.Truncated = false, want true -- the second page's events were evicted before Replay could read them")
	}
	if w.Count != corpusPageSize || len(got) != corpusPageSize {
		t.Fatalf("Count/visited = %d/%d, want exactly %d (only the first page's events -- the second page's cursor was evicted)", w.Count, len(got), corpusPageSize)
	}
}
