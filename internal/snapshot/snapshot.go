// SPDX-License-Identifier: AGPL-3.0-only

// Package snapshot writes and reads the rotated warm-restart documents:
// the derived state mikroview has learned since it started -- counters,
// per-minute buckets, baselines, device first/last-seen -- so a restart
// resumes from a few minutes ago instead of from nothing (#795).
//
// It is deliberately not internal/persist. A persisted store is one
// document that is the truth, written after every change and read back
// as authoritative; a snapshot is a disposable, dated copy of state that
// lives in memory, one file per generation, where the newest usable one
// wins and the rest are rotated away. The two share only the atomic
// temp-file/fsync/rename idiom, which is copied here rather than layered
// on FileBackend: FileBackend's compare-and-swap versioning exists to
// stop two writers clobbering one document, and there is nothing to
// clobber in a series where every write has its own name.
//
// This package knows nothing about what it carries. Each subsystem
// implements Part -- exporting and importing its own bytes -- so
// internal/store, internal/device and internal/engine keep their state's
// shape to themselves and this package only ever handles
// json.RawMessage.
//
// Never in a snapshot: event lines. The ring in internal/store holds raw
// log records and addresses, and #795 settled that writing those to disk
// would change the data-custody promise in SECURITY.md. Snapshots hold
// counts, timestamps and identifiers only. internal/routerstate is out
// for its own reason -- see that package's doc comment.
package snapshot

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var logger = logging.New("snapshot")

// Version is the document schema version. A loader that meets a document
// it does not recognise starts cold rather than guessing: the parts'
// bytes are opaque here, so there is nothing this package could migrate
// even if it wanted to.
const Version = 1

// MaxAge is how long a snapshot claims to be worth loading. It is
// written into each document as Expires rather than compared at load
// time against a constant, so a document carries its own answer: a
// future mikroview that shortens or lengthens this does not
// retroactively change what an already-written file promised.
//
// A day: long enough that a host rebooted overnight still comes back
// warm, short enough that counters restored from it are still recognisably
// about the same traffic. Past it the state is stale enough that
// presenting it as current would mislead, so the loader starts cold.
const MaxAge = 24 * time.Hour

// futureSkew is how far ahead of the loader's clock a snapshot's Taken
// may be before the file is refused.
//
// Some tolerance is needed because the writer's clock and the loader's
// are the same clock at different times, and NTP steps it: a snapshot
// written seconds before a small correction is legitimate and would
// otherwise be thrown away. Beyond a minute, "taken in the future" means
// either a badly wrong clock or a file planted to win the newest-first
// race for good, and neither is worth loading. #795 asks for exactly
// this: a snapshot from the future is skipped, never preferred.
const futureSkew = time.Minute

const (
	filePrefix = "snapshot-"
	fileSuffix = ".json"
	// stampLayout puts the taken time in the name in UTC, zero-padded and
	// fixed-width, so sorting the directory by name sorts it by age. Every
	// other ordering (mtime, a sequence number in a sidecar file) needs
	// state or a syscall per file to recover something the name can just
	// carry.
	stampLayout = "20060102T150405Z"
)

// ErrNoSnapshot is returned by Load when no file in the directory was
// usable -- including when the directory does not exist at all.
//
// It is not a failure. Every reason to reject a snapshot (nothing
// written yet, all of them expired, a corrupt file, a schema from
// another version) leaves the caller in the same place: start cold, and
// say so in the log. A missing directory is the first-run case and gets
// the same answer as an empty one, so callers do not need to tell them
// apart. Real I/O trouble -- an unreadable directory -- is returned as
// itself.
var ErrNoSnapshot = errors.New("snapshot: no usable snapshot")

// Part is one subsystem's slice of a snapshot document.
//
// The interface lives here and the implementations live with the state
// they describe (internal/store, internal/device, internal/engine), so
// this package imports none of them and a subsystem's internals never
// leak into the document format.
type Part interface {
	// Name is the document key this part's bytes are stored under, and
	// must be stable across releases: it is how a later load finds them
	// again. Short and lowercase, e.g. "store", "devices", "engine".
	Name() string

	// Export marshals the part's current state. An error means this part
	// is left out of the document and the write continues -- one
	// subsystem being unable to describe itself must not cost every
	// other subsystem its warm restart.
	Export() (json.RawMessage, error)

	// Import restores the part from raw. taken is when the snapshot was
	// written and now is the loader's clock, passed rather than read
	// inside so a part can age its own contents (dropping buckets that
	// have since fallen off the end of a window, say) and so tests can
	// drive both clocks.
	//
	// Import is called at boot, before the subsystem is fed live data.
	// An error is logged and the remaining parts still import: a
	// partially warm start beats a cold one.
	Import(raw json.RawMessage, taken, now time.Time) error
}

// Document is one snapshot file.
type Document struct {
	Version int       `json:"version"`
	Taken   time.Time `json:"taken"`
	// Expires is Taken plus MaxAge, written out rather than recomputed so
	// the file states its own shelf life -- see MaxAge.
	Expires time.Time `json:"expires"`
	// Parts is keyed by Part.Name. Unknown keys are ignored on load
	// (a part removed from a later build), and a key a registered part
	// expects but does not find is simply not imported (a part added in a
	// later build, meeting an older snapshot).
	Parts map[string]json.RawMessage `json:"parts"`
}

// fileName is the name a snapshot taken at t is written under.
func fileName(t time.Time) string {
	return filePrefix + t.UTC().Format(stampLayout) + fileSuffix
}

// isSnapshotName reports whether a directory entry is one of ours.
// Anything else in the directory -- an operator's notes, another tool's
// files -- is left alone by both the loader and the rotation.
func isSnapshotName(name string) bool {
	return len(name) > len(filePrefix)+len(fileSuffix) &&
		strings.HasPrefix(name, filePrefix) &&
		strings.HasSuffix(name, fileSuffix)
}
