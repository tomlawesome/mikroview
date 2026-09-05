// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// TestBackupRestoreRoundTripCarriesAnEncryptedStore is #853's own
// end-to-end requirement: -backup on a deployment with history.keyFile
// configured produces a plain, readable envelope (it runs with the key
// available, so there is no reason to ship ciphertext inside ciphertext),
// and -restore writes the store back encrypted, exactly as the running
// server would have.
//
// The store is seeded directly through persist.EncryptedFileBackend
// rather than through a real store package -- backup_cli.go treats every
// store as opaque bytes (see persist.LoadDocument/Save), so this is
// exactly what a flags/entities/etc document sealed under a key looks
// like on disk, without needing that package's own shape.
func TestBackupRestoreRoundTripCarriesAnEncryptedStore(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "history.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x5a}, retention.MinKeyBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}

	flagsPath := filepath.Join(dir, "flags.json")
	const marker = `{"flags":[{"ip":"203.0.113.9","reason":"e2e-853-marker"}]}`
	if _, err := persist.NewEncryptedFileBackend(flagsPath, key).Save(context.Background(), []byte(marker), 0); err != nil {
		t.Fatalf("seeding the encrypted flags store: %v", err)
	}

	// Sanity: what's actually on disk is not the marker text.
	onDisk, err := os.ReadFile(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte("e2e-853-marker")) {
		t.Fatal("test setup: the seeded flags file is not actually encrypted")
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HISTORY_KEY_FILE", keyPath)
	t.Setenv("MIKROVIEW_FLAGS_STORE_PATH", flagsPath)

	backupPath := filepath.Join(dir, "mikroview.backup")
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
	raw, ok := env.Stores["flags"]
	if !ok {
		t.Fatal("backup envelope is missing the flags store")
	}
	// The whole point: the envelope carries plain, readable JSON, not the
	// ciphertext that is actually on disk. It runs with the key available,
	// so there is no reason for a backup file to itself be undecipherable
	// without the same key mounted wherever it is later opened.
	if !bytes.Contains(raw, []byte("e2e-853-marker")) {
		t.Fatalf("backup envelope for \"flags\" is not readable JSON: %s", raw)
	}

	// Restore into a fresh directory, same key -- a disaster recovery
	// restoring onto a new host that mounts the same key file.
	dstDir := t.TempDir()
	newFlagsPath := filepath.Join(dstDir, "flags.json")
	t.Setenv("MIKROVIEW_FLAGS_STORE_PATH", newFlagsPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	// What restore actually wrote is ciphertext again, not the plain
	// bytes from the envelope -- #853 has no plaintext-on-disk mode.
	restoredOnDisk, err := os.ReadFile(newFlagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(restoredOnDisk, []byte("e2e-853-marker")) {
		t.Fatal("the restored flags file is plaintext on disk -- #853 requires it stay encrypted")
	}

	snap, err := persist.NewEncryptedFileBackend(newFlagsPath, key).Load(context.Background())
	if err != nil {
		t.Fatalf("Load after restore: %v", err)
	}
	// backup.Write's own envelope encoding re-indents the nested raw JSON
	// (whitespace only, same as TestRestoreOverwritesACorruptStoreDocument
	// notes for the accounts store), so compare the compacted shape.
	var got, want bytes.Buffer
	if err := json.Compact(&got, snap.Payload); err != nil {
		t.Fatalf("restored payload is not valid JSON: %v (%q)", err, snap.Payload)
	}
	if err := json.Compact(&want, []byte(marker)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("restored payload = %s, want %s", got.String(), want.String())
	}
}
