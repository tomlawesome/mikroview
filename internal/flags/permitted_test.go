// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"path/filepath"
	"testing"
	"time"
)

// This file covers #641's store half: the record of what an expected
// verdict permitted on the watchlist. The write itself belongs to
// internal/api (this package does not know the watchlist exists); what
// belongs here is being able to say what was added, by which verdict,
// and when -- for undo, and for the ledger #640 part C builds.

func TestRecordPermittedNeedsAnExpectationToHangOn(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")

	rec := PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}}, Verdict: VerdictExpected, At: now}
	if s.RecordPermitted(id, rec) {
		t.Error("recording a permission against a flag with no expectation must report false: a permission with no expectation beside it is half a judgement")
	}

	s.SetVerdict(id, VerdictExpected, "alice", now)
	if !s.RecordPermitted(id, rec) {
		t.Fatal("expected the record to attach once the expectation exists")
	}
	ex, ok := s.Expectation(TypeInternalRecon, "192.168.1.50")
	if !ok || len(ex.Permitted) != 1 {
		t.Fatalf("Expectation.Permitted = %+v, want the one record", ex.Permitted)
	}
	got := ex.Permitted[0]
	if got.EntryID != "entry-1" || got.Verdict != VerdictExpected || !got.At.Equal(now) {
		t.Errorf("record = %+v, want what was written", got)
	}
	if len(got.Dests) != 1 || got.Dests[0] != (HostPort{Host: "192.168.1.10", Port: 445}) {
		t.Errorf("record Dests = %+v, want the pair that was permitted", got.Dests)
	}
}

// TestWithdrawPermittedTakesTheLastVerdictsRecord pins the "one record
// per verdict" contract: undoing the second expected verdict takes back
// what the second one added, not everything ever permitted for the pair.
func TestWithdrawPermittedTakesTheLastVerdictsRecord(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	s.SetVerdict(id, VerdictExpected, "alice", now)

	first := PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}}, Verdict: VerdictExpected, At: now}
	second := PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.12", Port: 139}}, Verdict: VerdictExpected, At: now.Add(time.Hour)}
	s.RecordPermitted(id, first)
	s.RecordPermitted(id, second)

	got, ok := s.WithdrawPermitted(id)
	if !ok {
		t.Fatal("expected a record to withdraw")
	}
	if len(got.Dests) != 1 || got.Dests[0].Port != 139 {
		t.Errorf("withdrew %+v, want the most recent verdict's record", got)
	}
	ex, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")
	if len(ex.Permitted) != 1 || ex.Permitted[0].Dests[0].Port != 445 {
		t.Errorf("Permitted = %+v, want the earlier verdict's record left standing", ex.Permitted)
	}

	if _, ok := s.WithdrawPermitted(id); !ok {
		t.Error("expected the earlier record to be withdrawable too")
	}
	if _, ok := s.WithdrawPermitted(id); ok {
		t.Error("withdrawing from an expectation with no records must report false, not an empty record")
	}
}

// TestPermittedRecordsAreDetachedFromStoreState: a listed expectation is
// a copy, not a handle. Undo reads these records to decide what to take
// off the watchlist, so a caller mutating one it was handed would be
// editing the store's own answer to that question.
func TestPermittedRecordsAreDetachedFromStoreState(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	raiseSized(s, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	s.SetVerdict(id, VerdictExpected, "alice", now)
	s.RecordPermitted(id, PermittedRecord{EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}}, Verdict: VerdictExpected, At: now})

	listed := s.ListExclusions()
	if len(listed) != 1 || len(listed[0].Permitted) != 1 {
		t.Fatalf("ListExclusions = %+v, want the one expectation and its record", listed)
	}
	listed[0].Permitted[0].Dests[0].Port = 9999

	ex, _ := s.Expectation(TypeInternalRecon, "192.168.1.50")
	if ex.Permitted[0].Dests[0].Port != 445 {
		t.Errorf("store state changed through a listed copy: %+v", ex.Permitted[0].Dests)
	}
}

// TestPermittedRecordSurvivesReload proves the round trip rather than
// asserting it from the struct tags -- the same reason #640's own
// persistence tests exist. Additive and omitted when empty, so an
// expectation recorded before #641 reads back unchanged.
func TestPermittedRecordSurvivesReload(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 0
	defer func() { persistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "flags.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	raiseSized(s1, TypeInternalRecon, "192.168.1.50", intPtr(3), now)
	id := flagID(TypeInternalRecon, "192.168.1.50")
	s1.SetVerdict(id, VerdictExpected, "alice", now)
	s1.RecordPermitted(id, PermittedRecord{
		EntryID: "entry-1", Dests: []HostPort{{Host: "192.168.1.10", Port: 445}},
		CreatedEntry: true, Verdict: VerdictExpected, At: now,
	})
	flushForTest(t, s1)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	ex, ok := s2.Expectation(TypeInternalRecon, "192.168.1.50")
	if !ok || len(ex.Permitted) != 1 {
		t.Fatalf("Permitted after reload = %+v, want the one record", ex.Permitted)
	}
	rec := ex.Permitted[0]
	if rec.EntryID != "entry-1" || !rec.CreatedEntry || rec.Verdict != VerdictExpected || !rec.At.Equal(now) {
		t.Errorf("record after reload = %+v, want what was written", rec)
	}
	if len(rec.Dests) != 1 || rec.Dests[0] != (HostPort{Host: "192.168.1.10", Port: 445}) {
		t.Errorf("Dests after reload = %+v", rec.Dests)
	}
}

// TestGetReturnsACopy: internal/api reads a flag before changing its
// verdict, to know what the change has to reverse. That read must not be
// a handle onto store state.
func TestGetReturnsACopy(t *testing.T) {
	s, _ := Open("")
	now := time.Now()
	s.Add(TypePortScan, "203.0.113.9", "20 ports in 60s", now)
	id := s.List()[0].ID

	f, ok := s.Get(id)
	if !ok {
		t.Fatal("expected Get to find the flag it just added")
	}
	f.Detail = "rewritten"
	again, _ := s.Get(id)
	if again.Detail != "20 ports in 60s" {
		t.Errorf("store state changed through a Get copy: %q", again.Detail)
	}
	if _, ok := s.Get("no-such-flag"); ok {
		t.Error("Get must report false for an unknown id")
	}
}
