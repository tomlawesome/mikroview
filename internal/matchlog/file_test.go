// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func testEvent(raw string) store.Event {
	return store.Event{Action: store.ActionAccept, Raw: raw}
}

func mustOpen(t *testing.T, capacity int) *FileStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s, err := Open(path, capacity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func collect(t *testing.T, s *FileStore, q Query) []Record {
	t.Helper()
	var out []Record
	if err := s.Query(q, func(r Record) bool {
		out = append(out, r)
		return true
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	return out
}

func TestOpenRejectsNonPositiveCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	for _, c := range []int{0, -1, -100} {
		if _, err := Open(path, c); err == nil {
			t.Errorf("Open(capacity=%d) should have failed, did not", c)
		}
	}
}

func TestAppendAndQueryRoundTrip(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883}
	if err := s.Append("entry-1", tuple, testEvent("first"), now); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := collect(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	r := got[0]
	if r.EntryID != "entry-1" || r.Tuple.DestIP != "10.0.0.5" || r.Tuple.Port != 8883 {
		t.Errorf("unexpected record: %+v", r)
	}
	if r.Event.Raw != "first" {
		t.Errorf("full event evidence not preserved: got Raw=%q", r.Event.Raw)
	}
	if r.Count != 1 {
		t.Errorf("Count = %d, want 1 for a single occurrence", r.Count)
	}
	if !r.FirstSeen.Equal(now) || !r.LastSeen.Equal(now) {
		t.Errorf("FirstSeen/LastSeen = %v/%v, want both %v", r.FirstSeen, r.LastSeen, now)
	}
}

// The core of #243 section 4: a repeated identical tuple must not become
// a second stored record.
func TestRepeatedTupleCollapses(t *testing.T) {
	s := mustOpen(t, 10)
	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if err := s.Append("entry-1", tuple, testEvent("x"), t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	if st := s.Stats(); st.Count != 1 {
		t.Fatalf("Stats().Count = %d, want 1 -- five identical matches must collapse to one record", st.Count)
	}

	got := collect(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0].Count != 5 {
		t.Errorf("Count = %d, want 5", got[0].Count)
	}
	if !got[0].FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen = %v, want the first occurrence %v", got[0].FirstSeen, t0)
	}
	wantLast := t0.Add(4 * time.Minute)
	if !got[0].LastSeen.Equal(wantLast) {
		t.Errorf("LastSeen = %v, want the most recent occurrence %v", got[0].LastSeen, wantLast)
	}
}

// A new destination or port is a distinct fact -- section 4 is explicit
// that collapsing must never mask a genuinely novel tuple inside a noisy
// entry.
func TestDistinctTuplesDoNotCollapse(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Now()
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	cases := []Tuple{
		{Source: src, DestIP: "10.0.0.5", Port: 8883},
		{Source: src, DestIP: "10.0.0.6", Port: 8883}, // different dest
		{Source: src, DestIP: "10.0.0.5", Port: 443},  // different port
	}
	for _, tup := range cases {
		if err := s.Append("entry-1", tup, testEvent("x"), now); err != nil {
			t.Fatalf("Append(%+v): %v", tup, err)
		}
	}

	if st := s.Stats(); st.Count != 3 {
		t.Fatalf("Stats().Count = %d, want 3 distinct records", st.Count)
	}
}

// Two entries watching the exact same tuple must not collapse into each
// other's history -- an entry's matches are its own.
func TestSameTupleDifferentEntriesDoNotCollapse(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Now()
	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 443}

	if err := s.Append("entry-1", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("entry-2", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Count != 2 {
		t.Fatalf("Stats().Count = %d, want 2 -- different entries must not share a record", st.Count)
	}
}

func TestAppendRefusesEmptyIdentity(t *testing.T) {
	s := mustOpen(t, 10)
	err := s.Append("entry-1", Tuple{DestIP: "10.0.0.5", Port: 443}, testEvent("x"), time.Now())
	if !errors.Is(err, ErrEmptyIdentity) {
		t.Errorf("Append with no MAC/IP = %v, want ErrEmptyIdentity", err)
	}
}

func TestQueryRefusesEmptyIdentity(t *testing.T) {
	s := mustOpen(t, 10)
	err := s.Query(Query{}, func(Record) bool { return true })
	if !errors.Is(err, ErrEmptyIdentity) {
		t.Errorf("Query with no MAC/IP = %v, want ErrEmptyIdentity", err)
	}
}

// The whole point of ErrCapacityReached: the file backend refuses to
// grow past its limit rather than silently overwriting like the ring
// does (#243 section 3) -- a rare, high-value match must never be
// silently discarded.
func TestCapacityIsRefusedNotOverwritten(t *testing.T) {
	s := mustOpen(t, 2)
	now := time.Now()
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	if err := s.Append("e", Tuple{Source: src, DestIP: "1", Port: 1}, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", Tuple{Source: src, DestIP: "2", Port: 2}, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	err := s.Append("e", Tuple{Source: src, DestIP: "3", Port: 3}, testEvent("x"), now)
	if !errors.Is(err, ErrCapacityReached) {
		t.Fatalf("a third distinct tuple at capacity 2 = %v, want ErrCapacityReached", err)
	}
	if st := s.Stats(); st.Count != 2 || !st.Full {
		t.Errorf("Stats() = %+v, want Count=2 Full=true", st)
	}

	// The refused record must not have been written at all -- confirm by
	// querying for it and finding nothing.
	got := collect(t, s, Query{Source: src})
	for _, r := range got {
		if r.Tuple.DestIP == "3" {
			t.Errorf("the refused tuple was written anyway: %+v", r)
		}
	}
}

// A repeat of an already-open tuple must keep working even once the
// store is otherwise full -- collapsing costs no new capacity, and an
// entry's ongoing activity should not stop being tracked just because
// the log is full elsewhere.
func TestCollapsingStillWorksAtCapacity(t *testing.T) {
	s := mustOpen(t, 1)
	now := time.Now()
	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "1", Port: 1}

	if err := s.Append("e", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); !st.Full {
		t.Fatal("expected the store to be full at capacity 1")
	}
	if err := s.Append("e", tuple, testEvent("x"), now.Add(time.Minute)); err != nil {
		t.Fatalf("a repeat of the already-open tuple should still succeed at capacity: %v", err)
	}

	got := collect(t, s, Query{Source: tuple.Source})
	if len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("got %+v, want one record with Count=2", got)
	}
}

// MAC-preferred matching: an entry/query keyed on MAC must not be fooled
// by a device's IP changing, and must not match a different device that
// happens to reuse that IP later (#243 section 1).
func TestMatchingPrefersMACOverIP(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Now()

	// Same MAC, IP changes between two matches (a DHCP renewal).
	tuple1 := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.10"}, DestIP: "1.2.3.4", Port: 443}
	tuple2 := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.99"}, DestIP: "1.2.3.4", Port: 443}
	if err := s.Append("e", tuple1, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", tuple2, testEvent("x"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if st := s.Stats(); st.Count != 1 {
		t.Fatalf("Stats().Count = %d, want 1 -- same MAC across an IP change must collapse", st.Count)
	}

	// Querying by the OLD IP must not find it (MAC is the real identity);
	// querying by MAC must.
	byOldIP := collect(t, s, Query{Source: Identity{IP: "192.168.1.10"}})
	if len(byOldIP) != 0 {
		t.Errorf("querying by the stale IP found %d records, want 0 -- MAC identity should not be reachable by an old IP", len(byOldIP))
	}
	byMAC := collect(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
	if len(byMAC) != 1 || byMAC[0].Count != 2 {
		t.Fatalf("querying by MAC got %+v, want one record with Count=2", byMAC)
	}
}

// A device with no known MAC (e.g. traffic on a chain without src-mac)
// falls back to IP identity rather than being dropped entirely.
func TestIPFallbackWhenNoMACKnown(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Now()
	tuple := Tuple{Source: Identity{IP: "192.168.1.10"}, DestIP: "1.2.3.4", Port: 443}

	if err := s.Append("e", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	got := collect(t, s, Query{Source: Identity{IP: "192.168.1.10"}})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1 via IP fallback", len(got))
	}
}

func TestQueryFiltersByTimeWindow(t *testing.T) {
	s := mustOpen(t, 10)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	if err := s.Append("e", Tuple{Source: src, DestIP: "1", Port: 1}, testEvent("x"), base); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", Tuple{Source: src, DestIP: "2", Port: 2}, testEvent("x"), base.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Window covering only the first record.
	got := collect(t, s, Query{Source: src, Since: base, Until: base.Add(time.Hour)})
	if len(got) != 1 || got[0].Tuple.DestIP != "1" {
		t.Fatalf("windowed query got %+v, want only the first record", got)
	}

	// No Until: everything from Since onward.
	got = collect(t, s, Query{Source: src, Since: base})
	if len(got) != 2 {
		t.Fatalf("unbounded-Until query got %d records, want 2", len(got))
	}
}

// A record that started before the window but is still recurring inside
// it must be returned -- it is exactly the ongoing activity a caller
// asking "what happened in this window" needs, not just what started in
// it (see Query's doc comment on overlap).
func TestQueryIncludesRecordsSpanningIntoTheWindow(t *testing.T) {
	s := mustOpen(t, 10)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	tuple := Tuple{Source: src, DestIP: "1", Port: 1}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	if err := s.Append("e", tuple, testEvent("x"), base); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", tuple, testEvent("x"), base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := collect(t, s, Query{Source: src, Since: base.Add(time.Hour), Until: base.Add(3 * time.Hour)})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1 (the record spans into the window even though it started before Since)", len(got))
	}
}

func TestQueryRespectsLimitAndOrdersMostRecentFirst(t *testing.T) {
	s := mustOpen(t, 20)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: i + 1}
		if err := s.Append("e", tuple, testEvent("x"), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	got := collect(t, s, Query{Source: src, Limit: 3})
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3 (Limit)", len(got))
	}
	// Most recent first: port 10, then 9, then 8.
	for i, want := range []int{10, 9, 8} {
		if got[i].Tuple.Port != want {
			t.Errorf("result[%d].Tuple.Port = %d, want %d", i, got[i].Tuple.Port, want)
		}
	}
}

func TestQueryYieldFalseStopsDelivery(t *testing.T) {
	s := mustOpen(t, 20)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	now := time.Now()
	for i := 0; i < 5; i++ {
		tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: i + 1}
		if err := s.Append("e", tuple, testEvent("x"), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	n := 0
	if err := s.Query(Query{Source: src}, func(Record) bool {
		n++
		return n < 2 // stop after the second delivery
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("delivered %d records, want exactly 2 (yield returned false after the second)", n)
	}
}

// Everything must be intact after a restart: a fresh Open on the same
// path must rebuild its index correctly enough that further collapsing
// and capacity refusal both keep working exactly as before the restart.
func TestSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	tuple := Tuple{Source: src, DestIP: "10.0.0.5", Port: 8883}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	s1, err := Open(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Append("e", tuple, testEvent("first"), t0); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path, 2)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	defer s2.Close()

	got := collect(t, s2, Query{Source: src})
	if len(got) != 1 || got[0].Event.Raw != "first" {
		t.Fatalf("after restart, got %+v, want the original record with full evidence intact", got)
	}

	// The index must have been rebuilt: a repeat of the same tuple must
	// still collapse, not become a second record.
	if err := s2.Append("e", tuple, testEvent("second"), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st := s2.Stats(); st.Count != 1 {
		t.Fatalf("Stats().Count after a post-restart repeat = %d, want 1 (still collapsing)", st.Count)
	}

	// And capacity must still be enforced against the rebuilt count, not
	// reset to zero.
	other := Tuple{Source: src, DestIP: "10.0.0.6", Port: 443}
	if err := s2.Append("e", other, testEvent("x"), t0); err != nil {
		t.Fatal(err) // this is the 2nd distinct record, capacity is 2, should succeed
	}
	third := Tuple{Source: src, DestIP: "10.0.0.7", Port: 443}
	if err := s2.Append("e", third, testEvent("x"), t0); !errors.Is(err, ErrCapacityReached) {
		t.Errorf("a 3rd distinct tuple after restart at capacity 2 = %v, want ErrCapacityReached", err)
	}
}

// Append and Query use separate file handles specifically so a slow
// query never blocks a writer -- this exercises that concurrently rather
// than only reasoning about it: many goroutines appending distinct
// tuples while others query, under -race, so a real data race (not just
// a sequential test of the same code) would actually be scheduled to
// trigger the detector.
func TestConcurrentAppendAndQuery(t *testing.T) {
	const writers = 8
	const perWriter = 20
	s := mustOpen(t, writers*perWriter)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	// The query goroutine's stop/done signal is deliberately independent
	// of writersWG: it must keep querying *while* writers are still
	// running, then be told to stop only afterwards -- folding it into
	// the same WaitGroup writersWG.Wait() blocks on would deadlock,
	// since it would never see the stop signal until after the wait it
	// is itself part of returns.
	var writersWG sync.WaitGroup
	writersWG.Add(writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer writersWG.Done()
			for i := 0; i < perWriter; i++ {
				tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: w*perWriter + i}
				if err := s.Append("e", tuple, testEvent("x"), time.Now()); err != nil {
					t.Errorf("concurrent Append: %v", err)
				}
			}
		}(w)
	}

	stop := make(chan struct{})
	queryDone := make(chan struct{})
	go func() {
		defer close(queryDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Query(Query{Source: src}, func(Record) bool { return true })
				_ = s.Stats()
			}
		}
	}()

	writersWG.Wait()
	close(stop)
	<-queryDone

	if st := s.Stats(); st.Count != writers*perWriter {
		t.Errorf("Stats().Count = %d, want %d after all concurrent appends completed", st.Count, writers*perWriter)
	}
}

// A torn line (as an unclean shutdown could leave) must not break
// replay or querying -- it is skipped, not fatal, like every other
// optional store in this codebase.
func TestTornLineIsSkippedNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	s, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", Tuple{Source: src, DestIP: "1", Port: 1}, testEvent("good"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a torn write: append a truncated JSON fragment with no
	// trailing newline, as a crash mid-write could leave.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"kind":"record","id":"deadbeef`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s2, err := Open(path, 10)
	if err != nil {
		t.Fatalf("Open with a torn trailing line should not fail: %v", err)
	}
	defer s2.Close()

	got := collect(t, s2, Query{Source: src})
	if len(got) != 1 || got[0].Event.Raw != "good" {
		t.Fatalf("got %+v, want the one good record recovered despite the torn line", got)
	}
}
