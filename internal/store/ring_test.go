// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"fmt"
	"testing"
	"time"
)

func mkEvent(t time.Time, device string, action Action) Event {
	return Event{
		Time:       t,
		ReceivedAt: t,
		DeviceID:   device,
		Action:     action,
		Protocol:   "TCP",
		SrcIP:      "192.168.1.50",
		SrcPort:    1234,
		DstIP:      "1.2.3.4",
		DstPort:    443,
		Chain:      "forward",
		RuleLabel:  "lan-wan",
		Raw:        "raw line",
	}
}

func TestInsertAssignsIncreasingIDs(t *testing.T) {
	s := New(10, time.Hour)
	now := time.Now()
	e1 := s.Insert(mkEvent(now, "core", ActionAccept))
	e2 := s.Insert(mkEvent(now, "core", ActionAccept))
	if e1.ID != 1 || e2.ID != 2 {
		t.Errorf("IDs = %d, %d; want 1, 2", e1.ID, e2.ID)
	}
}

func TestRingWraparoundEvictsOldest(t *testing.T) {
	s := New(3, time.Hour)
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept))
	}

	res := s.Query(Query{Limit: 10})
	if len(res.Events) != 3 {
		t.Fatalf("expected 3 events retained (capacity), got %d", len(res.Events))
	}
	// oldest surviving event should be #3 (IDs 1,2 evicted), chronological order
	if res.Events[0].ID != 3 || res.Events[2].ID != 5 {
		t.Errorf("unexpected surviving IDs: %d..%d", res.Events[0].ID, res.Events[len(res.Events)-1].ID)
	}
}

func TestQueryRespectsRetentionWindow(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	s.Insert(mkEvent(now.Add(-2*time.Hour), "core", ActionAccept)) // outside window
	s.Insert(mkEvent(now.Add(-10*time.Minute), "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionAccept))

	res := s.Query(Query{Limit: 10})
	if len(res.Events) != 2 {
		t.Fatalf("expected 2 events within window, got %d", len(res.Events))
	}
}

func TestQueryLimitAndHasMore(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept))
	}

	res := s.Query(Query{Limit: 3})
	if len(res.Events) != 3 || !res.HasMore {
		t.Errorf("expected 3 events and HasMore=true, got %d events, HasMore=%v", len(res.Events), res.HasMore)
	}
	// should be the 3 most recent, in chronological order
	if res.Events[0].ID != 8 || res.Events[2].ID != 10 {
		t.Errorf("unexpected IDs: %v", ids(res.Events))
	}
}

func TestQuerySinceIDCursor(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept))
	}

	res := s.Query(Query{SinceID: 3, Limit: 10})
	if len(res.Events) != 2 || res.Events[0].ID != 4 || res.Events[1].ID != 5 {
		t.Errorf("expected IDs [4,5], got %v", ids(res.Events))
	}
}

func TestQueryUntilBoundsTheUpperEdge(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept))
	}

	res := s.Query(Query{Until: now.Add(2 * time.Second), Limit: 10})
	if len(res.Events) != 3 || res.Events[0].ID != 1 || res.Events[2].ID != 3 {
		t.Errorf("expected IDs [1,2,3] (events at or before Until), got %v", ids(res.Events))
	}
}

func TestQuerySinceAndUntilBoundABeforeAfterWindow(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.Insert(mkEvent(now.Add(time.Duration(i)*time.Second), "core", ActionAccept))
	}

	// A "before/after a timestamp" lookback (issue #29): center on event #5
	// (now+4s), 2s either side.
	center := now.Add(4 * time.Second)
	res := s.Query(Query{Since: center.Add(-2 * time.Second), Until: center.Add(2 * time.Second), Limit: 10})
	if len(res.Events) != 5 || res.Events[0].ID != 3 || res.Events[4].ID != 7 {
		t.Errorf("expected IDs [3..7] centered on the timestamp, got %v", ids(res.Events))
	}
}

func TestQueryWindowStartReportsRetentionTruncation(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	s.Insert(mkEvent(now, "core", ActionAccept))

	// Ask for far more history than the 1h retention actually holds --
	// WindowStart in the response should report the *actual* applied
	// lower bound, not silently honor the requested one, so a caller can
	// tell "before" context was truncated by retention.
	requestedSince := now.Add(-24 * time.Hour)
	res := s.Query(Query{Since: requestedSince, Limit: 10})
	if !res.WindowStart.After(requestedSince) {
		t.Errorf("expected WindowStart (%v) to be clamped later than the requested Since (%v)", res.WindowStart, requestedSince)
	}
}

func TestQueryFilters(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()

	accept := mkEvent(now, "core", ActionAccept)
	accept.Protocol = "TCP"
	accept.SrcIP, accept.DstIP = "10.0.0.5", "8.8.8.8"
	accept.SrcPort, accept.DstPort = 51234, 443
	s.Insert(accept)

	drop := mkEvent(now, "branch", ActionDrop)
	drop.Protocol = "UDP"
	drop.SrcIP, drop.DstIP = "10.0.0.9", "1.1.1.1"
	drop.SrcPort, drop.DstPort = 5000, 53
	drop.RuleLabel = "invalid"
	s.Insert(drop)

	t.Run("by device", func(t *testing.T) {
		res := s.Query(Query{Device: "branch", Limit: 10})
		if len(res.Events) != 1 || res.Events[0].DeviceID != "branch" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by action", func(t *testing.T) {
		res := s.Query(Query{Action: ActionDrop, Limit: 10})
		if len(res.Events) != 1 || res.Events[0].Action != ActionDrop {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by protocol case-insensitive", func(t *testing.T) {
		res := s.Query(Query{Protocol: "tcp", Limit: 10})
		if len(res.Events) != 1 || res.Events[0].Protocol != "TCP" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by CIDR matching dst", func(t *testing.T) {
		res := s.Query(Query{IP: "8.8.8.0/24", Limit: 10})
		if len(res.Events) != 1 {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by exact IP matching src", func(t *testing.T) {
		res := s.Query(Query{IP: "10.0.0.9", Limit: 10})
		if len(res.Events) != 1 || res.Events[0].DeviceID != "branch" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by port matching either side", func(t *testing.T) {
		res := s.Query(Query{Port: 53, Limit: 10})
		if len(res.Events) != 1 || res.Events[0].DstPort != 53 {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by rule substring", func(t *testing.T) {
		res := s.Query(Query{Rule: "inval", Limit: 10})
		if len(res.Events) != 1 || res.Events[0].RuleLabel != "invalid" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("by rule regex", func(t *testing.T) {
		res := s.Query(Query{Rule: "^inval", RuleRegex: true, Limit: 10})
		if len(res.Events) != 1 || res.Events[0].RuleLabel != "invalid" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("rule regex is case-insensitive", func(t *testing.T) {
		res := s.Query(Query{Rule: "INVALID", RuleRegex: true, Limit: 10})
		if len(res.Events) != 1 || res.Events[0].RuleLabel != "invalid" {
			t.Errorf("got %v", ids(res.Events))
		}
	})

	t.Run("rule regex also matches against the raw line", func(t *testing.T) {
		res := s.Query(Query{Rule: `raw.line`, RuleRegex: true, Limit: 10})
		if len(res.Events) != 2 {
			t.Errorf("expected regex to match both events' raw line, got %v", ids(res.Events))
		}
	})

	t.Run("invalid regex pattern disables rule filtering rather than erroring", func(t *testing.T) {
		res := s.Query(Query{Rule: "(unterminated[", RuleRegex: true, Limit: 10})
		if len(res.Events) != 2 {
			t.Errorf("expected an invalid pattern to leave both events unfiltered, got %v", ids(res.Events))
		}
	})

	t.Run("by src scope internal", func(t *testing.T) {
		// both events have a private SrcIP (10.0.0.x) -- should match both
		res := s.Query(Query{SrcScope: ScopeInternal, Limit: 10})
		if len(res.Events) != 2 {
			t.Errorf("expected both events (private SrcIP) to match, got %v", ids(res.Events))
		}
	})

	t.Run("by src scope external", func(t *testing.T) {
		res := s.Query(Query{SrcScope: ScopeExternal, Limit: 10})
		if len(res.Events) != 0 {
			t.Errorf("expected no events (neither has a public SrcIP), got %v", ids(res.Events))
		}
	})

	t.Run("by dst scope external", func(t *testing.T) {
		// both events have a public DstIP (8.8.8.8, 1.1.1.1) -- should match both
		res := s.Query(Query{DstScope: ScopeExternal, Limit: 10})
		if len(res.Events) != 2 {
			t.Errorf("expected both events (public DstIP) to match, got %v", ids(res.Events))
		}
	})

	t.Run("src and dst scope combine", func(t *testing.T) {
		res := s.Query(Query{SrcScope: ScopeInternal, DstScope: ScopeExternal, Limit: 10})
		if len(res.Events) != 2 {
			t.Errorf("expected both events (internal src, external dst) to match, got %v", ids(res.Events))
		}
	})
}

func TestScopeMatches(t *testing.T) {
	cases := []struct {
		name  string
		scope Scope
		addr  string
		want  bool
	}{
		{"any always matches, even empty", ScopeAny, "", true},
		{"internal matches a private IP", ScopeInternal, "192.168.1.1", true},
		{"internal rejects a public IP", ScopeInternal, "8.8.8.8", false},
		{"internal rejects an unparseable address", ScopeInternal, "not-an-ip", false},
		{"external matches a public IP", ScopeExternal, "8.8.8.8", true},
		{"external rejects a private IP", ScopeExternal, "10.0.0.1", false},
		{"external rejects an empty address", ScopeExternal, "", false},
		{"internal matches loopback", ScopeInternal, "127.0.0.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scopeMatches(c.scope, c.addr); got != c.want {
				t.Errorf("scopeMatches(%q, %q) = %v, want %v", c.scope, c.addr, got, c.want)
			}
		})
	}
}

func TestStats(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionDrop))

	stats := s.Stats()
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3", stats.Total)
	}
	if stats.ByAction[ActionAccept] != 2 || stats.ByAction[ActionDrop] != 1 {
		t.Errorf("ByAction = %v", stats.ByAction)
	}
	if stats.EventsPerSecond <= 0 {
		t.Errorf("expected EventsPerSecond > 0 right after inserts, got %f", stats.EventsPerSecond)
	}
}

// EventsPerSecond exists as a cheaper alternative to Stats().EventsPerSecond
// for a caller (main.go's global-spike ticker) that reads only that one
// field -- it must report exactly the same number Stats() would, not an
// approximation.
func TestEventsPerSecondMatchesStats(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionDrop))

	if got, want := s.EventsPerSecond(), s.Stats().EventsPerSecond; got != want {
		t.Errorf("EventsPerSecond() = %f, want %f (Stats().EventsPerSecond)", got, want)
	}
}

func TestStatsTopRules(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()

	lanWan := mkEvent(now, "core", ActionAccept)
	lanWan.RuleLabel = "lan-wan"
	s.Insert(lanWan)
	s.Insert(lanWan)
	s.Insert(lanWan)

	wanDef := mkEvent(now, "core", ActionDrop)
	wanDef.RuleLabel = "wan-in-def"
	s.Insert(wanDef)
	s.Insert(wanDef)

	unlabeled := mkEvent(now, "core", ActionDrop)
	unlabeled.RuleLabel = ""
	s.Insert(unlabeled)

	stats := s.Stats()
	if len(stats.TopRules) != 2 {
		t.Fatalf("expected 2 distinct labeled rules (unlabeled excluded), got %v", stats.TopRules)
	}
	if stats.TopRules[0].Rule != "lan-wan" || stats.TopRules[0].Count != 3 {
		t.Errorf("expected lan-wan first with count 3, got %+v", stats.TopRules[0])
	}
	if stats.TopRules[1].Rule != "wan-in-def" || stats.TopRules[1].Count != 2 {
		t.Errorf("expected wan-in-def second with count 2, got %+v", stats.TopRules[1])
	}
}

func TestStatsTimeSeries(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()

	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionAccept))
	s.Insert(mkEvent(now, "core", ActionDrop))

	stats := s.Stats()
	if len(stats.TimeSeries) != timeSeriesMinutes {
		t.Fatalf("expected %d buckets, got %d", timeSeriesMinutes, len(stats.TimeSeries))
	}
	for i := 1; i < len(stats.TimeSeries); i++ {
		if !stats.TimeSeries[i].Time.After(stats.TimeSeries[i-1].Time) {
			t.Fatalf("buckets not in chronological order at index %d: %v then %v",
				i, stats.TimeSeries[i-1].Time, stats.TimeSeries[i].Time)
		}
	}

	last := stats.TimeSeries[len(stats.TimeSeries)-1]
	if last.ByAction[ActionAccept] != 2 || last.ByAction[ActionDrop] != 1 {
		t.Errorf("expected current-minute bucket {accept:2, drop:1}, got %v", last.ByAction)
	}

	first := stats.TimeSeries[0]
	if len(first.ByAction) != 0 {
		t.Errorf("expected an empty (not nil) ByAction map for an idle minute, got %v", first.ByAction)
	}
}

// TestEveryActionHasItsOwnTimeSeriesSlot guards the one way the action
// vocabulary can be extended and still quietly lose events (#437).
//
// Stats.ByAction is a map and grows on its own; Stats.TimeSeries is a
// fixed array indexed by actionSlots, and actionSlot folds anything
// missing from that list into the last slot -- which is ActionUnknown.
// An Action constant added without a slot therefore reports correctly in
// one half of the same Stats call and as "unknown" in the other, with
// nothing failing.
func TestEveryActionHasItsOwnTimeSeriesSlot(t *testing.T) {
	all := []Action{
		ActionAccept, ActionDrop, ActionReject, ActionLog,
		ActionMarked, ActionNatted, ActionUnknown,
	}
	if len(all) != len(actionSlots) {
		t.Fatalf("this test lists %d actions but actionSlots has %d -- add the new one to both", len(all), len(actionSlots))
	}

	s := New(100, time.Hour)
	now := time.Now()
	for i, a := range all {
		for n := 0; n <= i; n++ { // a distinct count per action
			s.Insert(mkEvent(now, "core", a))
		}
	}

	stats := s.Stats()
	last := stats.TimeSeries[len(stats.TimeSeries)-1]
	for i, a := range all {
		want := uint64(i + 1)
		if got := last.ByAction[a]; got != want {
			t.Errorf("time-series count for %q = %d, want %d (folded into another slot?)", a, got, want)
		}
		if got := stats.ByAction[a]; got != want {
			t.Errorf("total count for %q = %d, want %d", a, got, want)
		}
	}
}

func ids(events []Event) []uint64 {
	out := make([]uint64, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

// BenchmarkEventsPerSecondVsStats pins EventsPerSecond()'s savings over
// Stats().EventsPerSecond for main.go's global-spike ticker, which
// polls this every 10s and reads nothing else from the result.
func BenchmarkEventsPerSecondVsStats(b *testing.B) {
	s := New(50_000, time.Hour)
	now := time.Now()
	for i := 0; i < 20_000; i++ {
		e := mkEvent(now, "core", ActionAccept)
		e.RuleLabel = fmt.Sprintf("rule-%d", i%20)
		s.Insert(e)
	}

	b.Run("Stats", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.Stats().EventsPerSecond
		}
	})
	b.Run("EventsPerSecond", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.EventsPerSecond()
		}
	})
}
