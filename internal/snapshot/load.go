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

// Report describes what a Load actually did, for the caller's own boot
// log and for the "counters from a snapshot N minutes old" statement the
// UI carries (#795).
type Report struct {
	// Path is the file the state came from.
	Path string
	// Taken is when that snapshot was written -- the age the UI reports,
	// and the instant a part is told to age its contents against.
	Taken time.Time
	// Imported names the parts that took their state back, in the order
	// they were given to Load.
	Imported []string
	// Skipped names the registered parts that did not: either the
	// document carried nothing under that name (an older snapshot, from
	// before the part existed) or the part refused what it was given.
	// Files rejected before this one are logged rather than reported --
	// the caller's question is what state it now has, not how many bad
	// files were passed over.
	Skipped []string
}

// Load restores the newest usable snapshot in dir and returns what it
// did.
//
// Files are tried newest first by name. One is rejected -- with a single
// log line, and never a crash -- when it cannot be read or parsed, when
// its schema version is not this build's, when it claims to have been
// taken in the future (see futureSkew), or when it has expired (see
// MaxAge). Rejection falls through to the next-newest file, so one
// truncated write does not cost a warm restart.
//
// The first acceptable file is imported part by part. A part that errors
// is logged and skipped while the rest still import: a partly warm start
// is worth more than a cold one.
//
// ErrNoSnapshot means nothing was usable, including the first-run case
// where dir does not exist. It is a normal cold start, not a failure --
// see ErrNoSnapshot.
func Load(dir string, now time.Time, parts ...Part) (Report, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{}, ErrNoSnapshot
		}
		return Report{}, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isSnapshotName(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	// Newest first: the name's fixed-width UTC stamp sorts
	// chronologically, so this needs no file contents and no stat calls.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	for _, name := range names {
		path := filepath.Join(dir, name)
		doc, ok := readDocument(path, now)
		if !ok {
			continue
		}
		return importDocument(path, doc, now, parts), nil
	}
	return Report{}, ErrNoSnapshot
}

// readDocument reads and vets one file. Every rejection costs exactly
// one log line saying which file and why, because the operator's
// question after a cold start is always "why did it not use the
// snapshot".
func readDocument(path string, now time.Time) (Document, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: %v", filepath.Base(path), err))
		return Document{}, false
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: not readable as a snapshot (%v)", filepath.Base(path), err))
		return Document{}, false
	}
	if doc.Version != Version {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: schema version %d, this build reads version %d", filepath.Base(path), doc.Version, Version))
		return Document{}, false
	}
	if doc.Taken.IsZero() {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: no taken time, so its age is unknown", filepath.Base(path)))
		return Document{}, false
	}
	if doc.Taken.After(now.Add(futureSkew)) {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: taken %s, which is ahead of this host's clock", filepath.Base(path), doc.Taken.UTC().Format(time.RFC3339)))
		return Document{}, false
	}
	if !doc.Expires.IsZero() && doc.Expires.Before(now) {
		logger.Warn(fmt.Sprintf("skipping snapshot %s: expired %s ago, starting those counters cold", filepath.Base(path), now.Sub(doc.Expires).Round(time.Minute)))
		return Document{}, false
	}
	return doc, true
}

// importDocument hands each registered part its own bytes.
func importDocument(path string, doc Document, now time.Time, parts []Part) Report {
	report := Report{Path: path, Taken: doc.Taken}
	for _, p := range parts {
		raw, ok := doc.Parts[p.Name()]
		if !ok {
			// An older snapshot, written before this part existed. Not a
			// fault and not worth a log line every boot: the part simply
			// starts cold, which the report already says.
			report.Skipped = append(report.Skipped, p.Name())
			continue
		}
		if err := p.Import(raw, doc.Taken, now); err != nil {
			logger.Warn(fmt.Sprintf("restoring %q from snapshot %s failed: %v -- that part starts cold, the rest of the snapshot still loaded", p.Name(), filepath.Base(path), err))
			report.Skipped = append(report.Skipped, p.Name())
			continue
		}
		report.Imported = append(report.Imported, p.Name())
	}
	return report
}
