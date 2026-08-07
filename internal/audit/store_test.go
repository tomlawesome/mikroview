package audit

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
	s.Record("admin", "user.create", "alice", "")
	if res := s.Query(Query{}); len(res.Entries) != 1 {
		t.Errorf("expected an in-memory-only store to still work, got %d entries", len(res.Entries))
	}
}

func TestOpenMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	if res := s.Query(Query{}); len(res.Entries) != 0 {
		t.Errorf("expected an empty store, got %d entries", len(res.Entries))
	}
}

func TestOpenMalformedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Error("expected a non-nil informational error for a malformed file")
	}
	if res := s.Query(Query{}); len(res.Entries) != 0 {
		t.Errorf("expected a malformed file to start empty, got %d entries", len(res.Entries))
	}
	// still usable despite the error
	s.Record("admin", "user.create", "alice", "")
	if res := s.Query(Query{}); len(res.Entries) != 1 {
		t.Error("store returned from Open() with a malformed file should still be usable")
	}
}

func TestRecordAssignsIncreasingIDsAndTimestamps(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	e1 := s.record("admin", "user.create", "alice", "role=user", now)
	e2 := s.record("admin", "token.create", "birdcage", "id=abc123", now.Add(time.Second))

	if e1.ID == 0 || e2.ID != e1.ID+1 {
		t.Errorf("expected strictly increasing IDs starting above 0, got %d then %d", e1.ID, e2.ID)
	}
	if e1.Actor != "admin" || e1.Action != "user.create" || e1.Target != "alice" || e1.Detail != "role=user" {
		t.Errorf("unexpected entry recorded: %+v", e1)
	}
	if !e1.Timestamp.Equal(now) {
		t.Errorf("expected the entry's Timestamp to be the time passed to record, got %v want %v", e1.Timestamp, now)
	}
}

func TestQueryReturnsOldestFirst(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.record("admin", "a1", "t1", "", now)
	s.record("admin", "a2", "t2", "", now.Add(time.Second))
	s.record("admin", "a3", "t3", "", now.Add(2*time.Second))

	res := s.Query(Query{})
	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}
	if res.Entries[0].Action != "a1" || res.Entries[1].Action != "a2" || res.Entries[2].Action != "a3" {
		t.Errorf("expected oldest-first order, got %+v", res.Entries)
	}
	if res.HasMore {
		t.Error("expected HasMore=false when everything fits under the limit")
	}
}

func TestQueryRespectsLimitAndReportsHasMore(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.record("admin", "action", "target", "", now.Add(time.Duration(i)*time.Second))
	}

	res := s.Query(Query{Limit: 2})
	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries under Limit: 2, got %d", len(res.Entries))
	}
	if !res.HasMore {
		t.Error("expected HasMore=true when more entries exist than the limit")
	}
	// The 2 returned should be the most recent 2 (walking backward from
	// newest), reported oldest-first.
	if res.Entries[0].Timestamp.Before(now.Add(2 * time.Second)) {
		t.Errorf("expected the most recent entries within the limit, got %+v", res.Entries)
	}
}

func TestQuerySinceExcludesOlderEntries(t *testing.T) {
	s, _ := Open("")
	base := time.Now()
	s.record("admin", "old", "t", "", base)
	s.record("admin", "new", "t", "", base.Add(time.Hour))

	res := s.Query(Query{Since: base.Add(30 * time.Minute)})
	if len(res.Entries) != 1 || res.Entries[0].Action != "new" {
		t.Errorf("expected Since to exclude the older entry, got %+v", res.Entries)
	}
}

func TestQueryUntilExcludesNewerEntries(t *testing.T) {
	s, _ := Open("")
	base := time.Now()
	s.record("admin", "old", "t", "", base)
	s.record("admin", "new", "t", "", base.Add(time.Hour))

	res := s.Query(Query{Until: base.Add(30 * time.Minute)})
	if len(res.Entries) != 1 || res.Entries[0].Action != "old" {
		t.Errorf("expected Until to exclude the newer entry, got %+v", res.Entries)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "audit.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Record("admin", "user.create", "alice", "role=admin")
	s1.Record("admin", "token.revoke", "tok-1", "")

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	res := s2.Query(Query{})
	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 persisted entries after reopening, got %d: %+v", len(res.Entries), res.Entries)
	}
	if res.Entries[0].Action != "user.create" || res.Entries[1].Action != "token.revoke" {
		t.Errorf("expected entries to survive persistence in order, got %+v", res.Entries)
	}
}

// TestRecordAfterReopenContinuesIDSequence proves ID allocation stays
// monotonic across a restart -- a fresh Store re-using an ID an earlier
// process already handed out would make two genuinely different entries
// share an ID, breaking anything that keys off it.
func TestRecordAfterReopenContinuesIDSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e1 := s1.Record("admin", "a1", "t", "")
	e2 := s1.Record("admin", "a2", "t", "")

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e3 := s2.Record("admin", "a3", "t", "")
	if e3.ID <= e2.ID {
		t.Errorf("expected the reopened store's next ID (%d) to continue past the last persisted ID (%d), got e1=%d", e3.ID, e2.ID, e1.ID)
	}
}

// TestPruneEvictsOldestOnceOverCapacity mirrors flags.Store's own prune
// test -- unlike flags (which only evicts cleared flags), every audit
// entry is equally eligible for eviction, so the oldest simply age out
// once maxEntries is exceeded.
func TestPruneEvictsOldestOnceOverCapacity(t *testing.T) {
	old := maxEntries
	maxEntries = 3
	defer func() { maxEntries = old }()

	s, _ := Open("")
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.record("admin", "action", "target", "", now.Add(time.Duration(i)*time.Second))
	}

	res := s.Query(Query{Limit: maxLimit})
	if len(res.Entries) != 3 {
		t.Fatalf("expected pruning to cap the store at 3 entries, got %d", len(res.Entries))
	}
	// The 3 oldest (indices 0,1) should have been evicted, leaving the
	// 3 most recent.
	if res.Entries[0].Timestamp.Before(now.Add(2 * time.Second)) {
		t.Errorf("expected the oldest entries to be evicted first, got %+v", res.Entries)
	}
}
