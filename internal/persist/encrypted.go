// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// EncryptedFileBackend wraps FileBackend so every document a file-backed
// store writes is ciphertext under the operator's retention key (#853) --
// the same key and the same AES-256-GCM/HKDF scheme internal/retention
// uses for the on-disk event history, reused rather than reinvented (see
// internal/retention/seal.go). Flags, entities, the MAC registry, rule
// usage, detector settings and (where main.go chooses to keep them keyed
// too) accounts and tokens all sit on this package's one-document-per-file
// shape, so wrapping FileBackend covers every one of them without
// visiting each store individually.
//
// Version numbers pass straight through to the wrapped FileBackend
// unchanged: they are opaque compare-and-swap tokens already computed
// correctly against whatever bytes are actually on disk (ciphertext,
// through this type), so nothing here needs to interpret or recompute
// them. A fresh random salt and nonce on every Save means two saves of
// identical plaintext never produce the same ciphertext -- which is fine,
// since nothing compares ciphertext for equality, only the version.
type EncryptedFileBackend struct {
	inner *FileBackend
	key   *retention.Key
	// aad binds every envelope to the store it belongs to (its file
	// path), so a document copied from one store's file to another's
	// fails to open there instead of silently decrypting as something
	// else.
	aad []byte
	// sealInPlace is WithSealInPlace's switch: see that method.
	sealInPlace bool
}

// WithSealInPlace makes Load seal a plaintext document it finds instead
// of refusing it, and returns b (#1202, ruling 11). It is for the auth
// stores alone -- accounts, tokens, recovery keys and the sign-in
// history -- which an install that ran without a key kept in the clear
// (#853 rule 6, now ended): from v0.7.0 a key is required, and those
// files have to come across without a manual step.
//
// Only a file that does not carry the sealed envelope's header and
// parses as JSON is treated as plaintext. It is read as it is, sealed
// under the key with this backend's path bound exactly as Save always
// binds it, and written back atomically under the version it was read
// at, so a concurrent writer is never overwritten. Anything else that
// will not open -- a damaged or truncated file, one sealed under another
// key or for another path, an empty file -- fails exactly as it does
// without the option: it is never sealed, never rewritten, and never
// read as an empty store. A file already sealed opens as it always did,
// and its bytes do not change until the store's own next save.
//
// Every other store keeps the refusal: there is no plaintext history to
// carry over for them, because without a key they were never written.
func (b *EncryptedFileBackend) WithSealInPlace() *EncryptedFileBackend {
	b.sealInPlace = true
	return b
}

// NewEncryptedFileBackend wraps the file at path so every read decrypts
// and every write encrypts under key. key must not be nil -- callers
// decide whether a store persists at all by whether they have a key (see
// storage.go's backendFor), and this type has no unencrypted mode to fall
// back to.
func NewEncryptedFileBackend(path string, key *retention.Key) *EncryptedFileBackend {
	return &EncryptedFileBackend{inner: NewFileBackend(path), key: key, aad: []byte(path)}
}

// NewEncryptedFileBackendForPath is NewEncryptedFileBackend with the
// bytes-on-disk location and the AAD-binding location told apart, for
// the one legitimate case they differ: a document read from somewhere
// other than where it was originally written and sealed.
//
// internal/persist/upgrade_test.go is the reason this exists (#1239):
// every recorded upgrade fixture was written by a real container at the
// file backend's fixed default data directory
// (config.DefaultDataDir, "/var/lib/mikroview"), which is where its
// ciphertext is bound to -- but the test unpacks the recording into a
// t.TempDir() to read it back, and NewEncryptedFileBackend has no way to
// open a document at one path while treating it as though it still
// lived at another. Ordinary callers -- every store main.go opens --
// always read a document from exactly where it lives, so they keep
// using NewEncryptedFileBackend; this is deliberately not the default.
func NewEncryptedFileBackendForPath(realPath, logicalPath string, key *retention.Key) *EncryptedFileBackend {
	return &EncryptedFileBackend{inner: NewFileBackend(realPath), key: key, aad: []byte(logicalPath)}
}

func (b *EncryptedFileBackend) Describe() string { return b.inner.Describe() }

func (b *EncryptedFileBackend) Close() error { return b.inner.Close() }

// ProtectedAtRest implements gauntlet's persist.AtRest: the file holds
// ciphertext only, so gauntlet's store accepts this backend without
// Options.AllowPlaintextAtRest (#1202).
func (b *EncryptedFileBackend) ProtectedAtRest() bool { return true }

// Load reads the file and decrypts it. A document that fails to decrypt
// -- the wrong key, or a file altered or written by something else -- is
// a real error, not a missing-document case: treating it as absent would
// silently reopen a first-run setup on top of a store that actually holds
// data. See internal/persist.Open, which every store's OpenWithBackend
// funnels through, for what happens with that error (issue #378: refuse
// to start rather than build a store around it).
func (b *EncryptedFileBackend) Load(ctx context.Context) (Snapshot, error) {
	// A few tries, for the seal-in-place race only: another process
	// sealing (or saving) the same file between this read and the
	// write-back makes that write lose its compare-and-swap, and the
	// file is then sealed already, so reading it again opens it.
	for attempt := 0; ; attempt++ {
		snap, err := b.load(ctx)
		if errors.Is(err, ErrConflict) && attempt < 2 {
			continue
		}
		return snap, err
	}
}

func (b *EncryptedFileBackend) load(ctx context.Context) (Snapshot, error) {
	snap, err := b.inner.Load(ctx)
	if err != nil || !snap.Exists {
		return snap, err
	}
	// Both tests, not just the JSON one: a sealed envelope begins with
	// its magic, which no JSON document can, so either alone would do
	// today -- but which rule decides is the security property here, and
	// it is written out rather than left to that coincidence.
	if b.sealInPlace && !retention.HasSealMagic(snap.Payload) && json.Valid(snap.Payload) {
		return b.sealPlaintext(ctx, snap)
	}
	plain, err := b.key.Open(retention.StateStoreKeyInfo, b.aad, snap.Payload)
	if err != nil {
		return Snapshot{}, fmt.Errorf("persist: %s: %w", b.inner.path, err)
	}
	snap.Payload = plain
	return snap, nil
}

// sealPlaintext is WithSealInPlace's write-back: the plaintext document
// snap holds is saved sealed, under the version it was read at, and
// handed back as the store asked for it -- the plaintext, with the
// version the sealed file now has, so the store's next save is accepted.
// A failed write leaves the file as it was and fails the open: a store
// must not start on a document it believes is sealed while the disk
// still holds it in the clear.
func (b *EncryptedFileBackend) sealPlaintext(ctx context.Context, snap Snapshot) (Snapshot, error) {
	version, err := b.Save(ctx, snap.Payload, snap.Version)
	if err != nil {
		return Snapshot{}, fmt.Errorf("persist: %s holds a plaintext document, and sealing it in place failed: %w", b.inner.path, err)
	}
	return Snapshot{Payload: snap.Payload, Version: version, Exists: true}, nil
}

// Save encrypts payload and writes it through the wrapped FileBackend,
// which still performs the version check against whatever ciphertext is
// currently on disk.
func (b *EncryptedFileBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	sealed, err := b.key.Seal(retention.StateStoreKeyInfo, b.aad, payload)
	if err != nil {
		return 0, fmt.Errorf("persist: encrypting %s: %w", b.inner.path, err)
	}
	return b.inner.Save(ctx, sealed, expect)
}

// Version implements persist.VersionReader by reading the raw (still
// encrypted) file's version without decrypting it -- unlike Load, this
// never fails on a document sealed under a different key, because the
// version is a hash of whatever bytes are on disk, encrypted or not. This
// exists for a caller that needs the current version to perform a
// compare-and-swap write without needing the document to actually
// decrypt first -- see runRestore in backup_cli.go, which overwrites a
// store's file regardless of whether the previous contents are readable.
func (b *EncryptedFileBackend) Version(ctx context.Context) (version int64, exists bool, err error) {
	snap, err := b.inner.Load(ctx)
	if err != nil {
		return 0, false, err
	}
	return snap.Version, snap.Exists, nil
}
