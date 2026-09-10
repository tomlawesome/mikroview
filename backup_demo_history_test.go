// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestRestoredSevenNightHistorySurvivesLiveFill is issue #959's "done when"
// test: a restored seven-night watchlist history must render its streaks
// once a fresh process opens it, without the honesty check
// (DefinitionsStore.watchingSince, definitions_nights.go) demoting any of
// those nights to "not observed".
//
// The risk this pins: FillWatchNights only ever treats a window that opened
// before its own process's watchingSince as honestly claimable if that
// window is already recorded (definitions_nights.go's watchObservationFor
// doc comment) -- a night arriving any other way would get downgraded the
// moment anything reads it. Restore is supposed to be the other, safe way:
// the nights already sit in the document FillWatchNights only ever appends
// *after*, so they are never re-examined by the honesty check at all. This
// test boots a second DefinitionsStore over the restored document (a fresh
// watchingSince, strictly after every restored night) and proves that
// holds -- not by construction, but by actually calling FillWatchNights and
// checking nothing changed.
//
// Building the envelope by hand here (rather than importing cmd/demo-history,
// a separate `main` package this one cannot import) mirrors what that
// command does: engine.ExpectationDefinitionFor is the same call it makes,
// and this test is what pins that call producing history a live process can
// render, independent of that command's own traffic-generation code.
func TestRestoredSevenNightHistorySurvivesLiveFill(t *testing.T) {
	now := time.Now().UTC()
	window := watchlist.Window{Start: 22 * 60, End: 6 * 60}

	occs := window.ClosedSince(time.Time{}, now, watchlist.MaxNights)
	if len(occs) < watchlist.MaxNights {
		t.Fatalf("test setup: only %d closed occurrences before %s, want %d -- this window's already-closed history "+
			"is too short right now for this test to mean anything", len(occs), now, watchlist.MaxNights)
	}

	var nights []watchlist.Night
	for _, o := range occs {
		nights = append(nights, watchlist.Night{Opened: o.Open, State: watchlist.NightKept, First: o.Open.Add(time.Hour), Count: 3})
	}
	ring := watchlist.UpdateRing(nights, window)
	if ring.Broken {
		t.Fatalf("test setup: all-kept nights produced a broken ring %+v", ring)
	}

	entry := watchlist.Entry{
		ID:        "demo-nas-shares-watch",
		Name:      "nas-shares-watch",
		Source:    matchlog.Identity{IP: "10.0.20.10"},
		Ports:     []int{445, 5001},
		Window:    window,
		Nights:    nights,
		Ring:      ring,
		CreatedAt: now.Add(-8 * 24 * time.Hour),
	}
	def, err := engine.ExpectationDefinitionFor(entry)
	if err != nil {
		t.Fatalf("ExpectationDefinitionFor: %v", err)
	}

	// Build the definitions store bytes the way cmd/demo-history does: a
	// scratch store, one Upsert, Flush, read the file back -- then hand
	// those bytes to backup.Write directly, since this test is pinning
	// runRestore's behavior and FillWatchNights' afterward, not runBackup's
	// (backup_definitions_roundtrip_test.go already covers that leg).
	srcDir := t.TempDir()
	scratchPath := filepath.Join(srcDir, "definitions.json")
	scratch, err := engine.OpenDefinitionsStore(scratchPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore (scratch): %v", err)
	}
	if err := scratch.Upsert(def); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := scratch.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	envelopePath := filepath.Join(srcDir, "demo-history.backup")
	ef, err := os.Create(envelopePath)
	if err != nil {
		t.Fatalf("creating envelope file: %v", err)
	}
	definitionsBytes, err := os.ReadFile(scratchPath)
	if err != nil {
		t.Fatalf("reading scratch definitions store: %v", err)
	}
	if err := backup.Write(ef, "test", map[string][]byte{"definitions": definitionsBytes}); err != nil {
		ef.Close()
		t.Fatalf("backup.Write: %v", err)
	}
	if err := ef.Close(); err != nil {
		t.Fatalf("closing envelope: %v", err)
	}

	// Restore into a fresh path -- a different process's store, the same
	// way a demo instance restores into its own fresh data directory.
	dstDir := t.TempDir()
	restoredPath := filepath.Join(dstDir, "definitions.json")
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH", restoredPath)
	if code := runRestore([]string{envelopePath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	// Open a *second*, independent DefinitionsStore over the restored
	// document -- this is the "fresh process boots" half: its
	// watchingSince is time.Now() at this line, strictly after every
	// restored night's Opened instant, exactly the situation
	// watchObservationFor's doc comment describes as unable to honestly
	// claim a window unless it was already recorded.
	live, err := engine.OpenDefinitionsStore(restoredPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore after restore: %v", err)
	}

	before, ok, err := live.GetExpectation(entry.ID)
	if err != nil || !ok {
		t.Fatalf("GetExpectation before fill: ok=%v err=%v", ok, err)
	}
	if got := watchlist.SummariseNights(before.Nights); got.Kept != watchlist.MaxNights {
		t.Fatalf("restored nights before any fill: %+v, want %d kept", got, watchlist.MaxNights)
	}

	// The lazy catch-up fill every read path triggers (definitions_nights.go),
	// run with coverage true -- the honest case, since a real boot only
	// calls this with coverage answered from live router state. If the
	// honesty check wrongly reached back into the restored nights, this is
	// where it would show up.
	live.FillWatchNights(time.Now(), map[string]bool{entry.ID: true})

	after, ok, err := live.GetExpectation(entry.ID)
	if err != nil || !ok {
		t.Fatalf("GetExpectation after fill: ok=%v err=%v", ok, err)
	}

	if len(after.Nights) < watchlist.MaxNights {
		t.Fatalf("fill dropped nights: got %d, want at least %d", len(after.Nights), watchlist.MaxNights)
	}
	// Every one of the seven restored nights must still be exactly what
	// was restored -- not re-derived, not downgraded to "not observed".
	restoredByOpen := make(map[time.Time]watchlist.Night, len(nights))
	for _, n := range nights {
		restoredByOpen[n.Opened] = n
	}
	seen := 0
	for _, n := range after.Nights {
		want, ok := restoredByOpen[n.Opened]
		if !ok {
			continue // a night FillWatchNights legitimately added going forward
		}
		seen++
		if n.State != want.State {
			t.Errorf("restored night opened %s: state = %q after a live fill, want unchanged %q -- "+
				"the honesty check reached back into restored history", n.Opened, n.State, want.State)
		}
	}
	if seen != len(nights) {
		t.Fatalf("only %d of the %d restored nights were found after the fill", seen, len(nights))
	}

	summary := watchlist.SummariseNights(after.Nights)
	if summary.Kept < watchlist.MaxNights {
		t.Errorf("summary after fill: %+v, want at least %d kept (the restored streak)", summary, watchlist.MaxNights)
	}
	if after.Ring.Broken {
		t.Errorf("ring after fill: %+v, want unbroken -- every restored night was kept", after.Ring)
	}
}

// TestRestoreWritesRetainedEventsThroughRetentionEncryption pins
// backup_cli.go's retainedEventsStore handling: a plain-JSON envelope store
// carrying back-dated events comes back on disk as real, encrypted
// internal/retention day files -- the same format a live process's own
// Store writes -- never as plaintext, and never through the generic
// per-store backend loop the rest of runRestore uses (retainedEventsStore
// is not one of backedUpStores' entries).
//
// This is the mechanism cmd/demo-history's envelope depends on: it writes
// exactly this store, in exactly this shape, and expects -restore to seal
// it under the target deployment's own key rather than requiring the
// generator to know that key in advance.
func TestRestoreWritesRetainedEventsThroughRetentionEncryption(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "history.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x42}, retention.MinKeyBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}

	const marker = "e2e-959-marker"
	day1 := time.Date(2026, 8, 20, 22, 30, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 21, 1, 15, 0, 0, time.UTC)
	records := []retainedEventRecord{
		{Event: store.Event{Action: store.ActionAccept, SrcIP: "10.0.20.10", DstPort: 445, Raw: marker}, ReceivedAt: day1},
		{Event: store.Event{Action: store.ActionAccept, SrcIP: "10.0.20.10", DstPort: 445, Raw: marker}, ReceivedAt: day2},
	}
	recordsJSON, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}

	srcDir := t.TempDir()
	envelopePath := filepath.Join(srcDir, "retained.backup")
	ef, err := os.Create(envelopePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Write(ef, "test", map[string][]byte{retainedEventsStore: recordsJSON}); err != nil {
		ef.Close()
		t.Fatalf("backup.Write: %v", err)
	}
	if err := ef.Close(); err != nil {
		t.Fatal(err)
	}

	historyDir := filepath.Join(dir, "history")
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HISTORY_KEY_FILE", keyPath)
	t.Setenv("MIKROVIEW_HISTORY_DIR", historyDir)

	if code := runRestore([]string{envelopePath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatalf("reading %s: %v", historyDir, err)
	}
	if len(entries) != 2 {
		t.Fatalf("history dir has %d file(s), want 2 (one per day the two events fall on)", len(entries))
	}
	for _, e := range entries {
		onDisk, err := os.ReadFile(filepath.Join(historyDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(onDisk, []byte(marker)) {
			t.Fatalf("%s is not encrypted -- the marker is readable on disk", e.Name())
		}
	}

	var replayed []store.Event
	w := retention.ReplayDir(historyDir, key, time.Time{}, func(e store.Event) { replayed = append(replayed, e) })
	if w.Err != nil {
		t.Fatalf("ReplayDir: %v", w.Err)
	}
	if len(replayed) != 2 {
		t.Fatalf("replayed %d event(s), want 2", len(replayed))
	}
	for _, e := range replayed {
		if e.Raw != marker {
			t.Errorf("replayed event Raw = %q, want %q", e.Raw, marker)
		}
	}

	// Re-running without --force must refuse now that the directory holds
	// what the first restore wrote -- the same "already exists" guard
	// every other store gets, extended to this one (backup_cli.go).
	if code := runRestore([]string{envelopePath}); code == 0 {
		t.Fatal("second runRestore without --force = 0, want a refusal: the history dir already holds retained events")
	}
}
