// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is issue #376's own repro, moved from
// internal/watchlist/race_test.go when #407 deleted watchlist.Store and
// the entry set it protected against this exact race moved into
// DefinitionsStore: a reader goroutine calling the store's
// cross-goroutine read API (ListExpectations and GetExpectation) and
// marshalling the result -- exactly what the definitions API's list/get
// handlers do -- while the evaluation goroutine records observations and
// an operator's update (promote) runs concurrently.
//
// The mechanism this protects against changed with the store. Before
// Entry.clone (the watchlist.Store era), List/Get handed out shallow
// struct copies whose Observed/Permitted/Ports slices still pointed at
// the stored entry's backing arrays, and this shape reported DATA RACE
// rooted at RecordObservation's element write and Promote's in-place
// compaction. DefinitionsStore never hands out a live Go value at all:
// every read (Get/List, and therefore GetExpectation/ListExpectations)
// decodes a fresh watchlist.Entry from that entry's stored JSON bytes on
// every call (definitions_store.go's decodeStored, called under s.mu),
// so a reader's result cannot alias anything a writer later mutates --
// aliasing is structurally impossible now, not merely absent by
// construction of a deep copy. These tests are kept anyway, as
// concurrency proofs rather than aliasing proofs: they are still the
// cheapest way to catch a future change that reintroduces a shared
// mutable value on this path (e.g. a caching layer over decodeStored).
// Run under -race (`go test -race ./internal/engine/`), which is what
// makes them mean anything; without it they only prove the API does not
// panic.

// TestListExpectationsIsSafeAgainstConcurrentObservationAndPromote is
// #376's headline scenario, carried over onto DefinitionsStore. Closes
// #376.
func TestListExpectationsIsSafeAgainstConcurrentObservationAndPromote(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	if err := s.UpsertExpectation(watchlist.Entry{ID: "device-x", Invert: true, Observing: true, Source: src}); err != nil {
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

	// An operator promoting, which compacts Observed in place on the
	// *decoded* entry inside UpdateExpectation's mutate callback -- the
	// stored bytes are only ever replaced wholesale afterwards.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.UpdateExpectation("device-x", func(e *watchlist.Entry) error {
				e.Promote([]watchlist.PermittedDest{{DestIP: "198.51.100.10", Port: 80}})
				return nil
			})
		}
	}()

	// The reader: ListExpectations() then marshal, with no lock held --
	// the shape the definitions API's list handler has.
	go func() {
		defer wg.Done()
		enc := json.NewEncoder(io.Discard)
		for i := 0; i < iterations; i++ {
			list, err := s.ListExpectations()
			if err != nil {
				t.Error(err)
				return
			}
			for _, e := range list {
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

// TestGetExpectationIsSafeAgainstConcurrentObservation covers
// GetExpectation's own half of #376: the same shallow copy fed the
// create/update/promote/observing handlers' response marshalling before
// #407.
func TestGetExpectationIsSafeAgainstConcurrentObservation(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertExpectation(watchlist.Entry{ID: "device-x", Invert: true, Observing: true, Source: matchlog.Identity{IP: "192.168.1.5"}}); err != nil {
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
			e, ok, err := s.GetExpectation("device-x")
			if err != nil {
				t.Error(err)
				return
			}
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

// TestListExpectationsResultDoesNotAliasStoredState is the same property
// stated without concurrency: a caller mutating what it was handed must
// not be able to reach back into the store. DefinitionsStore decodes a
// fresh watchlist.Entry from stored bytes on every call, so there is no
// shared backing array to alias in the first place -- this is the direct,
// deterministic evidence that decoding-fresh actually behaves that way,
// the same role TestListResultDoesNotAliasStoredState played for the deep
// copy watchlist.Store used to return.
func TestListExpectationsResultDoesNotAliasStoredState(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22, 443}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertExpectation(watchlist.Entry{
		ID: "e2", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		Permitted: []watchlist.PermittedDest{{DestIP: "198.51.100.10", Port: 80}},
		Observed:  []watchlist.ObservedDest{{DestIP: "198.51.100.20", Port: 443, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListExpectations()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
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

	e1, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(e1.Ports) != 2 || e1.Ports[0] != 22 || e1.Ports[1] != 443 {
		t.Errorf("stored Ports = %v, want [22 443] -- a ListExpectations() caller reached back into the store", e1.Ports)
	}
	e2, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}
	if len(e2.Permitted) != 1 || e2.Permitted[0].Port != 80 {
		t.Errorf("stored Permitted = %+v, want port 80", e2.Permitted)
	}
	if len(e2.Observed) != 1 || e2.Observed[0].Count != 1 {
		t.Errorf("stored Observed = %+v, want Count 1", e2.Observed)
	}

	// GetExpectation's own result is the same contract.
	got, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}
	got.Observed[0].Count = 4242
	again, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}
	if again.Observed[0].Count != 1 {
		t.Errorf("stored Observed Count = %d after mutating a GetExpectation() result, want 1", again.Observed[0].Count)
	}
}
