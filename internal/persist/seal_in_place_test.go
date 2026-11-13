// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 (authentication moves onto gauntlet), rulings 11 and 19 and the
// build notes' A1 step 3.
//
// Ruling 11 gave mikroview's EncryptedFileBackend a seal-in-place option
// for the auth stores; ruling 19 (2026-10-10, R26) removed sealing in
// place altogether. The tests below that used to expect a plaintext
// document to be sealed on first load now expect a refusal that leaves
// the file byte for byte, and none of them calls WithSealInPlace (which
// the fix deletes), so this file compiles before and after.
//
// Wording the fix must provide (ruling 19), for the error that
// ErrPlaintextDocument wraps: it says the data was written without a key
// before v0.7.0, and to restore a sealed copy or move it aside. It no
// longer says anything about the first start of v0.7.0 or about a start
// sealing the file (R24, R23).
//
//	(*EncryptedFileBackend).ProtectedAtRest() bool   (true)
//	(*WriteBehind).ProtectedAtRest() bool            (the inner backend's answer)

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// ruling19Wanted is what every plaintext refusal must tell the operator
// (ruling 19), and ruling19Banned what it must no longer claim.
var (
	ruling19Wanted = []string{"without a key", "v0.7.0", "restore a sealed copy", "move it aside"}
	ruling19Banned = []string{"first start of v0.7.0", "seals it", "will seal", "normal start"}
)

func wantRuling19Wording(t *testing.T, what, msg string) {
	t.Helper()
	low := strings.ToLower(msg)
	for _, w := range ruling19Wanted {
		if !strings.Contains(low, w) {
			t.Errorf("%s: the refusal does not say %q:\n%s", what, w, msg)
		}
	}
	for _, w := range ruling19Banned {
		if strings.Contains(low, w) {
			t.Errorf("%s: the refusal still says %q:\n%s", what, w, msg)
		}
	}
}

// #1202 review R26 (ruling 19), replacing "a plaintext document is sealed
// on first load" (B8.35, file backend half): a plaintext JSON document,
// an accounts object or a bare token list, is refused with
// ErrPlaintextDocument, and the file is left byte for byte. Nothing is
// sealed. Paired: moved aside, the path takes a sealed document and opens.
func TestAPlaintextJSONDocumentIsRefusedAndLeftByteForByte(t *testing.T) {
	cases := map[string][]byte{
		"accounts object": []byte(`{"users":[{"id":"aaaa","username":"admin","role":"admin"}]}`),
		"bare token list": []byte(`[{"id":"t1","name":"grafana","kind":"api"}]`),
	}
	for name, plain := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "users.json")
			writePlain(t, path, plain)

			b := NewEncryptedFileBackend(path, testKey(t, 0x01))
			snap, err := b.Load(context.Background())
			if err == nil {
				t.Fatalf("Load accepted a plaintext document, Payload %q", snap.Payload)
			}
			if !errors.Is(err, ErrPlaintextDocument) {
				t.Errorf("err = %v, want one wrapping ErrPlaintextDocument", err)
			}
			wantRuling19Wording(t, name, err.Error())
			if got := readFileBytes(t, path); !bytes.Equal(got, plain) {
				t.Errorf("the refused file was changed (sealed or rewritten): %q", got)
			}

			// Paired: the operator moves it aside; the path then takes a
			// sealed document, which opens.
			if err := os.Rename(path, path+".aside"); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Save(context.Background(), []byte(`{"users":[]}`), 0); err != nil {
				t.Fatalf("Save after moving the plaintext aside: %v", err)
			}
			again, err := NewEncryptedFileBackend(path, testKey(t, 0x01)).Load(context.Background())
			if err != nil || string(again.Payload) != `{"users":[]}` {
				t.Errorf("a sealed document at the same path did not open: %q, %v", again.Payload, err)
			}
		})
	}
}

// #1202 review R24: the refusal of a plaintext document does not claim it
// is "the first start of v0.7.0", for any store -- a stale plaintext
// flags.json is not an auth store and never had a first-start window.
// Paired with the sealed document of the same store, which opens.
func TestAPlaintextDocumentForANonAuthStoreIsRefusedWithoutClaimingTheFirstStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	plain := []byte(`{"flags":[]}`)
	writePlain(t, path, plain)

	_, err := NewEncryptedFileBackend(path, testKey(t, 0x01)).Load(context.Background())
	if err == nil {
		t.Fatal("a plaintext file was accepted")
	}
	if strings.Contains(strings.ToLower(err.Error()), "first start of v0.7.0") {
		t.Errorf("the refusal of a non-auth store's file claims the first start of v0.7.0:\n%v", err)
	}
	if strings.Contains(strings.ToLower(ErrPlaintextDocument.Error()), "first start of v0.7.0") {
		t.Errorf("ErrPlaintextDocument still claims the first start of v0.7.0:\n%v", ErrPlaintextDocument)
	}
	if got := readFileBytes(t, path); !bytes.Equal(got, plain) {
		t.Errorf("the refused file was changed: %q", got)
	}

	sealedPath := filepath.Join(t.TempDir(), "flags.json")
	if _, err := NewEncryptedFileBackend(sealedPath, testKey(t, 0x01)).Save(context.Background(), plain, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncryptedFileBackend(sealedPath, testKey(t, 0x01)).Load(context.Background()); err != nil {
		t.Errorf("the sealed document of the same store did not open: %v", err)
	}
}

// #1202 B9.40: a file the previous release sealed opens unchanged, and
// its bytes do not change until the first save.
func TestAFileTheLastReleaseSealedOpensByteForByteUntilTheFirstSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	key := testKey(t, 0x07)

	// What the previous release wrote: the encrypted backend.
	if _, err := NewEncryptedFileBackend(path, key).Save(context.Background(), []byte(`{"users":[{"username":"admin"}]}`), 0); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	before := readFileBytes(t, path)

	b := NewEncryptedFileBackend(path, key)
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
// JSON fails to open, is never read as an empty store, and is left byte
// for byte as it was. Paired with the valid-JSON case in the test above,
// which is refused as plaintext, so no input is "repaired" by sealing it.
func TestAFileThatIsNeitherSealedNorJSONFailsToOpenAndIsLeftAlone(t *testing.T) {
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

			b := NewEncryptedFileBackend(path, testKey(t, 0x01))
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
// properly nor JSON: it fails, and is not "repaired".
func TestATruncatedSealedFileFailsToOpenAndIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	key := testKey(t, 0x01)
	if _, err := NewEncryptedFileBackend(path, key).Save(context.Background(), []byte(`{"users":[{"username":"admin"}]}`), 0); err != nil {
		t.Fatal(err)
	}
	whole := readFileBytes(t, path)
	cut := whole[:len(whole)/2]
	writePlain(t, path, cut)

	if _, err := NewEncryptedFileBackend(path, key).Load(context.Background()); err == nil {
		t.Fatal("a truncated sealed file opened")
	}
	if got := readFileBytes(t, path); !bytes.Equal(got, cut) {
		t.Error("the truncated file was changed")
	}
}

// A wrong key fails as today and never seals over the document.
func TestAWrongKeyFailsAndLeavesTheSealedFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if _, err := NewEncryptedFileBackend(path, testKey(t, 0x01)).Save(context.Background(), []byte(`{"users":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	before := readFileBytes(t, path)

	b := NewEncryptedFileBackend(path, testKey(t, 0x02))
	if _, err := b.Load(context.Background()); err == nil {
		t.Fatal("a file sealed under another key opened")
	}
	if after := readFileBytes(t, path); !bytes.Equal(before, after) {
		t.Error("a wrong-key open changed the file")
	}
}

// The seal binds the file's path "as today": sealed bytes copied to another
// path do not open there.
func TestTheSealBindsTheFileToItsPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "users.json")
	if _, err := NewEncryptedFileBackend(a, testKey(t, 0x01)).Save(context.Background(), []byte(`{"users":[]}`), 0); err != nil {
		t.Fatalf("sealing at a: %v", err)
	}
	if _, err := NewEncryptedFileBackend(a, testKey(t, 0x01)).Load(context.Background()); err != nil {
		t.Fatalf("the sealed file does not open at its own path: %v", err)
	}
	sealed := readFileBytes(t, a)

	other := filepath.Join(dir, "tokens.json")
	writePlain(t, other, sealed)
	if _, err := NewEncryptedFileBackend(other, testKey(t, 0x01)).Load(context.Background()); err == nil {
		t.Error("sealed bytes copied to another path opened")
	}
	if !bytes.Equal(readFileBytes(t, other), sealed) {
		t.Error("the copied file was changed by a refused open")
	}
}

// The recorded upgrade fixtures were made by a container whose path is
// not the test's: NewEncryptedFileBackendForPath binds the logical path.
// A document sealed under it opens under it and not under the real path;
// a plaintext document is refused through it as through the other.
func TestTheLogicalPathConstructorSealsAndRefusesLikeTheOther(t *testing.T) {
	real := filepath.Join(t.TempDir(), "users.json")
	const logical = "/var/lib/mikroview/users.json"
	if _, err := NewEncryptedFileBackendForPath(real, logical, testKey(t, 0x03)).Save(context.Background(), []byte(`{"users":[]}`), 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := NewEncryptedFileBackendForPath(real, logical, testKey(t, 0x03)).Load(context.Background()); err != nil {
		t.Errorf("re-open under the logical path: %v", err)
	}
	if _, err := NewEncryptedFileBackend(real, testKey(t, 0x03)).Load(context.Background()); err == nil {
		t.Error("the seal was bound to the real path, not the logical one")
	}

	plainPath := filepath.Join(t.TempDir(), "users.json")
	plain := []byte(`{"users":[]}`)
	writePlain(t, plainPath, plain)
	_, err := NewEncryptedFileBackendForPath(plainPath, logical, testKey(t, 0x03)).Load(context.Background())
	if !errors.Is(err, ErrPlaintextDocument) {
		t.Errorf("a plaintext document through the logical-path constructor: err = %v, want ErrPlaintextDocument", err)
	}
	if got := readFileBytes(t, plainPath); !bytes.Equal(got, plain) {
		t.Error("the refused plaintext document was changed")
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
