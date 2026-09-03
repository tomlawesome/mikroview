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

// Writer holds the directory, the retention and the parts to ask on each
// write. It is created once at startup and Write is called on a ticker.
type Writer struct {
	dir   string
	keep  int
	parts []Part
}

// New returns a Writer that writes into dir, keeping the newest keep
// generations. keep is floored at 1: a snapshot series that keeps
// nothing would delete the file it just wrote, which is a rotation
// setting that silently disables the feature.
func New(dir string, keep int, parts ...Part) *Writer {
	if keep < 1 {
		keep = 1
	}
	return &Writer{dir: dir, keep: keep, parts: parts}
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
	payload, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	path := filepath.Join(w.dir, fileName(doc.Taken))

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
