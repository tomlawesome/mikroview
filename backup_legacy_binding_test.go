// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// #1450 S2: -backup never writes to the data directory. Backing up an
// install whose files are still sealed the old way (bound to their absolute
// path) succeeds, carries the content, and leaves every store file byte for
// byte as it was. (Re-binding is the server's job at its next start.)
func TestBackupOfOldFormatFilesLeavesThemByteForByteUnchanged(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "history.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x5a}, retention.MinKeyBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Sealed as every release before #1450 sealed: the absolute path of
	// the file is the associated data. Nothing in the product writes this
	// format any more.
	flagsPath := filepath.Join(dir, "flags.json")
	const marker = `{"flags":[{"ip":"203.0.113.9","reason":"e2e-1450-marker"}]}`
	sealed, err := key.Seal(retention.StateStoreKeyInfo, []byte(flagsPath), []byte(marker))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flagsPath, sealed, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HISTORY_KEY_FILE", keyPath)
	t.Setenv("MIKROVIEW_FLAGS_STORE_PATH", flagsPath)

	backupPath := filepath.Join(dir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0 for a data directory still in the old format", code)
	}

	after, err := os.ReadFile(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sealed, after) {
		t.Error("-backup rewrote a store file; it must never write to the data directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch e.Name() {
		case "history.key", "flags.json", "mikroview.backup":
		default:
			t.Errorf("-backup left an unexpected file in the data directory: %s", e.Name())
		}
	}

	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatalf("backup.Read: %v", err)
	}
	if !bytes.Contains(env.Stores["flags"], []byte("e2e-1450-marker")) {
		t.Errorf("the backup does not carry the old-format flags store: %s", env.Stores["flags"])
	}
}
