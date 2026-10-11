// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for #1450, written from its specification before the fix: a sealed
// store file is bound to its store NAME within the data directory (the
// file's base name), not to its absolute path, so a data directory can move.
//
// API assumption: the store name is the file's base name, so the existing
// constructors (NewEncryptedFileBackend, NewEncryptedFileBackendForPath)
// are used unchanged. "Opens at its original path" for a file sealed the
// old way is expressed with NewEncryptedFileBackendForPath(real, logical):
// logical is the absolute path the old writer bound.

const legacyLogical = "/var/lib/mikroview/flags.json"

const legacyPayload = `{"flags":[{"ip":"203.0.113.7"}]}`

// testdata/sealed-abspath-flags.json was written by the pre-change writer
// (NewEncryptedFileBackendForPath with logical path legacyLogical, key
// testKey(0x07)): it is bound to the absolute path.
func legacyFixture(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "sealed-abspath-flags.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "flags.json")
	writePlain(t, p, body)
	return p
}

// #1450 spec 1: a sealed file copied to a new directory opens there.
func TestSealedFileCopiedToANewPathOpensThere(t *testing.T) {
	ctx := context.Background()
	a := filepath.Join(t.TempDir(), "accounts.json")
	if _, err := NewEncryptedFileBackend(a, testKey(t, 0x01)).Save(ctx, []byte(`{"users":[1]}`), 0); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(t.TempDir(), "elsewhere", "accounts.json")
	if err := os.MkdirAll(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	writePlain(t, b, readFileBytes(t, a))

	snap, err := NewEncryptedFileBackend(b, testKey(t, 0x01)).Load(ctx)
	if err != nil {
		t.Fatalf("a sealed file moved to a new directory did not open: %v", err)
	}
	if string(snap.Payload) != `{"users":[1]}` {
		t.Errorf("payload = %q", snap.Payload)
	}
}

// #1450 spec 2: a genuine sealed file of store A at store B's name is
// refused, and the message names the store (the one it was placed at).
func TestSealedFileAtAnotherStoresNameIsRefusedNamingTheStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tokens := filepath.Join(dir, "tokens.json")
	if _, err := NewEncryptedFileBackend(tokens, testKey(t, 0x01)).Save(ctx, []byte(`{"tokens":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	accounts := filepath.Join(dir, "accounts.json")
	writePlain(t, accounts, readFileBytes(t, tokens))

	_, err := NewEncryptedFileBackend(accounts, testKey(t, 0x01)).Load(ctx)
	if err == nil {
		t.Fatal("the tokens file at the accounts file's name opened")
	}
	if !strings.Contains(err.Error(), "accounts.json") {
		t.Errorf("the refusal does not name the store %q: %v", "accounts.json", err)
	}
	if errors.Is(err, ErrPlaintextDocument) {
		t.Errorf("a sealed file was reported as plaintext: %v", err)
	}
}

// #1450 spec 3a: a file sealed under its absolute path opens at that path.
func TestLegacyAbsolutePathFileOpensAtItsOriginalPath(t *testing.T) {
	p := legacyFixture(t, t.TempDir())
	snap, err := NewEncryptedFileBackendForPath(p, legacyLogical, testKey(t, 0x07)).Load(context.Background())
	if err != nil {
		t.Fatalf("a file sealed under its absolute path did not open at that path: %v", err)
	}
	if string(snap.Payload) != legacyPayload {
		t.Errorf("payload = %q", snap.Payload)
	}
}

// #1450 spec 3b: opening it at its original path re-saves it under the
// name binding, so afterwards it opens from a new path. (If the re-save
// were deferred to the next write, TestLegacyFileOpensFromANewPathAfterTheNextSave
// is the weaker form of the same point.)
func TestLegacyAbsolutePathFileIsResavedOnOpenSoItOpensFromANewPath(t *testing.T) {
	ctx := context.Background()
	p := legacyFixture(t, t.TempDir())
	before := readFileBytes(t, p)
	if _, err := NewEncryptedFileBackendForPath(p, legacyLogical, testKey(t, 0x07)).Load(ctx); err != nil {
		t.Fatalf("open at the original path: %v", err)
	}
	if bytes.Equal(before, readFileBytes(t, p)) {
		t.Error("the file was not re-saved under the name binding when opened at its original path")
	}

	moved := filepath.Join(t.TempDir(), "new", "flags.json")
	if err := os.MkdirAll(filepath.Dir(moved), 0o700); err != nil {
		t.Fatal(err)
	}
	writePlain(t, moved, readFileBytes(t, p))
	snap, err := NewEncryptedFileBackend(moved, testKey(t, 0x07)).Load(ctx)
	if err != nil {
		t.Fatalf("the re-saved file did not open from a new path: %v", err)
	}
	if string(snap.Payload) != legacyPayload {
		t.Errorf("payload after re-save = %q", snap.Payload)
	}
}

// #1450 spec 3b (weaker form): after an open at the original path and a
// following save, the file opens from a new path.
func TestLegacyFileOpensFromANewPathAfterTheNextSave(t *testing.T) {
	ctx := context.Background()
	p := legacyFixture(t, t.TempDir())
	b := NewEncryptedFileBackendForPath(p, legacyLogical, testKey(t, 0x07))
	snap, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("open at the original path: %v", err)
	}
	if _, err := b.Save(ctx, []byte(`{"flags":[]}`), snap.Version); err != nil {
		t.Fatalf("save after open: %v", err)
	}
	moved := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, moved, readFileBytes(t, p))
	if _, err := NewEncryptedFileBackend(moved, testKey(t, 0x07)).Load(ctx); err != nil {
		t.Fatalf("the saved file did not open from a new path: %v", err)
	}
}

// #1450 spec 3c: an old-format file at a path other than the one it was
// sealed at is refused as before, and left unchanged.
func TestLegacyAbsolutePathFileAtADifferentPathIsRefused(t *testing.T) {
	ctx := context.Background()
	p := legacyFixture(t, t.TempDir())
	before := readFileBytes(t, p)
	_, err := NewEncryptedFileBackend(p, testKey(t, 0x07)).Load(ctx)
	if err == nil {
		t.Fatal("an old-format file opened at a path it was not sealed at")
	}
	if _, err := NewEncryptedFileBackendForPath(p, "/srv/other/flags.json", testKey(t, 0x07)).Load(ctx); err == nil {
		t.Error("an old-format file opened under another absolute path")
	}
	if !bytes.Equal(before, readFileBytes(t, p)) {
		t.Error("a refused open changed the file")
	}
}

// #1450 spec 4: forged, edited and plaintext files are refused as today.
func TestForgedEditedAndPlaintextFilesAreStillRefused(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	good := filepath.Join(dir, "flags.json")
	if _, err := NewEncryptedFileBackend(good, testKey(t, 0x01)).Save(ctx, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	sealed := readFileBytes(t, good)

	edited := append([]byte(nil), sealed...)
	edited[len(edited)-1] ^= 0x01

	cases := map[string][]byte{
		"edited":    edited,
		"truncated": sealed[:len(sealed)-3],
		"garbage":   []byte("not a sealed document at all, just some bytes"),
	}
	for name, body := range cases {
		p := filepath.Join(t.TempDir(), "flags.json")
		writePlain(t, p, body)
		if _, err := NewEncryptedFileBackend(p, testKey(t, 0x01)).Load(ctx); err == nil {
			t.Errorf("%s file opened", name)
		}
	}

	plain := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, plain, []byte(`{"n":1}`))
	if _, err := NewEncryptedFileBackend(plain, testKey(t, 0x01)).Load(ctx); !errors.Is(err, ErrPlaintextDocument) {
		t.Errorf("plaintext: err = %v, want ErrPlaintextDocument", err)
	}

	// Sealed under another key, at the right name.
	if _, err := NewEncryptedFileBackend(good, testKey(t, 0x02)).Load(ctx); err == nil {
		t.Error("a file sealed under another key opened")
	}
}
