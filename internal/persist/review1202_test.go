// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 review fixes (note_34034), written from the fix list before the
// fixes exist: R2 (moving a file install onto Postgres when the files are
// sealed) and R12 (a token recorded with no kind).
//
// Names the fix must provide, invented here:
//
//	AdoptSealedFile(ctx, src, storeName string, key *retention.Key, dst Backend) (AdoptResult, error)
//	    AdoptFile for a source the encrypted file backend sealed: the
//	    document is opened with key and handed to dst readable. A source
//	    that is plain JSON is refused (ErrPlaintextDocument, #1202 review
//	    R26, ruling 19), whichever store it is for. Whether it is then stored sealed is
//	    dst's business -- an auth store's dst is gauntlet's persist.Encrypt
//	    under the store's label; any other store's dst is the plain
//	    backend. The source file is never touched. The result has the same
//	    Migrated field as AdoptFile's.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"
)

// #1202 review R2: a sealed users.json adopted into a sealed destination
// arrives readable to the store (the destination seals it again under its
// own label), and the accounts still sign in. Today AdoptFile copies the
// sealed bytes into the destination, which then holds a document nothing
// can read.
func TestAdoptSealedFileHandsAnAuthStoreItsDocumentReadable(t *testing.T) {
	ctx := context.Background()
	// An accounts document gauntlet itself wrote.
	mem := gpersist.NewMemory()
	seed, err := gauntlet.OpenStore(mem, gauntlet.Options{OnSetupCode: gauntlet.SetupCodeFunc(func(string) {}), ProductName: "MikroView"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Register("moved-admin", "fixture-only-not-real", time.Now()); err != nil {
		t.Fatal(err)
	}
	snap, err := mem.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plain := snap.Payload

	dir := t.TempDir()
	src := filepath.Join(dir, "users.json")
	fileKey := testKey(t, 0x07)
	if _, err := NewEncryptedFileBackend(src, "auth", fileKey).Save(ctx, plain, 0); err != nil {
		t.Fatal(err)
	}
	sealedBytes := readFileBytes(t, src)

	// The destination: what Postgres gets for the auth store.
	underlying := NewFileBackend(filepath.Join(dir, "dst-auth-row.json"))
	dst, err := gpersist.Encrypt(underlying, bytes.Repeat([]byte("p"), gpersist.MinKeyBytes), gpersist.EncryptOptions{Label: "auth"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := AdoptSealedFile(ctx, src, "auth", fileKey, dst)
	if err != nil {
		t.Fatalf("AdoptSealedFile: %v", err)
	}
	if !res.Migrated {
		t.Error("expected Migrated")
	}
	if !bytes.Equal(readFileBytes(t, src), sealedBytes) {
		t.Error("the source file was changed; reverting to files needs it as it was")
	}
	row := readFileBytes(t, filepath.Join(dir, "dst-auth-row.json"))
	if !bytes.HasPrefix(bytes.TrimSpace(row), []byte(`{"sealed"`)) {
		t.Errorf("the auth destination does not hold a {\"sealed\": ...} document: %.80s", row)
	}
	if bytes.Contains(row, sealedBytes) {
		t.Error("the destination holds the file backend's sealed bytes verbatim")
	}
	var announced bool
	st, err := gauntlet.OpenStore(dst, gauntlet.Options{OnSetupCode: gauntlet.SetupCodeFunc(func(string) { announced = true }), ProductName: "MikroView"})
	if err != nil {
		t.Fatalf("opening the accounts store over the adopted document: %v", err)
	}
	if announced || st.Count() != 1 {
		t.Errorf("announced a setup code = %v, accounts = %d; want an existing install with 1 account", announced, st.Count())
	}
	if _, err := st.Authenticate("moved-admin", "fixture-only-not-real", time.Now()); err != nil {
		t.Errorf("the account cannot sign in after the move: %v", err)
	}
}

// #1202 review R2, other stores: a sealed droplist.json adopted into the
// plain destination arrives as the plain document, as it did before the
// auth stores were sealed.
func TestAdoptSealedFileHandsAnOtherStoreItsPlainDocument(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "droplist.json")
	fileKey := testKey(t, 0x07)
	plain := []byte(`{"entries":[{"cidr":"203.0.114.0/24","addedBy":"admin","addedAt":"2026-01-02T03:04:05Z","reason":"test"}]}`)
	if _, err := NewEncryptedFileBackend(src, "droplist", fileKey).Save(ctx, plain, 0); err != nil {
		t.Fatal(err)
	}
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))

	res, err := AdoptSealedFile(ctx, src, "droplist", fileKey, dst)
	if err != nil {
		t.Fatalf("AdoptSealedFile: %v", err)
	}
	if !res.Migrated {
		t.Error("expected Migrated")
	}
	got, err := dst.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Payload, plain) || !json.Valid(got.Payload) {
		t.Errorf("destination holds %q, want the readable document %q", got.Payload, plain)
	}
}

// Paired with the two above, the guards AdoptFile already has still hold
// for a sealed source: a store that has data is never overwritten, a
// wrong key fails and writes nothing, and no file means a fresh install.
func TestAdoptSealedFileKeepsAdoptFilesGuards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "users.json")
	fileKey := testKey(t, 0x07)
	if _, err := NewEncryptedFileBackend(src, "auth", fileKey).Save(ctx, []byte(`{"stale":true}`), 0); err != nil {
		t.Fatal(err)
	}

	live := NewFileBackend(filepath.Join(dir, "live.json"))
	if _, err := live.Save(ctx, []byte(`{"live":true}`), 0); err != nil {
		t.Fatal(err)
	}
	res, err := AdoptSealedFile(ctx, src, "auth", fileKey, live)
	if err != nil || res.Migrated {
		t.Errorf("over a store with data: migrated = %v, err = %v; want not migrated and no error", res.Migrated, err)
	}
	if got, _ := live.Load(ctx); string(got.Payload) != `{"live":true}` {
		t.Errorf("live data was overwritten: %q", got.Payload)
	}

	empty := NewFileBackend(filepath.Join(dir, "empty.json"))
	if _, err := AdoptSealedFile(ctx, src, "auth", testKey(t, 0x08), empty); err == nil {
		t.Error("a wrong key adopted the file")
	}
	if got, _ := empty.Load(ctx); got.Exists {
		t.Errorf("a wrong key still wrote %q to the destination", got.Payload)
	}

	res, err = AdoptSealedFile(ctx, filepath.Join(dir, "absent.json"), "auth", fileKey, empty)
	if err != nil || res.Migrated {
		t.Errorf("no file: migrated = %v, err = %v; want a fresh install", res.Migrated, err)
	}
}

// #1202 review R26 (ruling 19), plaintext sources in file-to-Postgres
// adoption: a plain-JSON accounts file, and a plain-JSON drop list, are
// refused rather than adopted or sealed on the way. The error wraps
// ErrPlaintextDocument and says the data was written without a key before
// v0.7.0 and to restore a sealed copy or move it aside; nothing is written
// to the destination (it still reads as no document) and the source is
// byte for byte as it was. Paired with the sealed source of the same
// store, which is adopted.
func TestAdoptSealedFileRefusesAPlaintextSourceAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	fileKey := testKey(t, 0x07)
	cases := map[string]struct {
		name  string
		plain []byte
		dst   func(t *testing.T, dir string) Backend
	}{
		"accounts into a sealed auth destination": {
			name:  "users.json",
			plain: []byte(`{"users":[{"id":"aaaa","username":"moved-admin","role":"admin"}]}`),
			dst: func(t *testing.T, dir string) Backend {
				b, err := gpersist.Encrypt(NewFileBackend(filepath.Join(dir, "dst-auth-row.json")), bytes.Repeat([]byte("p"), gpersist.MinKeyBytes), gpersist.EncryptOptions{Label: "auth"})
				if err != nil {
					t.Fatal(err)
				}
				return b
			},
		},
		"drop list into the plain destination": {
			name:  "droplist.json",
			plain: []byte(`{"entries":[{"cidr":"203.0.114.0/24","addedBy":"admin","addedAt":"2026-01-02T03:04:05Z","reason":"keyless era"}]}`),
			dst:   func(t *testing.T, dir string) Backend { return NewFileBackend(filepath.Join(dir, "dst.json")) },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tc.name)
			writePlain(t, src, tc.plain)
			dst := tc.dst(t, dir)

			res, err := AdoptSealedFile(ctx, src, tc.name, fileKey, dst)
			if err == nil {
				t.Fatalf("a plaintext source was adopted (migrated = %v)", res.Migrated)
			}
			if !errors.Is(err, ErrPlaintextDocument) {
				t.Errorf("err = %v, want one wrapping ErrPlaintextDocument", err)
			}
			wantRuling19Wording(t, name, err.Error())
			if res.Migrated {
				t.Error("Migrated is true for a refused source")
			}
			if got := readFileBytes(t, src); !bytes.Equal(got, tc.plain) {
				t.Errorf("the source was changed (sealed or rewritten): %q", got)
			}
			if snap, err := dst.Load(ctx); err == nil && snap.Exists {
				t.Errorf("the destination holds %q after a refused adoption", snap.Payload)
			}

			// Paired: the same document sealed by the previous release is adopted.
			sealedSrc := filepath.Join(dir, "sealed-"+tc.name)
			if _, err := NewEncryptedFileBackend(sealedSrc, tc.name, fileKey).Save(ctx, tc.plain, 0); err != nil {
				t.Fatal(err)
			}
			res, err = AdoptSealedFile(ctx, sealedSrc, tc.name, fileKey, dst)
			if err != nil || !res.Migrated {
				t.Errorf("the sealed source of the same store: migrated = %v, err = %v; want adopted", res.Migrated, err)
			}
		})
	}
}
