// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Query used to materialise a whole Record -- Tuple plus the entire
// store.Event, Raw line included -- for every record matching the source,
// because Limit cannot be applied before the ordering is known and the
// ordering is by LastSeen, which is not final until the last line has
// been read. So asking for one record still built all of them: measured
// at 237 MiB of heap and 1.86s to return a single record from a 164 MiB
// log holding 20,000 records for one MAC.
//
// That matters because GET /api/watchlist/matches reaches this on the
// lowest-privilege credential mikroview issues -- a read-only API token
// -- with no rate limiter in front of it.
//
// The property under test is that what Query *retains* scales with Limit
// rather than with how much history the log holds. It is asserted as a
// ratio between two limits against the same log, not as an absolute
// figure, so it does not become a flaky assertion about one machine's
// allocator.
func TestQueryRetainedMemoryScalesWithLimitNotHistory(t *testing.T) {
	const records = 4000
	s := mustOpen(t, records+10)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	// One source, many records, each carrying a raw line big enough that
	// the payload dominates the ordering metadata -- which is the whole
	// distinction being drawn.
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	raw := strings.Repeat("x", 2048)
	for i := 0; i < records; i++ {
		tuple := Tuple{Source: src, DestIP: fmt.Sprintf("10.0.%d.%d", i/256, i%256), Port: 443}
		if err := s.Append(fmt.Sprintf("entry-%d", i), tuple, testEvent(raw), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	measure := func(limit int) uint64 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)

		var held []Record
		if err := s.Query(context.Background(), Query{Source: src, Limit: limit}, func(r Record) bool {
			held = append(held, r)
			return true
		}); err != nil {
			t.Fatalf("Query(limit=%d): %v", limit, err)
		}
		if len(held) != limit {
			t.Fatalf("Query(limit=%d) returned %d records", limit, len(held))
		}

		// Collect first, then measure. Without this GC the delta counts
		// the query's transient garbage as well as what it retained,
		// and a two-pass query over 4000 records generates a lot of
		// churn -- so the figure tracked GC pacing rather than Limit,
		// which is the opposite of what this test claims to measure.
		//
		// That made it an assertion about the allocator: it failed
		// under Go 1.27's size-specialised malloc, and fails on any
		// toolchain under GOGC=off, both times while the property under
		// test still held perfectly. held is kept alive across the GC
		// by the KeepAlive below, so what survives here is retention.
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(held)
		if after.HeapAlloc < before.HeapAlloc {
			return 0
		}
		return after.HeapAlloc - before.HeapAlloc
	}

	small := measure(1)
	large := measure(records)
	t.Logf("heap delta: limit=1 %d bytes, limit=%d %d bytes", small, records, large)

	// Before the fix both limits built every record, so the two figures
	// were within noise of each other. The payload is ~2 KiB per record
	// against tens of bytes of ordering metadata, so a Limit of 1 must
	// now cost dramatically less than a Limit of everything. A factor of
	// four is far below the ~2000x the payload ratio implies, and is
	// chosen to leave room for allocator noise rather than to be tight.
	if small*4 > large {
		t.Errorf("Limit=1 retained %d bytes against %d for Limit=%d -- retention still scales with history, not with Limit",
			small, large, records)
	}
}

// The two-pass split must not change what Query returns. Ordering is by
// LastSeen descending, and Count/LastSeen come from the update lines the
// first pass walked, not from the record line the second pass re-read --
// so a record whose LastSeen moved must still sort and report correctly.
func TestQueryTwoPassPreservesOrderingAndCounts(t *testing.T) {
	s := mustOpen(t, 10)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	older := Tuple{Source: src, DestIP: "10.0.0.1", Port: 443}
	newer := Tuple{Source: src, DestIP: "10.0.0.2", Port: 443}
	if err := s.Append("entry-older", older, testEvent("older"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("entry-newer", newer, testEvent("newer"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The older record recurs, moving its LastSeen past the newer one --
	// exactly the case a single pass could not filter early on.
	for i := 0; i < 3; i++ {
		if err := s.Append("entry-older", older, testEvent("older"), now.Add(time.Duration(2+i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	got := collect(t, s, Query{Source: src})
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].Tuple.DestIP != "10.0.0.1" {
		t.Errorf("first record is %s, want 10.0.0.1 -- the recurring record has the most recent LastSeen", got[0].Tuple.DestIP)
	}
	if got[0].Count != 4 {
		t.Errorf("Count = %d, want 4 -- the update lines from the first pass must survive into the returned record", got[0].Count)
	}
	if got[0].Event.Raw != "older" {
		t.Errorf("Event.Raw = %q, want the payload re-read in the second pass", got[0].Event.Raw)
	}

	// Limit must take the most recent, not whichever the map happened to
	// iterate first.
	one := collect(t, s, Query{Source: src, Limit: 1})
	if len(one) != 1 || one[0].Tuple.DestIP != "10.0.0.1" {
		t.Errorf("Limit=1 returned %+v, want the most-recently-seen record", one)
	}
}
