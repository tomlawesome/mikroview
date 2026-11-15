// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// #1468: a file store's sidecar lock file is not data and must not be
// migrated; a *.lock with no store beside it is the operator's and must be.

func TestMigrationLeavesAStoreSidecarLockBehind(t *testing.T) {
	src, cfg := seedDataDir(t)
	if err := os.WriteFile(filepath.Join(src, "accounts.json"), []byte(`{"accounts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "accounts.json.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "new-data")

	migrate(t, cfg, dest, false)

	if got := mustRead(t, filepath.Join(dest, "accounts.json")); got != `{"accounts":[]}` {
		t.Errorf("accounts.json at destination = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "accounts.json.lock")); !os.IsNotExist(err) {
		t.Errorf("accounts.json.lock must not be migrated; stat err = %v", err)
	}
}

func TestMigrationMovesALockFileWithNoStoreBesideIt(t *testing.T) {
	src, cfg := seedDataDir(t)
	if err := os.WriteFile(filepath.Join(src, "notes.lock"), []byte("operator's own file"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "new-data")

	_, digests := migrate(t, cfg, dest, false) // migrate also verifies

	if got := mustRead(t, filepath.Join(dest, "notes.lock")); got != "operator's own file" {
		t.Errorf("notes.lock at destination = %q", got)
	}
	if _, ok := digests["notes.lock"]; !ok {
		t.Errorf("notes.lock missing from the copied digests: %v", digests)
	}
}
