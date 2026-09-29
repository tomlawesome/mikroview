// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

func TestServingIsEmptyOverAnEmptyWindow(t *testing.T) {
	s := New(1000, time.Hour)
	got := s.Serving(ServingQuery{})
	if got.Hosts == nil || len(got.Hosts) != 0 {
		t.Fatalf("an empty window must answer with no hosts, got %+v", got.Hosts)
	}
}

func TestServingCountsInDirectionOnly(t *testing.T) {
	s := New(1000, time.Hour)
	insert := func(e Event) {
		e.Time = time.Now()
		s.Insert(e)
	}
	// tom-desktop reaches out to nas on 445: nas served it, tom-desktop
	// did not -- reaching out is never serving.
	insert(Event{Action: ActionAccept, Protocol: "tcp", SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445, DstHostName: "nas"})
	// Something reaches tom-desktop itself on 3389: this direction *is*
	// serving, on the same host that a moment ago was only reaching out.
	insert(Event{Action: ActionAccept, Protocol: "tcp", SrcIP: "10.0.30.14", DstIP: "10.0.10.21", DstPort: 3389, DstHostName: "tom-desktop"})

	got := s.Serving(ServingQuery{})
	byIP := map[string]ServingHost{}
	for _, h := range got.Hosts {
		byIP[h.IP] = h
	}
	if len(byIP) != 2 {
		t.Fatalf("want both hosts that were reached, got %+v", got.Hosts)
	}
	nas := byIP["10.0.20.5"]
	if len(nas.Ports) != 1 || nas.Ports[0].Port != 445 {
		t.Fatalf("want nas credited with 445, got %+v", nas)
	}
	desktop := byIP["10.0.10.21"]
	if len(desktop.Ports) != 1 || desktop.Ports[0].Port != 3389 {
		t.Fatalf("want tom-desktop credited with 3389 only, not the 445 it reached out to; got %+v", desktop)
	}
}

func TestServingRefusesADroppedKnock(t *testing.T) {
	s := New(1000, time.Hour)
	// Fourteen refusals at the router: the host behind it was never
	// reached, so it never answered -- counting these would be exactly
	// the "open port" claim #1320 refuses to make.
	for i := 0; i < 14; i++ {
		s.Insert(Event{Time: time.Now(), Action: ActionDrop, RuleLabel: "default drop", Protocol: "tcp",
			SrcIP: "203.0.113.9", DstIP: "10.0.10.21", DstPort: 3389})
	}
	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 0 {
		t.Fatalf("a wholly refused host must not appear as a server, got %+v", got.Hosts)
	}
}

func TestServingCapsPortsAtSixWithMore(t *testing.T) {
	s := New(1000, time.Hour)
	for port := 1; port <= 9; port++ {
		s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
			SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: port})
	}
	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 1 {
		t.Fatalf("want one server host, got %+v", got.Hosts)
	}
	h := got.Hosts[0]
	if len(h.Ports) != 6 {
		t.Fatalf("want the badge capped at six ports, got %d: %+v", len(h.Ports), h.Ports)
	}
	if h.More != 3 {
		t.Fatalf("want the three beyond the cap counted as More, got %d", h.More)
	}
	// Every port here was hit once, so the cap must be stable rather than
	// arbitrary -- ties break on the port number.
	for i := 0; i < len(h.Ports)-1; i++ {
		if h.Ports[i].Port > h.Ports[i+1].Port {
			t.Fatalf("want ties broken by ascending port, got %+v", h.Ports)
		}
	}
}

func TestServingDeviceFilter(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{Time: time.Now(), DeviceID: "core", Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445})
	s.Insert(Event{Time: time.Now(), DeviceID: "branch", Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.22", DstIP: "10.0.30.5", DstPort: 445})

	got := s.Serving(ServingQuery{Device: "core"})
	if len(got.Hosts) != 1 || got.Hosts[0].IP != "10.0.20.5" {
		t.Fatalf("want only core's own server, got %+v", got.Hosts)
	}
}
