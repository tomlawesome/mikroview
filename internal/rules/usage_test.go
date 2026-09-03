// SPDX-License-Identifier: AGPL-3.0-only

package rules

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// flushForTest waits for s's write-behind writer to persist whatever is
// currently dirty -- issue #400 moved persistence off the caller's
// goroutine, so a test reopening the same path immediately after a
// Touch now needs an explicit synchronous checkpoint. See
// flags.flushForTest, the twin of this helper.
func flushForTest(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

// countingSaveBackend is an in-memory persist.Backend that counts Save
// calls -- see flags.countingSaveBackend, the twin of this type.
type countingSaveBackend struct {
	mu      sync.Mutex
	payload []byte
	version int64
	saves   int
}

func newCountingSaveBackend() *countingSaveBackend { return &countingSaveBackend{} }

func (b *countingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return persist.Snapshot{Payload: b.payload, Version: b.version, Exists: b.version != 0}, nil
}

func (b *countingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if expect != b.version {
		return 0, persist.ErrConflict
	}
	b.saves++
	b.payload = payload
	b.version++
	return b.version, nil
}

func (b *countingSaveBackend) Close() error     { return nil }
func (b *countingSaveBackend) Describe() string { return "counting test backend" }

func (b *countingSaveBackend) saveCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

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

// TestOpenMalformedFileFailsClosed pins issue #378's policy: a document
// that exists but cannot be parsed is refused outright, not treated as
// empty. See flags.TestOpenMalformedFileFailsClosed for the full
// reasoning -- same fix, same shape, applied through the same shared
// persist.Open helper.
func TestOpenMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("expected a non-nil error for a malformed file, want fail-closed")
	}
	if s != nil {
		t.Error("expected a nil store on a load failure -- a non-nil store here would still carry a live backend")
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
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s)

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

// TestPersistenceRateLimited confirms Touch doesn't hit the backend on
// every single call -- the same hot-path protection flags.persistMinInterval
// gives Add, needed here since Touch runs at the same per-event rate as
// internal/store/ring.go's totalByRule bump. Issue #400: persistence is
// now write-behind, so this counts backend Save calls through a fake
// persist.Backend rather than racing a file's mtime against a fixed
// sleep -- see flags.TestPersistLockedRateLimitsWrites for why that
// shape is the one this codebase has already been bitten by.
func TestPersistenceRateLimited(t *testing.T) {
	b := newCountingSaveBackend()
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	s.Touch("r1", now) // first write always attempts immediately (no prior attempt to debounce against)
	flushForTest(t, s)
	if got := b.saveCount(); got != 1 {
		t.Fatalf("expected the first Touch to persist immediately, got %d saves", got)
	}

	// Two more Touches, both well inside persistMinInterval, must
	// coalesce into a single additional attempt -- not one each.
	s.Touch("r1", now.Add(time.Millisecond))
	s.Touch("r2", now.Add(2*time.Millisecond))
	flushForTest(t, s)
	if got := b.saveCount(); got != 2 {
		t.Errorf("expected 2 Touches inside one debounce window to coalesce into 1 additional save (2 total), got %d", got)
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

// TestRecordingSinceStampedOnFreshStore covers issue #701's honesty
// bound: a store with no prior document (Open("") or a missing file)
// must still report a RecordingSince, so a caller building "distinct
// rules fired in the last N days" always has a window to bound its
// claim by. Bounded to "close to now" rather than an exact value since
// the stamp is taken from the wall clock during Open.
func TestRecordingSinceStampedOnFreshStore(t *testing.T) {
	before := time.Now()
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	after := time.Now()

	got := s.RecordingSince()
	if got.Before(before) || got.After(after) {
		t.Errorf("expected RecordingSince to be stamped with the load time, got %v, want between %v and %v", got, before, after)
	}
}

// TestRecordingSinceStampedOnMissingFile is TestRecordingSinceStampedOnFreshStore
// against a configured-but-not-yet-written path, the other documented
// "fresh store" case.
func TestRecordingSinceStampedOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	before := time.Now()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	after := time.Now()

	got := s.RecordingSince()
	if got.Before(before) || got.After(after) {
		t.Errorf("expected RecordingSince to be stamped with the load time, got %v, want between %v and %v", got, before, after)
	}
}

// TestRecordingSinceSurvivesRoundTripAndDoesNotMoveOnReopen pins the two
// load-bearing invariants from issue #701: the stamp set on first use
// persists across a save/load round trip, and reopening the same store
// later -- with the wall clock having moved on -- must not advance it.
// A stamp that crept forward on every restart would silently claim a
// wider "seen firing" window than the store actually covered, which is
// exactly the dishonesty the owner's decision rules out.
func TestRecordingSinceSurvivesRoundTripAndDoesNotMoveOnReopen(t *testing.T) {
	old := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = old }()

	path := filepath.Join(t.TempDir(), "rule-usage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := s.RecordingSince()
	if first.IsZero() {
		t.Fatal("expected a fresh store to already have a non-zero RecordingSince")
	}

	s.Touch("r1", time.Now())
	flushForTest(t, s)

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening persisted store: %v", err)
	}
	got := reopened.RecordingSince()
	if !got.Equal(first) {
		t.Errorf("expected RecordingSince to survive the round trip unchanged, got %v want %v", got, first)
	}

	// Touch again and reopen a second time, well after "first" -- the
	// stamp must still not have moved.
	reopened.Touch("r2", time.Now())
	flushForTest(t, reopened)
	reopenedAgain, err := Open(path)
	if err != nil {
		t.Fatalf("reopening persisted store a second time: %v", err)
	}
	if got := reopenedAgain.RecordingSince(); !got.Equal(first) {
		t.Errorf("expected RecordingSince to stay pinned across a second reopen, got %v want %v", got, first)
	}
}

// TestRecordingSinceInferredFromEarliestFirstSeenOnLegacyFile covers
// loading a document written before RecordingSince existed: the bare
// `[...]` array this package wrote pre-#701, with no stamp to read.
// Since the earliest FirstSeen across its records is the earliest
// moment this store can actually prove it was recording, that is what
// RecordingSince must report -- not the (later) moment this Open call
// happens to run.
func TestRecordingSinceInferredFromEarliestFirstSeenOnLegacyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	earliest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	data := `[
		{"rule":"r1","firstSeen":"2026-01-02T00:00:00Z","lastSeen":"2026-01-02T00:00:00Z","count":1},
		{"rule":"r2","firstSeen":"2026-01-01T00:00:00Z","lastSeen":"2026-01-01T00:00:00Z","count":1}
	]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a legacy bare-array file returned an error: %v", err)
	}
	if got := s.RecordingSince(); !got.Equal(earliest) {
		t.Errorf("expected RecordingSince to be inferred as the earliest FirstSeen (%v), got %v", earliest, got)
	}
}

// TestRecordingSinceInferredAsLoadTimeOnLegacyFileWithNoRecords is
// TestRecordingSinceInferredFromEarliestFirstSeenOnLegacyFile's
// companion for the edge case the spec calls out explicitly: a
// pre-#701 document with no records at all (every element null) has no
// FirstSeen to infer from, so RecordingSince falls back to the load
// time.
func TestRecordingSinceInferredAsLoadTimeOnLegacyFileWithNoRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule-usage.json")
	if err := os.WriteFile(path, []byte(`[null, null]`), 0o600); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a legacy empty-array file returned an error: %v", err)
	}
	after := time.Now()

	got := s.RecordingSince()
	if got.Before(before) || got.After(after) {
		t.Errorf("expected RecordingSince to fall back to the load time, got %v, want between %v and %v", got, before, after)
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
