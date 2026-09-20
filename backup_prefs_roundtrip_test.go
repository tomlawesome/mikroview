// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/prefs"
)

// TestBackupRestoreRoundTripCarriesPrefs is #1283's version of #372's
// guard: a user's server-held preferences surviving `-backup` followed
// by `-restore` into a fresh directory, end to end through the real
// store rather than through backedUpStores' (Name, Path) pairs alone.
// Modelled directly on TestBackupRestoreRoundTripCarriesDroplist -- see
// that test for why this shape (populate a real store, back up, unpack
// the envelope directly, restore into a different directory, reopen and
// check) is what actually catches a store missing from backedUpStores,
// rather than just exercising Store.Put/Get in isolation.
func TestBackupRestoreRoundTripCarriesPrefs(t *testing.T) {
	srcDir := t.TempDir()
	prefsPath := filepath.Join(srcDir, "preferences.json")

	ps, err := prefs.Open(prefsPath)
	if err != nil {
		t.Fatalf("prefs.Open: %v", err)
	}
	if err := ps.Put("user-1", json.RawMessage(`{"colorway":"teal"}`)); err != nil {
		t.Fatalf("prefs Put: %v", err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_PREFS_STORE_PATH", prefsPath)

	// --force: t.TempDir() is world-readable in some sandboxes, which
	// would otherwise trip writeBackup's own world-readable-directory
	// refusal -- unrelated to what this test is pinning, same reason
	// backup_droplist_roundtrip_test.go's own test writes straight
	// through writeBackup(force=true) rather than runBackup.
	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	// Unpack the envelope directly and assert the store is present --
	// the part that silently fails if "prefs" is ever dropped from
	// backedUpStores the way Watchlist's three fields were in #372.
	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatalf("backup.Read: %v", err)
	}
	if _, ok := env.Stores["prefs"]; !ok {
		t.Error("backup envelope is missing store \"prefs\"")
	}

	// Restore into a fresh directory -- a different set of paths, the
	// same way a disaster recovery restores onto a new host.
	dstDir := t.TempDir()
	newPrefsPath := filepath.Join(dstDir, "preferences.json")
	// #1293: the restore marker lives beside the data directory
	// (derived from Auth.StorePath), which must be writable even for a
	// restore that never touches the auth store itself.
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(dstDir, "users.json"))
	t.Setenv("MIKROVIEW_PREFS_STORE_PATH", newPrefsPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	ps2, err := prefs.Open(newPrefsPath)
	if err != nil {
		t.Fatalf("prefs.Open after restore: %v", err)
	}
	got, ok := ps2.Get("user-1")
	if !ok {
		t.Fatal("restored preferences store has no record for user-1")
	}
	// Compared by decoded value, not exact bytes: the backup envelope
	// (internal/backup.Write) pretty-prints the whole document for
	// readability, which re-indents this store's opaque json.RawMessage
	// values along with everything else -- a whitespace difference, not
	// a lost or changed preference.
	var gotValue, wantValue map[string]any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("restored record is not valid JSON: %v (%s)", err, got)
	}
	json.Unmarshal([]byte(`{"colorway":"teal"}`), &wantValue)
	if gotValue["colorway"] != wantValue["colorway"] {
		t.Errorf("restored record = %s, want the one added before backup", got)
	}
}
