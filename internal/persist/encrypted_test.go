// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

func testKey(t *testing.T, seed byte) *retention.Key {
	t.Helper()
	material := bytes.Repeat([]byte{seed}, retention.MinKeyBytes)
	key, err := retention.NewKeyFromMaterial(material)
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	return key
}

// The whole point of #853: what lands on disk must not contain the
// plaintext, or any obviously-JSON fragment of it, even though the store
// above believes it wrote plain JSON.
func TestEncryptedFileBackendWritesNoPlaintextToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	b := NewEncryptedFileBackend(path, testKey(t, 0x01))

	secret := `{"flags":[{"ip":"203.0.113.7","reason":"a very specific marker string"}]}`
	if _, err := b.Save(context.Background(), []byte(secret), 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"203.0.113.7", "a very specific marker string", "flags"} {
		if bytes.Contains(onDisk, []byte(marker)) {
			t.Errorf("on-disk bytes contain plaintext marker %q -- the file is not actually encrypted", marker)
		}
	}

	// And round-trips back through the same key.
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(snap.Payload) != secret {
		t.Errorf("Load = %q, want %q", snap.Payload, secret)
	}
}

func TestEncryptedFileBackendWrongKeyFailsCleanly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	writer := NewEncryptedFileBackend(path, testKey(t, 0x01))
	if _, err := writer.Save(context.Background(), []byte(`{"n":1}`), 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reader := NewEncryptedFileBackend(path, testKey(t, 0x02))
	if _, err := reader.Load(context.Background()); err == nil {
		t.Fatal("Load with the wrong key succeeded, want a clean failure")
	}
}

// A file that was never encrypted at all -- a legacy plaintext document,
// or one written by something else -- must fail the same clean way as a
// wrong key, never be silently treated as empty or as valid ciphertext.
func TestEncryptedFileBackendRefusesPlaintextOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	if err := os.WriteFile(path, []byte(`{"n":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b := NewEncryptedFileBackend(path, testKey(t, 0x01))
	if _, err := b.Load(context.Background()); err == nil {
		t.Fatal("Load of a plaintext file succeeded, want a clean failure -- #853 has no migration path")
	}
}

// Version must be readable without the document decrypting -- the whole
// reason it exists is to support a compare-and-swap overwrite (runRestore
// in backup_cli.go) of a document that cannot be decrypted, e.g. because
// it predates the key or was sealed under a different one.
func TestEncryptedFileBackendVersionWorksWithoutDecrypting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	if err := os.WriteFile(path, []byte("not a sealed document"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := NewEncryptedFileBackend(path, testKey(t, 0x01))

	version, exists, err := b.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if !exists {
		t.Error("Version reports exists=false for a file that is present")
	}
	if version == 0 {
		t.Error("Version reports 0 for a file that exists")
	}

	if _, err := b.Save(context.Background(), []byte(`{"n":1}`), version); err != nil {
		t.Fatalf("Save with the version Version reported: %v", err)
	}
}

func TestEncryptedFileBackendDescribeNeverLeaksAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	b := NewEncryptedFileBackend(path, testKey(t, 0x01))
	if got := b.Describe(); !strings.Contains(got, path) {
		t.Errorf("Describe() = %q, want it to name the path", got)
	}
}
