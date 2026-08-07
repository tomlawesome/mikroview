// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
)

// FileBackend is the default, zero-infrastructure backend: one JSON file
// per store, atomically replaced on write.
//
// It reproduces exactly what every store did before this package
// existed, including the file mode and the write-temp-then-rename dance,
// so switching a store onto persist.Backend is not itself a behaviour
// change.
type FileBackend struct {
	path string
}

func NewFileBackend(path string) *FileBackend { return &FileBackend{path: path} }

func (b *FileBackend) Describe() string { return "file " + b.path }

func (b *FileBackend) Close() error { return nil }

// Load reads the file. A missing file is the normal first-run case and
// returns a zero Snapshot with a nil error; an unreadable one is a real
// error, because treating it as absent is how a corrupt accounts file
// silently becomes a fresh install (see internal/auth's own
// fail-closed handling, which depends on being able to tell these
// apart).
func (b *FileBackend) Load(ctx context.Context) (Snapshot, error) {
	data, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	return Snapshot{Payload: data, Version: contentVersion(data), Exists: true}, nil
}

// contentVersion derives a store's version from its bytes rather than
// from the file's modification time.
//
// mtime was the obvious choice and is wrong. Filesystem timestamp
// granularity is coarser than the interval between two quick writes, so
// two successive saves routinely produce the *same* mtime -- which made
// the compare-and-swap in Save silently accept stale writes. The shared
// contract test caught that on its first run: the file backend passed a
// write it should have refused, while the Postgres backend refused it.
//
// A content hash has no such granularity. Different bytes give a
// different version, so a caller whose copy is stale is detected
// regardless of how fast the writes came.
//
// FNV-1a, not a cryptographic hash: this guards against accidental
// overwrite between cooperating processes, not against an attacker who
// can already write the file. A collision would let one stale write
// through -- the same failure the file backend had unconditionally
// before this, now reduced to a ~2^-64 chance.
func contentVersion(data []byte) int64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	v := int64(h.Sum64() & 0x7fffffffffffffff)
	if v == 0 {
		// 0 means "nothing stored" to Save; never hand it back for a
		// file that does exist.
		return 1
	}
	return v
}

// Save atomically replaces the file.
//
// expect is checked against the file's current modification time, so a
// write that would clobber someone else's change is refused rather than
// silently winning. expect == 0 additionally requires that no file
// exists yet.
func (b *FileBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	if b.path == "" {
		return 0, fmt.Errorf("persist: no file path configured")
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return 0, err
	}

	current, readErr := os.ReadFile(b.path)
	switch {
	case os.IsNotExist(readErr):
		if expect != 0 {
			// Caller believed a document existed. It doesn't -- someone
			// removed it underneath them.
			return 0, ErrConflict
		}
	case readErr != nil:
		return 0, readErr
	default:
		if expect == 0 {
			return 0, ErrConflict // caller expected to be creating it
		}
		if contentVersion(current) != expect {
			return 0, ErrConflict
		}
	}

	// 0600, matching every store's previous behaviour: these documents
	// hold password hashes and API-token digests.
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, b.path); err != nil {
		os.Remove(tmp)
		return 0, err
	}

	return contentVersion(payload), nil
}
