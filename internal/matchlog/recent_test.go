// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Covers the all-entries, recent-first query added by #586 -- RecentQuery
// and both backends' Recent. The Postgres half lives here rather than in
// postgres_test.go so the two implementations of one contract are read
// side by side: the properties asserted are identical, and the point of
// the mode is that a caller cannot tell which backend answered.

func collectRecent(t *testing.T, s *FileStore, q RecentQuery) []Record {
	t.Helper()
	var out []Record
	if err := s.Recent(context.Background(), q, func(r Record) bool {
		out = append(out, r)
		return true
	}); err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return out
}

func collectRecentPG(t *testing.T, s *PostgresStore, q RecentQuery) []Record {
	t.Helper()
	var out []Record
	if err := s.Recent(context.Background(), q, func(r Record) bool {
		out = append(out, r)
		return true
	}); err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return out
}

// recentBase is the timestamp every fixture below counts minutes from.
var recentBase = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

// seeded is one Append a fixture makes: entry, source and destination
// port, at recentBase + minute.
type seeded struct {
	entryID string
	source  Identity
	port    int
	minute  int
}

// appendAll writes a fixture through the Store interface, so the same
// fixture drives either backend.
func appendAll(t *testing.T, s Store, rows []seeded) {
	t.Helper()
	for _, r := range rows {
		tuple := Tuple{Source: r.source, DestIP: "10.0.0.9", Port: r.port}
		if err := s.Append(r.entryID, tuple, testEvent("x"), recentBase.Add(time.Duration(r.minute)*time.Minute)); err != nil {
			t.Fatalf("Append(%s, port %d): %v", r.entryID, r.port, err)
		}
	}
}

// portsOf renders a result set as the ports it delivered, in order --
// the whole assertion for an ordering test in one comparable value.
func portsOf(recs []Record) []int {
	out := make([]int, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Tuple.Port)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// threeEntries is the shared fixture: three watchlist entries, three
// different device identities, one match each, a minute apart. The third
// entry's device is known only by IP -- the shape a non-inverted entry
// with an empty Source records under, and the evidence the per-identity
// query cannot reach without already knowing that IP (#586).
var threeEntries = []seeded{
	{"entry-a", Identity{MAC: "aa:bb:cc:dd:ee:01"}, 1, 0},
	{"entry-b", Identity{MAC: "aa:bb:cc:dd:ee:02"}, 2, 1},
	{"entry-c", Identity{IP: "192.0.2.77"}, 3, 2},
}

// recentCase is one row of the shared table both backends run. Case
// names carry only letters, digits and spaces on purpose: the Postgres
// half derives a schema identifier from t.Name(), and newTestPool fails
// the test outright -- before it can skip for a missing DSN -- on
// anything else.
type recentCase struct {
	name  string
	seed  []seeded
	query RecentQuery
	// wantPorts is the exact result, in the exact order expected. nil
	// means "expect no records at all".
	wantPorts []int
	why       string
}

var recentCases = []recentCase{
	{
		name:      "every entry newest first",
		seed:      threeEntries,
		query:     RecentQuery{},
		wantPorts: []int{3, 2, 1},
		why:       "the happy path: one merged list across all three entries, ordered by LastSeen descending",
	},
	{
		name:      "reaches a match no identity query would find",
		seed:      threeEntries,
		query:     RecentQuery{Limit: 1},
		wantPorts: []int{3},
		why: "entry-c's device is IP-only, the shape an entry with an empty Source records under -- " +
			"retrievable here without anyone knowing to ask for 192.0.2.77",
	},
	{
		name:      "the limit actually bounds",
		seed:      threeEntries,
		query:     RecentQuery{Limit: 2},
		wantPorts: []int{3, 2},
		why:       "a limit below the number of records available truncates, keeping the most recent",
	},
	{
		name:      "empty result rather than an error",
		seed:      nil,
		query:     RecentQuery{},
		wantPorts: nil,
		why:       "an empty log answers with no records rather than refusing, unlike the per-identity path's empty identity",
	},
	{
		name:      "empty result from a window nothing falls in",
		seed:      threeEntries,
		query:     RecentQuery{Since: recentBase.Add(time.Hour)},
		wantPorts: nil,
		why:       "Since after every record's LastSeen excludes them all",
	},
	{
		name:      "since is inclusive",
		seed:      threeEntries,
		query:     RecentQuery{Since: recentBase.Add(time.Minute)},
		wantPorts: []int{3, 2},
		why:       "the same inclusive-Since convention Query uses, unchanged by dropping the identity",
	},
	{
		name:      "until is exclusive",
		seed:      threeEntries,
		query:     RecentQuery{Until: recentBase.Add(2 * time.Minute)},
		wantPorts: []int{2, 1},
		why:       "the record at exactly Until is excluded, matching Query and internal/store",
	},
	{
		name:      "a window with both bounds",
		seed:      threeEntries,
		query:     RecentQuery{Since: recentBase.Add(time.Minute), Until: recentBase.Add(2 * time.Minute)},
		wantPorts: []int{2},
		why:       "since/until compose the same way on this mode as on the per-identity one",
	},
}

func TestRecentAcrossAllEntries(t *testing.T) {
	for _, tc := range recentCases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustOpen(t, 100)
			appendAll(t, s, tc.seed)
			got := portsOf(collectRecent(t, s, tc.query))
			if !equalInts(got, tc.wantPorts) {
				t.Errorf("Recent(%+v) delivered ports %v, want %v -- %s", tc.query, got, tc.wantPorts, tc.why)
			}
		})
	}
}

func TestPostgresRecentAcrossAllEntries(t *testing.T) {
	for _, tc := range recentCases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustOpenPostgres(t, 7*24*time.Hour)
			appendAll(t, s, tc.seed)
			got := portsOf(collectRecentPG(t, s, tc.query))
			if !equalInts(got, tc.wantPorts) {
				t.Errorf("Recent(%+v) delivered ports %v, want %v -- %s", tc.query, got, tc.wantPorts, tc.why)
			}
		})
	}
}

// The bound is the safety property this mode exists to keep (see
// RecentQuery), so the clamp is asserted directly on the value both
// backends pass down rather than only through a store big enough to
// notice. A Limit of 0 or a negative one must never mean "no limit", and
// a caller-supplied one must never exceed MaxLimit.
func TestRecentLimitIsClampedNotTrusted(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int
		want int
		why  string
	}{
		{"zero means the default, never unbounded", 0, DefaultLimit,
			"an unset Limit is the common case and must not be the unbounded one"},
		{"negative means the default too", -1, DefaultLimit,
			"a negative limit is a caller bug; answering it with the whole log would be the worst reading"},
		{"a sane limit passes through", 250, 250,
			"clamping must not quietly rewrite a limit that is already fine"},
		{"MaxLimit passes through", MaxLimit, MaxLimit,
			"the ceiling itself is allowed, it is the boundary not the exclusion"},
		{"above MaxLimit is capped", MaxLimit + 1, MaxLimit,
			"the ceiling is what stops an all-entries read being an arbitrarily large response"},
		{"absurd is capped", 1 << 30, MaxLimit,
			"the clamp is on the value, so how absurd the caller was does not matter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.in); got != tc.want {
				t.Errorf("clampLimit(%d) = %d, want %d -- %s", tc.in, got, tc.want, tc.why)
			}
		})
	}
}

// The bound bounding for real, on the file backend: more records in the
// log than the limit asks for, and only the newest limit of them come
// back. Uses the default (no Limit set) as well as an explicit one,
// since "the caller forgot to bound it" is exactly the case
// RecentQuery's clamp exists for.
func TestRecentBoundsAnInstanceWithMoreEntriesThanTheLimit(t *testing.T) {
	s := mustOpen(t, 500)
	seed := make([]seeded, 0, DefaultLimit+25)
	for i := 0; i < DefaultLimit+25; i++ {
		// A distinct entry and a distinct identity per record: this is
		// the "more entries than the limit" instance, not one noisy
		// entry.
		seed = append(seed, seeded{
			entryID: fmt.Sprintf("entry-%d", i),
			source:  Identity{IP: fmt.Sprintf("198.51.100.%d", i%256)},
			port:    i + 1,
			minute:  i,
		})
	}
	appendAll(t, s, seed)

	got := collectRecent(t, s, RecentQuery{})
	if len(got) != DefaultLimit {
		t.Fatalf("Recent with no Limit delivered %d records against a log of %d, want the %d-record default -- "+
			"an unbounded all-entries read is the failure this mode is designed around", len(got), len(seed), DefaultLimit)
	}
	if got[0].Tuple.Port != len(seed) {
		t.Errorf("newest record has port %d, want %d -- the default limit must keep the newest, not the first written",
			got[0].Tuple.Port, len(seed))
	}
	for i := 1; i < len(got); i++ {
		if !got[i].LastSeen.Before(got[i-1].LastSeen) {
			t.Fatalf("result[%d] (%s) is not older than result[%d] (%s) -- ordering must be LastSeen descending",
				i, got[i].LastSeen, i-1, got[i-1].LastSeen)
		}
	}

	if n := len(collectRecent(t, s, RecentQuery{Limit: 7})); n != 7 {
		t.Errorf("Recent with Limit 7 delivered %d records, want 7", n)
	}
}

// Collapsing survives the identity being dropped: a repeated tuple is
// still one record carrying a Count, ordered by its refreshed LastSeen
// rather than by when it was first written. The all-entries mode reads
// the same folded records the per-identity one does, which is what makes
// the merged list's "n x" column mean the same thing.
func TestRecentFoldsCollapsedRepeats(t *testing.T) {
	s := mustOpen(t, 100)
	appendAll(t, s, threeEntries)

	// entry-a's match, oldest of the three, recurs and becomes newest.
	repeat := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:01"}, DestIP: "10.0.0.9", Port: 1}
	if err := s.Append("entry-a", repeat, testEvent("x"), recentBase.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got := collectRecent(t, s, RecentQuery{})
	if want := []int{1, 3, 2}; !equalInts(portsOf(got), want) {
		t.Fatalf("delivered ports %v, want %v -- a collapsed repeat is ordered by its refreshed LastSeen", portsOf(got), want)
	}
	if got[0].Count != 2 {
		t.Errorf("the collapsed record has Count %d, want 2", got[0].Count)
	}
	if len(got) != 3 {
		t.Errorf("delivered %d records, want 3 -- a repeat must fold, not become a fourth row", len(got))
	}
}

// yield returning false stops delivery here exactly as it does on
// Query -- a caller that has seen enough must be able to stop the work,
// which matters more on a mode with no identity narrowing it first.
func TestRecentYieldFalseStopsDelivery(t *testing.T) {
	s := mustOpen(t, 100)
	appendAll(t, s, threeEntries)

	n := 0
	if err := s.Recent(context.Background(), RecentQuery{}, func(Record) bool {
		n++
		return n < 2
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("delivered %d records, want exactly 2 (yield returned false after the second)", n)
	}
}

// A cancelled context stops delivery rather than running to completion:
// GET /api/matches has no rate limiter, so "the caller left" has to
// actually stop the work.
func TestRecentRespectsContextCancellation(t *testing.T) {
	s := mustOpen(t, 100)
	appendAll(t, s, threeEntries)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := 0
	err := s.Recent(ctx, RecentQuery{}, func(Record) bool {
		n++
		return true
	})
	if err == nil {
		t.Error("Recent with a cancelled context returned nil, want the context's error")
	}
	if n != 0 {
		t.Errorf("delivered %d records after cancellation, want 0", n)
	}
}

// Recent must never be reachable through the per-identity path's own
// refusal: Query with an empty Source still errors, and nothing about
// #586 turns that into "every device".
func TestQueryStillRefusesEmptyIdentityAfterRecentExists(t *testing.T) {
	s := mustOpen(t, 100)
	appendAll(t, s, threeEntries)

	err := s.Query(context.Background(), Query{}, func(Record) bool {
		t.Error("Query with an empty identity delivered a record -- it must refuse, not mean every device")
		return false
	})
	if err != ErrEmptyIdentity {
		t.Errorf("Query with an empty identity = %v, want ErrEmptyIdentity", err)
	}
}
