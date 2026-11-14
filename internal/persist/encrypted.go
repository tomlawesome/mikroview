// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/logging"
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
	// store is the store's own name ("flags", "tokens", ...), the one
	// its Postgres row uses where it has one. Every envelope is bound to
	// it (#1450): a sealed file moved to a new path, or a whole data
	// directory moved, still opens as the same store, while a genuine
	// file of one store put in another's place does not open there --
	// whatever the two files are called, since store paths are
	// separately configurable and two may share a base name.
	store string
	// aad is storeBindingPrefix + store.
	aad []byte
	// legacyAAD is what this file was bound to before #1450: its
	// configured path. A file still sealed that way opens once under it and
	// is re-saved under aad (see Load). Re-binding sealed data is not
	// sealing plaintext, so #1202 ruling 19 is untouched.
	legacyAAD []byte
	// noRebind makes Load leave a legacy file as it is (see
	// WithoutRebind).
	noRebind bool
}

// storeBindingPrefix is the domain separation in front of the store
// name in a seal's associated data (#1450). The legacy binding was the
// store's configured path, as written -- relative or absolute, since
// config never makes it absolute -- so it could be any text a path can
// be. This prefix holds a NUL byte, which no path can, so a store-name
// binding can never equal a legacy path binding's bytes, whatever a
// store is called or wherever it is configured.
const storeBindingPrefix = "mikroview/sealed-store-name/v1\x00"

// rebindLog reports each file re-sealed from the old path binding to
// the store-name binding -- once per file, since a re-bound file never
// takes that route again.
var rebindLog = logging.New("persist")

// NewEncryptedFileBackend wraps the file at path so every read decrypts
// and every write encrypts under key, bound to storeName. key must not
// be nil -- callers decide whether a store persists at all by whether
// they have a key (see storage.go's backendFor), and this type has no
// unencrypted mode to fall back to. storeName must be stable: it is in
// every envelope this store has written, so renaming it makes the store
// unreadable.
func NewEncryptedFileBackend(path, storeName string, key *retention.Key) *EncryptedFileBackend {
	return NewEncryptedFileBackendForPath(path, path, storeName, key)
}

// NewEncryptedFileBackendForPath is NewEncryptedFileBackend with the
// bytes-on-disk location and the location a pre-#1450 file was sealed
// at told apart, for the one legitimate case they differ: a document
// sealed before #1450 (bound to its absolute path), read from somewhere
// other than where it was written.
//
// internal/persist/upgrade_test.go is the reason this exists (#1239):
// every recorded upgrade fixture was written by a real container at the
// file backend's fixed default data directory
// (config.DefaultDataDir, "/var/lib/mikroview"), which is where its
// ciphertext is bound to -- but the test unpacks the recording into a
// t.TempDir() to read it back. Ordinary callers -- every store main.go
// opens -- always read a document from exactly where it lives, so they
// keep using NewEncryptedFileBackend; this is deliberately not the
// default.
func NewEncryptedFileBackendForPath(realPath, logicalPath, storeName string, key *retention.Key) *EncryptedFileBackend {
	return &EncryptedFileBackend{
		inner:     NewFileBackend(realPath),
		key:       key,
		store:     storeName,
		aad:       []byte(storeBindingPrefix + storeName),
		legacyAAD: []byte(logicalPath),
	}
}

// WithoutRebind is this backend for a caller that must never write to
// the data directory -- -backup (#1450 S2). Load still opens a file
// sealed under the pre-#1450 path binding, but hands its document back
// without re-sealing it; the server re-binds it at its next start. Save
// is unchanged.
func (b *EncryptedFileBackend) WithoutRebind() *EncryptedFileBackend {
	c := *b
	c.noRebind = true
	return &c
}

func (b *EncryptedFileBackend) Describe() string { return b.inner.Describe() }

func (b *EncryptedFileBackend) Close() error { return b.inner.Close() }

// ProtectedAtRest implements gauntlet's persist.AtRest: the file holds
// ciphertext only, so gauntlet's store accepts this backend without
// Options.AllowPlaintextAtRest (#1202).
func (b *EncryptedFileBackend) ProtectedAtRest() bool { return true }

// Load reads the file and decrypts it. A document that fails to decrypt
// -- the wrong key, or a file altered, written by something else, or
// belonging to another store -- is a real error, not a missing-document
// case: treating it as absent would silently reopen a first-run setup on
// top of a store that actually holds data. See internal/persist.Open,
// which every store's OpenWithBackend funnels through, for what happens
// with that error (issue #378: refuse to start rather than build a store
// around it).
//
// A file sealed before #1450, bound to its absolute path, opens here
// only at that path, and is re-saved under the store-name binding
// before Load returns, so the next open -- from wherever the data
// directory has moved to -- needs nothing from the old binding.
func (b *EncryptedFileBackend) Load(ctx context.Context) (Snapshot, error) {
	// Twice at most: a re-save that loses a race to another writer reads
	// the file again, which that writer will have re-bound or replaced;
	// losing it a second time returns the document as read.
	for attempt := 0; ; attempt++ {
		snap, err := b.inner.Load(ctx)
		if err != nil || !snap.Exists {
			return snap, err
		}
		// Both tests, not just the JSON one: a sealed envelope begins
		// with its magic, which no JSON document can, so either alone
		// would do today -- but which rule decides is the security
		// property here, and it is written out rather than left to that
		// coincidence.
		if !retention.HasSealMagic(snap.Payload) && json.Valid(snap.Payload) {
			// Named, so a caller can say what it means in its own words
			// (setup-only mode does). Never read, never rewritten
			// (#1202, ruling 19).
			return Snapshot{}, fmt.Errorf("persist: store %q (%s): %w", b.store, b.inner.path, ErrPlaintextDocument)
		}
		out, legacy, err := b.unseal(snap)
		if err != nil || !legacy || b.noRebind {
			return out, err
		}
		rebound, err := b.rebind(ctx, out)
		if err == nil {
			return rebound, nil
		}
		if errors.Is(err, ErrConflict) && attempt == 0 {
			continue
		}
		// Re-sealing is a best-effort migration: the document opened, so
		// hand it back whatever stopped the write (lost the race twice,
		// full disk, no permission). The file still reads under its old
		// binding and is re-sealed by a later load. (#1450, note 58486)
		rebindLog.Warn(fmt.Sprintf("%s, store %q, opened under its old path binding but could not be "+
			"re-sealed under its store name (%v); it stays readable and a later load re-seals it",
			b.inner.path, b.store, err))
		return out, nil
	}
}

// unseal opens the sealed document snap holds and hands back the
// plaintext under the same version. legacy reports that it opened only
// under the pre-#1450 path binding, and wants re-saving.
func (b *EncryptedFileBackend) unseal(snap Snapshot) (out Snapshot, legacy bool, err error) {
	plain, err := b.key.Open(retention.StateStoreKeyInfo, b.aad, snap.Payload)
	if err != nil {
		var legacyErr error
		plain, legacyErr = b.key.Open(retention.StateStoreKeyInfo, b.legacyAAD, snap.Payload)
		if legacyErr != nil {
			// The store-name error, not the legacy one: for anything
			// that is not an old file at its old path, that is the
			// binding that refused it.
			return Snapshot{}, false, fmt.Errorf("persist: store %q (%s) did not open: "+
				"it was not sealed under this key as this store, or it was altered: %w", b.store, b.inner.path, err)
		}
		legacy = true
	}
	snap.Payload = plain
	return snap, legacy, nil
}

// rebind re-seals a document opened under the pre-#1450 path binding
// under the store-name binding, written atomically through the wrapped
// FileBackend (WriteFileAtomic, with its version check against the bytes
// just read), and returns it under the new version.
func (b *EncryptedFileBackend) rebind(ctx context.Context, snap Snapshot) (Snapshot, error) {
	version, err := b.Save(ctx, snap.Payload, snap.Version)
	if err != nil {
		return Snapshot{}, fmt.Errorf("persist: store %q (%s): re-sealing under its store name: %w", b.store, b.inner.path, err)
	}
	rebindLog.Info(fmt.Sprintf("re-sealed %s, store %q, bound to its store name instead of its path, "+
		"so its data directory can now move (#1450); this happens once per file", b.inner.path, b.store))
	snap.Version = version
	return snap, nil
}

// Save encrypts payload and writes it through the wrapped FileBackend,
// which still performs the version check against whatever ciphertext is
// currently on disk.
func (b *EncryptedFileBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	sealed, err := b.key.Seal(retention.StateStoreKeyInfo, b.aad, payload)
	if err != nil {
		return 0, fmt.Errorf("persist: encrypting store %q (%s): %w", b.store, b.inner.path, err)
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
