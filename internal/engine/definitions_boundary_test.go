// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// #806's boundary rides in the definitions blob the same way the window
// and the nightly history do (definitions_nights_test.go's own file
// comment) -- a field addition, not a schema change, so the proof that
// matters is a real file-backed round trip.

// TestBoundarySurvivesARestart pins the persistence claim: a boundary an
// operator set comes back out of the definitions document unchanged, with
// no migration run.
func TestBoundarySurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	s, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	want := watchlist.Entry{
		ID:       "e1",
		Name:     "cam-porch quiet hours",
		Ports:    []int{443},
		Boundary: watchlist.Boundary{Chain: "forward", InInterface: "ether1", OutInterface: "bridge1"},
	}
	if err := s.UpsertExpectation(want); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("closing: %v", err)
	}

	reopened, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })

	got := mustGetEntry(t, reopened, "e1")
	if got.Boundary != want.Boundary {
		t.Errorf("boundary came back as %+v, want %+v", got.Boundary, want.Boundary)
	}
}

// TestAnEntryWithoutABoundaryLoadsUnscoped is the migration guarantee
// #806's own decision names explicitly: every entry stored before this
// field existed has no boundaryJSON param at all, and must load with the
// zero (unscoped) Boundary rather than erroring or inventing one.
func TestAnEntryWithoutABoundaryLoadsUnscoped(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	sd, ok := s.Get("e1")
	if !ok {
		t.Fatal("the entry is missing")
	}
	if _, present := sd.Definition.Params["boundaryJSON"]; present {
		t.Errorf("boundaryJSON was written for an entry with no boundary")
	}
	got := mustGetEntry(t, s, "e1")
	if !got.Boundary.Empty() {
		t.Errorf("an entry with no boundary came back scoped to %+v", got.Boundary)
	}
}

// TestBoundarySurvivesOnAnInvertedEntryToo: both watchlist schemas declare
// boundaryJSON (definitions_convert.go), since either kind of watcher can
// be scoped to a boundary.
func TestBoundarySurvivesOnAnInvertedEntryToo(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	want := watchlist.Entry{
		ID:       "e1",
		Invert:   true,
		Source:   matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		Boundary: watchlist.Boundary{Chain: "forward", InInterface: "ether1", OutInterface: "bridge1"},
	}
	if err := s.UpsertExpectation(want); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	got := mustGetEntry(t, s, "e1")
	if got.Boundary != want.Boundary {
		t.Errorf("boundary came back as %+v, want %+v", got.Boundary, want.Boundary)
	}
}
