// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"testing"
	"time"
)

// #1451: paths no other test reached.

func TestVerdictValidIsExactlyTheFourVerdicts(t *testing.T) {
	for _, v := range []Verdict{VerdictExpected, VerdictChecked, VerdictInvestigate, VerdictResolved} {
		if !v.Valid() {
			t.Errorf("%q not valid", v)
		}
	}
	for _, v := range []Verdict{"", "EXPECTED", "ignored", "cleared"} {
		if v.Valid() {
			t.Errorf("%q reported valid", v)
		}
	}
}

// Reset (the test-only POST /api/test/reset) empties flags and exclusions
// and leaves the store usable.
func TestResetEmptiesFlagsAndExclusions(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.Add(TypePortScan, "192.0.2.1", "scan", now)
	s.Exclude(TypeCriticalPort, "192.0.2.2")
	if len(s.List()) != 1 || !s.Excluded(TypeCriticalPort, "192.0.2.2") {
		t.Fatalf("setup: %d flags, excluded=%v", len(s.List()), s.Excluded(TypeCriticalPort, "192.0.2.2"))
	}
	s.Reset()
	if n := len(s.List()); n != 0 {
		t.Errorf("%d flags after Reset", n)
	}
	if s.Excluded(TypeCriticalPort, "192.0.2.2") || len(s.ListExclusions()) != 0 {
		t.Error("an exclusion survived Reset")
	}
	if !s.Add(TypePortScan, "192.0.2.1", "scan", now) {
		t.Error("the previously excluded/raised pair cannot be raised again after Reset")
	}
}
