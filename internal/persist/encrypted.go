// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
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
}

// NewEncryptedFileBackend wraps the file at path so every read decrypts
// and every write encrypts under key. key must not be nil -- callers
// decide whether a store persists at all by whether they have a key (see
// storage.go's backendFor), and this type has no unencrypted mode to fall
// back to.
func NewEncryptedFileBackend(path string, key *retention.Key) *EncryptedFileBackend {
	return &EncryptedFileBackend{inner: NewFileBackend(path), key: key, aad: []byte(path)}
}

func (b *EncryptedFileBackend) Describe() string { return b.inner.Describe() }

func (b *EncryptedFileBackend) Close() error { return b.inner.Close() }

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
