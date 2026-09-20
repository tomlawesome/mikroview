// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"path/filepath"
	"testing"
	"time"
)

// #1240: the crossing and its acknowledgement are the instance's, so
// both have to survive the instance being restarted.

func TestUpgradeSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !first.NoteUpgrade("v0.4.0", "v0.5.0", time.Now()) {
		t.Fatal("NoteUpgrade refused a real crossing")
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	u, ok := reopened.Upgrade()
	if !ok {
		t.Fatal("the crossing did not survive a reopen -- the notice would vanish on a restart")
	}
	if u.Previous != "v0.4.0" || u.Current != "v0.5.0" {
		t.Errorf("read back %q -> %q, want v0.4.0 -> v0.5.0", u.Previous, u.Current)
	}
	if u.Acknowledged() {
		t.Error("read back as acknowledged with nobody having pressed done")
	}
}

// The issue's "done when": done hides it across a restart.
func TestAcknowledgementSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	if _, ok := first.AcknowledgeUpgrade("admin", time.Now()); !ok {
		t.Fatal("AcknowledgeUpgrade found nothing to acknowledge")
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	u, _ := reopened.Upgrade()
	if !u.Acknowledged() {
		t.Fatal("the acknowledgement did not survive a reopen -- the notice would come back on every restart")
	}
	if u.AcknowledgedBy != "admin" {
		t.Errorf("acknowledgedBy = %q, want admin", u.AcknowledgedBy)
	}
}

// A first install and an ordinary restart are both not crossings.
func TestNoUpgradeOnFirstInstallOrRestart(t *testing.T) {
	s := New()
	if s.NoteUpgrade("", "v0.5.0", time.Now()) {
		t.Error("a first install (no previous version) recorded an upgrade")
	}
	if s.NoteUpgrade("v0.5.0", "v0.5.0", time.Now()) {
		t.Error("a restart at the same version recorded an upgrade")
	}
	if s.NoteUpgrade(" v0.5.0\n", "v0.5.0", time.Now()) {
		t.Error("a marker file with a trailing newline read as a version change")
	}
	if _, ok := s.Upgrade(); ok {
		t.Error("an upgrade exists after none was recorded")
	}
}

// Every restart after an upgrade re-notes the same pair. Doing that must
// not undo the operator's own done -- the notice coming back from the
// dead on the next restart is the whole defect this guards.
func TestReNotingTheSameCrossingKeepsTheAcknowledgement(t *testing.T) {
	s := New()
	noticed := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	s.NoteUpgrade("v0.4.0", "v0.5.0", noticed)
	s.AcknowledgeUpgrade("admin", noticed.Add(time.Minute))

	if s.NoteUpgrade("v0.4.0", "v0.5.0", noticed.Add(time.Hour)) {
		t.Error("re-noting the same crossing rewrote the record")
	}
	u, _ := s.Upgrade()
	if !u.Acknowledged() {
		t.Fatal("a restart un-acknowledged the upgrade")
	}
	if !u.NoticedAt.Equal(noticed) {
		t.Errorf("noticedAt = %v, want the moment it was first noticed (%v)", u.NoticedAt, noticed)
	}
}

// A later upgrade is a new thing to say, and an earlier admin's done
// does not settle it.
func TestALaterCrossingIsNotAcknowledgedByAnEarlierOne(t *testing.T) {
	s := New()
	s.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	s.AcknowledgeUpgrade("admin", time.Now())

	if !s.NoteUpgrade("v0.5.0", "v0.6.0", time.Now()) {
		t.Fatal("a second, later crossing was not recorded")
	}
	u, _ := s.Upgrade()
	if u.Previous != "v0.5.0" || u.Current != "v0.6.0" {
		t.Errorf("read back %q -> %q, want v0.5.0 -> v0.6.0", u.Previous, u.Current)
	}
	if u.Acknowledged() {
		t.Error("the new crossing arrived already acknowledged")
	}
}

// Acknowledging twice keeps the first admin's name: they are the one who
// did the work.
func TestAcknowledgeIsIdempotent(t *testing.T) {
	s := New()
	s.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	s.AcknowledgeUpgrade("first", time.Now())
	u, ok := s.AcknowledgeUpgrade("second", time.Now())
	if !ok || u.AcknowledgedBy != "first" {
		t.Errorf("second acknowledgement = %+v (ok=%v), want the first admin's name kept", u, ok)
	}
}

// Nothing to acknowledge is not an error to store: the caller decides
// what to tell the client, and no audit entry is written for a click
// against nothing.
func TestAcknowledgeWithNoUpgrade(t *testing.T) {
	if _, ok := New().AcknowledgeUpgrade("admin", time.Now()); ok {
		t.Error("acknowledged an upgrade that does not exist")
	}
}
