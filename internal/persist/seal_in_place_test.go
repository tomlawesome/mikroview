// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 (authentication moves onto gauntlet), ruling 11 and the build
// notes' A1 step 3: mikroview's own EncryptedFileBackend gains a
// seal-in-place option for the auth stores, and says it protects its
// document at rest.
//
// Written from the specification (docs/decisions/gauntlet-adoption.md,
// "Owner rulings" 2 and 11; build notes B8 35 and B9 40, 41) before the
// code exists. Names invented here for the build to provide:
//
//	(*EncryptedFileBackend).WithSealInPlace() *EncryptedFileBackend
//	    switches the option on and returns the same backend.
//	(*EncryptedFileBackend).ProtectedAtRest() bool   (true)
//	(*WriteBehind).ProtectedAtRest() bool            (the inner backend's answer)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gpersist "github.com/tomlawesome/gauntlet/persist"
)

// atRest is gauntlet's optional interface (persist.AtRest), restated so
// these tests do not depend on that type's exact home.
type atRest interface{ ProtectedAtRest() bool }

func writePlain(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// #1202 B8.35 (file backend half): a plaintext JSON document is read as
// the plaintext, sealed under the key with the path bound, and saved
// back; it opens again afterwards with no option set.
func TestSealInPlaceSealsAPlaintextJSONDocumentOnFirstLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	plain := []byte(`{"users":[{"id":"aaaa","username":"admin","role":"admin"}]}`)
	writePlain(t, path, plain)
	plainVersion := contentVersion(plain)

	b := NewEncryptedFileBackend(path, testKey(t, 0x01)).WithSealInPlace()
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load of a plaintext document with the seal-in-place option: %v", err)
	}
	if !bytes.Equal(snap.Payload, plain) {
		t.Errorf("Payload = %q, want the plaintext document %q", snap.Payload, plain)
	}
	if !snap.Exists {
		t.Error("Exists = false for a document that was there")
	}

	onDisk := readFileBytes(t, path)
	if json.Valid(onDisk) {
		t.Fatalf("the file is still valid JSON after the first open; it was not sealed:\n%s", onDisk)
	}
	if bytes.Contains(onDisk, []byte("admin")) {
		t.Error("the sealed file still contains the account name in the clear")
	}

	// The version handed back is the sealed file's, so the next Save
	// (which the store makes from this version) is accepted.
	if snap.Version == plainVersion {
		t.Error("Load returned the plaintext file's version; the file was sealed under a new one")
	}
	if v, exists, err := b.Version(context.Background()); err != nil || !exists || v != snap.Version {
		t.Errorf("Version() = %d, %v, %v; want the version Load returned (%d)", v, exists, err, snap.Version)
	}
	if _, err := b.Save(context.Background(), []byte(`{"users":[]}`), snap.Version); err != nil {
		t.Errorf("Save under the version Load returned: %v", err)
	}

	// A second open with the same key and no option reads it back: the
	// seal is the ordinary one, bound to this path.
	again := NewEncryptedFileBackend(path, testKey(t, 0x01))
	snap2, err := again.Load(context.Background())
	if err != nil {
		t.Fatalf("re-open without the option: %v", err)
	}
	if string(snap2.Payload) != `{"users":[]}` {
		t.Errorf("re-open Payload = %q", snap2.Payload)
	}
}

// Pairs with the test above: without the option the same plaintext file
// is refused and left alone (the behaviour for every non-auth store).
func TestSealInPlaceOffRefusesAPlaintextDocumentAndLeavesItAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	plain := []byte(`{"users":[]}`)
	writePlain(t, path, plain)

	b := NewEncryptedFileBackend(path, testKey(t, 0x01))
	if _, err := b.Load(context.Background()); err == nil {
		t.Fatal("a plaintext file was accepted without the option")
	}
	if got := readFileBytes(t, path); !bytes.Equal(got, plain) {
		t.Errorf("the refused file was changed: %q", got)
	}
}

// The tokens document of an older release is a bare JSON list, not an
// object (build notes B1.8): it must be sealed too.
func TestSealInPlaceSealsABareJSONListToo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	plain := []byte(`[{"id":"t1","name":"grafana","kind":"api"}]`)
	writePlain(t, path, plain)

	b := NewEncryptedFileBackend(path, testKey(t, 0x01)).WithSealInPlace()
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(snap.Payload, plain) {
		t.Errorf("Payload = %q, want %q", snap.Payload, plain)
	}
	if json.Valid(readFileBytes(t, path)) {
		t.Error("the bare list was not sealed")
	}
}

// #1202 B9.40: a file the previous release sealed opens unchanged, and
// its bytes do not change until the first save.
func TestSealInPlaceLeavesAFileTheLastReleaseSealedByteForByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	key := testKey(t, 0x07)

	// What the previous release wrote: the encrypted backend, no option.
	if _, err := NewEncryptedFileBackend(path, key).Save(context.Background(), []byte(`{"users":[{"username":"admin"}]}`), 0); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	before := readFileBytes(t, path)

	b := NewEncryptedFileBackend(path, key).WithSealInPlace()
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(snap.Payload) != `{"users":[{"username":"admin"}]}` {
		t.Errorf("Payload = %q", snap.Payload)
	}
	if after := readFileBytes(t, path); !bytes.Equal(before, after) {
		t.Error("opening an already-sealed file rewrote it; the bytes must not change until the first save")
	}

	// Paired: the first save does change them.
	if _, err := b.Save(context.Background(), []byte(`{"users":[]}`), snap.Version); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if after := readFileBytes(t, path); bytes.Equal(before, after) {
		t.Error("a save left the bytes unchanged")
	}
}

// #1202 B9.41 (store level): a file that is neither sealed nor valid
// JSON fails to open, is never sealed, never read as an empty store, and
// is left byte for byte as it was.
func TestSealInPlaceRefusesAFileThatIsNeitherSealedNorJSON(t *testing.T) {
	cases := map[string][]byte{
		"truncated json": []byte(`{"users":[{"id":"a"`),
		"random bytes":   {0x00, 0x01, 0xfe, 0xff, 0x10, 0x20, 0x30},
		"text":           []byte("this is not an accounts file"),
		"empty file":     {},
		"a json prefix":  []byte(`{"users":[]} trailing garbage`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "users.json")
			writePlain(t, path, body)

			b := NewEncryptedFileBackend(path, testKey(t, 0x01)).WithSealInPlace()
			snap, err := b.Load(context.Background())
			if err == nil {
				t.Fatalf("Load succeeded with Payload %q; a damaged file must fail to open, not read as empty", snap.Payload)
			}
			if got := readFileBytes(t, path); !bytes.Equal(got, body) {
				t.Errorf("the damaged file was changed:\n got %q\nwant %q", got, body)
			}
		})
	}
}

// A sealed file damaged after the fact (cut short) is neither sealed
// properly nor JSON: it fails, and is not "repaired" by sealing it.
func TestSealInPlaceRefusesATruncatedSealedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	key := testKey(t, 0x01)
	if _, err := NewEncryptedFileBackend(path, key).Save(context.Background(), []byte(`{"users":[{"username":"admin"}]}`), 0); err != nil {
		t.Fatal(err)
	}
	whole := readFileBytes(t, path)
	cut := whole[:len(whole)/2]
	writePlain(t, path, cut)

	if _, err := NewEncryptedFileBackend(path, key).WithSealInPlace().Load(context.Background()); err == nil {
		t.Fatal("a truncated sealed file opened")
	}
	if got := readFileBytes(t, path); !bytes.Equal(got, cut) {
		t.Error("the truncated file was changed")
	}
}

// A wrong key fails as today and never seals over the document.
func TestSealInPlaceWrongKeyFailsAndLeavesTheSealedFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if _, err := NewEncryptedFileBackend(path, testKey(t, 0x01)).Save(context.Background(), []byte(`{"users":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	before := readFileBytes(t, path)

	b := NewEncryptedFileBackend(path, testKey(t, 0x02)).WithSealInPlace()
	if _, err := b.Load(context.Background()); err == nil {
		t.Fatal("a file sealed under another key opened")
	}
	if after := readFileBytes(t, path); !bytes.Equal(before, after) {
		t.Error("a wrong-key open changed the file")
	}
}

// The seal binds the file's path "as today": sealed bytes copied to another
// path do not open there, with or without the option.
func TestSealInPlaceBindsTheSealToThePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "users.json")
	writePlain(t, a, []byte(`{"users":[]}`))
	if _, err := NewEncryptedFileBackend(a, testKey(t, 0x01)).WithSealInPlace().Load(context.Background()); err != nil {
		t.Fatalf("sealing at a: %v", err)
	}
	sealed := readFileBytes(t, a)

	other := filepath.Join(dir, "tokens.json")
	writePlain(t, other, sealed)
	for _, withOption := range []bool{false, true} {
		b := NewEncryptedFileBackend(other, testKey(t, 0x01))
		if withOption {
			b = b.WithSealInPlace()
		}
		if _, err := b.Load(context.Background()); err == nil {
			t.Errorf("sealed bytes copied to another path opened (option=%v)", withOption)
		}
	}
	if !bytes.Equal(readFileBytes(t, other), sealed) {
		t.Error("the copied file was changed by a refused open")
	}
}

// The recorded upgrade fixtures were made by a container whose path is
// not the test's: NewEncryptedFileBackendForPath binds the logical path.
// The option must work through that constructor too.
func TestSealInPlaceWorksThroughTheLogicalPathConstructor(t *testing.T) {
	real := filepath.Join(t.TempDir(), "users.json")
	writePlain(t, real, []byte(`{"users":[]}`))
	b := NewEncryptedFileBackendForPath(real, "/var/lib/mikroview/users.json", testKey(t, 0x03)).WithSealInPlace()
	if _, err := b.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if json.Valid(readFileBytes(t, real)) {
		t.Fatal("not sealed")
	}
	// Opens under the same logical path, not under the real one.
	if _, err := NewEncryptedFileBackendForPath(real, "/var/lib/mikroview/users.json", testKey(t, 0x03)).Load(context.Background()); err != nil {
		t.Errorf("re-open under the logical path: %v", err)
	}
	if _, err := NewEncryptedFileBackend(real, testKey(t, 0x03)).Load(context.Background()); err == nil {
		t.Error("the seal was bound to the real path, not the logical one")
	}
}

// #1202 A1 step 3: gauntlet reads a backend as protected only if it says
// so (persist.AtRest). The encrypted file backend does; the plain file
// backend does not (it is plaintext, which is what gauntlet then sees).
func TestEncryptedFileBackendSaysItIsProtectedAtRest(t *testing.T) {
	dir := t.TempDir()
	enc := NewEncryptedFileBackend(filepath.Join(dir, "a.json"), testKey(t, 0x01))
	var backend any = enc
	ar, ok := backend.(atRest)
	if !ok || !ar.ProtectedAtRest() {
		t.Fatal("EncryptedFileBackend does not report ProtectedAtRest() == true")
	}

	var plain any = NewFileBackend(filepath.Join(dir, "b.json"))
	if p, ok := plain.(atRest); ok && p.ProtectedAtRest() {
		t.Error("the plain file backend claims to protect its document at rest")
	}
}

func TestWriteBehindForwardsItsInnerBackendsAtRestAnswer(t *testing.T) {
	dir := t.TempDir()
	open := func(inner Backend) any {
		wb, _, err := OpenWriteBehind(context.Background(), inner, "test", WriteBehindOptions{MinInterval: time.Millisecond}, func([]byte) error { return nil })
		if err != nil {
			t.Fatalf("OpenWriteBehind: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			wb.Close(ctx)
		})
		return wb
	}

	sealed := open(NewEncryptedFileBackend(filepath.Join(dir, "a.json"), testKey(t, 0x01)))
	if ar, ok := sealed.(atRest); !ok || !ar.ProtectedAtRest() {
		t.Error("a write-behind over the encrypted backend does not report ProtectedAtRest() == true")
	}
	plain := open(NewFileBackend(filepath.Join(dir, "b.json")))
	ar, ok := plain.(atRest)
	if !ok {
		t.Fatal("the write-behind wrapper has no ProtectedAtRest method; it must forward its inner backend's answer (false here)")
	}
	if ar.ProtectedAtRest() {
		t.Error("a write-behind over a plain file backend claims protection at rest")
	}
}

// #1202 A1 steps 1-2: gauntlet's stores take mikroview's backends as they
// are, and compare conflicts with errors.Is against gauntlet's value.
func TestMikroviewBackendsAreGauntletBackends(t *testing.T) {
	dir := t.TempDir()
	var _ gpersist.Backend = NewFileBackend(filepath.Join(dir, "a.json"))
	var _ gpersist.Backend = NewEncryptedFileBackend(filepath.Join(dir, "b.json"), testKey(t, 0x01))
	var _ gpersist.Backend = NewPostgresBackend(nil, "auth")
}

func TestErrConflictIsGauntletsErrConflict(t *testing.T) {
	if !errors.Is(ErrConflict, gpersist.ErrConflict) {
		t.Fatal("persist.ErrConflict is not gauntlet's persist.ErrConflict; gauntlet's stores compare with errors.Is")
	}
	path := filepath.Join(t.TempDir(), "x.json")
	b := NewEncryptedFileBackend(path, testKey(t, 0x01))
	if _, err := b.Save(context.Background(), []byte(`{}`), 0); err != nil {
		t.Fatal(err)
	}
	_, err := b.Save(context.Background(), []byte(`{"n":1}`), 12345)
	if !errors.Is(err, gpersist.ErrConflict) {
		t.Errorf("a stale save returned %v, want an error that is gauntlet's ErrConflict", err)
	}
}
