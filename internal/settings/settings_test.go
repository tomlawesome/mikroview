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

func TestFirstRunHasNoHistoryStored(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.History(); ok {
		t.Error("a fresh store reported stored history settings -- the config file's must apply")
	}
}

func TestSetHistorySurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := History{Enabled: true, Days: 14, MaxBytes: 2 << 30}
	if err := s.SetHistory(want); err != nil {
		t.Fatalf("SetHistory: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.History()
	if !ok || got != want {
		t.Errorf("after a reopen: (%+v, %v), want (%+v, true)", got, ok, want)
	}
}

// The switch's whole point is that a stored "off" beats the config
// file's "on" -- so "nothing stored" cannot be read off Enabled, which
// is false in both cases. Days is what carries that distinction.
func TestStoredOffIsDistinguishableFromNothingStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHistory(History{Enabled: false, Days: 30, MaxBytes: 1 << 30}); err != nil {
		t.Fatalf("SetHistory: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.History()
	if !ok {
		t.Fatal("a stored off reads back as nothing stored -- the config file's switch would win and turn it back on")
	}
	if got.Enabled {
		t.Error("a stored off read back as on")
	}
}

func TestSetHistoryRefusesWhatItCannotStore(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []History{
		{Enabled: true, Days: 0, MaxBytes: 1 << 30},
		{Enabled: true, Days: -1, MaxBytes: 1 << 30},
		{Enabled: true, Days: 30, MaxBytes: 0},
		{Enabled: true, Days: 30, MaxBytes: -1},
	} {
		if err := s.SetHistory(bad); err == nil {
			t.Errorf("SetHistory(%+v) was accepted", bad)
		}
	}
	if _, ok := s.History(); ok {
		t.Error("a refused change was stored anyway")
	}
}

// The memory figure and the history settings share one document, so
// writing either must not erase the other.
func TestTheTwoSettingsDoNotOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaxMemory(480 << 20); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHistory(History{Enabled: true, Days: 7, MaxBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.MaxMemory(); !ok || got != 480<<20 {
		t.Errorf("MaxMemory = (%d, %v) after a history change, want (%d, true)", got, ok, int64(480<<20))
	}
	if got, ok := reopened.History(); !ok || got.Days != 7 {
		t.Errorf("History = (%+v, %v), want 7 days stored", got, ok)
	}
}

func TestNegativeStoredHistoryRefusesToOpen(t *testing.T) {
	for _, doc := range []string{
		`{"history":{"days":-1}}`,
		`{"history":{"maxBytes":-1}}`,
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil {
			t.Errorf("%s opened cleanly", doc)
		}
	}
}
