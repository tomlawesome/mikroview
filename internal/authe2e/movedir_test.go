// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// #1450: a keyed install's data directory moved to a new path, with the
// config pointing there, starts and reads every sealed store. The sealed
// files are bound to their store name within the directory, not to the
// absolute path they were written at.
//
// Written from the specification before the fix. The new instance is a
// fresh root: the data directory and the key are copied across, and the
// harness writes a config naming the new data directory.
func TestMovedDataDirectoryStartsAndKeepsAccountsTokensAndDropList(t *testing.T) {
	ri := newRichInstall(t)
	oldRoot := ri.w.a.root
	oldData := ri.w.a.data

	newRoot := t.TempDir()
	newData := filepath.Join(newRoot, "moved", "data")
	must(t, filepath.WalkDir(oldData, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(oldData, p)
		dst := filepath.Join(newData, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	}))
	// The harness lays the data directory out at <root>/data; move it
	// there, so the config the harness writes points at the new path.
	must(t, os.Rename(newData, filepath.Join(newRoot, "data")))
	must(t, os.MkdirAll(filepath.Join(newRoot, "keys"), 0o700))
	key, err := os.ReadFile(ri.w.a.key)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(newRoot, "keys", "history.key"), key, 0o600))
	if newRoot == oldRoot {
		t.Fatal("test setup: the data directory did not move")
	}

	a, err := start(t, Opts{Root: newRoot})
	if err != nil {
		t.Fatalf("an install whose data directory moved to a new path did not start: %v", err)
	}
	ri.checkWorks(t, a)
}
