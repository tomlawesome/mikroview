// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

// This file is issue #376's own repro, kept as a permanent test: a
// reader goroutine calling the store's cross-goroutine read API (List
// and Get) and marshalling the result -- exactly what
// handleWatchlistEntriesList does after the store's lock is gone --
// while the evaluation goroutine records observations and an operator's
// promote runs concurrently.
//
// Before Entry.clone (watchlist.go), List/Get handed out shallow struct
// copies whose Observed/Permitted/Ports slices still pointed at the
// stored entry's backing arrays, and this shape reported DATA RACE
// rooted at RecordObservation's element write and Promote's in-place
// compaction. Run under -race (`go test -race ./internal/watchlist/`),
// which is what makes these tests mean anything; without it they only
// prove the API does not panic.

// TestListIsSafeAgainstConcurrentObservationAndPromote is #376's headline
// scenario. Closes #376.
func TestListIsSafeAgainstConcurrentObservationAndPromote(t *testing.T) {
	s, err := OpenWithBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	if err := s.Upsert(Entry{ID: "device-x", Invert: true, Observing: true, Source: src}); err != nil {
		t.Fatal(err)
	}

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(3)

	// The evaluation goroutine: RecordObservation on every Observed
	// outcome. The first call for a (destIP, port) pair appends; every
	// later one writes LastSeen/Count into the existing element in
	// place, which is the write the reader used to race.
	go func() {
		defer wg.Done()
		t0 := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
		for i := 0; i < iterations; i++ {
			s.RecordObservation("device-x", "198.51.100.10", 80, t0.Add(time.Duration(i)*time.Second))
			s.RecordObservation("device-x", "198.51.100.20", 443, t0.Add(time.Duration(i)*time.Second))
		}
	}()

	// An operator promoting, which compacts Observed in place.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.Promote("device-x", []PermittedDest{{DestIP: "198.51.100.10", Port: 80}})
		}
	}()

	// The reader: List() then marshal, with no lock held -- the exact
	// shape handleWatchlistEntriesList has.
	go func() {
		defer wg.Done()
		enc := json.NewEncoder(io.Discard)
		for i := 0; i < iterations; i++ {
			for _, e := range s.List() {
				if err := enc.Encode(e); err != nil {
					t.Error(err)
					return
				}
				// Reading the slices element by element too, not only
				// through the encoder: a race is a race whether or not
				// encoding/json happens to touch the field.
				for _, o := range e.Observed {
					_ = o.Count
					_ = o.LastSeen
				}
				for _, p := range e.Permitted {
					_ = p.Port
				}
			}
		}
	}()

	wg.Wait()
}

// TestGetIsSafeAgainstConcurrentObservation covers Get's own half of
// #376: the same shallow copy fed the create/update/promote/observing
// handlers' response marshalling.
func TestGetIsSafeAgainstConcurrentObservation(t *testing.T) {
	s, err := OpenWithBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Entry{ID: "device-x", Invert: true, Observing: true, Source: matchlog.Identity{IP: "192.168.1.5"}}); err != nil {
		t.Fatal(err)
	}

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		t0 := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
		for i := 0; i < iterations; i++ {
			s.RecordObservation("device-x", "198.51.100.10", 80, t0.Add(time.Duration(i)*time.Second))
		}
	}()

	go func() {
		defer wg.Done()
		enc := json.NewEncoder(io.Discard)
		for i := 0; i < iterations; i++ {
			e, ok := s.Get("device-x")
			if !ok {
				t.Error("entry disappeared")
				return
			}
			if err := enc.Encode(e); err != nil {
				t.Error(err)
				return
			}
		}
	}()

	wg.Wait()
}

// TestListResultDoesNotAliasStoredState is the same property stated
// without concurrency: a caller mutating what it was handed must not be
// able to reach back into the store. The deep copy is what makes the
// race impossible; this is the direct, deterministic evidence that the
// copy is real rather than a scheduling accident that -race happened not
// to catch.
func TestListResultDoesNotAliasStoredState(t *testing.T) {
	s, err := OpenWithBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22, 443}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Entry{
		ID: "e2", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		Permitted: []PermittedDest{{DestIP: "198.51.100.10", Port: 80}},
		Observed:  []ObservedDest{{DestIP: "198.51.100.20", Port: 443, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	for _, e := range s.List() {
		for i := range e.Ports {
			e.Ports[i] = 9999
		}
		for i := range e.Permitted {
			e.Permitted[i].Port = 9999
		}
		for i := range e.Observed {
			e.Observed[i].Count = 9999
		}
	}

	e1, _ := s.Get("e1")
	if len(e1.Ports) != 2 || e1.Ports[0] != 22 || e1.Ports[1] != 443 {
		t.Errorf("stored Ports = %v, want [22 443] -- a List() caller reached back into the store", e1.Ports)
	}
	e2, _ := s.Get("e2")
	if len(e2.Permitted) != 1 || e2.Permitted[0].Port != 80 {
		t.Errorf("stored Permitted = %+v, want port 80", e2.Permitted)
	}
	if len(e2.Observed) != 1 || e2.Observed[0].Count != 1 {
		t.Errorf("stored Observed = %+v, want Count 1", e2.Observed)
	}

	// Get's own result is the same contract.
	got, _ := s.Get("e2")
	got.Observed[0].Count = 4242
	again, _ := s.Get("e2")
	if again.Observed[0].Count != 1 {
		t.Errorf("stored Observed Count = %d after mutating a Get() result, want 1", again.Observed[0].Count)
	}
}
