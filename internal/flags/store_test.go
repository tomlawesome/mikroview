package flags

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/reputation"
)

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
	data := `[null, {"id":"port_scan:1.2.3.4","type":"port_scan","target":"1.2.3.4"}, null]`
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

func TestOpenMalformedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Error("expected a non-nil informational error for a malformed file")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected a malformed file to start empty, got %d flags", len(s.List()))
	}
	// still usable despite the error
	s.Add(TypePortScan, "1.2.3.4", "test", time.Now())
	if len(s.List()) != 1 {
		t.Error("store returned from Open() with a malformed file should still be usable")
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
// disk writes within the window (not just that it compiles) -- a
// sustained re-fire burst (the scenario this exists for) must not hit
// disk once per event.
func TestPersistLockedRateLimitsWrites(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 80 * time.Millisecond
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	s.Add(TypePortScan, "1.1.1.1", "first", now)
	firstWrite, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the first Add to write immediately (empty lastPersist), got: %v", err)
	}

	// Within the debounce window: must NOT reach disk yet.
	s.Add(TypePortScan, "2.2.2.2", "second, still within the window", now)
	stillFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(stillFirst) != string(firstWrite) {
		t.Errorf("expected the second Add (within %v of the first) to be rate-limited, but the file changed", persistMinInterval)
	}

	// Past the window: the next call must flush the latest state.
	time.Sleep(persistMinInterval + 20*time.Millisecond)
	s.Add(TypePortScan, "3.3.3.3", "third, after the window", now)
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 3 {
		t.Fatalf("in-memory state should always have all 3 regardless of persistence timing, got %d", len(s.List()))
	}
	if !strings.Contains(string(final), "3.3.3.3") {
		t.Errorf("expected the post-window write to include all 3 flags, got:\n%s", final)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	// Every Add/Clear below happens back-to-back with no real delay --
	// persistMinInterval's rate limiting would otherwise skip all but
	// the first write, and reopening immediately after would read stale
	// data. Real callers spread naturally over wall-clock time; this
	// test doesn't.
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

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
	// guarantees every timestamp below stays in the same bucket.
	now := time.Now().Truncate(time.Minute).Add(10 * time.Second)

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
		Ports: []int{22, 3389},
		Hosts: []string{"192.168.1.5"},
		NAT:   &NATInfo{IP: "10.0.0.5", Port: 8080, Raw: "NAT (10.0.0.5:8080->192.168.1.5:80)"},
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

	// A plain re-fire recomputes evidence fresh, same as Detail already does.
	s.AddWithDetail(TypePortScan, "203.0.113.9", "detail", 42, Evidence{Ports: []int{22}}, "US", now.Add(time.Second))
	f = s.List()[0]
	if len(f.Evidence.Ports) != 1 || len(f.Evidence.Hosts) != 0 {
		t.Errorf("expected evidence to reflect the latest call, got %+v", f.Evidence)
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

// TestOpenReadsLegacyBareArrayFormat proves a flags.json file written
// before this feature existed (a bare `[...]` array of flags, no
// exclusions wrapper) still loads correctly -- an existing deployment
// upgrading mikroview must not lose its flag history or start treating
// that file as malformed just because the on-disk shape changed.
func TestOpenReadsLegacyBareArrayFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.json")
	data := `[{"id":"port_scan:1.2.3.4","type":"port_scan","target":"1.2.3.4","detail":"legacy entry"}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a legacy bare-array file returned an unexpected error: %v", err)
	}
	list := s.List()
	if len(list) != 1 || list[0].Detail != "legacy entry" {
		t.Fatalf("expected the legacy flag to load intact, got %+v", list)
	}
	if len(s.ListExclusions()) != 0 {
		t.Errorf("expected a legacy file to start with no exclusions, got %+v", s.ListExclusions())
	}
}
