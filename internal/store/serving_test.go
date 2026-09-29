// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"net"
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

// The scan walks backward through the ring exactly as Ports and
// DoorLastSeen do, so a store filled to exactly its capacity -- head
// wrapped back to 0 -- must still find its own newest event.
func TestServingWalksBackwardThroughAWrappedRing(t *testing.T) {
	s := New(2, time.Hour)
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445})
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.22", DstIP: "10.0.30.5", DstPort: 22})

	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 2 {
		t.Fatalf("want both servers found across the wrap, got %+v", got.Hosts)
	}
}

// An event older than Since must stop the backward scan rather than being
// silently skipped -- the same "break, not continue" contract Ports and
// Trace hold, since anything further back can only be older still.
func TestServingSinceFilterStopsAtTheWindowEdge(t *testing.T) {
	s := New(1000, time.Hour)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	s.Insert(Event{ReceivedAt: older, Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445})
	s.Insert(Event{ReceivedAt: newer, Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.22", DstIP: "10.0.30.5", DstPort: 22})

	got := s.Serving(ServingQuery{Since: newer.Add(-time.Minute)})
	if len(got.Hosts) != 1 || got.Hosts[0].IP != "10.0.30.5" {
		t.Fatalf("want only the event inside the window, got %+v", got.Hosts)
	}
}

// A line missing either end of what "served" means -- no destination
// address, or no destination port at all (ICMP, for instance) -- is not a
// host answering something and must not appear.
func TestServingSkipsEventsMissingDestination(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.21", DstIP: "", DstPort: 445})
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "icmp",
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 0})

	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 0 {
		t.Fatalf("neither line names a real server, got %+v", got.Hosts)
	}
}

// The picker only ever offers tcp or udp, the same restriction Ports
// applies to its own candidates, so a line on any other protocol must not
// mint a served port nothing can select.
func TestServingSkipsNonTCPUDPProtocol(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "icmp",
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 8})

	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 0 {
		t.Fatalf("an icmp line must not appear as a served port, got %+v", got.Hosts)
	}
}

// A host's own ports sort busiest first, and only fall back to port then
// protocol once two ports tie on events -- both tie-break rungs need
// their own case: one pair that actually differs in event count, and one
// pair tied on both events and port number (53/tcp vs 53/udp) which can
// only be split by protocol.
func TestServingSortsPortsByEventsThenPortThenProto(t *testing.T) {
	s := New(1000, time.Hour)
	insert := func(proto string, port, n int) {
		for i := 0; i < n; i++ {
			s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: proto,
				SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: port})
		}
	}
	insert("tcp", 443, 1)
	insert("tcp", 53, 2)
	insert("udp", 53, 2)
	insert("tcp", 80, 3)

	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != 1 {
		t.Fatalf("want one server host, got %+v", got.Hosts)
	}
	ports := got.Hosts[0].Ports
	want := []ServedPort{
		{Port: 80, Proto: "tcp", Events: 3},
		{Port: 53, Proto: "tcp", Events: 2},
		{Port: 53, Proto: "udp", Events: 2},
		{Port: 443, Proto: "tcp", Events: 1},
	}
	if len(ports) != len(want) {
		t.Fatalf("want %d ports, got %+v", len(want), ports)
	}
	for i, w := range want {
		if ports[i] != w {
			t.Fatalf("want %+v at position %d, got %+v", w, i, ports[i])
		}
	}
}

// servingLineCap bounds distinct host x port x proto lines the same way
// maxPortLines bounds Ports' own tally. Once the cap is reached, a
// wholly new line must be dropped rather than minting another host --
// the scan has already made its point about how many distinct servers
// this window holds.
func TestServingStopsCountingNewLinesPastTheCap(t *testing.T) {
	s := New(servingLineCap+1, time.Hour)
	// The scan walks backward from the newest event, so this marker -- the
	// one wholly new line that must be dropped -- has to be the *oldest*
	// insertion: everything filling the cap below is met first.
	s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
		SrcIP: "10.0.10.21", DstIP: "203.0.113.9", DstPort: 80})
	for i := 0; i < servingLineCap; i++ {
		ip := net.IPv4(10, 0, byte(i>>8), byte(i)).String()
		s.Insert(Event{Time: time.Now(), Action: ActionAccept, Protocol: "tcp",
			SrcIP: "10.0.10.21", DstIP: ip, DstPort: 80})
	}

	got := s.Serving(ServingQuery{})
	if len(got.Hosts) != servingLineCap {
		t.Fatalf("want exactly the capped number of hosts, got %d", len(got.Hosts))
	}
	for _, h := range got.Hosts {
		if h.IP == "203.0.113.9" {
			t.Fatal("a wholly new line past the cap must not be counted")
		}
	}
}
