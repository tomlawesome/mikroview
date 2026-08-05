package flags

import (
	"os"
	"path/filepath"
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
