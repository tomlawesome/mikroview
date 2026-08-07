package store

import (
	"math"
	"testing"
	"time"
)

// See internal/audit/limit_test.go -- same property, same reasoning, on
// the endpoint that actually carries volume (GET /api/events).
func TestQueryAllocationIsBoundedRegardlessOfRequestedLimit(t *testing.T) {
	s := New(1000, time.Hour)
	for _, limit := range []int{math.MaxInt, math.MaxInt32, 1 << 40, maxLimit + 1, -1, 0} {
		res := s.Query(Query{Limit: limit})
		if c := cap(res.Events); c > maxLimit {
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
