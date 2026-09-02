// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"path/filepath"
	"testing"
	"time"
)

// This file covers #640 part B's store half: the two verdicts that
// suppress nothing but are remembered (checked, resolved), and undo's
// reversal of the expectation an expected verdict records.
//
// Everything here drives the same entry points production does --
// SetVerdict, UndoVerdict and Add/AddEmission -- rather than reaching
// into store internals.

// TestReviveRemembersACheckedVerdict is what makes the returning card
// able to say "you checked this on 2 Sept and found it fine": checked
// clears and suppresses nothing, so the next firing revives the flag,
// and the only trace of the earlier judgement is PriorVerdict.
func TestReviveRemembersACheckedVerdict(t *testing.T) {
	s, _ := Open("")
	checkedAt := time.Now()

	s.Add(TypePortScan, "203.0.113.30", "20 ports in 60s", checkedAt)
	id := s.List()[0].ID
	if _, ok := s.SetVerdict(id, VerdictChecked, "alice", checkedAt); !ok {
		t.Fatal("setup: expected the checked verdict to land")
	}

	// Nothing was recorded to suppress it, so it comes straight back.
	s.Add(TypePortScan, "203.0.113.30", "20 ports in 60s", checkedAt.Add(time.Hour))
	f := mustFlag(t, s, TypePortScan, "203.0.113.30")

	if f.Verdict != "" {
		t.Errorf("the new episode must start unjudged, got Verdict = %q", f.Verdict)
	}
	if f.PriorVerdict != VerdictChecked {
		t.Errorf("PriorVerdict = %q, want %q", f.PriorVerdict, VerdictChecked)
	}
	if !f.PriorVerdictAt.Equal(checkedAt) {
		t.Errorf("PriorVerdictAt = %v, want the date it was checked, %v", f.PriorVerdictAt, checkedAt)
	}
}

// TestReviveRemembersAResolvedVerdict is the other half, and the one
// the issue's reasoning turns on: resolved is deliberately not a
// suppression, so a recurrence returns saying "resolved on 2 Sept --
// it's back" rather than staying quiet as an expectation would.
func TestReviveRemembersAResolvedVerdict(t *testing.T) {
	s, _ := Open("")
	resolvedAt := time.Now()

	s.Add(TypeCriticalPort, "198.51.100.30", "6 attempts on port 22", resolvedAt)
	id := s.List()[0].ID
	s.SetVerdict(id, VerdictResolved, "alice", resolvedAt)

	if s.Excluded(TypeCriticalPort, "198.51.100.30") {
		t.Fatal("a resolved verdict must not record an expectation -- it is not a suppression")
	}

	s.Add(TypeCriticalPort, "198.51.100.30", "6 attempts on port 22", resolvedAt.Add(24*time.Hour))
	f := mustFlag(t, s, TypeCriticalPort, "198.51.100.30")
	if f.PriorVerdict != VerdictResolved {
		t.Errorf("PriorVerdict = %q, want %q", f.PriorVerdict, VerdictResolved)
	}
	if !f.PriorVerdictAt.Equal(resolvedAt) {
		t.Errorf("PriorVerdictAt = %v, want %v", f.PriorVerdictAt, resolvedAt)
	}
}

// TestReviveForgetsAnInvestigateVerdict pins the other direction: only
// checked and resolved are remembered. An investigate verdict that was
// cleared some other way (ClearAll) leaves no memory, because the card
// copy is about what the last look *concluded*, and "still being looked
// at" concludes nothing.
func TestReviveForgetsAnInvestigateVerdict(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.31", "d", now)
	id := s.List()[0].ID
	s.SetVerdict(id, VerdictInvestigate, "alice", now)
	s.ClearAll(now.Add(time.Minute))

	s.Add(TypePortScan, "203.0.113.31", "d", now.Add(time.Hour))
	f := mustFlag(t, s, TypePortScan, "203.0.113.31")
	if f.PriorVerdict != "" {
		t.Errorf("PriorVerdict = %q, want empty for an investigate verdict", f.PriorVerdict)
	}
	if !f.PriorVerdictAt.IsZero() {
		t.Errorf("PriorVerdictAt = %v, want zero", f.PriorVerdictAt)
	}
}

// TestLaterVerdictReplacesTheRememberedOne: the memory is of the last
// judgement, not the last remembered one. A flag checked, revived and
// then called investigate must not still claim it was checked when it
// next comes back -- that would be the card telling the operator a
// conclusion that has since been superseded.
func TestLaterVerdictReplacesTheRememberedOne(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	s.Add(TypePortScan, "203.0.113.32", "d", now)
	id := s.List()[0].ID
	s.SetVerdict(id, VerdictChecked, "alice", now)
	s.Add(TypePortScan, "203.0.113.32", "d", now.Add(time.Hour)) // back, remembering "checked"
	s.SetVerdict(id, VerdictInvestigate, "alice", now.Add(2*time.Hour))
	s.ClearAll(now.Add(3 * time.Hour))
	s.Add(TypePortScan, "203.0.113.32", "d", now.Add(4*time.Hour))

	f := mustFlag(t, s, TypePortScan, "203.0.113.32")
	if f.PriorVerdict != "" {
		t.Errorf("PriorVerdict = %q, want empty -- the investigate call replaced the checked memory", f.PriorVerdict)
	}
}

// TestRememberedVerdictSurvivesReload proves the round trip in practice
// rather than by inspection of the struct tags -- both fields are
// additive JSON (omitempty/omitzero), the same shape SetVerdict's own
// persistence test covers for Verdict/VerdictBy/VerdictAt.
func TestRememberedVerdictSurvivesReload(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Now().UTC().Truncate(time.Millisecond)
	s1.Add(TypePortScan, "203.0.113.33", "d", checkedAt)
	id := s1.List()[0].ID
	s1.SetVerdict(id, VerdictChecked, "alice", checkedAt)
	s1.Add(TypePortScan, "203.0.113.33", "d", checkedAt.Add(time.Hour))
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 persisted flag after reopening, got %d: %+v", len(list), list)
	}
	if list[0].PriorVerdict != VerdictChecked {
		t.Errorf("PriorVerdict = %q after reload, want %q", list[0].PriorVerdict, VerdictChecked)
	}
	if !list[0].PriorVerdictAt.Equal(checkedAt) {
		t.Errorf("PriorVerdictAt = %v after reload, want %v", list[0].PriorVerdictAt, checkedAt)
	}
}

// TestUndoExpectedWithdrawsTheExpectationItRecorded is the case that
// would otherwise be the worst of both worlds: a flag visibly back in
// the inbox after an undo, and a store still silently absorbing every
// further firing of it.
func TestUndoExpectedWithdrawsTheExpectationItRecorded(t *testing.T) {
	s, _ := Open("")
	now := time.Now()

	raiseSized(s, TypePortScan, "203.0.113.34", intPtr(30), now)
	id := flagID(TypePortScan, "203.0.113.34")
	s.SetVerdict(id, VerdictExpected, "alice", now)
	if !s.Excluded(TypePortScan, "203.0.113.34") {
		t.Fatal("setup: expected the expected verdict to record an expectation")
	}

	f, ok := s.UndoVerdict(id)
	if !ok {
		t.Fatal("expected UndoVerdict to find the flag")
	}
	if f.Cleared {
		t.Error("undo should re-open the flag the expected verdict cleared")
	}
	if s.Excluded(TypePortScan, "203.0.113.34") {
		t.Error("undo must withdraw the expectation the verdict it undid recorded")
	}

	// And detection really is re-armed, not just the entry gone from a
	// list: a firing that would have been absorbed raises again.
	raiseSized(s, TypePortScan, "203.0.113.34", intPtr(31), now.Add(time.Minute))
	got := mustFlag(t, s, TypePortScan, "203.0.113.34")
	if got.Size == nil || *got.Size != 31 {
		t.Errorf("expected the re-armed pair to raise carrying size 31, got %v", got.Size)
	}
}

// TestUndoExpectedRestoresARaisedSize covers the other shape: saying
// Expected again on a flag that broke its ceiling raises the recorded
// size, so undoing that call must put the old size back rather than
// deleting an expectation the operator made earlier and never withdrew.
func TestUndoExpectedRestoresARaisedSize(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	id := flagID(TypePortScan, "203.0.113.35")

	raiseSized(s, TypePortScan, "203.0.113.35", intPtr(30), now)
	s.SetVerdict(id, VerdictExpected, "alice", now)
	raiseSized(s, TypePortScan, "203.0.113.35", intPtr(120), now.Add(time.Minute)) // back, above 45
	mustFlag(t, s, TypePortScan, "203.0.113.35")
	s.SetVerdict(id, VerdictExpected, "alice", now.Add(2*time.Minute)) // raises 30 -> 120

	if ex, _ := s.Expectation(TypePortScan, "203.0.113.35"); ex.Size == nil || *ex.Size != 120 {
		t.Fatalf("setup: expected the recorded size to be raised to 120, got %v", ex.Size)
	}

	if _, ok := s.UndoVerdict(id); !ok {
		t.Fatal("expected UndoVerdict to find the flag")
	}
	ex, ok := s.Expectation(TypePortScan, "203.0.113.35")
	if !ok {
		t.Fatal("undoing a raise must not delete the expectation the earlier verdict made")
	}
	if ex.Size == nil || *ex.Size != 30 {
		t.Errorf("expected the recorded size to go back to 30, got %v", ex.Size)
	}
}

// TestRejudgingAwayFromExpectedWithdrawsTheExpectation: an operator who
// changes their mind must not leave a suppression behind that nothing on
// screen still claims. Re-judging is the same retraction undo is, so it
// withdraws the expectation the same way.
func TestRejudgingAwayFromExpectedWithdrawsTheExpectation(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	id := flagID(TypePortScan, "203.0.113.36")

	raiseSized(s, TypePortScan, "203.0.113.36", intPtr(30), now)
	s.SetVerdict(id, VerdictExpected, "alice", now)
	s.SetVerdict(id, VerdictChecked, "bob", now.Add(time.Minute)) // changed their mind

	if s.Excluded(TypePortScan, "203.0.113.36") {
		t.Error("re-judging away from expected must withdraw the expectation that verdict recorded")
	}
	if f := s.List()[0]; !f.Cleared || f.Verdict != VerdictChecked {
		t.Errorf("the flag should still be cleared, now as checked, got %+v", f)
	}
}
