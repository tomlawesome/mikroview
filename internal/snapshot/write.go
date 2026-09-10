// SPDX-License-Identifier: AGPL-3.0-only

package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Sealer is the encryption capability Writer and Load need: the same
// master key and AES-256-GCM/HKDF cipher internal/retention uses for the
// on-disk event history (#853), taken as a small interface rather than
// the concrete *retention.Key so this package does not have to import
// internal/retention -- which imports internal/store, which imports this
// package for the Part interface, and a direct import back would be a
// cycle. *retention.Key satisfies this interface as-is; main.go passes
// one straight through (see storage.go's key field).
type Sealer interface {
	// Seal and Open have exactly retention.Key's signatures -- see that
	// type's doc comments for the envelope shape and what info and aad
	// are for.
	Seal(info string, aad, plaintext []byte) ([]byte, error)
	Open(info string, aad, envelope []byte) ([]byte, error)
}

// warmRestartKeyInfo namespaces the keys Sealer derives for these
// documents, distinct from every other purpose the same master key is
// used for (#853: the persisted state store, the event history). Local
// to this package for the reason Sealer's doc comment gives.
const warmRestartKeyInfo = "mikroview/warm-restart/v1/"

// Writer holds the directory, the retention and the parts to ask on each
// write. It is created once at startup and Write is called on a ticker.
type Writer struct {
	dir   string
	keep  int
	parts []Part
	// key seals every document under the operator's retention key (#853).
	// Required: New refuses to build a Writer without one, because there
	// is no unencrypted mode for these documents any more than there is
	// for the state store or the event history -- see
	// docs/decisions/event-retention.md's amendment.
	key Sealer
}

// New returns a Writer that writes into dir, keeping the newest keep
// generations, sealed under key. keep is floored at 1: a snapshot series
// that keeps nothing would delete the file it just wrote, which is a
// rotation setting that silently disables the feature.
//
// key must not be nil. Callers decide whether warm-restart snapshots run
// at all by whether they have a key -- see main.go/snapshot.go, which
// mirrors the same "no key, no snapshots" gate history.go applies to the
// event history -- and never call New without one.
func New(dir string, keep int, key Sealer, parts ...Part) *Writer {
	if keep < 1 {
		keep = 1
	}
	return &Writer{dir: dir, keep: keep, parts: parts, key: key}
}

// Write asks every part for its bytes, writes one document, and rotates
// the directory down to the newest keep files. It returns the path
// written.
//
// A part that cannot export is logged and left out; the document is
// still written with everything else. The alternative -- failing the
// whole write -- means one broken subsystem costs every other subsystem
// its warm restart, and costs it silently, since the next boot just
// finds an older file.
//
// Errors are the ones that lost the document itself: the directory could
// not be created, the temp file could not be written, the rename failed.
// A rotation failure is not one of those, since the new document is
// already safely in place by then.
func (w *Writer) Write(now time.Time) (string, error) {
	doc := Document{
		Version: Version,
		Taken:   now.UTC(),
		Expires: now.UTC().Add(MaxAge),
		Parts:   make(map[string]json.RawMessage, len(w.parts)),
	}
	for _, p := range w.parts {
		raw, err := p.Export()
		if err != nil {
			logger.Warn(fmt.Sprintf("exporting %q for the snapshot failed: %v -- this snapshot is written without it, so a restart starts that part cold", p.Name(), err))
			continue
		}
		doc.Parts[p.Name()] = raw
	}

	path, err := w.writeDocument(doc)
	if err != nil {
		return "", err
	}
	w.prune()
	return path, nil
}

// writeDocument marshals and atomically publishes one document.
func (w *Writer) writeDocument(doc Document) (string, error) {
	if w.dir == "" {
		return "", fmt.Errorf("snapshot: no directory configured")
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return "", err
	}
	plain, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	path := filepath.Join(w.dir, fileName(doc.Taken))
	// Sealed under the same key and cipher internal/retention uses for
	// the event history (#853), with the file's own name as additional
	// data: an envelope moved to another snapshot's name fails to open
	// there rather than silently being accepted as it.
	payload, err := w.key.Seal(warmRestartKeyInfo, []byte(filepath.Base(path)), plain)
	if err != nil {
		return "", fmt.Errorf("snapshot: encrypting document: %w", err)
	}

	// The temp file gets a unique name (CreateTemp's O_EXCL), not a fixed
	// one: internal/persist measured a shared ".tmp" name publishing a
	// byte mixture of two writers' payloads, and a snapshot directory has
	// the same exposure the moment a CLI command and the server both
	// write one.
	f, err := os.CreateTemp(w.dir, fileName(doc.Taken)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	cleanup := func() {
		f.Close()
		os.Remove(tmp)
	}
	// 0600 to match every other document mikroview writes. A snapshot
	// holds no event lines, but it does hold counts and identifiers that
	// describe a network, which is not world-readable material.
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return "", err
	}
	if _, err := f.Write(payload); err != nil {
		cleanup()
		return "", err
	}
	// Rename is atomic in ordering, not durability: without these syncs a
	// crash can publish the new name over blocks still sitting in page
	// cache, which is exactly the truncated file the loader then has to
	// skip. Same reasoning as persist.FileBackend.Save.
	if err := f.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if d, err := os.Open(w.dir); err == nil {
		// Best effort: some filesystems refuse to sync a directory, and a
		// failure costs durability of the rename, not correctness of the
		// bytes.
		_ = d.Sync()
		_ = d.Close()
	}
	return path, nil
}

// prune deletes all but the newest keep snapshots, oldest first by name
// -- which is oldest first by time, because the name carries a
// fixed-width UTC stamp (see stampLayout).
//
// Sorting by name rather than by the Taken inside each file is
// deliberate: rotation must work on a directory containing a file too
// corrupt to parse, and it must not be steerable by a file's contents. A
// file whose name is not ours is never counted and never deleted.
//
// A file dated in the future therefore sits at the newest end and is
// never rotated out, costing one of the keep generations for as long as
// it is there. That is the cheaper failure: the alternative -- deleting
// files whose name is ahead of the clock -- throws away genuine, current
// snapshots the moment NTP steps a fast clock backwards. The newest real
// snapshot is kept either way, and Load refuses the future one.
func (w *Writer) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		logger.Warn(fmt.Sprintf("rotating snapshots in %s failed: %v -- older snapshots are still on disk", w.dir, err))
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isSnapshotName(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) <= w.keep {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-w.keep] {
		if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
			logger.Warn(fmt.Sprintf("removing old snapshot %s failed: %v", name, err))
		}
	}
}
