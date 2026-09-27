// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/configsnap"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// The example's header has to name the release this tree is and the
// schema this build reads -- the one look at a file that says which
// settings it should have is worthless if the example itself lies. A
// release that bumps VERSION, or a key change that bumps
// config.CurrentSchema, fails here until the example's header follows.
func TestExampleConfigHeaderMatchesThisBuild(t *testing.T) {
	h, ok := config.ParseHeader(string(exampleConfigYAML))
	if !ok {
		t.Fatal("deploy/config.example.yaml has no header")
	}
	if want := "v" + strings.TrimSpace(releaseFile); h.WrittenBy != want {
		t.Errorf("example header says written-by %s, VERSION says %s", h.WrittenBy, want)
	}
	if h.Schema != config.CurrentSchema {
		t.Errorf("example header says schema %d, this build reads %d", h.Schema, config.CurrentSchema)
	}
}

func TestRunningRelease(t *testing.T) {
	release := strings.TrimSpace(releaseFile)
	for in, want := range map[string]string{
		"v0.6.1":          "0.6.1",
		"dev:abc1234":     release + "-dev.abc1234",
		"preview:abc1234": release + "-preview.abc1234",
		"dev:local":       release + "-dev.local",
	} {
		if got := runningRelease(in); got != want {
			t.Errorf("runningRelease(%q) = %q, want %q", in, got, want)
		}
	}
}

// The config editor's snapshots ride -backup/-restore like every other
// store (#1347): sealed on disk, plain in the envelope, sealed again
// where they are restored.
func TestBackupRestoreRoundTripCarriesConfigSnapshots(t *testing.T) {
	srcDir := t.TempDir()
	keyPath := filepath.Join(srcDir, "history.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x6b}, retention.MinKeyBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(srcDir, "users.json")
	snapPath := filepath.Join(srcDir, "config-snapshots.json")
	cfg := config.Config{}
	cfg.Auth.StorePath = authPath
	if got := configSnapshotsPath(cfg); got != snapPath {
		t.Fatalf("configSnapshotsPath = %s, want %s beside the accounts store", got, snapPath)
	}
	store, err := configsnap.Open(context.Background(), persist.NewEncryptedFileBackend(snapPath, key))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "snapshot-roundtrip-1347"
	if _, err := store.Add(configsnap.Snapshot{Text: "# " + marker + "\n"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HISTORY_KEY_FILE", keyPath)
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}
	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := env.Stores["config_snapshots"]; !ok || !bytes.Contains(raw, []byte(marker)) {
		t.Fatalf("the backup does not carry the config snapshots readably: %q", raw)
	}

	dstDir := t.TempDir()
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(dstDir, "users.json"))
	if code := runRestore([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}
	restoredPath := filepath.Join(dstDir, "config-snapshots.json")
	onDisk, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte(marker)) {
		t.Fatal("the restored snapshots are in the clear on disk")
	}
	restored, err := configsnap.Open(context.Background(), persist.NewEncryptedFileBackend(restoredPath, key))
	if err != nil {
		t.Fatal(err)
	}
	newest, ok := restored.Newest()
	if !ok || !strings.Contains(newest.Text, marker) {
		t.Errorf("restored snapshot = %+v, %v", newest, ok)
	}
}
