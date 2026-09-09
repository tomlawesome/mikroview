// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/store"
)

// The fixture is round 49's data story again, this time with the pushed
// filter table beside it -- the two facts the port answer keeps apart.
func portsServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s, st := newTestServer(t)
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "bridge1", OutInterface: "ether3", SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445})
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "bridge1", OutInterface: "ether3", SrcIP: "10.0.10.34", DstIP: "10.0.20.5", DstPort: 445})
	for i := 0; i < 14; i++ {
		st.Insert(store.Event{DeviceID: "core", Action: store.ActionDrop, Protocol: "tcp", Time: time.Now(),
			RuleLabel: "default drop", InInterface: "ether4", SrcHostName: "cam-porch",
			SrcIP: "10.0.30.14", DstIP: "10.0.10.21", DstHostName: "tom-desktop", DstPort: 445})
	}

	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindFilterRule, Page: 1, Pages: 1,
		FilterRules: []ingest.FilterRule{
			{Ordinal: 12, Chain: "forward", Action: "accept", Protocol: "tcp", DstPort: "445",
				InInterface: "bridge1", OutInterface: "ether3", Comment: "SMB to the NAS"},
			{Ordinal: 23, Chain: "input", Action: "drop", DstPort: "3389,445", InInterface: "ether1"},
			{Ordinal: 31, Chain: "forward", Action: "drop", DstPort: "137-445", InInterface: "ether5"},
			// A rule scoping no port at all covers 445 and names none:
			// it must never draw a door.
			{Ordinal: 40, Chain: "forward", Action: "drop"},
			// A disabled rule is a row in a table, not a door.
			{Ordinal: 41, Chain: "forward", Action: "accept", DstPort: "445", Disabled: true},
			// Another protocol's rule is not this protocol's door.
			{Ordinal: 42, Chain: "forward", Action: "accept", Protocol: "udp", DstPort: "445"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	return ts, s
}

func getPorts(t *testing.T, ts *httptest.Server, query string) portsResponse {
	t.Helper()
	res, err := http.Get(ts.URL + "/api/ports" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/ports%s: %d", query, res.StatusCode)
	}
	var out portsResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPortsOffersSeenAndNamedPorts(t *testing.T) {
	ts, _ := portsServer(t)
	got := getPorts(t, ts, "")

	if got.Selection != nil {
		t.Fatalf("no port asked for is no selection, got %+v", got.Selection)
	}
	seen := map[int]store.PortCandidate{}
	for _, c := range got.Candidates {
		seen[c.Port] = c
	}
	if c, ok := seen[445]; !ok || c.Count != 16 || !c.Named {
		t.Fatalf("445 is both seen and named, got %+v (%v)", c, ok)
	}
	// 3389 was never carried, but a rule names it -- which is the whole
	// reason it is offered at all.
	if c, ok := seen[3389]; !ok || c.Count != 0 || !c.Named {
		t.Fatalf("a port a rule names must be offered even unused, got %+v (%v)", c, ok)
	}
	// 137-445 is a range: it names 445 (doorsFor asks the spec), but it
	// puts no chip in the picker for the 308 ports nobody asked about.
	if _, ok := seen[200]; ok {
		t.Fatal("a range must not spray the picker with chips")
	}
}

func TestPortsDrawsSeenTrafficAndDoorsApart(t *testing.T) {
	ts, _ := portsServer(t)
	got := getPorts(t, ts, "?port=445&proto=tcp")

	if got.Selection == nil || got.Selection.Label != "445/tcp" {
		t.Fatalf("the pill's own label should come back with the answer, got %+v", got.Selection)
	}
	if got.Events != 16 || got.Accepts != 2 || got.Drops != 14 || got.Lines != 3 {
		t.Fatalf("want 16/2/14 over 3 lines, got %+v", got)
	}
	if len(got.Ribs) != 2 {
		t.Fatalf("two directions carried 445/tcp, got %+v", got.Ribs)
	}

	doors := map[int]portDoor{}
	for _, d := range got.Doors {
		doors[d.Ordinal] = d
	}
	if len(doors) != 3 {
		t.Fatalf("three enabled tcp-or-any rules name 445: #12, #23, #31; got %+v", got.Doors)
	}
	if d := doors[12]; d.Label != "#12" || d.Action != "accept" || d.Who != "bridge1 → ether3 accept" {
		t.Fatalf("the accepting door should name itself from the rule, got %+v", d)
	}
	if d := doors[23]; d.Action != "drop" || d.Who != "ether1 → any drop" {
		t.Fatalf("a rule with no out-interface reads 'any', got %+v", d)
	}
	if _, ok := doors[31]; !ok {
		t.Fatal("a range naming 445 is still a door")
	}
	for _, unwanted := range []int{40, 41, 42} {
		if _, ok := doors[unwanted]; ok {
			t.Fatalf("rule #%d must not draw a door", unwanted)
		}
	}
}

func TestPortsNothingSeenStillDrawsTheDoor(t *testing.T) {
	ts, _ := portsServer(t)
	got := getPorts(t, ts, "?port=3389&proto=tcp")

	if got.Events != 0 || len(got.Ribs) != 0 || len(got.Hosts) != 0 {
		t.Fatalf("3389/tcp was never logged, got %+v", got)
	}
	// "no logged traffic on 3389/tcp in the window · one rule names it"
	// -- the door is the second half of that sentence, and it is drawn.
	if len(got.Doors) != 1 || got.Doors[0].Ordinal != 23 {
		t.Fatalf("one rule names 3389 and its door is still drawn, got %+v", got.Doors)
	}
}

func TestPortsUdpTakesOnlyUdpDoors(t *testing.T) {
	ts, _ := portsServer(t)
	got := getPorts(t, ts, "?port=445&proto=udp")

	doors := map[int]bool{}
	for _, d := range got.Doors {
		doors[d.Ordinal] = true
	}
	// #42 is the udp rule; #23 and #31 scope no protocol, so they are
	// doors for either. #12 is tcp-only and must not appear.
	if !doors[42] || !doors[23] || !doors[31] || doors[12] {
		t.Fatalf("udp's doors are the udp and unscoped rules only, got %+v", got.Doors)
	}
}

func TestPortsRefusesAMalformedSelection(t *testing.T) {
	ts, _ := portsServer(t)
	for _, q := range []string{"?port=notaport", "?port=0", "?port=70000", "?port=445&proto=sctp", "?port=1-65535", "?since=yesterday"} {
		res, err := http.Get(ts.URL + "/api/ports" + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		// A filter the caller believes in and the server ignored is the
		// misreading badQueryParam exists to refuse.
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET /api/ports%s should be refused, got %d", q, res.StatusCode)
		}
	}
}

func TestPortLabelSaysWhatWasAskedFor(t *testing.T) {
	for _, c := range []struct {
		ports []int
		proto string
		want  string
	}{
		{[]int{445}, "tcp", "445/tcp"},
		{[]int{445}, "", "445"},
		{[]int{22, 23}, "tcp", "22-23/tcp"},
		{[]int{22, 80, 443}, "udp", "22,80,443/udp"},
		{nil, "tcp", ""},
	} {
		if got := portLabel(c.ports, c.proto); got != c.want {
			t.Fatalf("portLabel(%v, %q) = %q, want %q", c.ports, c.proto, got, c.want)
		}
	}
}

func TestTraceEndpointAnswersOneHop(t *testing.T) {
	ts, _ := portsServer(t)

	res, err := http.Get(ts.URL + "/api/trace?in=ether4&port=445&proto=tcp")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got traceResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Event == nil {
		t.Fatalf("the refused pair is traceable, got %+v", got)
	}
	if got.Verdict != "refused" {
		t.Fatalf("a dropped line is refused, got %q", got.Verdict)
	}
	// The crumb reads "and 13 more like it": the wire already carries
	// the 13, not the 14, so nothing subtracts on the far side.
	if got.Like != 13 || got.SrcSeen != 14 || got.DstReached != 0 {
		t.Fatalf("want like=13 srcSeen=14 dstReached=0, got %+v", got)
	}
	if got.Event.OutInterface != "" {
		t.Fatalf("the refusal never reached an out-interface, got %q", got.Event.OutInterface)
	}
}

func TestTraceEndpointMissesHonestly(t *testing.T) {
	ts, _ := portsServer(t)

	res, err := http.Get(ts.URL + "/api/trace?port=3389&proto=tcp")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got traceResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Found || got.Event != nil {
		t.Fatalf("nothing on 3389 was logged; a miss says so rather than drawing an empty path: %+v", got)
	}
}

func TestTraceEndpointRefusesAnEmptyOrMalformedAsk(t *testing.T) {
	ts, _ := portsServer(t)
	for _, q := range []string{"", "?event=nope", "?event=0", "?port=0", "?in=ether4&since=yesterday"} {
		res, err := http.Get(ts.URL + "/api/trace" + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET /api/trace%s should be refused, got %d", q, res.StatusCode)
		}
	}
}

func TestTraceEndpointNoOutTakesTheLineThatNeverLeft(t *testing.T) {
	ts, s := portsServer(t)
	// Newer, same in-interface and port, but this one did leave the
	// router. Without `noOut` the trace lands on it, which is a
	// different line from the one the map's callout is about.
	s.Store.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether4", OutInterface: "ether3", SrcIP: "10.0.30.9", DstIP: "10.0.20.5", DstPort: 445})

	read := func(q string) traceResponse {
		t.Helper()
		res, err := http.Get(ts.URL + "/api/trace" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out traceResponse
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := read("?in=ether4&port=445&proto=tcp"); got.Event == nil || got.Event.OutInterface != "ether3" {
		t.Fatalf("an unset out-interface means any, got %+v", got.Event)
	}
	got := read("?in=ether4&port=445&proto=tcp&noOut=1")
	if got.Event == nil || got.Event.OutInterface != "" || got.Verdict != "refused" {
		t.Fatalf("noOut must take the line that never left the router, got %+v", got.Event)
	}
}
