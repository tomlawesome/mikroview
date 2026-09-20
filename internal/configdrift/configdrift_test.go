// SPDX-License-Identifier: AGPL-3.0-only

package configdrift

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStoreDismissesNothing(t *testing.T) {
	s := New()
	if s.Dismissed("v1.2.3") {
		t.Error("a fresh store should not report any version dismissed")
	}
}

func TestDismissedIsTrueOnlyForTheDismissedVersion(t *testing.T) {
	s := New()
	s.Dismiss("v1.2.3")
	if !s.Dismissed("v1.2.3") {
		t.Error("Dismissed(v1.2.3) should be true right after Dismiss(v1.2.3)")
	}
	if s.Dismissed("v1.3.0") {
		t.Error("dismissing v1.2.3 must not silently dismiss a different version that has something new to say")
	}
}

// A second Dismiss for a later version replaces the first: only the
// most recent dismissal has ever mattered, since the notice for an
// earlier version cannot still be showing once a later one has run.
func TestDismissingALaterVersionReplacesTheEarlierOne(t *testing.T) {
	s := New()
	s.Dismiss("v1.2.3")
	s.Dismiss("v1.3.0")
	if s.Dismissed("v1.2.3") {
		t.Error("v1.2.3 should no longer read as the dismissed version")
	}
	if !s.Dismissed("v1.3.0") {
		t.Error("v1.3.0 should read as the dismissed version")
	}
}

func TestDismissedOfEmptyVersionIsAlwaysFalse(t *testing.T) {
	s := New()
	s.Dismiss("")
	if s.Dismissed("") {
		t.Error(`Dismissed("") must never be true -- an empty version names nothing to have dismissed`)
	}
}

// The whole point of persisting this at all: an operator who dismisses
// the notice must not see it again after a restart.
func TestDismissalSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config-drift.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Dismiss("v1.2.3")

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if !reopened.Dismissed("v1.2.3") {
		t.Error("a dismissal did not survive being reopened from the same path")
	}
}

// An empty path is the documented "persistence not configured" case:
// dismissing still works for the life of the process, it just does not
// survive a restart.
func TestEmptyPathIsInMemoryOnly(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	s.Dismiss("v1.2.3")
	if !s.Dismissed("v1.2.3") {
		t.Error("an in-memory-only store should still honour a dismissal made against it")
	}
}

// A document that exists but cannot be parsed is a hard error, the same
// contract every sibling store here follows -- silently treating it as
// empty would let #1218's own notice reappear for a version an operator
// already dismissed, immediately after whatever corrupted the file.
func TestUnparsableDocumentIsAHardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config-drift.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected Open to fail on an unparsable document, got nil")
	}
}
