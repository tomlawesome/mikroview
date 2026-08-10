// SPDX-License-Identifier: AGPL-3.0-only

package suggest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

func mustOpen(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	return s
}

func TestSyncAddsNewCandidatesAtOff(t *testing.T) {
	s := mustOpen(t)
	if err := s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("c1")
	if !ok {
		t.Fatal("candidate not found after Sync")
	}
	if got.Status != StatusOff {
		t.Errorf("Status = %q, want %q", got.Status, StatusOff)
	}
	if got.FirstSeen.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("FirstSeen/UpdatedAt not set on a new candidate")
	}
}

func TestSyncPreservesOnStatusAcrossResync(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}))
	must(t, s.Accept("c1", "entry-1"))

	// Same candidate generated again -- On must survive, unchanged.
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH (renamed)", Justification: "rule x"}}))
	got, _ := s.Get("c1")
	if got.Status != StatusOn || got.EntryID != "entry-1" {
		t.Errorf("On candidate disturbed by resync: %+v", got)
	}
	if got.Name != "SSH (renamed)" {
		t.Errorf("Name = %q, want the refreshed value", got.Name)
	}
}

func TestSyncPreservesHideStatusAcrossResync(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}))
	must(t, s.Hide("c1"))

	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}))
	got, _ := s.Get("c1")
	if got.Status != StatusHide {
		t.Errorf("Status = %q, want %q -- Hide must never be reset by a resync", got.Status, StatusHide)
	}
}

func TestSyncMarksOnCandidateStaleWhenMissingFromFreshBatch(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}))
	must(t, s.Accept("c1", "entry-1"))

	// The rule that justified c1 is gone -- it no longer appears in the
	// generated batch at all.
	must(t, s.Sync(nil))
	got, _ := s.Get("c1")
	if !got.Stale {
		t.Error("On candidate missing from a resync was not marked Stale")
	}
	if got.Status != StatusOn || got.EntryID != "entry-1" {
		t.Errorf("Stale must not change Status or EntryID: %+v", got)
	}
}

func TestSyncClearsStaleWhenCandidateReappears(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}}))
	must(t, s.Accept("c1", "entry-1"))
	must(t, s.Sync(nil))                                                                           // goes stale
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "SSH", Justification: "rule x"}})) // reappears

	got, _ := s.Get("c1")
	if got.Stale {
		t.Error("Stale was not cleared once the candidate's justification reappeared")
	}
}

func TestSyncDoesNotMarkOffOrHideCandidatesStale(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{
		{ID: "off1", Kind: KindPort, Name: "a", Justification: "x"},
		{ID: "hide1", Kind: KindPort, Name: "b", Justification: "y"},
	}))
	must(t, s.Hide("hide1"))

	must(t, s.Sync(nil)) // neither reappears

	off, _ := s.Get("off1")
	hide, _ := s.Get("hide1")
	if off.Stale || hide.Stale {
		t.Error("Stale is only meaningful for On candidates -- Off/Hide must never be marked")
	}
}

func TestSyncNeverRemovesAnyCandidate(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: "a", Justification: "x"}}))
	must(t, s.Sync(nil))
	if _, ok := s.Get("c1"); !ok {
		t.Error("Sync removed a candidate that stopped being generated -- it must only ever add or flag, never remove")
	}
}

func TestSyncRejectsCandidateWithNoIDOrKind(t *testing.T) {
	s := mustOpen(t)
	if err := s.Sync([]Candidate{{Kind: KindPort}}); err == nil {
		t.Error("Sync accepted a candidate with no ID")
	}
	if err := s.Sync([]Candidate{{ID: "c1"}}); err == nil {
		t.Error("Sync accepted a candidate with no Kind")
	}
}

func TestSyncRejectsInvalidText(t *testing.T) {
	s := mustOpen(t)
	long := strings.Repeat("a", maxTextLen+1)
	if err := s.Sync([]Candidate{{ID: "c1", Kind: KindPort, Name: long}}); err == nil {
		t.Error("Sync accepted an oversized Name")
	}
}

func TestSyncRejectingOneCandidateAppliesNothing(t *testing.T) {
	s := mustOpen(t)
	long := strings.Repeat("a", maxTextLen+1)
	err := s.Sync([]Candidate{
		{ID: "good", Kind: KindPort, Name: "fine"},
		{ID: "bad", Kind: KindPort, Name: long},
	})
	if err == nil {
		t.Fatal("expected the whole batch to be rejected")
	}
	if _, ok := s.Get("good"); ok {
		t.Error("a valid candidate in a rejected batch was still applied -- Sync must validate before mutating, not partially apply")
	}
}

func TestAcceptRequiresOff(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	must(t, s.Hide("c1"))
	if err := s.Accept("c1", "e1"); err != ErrNotOff {
		t.Errorf("Accept from Hide = %v, want ErrNotOff", err)
	}
}

func TestAcceptUnknownID(t *testing.T) {
	s := mustOpen(t)
	if err := s.Accept("nope", "e1"); err != ErrNotFound {
		t.Errorf("Accept(unknown) = %v, want ErrNotFound", err)
	}
}

func TestHideRequiresOff(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	must(t, s.Accept("c1", "e1"))
	if err := s.Hide("c1"); err != ErrNotOff {
		t.Errorf("Hide from On = %v, want ErrNotOff", err)
	}
}

func TestUnhideRequiresHide(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	if err := s.Unhide("c1"); err != ErrNotHidden {
		t.Errorf("Unhide from Off = %v, want ErrNotHidden", err)
	}
}

func TestHideThenUnhideReturnsToOff(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	must(t, s.Hide("c1"))
	must(t, s.Unhide("c1"))
	got, _ := s.Get("c1")
	if got.Status != StatusOff {
		t.Errorf("Status after Hide then Unhide = %q, want %q", got.Status, StatusOff)
	}
}

func TestMarkHiddenByEntryFindsAndHides(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}}))
	must(t, s.Accept("c1", "entry-1"))

	s.MarkHiddenByEntry("entry-1")

	got, _ := s.Get("c1")
	if got.Status != StatusHide {
		t.Errorf("Status = %q, want %q after its entry was deleted", got.Status, StatusHide)
	}
	if got.EntryID != "" {
		t.Errorf("EntryID = %q, want cleared once the entry it pointed to is gone", got.EntryID)
	}
}

func TestMarkHiddenByEntryNoOpWhenNotFound(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	s.MarkHiddenByEntry("no-such-entry")
	got, _ := s.Get("c1")
	if got.Status != StatusOff {
		t.Errorf("an unrelated entry deletion changed an unrelated candidate: %+v", got)
	}
}

func TestReset(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}, {ID: "c2", Kind: KindDevice}}))
	must(t, s.Accept("c1", "e1"))
	s.Reset()
	if len(s.List()) != 0 {
		t.Errorf("List() after Reset = %v, want empty", s.List())
	}
}

func TestListIsSortedByID(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "zzz", Kind: KindPort}, {ID: "aaa", Kind: KindPort}}))
	got := s.List()
	if len(got) != 2 || got[0].ID != "aaa" || got[1].ID != "zzz" {
		t.Errorf("List() = %+v, want sorted by ID", got)
	}
}

func TestSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggestions.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	must(t, s1.Sync([]Candidate{{ID: "c1", Kind: KindDevice, Name: "camera", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}}))
	must(t, s1.Accept("c1", "entry-1"))

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	got, ok := s2.Get("c1")
	if !ok {
		t.Fatal("candidate not found after restart")
	}
	if got.Status != StatusOn || got.EntryID != "entry-1" || got.Name != "camera" {
		t.Errorf("candidate not intact after restart: %+v", got)
	}
}

func TestEmptyPathIsInMemoryOnly(t *testing.T) {
	s := mustOpen(t)
	must(t, s.Sync([]Candidate{{ID: "c1", Kind: KindPort}}))
	if _, ok := s.Get("c1"); !ok {
		t.Error("in-memory-only store did not retain a synced candidate within the same process")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
