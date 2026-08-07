package audit

import (
	"math"
	"testing"
	"time"
)

// TestQueryAllocationIsBoundedRegardlessOfRequestedLimit pins the
// property CodeQL's uncontrolled-allocation-size rule asks about, since
// Query.Limit arrives straight off a ?limit= query string on
// GET /api/audit. The bound held before this test existed -- the rule
// fired on the *shape* of the code, not on a real overallocation -- so
// this exists to keep it true rather than to fix it.
func TestQueryAllocationIsBoundedRegardlessOfRequestedLimit(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 50; i++ {
		s.Record("actor", "action", "target", "")
	}

	for _, limit := range []int{math.MaxInt, math.MaxInt32, 1 << 40, maxLimit + 1, -1, 0} {
		res := s.Query(Query{Limit: limit, Until: time.Now().Add(time.Hour)})
		if c := cap(res.Entries); c > maxLimit {
			t.Errorf("Limit=%d allocated capacity %d, above the %d ceiling", limit, c, maxLimit)
		}
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, defaultLimit},
		{-1, defaultLimit},
		{math.MinInt, defaultLimit},
		{1, 1},
		{maxLimit, maxLimit},
		{maxLimit + 1, maxLimit},
		{math.MaxInt, maxLimit},
	}
	for _, tt := range tests {
		if got := clampLimit(tt.in); got != tt.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
