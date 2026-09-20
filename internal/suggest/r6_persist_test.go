// SPDX-License-Identifier: AGPL-3.0-only

package suggest

import (
	"context"
	"errors"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// failingSaveBackend lets Open succeed (nothing stored yet) but fails
// every Save -- the v0.6.0 audit's R6 fix needs a backend that can never
// durably record the change a scoped call is about to make. Copied from
// internal/auth/store_test.go's fixture of the same name/shape, this
// package's own tests having had no equivalent before this fix.
type failingSaveBackend struct{}

func (failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("backend unavailable")
}
func (failingSaveBackend) Close() error     { return nil }
func (failingSaveBackend) Describe() string { return "failing test backend" }

// saveBudgetBackend allows a fixed number of Saves and then fails every
// one after -- for the R6 cases here where the change under test has to
// land on a store that already holds something (an Off candidate to
// Accept/Hide, or a Hide one to Unhide). Same shape as
// internal/auth/store_test.go's fixture of the same name.
type saveBudgetBackend struct{ left int }

func (b *saveBudgetBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (b *saveBudgetBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	if b.left <= 0 {
		return 0, errors.New("backend unavailable")
	}
	b.left--
	return expect + 1, nil
}
func (b *saveBudgetBackend) Close() error     { return nil }
func (b *saveBudgetBackend) Describe() string { return "save-budget test backend" }

// This file is the v0.6.0 audit's R6 fix for this store: Accept, Hide,
// Unhide, Reset and MarkHiddenByEntry all change a candidate's Status --
// an operator accepting, dismissing, un-dismissing, wiping, or a
// definition delete forcing one to Hide -- so a failed save must not be
// reported as kept, and the in-memory state each would have changed must
// be put back exactly as it was -- see each method's own
// restore-on-error comment. Every test here proves both halves: a
// non-nil error, and the store reading back unchanged afterward.

func TestAcceptFailedSaveLeavesCandidateOff(t *testing.T) {
	s, err := OpenWithBackend(failingSaveBackend{})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	// Sync's own save is swallow-and-log (bookkeeping, periodic and
	// idempotent -- see tryPersistLocked's doc comment), so it never
	// reports the backend failure itself; this call only sets up the
	// candidate to accept below. Its returned error is about candidate
	// validation, not persistence, and is nil for this well-formed input.
	if err := s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "n"}}); err != nil {
		t.Fatalf("setup: Sync failed: %v", err)
	}
	before, ok := s.Get("c1")
	if !ok || before.Status != StatusOff {
		t.Fatalf("setup: candidate = %+v, want Status off", before)
	}

	if err := s.Accept("c1", "entry-1"); err == nil {
		t.Fatal("expected Accept to report the save failure")
	}

	after, ok := s.Get("c1")
	if !ok {
		t.Fatal("candidate disappeared after a failed save")
	}
	if after.Status != StatusOff || after.EntryID != "" {
		t.Errorf("Accept changed the candidate despite the failed save: %+v", after)
	}
}

func TestHideFailedSaveLeavesCandidateOff(t *testing.T) {
	s, err := OpenWithBackend(failingSaveBackend{})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "n"}})

	if err := s.Hide("c1"); err == nil {
		t.Fatal("expected Hide to report the save failure")
	}

	after, ok := s.Get("c1")
	if !ok || after.Status != StatusOff {
		t.Errorf("Hide changed the candidate despite the failed save: %+v", after)
	}
}

func TestUnhideFailedSaveLeavesCandidateHidden(t *testing.T) {
	// left:2 covers Sync's own save plus Hide's below -- both must
	// succeed to reach the state this test is actually about, so the
	// budget runs out exactly on Unhide's own save.
	b := &saveBudgetBackend{left: 2}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "n"}})
	if err := s.Hide("c1"); err != nil {
		t.Fatalf("setup: Hide failed: %v", err)
	}
	before, _ := s.Get("c1")
	if before.Status != StatusHide {
		t.Fatalf("setup: candidate = %+v, want Status hide", before)
	}

	if err := s.Unhide("c1"); err == nil {
		t.Fatal("expected Unhide to report the save failure")
	}

	after, _ := s.Get("c1")
	if after.Status != StatusHide {
		t.Errorf("Unhide changed the candidate despite the failed save: %+v", after)
	}
}

func TestMarkHiddenByEntryFailedSaveLeavesCandidateOn(t *testing.T) {
	// left:2 covers Sync's own save plus Accept's below -- see
	// TestUnhideFailedSaveLeavesCandidateHidden's comment for why.
	b := &saveBudgetBackend{left: 2}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "n"}})
	if err := s.Accept("c1", "entry-1"); err != nil {
		t.Fatalf("setup: Accept failed: %v", err)
	}
	before, _ := s.Get("c1")

	if err := s.MarkHiddenByEntry("entry-1"); err == nil {
		t.Fatal("expected MarkHiddenByEntry to report the save failure")
	}

	after, _ := s.Get("c1")
	if after.Status != before.Status || after.EntryID != before.EntryID {
		t.Errorf("MarkHiddenByEntry changed the candidate despite the failed save: before=%+v after=%+v", before, after)
	}
}

func TestResetFailedSaveLeavesCandidatesInPlace(t *testing.T) {
	b := &saveBudgetBackend{left: 1}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "n"}})
	beforeList := s.List()
	if len(beforeList) != 1 {
		t.Fatalf("setup: List() = %+v, want one candidate", beforeList)
	}

	if err := s.Reset(); err == nil {
		t.Fatal("expected Reset to report the save failure")
	}

	afterList := s.List()
	if len(afterList) != 1 || afterList[0].ID != "c1" {
		t.Errorf("Reset wiped candidates despite the failed save: %+v", afterList)
	}
}
