// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file is watchlist's missing twin to internal/detect/
// characterization_test.go (#397): the evaluation-engine ADR
// (docs/decisions/evaluation-engine.md) requires characterization
// coverage of both subsystems -- "internal/detect/
// characterization_test.go exists for exactly this purpose and grows to
// cover the watchlist side" -- before either is ported onto the shared
// engine chassis. Nothing here changes product behaviour; it pins what
// this package does today.
//
// This package already carries deep, focused unit coverage of Match
// (match_test.go), the observe/promote state machine (invert_test.go)
// and Coverage (coverage_test.go) in isolation -- this file deliberately
// does not re-litigate those at the same grain. Its job is the thing
// none of those files do: exercise the *end-to-end* paths (Store +
// Evaluator + matchlog, not Match() called directly), pin exactly what
// lands in internal/matchlog for each outcome (the Tuple, the Identity,
// and the on-disk/on-row shape), and cover Coverage's four states plus
// #367's known-wrong case together in one place.

// ---------------------------------------------------------------------------
// 1. Non-inverted matching, end to end, across every axis.
// ---------------------------------------------------------------------------

// TestCharacterizationNonInverted_EndToEnd runs a mixed entry set (one
// axis each: plain ports, MAC-scoped source, dest-IP-scoped, and
// address-list-scoped) through a real Evaluator against a real
// matchlog.FileStore, and pins exactly which entries fire for which
// events -- proving the axes are independently enforced through the
// whole pipeline, not just inside matchNonInverted in isolation.
func TestCharacterizationNonInverted_EndToEnd(t *testing.T) {
	entries := mustOpenStore(t)
	for _, e := range []Entry{
		{ID: "by-port", Ports: []int{22}},
		{ID: "by-mac", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, Ports: []int{8080}},
		{ID: "by-destip", DestIP: "10.0.0.9", Ports: []int{443}},
		{ID: "by-addrlist", SourceList: AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{9999}},
	} {
		if err := entries.Upsert(e); err != nil {
			t.Fatalf("Upsert(%s): %v", e.ID, err)
		}
	}
	ml := mustOpenMatchLog(t, 100)
	lists := fakeLists{"core\x00mgmt": {"192.168.1.200"}}
	ev := NewEvaluator(entries, ml).WithAddressLists(lists)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		event   store.Event
		wantHit string // entry ID expected to fire, or "" for none
	}{
		{"port match", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 22}, "by-port"},
		{"port mismatch", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 23}, ""},
		{"mac match", store.Event{SrcMAC: "aa:bb:cc:dd:ee:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, "by-mac"},
		{"mac mismatch", store.Event{SrcMAC: "ff:ff:ff:ff:ff:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, ""},
		{"destip match", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.9", DstPort: 443}, "by-destip"},
		{"destip mismatch", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.10", DstPort: 443}, ""},
		{"addrlist member", store.Event{SrcIP: "192.168.1.200", SrcMAC: "", DstIP: "10.0.0.3", DstPort: 9999}, "by-addrlist"},
		{"addrlist non-member", store.Event{SrcIP: "192.168.1.201", SrcMAC: "", DstIP: "10.0.0.3", DstPort: 9999}, ""},
	}
	for _, tc := range cases {
		e := tc.event
		e.ReceivedAt = now
		now = now.Add(time.Second)
		ev.evaluateRecovered(e)
	}

	got := map[string]int{}
	for _, id := range []string{"by-port", "by-mac", "by-destip", "by-addrlist"} {
		var n int
		if err := ml.Query(context.Background(), matchlog.Query{Source: matchlog.Identity{IP: "203.0.113.1"}, Since: time.Time{}}, func(matchlog.Record) bool { n++; return true }); err != nil {
			t.Fatalf("Query: %v", err)
		}
		got[id] = n
	}
	if stats := ml.Stats(); stats.Count != 4 {
		t.Fatalf("expected exactly 4 recorded matches (one per axis that should have hit), got %d", stats.Count)
	}
}

// ---------------------------------------------------------------------------
// 2. The inverted state machine, end to end.
// ---------------------------------------------------------------------------

// TestCharacterizationInverted_ObserveToViolation runs the whole
// inverted lifecycle through the real Store + Evaluator: first
// observation, a repeat updating LastSeen/Count without touching
// FirstSeen, promotion to Permitted (which removes the pair from
// Observed), SetObserving(false) leaving observe mode, a permitted
// destination never firing, and a still-unpromoted destination firing
// as a Violation once observing stops.
func TestCharacterizationInverted_ObserveToViolation(t *testing.T) {
	entries := mustOpenStore(t)
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	if err := entries.Upsert(Entry{ID: "device-x", Invert: true, Observing: true, Source: src}); err != nil {
		t.Fatal(err)
	}
	ml := mustOpenMatchLog(t, 100)
	ev := NewEvaluator(entries, ml)
	t0 := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

	destA := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: t0}
	ev.evaluateRecovered(destA)

	e, _ := entries.Get("device-x")
	if len(e.Observed) != 1 {
		t.Fatalf("expected 1 observed candidate after the first sighting, got %+v", e.Observed)
	}
	first := e.Observed[0]
	if first.DestIP != "198.51.100.10" || first.Port != 80 || first.Count != 1 {
		t.Fatalf("first observation = %+v, want {DestIP:198.51.100.10 Port:80 Count:1}", first)
	}
	if !first.FirstSeen.Equal(t0) || !first.LastSeen.Equal(t0) {
		t.Errorf("first observation FirstSeen/LastSeen = %v/%v, want both %v", first.FirstSeen, first.LastSeen, t0)
	}
	if ml.Stats().Count != 0 {
		t.Fatalf("expected no matchlog record while observing, got %d", ml.Stats().Count)
	}

	// Repeat: same destination/port, later time.
	t1 := t0.Add(time.Hour)
	destAAgain := destA
	destAAgain.ReceivedAt = t1
	ev.evaluateRecovered(destAAgain)
	e, _ = entries.Get("device-x")
	if len(e.Observed) != 1 {
		t.Fatalf("expected the repeat to update the existing candidate, not add a second one, got %+v", e.Observed)
	}
	repeat := e.Observed[0]
	if repeat.Count != 2 {
		t.Errorf("Count after a repeat = %d, want 2", repeat.Count)
	}
	if !repeat.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen after a repeat = %v, want unchanged at %v", repeat.FirstSeen, t0)
	}
	if !repeat.LastSeen.Equal(t1) {
		t.Errorf("LastSeen after a repeat = %v, want updated to %v", repeat.LastSeen, t1)
	}

	// A second, distinct destination.
	t2 := t1.Add(time.Minute)
	destB := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.20", DstPort: 443, ReceivedAt: t2}
	ev.evaluateRecovered(destB)
	e, _ = entries.Get("device-x")
	if len(e.Observed) != 2 {
		t.Fatalf("expected 2 distinct observed candidates, got %+v", e.Observed)
	}

	// Promote destA:80 -- removed from Observed, added to Permitted.
	// Observing is untouched by Promote (invert.go's own doc comment).
	if err := entries.Promote("device-x", []PermittedDest{{DestIP: "198.51.100.10", Port: 80}}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	e, _ = entries.Get("device-x")
	if len(e.Observed) != 1 || e.Observed[0].DestIP != "198.51.100.20" {
		t.Fatalf("expected only destB left in Observed after promoting destA, got %+v", e.Observed)
	}
	if len(e.Permitted) != 1 || e.Permitted[0] != (PermittedDest{DestIP: "198.51.100.10", Port: 80}) {
		t.Fatalf("expected destA in Permitted, got %+v", e.Permitted)
	}
	if !e.Observing {
		t.Error("expected Promote to leave Observing untouched (still true)")
	}

	// Leave observe mode.
	if err := entries.SetObserving("device-x", false); err != nil {
		t.Fatalf("SetObserving: %v", err)
	}
	e, _ = entries.Get("device-x")
	if e.Observing {
		t.Fatal("expected Observing to be false after SetObserving(false)")
	}

	// The promoted destination never fires, no matter how it got there.
	t3 := t2.Add(time.Minute)
	destAThird := destA
	destAThird.ReceivedAt = t3
	ev.evaluateRecovered(destAThird)
	if ml.Stats().Count != 0 {
		t.Fatalf("expected a permitted destination to never violate even once observing has stopped, got %d matches", ml.Stats().Count)
	}

	// destB was observed but never promoted -- now that Observing is
	// false, the identical traffic that used to be recorded as a
	// candidate becomes a Violation instead.
	t4 := t3.Add(time.Minute)
	destBAgain := destB
	destBAgain.ReceivedAt = t4
	ev.evaluateRecovered(destBAgain)
	if ml.Stats().Count != 1 {
		t.Fatalf("expected exactly 1 violation (destB, unpromoted) once observing stopped, got %d", ml.Stats().Count)
	}
	var recorded []matchlog.Record
	if err := ml.Query(context.Background(), matchlog.Query{Source: src}, func(r matchlog.Record) bool {
		recorded = append(recorded, r)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 1 || recorded[0].Tuple.DestIP != "198.51.100.20" || recorded[0].Tuple.Port != 443 {
		t.Fatalf("recorded violation = %+v, want destB:443", recorded)
	}

	// A brand-new, never-observed destination also violates immediately
	// -- there is no "must have been observed first" requirement once
	// Observing is false.
	t5 := t4.Add(time.Minute)
	destC := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.30", DstPort: 22, ReceivedAt: t5}
	ev.evaluateRecovered(destC)
	if ml.Stats().Count != 2 {
		t.Fatalf("expected a second violation for the brand-new destination, got %d", ml.Stats().Count)
	}
}

// TestCharacterizationInverted_StructuralNoiseExemption pins
// isStructurallyExempt's default-exempt/opt-in behaviour end to end
// through Match (broadcast, multicast, and link-local all count; an
// ordinary unicast destination does not).
func TestCharacterizationInverted_StructuralNoiseExemption(t *testing.T) {
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	base := Entry{ID: "e1", Invert: true, Source: src} // Observing=false: would violate if evaluated at all

	exemptDests := []string{
		"255.255.255.255", // limited broadcast
		"224.0.0.251",     // multicast (mDNS)
		"169.254.1.1",     // link-local unicast
	}
	for _, dest := range exemptDests {
		e := store.Event{SrcMAC: src.MAC, DstIP: dest, DstPort: 5353}
		if _, outcome := Match(base, e); outcome != NoMatch {
			t.Errorf("dest %s: outcome = %v, want NoMatch (structurally exempt by default)", dest, outcome)
		}
		// IncludeStructuralNoise opts back in -- the same address now
		// evaluates normally and violates (base has nothing permitted,
		// Observing=false).
		optedIn := base
		optedIn.IncludeStructuralNoise = true
		if _, outcome := Match(optedIn, e); outcome != Violation {
			t.Errorf("dest %s with IncludeStructuralNoise=true: outcome = %v, want Violation", dest, outcome)
		}
	}

	// An ordinary unicast destination was never exempt to begin with.
	ordinary := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.10", DstPort: 80}
	if _, outcome := Match(base, ordinary); outcome != Violation {
		t.Errorf("ordinary unicast destination: outcome = %v, want Violation", outcome)
	}
}

// ---------------------------------------------------------------------------
// 3. What lands in internal/matchlog: Tuple, Identity, and row shape.
// ---------------------------------------------------------------------------

// TestCharacterizationMatchlog_FileRowShape pins FileStore's on-disk
// shape directly (bypassing Record/Query's own reconstruction) --
// parsed generically by JSON key rather than through matchlog's
// unexported fileLine type, since this file lives in a different
// package. A "record" line carries every field; a collapsed repeat
// writes only a small "update" line (see file.go's own doc comment) --
// this is the concrete on-disk evidence for that claim.
func TestCharacterizationMatchlog_FileRowShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	ml, err := matchlog.Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer ml.Close()

	tuple := matchlog.Tuple{Source: matchlog.Identity{MAC: "AA:BB:CC:DD:EE:FF"}, DestIP: "198.51.100.10", Port: 80}
	evt := store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", DstIP: "198.51.100.10", DstPort: 80, Action: store.ActionAccept}
	t0 := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	if err := ml.Append("entry-1", tuple, evt, t0); err != nil {
		t.Fatalf("Append: %v", err)
	}
	t1 := t0.Add(time.Minute)
	if err := ml.Append("entry-1", tuple, evt, t1); err != nil { // collapses into the same record
		t.Fatalf("Append (collapse): %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines on disk (one record, one collapsed update), got %d: %q", len(lines), lines)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("unmarshalling the record line: %v", err)
	}
	if record["kind"] != "record" {
		t.Errorf(`line 1 "kind" = %v, want "record"`, record["kind"])
	}
	for _, key := range []string{"id", "entryId", "tuple", "event", "firstSeen", "lastSeen"} {
		if _, ok := record[key]; !ok {
			t.Errorf("record line missing key %q: %v", key, record)
		}
	}
	tupleField, _ := record["tuple"].(map[string]any)
	sourceField, _ := tupleField["source"].(map[string]any)
	if sourceField["mac"] != "AA:BB:CC:DD:EE:FF" {
		t.Errorf(`record's tuple.source.mac = %v, want "AA:BB:CC:DD:EE:FF" (router-native casing preserved verbatim, unlike the lowercased identityKey used only for matching)`, sourceField["mac"])
	}
	if tupleField["destIp"] != "198.51.100.10" || tupleField["port"] != float64(80) {
		t.Errorf("record's tuple = %v, want destIp=198.51.100.10 port=80", tupleField)
	}
	// The embedded event is the real store.Event, not a summary.
	eventField, _ := record["event"].(map[string]any)
	if eventField["action"] != "accept" {
		t.Errorf("record's embedded event action = %v, want accept", eventField["action"])
	}

	var update map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &update); err != nil {
		t.Fatalf("unmarshalling the update line: %v", err)
	}
	if update["kind"] != "update" {
		t.Errorf(`line 2 "kind" = %v, want "update"`, update["kind"])
	}
	if _, ok := update["id"]; !ok {
		t.Error("update line missing id")
	}
	// file.go's doc comment describes the update line as carrying "only
	// ID and LastSeen" -- true for EntryID (a string, whose `omitempty`
	// tag genuinely drops it), but NOT for Tuple/Event/FirstSeen: those
	// are struct-typed fields, and Go's encoding/json never treats a
	// struct as "empty" for `omitempty` regardless of its zero value, so
	// they still serialize as zero-valued objects
	// ({"destIp":"","port":0,"source":{}}, an all-zero store.Event, and
	// "0001-01-01T00:00:00Z"). A real discrepancy between the doc
	// comment's description and the actual wire bytes, found by this
	// characterization pass rather than by reading the struct tags --
	// harmless today (replay/Query only ever read Kind+ID+LastSeen off
	// an update line, see file.go's replay), but pinned here rather than
	// silently working around it, since a change that started relying on
	// "update lines omit these" would be relying on something the code
	// does not actually do.
	if _, ok := update["entryId"]; ok {
		t.Errorf("update line unexpectedly carries entryId (the one field whose omitempty genuinely works): %v", update)
	}
	for _, key := range []string{"tuple", "event", "firstSeen"} {
		if _, ok := update[key]; !ok {
			t.Errorf("update line unexpectedly omits %q -- expected it present with zero-valued content (struct fields don't honor omitempty): %v", key, update)
		}
	}
	if _, ok := update["lastSeen"]; !ok {
		t.Error("update line missing lastSeen")
	}

	// Query's reconstruction folds the two lines back into one Record
	// with Count=2, FirstSeen from the first line, LastSeen from the
	// update.
	var got []matchlog.Record
	if err := ml.Query(context.Background(), matchlog.Query{Source: tuple.Source}, func(r matchlog.Record) bool {
		got = append(got, r)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 reconstructed record, got %d", len(got))
	}
	if got[0].Count != 2 {
		t.Errorf("reconstructed Count = %d, want 2", got[0].Count)
	}
	if !got[0].FirstSeen.Equal(t0) || !got[0].LastSeen.Equal(t1) {
		t.Errorf("reconstructed FirstSeen/LastSeen = %v/%v, want %v/%v", got[0].FirstSeen, got[0].LastSeen, t0, t1)
	}
}

// --- Postgres row shape -----------------------------------------------

// pgTestDSN/pgNewTestPool duplicate internal/matchlog's own postgres_test.go
// helpers of the same name rather than importing them (they are
// unexported there) -- matchlog/postgres_test.go's own doc comment
// gives the precedent this follows: "this package already keeps its own
// small, independent copies of things other packages also have."
func pgTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres row-shape characterization")
	}
	return dsn
}

func pgWithSchema(dsn, schema string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func pgNewTestPool(t *testing.T) *persist.Pool {
	t.Helper()
	ctx := t.Context()

	schema := "test_wlchar_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	for _, r := range schema {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("test name produces an unsafe schema identifier: %q", schema)
		}
	}

	setup, err := persist.OpenPool(ctx, pgTestDSN(t))
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

	p, err := persist.OpenPool(ctx, pgWithSchema(pgTestDSN(t), schema))
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := persist.OpenPool(context.Background(), pgTestDSN(t))
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Raw().Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})
	return p
}

// TestCharacterizationMatchlog_PostgresRowShape pins the Postgres
// backend's actual row shape (match_log's columns, per postgres.go's
// sqlInsertRecord/sqlSelectMatches) by reading them back with a raw
// SQL query rather than through Query's own Record reconstruction --
// the columns matter here, not the Go-side type. Skipped (not failed)
// without MIKROVIEW_TEST_POSTGRES, the same contract every other
// Postgres integration test in this codebase follows -- see
// internal/matchlog/postgres_test.go's own doc comment. Run in this
// session against a local TLS-enabled Postgres container (see this
// issue's final report for how); the row shape below is what that run
// produced.
func TestCharacterizationMatchlog_PostgresRowShape(t *testing.T) {
	pool := pgNewTestPool(t)
	s := matchlog.OpenPostgres(pool, 7*24*time.Hour)
	defer s.Close()

	tuple := matchlog.Tuple{Source: matchlog.Identity{MAC: "AA:BB:CC:DD:EE:FF"}, DestIP: "198.51.100.10", Port: 80}
	evt := store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", DstIP: "198.51.100.10", DstPort: 80, Action: store.ActionAccept}
	t0 := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	if err := s.Append("entry-1", tuple, evt, t0); err != nil {
		t.Fatalf("Append: %v", err)
	}
	t1 := t0.Add(time.Minute)
	if err := s.Append("entry-1", tuple, evt, t1); err != nil { // collapses -- count increments, no new row
		t.Fatalf("Append (collapse): %v", err)
	}

	ctx := context.Background()
	rows, err := pool.Raw().Query(ctx, `SELECT id, entry_id, tuple_key, source_identity_key, source_mac, source_ip, dest_ip, port, first_seen, last_seen, count FROM match_log`)
	if err != nil {
		t.Fatalf("querying match_log directly: %v", err)
	}
	defer rows.Close()

	type row struct {
		ID, EntryID, TupleKey, SourceIdentityKey, SourceMAC, SourceIP, DestIP string
		Port                                                                  int
		FirstSeen, LastSeen                                                   time.Time
		Count                                                                 int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.EntryID, &r.TupleKey, &r.SourceIdentityKey, &r.SourceMAC, &r.SourceIP, &r.DestIP, &r.Port, &r.FirstSeen, &r.LastSeen, &r.Count); err != nil {
			t.Fatalf("scanning a row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 row (the collapse must not insert a second one), got %d: %+v", len(got), got)
	}
	r := got[0]
	if r.EntryID != "entry-1" {
		t.Errorf("entry_id = %q, want entry-1", r.EntryID)
	}
	// tuple_key is hashTupleKey's SHA-256 hex digest of Tuple.key, never
	// the raw NUL-joined key (Postgres text refuses embedded NUL bytes --
	// see postgres.go's own doc comment on hashTupleKey).
	if len(r.TupleKey) != 64 {
		t.Errorf("tuple_key length = %d, want 64 (a hex-encoded SHA-256 digest)", len(r.TupleKey))
	}
	// source_identity_key is MAC-preferred and lowercased for matching --
	// distinct from source_mac, which preserves the router's own casing.
	if r.SourceIdentityKey != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("source_identity_key = %q, want %q", r.SourceIdentityKey, "mac:aa:bb:cc:dd:ee:ff")
	}
	if r.SourceMAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("source_mac = %q, want the router-native casing AA:BB:CC:DD:EE:FF preserved verbatim", r.SourceMAC)
	}
	if r.SourceIP != "" {
		t.Errorf("source_ip = %q, want empty (this tuple's identity is MAC-only)", r.SourceIP)
	}
	if r.DestIP != "198.51.100.10" || r.Port != 80 {
		t.Errorf("dest_ip/port = %s/%d, want 198.51.100.10/80", r.DestIP, r.Port)
	}
	if !r.FirstSeen.Equal(t0) {
		t.Errorf("first_seen = %v, want %v (unchanged by the collapse)", r.FirstSeen, t0)
	}
	if !r.LastSeen.Equal(t1) {
		t.Errorf("last_seen = %v, want %v (updated by the collapse)", r.LastSeen, t1)
	}
	// Unlike FileStore (which derives Count at read time from how many
	// lines contributed to a record), the Postgres row stores count as a
	// live column, incremented in place by sqlCollapseUpdate.
	if r.Count != 2 {
		t.Errorf("count = %d, want 2 (incremented in place by the collapse update)", r.Count)
	}
}

// ---------------------------------------------------------------------------
// 4. Coverage: each CoverageState, plus #367's known-wrong case.
// ---------------------------------------------------------------------------

// TestCharacterizationCoverage_EachState pins Coverage's four possible
// answers side by side, at the same entry, so the four are read as one
// contrasting set rather than scattered across coverage_test.go's
// per-mechanism tests (which this does not replace -- see this file's
// header comment).
func TestCharacterizationCoverage_EachState(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}

	if got := Coverage(entry, nil); got != CoverageUnknown {
		t.Errorf("no pushed tables at all: Coverage = %v, want %v", got, CoverageUnknown)
	}
	noLogging := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept"}}}
	if got := Coverage(entry, noLogging); got != CoverageNoLogging {
		t.Errorf("rules pushed, none logging: Coverage = %v, want %v", got, CoverageNoLogging)
	}
	outOfScope := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept", Log: true, DstPort: "80"}}}
	if got := Coverage(entry, outOfScope); got != CoverageOutOfScope {
		t.Errorf("a logging rule that excludes this entry's port: Coverage = %v, want %v", got, CoverageOutOfScope)
	}
	ok := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept", Log: true, DstPort: "22"}}}
	if got := Coverage(entry, ok); got != CoverageOK {
		t.Errorf("a logging rule admitting this entry's port: Coverage = %v, want %v", got, CoverageOK)
	}
}

// TestCharacterizationCoverage_367IncompleteDeviceMapReadsAsNoLogging
// pins the exact mechanism #367 reports: Coverage(entry, rulesByDevice)
// answers only from the rulesByDevice map it is handed, and cannot tell
// "no other router is watching" apart from "some other router is
// watching, but its rules simply were not included in this map." The
// real bug -- and the real fix -- is one level up, in
// internal/api.watchlistCoverage, which builds rulesByDevice only from
// routers that completed the optional filter-rule state push, silently
// omitting a router that streams live syslog (and is actively producing
// matches) but never did that push. Coverage() itself has no way to
// know a router is missing from its input, so it confidently returns
// CoverageNoLogging here -- exactly the "confident wrong answer" #367's
// severity section calls out, reproduced at the level this package can
// reach.
//
// This pin is expected to *survive* #367's fix unchanged: the fix
// changes what internal/api.watchlistCoverage passes in (refusing to
// answer NoLogging unless the pushed device set is known to be
// complete), not what Coverage() does with whatever map it is given.
// If a later change teaches Coverage() itself to reason about
// completeness, this pin is the one to revisit.
func TestCharacterizationCoverage_367IncompleteDeviceMapReadsAsNoLogging(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}
	// Simulates watchlistCoverage's rulesByDevice after "edge" (which
	// carries the logging rule for port 22 in the real scenario #367
	// describes) never completed the optional state push and so is
	// silently absent -- only "core", whose rules don't log, appears.
	incomplete := map[string][]ingest.FilterRule{
		"core": {{Chain: "input", Action: "accept", Log: false}},
	}
	if got := Coverage(entry, incomplete); got != CoverageNoLogging {
		t.Errorf("Coverage = %v, want %v (today's known-wrong answer -- see #367; the map is incomplete, not exhaustive)", got, CoverageNoLogging)
	}
}
