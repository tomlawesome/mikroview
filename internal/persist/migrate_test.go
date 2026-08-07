// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptFileCopiesIntoAnEmptyStore(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "users.json")
	body := []byte(`{"disabled":false,"users":[{"id":"1"}]}`)
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))

	res, err := AdoptFile(context.Background(), src, dst)
	if err != nil {
		t.Fatalf("AdoptFile: %v", err)
	}
	if !res.Migrated {
		t.Error("expected Migrated")
	}
	snap, _ := dst.Load(context.Background())
	if string(snap.Payload) != string(body) {
		t.Errorf("payload = %q, want byte-identical to the source", snap.Payload)
	}
	// The source must still be there: reverting is "remove the config
	// and restart", which needs the file.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the source file was removed: %v", err)
	}
}

// The rule that stops a stale file on disk rolling live data back on
// every restart.
func TestAdoptFileNeverOverwritesAnExistingStore(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "users.json")
	if err := os.WriteFile(src, []byte(`{"stale":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))
	if _, err := dst.Save(context.Background(), []byte(`{"live":true}`), 0); err != nil {
		t.Fatal(err)
	}

	res, err := AdoptFile(context.Background(), src, dst)
	if err != nil {
		t.Fatalf("AdoptFile: %v", err)
	}
	if res.Migrated {
		t.Error("migrated over a store that already had data")
	}
	snap, _ := dst.Load(context.Background())
	if string(snap.Payload) != `{"live":true}` {
		t.Errorf("live data was overwritten: %q", snap.Payload)
	}
}

// An empty-but-existing document counts as "has data" -- a deliberately
// emptied store must not be repopulated from an old file.
func TestAdoptFileTreatsAnEmptyDocumentAsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "users.json")
	if err := os.WriteFile(src, []byte(`{"users":[{"id":"1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))
	if _, err := dst.Save(context.Background(), []byte(``), 0); err != nil {
		t.Fatal(err)
	}

	res, err := AdoptFile(context.Background(), src, dst)
	if err != nil {
		t.Fatalf("AdoptFile: %v", err)
	}
	if res.Migrated {
		t.Error("repopulated a deliberately empty store from an old file")
	}
}

func TestAdoptFileWithNoFileIsAFreshInstall(t *testing.T) {
	dir := t.TempDir()
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))
	res, err := AdoptFile(context.Background(), filepath.Join(dir, "absent.json"), dst)
	if err != nil {
		t.Fatalf("AdoptFile: %v", err)
	}
	if res.Migrated {
		t.Error("claimed to migrate a file that does not exist")
	}
}
