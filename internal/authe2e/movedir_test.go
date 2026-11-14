// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
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

// #1450 S2, end to end: a keyed install whose files are all still sealed the
// old way (bound to their absolute paths) is backed up with -backup, which
// succeeds and leaves every store file byte for byte as it was; a later
// server start then reads and re-binds them, and everything still works.
func TestBackupOfAnOldFormatInstallWritesNothingAndTheNextStartRebinds(t *testing.T) {
	ri := newRichInstall(t)
	key, err := retention.LoadKey(ri.w.a.key)
	must(t, err)

	// Put every sealed file back into the pre-#1450 format.
	old := map[string][]byte{}
	for _, name := range ri.files {
		path := ri.w.a.file(name)
		sealed, err := key.Seal(retention.StateStoreKeyInfo, []byte(path), ri.plain[name])
		must(t, err)
		must(t, os.WriteFile(path, sealed, 0o600))
		old[name] = sealed
	}

	// A stopped server leaves a match log that -backup cannot read as sealed
	// ("not a sealed document (too short)") whatever the binding -- #1467,
	// a separate matter from #1450 -- so it is cleared to keep this test about
	// the store files.
	must(t, os.Remove(ri.w.a.file("matchlog.jsonl")))

	out := filepath.Join(ri.w.a.root, "out.backup")
	code, text, _ := cli(t, Opts{Root: ri.w.a.root}, "", "-backup", out, "--force")
	if code != 0 {
		t.Fatalf("-backup of an old-format install exit %d:\n%s", code, text)
	}
	for _, name := range ri.files {
		if got := readBytes(t, ri.w.a.file(name)); !bytes.Equal(got, old[name]) {
			t.Errorf("-backup rewrote %s; it must never write to the data directory", name)
		}
	}

	a, err := start(t, Opts{Root: ri.w.a.root})
	if err != nil {
		t.Fatalf("start after -backup on an old-format install: %v", err)
	}
	ri.checkWorks(t, a)
	a.stop()
	for _, name := range []string{"users.json", "tokens.json", "droplist.json"} {
		if bytes.Equal(readBytes(t, ri.w.a.file(name)), old[name]) {
			t.Errorf("%s was not re-bound to its store name by the start", name)
		}
	}
}
