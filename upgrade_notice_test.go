// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/setup"
)

// #1240: the version marker is the only record of what this data
// directory last ran, and logVersionAndMigration overwrites it. These
// pin the hand-off -- what it read has to reach the setup ledger, which
// is what the notice is served from.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

// markerIn points the version marker at a test's own directory for the
// duration of that test.
func markerIn(t *testing.T, dir string) {
	t.Helper()
	previous := versionMarkerPath
	versionMarkerPath = filepath.Join(dir, "version")
	t.Cleanup(func() { versionMarkerPath = previous })
}

// The issue's "done when", first half: a data directory whose version
// file says v0.4.0 produces the notice with that version.
func TestUpgradeNoticeFromAVersionMarker(t *testing.T) {
	dir := t.TempDir()
	markerIn(t, dir)
	if err := os.WriteFile(versionMarkerPath, []byte("v0.4.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	previous := logVersionAndMigration(quietLogger(), 0)
	if previous != "v0.4.0" {
		t.Fatalf("logVersionAndMigration returned %q, want v0.4.0 -- the marker is gone by now, so this is the only chance to read it", previous)
	}

	ledger, err := setup.Open(filepath.Join(dir, "setup.json"))
	if err != nil {
		t.Fatalf("setup.Open: %v", err)
	}
	if !ledger.NoteUpgrade(previous, "v0.5.0", time.Now()) {
		t.Fatal("the ledger refused the crossing main hands it")
	}
	u, ok := ledger.Upgrade()
	if !ok || u.Previous != "v0.4.0" {
		t.Errorf("the ledger holds %+v (found=%v), want a crossing from v0.4.0", u, ok)
	}

	// And the marker now names this build, so the next restart is a
	// restart rather than a second upgrade.
	stamped, err := os.ReadFile(versionMarkerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stamped) != version {
		t.Errorf("the marker was left saying %q, want %q", stamped, version)
	}
}

// The other half: a fresh data directory shows nothing.
func TestUpgradeNoticeSilentOnAFreshDataDir(t *testing.T) {
	dir := t.TempDir()
	markerIn(t, dir)

	previous := logVersionAndMigration(quietLogger(), 0)
	if previous != "" {
		t.Fatalf("logVersionAndMigration returned %q on a data directory with no marker, want empty", previous)
	}

	ledger, err := setup.Open(filepath.Join(dir, "setup.json"))
	if err != nil {
		t.Fatalf("setup.Open: %v", err)
	}
	ledger.NoteUpgrade(previous, version, time.Now())
	if u, ok := ledger.Upgrade(); ok {
		t.Errorf("a first install recorded a crossing (%+v) -- the notice would greet a new operator with an upgrade they never made", u)
	}
}
