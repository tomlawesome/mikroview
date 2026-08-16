// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/suggest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// watchlistDocument is the on-disk shape of the watchlist JSON document:
// {"entries": [...]}, unchanged by #407. internal/watchlist.Store owned
// this shape (as an unexported storeFile) before #407 deleted the store
// entirely; engine.migrateWatchlistFile (definitions_migrate.go) mirrors
// it independently for the same reason this does -- the document itself
// is still real, read at boot as a migration source by
// engine.MigrateDefinitions and engine.AdoptWatchlistEntries, and these
// tests write and read it directly since there is no store left to go
// through. A typed helper is deliberate over a raw string literal or a
// map: an "entries" field rename breaks this test loudly at compile
// time, rather than the test silently asserting nothing.
type watchlistDocument struct {
	Entries []watchlist.Entry `json:"entries"`
}

// writeWatchlistDocument writes a watchlist document holding entries
// straight to path, standing in for watchlist.Store.Upsert now that the
// store is gone (#407) -- see watchlistDocument's own doc comment. There
// is no write-behind persistence to flush afterward, unlike the store it
// replaces: the bytes are on disk before this returns.
func writeWatchlistDocument(t *testing.T, path string, entries ...watchlist.Entry) {
	t.Helper()
	doc := watchlistDocument{Entries: entries}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling watchlist document: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("writing watchlist document: %v", err)
	}
}

// readWatchlistDocument reads back a watchlist document -- one written
// by writeWatchlistDocument, or one produced by a real deployment and
// carried through a backup/restore round trip.
func readWatchlistDocument(t *testing.T, path string) watchlistDocument {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading watchlist document: %v", err)
	}
	var doc watchlistDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshaling watchlist document: %v", err)
	}
	return doc
}

// TestBackupRestoreRoundTripCarriesWatchlist pins the failure scenario
// #372 reported: an operator's watchlist entries, suggestions and match
// log surviving `-backup` followed by `-restore` into a fresh directory,
// end to end through the real stores and documents rather than through
// backedUpStores' (Name, Path) pairs alone. It fails against the pre-fix
// backedUpStores (verified by hand: with the three Watchlist entries
// removed, the envelope carries none of these three stores and the
// restored watchlist document is empty).
//
// #407 deleted internal/watchlist.Store: the watchlist document is now a
// migration source only, read at boot by engine.MigrateDefinitions and
// engine.AdoptWatchlistEntries into the definitions store (see
// backup_definitions_roundtrip_test.go for that half). It is still
// listed in backedUpStores and this test still pins that it survives
// backup/restore -- a backup silently dropping it would still lose every
// entry an unmigrated (or not-yet-upgraded) deployment has, exactly as
// before #407, just one hop further back in the pipeline.
//
// The match log gets two records, deliberately, not one -- a single
// record is the one case that happens to already be valid JSON on its
// own and would not have caught internal/matchlog's JSON-lines shape
// breaking the envelope's json.RawMessage embedding (see backup_cli.go's
// jsonLinesStore/wrapForEnvelope): backup.Write fails outright once a
// match log has more than one line unless that wrapping is in place.
func TestBackupRestoreRoundTripCarriesWatchlist(t *testing.T) {
	srcDir := t.TempDir()
	watchlistPath := filepath.Join(srcDir, "watchlist.json")
	suggestionsPath := filepath.Join(srcDir, "suggestions.json")
	matchLogPath := filepath.Join(srcDir, "matchlog.jsonl")

	// Write the watchlist document directly with one entry -- the
	// document is a migration source only now (#407: internal/watchlist.
	// Store is deleted), so there is no store to seed through, and
	// correspondingly no write-behind persistence to flush before
	// runBackup reads watchlistPath below: the bytes are already on disk.
	entry := watchlist.Entry{ID: "e1", Name: "ssh-watch", Ports: []int{22}}
	writeWatchlistDocument(t, watchlistPath, entry)

	// Populate suggestions with one candidate.
	sg, err := suggest.Open(suggestionsPath)
	if err != nil {
		t.Fatalf("suggest.Open: %v", err)
	}
	candidate := suggest.Candidate{
		ID:            "c1",
		Kind:          suggest.KindPort,
		Name:          "rdp-test",
		Justification: "blocked by rule D|rdp-test| on chain-1",
		RouterDevice:  "router-1",
		Ports:         []int{3389},
	}
	if err := sg.Sync([]suggest.Candidate{candidate}); err != nil {
		t.Fatalf("suggest Sync: %v", err)
	}

	// Populate the match log with two records -- two distinct tuples, so
	// the file has two "record" lines rather than a record and a
	// collapsed update.
	ml, err := matchlog.Open(matchLogPath, 100)
	if err != nil {
		t.Fatalf("matchlog.Open: %v", err)
	}
	src := matchlog.Identity{IP: "10.0.0.5"}
	now := time.Now()
	if err := ml.Append("e1", matchlog.Tuple{Source: src, DestIP: "10.0.0.1", Port: 22}, testMatchEvent(), now); err != nil {
		t.Fatalf("matchlog Append 1: %v", err)
	}
	if err := ml.Append("e1", matchlog.Tuple{Source: src, DestIP: "10.0.0.1", Port: 23}, testMatchEvent(), now.Add(time.Second)); err != nil {
		t.Fatalf("matchlog Append 2: %v", err)
	}
	if err := ml.Close(); err != nil {
		t.Fatalf("matchlog Close: %v", err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_WATCHLIST_STORE_PATH", watchlistPath)
	t.Setenv("MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH", suggestionsPath)
	t.Setenv("MIKROVIEW_WATCHLIST_MATCH_LOG_PATH", matchLogPath)

	// --force: t.TempDir() is world-readable in some sandboxes, which
	// would otherwise trip writeBackup's own world-readable-directory
	// refusal -- unrelated to what this test is pinning, same reason
	// backup_cli_test.go's existing tests write straight through
	// writeBackup(force=true) rather than runBackup.
	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	// Unpack the envelope directly and assert the three watchlist stores
	// are present -- this is the part that silently failed before #372's
	// fix: none of these three keys existed in the envelope at all.
	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatalf("backup.Read: %v", err)
	}
	for _, name := range []string{"watchlist", "suggestions", "match_log"} {
		if _, ok := env.Stores[name]; !ok {
			t.Errorf("backup envelope is missing store %q", name)
		}
	}

	// Restore into a fresh directory -- a different set of paths, the
	// same way a disaster recovery restores onto a new host.
	dstDir := t.TempDir()
	newWatchlistPath := filepath.Join(dstDir, "watchlist.json")
	newSuggestionsPath := filepath.Join(dstDir, "suggestions.json")
	newMatchLogPath := filepath.Join(dstDir, "matchlog.jsonl")
	t.Setenv("MIKROVIEW_WATCHLIST_STORE_PATH", newWatchlistPath)
	t.Setenv("MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH", newSuggestionsPath)
	t.Setenv("MIKROVIEW_WATCHLIST_MATCH_LOG_PATH", newMatchLogPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	// The watchlist entry comes back -- read directly out of the restored
	// document, there being no store left to Open it through (#407).
	restoredWatchlist := readWatchlistDocument(t, newWatchlistPath)
	var got *watchlist.Entry
	for i := range restoredWatchlist.Entries {
		if restoredWatchlist.Entries[i].ID == "e1" {
			got = &restoredWatchlist.Entries[i]
			break
		}
	}
	if got == nil {
		t.Fatal("restored watchlist document has no entry \"e1\"")
	}
	if got.Name != "ssh-watch" || len(got.Ports) != 1 || got.Ports[0] != 22 {
		t.Errorf("restored watchlist entry = %+v, want Name=ssh-watch Ports=[22]", *got)
	}

	// The suggestion comes back.
	sg2, err := suggest.Open(newSuggestionsPath)
	if err != nil {
		t.Fatalf("suggest.Open after restore: %v", err)
	}
	gotCandidate, ok := sg2.Get("c1")
	if !ok {
		t.Fatal("restored suggestions store has no candidate \"c1\"")
	}
	if gotCandidate.Justification != candidate.Justification {
		t.Errorf("restored candidate Justification = %q, want %q", gotCandidate.Justification, candidate.Justification)
	}

	// Both match log records come back -- proving the JSON-lines wrapping
	// round-trips, not just that the file exists.
	ml2, err := matchlog.Open(newMatchLogPath, 100)
	if err != nil {
		t.Fatalf("matchlog.Open after restore: %v", err)
	}
	t.Cleanup(func() { ml2.Close() })
	var records []matchlog.Record
	if err := ml2.Query(context.Background(), matchlog.Query{Source: src}, func(r matchlog.Record) bool {
		records = append(records, r)
		return true
	}); err != nil {
		t.Fatalf("matchlog Query after restore: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("restored match log has %d record(s), want 2", len(records))
	}
}

func testMatchEvent() store.Event {
	return store.Event{Action: store.ActionAccept, Raw: "test"}
}
