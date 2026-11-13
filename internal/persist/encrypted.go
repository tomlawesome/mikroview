// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// ErrPlaintextDocument is the refusal of a document that parses as JSON
// but is not sealed (#1202, ruling 19). Every store file MikroView v0.7.0
// writes is sealed, and there is no sealing in place: a plaintext one
// was written by a release that ran without a key, or put there by
// something else -- and anyone who can write the data directory, key or
// no key, could otherwise plant an accounts document of their own. It is
// never read and never rewritten.
var ErrPlaintextDocument = errors.New("the file holds a plaintext document where a sealed one belongs -- it was written without a key before v0.7.0, " +
	"and MikroView does not seal plaintext in place: restore a sealed copy from a backup taken with the key mounted, " +
	"or move it aside and start fresh")

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
	snap, err := b.inner.Load(ctx)
	if err != nil || !snap.Exists {
		return snap, err
	}
	// Both tests, not just the JSON one: a sealed envelope begins with
	// its magic, which no JSON document can, so either alone would do
	// today -- but which rule decides is the security property here, and
	// it is written out rather than left to that coincidence.
	if !retention.HasSealMagic(snap.Payload) && json.Valid(snap.Payload) {
		// Named, so a caller can say what it means in its own words
		// (setup-only mode does). Never read, never rewritten (#1202,
		// ruling 19).
		return Snapshot{}, fmt.Errorf("persist: %s: %w", b.inner.path, ErrPlaintextDocument)
	}
	return b.unseal(snap)
}

// unseal opens the sealed document snap holds, with this backend's path
// bound, and hands back the plaintext under the same version.
func (b *EncryptedFileBackend) unseal(snap Snapshot) (Snapshot, error) {
	plain, err := b.key.Open(retention.StateStoreKeyInfo, b.aad, snap.Payload)
	if err != nil {
		return Snapshot{}, fmt.Errorf("persist: %s: %w", b.inner.path, err)
	}
	snap.Payload = plain
	return snap, nil
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

// RefusePlaintextRows wraps an auth store's Postgres backend, below
// gauntlet's persist.Encrypt, so a row that holds a plaintext document
// is refused with ErrPlaintextDocument -- the same refusal, in the same
// words, as a plaintext file (#1202, ruling 19). gauntlet refuses that
// row too (without EncryptOptions.MigratePlaintext, which mikroview never
// sets), but with an error of its own that cannot be told apart from
// any other failure to open, so the operator would not learn what the
// row is or what to do about it. The row is never read as a document
// and never rewritten.
//
// A row is plaintext when it is JSON but neither envelope gauntlet
// writes: not the binary form (its magic is not JSON) and not the text
// form, an object with a "sealed" member. Anything else -- a damaged
// envelope, a row sealed under another key or label -- passes through
// for gauntlet to refuse as it always has.
func RefusePlaintextRows(inner Backend) Backend {
	return &plaintextRowGuard{inner: inner}
}

type plaintextRowGuard struct{ inner Backend }

func (g *plaintextRowGuard) Load(ctx context.Context) (Snapshot, error) {
	snap, err := g.inner.Load(ctx)
	if err != nil || !snap.Exists {
		return snap, err
	}
	if isPlaintextDocument(snap.Payload) {
		return Snapshot{}, fmt.Errorf("persist: %s: %w", g.inner.Describe(), ErrPlaintextDocument)
	}
	return snap, nil
}

func (g *plaintextRowGuard) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return g.inner.Save(ctx, payload, expect)
}

func (g *plaintextRowGuard) Close() error     { return g.inner.Close() }
func (g *plaintextRowGuard) Describe() string { return g.inner.Describe() }

// Version passes the inner backend's through, so a store polling for
// staleness keeps its cheap check; a backend without one is read whole,
// as a caller with no VersionReader would.
func (g *plaintextRowGuard) Version(ctx context.Context) (int64, bool, error) {
	if vr, ok := g.inner.(VersionReader); ok {
		return vr.Version(ctx)
	}
	snap, err := g.inner.Load(ctx)
	return snap.Version, snap.Exists, err
}

// isPlaintextDocument is RefusePlaintextRows' test; see there.
func isPlaintextDocument(payload []byte) bool {
	if retention.HasSealMagic(payload) || !json.Valid(payload) {
		return false
	}
	var head map[string]json.RawMessage
	if err := json.Unmarshal(payload, &head); err != nil {
		return true // JSON, but not an object: a bare token list, say
	}
	_, sealed := head["sealed"]
	return !sealed
}
