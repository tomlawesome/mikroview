// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file carries forward the test coverage issue #407 orphaned when it
// deleted internal/watchlist.Store:
//
//   - internal/watchlist/store_test.go's storage tests (upsert/get,
//     delete, list, CreatedAt preservation, restart survival, in-memory
//     mode) -- watchlist.Store.Upsert/Get/Delete/List's contract, now
//     DefinitionsStore.UpsertExpectation/GetExpectation/
//     DeleteExpectation/ListExpectations's. store_test.go's own
//     *validation* tests (empty ID, no ports, and so on) moved instead to
//     internal/watchlist/validate_test.go, against the exported
//     ValidateEntry -- they test a rule, not storage, and the rule did
//     not move.
//   - internal/watchlist/invert_test.go's RecordObservation* and
//     SetObserving* tests -- watchlist.Store.RecordObservation is now
//     DefinitionsStore.RecordObservation unchanged in behaviour (moved
//     with its own doc comment intact, definitions_expectations.go), and
//     SetObserving (a store method before #407) is now an ordinary
//     UpdateExpectation mutation, since there is no bespoke per-field
//     setter left to move.
//   - invert_test.go's TestObserveAndPermittedSurviveRestart, the pin
//     that an upgrade keeps its observations -- rebuilt here as a genuine
//     file-backed round trip through DefinitionsStore, the same proof it
//     was before, against the store that now holds this state.
//   - invert_test.go's TestPromoteErrors, whose *cases* (not found,
//     not inverted) no longer belong to Promote (a plain *Entry method
//     since #407, with no id to look up and no store to refuse against --
//     see invert_test.go's own note) but to UpdateExpectation, the door
//     every mutation of a stored entry -- Promote included -- now goes
//     through. TestSetObservingErrors asserted the identical two cases
//     against the same door under a different name (SetObserving), so its
//     assertions are the same test as TestPromoteErrors's once both are
//     expressed against UpdateExpectation; folded in here rather than
//     duplicated for no new coverage.

// mustOpenExpectationsStore opens an in-memory (no persistence)
// DefinitionsStore -- the right default for a test that only cares about
// the entry-set contract, not about surviving a restart. Close is a safe
// no-op with no backend configured (DefinitionsStore.Close's own doc
// comment), so no cleanup is required.
func mustOpenExpectationsStore(t *testing.T) *DefinitionsStore {
	t.Helper()
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatalf("OpenDefinitionsStore(\"\"): %v", err)
	}
	return s
}

// mustUpsertInvertedExpectation creates an inverted, observing entry and
// reads it back -- the fixture every RecordObservation/UpdateExpectation
// test below starts from, the same role mustUpsertInverted played in
// invert_test.go before #407.
func mustUpsertInvertedExpectation(t *testing.T, s *DefinitionsStore, id string) watchlist.Entry {
	t.Helper()
	e := watchlist.Entry{ID: id, Invert: true, Observing: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}
	if err := s.UpsertExpectation(e); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	got, _, err := s.GetExpectation(id)
	if err != nil {
		t.Fatalf("GetExpectation: %v", err)
	}
	return got
}

// --- storage: upsert/get/delete/list ---------------------------------

func TestUpsertExpectationAndGet(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	e := watchlist.Entry{ID: "e1", Name: "SSH watch", Ports: []int{22}}
	if err := s.UpsertExpectation(e); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	got, ok, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatalf("GetExpectation: %v", err)
	}
	if !ok {
		t.Fatal("GetExpectation did not find the entry just upserted")
	}
	if got.Name != "SSH watch" || len(got.Ports) != 1 || got.Ports[0] != 22 {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set on first creation")
	}
}

func TestGetExpectationMissingReturnsFalse(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if _, ok, err := s.GetExpectation("nope"); err != nil || ok {
		t.Errorf("GetExpectation(\"nope\") = (ok=%v, err=%v), want (false, nil) for an entry that was never created", ok, err)
	}
}

// An update must not reset CreatedAt to now -- how long an entry has
// existed is meaningful (e.g. for a future "how long has this been
// observing" display) and a routine edit isn't a new entry.
func TestUpsertExpectationUpdatePreservesCreatedAt(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	first, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22, 23}, Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	second, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}

	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt changed on update: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if second.Name != "renamed" || len(second.Ports) != 2 {
		t.Errorf("update did not apply: %+v", second)
	}
}

func TestDeleteExpectationRemovesEntry(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExpectation("e1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetExpectation("e1"); err != nil || ok {
		t.Errorf("entry still present after DeleteExpectation: ok=%v, err=%v", ok, err)
	}
}

func TestDeleteExpectationMissingIsNoop(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.DeleteExpectation("never-existed"); err != nil {
		t.Errorf("DeleteExpectation(never-existed) = %v, want nil (a no-op)", err)
	}
}

func TestListExpectationsIsSortedByID(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	for _, id := range []string{"c", "a", "b"} {
		if err := s.UpsertExpectation(watchlist.Entry{ID: id, Ports: []int{22}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListExpectations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("ListExpectations() returned %d entries, want 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].ID != want {
			t.Errorf("ListExpectations()[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestExpectationsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	s1, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := watchlist.Entry{ID: "e1", Name: "SSH watch", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Ports: []int{22, 2222}}
	if err := s1.UpsertExpectation(entry); err != nil {
		t.Fatal(err)
	}
	flushDefinitionsForTest(t, s1)

	s2, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	got, ok, err := s2.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("entry not found after restart")
	}
	if got.Name != entry.Name || got.Source.MAC != entry.Source.MAC || got.DestIP != entry.DestIP || len(got.Ports) != 2 {
		t.Errorf("entry not intact after restart: %+v", got)
	}
}

func TestExpectationsEmptyPathIsInMemoryOnly(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetExpectation("e1"); err != nil || !ok {
		t.Errorf("in-memory-only store did not retain an upserted entry within the same process: ok=%v, err=%v", ok, err)
	}
}

// --- RecordObservation -------------------------------------------------

func TestRecordObservationAddsACandidate(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1")
	now := time.Now()

	s.RecordObservation("e1", "10.0.0.5", 8883, now)

	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
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
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1")
	t0 := time.Now()

	for i := 0; i < 5; i++ {
		s.RecordObservation("e1", "10.0.0.5", 8883, t0.Add(time.Duration(i)*time.Minute))
	}

	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
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
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1")
	now := time.Now()

	s.RecordObservation("e1", "10.0.0.5", 8883, now)
	s.RecordObservation("e1", "10.0.0.6", 8883, now) // different dest
	s.RecordObservation("e1", "10.0.0.5", 443, now)  // different port

	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Observed) != 3 {
		t.Errorf("Observed has %d entries, want 3 distinct candidates", len(e.Observed))
	}
}

func TestRecordObservationIsNoopForUnknownOrNonInvertedEntry(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.RecordObservation("never-existed", "10.0.0.5", 8883, time.Now()) // must not panic

	if err := s.UpsertExpectation(watchlist.Entry{ID: "e2", Ports: []int{22}}); err != nil { // non-inverted
		t.Fatal(err)
	}
	s.RecordObservation("e2", "10.0.0.5", 8883, time.Now())
	e, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Observed) != 0 {
		t.Errorf("a non-inverted entry gained an observation: %+v", e.Observed)
	}
}

// The risk #243 open question 7 names directly -- an inverted entry in
// observe state collecting unbounded volume. RecordObservation must cap
// rather than grow without bound.
func TestRecordObservationCapsAtMaxObservedPerEntry(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1")

	orig := maxObservedPerEntry
	maxObservedPerEntry = 3
	t.Cleanup(func() { maxObservedPerEntry = orig })

	now := time.Now()
	for i := 0; i < 10; i++ {
		s.RecordObservation("e1", "10.0.0.5", i, now) // 10 distinct ports -> 10 distinct candidates
	}

	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Observed) != 3 {
		t.Fatalf("Observed has %d entries, want capped at 3", len(e.Observed))
	}
}

// A repeat of an already-observed pair must keep updating even once the
// cap is reached -- collapsing an existing candidate costs no new
// capacity, mirroring internal/matchlog's own "collapsing still works at
// capacity" rule.
func TestRecordObservationStillCollapsesAtCap(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1")

	orig := maxObservedPerEntry
	maxObservedPerEntry = 1
	t.Cleanup(func() { maxObservedPerEntry = orig })

	t0 := time.Now()
	s.RecordObservation("e1", "10.0.0.5", 8883, t0)
	s.RecordObservation("e1", "10.0.0.5", 8883, t0.Add(time.Minute)) // same pair, cap already at 1

	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Observed) != 1 || e.Observed[0].Count != 2 {
		t.Fatalf("got %+v, want one observation with Count=2", e.Observed)
	}
}

// --- UpdateExpectation: observing toggle and error contract -----------

// TestUpdateExpectationTogglesObserving is TestSetObserving's positive
// path, moved: SetObserving was a bespoke store method before #407; it is
// an ordinary field mutation through UpdateExpectation now, since there
// is no per-field setter left on this store to call instead.
func TestUpdateExpectationTogglesObserving(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	mustUpsertInvertedExpectation(t, s, "e1") // starts Observing: true

	if err := s.UpdateExpectation("e1", func(e *watchlist.Entry) error {
		e.Observing = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e, _, err := s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Observing {
		t.Error("UpdateExpectation did not clear Observing")
	}

	if err := s.UpdateExpectation("e1", func(e *watchlist.Entry) error {
		e.Observing = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e, _, err = s.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
	if !e.Observing {
		t.Error("UpdateExpectation did not set Observing")
	}
}

// TestUpdateExpectationUnknownIDIsErrEntryNotFound carries forward
// TestPromoteErrors's and TestSetObservingErrors's identical unknown-id
// assertion (Promote("never-existed", ...) and SetObserving("never-existed",
// ...) both wanted ErrEntryNotFound against watchlist.Store): with both
// methods gone, this is now UpdateExpectation's own contract, tested once
// rather than once per caller that used to wrap it.
func TestUpdateExpectationUnknownIDIsErrEntryNotFound(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	err := s.UpdateExpectation("never-existed", func(e *watchlist.Entry) error {
		return nil
	})
	if !errors.Is(err, watchlist.ErrEntryNotFound) {
		t.Errorf("UpdateExpectation(unknown id) = %v, want an error matching watchlist.ErrEntryNotFound", err)
	}
}

// TestUpdateExpectationMutateErrorIsNotPersisted carries forward
// TestPromoteErrors's and TestSetObservingErrors's identical
// not-inverted assertion. Promote and SetObserving used to refuse a
// non-inverted entry themselves (ErrNotInverted) before ever touching
// storage; now that refusal is the caller's own mutate closure's job --
// UpdateExpectation itself has no opinion on Invert -- so this pins the
// other half of the contract that matters once the check moved:
// whatever error the mutate closure returns must come back out of
// UpdateExpectation unchanged, and the entry on disk must be exactly as
// it was before the call (no partial write of whatever the closure did
// before returning the error).
func TestUpdateExpectationMutateErrorIsNotPersisted(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e2", Ports: []int{22}}); err != nil { // non-inverted
		t.Fatal(err)
	}
	before, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateExpectation("e2", func(e *watchlist.Entry) error {
		if !e.Invert {
			return watchlist.ErrNotInverted
		}
		e.Observing = true // would be a write, if the error below didn't stop it
		return nil
	})
	if !errors.Is(err, watchlist.ErrNotInverted) {
		t.Errorf("UpdateExpectation(mutate returns ErrNotInverted) = %v, want an error matching watchlist.ErrNotInverted", err)
	}

	after, _, err := s.GetExpectation("e2")
	if err != nil {
		t.Fatal(err)
	}
	if after.Observing != before.Observing {
		t.Errorf("UpdateExpectation persisted a change despite the mutate closure returning an error: before=%+v after=%+v", before, after)
	}
}

// --- observe/promote/permitted lifecycle survives a restart -----------

// TestObserveAndPermittedSurviveRestart is the pin proving an upgrade
// keeps its observations: the observe/promote/permitted lifecycle must
// survive a restart just like everything else DefinitionsStore persists.
// Genuinely round-trips through a file-backed store (open, mutate,
// Flush, reopen, assert) rather than asserting against the same process's
// in-memory state -- the only way this pin actually proves what it
// claims.
func TestObserveAndPermittedSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	s1, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	mustUpsertInvertedExpectation(t, s1, "e1")
	s1.RecordObservation("e1", "10.0.0.5", 8883, time.Now())
	if err := s1.UpdateExpectation("e1", func(e *watchlist.Entry) error {
		e.Promote([]watchlist.PermittedDest{{DestIP: "1.2.3.4", Port: 443}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s1.UpdateExpectation("e1", func(e *watchlist.Entry) error {
		e.Observing = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	flushDefinitionsForTest(t, s1)

	s2, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	e, ok, err := s2.GetExpectation("e1")
	if err != nil {
		t.Fatal(err)
	}
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
