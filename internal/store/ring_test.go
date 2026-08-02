package store

import (
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

func ids(events []Event) []uint64 {
	out := make([]uint64, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}
