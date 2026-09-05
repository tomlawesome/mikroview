// SPDX-License-Identifier: AGPL-3.0-only

package backupsftp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateHostKeyPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()

	k1, err := LoadOrGenerateHostKey(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	k2, err := LoadOrGenerateHostKey(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(k1.PublicKey().Marshal()) != string(k2.PublicKey().Marshal()) {
		t.Fatal("LoadOrGenerateHostKey generated a different key on the second call -- it should have loaded the persisted one")
	}
}

func TestLoadOrGenerateHostKeyIsPrivate(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrGenerateHostKey(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, hostKeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("host key file mode is %v, group/world readable", info.Mode().Perm())
	}
}
