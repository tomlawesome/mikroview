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
// (match_test.go) and the observe/promote state machine (invert_test.go)
// in isolation -- this file deliberately does not re-litigate those at
// the same grain. Its job is the thing none of those files do: pin
// exactly what lands in internal/matchlog for each outcome (the Tuple,
// the Identity, and the on-disk/on-row shape).
//
// Pins that were here have moved to internal/engine with the code they
// characterize, twice over:
//
//   - #406 ported evaluation onto the chassis: the non-inverted
//     end-to-end pass and the inverted observe-to-violation lifecycle
//     both drove Store + Evaluator + matchlog, and the Evaluator is gone.
//     They are unchanged in what they assert -- see
//     internal/engine/expectation_characterization_test.go, which says so
//     and why.
//   - #407 deleted watchlist.Store and moved Coverage itself to
//     internal/engine/coverage.go as Definition.Coverage: the
//     Coverage's-four-states pin and the #367 known-wrong-answer pin
//     moved with it, driven through ExpectationDefinitionFor + Coverage
//     rather than the package-level Coverage(entry, rulesByDevice) this
//     package no longer has -- see
//     internal/engine/definitions_expectations_coverage_test.go, which
//     carries the same reasoning forward unchanged.
//
// Everything below characterizes code that did not move, so it stays
// where it was written.

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
