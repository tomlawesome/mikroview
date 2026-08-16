// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestBackupRestoreRoundTripCarriesMigratedDefinitions is issue #404's
// end-to-end backup requirement: `-backup` on a *migrated* deployment,
// `-restore` onto a fresh one, same definitions set comes back --
// exercised through runBackup/runRestore (the real CLI entry points),
// mirroring TestBackupRestoreRoundTripCarriesWatchlist's own shape for
// #372.
//
// Setting up "a migrated deployment" (a detector settings store and a
// watchlist store with real content, converted once via
// engine.MigrateDefinitions) is precondition setup, not the thing under
// test -- what's actually asserted is that the definitions document that
// setup produced survives runBackup followed by runRestore into a fresh
// directory unchanged, which is the only part this test checks by
// re-opening a store rather than by comparing bytes.
func TestBackupRestoreRoundTripCarriesMigratedDefinitions(t *testing.T) {
	srcDir := t.TempDir()
	settingsPath := filepath.Join(srcDir, "detector-settings.json")
	watchlistPath := filepath.Join(srcDir, "watchlist.json")
	definitionsPath := filepath.Join(srcDir, "definitions.json")

	settings, err := detect.OpenSettingsStoreWithBackend(persist.NewFileBackend(settingsPath), detect.DefaultSettingsMap())
	if err != nil {
		t.Fatalf("detect.OpenSettingsStoreWithBackend: %v", err)
	}
	settings.Set(detect.DetectorPortScan, detect.Settings{Enabled: false})
	flushStore(t, settings)

	wl, err := watchlist.Open(watchlistPath)
	if err != nil {
		t.Fatalf("watchlist.Open: %v", err)
	}
	if err := wl.Upsert(watchlist.Entry{ID: "e1", Name: "ssh-watch", Ports: []int{22}}); err != nil {
		t.Fatalf("watchlist Upsert: %v", err)
	}
	flushStore(t, wl)

	migrated, err := engine.MigrateDefinitions(context.Background(),
		persist.NewFileBackend(definitionsPath),
		persist.NewFileBackend(settingsPath),
		persist.NewFileBackend(watchlistPath))
	if err != nil {
		t.Fatalf("MigrateDefinitions: %v", err)
	}
	if !migrated {
		t.Fatal("test setup: expected migration to run")
	}

	defsBefore, err := engine.OpenDefinitionsStore(definitionsPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore: %v", err)
	}
	wantList := defsBefore.List()
	if len(wantList) == 0 {
		t.Fatal("test setup: migrated definitions store is empty")
	}
	wl1, ok := defsBefore.Get("e1")
	if !ok || wl1.Definition.Name != "ssh-watch" {
		t.Fatalf("test setup: migrated watchlist entry missing or wrong: %+v, %v", wl1, ok)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH", settingsPath)
	t.Setenv("MIKROVIEW_WATCHLIST_STORE_PATH", watchlistPath)
	t.Setenv("MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH", definitionsPath)

	// --force: t.TempDir() is world-readable in some sandboxes -- same
	// reasoning backup_watchlist_roundtrip_test.go gives for the
	// identical flag.
	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatalf("backup.Read: %v", err)
	}
	if _, ok := env.Stores["definitions"]; !ok {
		t.Fatal("backup envelope is missing the definitions store")
	}

	// Restore into a fresh directory -- a different set of paths, the
	// same way a disaster recovery restores onto a new host. The other
	// two source stores are pointed at empty paths in the new directory
	// too, purely so restore's existing-file refusal doesn't trip on
	// them; this test's assertions are all about the definitions store.
	dstDir := t.TempDir()
	newDefinitionsPath := filepath.Join(dstDir, "definitions.json")
	t.Setenv("MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH", filepath.Join(dstDir, "detector-settings.json"))
	t.Setenv("MIKROVIEW_WATCHLIST_STORE_PATH", filepath.Join(dstDir, "watchlist.json"))
	t.Setenv("MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH", newDefinitionsPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	defsAfter, err := engine.OpenDefinitionsStore(newDefinitionsPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore after restore: %v", err)
	}
	gotList := defsAfter.List()
	if len(gotList) != len(wantList) {
		t.Fatalf("restored definitions set has %d entries, want %d", len(gotList), len(wantList))
	}
	wantByID := make(map[string]engine.StoredDefinition, len(wantList))
	for _, d := range wantList {
		wantByID[d.Definition.ID] = d
	}
	for _, got := range gotList {
		want, ok := wantByID[got.Definition.ID]
		if !ok {
			t.Errorf("restored definition %q was not part of the original migrated set", got.Definition.ID)
			continue
		}
		if got.Available != want.Available {
			t.Errorf("restored definition %q Available = %v, want %v", got.Definition.ID, got.Available, want.Available)
		}
		if got.Definition.Enabled != want.Definition.Enabled {
			t.Errorf("restored definition %q Enabled = %v, want %v", got.Definition.ID, got.Definition.Enabled, want.Definition.Enabled)
		}
		if got.Definition.Name != want.Definition.Name {
			t.Errorf("restored definition %q Name = %q, want %q", got.Definition.ID, got.Definition.Name, want.Definition.Name)
		}
	}

	// The disabled detector and the migrated watchlist entry
	// specifically -- not just the count.
	portScan, ok := defsAfter.Get(string(detect.DetectorPortScan))
	if !ok || portScan.Definition.Enabled {
		t.Errorf("restored port_scan = %+v, %v, want present and disabled", portScan, ok)
	}
	restoredEntry, ok := defsAfter.Get("e1")
	if !ok || restoredEntry.Definition.Name != "ssh-watch" {
		t.Errorf("restored watchlist-derived definition e1 = %+v, %v", restoredEntry, ok)
	}
}

// storeFlusher is the small common surface flushStore needs --
// detect.SettingsStore and watchlist.Store both satisfy it, the same way
// closeStoreOnShutdown's own interface{ Close(context.Context) error }
// parameter works in main.go.
type storeFlusher interface {
	Flush(context.Context) error
}

// flushStore forces a store's write-behind writer to persist now --
// #400 made every store's persistence write-behind, so a Set/Upsert
// returning is not the same as the change having reached disk yet, and
// this test reads those files back through a completely separate
// *DefinitionsStore/engine.MigrateDefinitions call immediately
// afterward. Same synchronous checkpoint
// backup_watchlist_roundtrip_test.go's own flush calls document.
func flushStore(t *testing.T, s storeFlusher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}
