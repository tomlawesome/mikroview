// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/snapshot"
	"github.com/tomlawesome/mikroview/internal/store"
)

// testSnapshotKey is the retention key these tests seal and load
// snapshots under (#853): warm restart has no unencrypted mode any more
// than the state store does, so every New/restoreSnapshot call in this
// file needs one.
var testSnapshotKey = func() *retention.Key {
	k, err := retention.NewKeyFromMaterial(bytes.Repeat([]byte{0x37}, retention.MinKeyBytes))
	if err != nil {
		panic(err)
	}
	return k
}()

// captureLog gives a test the lines a seam actually wrote, which is the
// whole observable behaviour of "log once and carry on".
func captureLog(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// A snapshot directory that cannot be written costs one log line and
// nothing else. Refusing to start over it would trade the operator's
// whole monitoring for their counters, which is the wrong way round
// (#795).
func TestUnwritableSnapshotDirLogsAndCarriesOn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write into a 0500 directory")
	}
	parent := t.TempDir()
	readOnly := filepath.Join(parent, "read-only")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })

	log, buf := captureLog(t)
	// A directory *inside* the read-only one: MkdirAll cannot create it.
	if got := usableSnapshotDir(log, filepath.Join(readOnly, "snapshots")); got != "" {
		t.Errorf("usableSnapshotDir = %q, want \"\" so the caller runs without snapshots", got)
	}
	if !strings.Contains(buf.String(), "cannot be created") {
		t.Errorf("nothing in the log says why there are no snapshots:\n%s", buf.String())
	}

	// And the read-only directory itself, which exists but refuses a
	// write -- the case a mode check alone would pass.
	log, buf = captureLog(t)
	if got := usableSnapshotDir(log, readOnly); got != "" {
		t.Errorf("usableSnapshotDir = %q for an unwritable directory, want \"\"", got)
	}
	if !strings.Contains(buf.String(), "not writable") {
		t.Errorf("an unwritable directory was accepted or unexplained:\n%s", buf.String())
	}
}

// No writer means snapshots are off for this run. Every entry point has
// to be a no-op in that state, because main wires them unconditionally.
func TestSnapshotSeamsAreNoOpsWithoutADirectory(t *testing.T) {
	log, buf := captureLog(t)
	restoreSnapshot(log, "", testSnapshotKey, time.Now())
	writeFinalSnapshot(log, nil, snapshotShutdownBudget)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runSnapshotWriter(ctx, log, nil, time.Millisecond)
	if buf.Len() != 0 {
		t.Errorf("a run without snapshots logged during operation, which would repeat every interval:\n%s", buf.String())
	}
}

func TestUsableSnapshotDirCreatesTheDirectory(t *testing.T) {
	log, buf := captureLog(t)
	dir := filepath.Join(t.TempDir(), "snapshots")
	if got := usableSnapshotDir(log, dir); got != dir {
		t.Fatalf("usableSnapshotDir = %q, want %q", got, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the directory was reported usable but does not exist: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("snapshot directory mode is %o, want 700 -- it holds counts and identifiers that describe a network", perm)
	}
	// The probe file must not survive: a stray dotfile in a rotated
	// directory is the kind of thing an operator later has to guess at.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the writability probe left %d file(s) behind: %v", len(entries), entries)
	}
	if buf.Len() != 0 {
		t.Errorf("a working snapshot directory logged something:\n%s", buf.String())
	}
}

// The configured directory wins; empty follows the data directory, so
// an operator who moved their state does not leave snapshots behind on
// the default volume.
func TestSnapshotDirectoryResolution(t *testing.T) {
	if got := snapshotDirectory(config.Config{Snapshot: config.Snapshot{Dir: "/srv/snapshots"}}); got != "/srv/snapshots" {
		t.Errorf("snapshotDirectory = %q, want the configured /srv/snapshots", got)
	}
	cfg := config.Config{}
	cfg.Auth.StorePath = "/mnt/data/accounts.json"
	if got := snapshotDirectory(cfg); got != "/mnt/data/snapshots" {
		t.Errorf("snapshotDirectory = %q, want it to follow the moved data directory to /mnt/data/snapshots", got)
	}
	if got := snapshotDirectory(config.Config{}); got != config.DefaultDataDir+"/snapshots" {
		t.Errorf("snapshotDirectory = %q, want %q", got, config.DefaultDataDir+"/snapshots")
	}
}

// A cold start says so in one line and names where it looked, because
// "why did it not use the snapshot" is the question that follows.
func TestRestoreSnapshotOnAnEmptyDirectoryReportsAColdStart(t *testing.T) {
	log, buf := captureLog(t)
	restoreSnapshot(log, t.TempDir(), testSnapshotKey, time.Now(), store.New(16, time.Hour).SnapshotPart())
	if !strings.Contains(buf.String(), "cold start") {
		t.Errorf("an empty snapshot directory did not report a cold start:\n%s", buf.String())
	}
}

// The three parts main registers go round the loop together: written by
// the writer main builds, read back by the loader main calls, under the
// names a later release has to keep finding them by.
func TestSnapshotRoundTripThroughTheWiredParts(t *testing.T) {
	dir := t.TempDir()
	log, buf := captureLog(t)

	written := store.New(64, time.Hour)
	written.Insert(store.Event{Action: store.ActionDrop, RuleLabel: "wan-in"})
	writtenDevices := device.NewRegistry(nil)
	writtenDevices.Resolve("192.168.1.1", time.Now())
	eng := engine.New()

	parts := []snapshot.Part{written.SnapshotPart(), writtenDevices.SnapshotPart(), engineSnapshotPart{eng: eng}}
	writeSnapshot(log, snapshot.New(dir, 6, testSnapshotKey, parts...))
	if buf.Len() != 0 {
		t.Fatalf("writing a snapshot logged a problem:\n%s", buf.String())
	}

	restored := store.New(64, time.Hour)
	restoredDevices := device.NewRegistry(nil)
	restoreSnapshot(log, dir, testSnapshotKey, time.Now(),
		restored.SnapshotPart(), restoredDevices.SnapshotPart(), engineSnapshotPart{eng: engine.New()})

	line := buf.String()
	if !strings.Contains(line, "warm start") {
		t.Fatalf("a snapshot was written and not loaded back:\n%s", line)
	}
	if !strings.Contains(line, "restored store+devices+engine") {
		t.Errorf("not every part took its state back, so a key changed or a part refused:\n%s", line)
	}
	if restored.RestoredTo().IsZero() {
		t.Error("the store does not know it was restored, so /api/stats will report a cold start")
	}
	if got := restored.Stats().Total; got != 1 {
		t.Errorf("restored total = %d, want the 1 event counted before the snapshot", got)
	}
	if got := restored.Stats().Count; got != 0 {
		t.Errorf("the restored store holds %d event(s) -- a snapshot carries counters, never the lines themselves", got)
	}
	if len(restoredDevices.List()) != 1 {
		t.Errorf("restored %d device(s), want the one that was in the snapshot", len(restoredDevices.List()))
	}
}

// The engine's key in the document is a name a later boot has to find
// again, so it is pinned rather than left to the adapter's spelling.
func TestEngineSnapshotPartIsNamedEngine(t *testing.T) {
	if got := (engineSnapshotPart{eng: engine.New()}).Name(); got != "engine" {
		t.Errorf("engineSnapshotPart.Name() = %q, want \"engine\" -- changing it orphans every snapshot already on disk", got)
	}
}

// The final write is bounded: a wedged subsystem costs the last few
// minutes of counters, not a process that will not stop.
func TestWriteFinalSnapshotReturnsEvenWhenAPartHangs(t *testing.T) {
	dir := t.TempDir()
	log, buf := captureLog(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeFinalSnapshot(log, snapshot.New(dir, 6, testSnapshotKey, blockingPart{release: make(chan struct{})}), 50*time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("writeFinalSnapshot did not give up, so shutdown would hang on a wedged part")
	}
	if !strings.Contains(buf.String(), "did not finish") {
		t.Errorf("giving up on the final snapshot was silent:\n%s", buf.String())
	}
}

// blockingPart never returns from Export, standing in for a subsystem
// that has wedged for some reason of its own.
type blockingPart struct{ release chan struct{} }

func (p blockingPart) Name() string { return "blocking" }

func (p blockingPart) Export() (json.RawMessage, error) {
	<-p.release
	return []byte("null"), nil
}

func (p blockingPart) Import(json.RawMessage, time.Time, time.Time) error { return nil }
