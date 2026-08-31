// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"context"
	"fmt"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// countingSaveBackend is an in-memory persist.Backend that counts Save
// calls -- used to prove write-behind coalescing by call count rather
// than by racing a file write against a fixed sleep (the flakiness
// TestPersistLockedRateLimitsWrites' own history already warns about).
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

func (b *countingSaveBackend) lastPayload() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.payload
}

// flushForTest waits for s's write-behind writer to persist whatever is
// currently dirty -- issue #400 moved persistence off the caller's
// goroutine, so a test that used to rely on Add/Clear/etc. returning
// only once the write had already landed (Open() immediately reopening
// the same path, for instance) now needs an explicit synchronous
// checkpoint. Fails the test outright rather than returning an error,
// since every call site treats a flush failure as a broken test setup.
func flushForTest(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

func TestOpenEmptyPathIsUsable(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	s.Add(TypePortScan, "1.2.3.4", "test", time.Now())
	if len(s.List()) != 1 {
		t.Errorf("expected an in-memory-only store to still work, got %d flags", len(s.List()))
	}
}

func TestOpenMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected an empty store, got %d flags", len(s.List()))
	}
}

// A JSON array containing null is syntactically valid, so it unmarshals
// without error into a slice with a nil *Flag element -- before the
// fix, the very next line (indexing f.ID) paniced on that nil pointer,
// contradicting Open's own documented "malformed file never blocks
// startup" contract.
func TestOpenSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	data := `{"flags":[null, {"id":"port_scan:1.2.3.4","type":"port_scan","target":"1.2.3.4"}, null]}`
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
	if list[0].Target != "1.2.3.4" {
		t.Errorf("expected the real flag's data to be intact, got %+v", list[0])
	}
}

// TestOpenMalformedFileFailsClosed pins issue #378's policy: a document
// that exists but cannot be parsed is refused outright, not treated as
// empty. Before the fix, Open returned a usable store with its backend
// still attached, and the caller (main.go) logged a warning and kept
// running -- so the very next persist call overwrote the malformed file
// with near-empty in-memory state, destroying whatever was recoverable
// in it. Open must now return (nil, err), so there is no store left
// holding that backend to write over the file.
func TestOpenMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	original := []byte("not valid json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("expected a non-nil error for a malformed file, want fail-closed")
	}
	if s != nil {
		t.Error("expected a nil store on a load failure -- a non-nil store here would still carry a live backend")
	}
	// The whole point: nothing about a failed Open may touch the file
	// that failed to load. Before the fix this held anyway on the very
	// next line (Open never writes), but the actual bug was the store
	// *returned* by Open still holding a live backend, so its first
	// Add/persist call overwrote this file with near-empty state -- a
	// step this test can no longer even reach, since s is nil.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Errorf("the file changed across a failed Open: before %q, after %q", original, after)
	}
}

func TestAddCreatesNewFlag(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "1.2.3.4", "20 ports in 60s", now)

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(list))
	}
	f := list[0]
	if f.Type != TypePortScan || f.Target != "1.2.3.4" || f.Detail != "20 ports in 60s" {
		t.Errorf("unexpected flag: %+v", f)
	}
	if f.Count != 1 || f.Cleared {
		t.Errorf("expected Count=1, Cleared=false, got %+v", f)
	}
	if !f.FirstSeen.Equal(now) || !f.LastSeen.Equal(now) {
		t.Errorf("expected FirstSeen=LastSeen=now, got %+v", f)
	}
}

func TestAddLeavesConfidenceNil(t *testing.T) {
	s, _ := Open("")
	s.Add(TypePortScan, "1.2.3.4", "20 ports in 60s", time.Now())

	f := s.List()[0]
	if f.Confidence != nil {
		t.Errorf("expected a plain Add to leave Confidence nil (not scored), got %v", *f.Confidence)
	}
}

func TestAddWithConfidenceSetsConfidence(t *testing.T) {
	s, _ := Open("")
	s.AddWithConfidence(TypeActivitySpike, "1.2.3.4", "5x baseline", 73, time.Now())

	f := s.List()[0]
	if f.Confidence == nil || *f.Confidence != 73 {
		t.Errorf("expected Confidence=73, got %+v", f.Confidence)
	}
}

func TestAddUpdatesExistingActiveFlagInPlace(t *testing.T) {
	s, _ := Open("")
	t0 := time.Now()
	t1 := t0.Add(time.Minute)

	s.Add(TypePortScan, "1.2.3.4", "15 ports in 60s", t0)
	s.Add(TypePortScan, "1.2.3.4", "22 ports in 60s", t1)

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected re-firing to update in place, got %d flags", len(list))
	}
	f := list[0]
	if f.Count != 2 {
		t.Errorf("Count = %d, want 2", f.Count)
	}
	if !f.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen changed on update: got %v, want %v", f.FirstSeen, t0)
	}
	if !f.LastSeen.Equal(t1) {
		t.Errorf("LastSeen = %v, want %v", f.LastSeen, t1)
	}
	if f.Detail != "22 ports in 60s" {
		t.Errorf("Detail = %q, want the latest observation", f.Detail)
	}
}

func TestAddRevivesClearedFlagAsFreshEpisode(t *testing.T) {
	s, _ := Open("")
	t0 := time.Now()
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)

	s.Add(TypePortScan, "1.2.3.4", "first episode", t0)
	id := s.List()[0].ID
	if !s.Clear(id, t1) {
		t.Fatal("Clear() on a freshly-added flag should succeed")
	}

	s.Add(TypePortScan, "1.2.3.4", "second episode", t2)
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected the revived flag to reuse the same ID, got %d flags", len(list))
	}
	f := list[0]
	if f.Cleared {
		t.Error("expected re-firing to un-clear the flag")
	}
	if f.Count != 1 {
		t.Errorf("expected Count to reset to 1 for a fresh episode, got %d", f.Count)
	}
	if !f.FirstSeen.Equal(t2) {
		t.Errorf("expected FirstSeen to reset to the new episode's start, got %v", f.FirstSeen)
	}
}

func TestClearUnknownOrAlreadyClearedReturnsFalse(t *testing.T) {
	s, _ := Open("")
	if s.Clear("nonexistent", time.Now()) {
		t.Error("Clear() on an unknown ID should return false")
	}

	s.Add(TypePortScan, "1.2.3.4", "x", time.Now())
	id := s.List()[0].ID
	if !s.Clear(id, time.Now()) {
		t.Fatal("first Clear() should succeed")
	}
	if s.Clear(id, time.Now()) {
		t.Error("second Clear() on an already-cleared flag should return false")
	}
}

func TestListOrdersMostRecentlyActiveFirst(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "1.1.1.1", "oldest", now)
	s.Add(TypePortScan, "2.2.2.2", "newest", now.Add(time.Minute))
	s.Add(TypePortScan, "3.3.3.3", "middle", now.Add(30*time.Second))

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 flags, got %d", len(list))
	}
	if list[0].Target != "2.2.2.2" || list[1].Target != "3.3.3.3" || list[2].Target != "1.1.1.1" {
		t.Errorf("unexpected order: %v, %v, %v", list[0].Target, list[1].Target, list[2].Target)
	}
}

// TestPersistLockedRateLimitsWrites proves the debounce actually skips
// backend writes within the window (not just that it compiles) -- a
// sustained re-fire burst (the scenario this exists for) must not hit
// the backend once per event. Issue #400: persistence moved off the
// caller's goroutine onto persist.WriteBehind's own writer, so this no
// longer reads the file directly (there is no guarantee a write has
// landed by the time Add returns) -- it counts backend Save calls
// through a fake persist.Backend instead, which is a claim about
// behaviour rather than about timing either way.
func TestPersistLockedRateLimitsWrites(t *testing.T) {
	b := newCountingSaveBackend()
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	s.Add(TypePortScan, "1.1.1.1", "first", now)
	flushForTest(t, s) // the very first write attempts immediately; wait for it as a baseline
	if got := b.saveCount(); got != 1 {
		t.Fatalf("expected the first Add to write immediately, got %d saves", got)
	}

	// Within the debounce window: must coalesce into the next flush
	// rather than producing its own attempt.
	s.Add(TypePortScan, "2.2.2.2", "second, still within the window", now)
	s.Add(TypePortScan, "3.3.3.3", "third, still within the window", now)
	flushForTest(t, s)
	if got := b.saveCount(); got != 2 {
		t.Errorf("expected 2 total saves after 3 Adds inside one debounce window (1 immediate + 1 coalesced flush), got %d", got)
	}
	if len(s.List()) != 3 {
		t.Fatalf("in-memory state should always have all 3 regardless of persistence timing, got %d", len(s.List()))
	}
	if payload := b.lastPayload(); !strings.Contains(string(payload), "3.3.3.3") {
		t.Errorf("expected the flushed write to include all 3 flags, got:\n%s", payload)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "flags.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s1.Add(TypeCriticalPort, "5.6.7.8", "6 attempts on port 22 in 5m", now)
	s1.AddWithConfidence(TypeActivitySpike, "9.9.9.9", "5x baseline", 82, now)
	id := s1.List()[0].ID
	s1.Clear(id, now.Add(time.Minute))
	// #400: persistence is now write-behind (persist.WriteBehind), so a
	// reopen immediately after these calls would otherwise race the
	// writer goroutine -- flush explicitly first, the synchronous
	// checkpoint this type exists to provide.
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	list := s2.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 persisted flags after reopening, got %d: %+v", len(list), list)
	}

	for _, f := range list {
		if f.Target == "9.9.9.9" && (f.Confidence == nil || *f.Confidence != 82) {
			t.Errorf("expected Confidence=82 to survive persistence, got %+v", f.Confidence)
		}
	}

	var sawCleared, sawActive bool
	for _, f := range list {
		if f.Cleared {
			sawCleared = true
		} else {
			sawActive = true
		}
	}
	if !sawCleared || !sawActive {
		t.Errorf("expected one cleared and one active flag to survive reopening, got %+v", list)
	}
}

func TestPruneEvictsOldestClearedFlagsOverCap(t *testing.T) {
	orig := maxFlags
	maxFlags = 3
	defer func() { maxFlags = orig }()

	s, _ := Open("")
	now := time.Now()

	// Two cleared flags (oldest first) plus one active one -- adding a
	// fourth should evict the oldest cleared entry, never the active one.
	s.Add(TypePortScan, "1.1.1.1", "x", now)
	s.Clear(flagID(TypePortScan, "1.1.1.1"), now.Add(time.Minute))

	s.Add(TypePortScan, "2.2.2.2", "x", now.Add(2*time.Minute))
	s.Clear(flagID(TypePortScan, "2.2.2.2"), now.Add(3*time.Minute))

	s.Add(TypePortScan, "3.3.3.3", "active, never cleared", now.Add(4*time.Minute))

	s.Add(TypePortScan, "4.4.4.4", "pushes the store over the cap", now.Add(5*time.Minute))

	list := s.List()
	if len(list) != maxFlags {
		t.Fatalf("expected pruning to hold the store at %d, got %d: %+v", maxFlags, len(list), list)
	}
	for _, f := range list {
		if f.Target == "1.1.1.1" {
			t.Error("expected the oldest cleared flag (1.1.1.1) to be evicted")
		}
		if f.Target == "3.3.3.3" && f.Cleared {
			t.Error("the active flag should never be evicted or altered by pruning")
		}
	}
}

func TestAddReportsNewEpisode(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	if isNew := s.Add(TypePortScan, "1.1.1.1", "first", now); !isNew {
		t.Error("expected the first-ever raise to report a new episode")
	}
	if isNew := s.Add(TypePortScan, "1.1.1.1", "re-fire", now.Add(time.Second)); isNew {
		t.Error("expected a plain re-fire of an active flag to not report a new episode")
	}

	id := flagID(TypePortScan, "1.1.1.1")
	s.Clear(id, now.Add(2*time.Second))
	if isNew := s.Add(TypePortScan, "1.1.1.1", "revived", now.Add(3*time.Second)); !isNew {
		t.Error("expected a revival from cleared to report a new episode")
	}
}

// TestTimeSeriesCountsOnlyNewEpisodes is the granularity contract
// FlagsChart depends on: a bucket counts an episode start (first-ever
// raise, or a revival from Cleared), never a plain re-fire of an
// already-active flag -- the same "isNew" distinction Add already
// reports and onRaise already keys off of, kept consistent here rather
// than introducing a second, different notion of flag "activity."
func TestTimeSeriesCountsOnlyNewEpisodes(t *testing.T) {
	s, _ := Open("")
	// Pinned to :10s past the minute, not time.Now() directly -- the
	// offsets below run up to +4s, and a bare time.Now() base
	// occasionally lands within 4s of a minute boundary, pushing the
	// revival into the next minute's bucket and flaking this test (hit
	// in CI once already). A fixed offset from a truncated minute
	// Pinned to the start of the current minute. TimeSeries buckets by
	// minute and this test reads the final bucket, so a plain
	// time.Now() made it flaky: started late enough in a minute, the
	// +4s revival below landed in the *next* bucket and the count came
	// back 1 instead of 2. Truncating guarantees every timestamp here
	// shares one bucket regardless of when the suite runs.
	now := time.Now().Truncate(time.Minute)

	s.Add(TypePortScan, "1.1.1.1", "first", now)
	series := s.TimeSeries()
	last := series[len(series)-1]
	if got := last.ByType[TypePortScan]; got != 1 {
		t.Fatalf("expected 1 new episode counted after the first raise, got %d (bucket=%+v)", got, last)
	}

	// A plain re-fire of the still-active flag must not bump the bucket.
	s.Add(TypePortScan, "1.1.1.1", "re-fire", now.Add(time.Second))
	s.Add(TypePortScan, "1.1.1.1", "re-fire again", now.Add(2*time.Second))
	series = s.TimeSeries()
	last = series[len(series)-1]
	if got := last.ByType[TypePortScan]; got != 1 {
		t.Errorf("expected re-fires to leave the bucket at 1, got %d (bucket=%+v)", got, last)
	}

	// A revival from Cleared is a new episode and must bump it again.
	id := flagID(TypePortScan, "1.1.1.1")
	s.Clear(id, now.Add(3*time.Second))
	s.Add(TypePortScan, "1.1.1.1", "revived", now.Add(4*time.Second))
	series = s.TimeSeries()
	last = series[len(series)-1]
	if got := last.ByType[TypePortScan]; got != 2 {
		t.Errorf("expected a revival to bump the bucket to 2, got %d (bucket=%+v)", got, last)
	}
}

// TestTimeSeriesBucketsByType proves distinct Types land in separate
// counters within the same minute, and that a Type with a zero count
// for a given minute is omitted from that bucket's ByType map (same
// "omit rather than list every known value" convention as
// internal/store/ring.go's TimeBucket.ByAction).
func TestTimeSeriesBucketsByType(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "1.1.1.1", "d1", now)
	s.Add(TypeCriticalPort, "2.2.2.2", "d2", now)
	s.Add(TypeCriticalPort, "3.3.3.3", "d3", now)

	series := s.TimeSeries()
	last := series[len(series)-1]
	if last.ByType[TypePortScan] != 1 {
		t.Errorf("expected 1 port_scan episode, got %+v", last.ByType)
	}
	if last.ByType[TypeCriticalPort] != 2 {
		t.Errorf("expected 2 critical_port episodes, got %+v", last.ByType)
	}
	if _, ok := last.ByType[TypeGlobalSpike]; ok {
		t.Errorf("expected a Type with zero episodes this minute to be omitted, got %+v", last.ByType)
	}
}

// TestTimeSeriesReturnsFixedWidthWindow proves TimeSeries always returns
// flagTimeSeriesMinutes entries, oldest first, even when nothing has
// ever been raised -- same fixed-width-window contract as
// internal/store/ring.go's Stats.TimeSeries, so FlagsChart can render a
// consistent x-axis regardless of how much history actually exists yet.
func TestTimeSeriesReturnsFixedWidthWindow(t *testing.T) {
	s, _ := Open("")
	series := s.TimeSeries()
	if len(series) != flagTimeSeriesMinutes {
		t.Fatalf("expected %d buckets, got %d", flagTimeSeriesMinutes, len(series))
	}
	for i, b := range series {
		if len(b.ByType) != 0 {
			t.Errorf("expected an empty store to report zero episodes in every bucket, bucket %d = %+v", i, b)
		}
	}
	for i := 1; i < len(series); i++ {
		if !series[i].Time.After(series[i-1].Time) {
			t.Fatalf("expected strictly increasing bucket times, got %v then %v at index %d", series[i-1].Time, series[i].Time, i)
		}
	}
}

// TestTimeSeriesExcludedTargetNeverCounted proves a permanently-excluded
// (Type, Target) pair -- which add() silently no-ops for, before isNew
// is ever determined -- doesn't sneak into the bucket history either.
func TestTimeSeriesExcludedTargetNeverCounted(t *testing.T) {
	s, _ := Open("")
	s.Exclude(TypePortScan, "1.1.1.1")
	s.Add(TypePortScan, "1.1.1.1", "should be a no-op", time.Now())

	series := s.TimeSeries()
	last := series[len(series)-1]
	if got := last.ByType[TypePortScan]; got != 0 {
		t.Errorf("expected an excluded target's Add to never reach the bucket counter, got %d", got)
	}
}

func TestRaiseConfidenceFloorOnlyRaises(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 20, now)

	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.9", 10)
	if c := *s.List()[0].Confidence; c != 20 {
		t.Errorf("expected a lower floor to be a no-op, got confidence %d", c)
	}

	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.9", 90)
	if c := *s.List()[0].Confidence; c != 90 {
		t.Errorf("expected a higher floor to raise confidence, got %d", c)
	}

	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.9", 50)
	if c := *s.List()[0].Confidence; c != 90 {
		t.Errorf("expected a subsequent lower floor to still be a no-op, got %d", c)
	}

	// Unknown ID -- no-op, not an error.
	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.99", 100)
}

func TestRaiseConfidenceFloorSurvivesAPlainRefire(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 20, now)
	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.9", 90)

	// A later re-fire with a lower fresh confidence must not discard the
	// reputation floor established earlier in the same episode.
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "re-fire, lower overshoot", 15, now.Add(time.Second))
	if c := *s.List()[0].Confidence; c != 90 {
		t.Errorf("expected the reputation floor to survive a plain re-fire's lower confidence, got %d", c)
	}

	// A later re-fire with a higher fresh confidence than the floor
	// should win on its own merits.
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "re-fire, higher overshoot", 95, now.Add(2*time.Second))
	if c := *s.List()[0].Confidence; c != 95 {
		t.Errorf("expected a fresh confidence above the floor to win, got %d", c)
	}
}

func TestRaiseConfidenceFloorResetOnRevival(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 20, now)
	s.RaiseConfidenceFloor(TypeCriticalPort, "203.0.113.9", 90)
	s.Clear(flagID(TypeCriticalPort, "203.0.113.9"), now.Add(time.Second))

	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "revived episode", 15, now.Add(2*time.Second))
	if c := *s.List()[0].Confidence; c != 15 {
		t.Errorf("expected a revived episode to start its confidence history fresh (no stale reputation floor), got %d", c)
	}
}

func TestAddWithDetailPersistsEvidenceAndCountry(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	evidence := Evidence{
		Ports:             []int{22, 3389},
		Hosts:             []string{"192.168.1.5"},
		NAT:               &NATInfo{IP: "10.0.0.5", Port: 8080, Raw: "NAT (10.0.0.5:8080->192.168.1.5:80)"},
		Pairs:             []HostPort{{Host: "192.168.1.5", Port: 22}},
		PairsTotal:        214,
		PairsTotalIsFloor: true,
		SrcMAC:            "aa:bb:cc:dd:ee:ff",
	}
	s.AddWithDetail(TypePortScan, "203.0.113.9", "detail", 42, evidence, "US", now)

	f := s.List()[0]
	if f.Country != "US" {
		t.Errorf("expected Country to be set, got %q", f.Country)
	}
	if len(f.Evidence.Ports) != 2 || len(f.Evidence.Hosts) != 1 || f.Evidence.NAT == nil {
		t.Fatalf("expected the full evidence to be persisted, got %+v", f.Evidence)
	}
	if f.Evidence.NAT.IP != "10.0.0.5" || f.Evidence.NAT.Port != 8080 {
		t.Errorf("expected NAT detail to round-trip, got %+v", f.Evidence.NAT)
	}
	// #654: Pairs/PairsTotal/PairsTotalIsFloor/SrcMAC round-trip the same
	// way every other Evidence field above already does -- additive
	// fields, no special handling anywhere in the store.
	if len(f.Evidence.Pairs) != 1 || f.Evidence.Pairs[0] != (HostPort{Host: "192.168.1.5", Port: 22}) {
		t.Errorf("expected Pairs to be persisted, got %+v", f.Evidence.Pairs)
	}
	if f.Evidence.PairsTotal != 214 {
		t.Errorf("expected PairsTotal to be persisted, got %d", f.Evidence.PairsTotal)
	}
	if !f.Evidence.PairsTotalIsFloor {
		t.Error("expected PairsTotalIsFloor to be persisted as true")
	}
	if f.Evidence.SrcMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("expected SrcMAC to be persisted, got %q", f.Evidence.SrcMAC)
	}

	// A plain re-fire recomputes evidence fresh, same as Detail already
	// does -- #654's own "prospective only" requirement: Pairs/SrcMAC
	// from the earlier call must not linger once a re-fire's evidence
	// doesn't carry them.
	s.AddWithDetail(TypePortScan, "203.0.113.9", "detail", 42, Evidence{Ports: []int{22}}, "US", now.Add(time.Second))
	f = s.List()[0]
	if len(f.Evidence.Ports) != 1 || len(f.Evidence.Hosts) != 0 {
		t.Errorf("expected evidence to reflect the latest call, got %+v", f.Evidence)
	}
	if len(f.Evidence.Pairs) != 0 || f.Evidence.PairsTotal != 0 || f.Evidence.PairsTotalIsFloor || f.Evidence.SrcMAC != "" {
		t.Errorf("expected Pairs/PairsTotal/PairsTotalIsFloor/SrcMAC to be overwritten (not accumulated) by the re-fire, got %+v", f.Evidence)
	}
}

// TestAddProvisionalSetsProvisionalMarker is the in-memory half of
// Flag.Provisional's contract: AddProvisional(..., provisional=true, ...)
// sets it, and every other Add* entry point (which never takes a
// provisional argument) leaves it false.
func TestAddProvisionalSetsProvisionalMarker(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.AddProvisional(TypePortScan, "203.0.113.9", "detail", 10, Evidence{}, "US", true, now)
	f := s.List()[0]
	if !f.Provisional {
		t.Fatal("expected Provisional to be true after AddProvisional(..., true, ...)")
	}

	s.AddWithDetail(TypeCriticalPort, "203.0.113.10", "detail", 10, Evidence{}, "US", now)
	var f2 Flag
	for _, x := range s.List() {
		if x.Type == TypeCriticalPort {
			f2 = x
		}
	}
	if f2.Provisional {
		t.Fatal("expected Provisional to stay false for a flag raised via AddWithDetail")
	}
}

// TestAddProvisionalPersistsAndSurvivesReload is #399's "verify the
// store round-trips it" requirement for internal/flags: Flag.Provisional
// is additive JSON (omitempty), so nothing about persistedState's shape
// changes -- this proves that in practice, not just by inspection, the
// same way TestPersistenceRoundTrip already does for the rest of Flag's
// fields. Both backends share the same JSON encoding (see
// persist.Backend), so a file-backend round trip exercises the encoding
// both would use.
func TestAddProvisionalPersistsAndSurvivesReload(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s1.AddProvisional(TypeActivitySpike, "9.9.9.9", "warming up", 40, Evidence{}, "", true, now)
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 persisted flag after reopening, got %d: %+v", len(list), list)
	}
	if !list[0].Provisional {
		t.Fatal("expected Provisional=true to survive a persist/reload round trip")
	}
}

func TestApplyReputationSnapshotSetsSnapshotAndFloor(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)

	score := 88
	reports := 42
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{
		IP: "203.0.113.9", AbuseScore: &score, TotalReports: &reports, ISP: "Example Hosting",
	})

	f := s.List()[0]
	if f.Reputation == nil || f.Reputation.ISP != "Example Hosting" {
		t.Fatalf("expected the reputation snapshot to be stored, got %+v", f.Reputation)
	}
	if f.Confidence == nil || *f.Confidence != 88 {
		t.Errorf("expected the AbuseScore to raise the confidence floor, got %+v", f.Confidence)
	}
}

func TestApplyReputationSnapshotWithoutAbuseScoreStillStoresSnapshot(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)

	// Shodan-only result -- no AbuseIPDB key configured, so AbuseScore is nil.
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{
		IP: "203.0.113.9", Ports: []int{22, 80}, Vulns: []string{"CVE-2021-1234"},
	})

	f := s.List()[0]
	if f.Reputation == nil || len(f.Reputation.Vulns) != 1 {
		t.Fatalf("expected the Shodan-only snapshot to still be stored, got %+v", f.Reputation)
	}
	if f.Confidence == nil || *f.Confidence != 10 {
		t.Errorf("expected confidence to be untouched without an AbuseScore, got %+v", f.Confidence)
	}
}

func TestApplyReputationSnapshotIsTorRaisesFloorWithoutAbuseScore(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)

	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{
		IP: "203.0.113.9", IsTor: true,
	})

	f := s.List()[0]
	if f.Confidence == nil || *f.Confidence != reputation.TorExitNodeFloor {
		t.Errorf("expected IsTor to raise confidence to %d, got %+v", reputation.TorExitNodeFloor, f.Confidence)
	}
}

func TestApplyReputationSnapshotHostingUsageTypeRaisesFloor(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)

	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{
		IP: "203.0.113.9", UsageType: "Data Center/Web Hosting/Transit",
	})

	f := s.List()[0]
	if f.Confidence == nil || *f.Confidence != reputation.HostingProviderFloor {
		t.Errorf("expected a hosting usageType to raise confidence to %d, got %+v", reputation.HostingProviderFloor, f.Confidence)
	}
}

func TestApplyReputationSnapshotStrongerSignalWins(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	// AbuseScore (80) beats IsTor's floor (60).
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)
	highScore := 80
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{AbuseScore: &highScore, IsTor: true})
	if f := s.List()[0]; f.Confidence == nil || *f.Confidence != 80 {
		t.Errorf("expected the higher AbuseScore to win, got %+v", f.Confidence)
	}

	// IsTor's floor (60) beats a low AbuseScore (10).
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.10", "detail", 10, now)
	lowScore := 10
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.10", reputation.Result{AbuseScore: &lowScore, IsTor: true})
	var found *Flag
	for _, f := range s.List() {
		if f.Target == "203.0.113.10" {
			found = &f
		}
	}
	if found == nil || found.Confidence == nil || *found.Confidence != reputation.TorExitNodeFloor {
		t.Errorf("expected the higher RiskFloor to win over a low AbuseScore, got %+v", found)
	}
}

func TestApplyReputationSnapshotUnknownIDIsNoOp(t *testing.T) {
	s, _ := Open("")
	score := 90
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.99", reputation.Result{AbuseScore: &score})
	if len(s.List()) != 0 {
		t.Errorf("expected no flag to be created for an unknown ID, got %+v", s.List())
	}
}

func TestReputationSnapshotResetOnRevival(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "detail", 10, now)
	score := 90
	s.ApplyReputationSnapshot(TypeCriticalPort, "203.0.113.9", reputation.Result{IP: "203.0.113.9", AbuseScore: &score})
	s.Clear(flagID(TypeCriticalPort, "203.0.113.9"), now.Add(time.Second))

	s.AddWithConfidence(TypeCriticalPort, "203.0.113.9", "revived episode", 15, now.Add(2*time.Second))
	if f := s.List()[0]; f.Reputation != nil {
		t.Errorf("expected a revived episode to start with no stale reputation snapshot, got %+v", f.Reputation)
	}
}

func TestOnRaiseFiresOnNewEpisodeOnly(t *testing.T) {
	s, _ := Open("")
	var raised []Flag
	s.WithOnRaise(func(f Flag) {
		raised = append(raised, f)
	})
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.9", "first", now)
	if len(raised) != 1 || raised[0].Detail != "first" {
		t.Fatalf("expected onRaise to fire once for the first-ever raise, got %+v", raised)
	}

	s.Add(TypePortScan, "203.0.113.9", "re-fire", now.Add(time.Second))
	if len(raised) != 1 {
		t.Fatalf("expected a plain re-fire to not trigger onRaise, got %d calls", len(raised))
	}

	id := flagID(TypePortScan, "203.0.113.9")
	s.Clear(id, now.Add(2*time.Second))
	s.Add(TypePortScan, "203.0.113.9", "revived", now.Add(3*time.Second))
	if len(raised) != 2 || raised[1].Detail != "revived" {
		t.Fatalf("expected a revival from cleared to trigger onRaise again, got %+v", raised)
	}
}

func TestOnRaiseNotSetIsNoOp(t *testing.T) {
	s, _ := Open("")
	// No WithOnRaise call -- Add must not panic on a nil hook.
	if isNew := s.Add(TypePortScan, "203.0.113.9", "detail", time.Now()); !isNew {
		t.Error("expected the raise itself to still succeed with no hook configured")
	}
}

// TestExcludedTargetNeverRaises is the core contract this feature
// exists for: once excluded, (Type, Target) must never raise again --
// not once, not after being raised-then-cleared elsewhere, and not
// across many repeated calls (proving it's a durable exclusion, not a
// one-shot suppression that resets after the first blocked call).
func TestExcludedTargetNeverRaises(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Exclude(TypePortScan, "203.0.113.9")

	if isNew := s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now); isNew {
		t.Error("expected Add on an excluded target to report no new episode")
	}
	if len(s.List()) != 0 {
		t.Fatalf("expected an excluded target to raise nothing at all, got %+v", s.List())
	}

	// Repeated re-fires: still nothing, every time.
	for i := 0; i < 5; i++ {
		s.Add(TypePortScan, "203.0.113.9", "re-fire", now.Add(time.Duration(i)*time.Second))
	}
	if len(s.List()) != 0 {
		t.Fatalf("expected repeated Add calls on an excluded target to keep raising nothing, got %+v", s.List())
	}

	// AddWithConfidence/AddWithDetail go through the same add() -- must
	// be silenced too, not just the plain Add path.
	s.AddWithConfidence(TypePortScan, "203.0.113.9", "detail", 90, now)
	s.AddWithDetail(TypePortScan, "203.0.113.9", "detail", 90, Evidence{Ports: []int{22}}, "US", now)
	if len(s.List()) != 0 {
		t.Fatalf("expected AddWithConfidence/AddWithDetail to also be silenced, got %+v", s.List())
	}

	// A different (Type, Target) pair is unaffected.
	s.Add(TypePortScan, "203.0.113.10", "unrelated target", now)
	if len(s.List()) != 1 {
		t.Errorf("expected the exclusion to be scoped to its exact (Type, Target), got %+v", s.List())
	}
	s.Add(TypeActivitySpike, "203.0.113.9", "same target, different type", now)
	if len(s.List()) != 2 {
		t.Errorf("expected the exclusion to be scoped to its exact Type too, got %+v", s.List())
	}
}

// TestExcludeIsPermanentNotRaiseThenAutoClear guards specifically
// against the rejected "raise then immediately auto-clear" alternative
// design the issue calls out -- an excluded target must never even
// briefly appear in the store, cleared or otherwise.
func TestExcludeIsPermanentNotRaiseThenAutoClear(t *testing.T) {
	s, _ := Open("")
	s.Exclude(TypeCriticalPort, "198.51.100.4")
	s.Add(TypeCriticalPort, "198.51.100.4", "6 attempts on port 22", time.Now())

	if len(s.List()) != 0 {
		t.Fatalf("expected an excluded target to never appear in the store at all (not raised-then-cleared), got %+v", s.List())
	}
}

func TestClearAndExcludeClearsAndPreventsFutureRaises(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now)
	id := s.List()[0].ID

	if ok := s.ClearAndExclude(id, now.Add(time.Minute)); !ok {
		t.Fatal("expected ClearAndExclude on a known ID to succeed")
	}

	list := s.List()
	if len(list) != 1 || !list[0].Cleared {
		t.Fatalf("expected the flag to be marked cleared, got %+v", list)
	}
	if !s.Excluded(TypePortScan, "203.0.113.9") {
		t.Error("expected ClearAndExclude to record the exclusion")
	}

	// The whole point: it must never raise again after this.
	s.Add(TypePortScan, "203.0.113.9", "re-fire attempt", now.Add(2*time.Minute))
	list = s.List()
	if len(list) != 1 || !list[0].Cleared || list[0].Detail != "20 ports in 60s" {
		t.Errorf("expected the excluded target to stay cleared and untouched by further Add calls, got %+v", list)
	}
}

func TestClearAndExcludeUnknownIDReturnsFalse(t *testing.T) {
	s, _ := Open("")
	if s.ClearAndExclude("nonexistent", time.Now()) {
		t.Error("expected ClearAndExclude on an unknown ID to return false")
	}
	if len(s.ListExclusions()) != 0 {
		t.Error("expected no exclusion to be recorded for an unknown ID")
	}
}

func TestExcludeIsIdempotent(t *testing.T) {
	s, _ := Open("")
	s.Exclude(TypePortScan, "203.0.113.9")
	s.Exclude(TypePortScan, "203.0.113.9")

	if len(s.ListExclusions()) != 1 {
		t.Errorf("expected excluding the same pair twice to still yield one entry, got %+v", s.ListExclusions())
	}
}

func TestRemoveExclusionReEnablesRaising(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Exclude(TypePortScan, "203.0.113.9")
	s.Add(TypePortScan, "203.0.113.9", "should be suppressed", now)
	if len(s.List()) != 0 {
		t.Fatal("expected the flag to be suppressed while excluded")
	}

	if !s.RemoveExclusion(TypePortScan, "203.0.113.9") {
		t.Fatal("expected RemoveExclusion on a known exclusion to return true")
	}
	if s.Excluded(TypePortScan, "203.0.113.9") {
		t.Error("expected Excluded to report false after removal")
	}

	s.Add(TypePortScan, "203.0.113.9", "should raise again now", now.Add(time.Second))
	list := s.List()
	if len(list) != 1 || list[0].Detail != "should raise again now" {
		t.Errorf("expected the target to raise normally again after RemoveExclusion, got %+v", list)
	}
}

func TestRemoveExclusionUnknownReturnsFalse(t *testing.T) {
	s, _ := Open("")
	if s.RemoveExclusion(TypePortScan, "203.0.113.9") {
		t.Error("expected RemoveExclusion on a never-excluded pair to return false")
	}
}

func TestRemoveExclusionByID(t *testing.T) {
	s, _ := Open("")
	s.Exclude(TypePortScan, "203.0.113.9")
	id := flagID(TypePortScan, "203.0.113.9")

	if !s.RemoveExclusionByID(id) {
		t.Fatal("expected RemoveExclusionByID on a known exclusion ID to return true")
	}
	if s.RemoveExclusionByID(id) {
		t.Error("expected a second RemoveExclusionByID call to return false (already removed)")
	}
}

func TestListExclusionsSortedByID(t *testing.T) {
	s, _ := Open("")
	s.Exclude(TypePortScan, "203.0.113.9")
	s.Exclude(TypeActivitySpike, "198.51.100.4")

	list := s.ListExclusions()
	if len(list) != 2 {
		t.Fatalf("expected 2 exclusions, got %d: %+v", len(list), list)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].ID > list[i].ID {
			t.Errorf("expected exclusions sorted by ID, got %+v", list)
		}
	}
	for _, e := range list {
		if e.Target == "203.0.113.9" && e.Type != TypePortScan {
			t.Errorf("expected the (type, target) pair to round-trip intact, got %+v", e)
		}
	}
}

// TestExclusionPersistenceRoundTrip proves the exclusion survives a
// process restart (Open() round-trip), same as TestPersistenceRoundTrip
// already proves for flags themselves -- the whole point of a
// "permanent" exclusion is that it stays permanent across restarts, not
// just for the life of the current process.
func TestExclusionPersistenceRoundTrip(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Exclude(TypePortScan, "203.0.113.9")
	s1.Exclude(TypeCriticalPort, "198.51.100.4")
	// Also persist an ordinary active flag alongside the exclusions, to
	// prove the two coexist correctly in the same file.
	s1.Add(TypeActivitySpike, "10.0.0.5", "5x baseline", time.Now())
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}

	if !s2.Excluded(TypePortScan, "203.0.113.9") {
		t.Error("expected the first exclusion to survive reopening")
	}
	if !s2.Excluded(TypeCriticalPort, "198.51.100.4") {
		t.Error("expected the second exclusion to survive reopening")
	}
	if len(s2.ListExclusions()) != 2 {
		t.Errorf("expected exactly 2 persisted exclusions, got %+v", s2.ListExclusions())
	}
	if len(s2.List()) != 1 {
		t.Errorf("expected the ordinary flag to also survive reopening alongside the exclusions, got %+v", s2.List())
	}

	// And the reopened store must still actually enforce the exclusion,
	// not just report it via Excluded().
	s2.Add(TypePortScan, "203.0.113.9", "should stay suppressed after reload", time.Now())
	for _, f := range s2.List() {
		if f.Type == TypePortScan && f.Target == "203.0.113.9" {
			t.Errorf("expected the reloaded exclusion to still suppress raises, got %+v", f)
		}
	}
}

// TestActiveFlagsCannotGrowUnbounded is the regression test for a
// resource-exhaustion vector reachable from unauthenticated syslog.
//
// pruneLocked prefers to evict cleared flags, which is the right
// instinct: an active flag is something a human still needs to see.
// But "never evict an active flag" is only safe if the active set is
// bounded, and flag targets come straight from log lines an attacker
// controls -- so it was not. Proven before this fix: ~40k spoofed MAC
// addresses produced ~40k live flags and drove ingest to a crawl.
func TestActiveFlagsCannotGrowUnbounded(t *testing.T) {
	prevCeiling := maxFlagsHardCeiling
	prevSoft := maxFlags
	maxFlagsHardCeiling = 200
	maxFlags = 50
	t.Cleanup(func() {
		maxFlagsHardCeiling = prevCeiling
		maxFlags = prevSoft
	})

	s, _ := Open("")
	now := time.Now()
	// Every flag stays ACTIVE -- nothing is ever cleared, which is the
	// case the old prune could not handle at all.
	for i := 0; i < 5000; i++ {
		s.Add(TypeNewDevice, fmt.Sprintf("aa:bb:cc:dd:%02x:%02x", i>>8, i&0xff), "spoofed", now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(s.List()); got > maxFlagsHardCeiling {
		t.Errorf("store holds %d flags with none cleared, want <= %d", got, maxFlagsHardCeiling)
	}
}

// TestPruneStillPrefersClearedFlags: the hard ceiling must not change
// the normal behaviour -- below it, reviewed noise is shed first and
// active alerts survive.
func TestPruneStillPrefersClearedFlags(t *testing.T) {
	prevSoft := maxFlags
	maxFlags = 10
	t.Cleanup(func() { maxFlags = prevSoft })

	s, _ := Open("")
	now := time.Now()

	// One active flag raised first, so it is the oldest of all.
	s.Add(TypePortScan, "203.0.113.1", "real alert", now)

	for i := 0; i < 100; i++ {
		target := fmt.Sprintf("198.51.100.%d", i)
		s.Add(TypePortScan, target, "noise", now.Add(time.Duration(i+1)*time.Millisecond))
		s.Clear(flagID(TypePortScan, target), now.Add(time.Duration(i+1)*time.Second))
	}

	var found bool
	for _, f := range s.List() {
		if f.Target == "203.0.113.1" && !f.Cleared {
			found = true
		}
	}
	if !found {
		t.Error("the active flag was evicted while cleared flags remained; cleared flags must be shed first")
	}
}

// The hard ceiling used to shed by FirstSeen ascending -- earliest
// raised first -- which selects for exactly the wrong thing: the first
// flag of a real incident is the most valuable item in the store, and
// flag targets come from unauthenticated syslog, so an attacker only has
// to mint maxFlagsHardCeiling junk targets to push it out. Reproduced on
// the old code with one genuine flag and 5,001 `new_device` flags (any
// unseen src-mac, no threshold to cross), about 600 KB of syslog: the
// genuine flag was gone from byID and List() permanently. See #285.
//
// The store cannot be made immune -- a bounded store under an unbounded
// flood must drop something -- but the eviction order must not prefer
// the evidence worth keeping. Count, how many times a detector re-fired
// for a target, is the available signal: a real incident re-fires, minted
// flags fire once each.
func TestHardCeilingShedsOneShotNoiseBeforeARefiringAlert(t *testing.T) {
	prevCeiling := maxFlagsHardCeiling
	prevSoft := maxFlags
	maxFlagsHardCeiling = 200
	maxFlags = 50
	t.Cleanup(func() {
		maxFlagsHardCeiling = prevCeiling
		maxFlags = prevSoft
	})

	s, _ := Open("")
	now := time.Now()

	// A genuine, repeatedly-firing alert, raised FIRST -- so under the
	// old FirstSeen ordering it was the very first thing evicted.
	const genuine = "203.0.113.9"
	for i := 0; i < 20; i++ {
		s.Add(TypePortScan, genuine, "23 distinct ports in 60s", now.Add(time.Duration(i)*time.Second))
	}

	// Then the flood: distinct targets, one flag each, all active.
	for i := 0; i < 5000; i++ {
		s.Add(TypeNewDevice, fmt.Sprintf("aa:bb:cc:dd:%02x:%02x", i>>8, i&0xff), "unseen mac",
			now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(s.List()); got > maxFlagsHardCeiling {
		t.Errorf("store holds %d flags, want <= %d", got, maxFlagsHardCeiling)
	}
	for _, f := range s.List() {
		if f.Target == genuine {
			return
		}
	}
	t.Error("the repeatedly-firing genuine alert was evicted by a flood of one-shot flags")
}

// TestClearedCountSurvivesReload guards a bug introduced alongside the
// clearedCount optimisation: pruneLocked skips its scan when
// clearedCount is zero, so a store reloaded from disk with cleared
// flags but a zero counter would silently never evict them again --
// the exact leak the counter was added to make cheap to avoid.
func TestClearedCountSurvivesReload(t *testing.T) {
	// persistLocked throttles real writes to persistMinInterval, so
	// without shrinking it nothing reaches disk and this would test an
	// empty file rather than the reload path.
	prevInterval := persistMinInterval
	persistMinInterval = 0
	t.Cleanup(func() { persistMinInterval = prevInterval })

	path := filepath.Join(t.TempDir(), "flags.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		target := fmt.Sprintf("203.0.113.%d", i)
		s1.Add(TypePortScan, target, "d", now)
		s1.Clear(flagID(TypePortScan, target), now.Add(time.Second))
	}
	s1.Add(TypePortScan, "198.51.100.1", "still active", now)
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.clearedCount; got != 5 {
		t.Errorf("clearedCount after reload = %d, want 5 -- a stale zero disables cleared-flag eviction entirely", got)
	}
}

// TestClearAllClearsEveryActiveFlag covers issue #198's "Clear all":
// every active flag is cleared in one call, cleared ones are left
// exactly as they were, and the count returned is the number actually
// cleared.
func TestClearAllClearsEveryActiveFlag(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.1", "d1", now)
	s.Add(TypeActivitySpike, "203.0.113.2", "d2", now)
	s.Add(TypeCriticalPort, "203.0.113.3", "d3", now)
	// Already cleared before ClearAll runs -- must stay untouched, not
	// double-counted, and not have its ClearedAt overwritten.
	s.Add(TypeOutboundAnomaly, "203.0.113.4", "d4", now)
	preClearedID := flagID(TypeOutboundAnomaly, "203.0.113.4")
	s.Clear(preClearedID, now.Add(time.Second))
	preClearedAt := now.Add(time.Second)

	later := now.Add(time.Minute)
	n := s.ClearAll(later)
	if n != 3 {
		t.Errorf("ClearAll returned %d, want 3 (the already-cleared flag must not be recounted)", n)
	}

	for _, f := range s.List() {
		if !f.Cleared {
			t.Errorf("flag %+v is still active after ClearAll", f)
		}
	}

	// The pre-cleared flag's own ClearedAt must be untouched by this
	// call -- ClearAll only ever clears what was still active.
	for _, f := range s.List() {
		if f.ID == preClearedID && !f.ClearedAt.Equal(preClearedAt) {
			t.Errorf("ClearAll overwrote an already-cleared flag's ClearedAt: got %v, want %v", f.ClearedAt, preClearedAt)
		}
	}
}

// TestClearAllCreatesNoExclusions is the invariant #198 states
// explicitly: Clear all performs regular clears only. A flag cleared by
// it must still raise again on the next matching event, exactly like a
// single Clear -- and unlike ClearAndExclude.
func TestClearAllCreatesNoExclusions(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.9", "d", now)
	s.ClearAll(now.Add(time.Minute))

	if len(s.ListExclusions()) != 0 {
		t.Fatalf("ClearAll created exclusions: %+v, want none", s.ListExclusions())
	}

	// The whole point of "no exclusion": it must be able to raise again.
	s.Add(TypePortScan, "203.0.113.9", "re-fire", now.Add(2*time.Minute))
	list := s.List()
	if len(list) != 1 || list[0].Cleared || list[0].Detail != "re-fire" {
		t.Errorf("expected the target to raise again after Clear all, got %+v", list)
	}
}

// TestClearAllOnEmptyStoreIsANoOp guards the persistLocked-skip path: no
// active flags means nothing to write, and the call should not error or
// panic on a store with nothing in it.
func TestClearAllOnEmptyStoreIsANoOp(t *testing.T) {
	s, _ := Open("")
	if n := s.ClearAll(time.Now()); n != 0 {
		t.Errorf("ClearAll on an empty store returned %d, want 0", n)
	}
}

// Exclude on a pair that already has an active flag must clear it.
//
// add() skips excluded pairs before touching s.byID, so without this the
// existing entry stayed in List() as Cleared:false forever, frozen, with
// every later update silently no-op'd. Not reachable through the API --
// ClearAndExclude clears first -- which is what made it a landmine for
// the next caller rather than a live bug.
func TestExcludeClearsAnAlreadyActiveFlag(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.5", "20 ports", now)
	if active := activeTargets(s.List()); len(active) != 1 {
		t.Fatalf("setup: expected one active flag, got %d", len(active))
	}

	s.Exclude(TypePortScan, "203.0.113.5")

	for _, f := range s.List() {
		if f.Type == TypePortScan && f.Target == "203.0.113.5" && !f.Cleared {
			t.Fatal("the pre-existing flag is still active after Exclude, and no later update can reach it")
		}
	}
}

func activeTargets(flags []Flag) []string {
	var out []string
	for _, f := range flags {
		if !f.Cleared {
			out = append(out, f.Target)
		}
	}
	return out
}

// -- #638: SetVerdict ------------------------------------------------

// TestSetVerdictUnknownIDReturnsFalse mirrors
// TestClearUnknownOrAlreadyClearedReturnsFalse's "unknown ID" half --
// the handler maps this to 404.
func TestSetVerdictUnknownIDReturnsFalse(t *testing.T) {
	s, _ := Open("")
	if _, ok := s.SetVerdict("nonexistent", VerdictReal, "alice", time.Now()); ok {
		t.Error("SetVerdict() on an unknown ID should return false")
	}
}

// TestSetVerdictExpectedClearsFlag and TestSetVerdictNoiseClearsFlag
// cover #638's contract that expected/noise clear the flag, reusing the
// same clearLocked path Clear itself uses (see the doc comment on
// both).
func TestSetVerdictExpectedClearsFlag(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "d", now)
	id := s.List()[0].ID

	f, ok := s.SetVerdict(id, VerdictExpected, "alice", now)
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if f.Verdict != VerdictExpected {
		t.Errorf("Verdict = %q, want %q", f.Verdict, VerdictExpected)
	}
	if f.VerdictBy != "alice" {
		t.Errorf("VerdictBy = %q, want alice", f.VerdictBy)
	}
	if f.VerdictAt.IsZero() {
		t.Error("VerdictAt should be set")
	}
	if !f.Cleared {
		t.Error("expected verdict should clear the flag")
	}
	if f.ClearedAt.IsZero() {
		t.Error("expected verdict should set ClearedAt")
	}
}

func TestSetVerdictNoiseClearsFlag(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypeActivitySpike, "203.0.113.10", "d", now)
	id := s.List()[0].ID

	f, ok := s.SetVerdict(id, VerdictNoise, "bob", now)
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if f.Verdict != VerdictNoise {
		t.Errorf("Verdict = %q, want %q", f.Verdict, VerdictNoise)
	}
	if !f.Cleared {
		t.Error("noise verdict should clear the flag")
	}
}

// TestSetVerdictRealLeavesFlagOpen is the invariant #638 exists to
// establish: a real verdict records the judgement but must never clear
// the flag.
func TestSetVerdictRealLeavesFlagOpen(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypeCriticalPort, "203.0.113.11", "d", now)
	id := s.List()[0].ID

	f, ok := s.SetVerdict(id, VerdictReal, "carol", now)
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if f.Verdict != VerdictReal {
		t.Errorf("Verdict = %q, want %q", f.Verdict, VerdictReal)
	}
	if f.VerdictBy != "carol" {
		t.Errorf("VerdictBy = %q, want carol", f.VerdictBy)
	}
	if f.Cleared {
		t.Error("a real verdict must not clear the flag")
	}
	if !f.ClearedAt.IsZero() {
		t.Error("a real verdict must not set ClearedAt")
	}
}

// TestSetVerdictPersistsAndSurvivesReload is #638's "verify the store
// round-trips it" requirement, the same shape as
// TestAddProvisionalPersistsAndSurvivesReload for Flag.Provisional:
// Verdict/VerdictBy/VerdictAt are additive JSON (omitempty/omitzero),
// so this proves the round trip in practice, not just by inspection of
// the struct tags.
func TestSetVerdictPersistsAndSurvivesReload(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s1.Add(TypeGlobalSpike, "global", "d", now)
	id := s1.List()[0].ID
	if _, ok := s1.SetVerdict(id, VerdictReal, "dana", now); !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 persisted flag after reopening, got %d: %+v", len(list), list)
	}
	if list[0].Verdict != VerdictReal {
		t.Errorf("Verdict = %q after reload, want %q", list[0].Verdict, VerdictReal)
	}
	if list[0].VerdictBy != "dana" {
		t.Errorf("VerdictBy = %q after reload, want dana", list[0].VerdictBy)
	}
	if !list[0].VerdictAt.Equal(now) {
		t.Errorf("VerdictAt = %v after reload, want %v", list[0].VerdictAt, now)
	}
}

// TestSetVerdictOverwritesPreviousVerdict covers re-judging an
// already-judged flag: the latest call wins, no history kept -- same
// in-place-mutation convention as every other store field.
func TestSetVerdictOverwritesPreviousVerdict(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.12", "d", now)
	id := s.List()[0].ID

	s.SetVerdict(id, VerdictReal, "alice", now)
	f, ok := s.SetVerdict(id, VerdictNoise, "bob", now.Add(time.Minute))
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if f.Verdict != VerdictNoise {
		t.Errorf("Verdict = %q, want %q after re-judging", f.Verdict, VerdictNoise)
	}
	if f.VerdictBy != "bob" {
		t.Errorf("VerdictBy = %q, want bob after re-judging", f.VerdictBy)
	}
	if !f.Cleared {
		t.Error("the noise verdict from re-judging should still clear the flag")
	}
}

// TestReviveResetsVerdict covers the store.go add() revival branch this
// change touches: a flag cleared by an expected/noise verdict that then
// re-fires as a new episode must not silently carry the old verdict
// forward onto the new episode -- same reasoning already applied to
// ReputationFloor/Reputation on revival.
func TestReviveResetsVerdict(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.13", "d", now)
	id := s.List()[0].ID
	if _, ok := s.SetVerdict(id, VerdictNoise, "alice", now); !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}

	s.Add(TypePortScan, "203.0.113.13", "d again", now.Add(time.Hour))
	f := s.List()[0]
	if f.Verdict != "" {
		t.Errorf("Verdict = %q after revival, want empty", f.Verdict)
	}
	if f.VerdictBy != "" {
		t.Errorf("VerdictBy = %q after revival, want empty", f.VerdictBy)
	}
	if !f.VerdictAt.IsZero() {
		t.Error("VerdictAt should be zero after revival")
	}
}

// -- #638 follow-on: UndoVerdict --------------------------------------

// TestUndoVerdictUnknownIDReturnsFalse mirrors SetVerdict's own unknown-
// id case -- the handler maps this to 404.
func TestUndoVerdictUnknownIDReturnsFalse(t *testing.T) {
	s, _ := Open("")
	if _, ok := s.UndoVerdict("nonexistent"); ok {
		t.Error("UndoVerdict() on an unknown ID should return false")
	}
}

// TestUndoVerdictReopensAFlagTheVerdictItselfCleared is the ordinary
// case: judge an open flag expected/noise (which clears it via
// clearLocked), then undo -- the flag must re-open, and clearedCount
// must fall back to what it was before the verdict.
func TestUndoVerdictReopensAFlagTheVerdictItselfCleared(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.20", "d", now)
	id := s.List()[0].ID
	before := s.clearedCount

	f, ok := s.SetVerdict(id, VerdictNoise, "alice", now)
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if !f.Cleared {
		t.Fatal("setup: expected the noise verdict to clear the flag")
	}
	if got := s.clearedCount; got != before+1 {
		t.Fatalf("clearedCount after judging = %d, want %d", got, before+1)
	}

	f, ok = s.UndoVerdict(id)
	if !ok {
		t.Fatal("expected UndoVerdict to find the flag")
	}
	if f.Cleared {
		t.Error("undo should re-open a flag the verdict itself cleared")
	}
	if !f.ClearedAt.IsZero() {
		t.Error("undo should reset ClearedAt")
	}
	if f.Verdict != "" || f.VerdictBy != "" || !f.VerdictAt.IsZero() {
		t.Errorf("undo should reset every verdict field, got %+v", f)
	}
	if got := s.clearedCount; got != before {
		t.Errorf("clearedCount after undo = %d, want %d (back to pre-verdict)", got, before)
	}
}

// TestUndoVerdictLeavesAnAlreadyClearedFlagCleared is the subtlety
// #638's follow-on exists to cover: judging an already-cleared flag
// (expected/noise on a flag a plain Clear already closed) must not let
// undo re-open it, because the verdict's own clearLocked call was a
// no-op -- it wasn't what cleared the flag, so undoing the verdict has
// no business touching Cleared.
func TestUndoVerdictLeavesAnAlreadyClearedFlagCleared(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.21", "d", now)
	id := s.List()[0].ID

	if !s.Clear(id, now) {
		t.Fatal("setup: expected the plain Clear to succeed")
	}
	before := s.clearedCount

	f, ok := s.SetVerdict(id, VerdictExpected, "bob", now.Add(time.Minute))
	if !ok {
		t.Fatal("expected SetVerdict to find the flag")
	}
	if !f.Cleared {
		t.Fatal("setup: the flag should still read cleared after judging it")
	}
	if got := s.clearedCount; got != before {
		t.Fatalf("clearedCount after judging an already-cleared flag = %d, want unchanged %d", got, before)
	}

	f, ok = s.UndoVerdict(id)
	if !ok {
		t.Fatal("expected UndoVerdict to find the flag")
	}
	if !f.Cleared {
		t.Error("undo must not re-open a flag that was already cleared before it was judged")
	}
	if f.Verdict != "" || f.VerdictBy != "" || !f.VerdictAt.IsZero() {
		t.Errorf("undo should still reset every verdict field even when it doesn't re-open, got %+v", f)
	}
	if got := s.clearedCount; got != before {
		t.Errorf("clearedCount after undo of an already-cleared flag = %d, want unchanged %d", got, before)
	}
}

// TestUndoVerdictOnUnjudgedFlagIsANoOp documents the deliberate choice
// for undoing a flag with no verdict recorded: succeed as a no-op
// (same "no-op, not an error" reasoning as Clear on an unknown/already-
// cleared id) rather than returning an error, since the caller may be a
// stale undo affordance racing a page that already moved on and can't
// always tell the two cases apart either.
func TestUndoVerdictOnUnjudgedFlagIsANoOp(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.22", "d", now)
	id := s.List()[0].ID
	before := s.clearedCount

	f, ok := s.UndoVerdict(id)
	if !ok {
		t.Fatal("expected UndoVerdict to find the flag")
	}
	if f.Cleared {
		t.Error("undoing an unjudged, never-cleared flag must not clear it")
	}
	if f.Verdict != "" {
		t.Errorf("Verdict = %q, want still empty", f.Verdict)
	}
	if got := s.clearedCount; got != before {
		t.Errorf("clearedCount changed on a no-op undo: got %d, want unchanged %d", got, before)
	}
}
