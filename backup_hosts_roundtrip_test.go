// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/hosts"
)

// TestBackupRestoreRoundTripCarriesHosts proves the host presence
// register (#1016) travels in the backup envelope, end to end through
// the real store and document rather than through backedUpStores'
// (Name, Path) pairs alone.
//
// TestBackupCoversAllConfigPathFields next door already fails the build
// if Hosts.StorePath is absent from both backedUpStores and
// excludedFromBackup, so the *decision* cannot drift. This is the other
// half, in the shape of backup_watchlist_roundtrip_test.go: that the
// decision actually works, and specifically that an operator's marks --
// the one thing here the feed cannot rebuild -- come back after a
// restore onto fresh paths.
func TestBackupRestoreRoundTripCarriesHosts(t *testing.T) {
	srcDir := t.TempDir()
	hostsPath := filepath.Join(srcDir, "hosts.json")

	r, err := hosts.Open(hostsPath)
	if err != nil {
		t.Fatalf("hosts.Open: %v", err)
	}
	seen := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r.Observe("bridge-lan", "10.0.10.5", "lab-nas", seen)
	r.Observe("bridge-lan", "10.0.10.9", "", seen)
	if _, err := r.Mark("bridge-lan|10.0.10.5", hosts.MarkIntended, "the lab NAS, powered on twice a month", "admin"); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HOSTS_STORE_PATH", hostsPath)

	// --force: t.TempDir() is world-readable in some sandboxes, which
	// would otherwise trip writeBackup's own world-readable-directory
	// refusal -- unrelated to what this test is pinning, same reason
	// backup_watchlist_roundtrip_test.go passes it.
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
	if _, ok := env.Stores["hosts"]; !ok {
		t.Fatal("backup envelope is missing store \"hosts\"")
	}

	// Restore onto a different path, the way a disaster recovery
	// restores onto a new host.
	dstDir := t.TempDir()
	newHostsPath := filepath.Join(dstDir, "hosts.json")
	t.Setenv("MIKROVIEW_HOSTS_STORE_PATH", newHostsPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	r2, err := hosts.Open(newHostsPath)
	if err != nil {
		t.Fatalf("hosts.Open after restore: %v", err)
	}
	if n := len(r2.List()); n != 2 {
		t.Errorf("restored register holds %d host(s), want 2", n)
	}
	h, ok := r2.Get("bridge-lan|10.0.10.5")
	if !ok {
		t.Fatal("restored register has no host \"bridge-lan|10.0.10.5\"")
	}
	if h.Label != "lab-nas" || !h.LastSeen.Equal(seen) {
		t.Errorf("restored host = %+v, want the observation intact", h)
	}
	if h.Mark == nil || h.Mark.Kind != hosts.MarkIntended {
		t.Fatalf("restored mark = %+v, want the intended mark carried through", h.Mark)
	}
	if h.Mark.Reason != "the lab NAS, powered on twice a month" || h.Mark.By != "admin" {
		t.Errorf("restored mark = %+v, want the reason and actor unchanged -- the mark is the one thing here the feed cannot rebuild", h.Mark)
	}
}
