// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestBackupRestoreRoundTripCarriesDefinitions is issue #404's end-to-end
// backup requirement: `-backup` on a deployment with a populated
// definitions store, `-restore` onto a fresh one, same definitions set
// comes back -- exercised through runBackup/runRestore (the real CLI
// entry points), mirroring TestBackupRestoreRoundTripCarriesWatchlist's
// own shape for #372.
//
// Populating the store (the shipped catalogue with one detector switched
// off, plus one watchlist expectation) is precondition setup, not the
// thing under test -- what's actually asserted is that the document that
// setup produced survives runBackup followed by runRestore into a fresh
// directory unchanged, which is the only part this test checks by
// re-opening a store rather than by comparing bytes.
func TestBackupRestoreRoundTripCarriesDefinitions(t *testing.T) {
	srcDir := t.TempDir()
	definitionsPath := filepath.Join(srcDir, "definitions.json")

	defsBefore, err := engine.OpenDefinitionsStore(definitionsPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore: %v", err)
	}
	// The shipped catalogue with port_scan switched off, exactly as a boot
	// seeds it from config.yaml's flags.detectors.
	seed := engine.DefaultDetectorSettings()
	seed["port_scan"] = engine.DetectorSettings{Enabled: false}
	if err := engine.SeedShippedDefinitions(defsBefore, seed, engine.DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	// One operator-authored expectation alongside them.
	expectation, err := engine.ExpectationDefinitionFor(watchlist.Entry{ID: "e1", Name: "ssh-watch", Ports: []int{22}})
	if err != nil {
		t.Fatalf("ExpectationDefinitionFor: %v", err)
	}
	if err := defsBefore.Upsert(expectation); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// The definitions store persists write-behind, so the bytes runBackup
	// reads below only exist once this returns.
	if err := defsBefore.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	wantList := defsBefore.List()
	if len(wantList) == 0 {
		t.Fatal("test setup: definitions store is empty")
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
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
	// same way a disaster recovery restores onto a new host.
	dstDir := t.TempDir()
	newDefinitionsPath := filepath.Join(dstDir, "definitions.json")
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

	// The disabled detector and the expectation specifically -- not just
	// the count.
	portScan, ok := defsAfter.Get("port_scan")
	if !ok || portScan.Definition.Enabled {
		t.Errorf("restored port_scan = %+v, %v, want present and disabled", portScan, ok)
	}
	restoredEntry, ok := defsAfter.Get("e1")
	if !ok || restoredEntry.Definition.Name != "ssh-watch" {
		t.Errorf("restored expectation definition e1 = %+v, %v", restoredEntry, ok)
	}
}
