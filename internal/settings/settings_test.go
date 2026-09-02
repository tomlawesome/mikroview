// SPDX-License-Identifier: AGPL-3.0-only

package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstRunHasNothingStored(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("a missing document is the first-run case, not an error: %v", err)
	}
	if _, ok := s.MaxMemory(); ok {
		t.Error("a fresh store reported a stored figure -- the config file's must apply")
	}
}

func TestSetMaxMemorySurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = 480 << 20
	if err := s.SetMaxMemory(want); err != nil {
		t.Fatalf("SetMaxMemory: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.MaxMemory()
	if !ok || got != want {
		t.Errorf("after a reopen: (%d, %v), want (%d, true) -- the stored figure must outlive the process", got, ok, int64(want))
	}
}

func TestSetMaxMemoryRefusesANonSize(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []int64{0, -1} {
		if err := s.SetMaxMemory(bad); err == nil {
			t.Errorf("SetMaxMemory(%d) was accepted", bad)
		}
	}
}

// Persistence switched off is a supported state, not a degraded one:
// the change applies to the running instance and simply does not
// survive a restart.
func TestNoBackendStillApplies(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaxMemory(64 << 20); err != nil {
		t.Fatalf("SetMaxMemory with no backend: %v", err)
	}
	if got, ok := s.MaxMemory(); !ok || got != 64<<20 {
		t.Errorf("MaxMemory = (%d, %v), want (%d, true)", got, ok, int64(64<<20))
	}
}

// A document that exists but is not readable as settings must refuse
// startup rather than be treated as "nothing stored" -- otherwise the
// first write silently overwrites whatever was really in there (#378).
func TestUnparseableDocumentRefusesToOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("an unparseable settings document opened cleanly")
	}
}

func TestNegativeStoredFigureRefusesToOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"store":{"maxMemoryBytes":-5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("a negative stored budget opened cleanly")
	}
}
