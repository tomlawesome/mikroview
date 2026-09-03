// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/snapshot"
)

// This file is main's half of warm restart (#795). internal/snapshot
// knows how to write and read a document; internal/store,
// internal/device and internal/engine each know how to describe
// themselves. What is left is the part only main can do: decide where
// the files go, put the state back *before* anything starts feeding the
// subsystems live data, and keep the ticker honest afterwards.
//
// The ordering is the whole load-bearing part. store.Import refuses a
// store that has already counted an event and engine.ImportState refuses
// an engine that has already evaluated one, both because restoring over
// live state is double counting nothing downstream could detect. So
// restoreSnapshot is called from run() between the last definition being
// registered and `go ingest(...)`: late enough that every part exists,
// early enough that none of them has seen traffic.

// snapshotShutdownBudget is how long the final snapshot may take before
// shutdown stops waiting for it. Deliberately the same figure
// closeStoreOnShutdown gives every write-behind store, and for the same
// reason: a process being asked to stop has a finite welcome, and the
// cost of giving up here is one cold start rather than lost data.
const snapshotShutdownBudget = 5 * time.Second

// snapshotDirectory is where this deployment's snapshots live.
//
// The configured value wins. Empty means "wherever mikroview's other
// state is", resolved through dataDir the same way the Postgres
// adoption marker is -- so an operator who moved the data directory does
// not end up with snapshots stranded on the default volume.
func snapshotDirectory(cfg config.Config) string {
	if dir := strings.TrimSpace(cfg.Snapshot.Dir); dir != "" {
		return dir
	}
	return filepath.Join(dataDir(cfg), "snapshots")
}

// engineSnapshotPart adapts *engine.Engine to snapshot.Part.
//
// The adapter lives here rather than in internal/engine because the
// engine's export/import pair is not shaped by the snapshot document:
// ExportState/ImportState exist as the chassis's own way to move
// definition state around, and giving the Engine a Name() method whose
// only meaning is "a key in a snapshot file" would put this file's
// vocabulary inside the evaluation chassis. Three lines here keep that
// boundary where internal/snapshot's doc comment says it is.
//
// The name is stable across releases: it is how a later boot finds these
// bytes again.
type engineSnapshotPart struct{ eng *engine.Engine }

func (p engineSnapshotPart) Name() string { return "engine" }

func (p engineSnapshotPart) Export() (json.RawMessage, error) { return p.eng.ExportState() }

func (p engineSnapshotPart) Import(raw json.RawMessage, taken, now time.Time) error {
	return p.eng.ImportState(raw, taken, now)
}

// usableSnapshotDir returns dir when snapshots can actually be written
// there, and "" when they cannot -- after saying so once, at startup.
//
// Never fatal. A snapshot is derived state whose loss costs one cold
// start; refusing to boot over an unwritable directory would cost the
// operator all their monitoring to protect their counters, which is the
// wrong way round for a security monitor. The check is done here, up
// front, rather than left to the first tick five minutes later: an
// operator who has just moved a volume and got the ownership wrong wants
// to find out while they are still watching the log.
//
// A create-and-remove probe rather than a permission bit: the answer
// that matters is whether this process can write a file here, and on a
// read-only mount, a full filesystem or an NFS export that squashes
// root, the mode says yes and the write still fails.
func usableSnapshotDir(log *slog.Logger, dir string) string {
	if dir == "" {
		log.Warn("no snapshot directory resolved, so no warm-restart snapshots will be written -- mikroview runs normally and starts cold after the next restart")
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Warn(fmt.Sprintf("snapshot directory %s cannot be created (%v) -- mikroview runs normally, but nothing is written and the next restart starts cold", dir, err))
		return ""
	}
	probe, err := os.CreateTemp(dir, ".writable-*")
	if err != nil {
		log.Warn(fmt.Sprintf("snapshot directory %s is not writable (%v) -- mikroview runs normally, but nothing is written and the next restart starts cold", dir, err))
		return ""
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return dir
}

// restoreSnapshot puts back the newest usable snapshot in dir and says
// in one line what it did.
//
// One line, because the operator's question after a restart is a single
// one -- "did it come back warm, and how warm" -- and the answer is the
// snapshot's own time plus which parts took their state. A part that was
// skipped is on the same line rather than in its own: skipped is normal
// (a snapshot written before that part existed), and a second line per
// part would make an ordinary boot look like a fault.
//
// Every rejected file has already cost its own line inside
// snapshot.Load, which is where the reason lives.
func restoreSnapshot(log *slog.Logger, dir string, now time.Time, parts ...snapshot.Part) {
	if dir == "" {
		return
	}
	report, err := snapshot.Load(dir, now, parts...)
	switch {
	case errors.Is(err, snapshot.ErrNoSnapshot):
		log.Info(fmt.Sprintf("no snapshot -- cold start: counters, detector windows and device first-seen dates all begin now (looked in %s)", dir))
	case err != nil:
		log.Warn(fmt.Sprintf("reading snapshots in %s failed: %v -- starting cold", dir, err))
	default:
		log.Info(fmt.Sprintf("warm start from %s: taken %s (%s ago), restored %s, cold %s",
			report.Path,
			report.Taken.UTC().Format(time.RFC3339),
			now.Sub(report.Taken).Round(time.Second),
			partList(report.Imported),
			partList(report.Skipped)))
	}
}

// partList renders a report's part names for the boot line, naming the
// empty case rather than printing "[]".
func partList(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, "+")
}

// runSnapshotWriter writes a snapshot every interval until ctx is done.
//
// Modelled on the engine tick driver and the blocklist refresher above
// it: one ticker, one select, and logging.Recover around the work so a
// panic in one export costs a snapshot rather than the process.
//
// The writes themselves never block ingest. Store and Registry each take
// their own read lock for the length of a marshal, and the engine's
// export borrows the evaluation goroutine between two events rather than
// putting a lock on the per-event path (see
// engine.runOnEvaluationGoroutine). What that buys is a cadence that
// stays honest under load, which is the only property this ticker needs.
//
// A nil writer means snapshots are switched off for this run (an
// unwritable directory) and the goroutine simply returns.
func runSnapshotWriter(ctx context.Context, log *slog.Logger, w *snapshot.Writer, interval time.Duration) {
	if w == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer logging.Recover(log)
				writeSnapshot(log, w)
			}()
		}
	}
}

// writeSnapshot writes one document. A failure is a warning, never
// fatal: the previous generation is still on disk, and the next tick
// tries again.
func writeSnapshot(log *slog.Logger, w *snapshot.Writer) {
	if _, err := w.Write(time.Now()); err != nil {
		log.Warn(fmt.Sprintf("writing a snapshot failed: %v -- the previous one is still on disk, and the next one is due at the configured interval", err))
	}
}

// writeFinalSnapshot writes one last snapshot as the process stops, so a
// planned restart resumes from the moment it went down rather than from
// the last tick.
//
// Bounded by budget -- snapshotShutdownBudget in production, taken as a
// parameter only so a test can prove the bound without waiting it out --
// and run on its own goroutine,
// because the alternative is a shutdown that hangs: the engine's export
// borrows the evaluation goroutine, and by this point Run has returned,
// so the borrow falls back to running inline (see
// engine.runOnEvaluationGoroutine) -- safe, but a subsystem that has
// wedged for some other reason must not be able to hold the process
// open. Giving up costs the last few minutes of counters, which is what
// the previous generation on disk already holds.
func writeFinalSnapshot(log *slog.Logger, w *snapshot.Writer, budget time.Duration) {
	if w == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer logging.Recover(log)
		defer close(done)
		writeSnapshot(log, w)
	}()
	select {
	case <-done:
	case <-time.After(budget):
		log.Warn(fmt.Sprintf("the final snapshot did not finish within %s -- the last periodic one is still on disk, so a restart comes back to that instead", budget))
	}
}
