// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"path/filepath"
	"testing"
	"time"
)

// This file covers #640's store half: an exclusion is now a *sized*
// expectation -- "this much of this, from this host, is normal" -- that
// absorbs a later firing within ExpectationTolerance of the size it
// recorded and refuses one above it.
//
// Every test here drives the same entry point production does
// (AddEmission, the path internal/engine's FlagsSink calls) rather than
// reaching into s.excluded, so what is proven is the behaviour a firing
// actually meets.

func intPtr(v int) *int { return &v }

// raiseSized is the one raise helper these tests share: one firing of
// (t, target) at the given size, through the same AddEmission the
// engine's flags sink calls.
func raiseSized(s *Store, t Type, target string, size *int, now time.Time) {
	s.AddEmission(t, target, "a firing", nil, Evidence{}, "", false, size, now)
}

// mustFlag returns the active (not cleared) flag for (ty, target),
// failing if there is none.
//
// Active, not merely present, is the right test throughout this file:
// recording an expectation *clears* the flag it was recorded from
// (an expected verdict) rather than deleting it, so a cleared entry
// for the
// pair is still in List() afterwards. Only a firing that got past the
// expectation revives it -- see add()'s Cleared branch -- so "is there
// an active flag" is exactly the question "did this firing raise".
func mustFlag(tb testing.TB, s *Store, ty Type, target string) Flag {
	tb.Helper()
	f, ok := activeFlag(s, ty, target)
	if !ok {
		tb.Fatalf("expected an active flag for (%s, %s), got %+v", ty, target, s.List())
	}
	return f
}

func activeFlag(s *Store, ty Type, target string) (Flag, bool) {
	for _, f := range s.List() {
		if f.Type == ty && f.Target == target && !f.Cleared {
			return f, true
		}
	}
	return Flag{}, false
}

func hasActiveFlag(s *Store, ty Type, target string) bool {
	_, ok := activeFlag(s, ty, target)
	return ok
}

// TestExpectationAbsorbsFiringWithinTolerance is the core of #640: a
// firing no bigger than 1.5x what the operator judged normal never
// reaches the inbox, and the expectation counts what it swallowed so the
// ledger can show the entry earning its place.
//
// 30 recorded, so the ceiling is 45. Both a firing at exactly the
// recorded size and one at exactly the ceiling must be absorbed --
// tolerance is inclusive, or the boundary itself would re-raise and the
// number shown on the ledger would not be the number enforced.
func TestExpectationAbsorbsFiringWithinTolerance(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(30), now)
	id := flagID(TypePortScan, "203.0.113.9")
	if _, ok := s.SetVerdict(id, VerdictExpected, "operator", now); !ok {
		t.Fatal("expected an expected verdict on the raised flag to succeed")
	}

	ex, ok := s.Expectation(TypePortScan, "203.0.113.9")
	if !ok {
		t.Fatal("expected the expected verdict to record an expectation")
	}
	if ex.Size == nil || *ex.Size != 30 {
		t.Fatalf("expected the expectation to record the flag's own size 30, got %v", ex.Size)
	}
	if ceiling, ok := ex.Ceiling(); !ok || ceiling != 45 {
		t.Errorf("expected a ceiling of 45 (30 x %v), got %d ok=%v", ExpectationTolerance, ceiling, ok)
	}

	for _, size := range []int{30, 44, 45} {
		raiseSized(s, TypePortScan, "203.0.113.9", intPtr(size), now.Add(time.Minute))
		if hasActiveFlag(s, TypePortScan, "203.0.113.9") {
			t.Fatalf("a firing of size %d is within 1.5x the expected 30 and must not raise a flag, got %+v", size, s.List())
		}
	}

	ex, _ = s.Expectation(TypePortScan, "203.0.113.9")
	if ex.Absorbed != 3 {
		t.Errorf("expected all 3 suppressed firings to be counted, got Absorbed = %d", ex.Absorbed)
	}
	if ex.Size == nil || *ex.Size != 30 {
		t.Errorf("absorbing a firing must not move the recorded size: got %v, want 30", ex.Size)
	}
}

// TestExpectationRaisesAboveToleranceCarryingBothSizes is the other
// half: past the ceiling the flag comes back, and it carries both
// numbers so a card can say "expected up to 30, saw 120" rather than
// re-reporting a bare count the operator already judged normal.
func TestExpectationRaisesAboveToleranceCarryingBothSizes(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(30), now)
	if _, ok := s.SetVerdict(flagID(TypePortScan, "203.0.113.9"), VerdictExpected, "operator", now); !ok {
		t.Fatal("expected an expected verdict on the raised flag to succeed")
	}

	// 46 is one past the ceiling of 45 -- the smallest firing that must
	// come back, so the test pins the boundary rather than a value so
	// large it would pass under a much looser tolerance too.
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(46), now.Add(time.Minute))
	f := mustFlag(t, s, TypePortScan, "203.0.113.9")
	if f.Cleared {
		t.Error("a firing above tolerance must raise an active flag, not a cleared one")
	}
	if f.Size == nil || *f.Size != 46 {
		t.Errorf("expected the re-raised flag to carry the observed size 46, got %v", f.Size)
	}
	if f.ExpectedSize == nil || *f.ExpectedSize != 30 {
		t.Errorf("expected the re-raised flag to carry the recorded size 30, got %v", f.ExpectedSize)
	}

	// A further firing that has grown again reports its own current
	// numbers, not the ones the episode opened with.
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(120), now.Add(2*time.Minute))
	f = mustFlag(t, s, TypePortScan, "203.0.113.9")
	if f.Size == nil || *f.Size != 120 {
		t.Errorf("expected a re-fire to refresh the observed size to 120, got %v", f.Size)
	}
	if f.ExpectedSize == nil || *f.ExpectedSize != 30 {
		t.Errorf("expected the recorded size to stay 30 on a re-fire, got %v", f.ExpectedSize)
	}

	// The expectation itself is untouched by traffic breaking it:
	// raising the recorded size is an operator's judgement, not
	// something the store does to itself.
	ex, _ := s.Expectation(TypePortScan, "203.0.113.9")
	if ex.Size == nil || *ex.Size != 30 {
		t.Errorf("a firing above tolerance must not move the recorded size: got %v, want 30", ex.Size)
	}
	if ex.Absorbed != 0 {
		t.Errorf("a firing above tolerance is not absorbed and must not be counted, got Absorbed = %d", ex.Absorbed)
	}
}

// TestExpectedAgainRaisesTheRecordedSize proves the operator's route out
// of a returning flag: saying Expected on the firing that broke the
// ceiling widens the expectation to that firing's size, and what was
// just re-raised is absorbed from then on. Absorbed and Since survive --
// it is the same expectation grown, not a fresh one.
func TestExpectedAgainRaisesTheRecordedSize(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id := flagID(TypePortScan, "203.0.113.9")

	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(30), now)
	s.SetVerdict(id, VerdictExpected, "operator", now)
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(40), now.Add(time.Minute)) // absorbed
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(120), now.Add(2*time.Minute))
	mustFlag(t, s, TypePortScan, "203.0.113.9") // it came back

	if _, ok := s.SetVerdict(id, VerdictExpected, "operator", now.Add(3*time.Minute)); !ok {
		t.Fatal("expected a second expected verdict to succeed")
	}
	ex, _ := s.Expectation(TypePortScan, "203.0.113.9")
	if ex.Size == nil || *ex.Size != 120 {
		t.Fatalf("expected Expected-again to raise the recorded size to 120, got %v", ex.Size)
	}
	if ex.Absorbed != 1 {
		t.Errorf("raising the size must keep the entry's absorbed history, got %d want 1", ex.Absorbed)
	}
	if !ex.Since.Equal(now) {
		t.Errorf("raising the size must keep the original since-when %v, got %v", now, ex.Since)
	}

	// 150 is within 1.5x of 120 and must now be absorbed, where it would
	// have re-raised against the old size of 30.
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(150), now.Add(4*time.Minute))
	if hasActiveFlag(s, TypePortScan, "203.0.113.9") {
		t.Errorf("a firing of 150 is within 1.5x the newly-expected 120 and must be absorbed, got %+v", s.List())
	}
}

// TestExpectedAgainNeverLowersOrNarrowsAnExpectation pins the two
// restrictions on raising: a quieter firing must not shrink an
// expectation the operator widened on purpose, and a size-less
// expectation must not silently acquire a ceiling -- that would turn
// "ignore this outright" into a re-raise the operator never asked for.
func TestExpectedAgainNeverLowersOrNarrowsAnExpectation(t *testing.T) {
	now := time.Now()

	t.Run("a smaller firing does not lower the recorded size", func(t *testing.T) {
		s, err := Open("")
		if err != nil {
			t.Fatal(err)
		}
		id := flagID(TypePortScan, "198.51.100.7")
		raiseSized(s, TypePortScan, "198.51.100.7", intPtr(100), now)
		s.SetVerdict(id, VerdictExpected, "operator", now)
		// A firing of 5 is absorbed, so it never reaches a flag; drive
		// the lowering attempt through the flag that is still there.
		raiseSized(s, TypePortScan, "198.51.100.7", intPtr(5), now.Add(time.Minute))
		s.SetVerdict(id, VerdictExpected, "operator", now.Add(time.Minute))

		ex, _ := s.Expectation(TypePortScan, "198.51.100.7")
		if ex.Size == nil || *ex.Size != 100 {
			t.Errorf("expected the recorded size to stay at 100, got %v", ex.Size)
		}
	})

	t.Run("a size-less expectation stays size-less", func(t *testing.T) {
		s, err := Open("")
		if err != nil {
			t.Fatal(err)
		}
		// A detector that declares no size: the firing carries nil.
		raiseSized(s, TypeDeviceSilence, "router-1", nil, now)
		id := flagID(TypeDeviceSilence, "router-1")
		s.SetVerdict(id, VerdictExpected, "operator", now)

		ex, _ := s.Expectation(TypeDeviceSilence, "router-1")
		if ex.Size != nil {
			t.Fatalf("a flag with no size must record a size-less expectation, got %v", ex.Size)
		}
		if _, ok := ex.Ceiling(); ok {
			t.Error("a size-less expectation has no ceiling")
		}
	})
}

// TestSizelessExpectationIgnoresOutright is the compatibility guarantee
// #640 rests on: an exclusion recorded before sizes existed, or one for
// a detector that declares no size, keeps its old blunt meaning --
// ignore this host on this detector, whatever it does next. Nothing
// re-raises, however large the firing.
func TestSizelessExpectationIgnoresOutright(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// Exclude is the size-less entry point -- what the pre-#640 store
	// wrote, and what an admin exclusion still writes.
	s.Exclude(TypePortScan, "203.0.113.9")

	for _, size := range []*int{nil, intPtr(1), intPtr(10_000)} {
		raiseSized(s, TypePortScan, "203.0.113.9", size, now)
		if hasActiveFlag(s, TypePortScan, "203.0.113.9") {
			t.Fatalf("a size-less exclusion must absorb a firing of size %v outright, got %+v", size, s.List())
		}
	}

	ex, ok := s.Expectation(TypePortScan, "203.0.113.9")
	if !ok {
		t.Fatal("expected the exclusion to be listed as an expectation")
	}
	if ex.Size != nil {
		t.Errorf("Exclude must record no size, got %v", ex.Size)
	}
	if ex.Absorbed != 3 {
		t.Errorf("expected a size-less expectation to still count what it absorbed, got %d", ex.Absorbed)
	}
}

// TestSizedExpectationPersistenceRoundTrip proves the whole expectation
// -- size, absorbed count and since-when -- survives a restart, and that
// the reopened store still enforces the same ceiling rather than merely
// reporting the fields. A "permanent" expectation that forgets its size
// on restart would silently become an ignore-outright, which is exactly
// the suppression #640 replaced.
func TestSizedExpectationPersistenceRoundTrip(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	since := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	closeForTest(t, s1)
	raiseSized(s1, TypePortScan, "203.0.113.9", intPtr(30), since)
	s1.SetVerdict(flagID(TypePortScan, "203.0.113.9"), VerdictExpected, "operator", since)
	raiseSized(s1, TypePortScan, "203.0.113.9", intPtr(40), since.Add(time.Minute)) // absorbed
	// A size-less expectation alongside it, so the round trip proves the
	// two shapes coexist in one document.
	s1.Exclude(TypeCriticalPort, "198.51.100.4")
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	// #836: s2 is mutated below (raiseSized, to prove the reloaded
	// ceiling is still enforced), so its own write-behind persister must
	// also be stopped before the test returns -- see closeForTest.
	closeForTest(t, s2)

	ex, ok := s2.Expectation(TypePortScan, "203.0.113.9")
	if !ok {
		t.Fatal("expected the sized expectation to survive reopening")
	}
	if ex.Size == nil || *ex.Size != 30 {
		t.Errorf("recorded size did not survive: got %v, want 30", ex.Size)
	}
	if ex.Absorbed != 1 {
		t.Errorf("absorbed count did not survive: got %d, want 1", ex.Absorbed)
	}
	if !ex.Since.Equal(since) {
		t.Errorf("since-when did not survive: got %v, want %v", ex.Since, since)
	}

	sizeless, ok := s2.Expectation(TypeCriticalPort, "198.51.100.4")
	if !ok {
		t.Fatal("expected the size-less exclusion to survive reopening")
	}
	if sizeless.Size != nil {
		t.Errorf("a size-less exclusion must read back with no size, got %v", sizeless.Size)
	}

	// The reopened store must still enforce the ceiling it read back,
	// not merely report it.
	raiseSized(s2, TypePortScan, "203.0.113.9", intPtr(45), time.Now())
	if hasActiveFlag(s2, TypePortScan, "203.0.113.9") {
		t.Errorf("a firing at the ceiling must still be absorbed after a reload, got %+v", s2.List())
	}
	raiseSized(s2, TypePortScan, "203.0.113.9", intPtr(46), time.Now())
	f := mustFlag(t, s2, TypePortScan, "203.0.113.9")
	if f.ExpectedSize == nil || *f.ExpectedSize != 30 {
		t.Errorf("a firing above the reloaded ceiling must carry the recorded size 30, got %v", f.ExpectedSize)
	}
}

// TestAddWithoutSizeLeavesFlagSizeUnset pins that the Add* entry points
// with no size to offer produce a size-less flag rather than a flag
// claiming size zero -- the same "nil means not scored" distinction
// Confidence draws, and what stops an expected verdict on such a flag from
// recording a nonsense expectation of "up to 0".
func TestAddWithoutSizeLeavesFlagSizeUnset(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "no size available", now)

	f := mustFlag(t, s, TypePortScan, "203.0.113.9")
	if f.Size != nil {
		t.Errorf("expected a sizeless raise to leave Flag.Size nil, got %v", f.Size)
	}
	if f.ExpectedSize != nil {
		t.Errorf("expected no ExpectedSize on a flag with no expectation behind it, got %v", f.ExpectedSize)
	}
}

// TestExpectationReturnsACopy proves a caller cannot reach back through
// the returned Size pointer and mutate the store's own expectation.
func TestExpectationReturnsACopy(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	raiseSized(s, TypePortScan, "203.0.113.9", intPtr(30), now)
	s.SetVerdict(flagID(TypePortScan, "203.0.113.9"), VerdictExpected, "operator", now)

	ex, _ := s.Expectation(TypePortScan, "203.0.113.9")
	*ex.Size = 9999

	again, _ := s.Expectation(TypePortScan, "203.0.113.9")
	if again.Size == nil || *again.Size != 30 {
		t.Errorf("mutating a returned expectation changed store state: got %v, want 30", again.Size)
	}
	for _, listed := range s.ListExclusions() {
		if listed.Size != nil && *listed.Size != 30 {
			t.Errorf("ListExclusions leaked a mutable size pointer: got %v, want 30", *listed.Size)
		}
	}
}
