// SPDX-License-Identifier: AGPL-3.0-only

package rules

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenEmptyPathIsUsable(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	s.Touch("r1", time.Now())
	if len(s.List()) != 1 {
		t.Errorf("expected an in-memory-only store to still work, got %d entries", len(s.List()))
	}
}

func TestOpenMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected an empty store, got %d entries", len(s.List()))
	}
}

func TestOpenSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	data := `[null, {"rule":"r1","firstSeen":"2026-01-01T00:00:00Z","lastSeen":"2026-01-01T00:00:00Z","count":1}, null]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path) // must not panic
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected the one real entry to survive, got %d: %+v", len(list), list)
	}
	if list[0].Rule != "r1" {
		t.Errorf("expected the real entry's data to be intact, got %+v", list[0])
	}
}

func TestOpenMalformedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Error("expected a non-nil informational error for a malformed file")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected a malformed file to start empty, got %d entries", len(s.List()))
	}
	// still usable despite the error
	s.Touch("r1", time.Now())
	if len(s.List()) != 1 {
		t.Error("store returned from Open() with a malformed file should still be usable")
	}
}

func TestTouchIgnoresBlankRuleLabel(t *testing.T) {
	s, _ := Open("")
	s.Touch("", time.Now())
	if len(s.List()) != 0 {
		t.Errorf("expected a blank rule label to be a no-op, got %d entries", len(s.List()))
	}
}

func TestTouchCreatesFirstSeenOnce(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Touch("r1", now)
	s.Touch("r1", now.Add(time.Hour))
	s.Touch("r1", now.Add(2*time.Hour))

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected one entry, got %d: %+v", len(list), list)
	}
	u := list[0]
	if !u.FirstSeen.Equal(now) {
		t.Errorf("expected FirstSeen to stay pinned to the first Touch, got %v", u.FirstSeen)
	}
	if !u.LastSeen.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("expected LastSeen to track the most recent Touch, got %v", u.LastSeen)
	}
	if u.Count != 3 {
		t.Errorf("expected Count=3, got %d", u.Count)
	}
}

func TestTouchTracksMultipleRulesIndependently(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Touch("r1", now)
	s.Touch("r2", now)
	s.Touch("r1", now.Add(time.Minute))

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 distinct rule entries, got %d: %+v", len(list), list)
	}
	byRule := map[string]Usage{}
	for _, u := range list {
		byRule[u.Rule] = u
	}
	if byRule["r1"].Count != 2 {
		t.Errorf("expected r1 count=2, got %d", byRule["r1"].Count)
	}
	if byRule["r2"].Count != 1 {
		t.Errorf("expected r2 count=1, got %d", byRule["r2"].Count)
	}
}

// TestPersistenceRoundTrip covers the actual point of this package: a
// long-lived record that survives a restart, unlike internal/store's
// ring buffer. persistMinInterval is shrunk to 0 for the duration of
// the test so every Touch persists immediately rather than needing a
// real-time wait between calls (same technique flags' tests use for
// persistMinInterval).
func TestPersistenceRoundTrip(t *testing.T) {
	old := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = old }()

	path := filepath.Join(t.TempDir(), "rule-usage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	s.Touch("r1", now)
	s.Touch("r2", now.Add(time.Minute))
	s.Touch("r1", now.Add(2*time.Minute))

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected a persisted file to exist: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening persisted store: %v", err)
	}
	list := reopened.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 persisted rule entries after reopen, got %d: %+v", len(list), list)
	}
	byRule := map[string]Usage{}
	for _, u := range list {
		byRule[u.Rule] = u
	}
	if byRule["r1"].Count != 2 {
		t.Errorf("expected r1 count=2 to survive persistence, got %d", byRule["r1"].Count)
	}
	if !byRule["r1"].FirstSeen.Equal(now) {
		t.Errorf("expected r1 FirstSeen to survive persistence, got %v want %v", byRule["r1"].FirstSeen, now)
	}
	if !byRule["r1"].LastSeen.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("expected r1 LastSeen to survive persistence, got %v want %v", byRule["r1"].LastSeen, now.Add(2*time.Minute))
	}
	if byRule["r2"].Count != 1 {
		t.Errorf("expected r2 count=1 to survive persistence, got %d", byRule["r2"].Count)
	}
}

// TestPersistenceRateLimited confirms Touch doesn't hit disk on every
// single call -- the same hot-path protection flags.persistMinInterval
// gives Add, needed here since Touch runs at the same per-event rate as
// internal/store/ring.go's totalByRule bump.
func TestPersistenceRateLimited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	s.Touch("r1", now) // first write always happens (lastPersist is zero)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected the first Touch to persist: %v", err)
	}
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	s.Touch("r1", now.Add(time.Millisecond)) // well within persistMinInterval
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("expected a Touch within persistMinInterval to skip the disk write")
	}
}

func TestStaleReturnsOnlyEntriesOlderThanMaxAge(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Touch("fresh", now.Add(-1*time.Hour))
	s.Touch("stale1", now.Add(-40*24*time.Hour))
	s.Touch("stale2", now.Add(-31*24*time.Hour))
	s.Touch("borderline", now.Add(-30*24*time.Hour)) // exactly at the boundary

	stale := s.Stale(30*24*time.Hour, now)

	var got []string
	for _, u := range stale {
		got = append(got, u.Rule)
	}
	want := []string{"stale1", "stale2"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestStaleEmptyWhenNothingOldEnough(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Touch("r1", now)
	s.Touch("r2", now.Add(-time.Hour))

	if stale := s.Stale(24*time.Hour, now); len(stale) != 0 {
		t.Errorf("expected no stale entries, got %+v", stale)
	}
}
