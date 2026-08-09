// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

func mustUpsertInverted(t *testing.T, s *Store, id string) Entry {
	t.Helper()
	e := Entry{ID: id, Invert: true, Observing: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}
	if err := s.Upsert(e); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, _ := s.Get(id)
	return got
}

func TestRecordObservationAddsACandidate(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")
	now := time.Now()

	s.RecordObservation("e1", "10.0.0.5", 8883, now)

	e, _ := s.Get("e1")
	if len(e.Observed) != 1 {
		t.Fatalf("Observed has %d entries, want 1", len(e.Observed))
	}
	o := e.Observed[0]
	if o.DestIP != "10.0.0.5" || o.Port != 8883 || o.Count != 1 {
		t.Errorf("unexpected observation: %+v", o)
	}
	if !o.FirstSeen.Equal(now) || !o.LastSeen.Equal(now) {
		t.Errorf("FirstSeen/LastSeen = %v/%v, want both %v", o.FirstSeen, o.LastSeen, now)
	}
}

func TestRecordObservationCollapsesRepeats(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")
	t0 := time.Now()

	for i := 0; i < 5; i++ {
		s.RecordObservation("e1", "10.0.0.5", 8883, t0.Add(time.Duration(i)*time.Minute))
	}

	e, _ := s.Get("e1")
	if len(e.Observed) != 1 {
		t.Fatalf("Observed has %d entries, want 1 -- repeats must collapse", len(e.Observed))
	}
	if e.Observed[0].Count != 5 {
		t.Errorf("Count = %d, want 5", e.Observed[0].Count)
	}
	wantLast := t0.Add(4 * time.Minute)
	if !e.Observed[0].LastSeen.Equal(wantLast) {
		t.Errorf("LastSeen = %v, want %v", e.Observed[0].LastSeen, wantLast)
	}
}

func TestRecordObservationDistinctDestinationsDoNotCollapse(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")
	now := time.Now()

	s.RecordObservation("e1", "10.0.0.5", 8883, now)
	s.RecordObservation("e1", "10.0.0.6", 8883, now) // different dest
	s.RecordObservation("e1", "10.0.0.5", 443, now)  // different port

	e, _ := s.Get("e1")
	if len(e.Observed) != 3 {
		t.Errorf("Observed has %d entries, want 3 distinct candidates", len(e.Observed))
	}
}

func TestRecordObservationIsNoopForUnknownOrNonInvertedEntry(t *testing.T) {
	s := mustOpenStore(t)
	s.RecordObservation("never-existed", "10.0.0.5", 8883, time.Now()) // must not panic

	if err := s.Upsert(Entry{ID: "e2", Ports: []int{22}}); err != nil { // non-inverted
		t.Fatal(err)
	}
	s.RecordObservation("e2", "10.0.0.5", 8883, time.Now())
	e, _ := s.Get("e2")
	if len(e.Observed) != 0 {
		t.Errorf("a non-inverted entry gained an observation: %+v", e.Observed)
	}
}

// The risk #243 open question 7 names directly -- an inverted entry in
// observe state collecting unbounded volume. RecordObservation must cap
// rather than grow without bound.
func TestRecordObservationCapsAtMaxObservedPerEntry(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")

	orig := maxObservedPerEntry
	maxObservedPerEntry = 3
	t.Cleanup(func() { maxObservedPerEntry = orig })

	now := time.Now()
	for i := 0; i < 10; i++ {
		s.RecordObservation("e1", "10.0.0.5", i, now) // 10 distinct ports -> 10 distinct candidates
	}

	e, _ := s.Get("e1")
	if len(e.Observed) != 3 {
		t.Fatalf("Observed has %d entries, want capped at 3", len(e.Observed))
	}
}

// A repeat of an already-observed pair must keep updating even once the
// cap is reached -- collapsing an existing candidate costs no new
// capacity, mirroring internal/matchlog's own "collapsing still works at
// capacity" rule.
func TestRecordObservationStillCollapsesAtCap(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")

	orig := maxObservedPerEntry
	maxObservedPerEntry = 1
	t.Cleanup(func() { maxObservedPerEntry = orig })

	t0 := time.Now()
	s.RecordObservation("e1", "10.0.0.5", 8883, t0)
	s.RecordObservation("e1", "10.0.0.5", 8883, t0.Add(time.Minute)) // same pair, cap already at 1

	e, _ := s.Get("e1")
	if len(e.Observed) != 1 || e.Observed[0].Count != 2 {
		t.Fatalf("got %+v, want one observation with Count=2", e.Observed)
	}
}

func TestPromoteMovesFromObservedToPermitted(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")
	s.RecordObservation("e1", "10.0.0.5", 8883, time.Now())
	s.RecordObservation("e1", "10.0.0.6", 443, time.Now())

	if err := s.Promote("e1", []PermittedDest{{DestIP: "10.0.0.5", Port: 8883}}); err != nil {
		t.Fatal(err)
	}

	e, _ := s.Get("e1")
	if len(e.Permitted) != 1 || e.Permitted[0].DestIP != "10.0.0.5" || e.Permitted[0].Port != 8883 {
		t.Errorf("Permitted = %+v, want the promoted pair", e.Permitted)
	}
	if len(e.Observed) != 1 || e.Observed[0].DestIP != "10.0.0.6" {
		t.Errorf("Observed = %+v, want only the un-promoted pair left", e.Observed)
	}
}

// Promoting something never observed is a legitimate, deliberate choice
// -- not every permitted destination has to come from the review list.
func TestPromoteAllowsAPairNeverObserved(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")

	if err := s.Promote("e1", []PermittedDest{{DestIP: "1.2.3.4", Port: 443}}); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Get("e1")
	if len(e.Permitted) != 1 {
		t.Errorf("Permitted = %+v, want the pair promoted despite never being observed", e.Permitted)
	}
}

func TestPromoteIsIdempotent(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1")
	pair := []PermittedDest{{DestIP: "1.2.3.4", Port: 443}}

	if err := s.Promote("e1", pair); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote("e1", pair); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Get("e1")
	if len(e.Permitted) != 1 {
		t.Errorf("Permitted has %d entries after promoting the same pair twice, want 1", len(e.Permitted))
	}
}

func TestPromoteDoesNotChangeObserving(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1") // Observing: true

	if err := s.Promote("e1", []PermittedDest{{DestIP: "1.2.3.4", Port: 443}}); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Get("e1")
	if !e.Observing {
		t.Error("Promote must not change Observing -- that is SetObserving's job")
	}
}

func TestPromoteErrors(t *testing.T) {
	s := mustOpenStore(t)
	if err := s.Upsert(Entry{ID: "e2", Ports: []int{22}}); err != nil { // non-inverted
		t.Fatal(err)
	}

	if err := s.Promote("never-existed", nil); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Promote(unknown id) = %v, want ErrEntryNotFound", err)
	}
	if err := s.Promote("e2", nil); !errors.Is(err, ErrNotInverted) {
		t.Errorf("Promote(non-inverted) = %v, want ErrNotInverted", err)
	}
}

func TestSetObserving(t *testing.T) {
	s := mustOpenStore(t)
	mustUpsertInverted(t, s, "e1") // starts Observing: true

	if err := s.SetObserving("e1", false); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Get("e1")
	if e.Observing {
		t.Error("SetObserving(false) did not clear Observing")
	}

	if err := s.SetObserving("e1", true); err != nil {
		t.Fatal(err)
	}
	e, _ = s.Get("e1")
	if !e.Observing {
		t.Error("SetObserving(true) did not set Observing")
	}
}

func TestSetObservingErrors(t *testing.T) {
	s := mustOpenStore(t)
	if err := s.Upsert(Entry{ID: "e2", Ports: []int{22}}); err != nil { // non-inverted
		t.Fatal(err)
	}

	if err := s.SetObserving("never-existed", true); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("SetObserving(unknown id) = %v, want ErrEntryNotFound", err)
	}
	if err := s.SetObserving("e2", true); !errors.Is(err, ErrNotInverted) {
		t.Errorf("SetObserving(non-inverted) = %v, want ErrNotInverted", err)
	}
}

// The observe/promote/permitted lifecycle must survive a restart just
// like everything else the store persists.
func TestObserveAndPermittedSurviveRestart(t *testing.T) {
	path := t.TempDir() + "/watchlist.json"
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustUpsertInverted(t, s1, "e1")
	s1.RecordObservation("e1", "10.0.0.5", 8883, time.Now())
	if err := s1.Promote("e1", []PermittedDest{{DestIP: "1.2.3.4", Port: 443}}); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetObserving("e1", false); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	e, ok := s2.Get("e1")
	if !ok {
		t.Fatal("entry not found after restart")
	}
	if e.Observing {
		t.Error("Observing did not survive restart as false")
	}
	if len(e.Permitted) != 1 || e.Permitted[0].DestIP != "1.2.3.4" {
		t.Errorf("Permitted did not survive restart: %+v", e.Permitted)
	}
	if len(e.Observed) != 1 || e.Observed[0].DestIP != "10.0.0.5" {
		t.Errorf("Observed did not survive restart: %+v", e.Observed)
	}
}
