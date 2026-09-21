// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/droplist"
)

// TestBackupRestoreRoundTripCarriesDroplist is issue #1223's version of
// #372's guard: an operator's drop list entries surviving `-backup`
// followed by `-restore` into a fresh directory, end to end through the
// real store rather than through backedUpStores' (Name, Path) pairs
// alone. Modelled directly on
// TestBackupRestoreRoundTripCarriesWatchlist -- see that test for why
// this shape (populate a real store, back up, unpack the envelope
// directly, restore into a different directory, reopen and check) is
// what actually catches a store missing from backedUpStores, rather than
// just exercising Store.Add/Remove in isolation.
func TestBackupRestoreRoundTripCarriesDroplist(t *testing.T) {
	srcDir := t.TempDir()
	droplistPath := filepath.Join(srcDir, "droplist.json")

	bs, err := droplist.Open(droplistPath)
	if err != nil {
		t.Fatalf("droplist.Open: %v", err)
	}
	if _, _, err := bs.Add("admin", "203.0.114.0/24", "scanning our SSH port", ""); err != nil {
		t.Fatalf("droplist Add: %v", err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_DROPLIST_STORE_PATH", droplistPath)

	// --force: t.TempDir() is world-readable in some sandboxes, which
	// would otherwise trip writeBackup's own world-readable-directory
	// refusal -- unrelated to what this test is pinning, same reason
	// backup_watchlist_roundtrip_test.go's own test writes straight
	// through writeBackup(force=true) rather than runBackup.
	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	// Unpack the envelope directly and assert the store is present --
	// the part that silently fails if "droplist" is ever dropped from
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
	if _, ok := env.Stores["droplist"]; !ok {
		t.Error("backup envelope is missing store \"droplist\"")
	}

	// Restore into a fresh directory -- a different set of paths, the
	// same way a disaster recovery restores onto a new host.
	dstDir := t.TempDir()
	newDroplistPath := filepath.Join(dstDir, "droplist.json")
	// #1293: the restore marker lives beside the data directory
	// (derived from Auth.StorePath), which must be writable even for a
	// restore that never touches the auth store itself.
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(dstDir, "users.json"))
	t.Setenv("MIKROVIEW_DROPLIST_STORE_PATH", newDroplistPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	bs2, err := droplist.Open(newDroplistPath)
	if err != nil {
		t.Fatalf("droplist.Open after restore: %v", err)
	}
	got := bs2.List()
	if len(got) != 1 {
		t.Fatalf("restored droplist store has %d entries, want 1: %+v", len(got), got)
	}
	if got[0].CIDR.String() != "203.0.114.0/24" || got[0].Reason != "scanning our SSH port" {
		t.Errorf("restored entry = %+v, want the one added before backup", got[0])
	}
}
