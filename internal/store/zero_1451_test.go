// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// #1451: Restamp rewrites display names across the ring, including after it wraps.
func TestRestampVisitsEveryHeldEventOnceAcrossAWrap(t *testing.T) {
	s := New(3, time.Hour)
	now := time.Now()
	for i := 0; i < 5; i++ { // 5 into a ring of 3: the two oldest are gone
		e := mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept)
		e.SrcPort = 1000 + i
		s.Insert(e)
	}
	visited := 0
	s.Restamp(func(e *Event) {
		visited++
		e.SrcHostName = "host-" + e.DeviceID
		e.RuleName = "rewritten"
	})
	if visited != 3 {
		t.Fatalf("rewrite ran %d times, want 3 (the events still held)", visited)
	}
	events, _, _ := s.Since(0, 10)
	if len(events) != 3 {
		t.Fatalf("Since returned %d events", len(events))
	}
	for i, e := range events {
		if e.SrcHostName != "host-core" || e.RuleName != "rewritten" {
			t.Errorf("event %d not restamped: %+v", i, e)
		}
		if want := 1002 + i; e.SrcPort != want {
			t.Errorf("event %d SrcPort = %d, want %d (order or identity changed)", i, e.SrcPort, want)
		}
	}
	empty := New(3, time.Hour)
	empty.Restamp(func(*Event) { t.Error("rewrite ran on an empty store") })
}
