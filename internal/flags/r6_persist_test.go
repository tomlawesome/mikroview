// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// withFastSaveTimeout shrinks persist.SaveTimeout for the duration of a
// test, restoring it on cleanup -- tryPersistLocked's Flush escape hatch
// (see that method's own doc comment) waits out this bound before giving
// up on a stuck-or-failing backend, and the real 5s default would make
// every test below slow for no reason. persist.SaveTimeout is a var for
// exactly this ("so a test can shrink it") per its own doc comment.
func withFastSaveTimeout(t *testing.T) {
	t.Helper()
	orig := persist.SaveTimeout
	persist.SaveTimeout = 30 * time.Millisecond
	t.Cleanup(func() { persist.SaveTimeout = orig })
}

// switchableFailBackend succeeds every Save until fail() is called, so a
// test can run whatever ordinary setup it needs first -- which may
// itself trigger any number of background write-behind saves, timed by
// a goroutine this test does not control -- before deterministically
// putting the backend into the failing state the assertion under test
// needs. Counting saves instead (as auth's saveBudgetBackend does
// against a synchronous, one-save-per-call backend) would be racy here:
// persist.WriteBehind's writer goroutine can coalesce or additionally
// attempt a save on its own timing, so "the Nth save fails" is not a
// deterministic way to target one specific call against this backend.
type switchableFailBackend struct {
	mu      sync.Mutex
	version int64
	failing bool
}

func (b *switchableFailBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *switchableFailBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing {
		return 0, errors.New("backend unavailable")
	}
	b.version++
	return b.version, nil
}

func (b *switchableFailBackend) Close() error     { return nil }
func (b *switchableFailBackend) Describe() string { return "switchable test backend" }

func (b *switchableFailBackend) fail() {
	b.mu.Lock()
	b.failing = true
	b.mu.Unlock()
}

// This file is the v0.6.0 audit's R6 fix for this store: SetVerdict,
// UndoVerdict, SetNote, ClearAll, Exclude, RemoveExclusionByID,
// RecordPermitted and WithdrawPermitted all change something an operator
// explicitly did, so a failed save must not be reported as kept, and the
// in-memory state each would have changed must be put back exactly as it
// was -- see each method's own restore-on-error comment. Every test here
// proves both halves: a non-nil error, and the store reading back
// unchanged afterward. Before this fix, each of these methods called the
// swallow-and-log persistLocked and reported success regardless of
// whether the write actually landed.

// TestSetVerdictFailedSaveLeavesFlagUnchanged covers the ordinary
// (non-expected) verdict path.
func TestSetVerdictFailedSaveLeavesFlagUnchanged(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now)
	id := flagID(TypePortScan, "203.0.113.9")
	before, ok := s.Get(id)
	if !ok {
		t.Fatal("setup: expected the flag to exist")
	}

	b.fail()
	_, ok, err = s.SetVerdict(id, VerdictChecked, "operator", "a note", now.Add(time.Minute))
	if err == nil {
		t.Fatal("expected SetVerdict to report the save failure")
	}
	if !ok {
		t.Error("expected ok=true (the flag was found) alongside the error")
	}

	after, stillOk := s.Get(id)
	if !stillOk {
		t.Fatal("flag disappeared after a failed save")
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("SetVerdict changed in-memory state despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestSetVerdictExpectedFailedSaveLeavesNoExpectation covers the
// VerdictExpected branch, which also writes to s.excluded --
// recordExpectationLocked must be rolled back too, not just the flag.
func TestSetVerdictExpectedFailedSaveLeavesNoExpectation(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	before, _ := s.Get(id)

	b.fail()
	if _, _, err := s.SetVerdict(id, VerdictExpected, "operator", "", now); err == nil {
		t.Fatal("expected SetVerdict to report the save failure")
	}

	if _, ok := s.Expectation(TypeInternalRecon, "192.168.1.50"); ok {
		t.Error("expected no expectation to have been recorded after a failed save")
	}
	after, _ := s.Get(id)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("SetVerdict changed the flag despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestUndoVerdictFailedSaveLeavesVerdictInPlace proves an undo that
// cannot be saved leaves the verdict it was undoing standing.
func TestUndoVerdictFailedSaveLeavesVerdictInPlace(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now)
	id := flagID(TypePortScan, "203.0.113.9")
	if _, ok, err := s.SetVerdict(id, VerdictChecked, "operator", "", now); !ok || err != nil {
		t.Fatalf("setup: SetVerdict failed: ok=%v err=%v", ok, err)
	}
	before, _ := s.Get(id)

	b.fail()
	if _, ok, err := s.UndoVerdict(id); err == nil {
		t.Fatal("expected UndoVerdict to report the save failure")
	} else if !ok {
		t.Error("expected ok=true (the flag was found) alongside the error")
	}

	after, _ := s.Get(id)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("UndoVerdict changed in-memory state despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestSetNoteFailedSaveLeavesOldNote proves a note edit that cannot be
// saved leaves the previous note standing rather than reporting the new
// one kept.
func TestSetNoteFailedSaveLeavesOldNote(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now)
	id := flagID(TypePortScan, "203.0.113.9")
	if _, ok, err := s.SetVerdict(id, VerdictChecked, "operator", "first note", now); !ok || err != nil {
		t.Fatalf("setup: SetVerdict failed: ok=%v err=%v", ok, err)
	}

	b.fail()
	_, known, judged, err := s.SetNote(id, "second note")
	if err == nil {
		t.Fatal("expected SetNote to report the save failure")
	}
	if !known || !judged {
		t.Errorf("expected known=true, judged=true alongside the error, got known=%v judged=%v", known, judged)
	}

	after, _ := s.Get(id)
	if after.Note != "first note" {
		t.Errorf("Note = %q after a failed save, want the original %q", after.Note, "first note")
	}
}

// TestClearAllFailedSaveReopensEveryTouchedFlag proves a bulk clear that
// cannot be saved reports zero cleared and leaves every flag it touched
// active.
func TestClearAllFailedSaveReopensEveryTouchedFlag(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "d1", now)
	s.Add(TypeActivitySpike, "203.0.113.10", "d2", now)
	beforeCleared := s.clearedCount

	b.fail()
	n, err := s.ClearAll(now.Add(time.Minute))
	if err == nil {
		t.Fatal("expected ClearAll to report the save failure")
	}
	if n != 0 {
		t.Errorf("ClearAll returned n=%d on a failed save, want 0", n)
	}

	if s.clearedCount != beforeCleared {
		t.Errorf("clearedCount = %d after a failed save, want unchanged %d", s.clearedCount, beforeCleared)
	}
	for _, f := range s.List() {
		if f.Cleared {
			t.Errorf("flag %s is Cleared after ClearAll's save failed -- it must have been put back to active", f.ID)
		}
	}
}

// TestExcludeFailedSaveLeavesNoExclusionAndFlagActive proves a failed
// Exclude neither creates the permanent exclusion nor leaves the flag it
// force-cleared as a side effect cleared.
func TestExcludeFailedSaveLeavesNoExclusionAndFlagActive(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "d1", now)
	id := flagID(TypePortScan, "203.0.113.9")

	b.fail()
	if err := s.Exclude(TypePortScan, "203.0.113.9"); err == nil {
		t.Fatal("expected Exclude to report the save failure")
	}

	if s.Excluded(TypePortScan, "203.0.113.9") {
		t.Error("expected no exclusion to exist after a failed save")
	}
	f, ok := s.Get(id)
	if !ok {
		t.Fatal("flag disappeared after a failed save")
	}
	if f.Cleared {
		t.Error("Exclude's own side-effect clear must have been undone alongside the failed save")
	}
}

// TestRemoveExclusionByIDFailedSaveLeavesExclusionInPlace proves a
// failed removal puts the exclusion back rather than reporting it gone.
func TestRemoveExclusionByIDFailedSaveLeavesExclusionInPlace(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	if err := s.Exclude(TypePortScan, "203.0.113.9"); err != nil {
		t.Fatalf("setup: Exclude failed: %v", err)
	}
	before, _ := s.Expectation(TypePortScan, "203.0.113.9")

	b.fail()
	ok, err := s.RemoveExclusionByID(flagID(TypePortScan, "203.0.113.9"))
	if err == nil {
		t.Fatal("expected RemoveExclusionByID to report the save failure")
	}
	if ok {
		t.Error("expected ok=false alongside the error")
	}

	after, stillExcluded := s.Expectation(TypePortScan, "203.0.113.9")
	if !stillExcluded {
		t.Fatal("exclusion is gone after a failed save -- it must have been put back")
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("exclusion changed despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestRecordPermittedFailedSaveLeavesExpectationUnchanged proves a
// failed RecordPermitted does not leave the new record standing.
func TestRecordPermittedFailedSaveLeavesExpectationUnchanged(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	if _, _, err := s.SetVerdict(id, VerdictExpected, "operator", "", now); err != nil {
		t.Fatalf("setup: SetVerdict failed: %v", err)
	}
	before, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")

	b.fail()
	rec := PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}}, Verdict: VerdictExpected, At: now}
	ok, err := s.RecordPermitted(id, rec)
	if err == nil {
		t.Fatal("expected RecordPermitted to report the save failure")
	}
	if ok {
		t.Error("expected ok=false alongside the error")
	}

	after, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("expectation changed despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

// TestWithdrawPermittedFailedSaveLeavesRecordInPlace proves a failed
// withdrawal does not remove the record it was about to hand to the
// caller for reversing on the watchlist.
func TestWithdrawPermittedFailedSaveLeavesRecordInPlace(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	if _, _, err := s.SetVerdict(id, VerdictExpected, "operator", "", now); err != nil {
		t.Fatalf("setup: SetVerdict failed: %v", err)
	}
	rec := PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}}, Verdict: VerdictExpected, At: now}
	if ok, err := s.RecordPermitted(id, rec); !ok || err != nil {
		t.Fatalf("setup: RecordPermitted failed: ok=%v err=%v", ok, err)
	}
	before, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")

	b.fail()
	_, ok, err := s.WithdrawPermitted(id)
	if err == nil {
		t.Fatal("expected WithdrawPermitted to report the save failure")
	}
	if ok {
		t.Error("expected ok=false alongside the error")
	}

	after, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("expectation changed despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}
