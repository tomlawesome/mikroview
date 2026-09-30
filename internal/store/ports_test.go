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

// The fixture's refused pair has fourteen same-line drops and no
// same-minute traffic at all (everything else in the fixture is a
// different source). This exercises the cap and the newest-first order.
func TestTraceSameLineIsCappedNewestFirstAndIncludesTheSubject(t *testing.T) {
	s := New(1000, time.Hour)
	var subjectID uint64
	for i := 0; i < 14; i++ {
		inserted := s.Insert(Event{Action: ActionDrop, RuleLabel: "default drop", InInterface: "ether4", Protocol: "tcp",
			SrcIP: "10.0.30.14", DstIP: "10.0.10.21", DstPort: 445, Time: time.Now()})
		subjectID = inserted.ID
	}

	got := s.Trace(TraceQuery{ID: subjectID})
	if got.Event == nil {
		t.Fatal("the newest of the fourteen should be traceable by id")
	}
	if len(got.SameLine) != maxTraceRelated {
		t.Fatalf("want the list capped at %d, got %d", maxTraceRelated, len(got.SameLine))
	}
	var sawSubject bool
	for _, e := range got.SameLine {
		if e.ID == subjectID {
			sawSubject = true
		}
	}
	if !sawSubject {
		t.Fatal("the traced event itself must be in SameLine so the list can mark it")
	}
	// Newest first: the buffer holds ids ascending on insert, so the
	// first row must be the highest id and each row after strictly lower.
	for i := 1; i < len(got.SameLine); i++ {
		if got.SameLine[i].ID >= got.SameLine[i-1].ID {
			t.Fatalf("SameLine must be newest first, got ids %v", idsOf(got.SameLine))
		}
	}
}

func idsOf(es []Event) []uint64 {
	out := make([]uint64, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

// SAME MINUTE is the sender's other lines in its own clock minute,
// excluding whatever SAME LINE already counted.
func TestTraceSameMinuteExcludesSameLineAndOtherSenders(t *testing.T) {
	s := New(1000, time.Hour)
	now := time.Date(2026, 9, 9, 22, 4, 31, 0, time.UTC)
	s.Insert(Event{Action: ActionDrop, InInterface: "ether4", Protocol: "tcp",
		SrcIP: "10.0.30.14", DstIP: "10.0.10.21", DstPort: 445, Time: now})
	// Same sender, same minute, a different line: belongs in SAME MINUTE.
	s.Insert(Event{Action: ActionAccept, Protocol: "udp",
		SrcIP: "10.0.30.14", DstIP: "10.0.20.5", DstPort: 53, Time: now.Add(-5 * time.Second)})
	// Same sender, same minute, but the very same line: must not double
	// up into SAME MINUTE as well as SAME LINE.
	s.Insert(Event{Action: ActionDrop, InInterface: "ether4", Protocol: "tcp",
		SrcIP: "10.0.30.14", DstIP: "10.0.10.21", DstPort: 445, Time: now.Add(-10 * time.Second)})
	// Same sender, one second into the next minute: excluded.
	s.Insert(Event{Action: ActionAccept, Protocol: "udp",
		SrcIP: "10.0.30.14", DstIP: "10.0.20.5", DstPort: 53, Time: now.Truncate(time.Minute).Add(time.Minute)})
	// A different sender in the same minute: not this trace's business.
	s.Insert(Event{Action: ActionAccept, Protocol: "udp",
		SrcIP: "10.0.30.9", DstIP: "10.0.20.5", DstPort: 53, Time: now})

	got := s.Trace(TraceQuery{In: "ether4", Port: 445, Proto: "tcp"})
	if got.Event == nil {
		t.Fatal("the refused pair should be traceable")
	}
	if got.Like != 2 {
		t.Fatalf("two same-line drops, got %d", got.Like)
	}
	if got.SameMinuteTotal != 1 || len(got.SameMinute) != 1 {
		t.Fatalf("want exactly one same-minute row (the DNS lookup), got total=%d rows=%+v", got.SameMinuteTotal, got.SameMinute)
	}
	if got.SameMinute[0].DstPort != 53 {
		t.Fatalf("want the DNS lookup, got %+v", got.SameMinute[0])
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

// DoorLastSeen backs #1319's "seen arriving" mark: it answers about
// traffic that arrived at the WAN edge, regardless of what the router
// then did with it, and regardless of whether a door was even pushed
// for the port -- the caller decides which ports to ask about.
func TestDoorLastSeen(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})
	// A drop still counts as "arrived" -- this mark is about traffic
	// reaching the edge, not about the router's own verdict on it.
	s.Insert(Event{DeviceID: "core", Action: ActionDrop, Protocol: "udp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.51", DstIP: "203.0.113.9", DstPort: 500})
	// A private source is never "from the internet".
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "10.0.0.5", DstIP: "203.0.113.9", DstPort: 8443})
	// The wrong interface: not this device's WAN.
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "bridge1", SrcIP: "203.0.113.52", DstIP: "10.0.0.9", DstPort: 445})
	// A second device's own WAN traffic must not answer for "core".
	s.Insert(Event{DeviceID: "shed", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.53", DstIP: "203.0.113.1", DstPort: 22})

	got := s.DoorLastSeen("core", "ether1", []DoorPort{
		{Port: 22, Proto: "tcp"},
		{Port: 500, Proto: "udp"},
		{Port: 8443, Proto: "tcp"},
		{Port: 445, Proto: "tcp"},
	})
	if _, ok := got[DoorPort{22, "tcp"}]; !ok {
		t.Fatal("a public-source accept on the WAN interface must answer")
	}
	if _, ok := got[DoorPort{500, "udp"}]; !ok {
		t.Fatal("a dropped packet still arrived")
	}
	if _, ok := got[DoorPort{8443, "tcp"}]; ok {
		t.Fatal("a private source is not from the internet")
	}
	if _, ok := got[DoorPort{445, "tcp"}]; ok {
		t.Fatal("traffic on another interface is not this WAN's arrival")
	}
	if len(got) != 2 {
		t.Fatalf("want exactly two doors answered, got %+v", got)
	}
}

// An unset device or wan is never a valid door to ask about: the caller
// forgot to name one, not "match everything".
func TestDoorLastSeenEmptyDeviceOrWanAnswersNothing(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})

	if got := s.DoorLastSeen("", "ether1", []DoorPort{{Port: 22, Proto: "tcp"}}); len(got) != 0 {
		t.Fatalf("an empty device must answer nothing, got %+v", got)
	}
	if got := s.DoorLastSeen("core", "", []DoorPort{{Port: 22, Proto: "tcp"}}); len(got) != 0 {
		t.Fatalf("an empty wan must answer nothing, got %+v", got)
	}
}

// A caller that names no port worth asking about -- an empty list, or one
// holding only non-positive ports -- gets an empty answer without a scan.
func TestDoorLastSeenNoValidPortsAnswersNothing(t *testing.T) {
	s := New(1000, time.Hour)
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})

	if got := s.DoorLastSeen("core", "ether1", nil); len(got) != 0 {
		t.Fatalf("no ports asked must answer nothing, got %+v", got)
	}
	if got := s.DoorLastSeen("core", "ether1", []DoorPort{{Port: 0}, {Port: -1}}); len(got) != 0 {
		t.Fatalf("only non-positive ports asked must answer nothing, got %+v", got)
	}
}

// An empty window -- nothing ever inserted -- answers nothing rather than
// scanning a buffer that holds no events.
func TestDoorLastSeenOverAnEmptyWindowAnswersNothing(t *testing.T) {
	s := New(1000, time.Hour)
	got := s.DoorLastSeen("core", "ether1", []DoorPort{{Port: 22, Proto: "tcp"}})
	if len(got) != 0 {
		t.Fatalf("an empty store must answer nothing, got %+v", got)
	}
}

// The scan walks backward through the ring exactly as Ports does, so once
// the buffer has wrapped (head back at 0), the newest event is at the far
// end of the slice rather than the one before head. This fills a
// capacity-2 store exactly full so that wraparound path runs.
func TestDoorLastSeenWalksBackwardThroughAWrappedRing(t *testing.T) {
	s := New(2, time.Hour)
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "udp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.51", DstIP: "203.0.113.9", DstPort: 500})

	got := s.DoorLastSeen("core", "ether1", []DoorPort{{Port: 22, Proto: "tcp"}, {Port: 500, Proto: "udp"}})
	if _, ok := got[DoorPort{22, "tcp"}]; !ok {
		t.Fatalf("want the wrapped buffer's own arrival still found, got %+v", got)
	}
	if _, ok := got[DoorPort{500, "udp"}]; !ok {
		t.Fatalf("want both doors found across the wrap, got %+v", got)
	}
}

// A door already answered from a newer arrival must not be overwritten by
// an older one met later in the backward scan -- the mark is "most recent
// seen", and the newest event on a line is met first.
func TestDoorLastSeenKeepsTheNewestArrivalOnRepeat(t *testing.T) {
	s := New(1000, time.Hour)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", ReceivedAt: older,
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})
	s.Insert(Event{DeviceID: "core", Action: ActionAccept, Protocol: "tcp", ReceivedAt: newer,
		InInterface: "ether1", SrcIP: "203.0.113.60", DstIP: "203.0.113.9", DstPort: 22})

	// A second, never-matching door keeps remaining above zero after the
	// newer 22/tcp arrival is recorded, so the scan does not stop short --
	// it must still walk back to the older, repeated 22/tcp event and hit
	// the "already answered" branch rather than overwriting it.
	got := s.DoorLastSeen("core", "ether1", []DoorPort{{Port: 22, Proto: "tcp"}, {Port: 999, Proto: "tcp"}})
	seen, ok := got[DoorPort{22, "tcp"}]
	if !ok {
		t.Fatal("want the repeated door answered")
	}
	if !seen.Equal(newer) {
		t.Fatalf("want the newest arrival kept despite the older one met later in the scan, got %v want %v", seen, newer)
	}
}
