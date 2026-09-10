// SPDX-License-Identifier: AGPL-3.0-only

package backupsftp

import (
	"os"
	"path/filepath"
	"strings"
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

// TestLoadOrGenerateHostKeyLeavesNoTempLitter covers #1082: the generated
// key is now written via persist.WriteFileAtomic's temp-then-rename dance
// instead of a direct os.WriteFile. After a successful first-run
// generation, no "*.tmp*" artifact from that dance should remain in
// storeDir.
func TestLoadOrGenerateHostKeyLeavesNoTempLitter(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrGenerateHostKey(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp artifact after host key generation: %s", e.Name())
		}
	}
}

// TestLoadOrGenerateHostKeyTruncatedPEMNamesThePath covers #1082's read
// side (lines 39-47): a host key file that exists but fails to parse --
// here, a truncated PEM, the shape a crash mid-write used to be able to
// leave behind before the write path became crash-safe -- must return an
// error naming the path, not a bare parser error, so whoever reads the
// log knows which file to look at.
func TestLoadOrGenerateHostKeyTruncatedPEMNamesThePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, hostKeyFileName)
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\ntruncated"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrGenerateHostKey(dir)
	if err == nil {
		t.Fatal("LoadOrGenerateHostKey with a truncated PEM = nil error, want one naming the path")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path %q", err.Error(), path)
	}
}
