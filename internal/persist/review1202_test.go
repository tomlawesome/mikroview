// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 review fixes (note_34034), written from the fix list before the
// fixes exist: R2 (moving a file install onto Postgres when the files are
// sealed) and R12 (a token recorded with no kind).
//
// Names the fix must provide, invented here:
//
//	AdoptSealedFile(ctx, src string, key *retention.Key, dst Backend) (AdoptResult, error)
//	    AdoptFile for a source the encrypted file backend sealed (or, with
//	    a valid-JSON source, a plain one): the document is opened with key
//	    and handed to dst readable. Whether it is then stored sealed is
//	    dst's business -- an auth store's dst is gauntlet's persist.Encrypt
//	    under the store's label; any other store's dst is the plain
//	    backend. The source file is never touched. The result has the same
//	    Migrated field as AdoptFile's.

import (
	"bytes"
	"context"
	"encoding/json"
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
	if _, err := NewEncryptedFileBackend(src, fileKey).Save(ctx, plain, 0); err != nil {
		t.Fatal(err)
	}
	sealedBytes := readFileBytes(t, src)

	// The destination: what Postgres gets for the auth store.
	underlying := NewFileBackend(filepath.Join(dir, "dst-auth-row.json"))
	dst, err := gpersist.Encrypt(underlying, bytes.Repeat([]byte("p"), gpersist.MinKeyBytes), gpersist.EncryptOptions{Label: "auth", MigratePlaintext: true})
	if err != nil {
		t.Fatal(err)
	}

	res, err := AdoptSealedFile(ctx, src, fileKey, dst)
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
	if _, err := NewEncryptedFileBackend(src, fileKey).Save(ctx, plain, 0); err != nil {
		t.Fatal(err)
	}
	dst := NewFileBackend(filepath.Join(dir, "dst.json"))

	res, err := AdoptSealedFile(ctx, src, fileKey, dst)
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
	if _, err := NewEncryptedFileBackend(src, fileKey).Save(ctx, []byte(`{"stale":true}`), 0); err != nil {
		t.Fatal(err)
	}

	live := NewFileBackend(filepath.Join(dir, "live.json"))
	if _, err := live.Save(ctx, []byte(`{"live":true}`), 0); err != nil {
		t.Fatal(err)
	}
	res, err := AdoptSealedFile(ctx, src, fileKey, live)
	if err != nil || res.Migrated {
		t.Errorf("over a store with data: migrated = %v, err = %v; want not migrated and no error", res.Migrated, err)
	}
	if got, _ := live.Load(ctx); string(got.Payload) != `{"live":true}` {
		t.Errorf("live data was overwritten: %q", got.Payload)
	}

	empty := NewFileBackend(filepath.Join(dir, "empty.json"))
	if _, err := AdoptSealedFile(ctx, src, testKey(t, 0x08), empty); err == nil {
		t.Error("a wrong key adopted the file")
	}
	if got, _ := empty.Load(ctx); got.Exists {
		t.Errorf("a wrong key still wrote %q to the destination", got.Payload)
	}

	res, err = AdoptSealedFile(ctx, filepath.Join(dir, "absent.json"), fileKey, empty)
	if err != nil || res.Migrated {
		t.Errorf("no file: migrated = %v, err = %v; want a fresh install", res.Migrated, err)
	}
}
