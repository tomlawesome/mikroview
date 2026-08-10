// SPDX-License-Identifier: AGPL-3.0-only

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

// See internal/store/query.go's maxScannedPerQuery doc comment -- a
// selective filter matching nothing can otherwise force Query to walk
// the entire in-window buffer while holding s.mu.RLock(), which blocks
// Insert (the sole ingest writer) for the duration.
func TestQueryStopsScanningAtMaxScannedPerQuery(t *testing.T) {
	old := maxScannedPerQuery
	maxScannedPerQuery = 10
	t.Cleanup(func() { maxScannedPerQuery = old })

	s := New(1000, time.Hour)
	for i := 0; i < 100; i++ {
		s.Insert(Event{DeviceID: "core"})
	}

	res := s.Query(Query{Device: "does-not-exist"})
	if !res.HasMore {
		t.Error("expected HasMore=true once the scan cap is hit, even though nothing matched")
	}
	if len(res.Events) != 0 {
		t.Errorf("expected no matches, got %d", len(res.Events))
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

// BenchmarkQueryNoMatchAtCapacity is Query's worst case: a filter
// matching nothing, at the default ~204,700-event capacity, so nothing
// short-circuits the scan via Limit or the window. Pins
// maxScannedPerQuery's effect -- unbounded, this cost was measured at
// ~60ms; capped, it should stay close to what maxScannedPerQuery's own
// doc comment estimates.
func BenchmarkQueryNoMatchAtCapacity(b *testing.B) {
	s := New(200_000, time.Hour)
	for i := 0; i < 200_000; i++ {
		s.Insert(Event{DeviceID: "core", Action: ActionAccept})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Query(Query{Device: "does-not-exist"})
	}
}
