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

	"github.com/tomlawesome/mikroview/internal/retention"
)

// Tests for #1450, written from its specification before the fix: a sealed
// store file is bound to the STORE NAME the caller passes ("flags",
// "accounts", "tokens", ...), not to its absolute path or base name, so a
// data directory can move and two stores sharing a base name stay apart.
//
// API assumptions (these do not compile until the product has them):
//   - NewEncryptedFileBackend(path, storeName string, key *retention.Key)
//   - NewEncryptedFileBackendForPath(realPath, logicalPath, storeName string,
//     key *retention.Key): the legacy route; logicalPath is the absolute
//     path an old-format file was sealed at.

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

// #1450 spec 1: a sealed file copied to a new directory, even under a
// different file name, opens there under the same store name.
func TestSealedFileCopiedToANewPathOpensUnderTheSameStoreName(t *testing.T) {
	ctx := context.Background()
	a := filepath.Join(t.TempDir(), "accounts.json")
	if _, err := NewEncryptedFileBackend(a, "accounts", testKey(t, 0x01)).Save(ctx, []byte(`{"users":[1]}`), 0); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(t.TempDir(), "elsewhere", "renamed-users.json")
	if err := os.MkdirAll(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	writePlain(t, b, readFileBytes(t, a))

	snap, err := NewEncryptedFileBackend(b, "accounts", testKey(t, 0x01)).Load(ctx)
	if err != nil {
		t.Fatalf("a sealed file moved to a new path and name did not open: %v", err)
	}
	if string(snap.Payload) != `{"users":[1]}` {
		t.Errorf("payload = %q", snap.Payload)
	}
}

// #1450 spec 2: a genuine sealed file of store A opened as store B is
// refused, and the message names store B. The two files have the SAME base
// name in different directories, so only the store name tells them apart.
func TestSealedFileOpenedAsAnotherStoreIsRefusedNamingTheStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tokens := filepath.Join(dir, "one", "store.json")
	if err := os.MkdirAll(filepath.Dir(tokens), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncryptedFileBackend(tokens, "tokens", testKey(t, 0x01)).Save(ctx, []byte(`{"tokens":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	accounts := filepath.Join(dir, "two", "store.json")
	if err := os.MkdirAll(filepath.Dir(accounts), 0o700); err != nil {
		t.Fatal(err)
	}
	sealed := readFileBytes(t, tokens)
	writePlain(t, accounts, sealed)

	_, err := NewEncryptedFileBackend(accounts, "accounts", testKey(t, 0x01)).Load(ctx)
	if err == nil {
		t.Fatal("the tokens store's file opened as the accounts store")
	}
	if !strings.Contains(err.Error(), "accounts") {
		t.Errorf("the refusal does not name the store %q: %v", "accounts", err)
	}
	if errors.Is(err, ErrPlaintextDocument) {
		t.Errorf("a sealed file was reported as plaintext: %v", err)
	}
	if !bytes.Equal(sealed, readFileBytes(t, accounts)) {
		t.Error("a refused open changed the file")
	}
	// And it still opens as its own store.
	if _, err := NewEncryptedFileBackend(accounts, "tokens", testKey(t, 0x01)).Load(ctx); err != nil {
		t.Errorf("the tokens file did not open as the tokens store: %v", err)
	}
}

// #1450 spec 3a: a file sealed under its absolute path opens at that path.
func TestLegacyAbsolutePathFileOpensAtItsOriginalPath(t *testing.T) {
	p := legacyFixture(t, t.TempDir())
	snap, err := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", testKey(t, 0x07)).Load(context.Background())
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
	if _, err := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", testKey(t, 0x07)).Load(ctx); err != nil {
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
	snap, err := NewEncryptedFileBackend(moved, "flags", testKey(t, 0x07)).Load(ctx)
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
	b := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", testKey(t, 0x07))
	snap, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("open at the original path: %v", err)
	}
	if _, err := b.Save(ctx, []byte(`{"flags":[]}`), snap.Version); err != nil {
		t.Fatalf("save after open: %v", err)
	}
	moved := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, moved, readFileBytes(t, p))
	if _, err := NewEncryptedFileBackend(moved, "flags", testKey(t, 0x07)).Load(ctx); err != nil {
		t.Fatalf("the saved file did not open from a new path: %v", err)
	}
}

// #1450 spec 3c: an old-format file at a path other than the one it was
// sealed at is refused as before, and left unchanged.
func TestLegacyAbsolutePathFileAtADifferentPathIsRefused(t *testing.T) {
	ctx := context.Background()
	p := legacyFixture(t, t.TempDir())
	before := readFileBytes(t, p)
	_, err := NewEncryptedFileBackend(p, "flags", testKey(t, 0x07)).Load(ctx)
	if err == nil {
		t.Fatal("an old-format file opened at a path it was not sealed at")
	}
	if _, err := NewEncryptedFileBackendForPath(p, "/srv/other/flags.json", "flags", testKey(t, 0x07)).Load(ctx); err == nil {
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
	if _, err := NewEncryptedFileBackend(good, "flags", testKey(t, 0x01)).Save(ctx, []byte(`{"n":1}`), 0); err != nil {
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
		if _, err := NewEncryptedFileBackend(p, "flags", testKey(t, 0x01)).Load(ctx); err == nil {
			t.Errorf("%s file opened", name)
		}
	}

	plain := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, plain, []byte(`{"n":1}`))
	if _, err := NewEncryptedFileBackend(plain, "flags", testKey(t, 0x01)).Load(ctx); !errors.Is(err, ErrPlaintextDocument) {
		t.Errorf("plaintext: err = %v, want ErrPlaintextDocument", err)
	}

	// Sealed under another key, at the right name.
	if _, err := NewEncryptedFileBackend(good, "flags", testKey(t, 0x02)).Load(ctx); err == nil {
		t.Error("a file sealed under another key opened")
	}
}

// #1450 S1: the store-name binding can never equal any old path binding
// (its associated data holds a byte, NUL, that no path can). A store whose
// configured relative path is the literal text the name binding would have
// used if it had no NUL cannot open another store's new-format file through
// the legacy fallback.
func TestStoreNameBindingCanNeverBeReachedThroughALegacyPathBinding(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Chdir(tmp)

	// The tokens store's genuine new-format file.
	sealedAt := filepath.Join(tmp, "tokens-real.json")
	if _, err := NewEncryptedFileBackend(sealedAt, "tokens", testKey(t, 0x01)).Save(ctx, []byte(`{"tokens":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	sealed := readFileBytes(t, sealedAt)

	// Another store, whose configured relative path spells out the
	// binding text, and which holds those bytes.
	const hostile = "mikroview/sealed-store-name/v1:tokens"
	if err := os.MkdirAll(filepath.Dir(hostile), 0o700); err != nil {
		t.Fatal(err)
	}
	writePlain(t, hostile, sealed)
	if _, err := NewEncryptedFileBackend(hostile, "accounts", testKey(t, 0x01)).Load(ctx); err == nil {
		t.Fatal("the tokens store's file opened as another store through its path being the binding text")
	}
	if !bytes.Equal(sealed, readFileBytes(t, hostile)) {
		t.Error("a refused open changed the file")
	}
	// Control: the same bytes are the tokens store's own at any path.
	if _, err := NewEncryptedFileBackend(hostile, "tokens", testKey(t, 0x01)).Load(ctx); err != nil {
		t.Errorf("control: the tokens file did not open as the tokens store: %v", err)
	}
}

// The old-format writer used by the other tests of this file agrees with
// the committed fixture's format: a file sealed with the path as associated
// data opens through the legacy route. (Nothing in the product writes the
// old format any more, so this guards the helper the tests rely on.)
func TestOldFormatHelperAgreesWithTheCommittedFixture(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "flags.json")
	sealed, err := testKey(t, 0x07).Seal(retention.StateStoreKeyInfo, []byte(legacyLogical), []byte(legacyPayload))
	if err != nil {
		t.Fatal(err)
	}
	writePlain(t, p, sealed)
	snap, err := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", testKey(t, 0x07)).Load(context.Background())
	if err != nil {
		t.Fatalf("a file sealed by hand with the path as associated data did not open: %v", err)
	}
	if string(snap.Payload) != legacyPayload {
		t.Errorf("payload = %q", snap.Payload)
	}
}

// #1450 decision (note 58486): when the re-seal of an old-format file fails
// for any reason other than a version conflict, Load still returns the
// document it opened, with no error, and leaves the file as it was.
//
// The write is made to fail without a permission trick (a directory chmod
// does not stop root, which CI runs as): the file's base name is 250 bytes,
// so it opens, but WriteFileAtomic's temp file, named base+".tmp-*", exceeds
// the 255-byte name limit and CreateTemp fails with ENAMETOOLONG.
func TestLegacyFileWhoseReSealFailsStillOpensAndIsLeftUnchanged(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), strings.Repeat("f", 245)+".json")
	body, err := os.ReadFile(filepath.Join("testdata", "sealed-abspath-flags.json"))
	if err != nil {
		t.Fatal(err)
	}
	writePlain(t, p, body)
	before := readFileBytes(t, p)

	// Guard: the write really is refused here, for a reason other than a
	// version conflict.
	if _, err := NewEncryptedFileBackend(p, "flags", testKey(t, 0x07)).Save(ctx, []byte(`{}`), contentVersion(before)); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("test setup: a save here must fail with a write error, got %v", err)
	}

	snap, err := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", testKey(t, 0x07)).Load(ctx)
	if err != nil {
		t.Fatalf("Load returned an error for a document it opened: %v", err)
	}
	if string(snap.Payload) != legacyPayload {
		t.Errorf("payload = %q, want %q", snap.Payload, legacyPayload)
	}
	if !bytes.Equal(before, readFileBytes(t, p)) {
		t.Error("a failed re-seal changed the file on disk")
	}
}

// PROP-R1: a server start opens a dozen stores before the one that may
// refuse it, so a file still sealed under its pre-#1450 path binding is
// left exactly as the earlier release wrote it until the start is
// committed (Rebind). Written from the ruling; does not compile until
// DeferRebind and Rebind exist.
func TestDeferredRebindLeavesALegacyFileUntilRebind(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := legacyFixture(t, dir)
	key := testKey(t, 0x07)
	before := readFileBytes(t, p)

	b := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", key).DeferRebind()
	snap, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(snap.Payload) != legacyPayload {
		t.Errorf("payload = %q, want %q", snap.Payload, legacyPayload)
	}
	if !bytes.Equal(before, readFileBytes(t, p)) {
		t.Fatal("a deferred Load changed the file")
	}

	saved := `{"flags":[]}`
	if _, err := b.Save(ctx, []byte(saved), snap.Version); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Still the old binding: the release that wrote it can open it...
	got, err := NewEncryptedFileBackendForPath(p, legacyLogical, "flags", key).WithoutRebind().Load(ctx)
	if err != nil || string(got.Payload) != saved {
		t.Fatalf("a save before Rebind moved the file off its old binding: %v %q", err, got.Payload)
	}
	// ...and it is still path-bound, so a copy elsewhere is refused.
	other := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, other, readFileBytes(t, p))
	if _, err := NewEncryptedFileBackend(other, "flags", key).Load(ctx); err == nil {
		t.Fatal("a copy at another path opened before Rebind; the file was re-sealed early")
	}

	mid := readFileBytes(t, p)
	if err := b.Rebind(ctx); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if bytes.Equal(mid, readFileBytes(t, p)) {
		t.Fatal("Rebind did not re-seal the file")
	}
	moved := filepath.Join(t.TempDir(), "flags.json")
	writePlain(t, moved, readFileBytes(t, p))
	got, err = NewEncryptedFileBackend(moved, "flags", key).Load(ctx)
	if err != nil || string(got.Payload) != saved {
		t.Fatalf("after Rebind a copy at another path did not open with the saved payload: %v %q", err, got.Payload)
	}
}

// PROP-R1: Rebind on a file already bound to its store name writes nothing.
func TestRebindOnAFileAlreadyBoundToItsNameWritesNothing(t *testing.T) {
	ctx := context.Background()
	key := testKey(t, 0x07)
	p := filepath.Join(t.TempDir(), "flags.json")
	if _, err := NewEncryptedFileBackend(p, "flags", key).Save(ctx, []byte(legacyPayload), 0); err != nil {
		t.Fatalf("seal: %v", err)
	}
	before := readFileBytes(t, p)
	b := NewEncryptedFileBackend(p, "flags", key).DeferRebind()
	if _, err := b.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Rebind(ctx); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if !bytes.Equal(before, readFileBytes(t, p)) {
		t.Error("Rebind rewrote a file already bound to its store name")
	}
}
