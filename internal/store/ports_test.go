// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// The fixture is round 49's own data story, which round 53 filters:
// 445/tcp accepted between LAN and Servers, refused from IoT to LAN, and
// 3389/tcp seen nowhere.
func portsFixture(t *testing.T) *Store {
	t.Helper()
	s := New(1000, time.Hour)
	insert := func(e Event) {
		e.Time = time.Now()
		s.Insert(e)
	}
	// Two accepted SMB lines, LAN -> Servers.
	insert(Event{Action: ActionAccept, InInterface: "bridge1", OutInterface: "ether3", Protocol: "tcp",
		SrcIP: "10.0.10.21", SrcHostName: "tom-desktop", DstIP: "10.0.20.5", DstHostName: "nas", DstPort: 445})
	insert(Event{Action: ActionAccept, InInterface: "bridge1", OutInterface: "ether3", Protocol: "tcp",
		SrcIP: "10.0.10.34", SrcHostName: "laptop-anna", DstIP: "10.0.20.5", DstHostName: "nas", DstPort: 445})
	// Fourteen refusals, IoT -> LAN, the unplanned pair. No out-interface:
	// the packet never reached one.
	for i := 0; i < 14; i++ {
		insert(Event{Action: ActionDrop, RuleLabel: "default drop", InInterface: "ether4", Protocol: "tcp",
			SrcIP: "10.0.30.14", SrcHostName: "cam-porch", DstIP: "10.0.10.21", DstHostName: "tom-desktop", DstPort: 445})
	}
	// Traffic on other ports, so the candidate list has more than one
	// entry and the filter has something to exclude.
	insert(Event{Action: ActionAccept, InInterface: "bridge1", OutInterface: "ether1", Protocol: "tcp",
		SrcIP: "10.0.10.34", DstIP: "198.51.100.23", DstPort: 22})
	insert(Event{Action: ActionAccept, InInterface: "bridge1", OutInterface: "ether1", Protocol: "udp",
		SrcIP: "10.0.10.34", DstIP: "1.1.1.1", DstPort: 53})
	// ICMP has no port at all, and must not appear anywhere.
	insert(Event{Action: ActionAccept, InInterface: "bridge1", OutInterface: "ether1", Protocol: "icmp",
		SrcIP: "10.0.10.34", DstIP: "1.1.1.1"})
	return s
}

func TestPortsCandidatesAreWhatTheWindowCarried(t *testing.T) {
	s := portsFixture(t)
	got := s.Ports(PortQuery{})

	if got.Events != 0 || len(got.Ribs) != 0 || len(got.Hosts) != 0 {
		t.Fatalf("no selection must summarise nothing, got %+v", got)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("want 445/tcp, 22/tcp and 53/udp offered, got %+v", got.Candidates)
	}
	// Busiest first: sixteen lines on 445 against one each on 22 and 53.
	if got.Candidates[0] != (PortCandidate{Port: 445, Proto: "tcp", Count: 16}) {
		t.Fatalf("busiest candidate should be 445/tcp x16, got %+v", got.Candidates[0])
	}
	for _, c := range got.Candidates {
		if c.Proto != "tcp" && c.Proto != "udp" {
			t.Fatalf("the picker offers tcp and udp only, got %q", c.Proto)
		}
	}
}

func TestPortsSummarisesOneSelectedPort(t *testing.T) {
	s := portsFixture(t)
	got := s.Ports(PortQuery{Ports: []int{445}, Proto: "tcp"})

	if got.Events != 16 || got.Accepts != 2 || got.Drops != 14 {
		t.Fatalf("want 16 events, 2 accepted, 14 dropped; got %d/%d/%d", got.Events, got.Accepts, got.Drops)
	}
	// Three distinct source -> destination lines: two accepted, one
	// refused fourteen times.
	if got.Lines != 3 {
		t.Fatalf("want 3 distinct lines, got %d", got.Lines)
	}

	byKey := map[string]PortRib{}
	for _, r := range got.Ribs {
		byKey[r.In+"|"+r.Out] = r
	}
	if len(byKey) != 2 {
		t.Fatalf("want two directions carrying 445/tcp, got %+v", got.Ribs)
	}
	if r := byKey["bridge1|ether3"]; r.Accepts != 2 || r.Drops != 0 {
		t.Fatalf("LAN -> Servers should be wholly accepted, got %+v", r)
	}
	// A refusal with no out-interface keeps the empty out rather than
	// being dropped from the answer: the map draws that direction dying
	// at the router.
	r := byKey["ether4|"]
	if r.Drops != 14 || r.Accepts != 0 {
		t.Fatalf("IoT -> nowhere should be fourteen refusals, got %+v", r)
	}
	if r.RefusedBy != "default drop" {
		t.Fatalf("the refusing rule should be named from the events, got %q", r.RefusedBy)
	}

	names := map[string]PortHost{}
	for _, h := range got.Hosts {
		names[h.IP] = h
	}
	if len(names) != 4 {
		t.Fatalf("want both ends of every line, got %+v", got.Hosts)
	}
	if h := names["10.0.30.14"]; h.Name != "cam-porch" || h.Drops != 14 {
		t.Fatalf("cam-porch should carry its fourteen refusals, got %+v", h)
	}
	if h := names["10.0.10.21"]; h.Events != 15 {
		t.Fatalf("tom-desktop is on both sides here: 1 accepted out, 14 refused in; got %+v", h)
	}
}

func TestPortsProtocolAndPortNarrowTheAnswer(t *testing.T) {
	s := portsFixture(t)

	if got := s.Ports(PortQuery{Ports: []int{445}, Proto: "udp"}); got.Events != 0 || len(got.Ribs) != 0 {
		t.Fatalf("445/udp was never seen; got %+v", got)
	}
	// Nothing seen is not an error and not an empty candidate list: the
	// picker still offers what the window holds, so the operator can
	// pick something else without reopening anything.
	if got := s.Ports(PortQuery{Ports: []int{3389}, Proto: "tcp"}); got.Events != 0 || len(got.Candidates) != 3 {
		t.Fatalf("an unseen port must still offer the picker's own list; got %+v", got)
	}
	if got := s.Ports(PortQuery{Ports: []int{22, 53}}); got.Events != 2 {
		t.Fatalf("several ports with no protocol should take both; got %+v", got)
	}
}

func TestPortsWindowCutsTheScan(t *testing.T) {
	s := portsFixture(t)
	// Everything in the fixture arrived just now, so a window starting
	// in a minute's time holds none of it.
	if got := s.Ports(PortQuery{Ports: []int{445}, Since: time.Now().Add(time.Minute)}); got.Events != 0 {
		t.Fatalf("a window past every event must summarise nothing, got %+v", got)
	}
	if got := s.Ports(PortQuery{Ports: []int{445}, Since: time.Now().Add(-time.Hour)}); got.Events != 16 {
		t.Fatalf("a window covering every event must summarise all of them, got %d", got.Events)
	}
}

func TestPortsDeviceNarrowsTheAnswer(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", DstPort: 445, InInterface: "a", OutInterface: "b"})
	s.Insert(Event{DeviceID: "shed", Action: ActionAccept, Protocol: "tcp", DstPort: 445, InInterface: "c", OutInterface: "d"})

	if got := s.Ports(PortQuery{Ports: []int{445}, Device: "core"}); got.Events != 1 || got.Ribs[0].In != "a" {
		t.Fatalf("one device's own answer only, got %+v", got)
	}
	if got := s.Ports(PortQuery{Ports: []int{445}}); got.Events != 2 {
		t.Fatalf("no device named is every device, got %+v", got)
	}
}

func TestTraceFindsTheLineAndItsTallies(t *testing.T) {
	s := portsFixture(t)

	got := s.Trace(TraceQuery{In: "ether4", Port: 445, Proto: "tcp"})
	if got.Event == nil {
		t.Fatal("the refused pair should be traceable by its own boundary and port")
	}
	if got.Event.SrcIP != "10.0.30.14" || got.Event.DstIP != "10.0.10.21" {
		t.Fatalf("traced the wrong line: %+v", got.Event)
	}
	if got.Like != 14 {
		t.Fatalf("the crumb says 'and 13 more like it' from 14 total, got %d", got.Like)
	}
	// The whole point of the refused drawing: the far end was never
	// reached, and the number says so rather than the drawing implying it.
	if got.DstReached != 0 {
		t.Fatalf("a wholly refused line reached nothing, got %d", got.DstReached)
	}
}

func TestTraceByEventIDIgnoresTheWindow(t *testing.T) {
	s := portsFixture(t)
	all := s.Query(Query{Limit: 100})
	var subject Event
	for _, e := range all.Events {
		if e.DstPort == 22 {
			subject = e
		}
	}
	if subject.ID == 0 {
		t.Fatal("fixture is missing the 22/tcp line this test traces")
	}

	// An operator naming one event has asked about that event. A window
	// that would have excluded it is not a reason to decline.
	got := s.Trace(TraceQuery{ID: subject.ID, Since: time.Now().Add(time.Minute)})
	if got.Event == nil || got.Event.ID != subject.ID {
		t.Fatalf("an event named by id must be traceable whatever the window says, got %+v", got.Event)
	}
	if got.Like != 1 || got.DstReached != 1 {
		t.Fatalf("one accepted line, seen once: got like=%d reached=%d", got.Like, got.DstReached)
	}
}

func TestTraceMissesHonestly(t *testing.T) {
	s := portsFixture(t)
	if got := s.Trace(TraceQuery{Port: 3389, Proto: "tcp"}); got.Event != nil {
		t.Fatalf("nothing on 3389 was ever logged, got %+v", got.Event)
	}
	if got := s.Trace(TraceQuery{ID: 99999}); got.Event != nil {
		t.Fatalf("an id the buffer never held is a miss, got %+v", got.Event)
	}
}

// The cap is what the picker shows, so what falls off it matters: a
// port the window carried must never come back as a named port with
// count 0, which would say "nothing logged on it" about a port that was
// simply busier than the list was long.
func TestPortsCandidateCapCountsNamedAndSeenTogether(t *testing.T) {
	s := New(5000, time.Hour)
	// More distinct ports than the cap, each busier than the last.
	for p := 1; p <= maxPortCandidates+10; p++ {
		for i := 0; i < p; i++ {
			s.Insert(Event{Action: ActionAccept, Protocol: "tcp", DstPort: 1000 + p, InInterface: "a", OutInterface: "b"})
		}
	}
	// 1001 is the quietest port here, so the cap cuts it -- and a rule
	// names it, which is exactly the case that used to come back saying
	// nothing had been logged on it.
	named := map[int]bool{1001: true, 9999: true}
	got := s.Ports(PortQuery{Named: named, NamedProto: "tcp"})

	// The cap falls on traffic, never on the rules: 1001 is busy enough
	// to survive it and 9999 is offered because a rule names it.
	// The cap falls on traffic, never on the rules: the 24 busiest plus
	// 9999, which is offered only because a rule names it.
	if len(got.Candidates) != maxPortCandidates+1 {
		t.Fatalf("want the %d busiest plus the one unused named port, got %d", maxPortCandidates, len(got.Candidates))
	}
	for _, c := range got.Candidates {
		if c.Port == 1001 {
			t.Fatalf("a port the window carried and the cap cut must not reappear as unused, got %+v", c)
		}
	}
	for _, c := range got.Candidates {
		if c.Port == 1001 && c.Count == 0 {
			t.Fatal("a port the window carried came back as though nothing had been logged on it")
		}
	}
	// A named port the window never carried is offered, which is the only
	// way it is ever offered.
	var offered bool
	for _, c := range got.Candidates {
		if c.Port == 9999 {
			offered = true
			if c.Count != 0 || !c.Named || c.Proto != "tcp" {
				t.Fatalf("an unused named port is offered as such, got %+v", c)
			}
		}
	}
	if !offered {
		t.Fatal("a port only a rule names must still reach the picker")
	}
}

func TestTraceNoOutTakesOnlyALineThatNeverLeftTheRouter(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{Action: ActionDrop, RuleLabel: "default drop", Protocol: "tcp", DstPort: 445,
		InInterface: "ether4", SrcIP: "10.0.30.14", DstIP: "10.0.10.21"})
	// Newer, same in-interface and port, but this one did leave.
	s.Insert(Event{Action: ActionAccept, Protocol: "tcp", DstPort: 445,
		InInterface: "ether4", OutInterface: "ether3", SrcIP: "10.0.30.9", DstIP: "10.0.20.5"})

	// Unset means "any", so the newest wins.
	if got := s.Trace(TraceQuery{In: "ether4", Port: 445}); got.Event == nil || got.Event.OutInterface != "ether3" {
		t.Fatalf("an unset out-interface means any, got %+v", got.Event)
	}
	// Stated-absent is a different ask: the callout that opened this is
	// about a pair that never reached one, and the drawing must be about
	// the same line the callout is.
	got := s.Trace(TraceQuery{In: "ether4", Port: 445, NoOut: true})
	if got.Event == nil || got.Event.OutInterface != "" || got.Event.SrcIP != "10.0.30.14" {
		t.Fatalf("noOut must take the line that never left the router, got %+v", got.Event)
	}
}

func TestTraceTalliesOnlyTheDeviceItWasAskedAbout(t *testing.T) {
	s := New(1000, time.Hour)
	for i := 0; i < 3; i++ {
		s.Insert(Event{DeviceID: "core", Action: ActionDrop, Protocol: "tcp", DstPort: 445,
			InInterface: "ether4", SrcIP: "10.0.30.14", DstIP: "10.0.10.21"})
	}
	// A second router logging the same five-tuple. "and N more like it"
	// must not quietly count it.
	for i := 0; i < 5; i++ {
		s.Insert(Event{DeviceID: "shed", Action: ActionDrop, Protocol: "tcp", DstPort: 445,
			InInterface: "ether4", SrcIP: "10.0.30.14", DstIP: "10.0.10.21"})
	}

	if got := s.Trace(TraceQuery{Device: "core", In: "ether4", Port: 445}); got.Like != 3 {
		t.Fatalf("one device's own lines only, got %d", got.Like)
	}
	if got := s.Trace(TraceQuery{In: "ether4", Port: 445}); got.Like != 8 {
		t.Fatalf("no device named is every device, got %d", got.Like)
	}
}
