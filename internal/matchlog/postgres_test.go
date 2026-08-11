// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// These run against a real Postgres, not a mock -- same reasoning
// internal/persist's own postgres_test.go gives: the properties worth
// testing here (that a repeated tuple collapses under real concurrent
// writers, that the retention purge actually deletes) are properties of
// the database.
//
// Skipped, not failed, when MIKROVIEW_TEST_POSTGRES is unset, so
// `go test ./...` still works without one. CI supplies it via a service
// container -- see internal/persist/postgres_test.go for the identical
// contract this mirrors.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres integration tests")
	}
	return dsn
}

// withSchema and newTestPool duplicate internal/persist's own test
// helpers of the same name rather than importing them -- they're
// unexported there, and this package already keeps its own small,
// independent copies of things other packages also have (see newID's
// doc comment) rather than reaching across a package boundary for test
// infrastructure.
func withSchema(dsn, schema string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func newTestPool(t *testing.T) *persist.Pool {
	t.Helper()
	ctx := t.Context()

	schema := "test_matchlog_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	for _, r := range schema {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("test name produces an unsafe schema identifier: %q", schema)
		}
	}

	setup, err := persist.OpenPool(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("OpenPool (setup): %v", err)
	}
	if _, err := setup.Raw().Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatalf("dropping schema: %v", err)
	}
	if _, err := setup.Raw().Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	setup.Close()

	p, err := persist.OpenPool(ctx, withSchema(testDSN(t), schema))
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := persist.OpenPool(context.Background(), testDSN(t))
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Raw().Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})
	return p
}

func mustOpenPostgres(t *testing.T, retention time.Duration) *PostgresStore {
	t.Helper()
	return OpenPostgres(newTestPool(t), retention)
}

func collectPG(t *testing.T, s *PostgresStore, q Query) []Record {
	t.Helper()
	var out []Record
	if err := s.Query(context.Background(), q, func(r Record) bool {
		out = append(out, r)
		return true
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	return out
}

func TestPostgresAppendAndQueryRoundTrip(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883}
	if err := s.Append("entry-1", tuple, testEvent("first"), now); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := collectPG(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
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

// The core of #243 section 4, same as the file backend's own
// TestRepeatedTupleCollapses: a repeated identical tuple must not become
// a second stored row.
func TestPostgresRepeatedTupleCollapses(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if err := s.Append("entry-1", tuple, testEvent("x"), t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	if st := s.Stats(); st.Count != 1 {
		t.Fatalf("Stats().Count = %d, want 1 -- five identical matches must collapse to one row", st.Count)
	}

	got := collectPG(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
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

func TestPostgresDistinctTuplesDoNotCollapse(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	now := time.Now()

	if err := s.Append("e", Tuple{Source: src, DestIP: "10.0.0.5", Port: 8883}, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", Tuple{Source: src, DestIP: "10.0.0.5", Port: 443}, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Count != 2 {
		t.Errorf("Stats().Count = %d, want 2 -- a different port is a distinct tuple", st.Count)
	}
}

func TestPostgresSameTupleDifferentEntriesDoNotCollapse(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	tuple := Tuple{Source: src, DestIP: "10.0.0.5", Port: 8883}
	now := time.Now()

	if err := s.Append("entry-a", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("entry-b", tuple, testEvent("x"), now); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Count != 2 {
		t.Errorf("Stats().Count = %d, want 2 -- the same tuple under two entries must not collapse", st.Count)
	}
}

func TestPostgresAppendRefusesEmptyIdentity(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	err := s.Append("e", Tuple{DestIP: "1.2.3.4", Port: 1}, testEvent("x"), time.Now())
	if err != ErrEmptyIdentity {
		t.Errorf("Append with no MAC/IP = %v, want ErrEmptyIdentity", err)
	}
}

func TestPostgresQueryRefusesEmptyIdentity(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	err := s.Query(context.Background(), Query{}, func(Record) bool { return true })
	if err != ErrEmptyIdentity {
		t.Errorf("Query with no MAC/IP = %v, want ErrEmptyIdentity", err)
	}
}

// Same MAC-preferred matching contract as the file backend's own
// TestMatchingPrefersMACOverIP (#243 section 1).
func TestPostgresMatchingPrefersMACOverIP(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	now := time.Now()

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

	byOldIP := collectPG(t, s, Query{Source: Identity{IP: "192.168.1.10"}})
	if len(byOldIP) != 0 {
		t.Errorf("querying by the stale IP found %d records, want 0", len(byOldIP))
	}
	byMAC := collectPG(t, s, Query{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
	if len(byMAC) != 1 || byMAC[0].Count != 2 {
		t.Fatalf("querying by MAC got %+v, want one record with Count=2", byMAC)
	}
}

func TestPostgresIPFallbackWhenNoMACKnown(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	tuple := Tuple{Source: Identity{IP: "192.168.1.10"}, DestIP: "1.2.3.4", Port: 443}
	if err := s.Append("e", tuple, testEvent("x"), time.Now()); err != nil {
		t.Fatal(err)
	}
	got := collectPG(t, s, Query{Source: Identity{IP: "192.168.1.10"}})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1 via IP fallback", len(got))
	}
}

func TestPostgresQueryFiltersByTimeWindow(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	if err := s.Append("e", Tuple{Source: src, DestIP: "1.1.1.1", Port: 1}, testEvent("x"), base); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", Tuple{Source: src, DestIP: "2.2.2.2", Port: 2}, testEvent("x"), base.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := collectPG(t, s, Query{Source: src, Since: base, Until: base.Add(time.Hour)})
	if len(got) != 1 || got[0].Tuple.DestIP != "1.1.1.1" {
		t.Fatalf("windowed query got %+v, want only the first record", got)
	}

	got = collectPG(t, s, Query{Source: src, Since: base})
	if len(got) != 2 {
		t.Fatalf("unbounded-Until query got %d records, want 2", len(got))
	}
}

// Same overlap contract as the file backend's own
// TestQueryIncludesRecordsSpanningIntoTheWindow.
func TestPostgresQueryIncludesRecordsSpanningIntoTheWindow(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	tuple := Tuple{Source: src, DestIP: "1.1.1.1", Port: 1}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	if err := s.Append("e", tuple, testEvent("x"), base); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", tuple, testEvent("x"), base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := collectPG(t, s, Query{Source: src, Since: base.Add(time.Hour), Until: base.Add(3 * time.Hour)})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1 (the record spans into the window even though it started before Since)", len(got))
	}
}

func TestPostgresQueryRespectsLimitAndOrdersMostRecentFirst(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: i + 1}
		if err := s.Append("e", tuple, testEvent("x"), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	got := collectPG(t, s, Query{Source: src, Limit: 3})
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3 (Limit)", len(got))
	}
	for i, want := range []int{10, 9, 8} {
		if got[i].Tuple.Port != want {
			t.Errorf("result[%d].Tuple.Port = %d, want %d", i, got[i].Tuple.Port, want)
		}
	}
}

func TestPostgresQueryYieldFalseStopsDelivery(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	now := time.Now()
	for i := 0; i < 5; i++ {
		tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: i + 1}
		if err := s.Append("e", tuple, testEvent("x"), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	n := 0
	if err := s.Query(context.Background(), Query{Source: src}, func(Record) bool {
		n++
		return n < 2
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("delivered %d records, want exactly 2 (yield returned false after the second)", n)
	}
}

// Unlike the file backend, there is no ceiling here -- see
// PostgresStore's own doc comment.
func TestPostgresStatsHasNoCeiling(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	for i := 0; i < 5; i++ {
		tuple := Tuple{Source: src, DestIP: "10.0.0.1", Port: i + 1}
		if err := s.Append("e", tuple, testEvent("x"), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	st := s.Stats()
	if st.Count != 5 {
		t.Errorf("Stats().Count = %d, want 5", st.Count)
	}
	if st.Capacity != 0 || st.Full {
		t.Errorf("Stats() = %+v, want Capacity=0 and Full=false -- this backend has no ceiling", st)
	}
}

// The retention purge is this backend's own mechanism, replacing the
// file backend's capacity refusal (#243 section 3: "config knob,
// default 7 days" on Postgres, "pragmatically unlimited" record count).
func TestPostgresPurgeDeletesOlderThanRetention(t *testing.T) {
	s := mustOpenPostgres(t, time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}

	old := Tuple{Source: src, DestIP: "1.1.1.1", Port: 1}
	fresh := Tuple{Source: src, DestIP: "2.2.2.2", Port: 2}
	if err := s.Append("e", old, testEvent("x"), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("e", fresh, testEvent("x"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Count != 2 {
		t.Fatalf("setup: Stats().Count = %d, want 2 before purging", st.Count)
	}

	s.purgeOnceRecovered()

	got := collectPG(t, s, Query{Source: src})
	if len(got) != 1 || got[0].Tuple.DestIP != "2.2.2.2" {
		t.Fatalf("after purge: got %+v, want only the fresh record to survive", got)
	}
	if st := s.Stats(); st.Count != 1 {
		t.Errorf("Stats().Count = %d after purge, want 1", st.Count)
	}
}

func TestPostgresPurgeKeepsRecentMatches(t *testing.T) {
	s := mustOpenPostgres(t, 7*24*time.Hour)
	src := Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	if err := s.Append("e", Tuple{Source: src, DestIP: "1.1.1.1", Port: 1}, testEvent("x"), time.Now()); err != nil {
		t.Fatal(err)
	}

	s.purgeOnceRecovered()

	if st := s.Stats(); st.Count != 1 {
		t.Errorf("Stats().Count = %d after purge, want 1 -- a match inside the retention window must survive", st.Count)
	}
}

// Real concurrent writers, all appending the exact same tuple -- the
// path where two processes can both lose the initial collapse check and
// race to insert (see Append's own doc comment on the unique-violation
// retry). Every Append must succeed and the result must still collapse
// to exactly one row with the right count, not error out and not
// silently create duplicates.
func TestPostgresConcurrentAppendCollapsesCorrectly(t *testing.T) {
	const writers = 16
	s := mustOpenPostgres(t, 7*24*time.Hour)
	tuple := Tuple{Source: Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.Append("e", tuple, testEvent("x"), time.Now())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	if st := s.Stats(); st.Count != 1 {
		t.Errorf("Stats().Count = %d, want 1 -- concurrent appends of the same tuple must still collapse to one row", st.Count)
	}
	got := collectPG(t, s, Query{Source: tuple.Source})
	if len(got) != 1 || got[0].Count != writers {
		t.Fatalf("got %+v, want one record with Count=%d", got, writers)
	}
}
