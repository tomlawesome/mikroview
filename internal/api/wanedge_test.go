// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/store"
)

func wanDoorsServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s, _ := newTestServer(t)

	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindFilterRule, Page: 1, Pages: 1,
		FilterRules: []ingest.FilterRule{
			// A door to the router itself.
			{Ordinal: 5, Chain: "input", Action: "accept", Protocol: "tcp", DstPort: "22",
				InInterface: "ether1", Comment: "admin ssh"},
			// Same shape, but the wrong interface: not a door.
			{Ordinal: 6, Chain: "input", Action: "accept", Protocol: "tcp", DstPort: "23",
				InInterface: "bridge1"},
			// Forward-chain accepts are policy.svelte.ts's business, not
			// this panel's.
			{Ordinal: 7, Chain: "forward", Action: "accept", Protocol: "tcp", DstPort: "443", InInterface: "ether1"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindNATRule, Page: 1, Pages: 1,
		NATRules: []ingest.NATRule{
			{Ordinal: 10, Chain: "dstnat", Action: "dst-nat", Protocol: "tcp", DstPort: "8443",
				InInterface: "ether1", ToAddresses: "192.168.1.5", Comment: "plex"},
			// Disabled: a row in a table, not a door.
			{Ordinal: 11, Chain: "dstnat", Action: "dst-nat", Protocol: "tcp", DstPort: "8080",
				InInterface: "ether1", ToAddresses: "192.168.1.6", Disabled: true},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindIPAddress, Page: 1, Pages: 1,
		IPAddresses: []ingest.IPAddressEntry{
			{Address: "203.0.113.9/32", Interface: "ether1"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	return ts, s
}

func getWANDoors(t *testing.T, ts *httptest.Server, query string) wanDoorsResponse {
	t.Helper()
	res, err := http.Get(ts.URL + "/api/doors/internet" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/doors/internet%s: %d", query, res.StatusCode)
	}
	var out wanDoorsResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWANDoorsRequiresDeviceAndWAN(t *testing.T) {
	ts, _ := wanDoorsServer(t)
	for _, q := range []string{"", "?device=core", "?wan=ether1"} {
		res, err := http.Get(ts.URL + "/api/doors/internet" + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q: want 400, got %d", q, res.StatusCode)
		}
	}
}

func TestWANDoorsDisabledNATRuleIsNotADoor(t *testing.T) {
	ts, _ := wanDoorsServer(t)
	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	for _, d := range got.Devices[0].Doors {
		if d.Ordinal == 11 {
			t.Fatalf("a disabled dst-nat rule must not draw a door, got %+v", d)
		}
	}
}

func TestWANDoorsInputAcceptOnNonWANInterfaceIsNotADoor(t *testing.T) {
	ts, _ := wanDoorsServer(t)
	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	dev := got.Devices[0]
	byOrdinal := map[int]wanDoor{}
	for _, d := range dev.Doors {
		byOrdinal[d.Ordinal] = d
	}
	if _, ok := byOrdinal[6]; ok {
		t.Fatalf("an input accept on bridge1 is not a door to ether1's WAN, got %+v", dev.Doors)
	}
	if _, ok := byOrdinal[7]; ok {
		t.Fatalf("a forward-chain accept is a policy edge, not a WAN door, got %+v", dev.Doors)
	}
	d, ok := byOrdinal[5]
	if !ok {
		t.Fatalf("the input accept on ether1 must be a door, got %+v", dev.Doors)
	}
	if d.To != nil {
		t.Fatalf("an input-chain door ends at the router itself, want To nil, got %+v", d.To)
	}
	if d.Label != "#5" || d.DstPort != "22" || d.Comment != "admin ssh" {
		t.Fatalf("the door should carry the rule's own label/port/comment, got %+v", d)
	}

	nat, ok := byOrdinal[10]
	if !ok {
		t.Fatalf("the enabled dst-nat rule must be a door, got %+v", dev.Doors)
	}
	if nat.To == nil || nat.To.IP != "192.168.1.5" {
		t.Fatalf("a dst-nat door must name the host it forwards to, got %+v", nat.To)
	}
}

func TestWANDoorsServicesNullWithoutThePush(t *testing.T) {
	ts, _ := wanDoorsServer(t)
	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	if got.Devices[0].Services != nil {
		t.Fatalf("no /ip/service push means services is null, got %+v", got.Devices[0].Services)
	}

	// Raw JSON check too: the frontend's "this router does not push
	// /ip/service yet" line depends on seeing a literal null, not an
	// empty array that happens to unmarshal the same way into Go.
	res, err := http.Get(ts.URL + "/api/doors/internet?device=core&wan=ether1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var raw struct {
		Devices []struct {
			Services json.RawMessage `json:"services"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.Devices[0].Services) != "null" {
		t.Fatalf("want a literal JSON null for services, got %s", raw.Devices[0].Services)
	}
}

func TestWANDoorsServicesListedAndAddressEmptyMeansNoRestriction(t *testing.T) {
	ts, s := wanDoorsServer(t)
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindIPService, Page: 1, Pages: 1,
		IPServices: []ingest.IPServiceEntry{
			{Name: "winbox", Port: 8291},
			{Name: "telnet", Port: 23, Disabled: true},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	svc := got.Devices[0].Services
	if svc == nil {
		t.Fatal("a pushed /ip/service table must not read as null")
	}
	if len(svc) != 1 {
		t.Fatalf("the disabled service is never a row, want 1, got %+v", svc)
	}
	if svc[0].Name != "winbox" || svc[0].Port != 8291 {
		t.Fatalf("want winbox/8291, got %+v", svc[0])
	}
	if svc[0].Address != "" {
		t.Fatalf("no address restriction must serialise as an empty string, got %q", svc[0].Address)
	}
}

func TestWANDoorsSeenArrivingMark(t *testing.T) {
	ts, s := wanDoorsServer(t)
	st := s.Store
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	var sawFive, sawTen bool
	for _, d := range got.Devices[0].Doors {
		switch d.Ordinal {
		case 5:
			sawFive = d.LastSeen != nil
		case 10:
			sawTen = d.LastSeen != nil
		}
	}
	if !sawFive {
		t.Fatal("port 22 arrived from a public source on ether1 -- door #5 should carry a last-seen mark")
	}
	if sawTen {
		t.Fatal("nothing arrived on 8443 -- door #10 must carry no last-seen mark")
	}
}

// A real router's input chain is full of `accept connection-state=
// established,related` -- reply traffic on an already-open connection,
// never a fresh one arriving from the internet. Drawing it as a door
// would be exactly the false claim this panel must never make.
func TestWANDoorsConnectionStateAcceptIsNotADoor(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindFilterRule, Page: 1, Pages: 1,
		FilterRules: []ingest.FilterRule{
			{Ordinal: 1, Chain: "input", Action: "accept", ConnectionState: ingest.RouterOSList{"established", "related"}},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	if len(got.Devices[0].Doors) != 0 {
		t.Fatalf("a connection-state accept answers only already-open traffic, not a door, got %+v", got.Devices[0].Doors)
	}
}

// A rule that restricts who may use it (src-address or its list form)
// cannot be read as "from the internet" -- this endpoint has no way to
// tell whether the restriction names the internet or the operator's own
// LAN (src-address-list=LAN is the worked example), and the panel's
// fixed caveat already says the list here may be incomplete, never that
// it may be wrong. Covers both fields on the filter table, and the one
// field NATRule carries, on a dst-nat rule.
func TestWANDoorsSenderRestrictedRuleIsNotADoor(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindFilterRule, Page: 1, Pages: 1,
		FilterRules: []ingest.FilterRule{
			{Ordinal: 1, Chain: "input", Action: "accept", SrcAddress: "10.0.0.0/24"},
			{Ordinal: 2, Chain: "input", Action: "accept", SrcAddressList: "LAN"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindNATRule, Page: 1, Pages: 1,
		NATRules: []ingest.NATRule{
			{Ordinal: 3, Chain: "dstnat", Action: "dst-nat", SrcAddress: "10.0.0.0/24", ToAddresses: "192.168.1.5"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	if len(got.Devices[0].Doors) != 0 {
		t.Fatalf("a sender-restricted rule's own restriction is not knowably the internet, got %+v", got.Devices[0].Doors)
	}
}

// A rule naming no dst-port at all covers every port, not none -- it is
// still a door, and the frontend is what turns its empty DstPort into
// "any port" rather than a blank.
func TestWANDoorsNoDstPortIsStillADoor(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindFilterRule, Page: 1, Pages: 1,
		FilterRules: []ingest.FilterRule{
			{Ordinal: 1, Chain: "input", Action: "accept", InInterface: "ether1"},
		},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	if len(got.Devices[0].Doors) != 1 || got.Devices[0].Doors[0].DstPort != "" {
		t.Fatalf("a rule with no dst-port is still a door, with an empty DstPort for the frontend to say 'any port', got %+v", got.Devices[0].Doors)
	}
}

func TestWANDoorsInterfaceListScopedRuleIsNotADoor(t *testing.T) {
	s, _ := newTestServer(t)
	decode := func(body string) ingest.Payload {
		t.Helper()
		p, err := ingest.DecodePayload(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	filter := decode(`{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":1,"chain":"input","action":"accept","inInterfaceList":"LAN"},{"ordinal":2,"chain":"input","action":"accept","protocol":"tcp","dstPort":8291,"inInterfaceList":"LAN"},{"ordinal":3,"chain":"input","action":"accept","protocol":"tcp","dstPort":22,"inInterface":"ether1"}]}`)
	nat := decode(`{"kind":"nat-rule","page":1,"pages":1,"records":[{"ordinal":4,"chain":"dstnat","action":"dst-nat","protocol":"udp","dstPort":53,"inInterfaceList":"LAN","toAddresses":"192.0.2.2"}]}`)
	if err := s.RouterState.Apply("core", filter, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", nat, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	d := got.Devices[0].Doors
	if len(d) != 1 || d[0].Ordinal != 3 || d[0].DstPort != "22" || d[0].To != nil {
		t.Fatalf("a rule scoped by an interface list is not knowably from the internet; only the plain WAN accept (#3) is a door, got %+v", d)
	}
}

// wanJSONServer applies filter and NAT tables written as the push's own
// JSON records (A4-C1), so a rule can carry any condition the script sends.
func wanJSONServer(t *testing.T, filterRecords, natRecords string) *httptest.Server {
	t.Helper()
	s, _ := newTestServer(t)
	apply := func(kind, records string) {
		if records == "" {
			return
		}
		p, err := ingest.DecodePayload(strings.NewReader(`{"kind":"` + kind + `","page":1,"pages":1,"records":[` + records + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RouterState.Apply("core", p, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	apply("filter-rule", filterRecords)
	apply("nat-rule", natRecords)
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	return ts
}

// A4-C1 case 1: a rule the endpoint cannot judge is reported, not dropped;
// the plain WAN accept stays the only door.
func TestWANDoorsInterfaceListRuleIsReportedUnjudged(t *testing.T) {
	ts := wanJSONServer(t,
		`{"ordinal":1,"chain":"input","action":"accept","protocol":"tcp","dstPort":8291,"inInterfaceList":"WAN","comment":"winbox"},`+
			`{"ordinal":3,"chain":"input","action":"accept","protocol":"tcp","dstPort":22,"inInterface":"ether1"}`, "")
	dev := getWANDoors(t, ts, "?device=core&wan=ether1").Devices[0]
	if len(dev.Doors) != 1 || dev.Doors[0].Ordinal != 3 {
		t.Fatalf("only the plain WAN accept (#3) is a door, got %+v", dev.Doors)
	}
	if len(dev.Unjudged) != 1 {
		t.Fatalf("the interface-list rule must be reported as unjudged, got %+v", dev.Unjudged)
	}
	u := dev.Unjudged[0]
	if u.Ordinal != 1 || u.Label != "#1" || u.DstPort != "8291" || u.Proto != "tcp" || u.Comment != "winbox" ||
		u.Scope != "in-interface-list=WAN" || u.To != nil {
		t.Fatalf("unjudged entry carries the rule's label/port/proto/comment and verbatim scope, To nil, got %+v", u)
	}

	// Not a door, so no lastSeen is asked for it: the key is absent.
	res, err := http.Get(ts.URL + "/api/doors/internet?device=core&wan=ether1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var raw struct {
		Devices []struct {
			Unjudged []map[string]json.RawMessage `json:"unjudged"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Devices[0].Unjudged) != 1 {
		t.Fatalf("want one raw unjudged entry, got %+v", raw.Devices[0].Unjudged)
	}
	if _, has := raw.Devices[0].Unjudged[0]["lastSeen"]; has {
		t.Fatalf("an unjudged rule is not a door and carries no lastSeen, got %v", raw.Devices[0].Unjudged[0])
	}
}

// A4-C1 case 2.
func TestWANDoorsUnjudgedDstNATCarriesItsHost(t *testing.T) {
	ts := wanJSONServer(t, "",
		`{"ordinal":4,"chain":"dstnat","action":"dst-nat","protocol":"udp","dstPort":53,"inInterfaceList":"WAN","toAddresses":"192.0.2.2"}`)
	dev := getWANDoors(t, ts, "?device=core&wan=ether1").Devices[0]
	if len(dev.Doors) != 0 {
		t.Fatalf("no door expected, got %+v", dev.Doors)
	}
	if len(dev.Unjudged) != 1 || dev.Unjudged[0].To == nil || dev.Unjudged[0].To.IP != "192.0.2.2" ||
		dev.Unjudged[0].Scope != "in-interface-list=WAN" || dev.Unjudged[0].Ordinal != 4 {
		t.Fatalf("a dst-nat rule scoped by a list is unjudged with To set, got %+v", dev.Unjudged)
	}
}

// A4-C1 case 3: scope names the condition verbatim; the list wins.
func TestWANDoorsUnjudgedScopeNamesTheCondition(t *testing.T) {
	ts := wanJSONServer(t,
		`{"ordinal":1,"chain":"input","action":"accept","protocol":"tcp","dstPort":80,"srcAddressList":"LAN"},`+
			`{"ordinal":2,"chain":"input","action":"accept","protocol":"tcp","dstPort":81,"srcAddress":"203.0.113.0/24"},`+
			`{"ordinal":3,"chain":"input","action":"accept","protocol":"tcp","dstPort":82,"srcAddress":"203.0.113.0/24","srcAddressList":"LAN"},`+
			`{"ordinal":4,"chain":"input","action":"accept","protocol":"tcp","dstPort":83,"srcAddress":"203.0.113.0/24","inInterfaceList":"WAN"}`, "")
	dev := getWANDoors(t, ts, "?device=core&wan=ether1").Devices[0]
	want := []string{"src-address-list=LAN", "src-address=203.0.113.0/24", "src-address-list=LAN", "in-interface-list=WAN"}
	if len(dev.Unjudged) != len(want) {
		t.Fatalf("want %d unjudged rules, got %+v", len(want), dev.Unjudged)
	}
	for i, w := range want {
		if dev.Unjudged[i].Ordinal != i+1 || dev.Unjudged[i].Scope != w {
			t.Errorf("unjudged[%d] = %+v, want ordinal %d scope %q", i, dev.Unjudged[i], i+1, w)
		}
	}
}

// A4-C1 case 3, order: filter rules first, then NAT, as Doors.
func TestWANDoorsUnjudgedKeepsFilterThenNATOrder(t *testing.T) {
	ts := wanJSONServer(t,
		`{"ordinal":9,"chain":"input","action":"accept","protocol":"tcp","dstPort":80,"inInterfaceList":"WAN"}`,
		`{"ordinal":2,"chain":"dstnat","action":"dst-nat","protocol":"tcp","dstPort":81,"inInterfaceList":"WAN","toAddresses":"192.0.2.2"}`)
	dev := getWANDoors(t, ts, "?device=core&wan=ether1").Devices[0]
	if len(dev.Unjudged) != 2 || dev.Unjudged[0].Ordinal != 9 || dev.Unjudged[1].Ordinal != 2 {
		t.Fatalf("filter rules come before NAT rules in source order, got %+v", dev.Unjudged)
	}
}

// A4-C1 case 4: a rule failing a known "not a door" test is in neither list.
func TestWANDoorsKnownNotDoorRulesAreNotUnjudged(t *testing.T) {
	ts := wanJSONServer(t,
		`{"ordinal":1,"chain":"input","action":"accept","protocol":"tcp","dstPort":80,"inInterfaceList":"WAN","connectionState":"established,related"},`+
			`{"ordinal":2,"chain":"input","action":"accept","protocol":"tcp","dstPort":81,"inInterfaceList":"WAN","disabled":true},`+
			`{"ordinal":3,"chain":"forward","action":"accept","protocol":"tcp","dstPort":82,"inInterfaceList":"WAN"},`+
			`{"ordinal":4,"chain":"input","action":"accept","protocol":"tcp","dstPort":83,"inInterfaceList":"WAN","inInterface":"ether2"},`+
			`{"ordinal":5,"chain":"input","action":"accept","protocol":"tcp","dstPort":84,"srcAddressList":"LAN","inInterface":"ether2"}`, "")
	dev := getWANDoors(t, ts, "?device=core&wan=ether1").Devices[0]
	if len(dev.Doors) != 0 || len(dev.Unjudged) != 0 {
		t.Fatalf("rules failing a known test belong in neither list, got doors %+v unjudged %+v", dev.Doors, dev.Unjudged)
	}
}

// A4-C1 case 5: unjudged is always an array, never null.
func TestWANDoorsUnjudgedIsAnEmptyArrayNotNull(t *testing.T) {
	cases := map[string]*httptest.Server{
		"no rules": wanJSONServer(t, `{"ordinal":1,"chain":"input","action":"accept","protocol":"tcp","dstPort":22,"inInterface":"ether1"}`, ""),
	}
	s, _ := newTestServer(t)
	s.RouterState = nil
	nilState := httptest.NewServer(s.mux())
	t.Cleanup(nilState.Close)
	cases["no RouterState"] = nilState
	for name, ts := range cases {
		res, err := http.Get(ts.URL + "/api/doors/internet?device=core&wan=ether1")
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Devices []struct {
				Unjudged json.RawMessage `json:"unjudged"`
			} `json:"devices"`
		}
		err = json.NewDecoder(res.Body).Decode(&raw)
		res.Body.Close()
		if err != nil || len(raw.Devices) == 0 {
			t.Fatalf("%s: decode %v, devices %d", name, err, len(raw.Devices))
		}
		if got := string(raw.Devices[0].Unjudged); got != "[]" {
			t.Fatalf("%s: want unjudged to be a literal [], got %s", name, got)
		}
	}
}

func wanSeenServer(t *testing.T, filters []ingest.FilterRule, nats []ingest.NATRule) (*Server, *httptest.Server) {
	t.Helper()
	s, _ := newTestServer(t)
	if err := s.RouterState.Apply("core", ingest.Payload{Kind: ingest.KindFilterRule, Page: 1, Pages: 1, FilterRules: filters}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(nats) > 0 {
		if err := s.RouterState.Apply("core", ingest.Payload{Kind: ingest.KindNATRule, Page: 1, Pages: 1, NATRules: nats}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestWANDoorsAnyPortAndRangeDoorsCarryTheSeenArrivingMark(t *testing.T) {
	s, ts := wanSeenServer(t, []ingest.FilterRule{
		{Ordinal: 1, Chain: "input", Action: "accept", Protocol: "tcp", InInterface: "ether1"},
		{Ordinal: 2, Chain: "input", Action: "accept", Protocol: "udp", DstPort: "5000-5100", InInterface: "ether1"},
		{Ordinal: 3, Chain: "input", Action: "accept", Protocol: "tcp", DstPort: "6000-6100", InInterface: "ether1"},
	}, []ingest.NATRule{
		{Ordinal: 10, Chain: "dstnat", Action: "dst-nat", Protocol: "tcp", DstPort: "5000-5100", InInterface: "ether1", ToAddresses: "192.0.2.5"},
	})
	for _, e := range []store.Event{
		{Protocol: "tcp", SrcIP: "203.0.113.50", DstPort: 443},
		{Protocol: "udp", SrcIP: "203.0.113.51", DstPort: 5050},
		{Protocol: "tcp", SrcIP: "203.0.113.52", DstPort: 5100},
	} {
		e.DeviceID, e.Action, e.Time, e.InInterface, e.DstIP = "core", store.ActionAccept, time.Now(), "ether1", "203.0.113.9"
		s.Store.Insert(e)
	}
	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	doors := got.Devices[0].Doors
	if len(doors) != 4 {
		t.Fatalf("want 4 doors, got %+v", doors)
	}
	by := map[int]wanDoor{}
	for _, d := range doors {
		by[d.Ordinal] = d
	}
	for _, o := range []int{1, 2, 10} {
		if by[o].LastSeen == nil {
			t.Errorf("door #%d: traffic arrived in the window but LastSeen is nil", o)
		}
	}
	if by[3].LastSeen != nil {
		t.Errorf("door #3 (tcp 6000-6100): nothing arrived, LastSeen = %v", by[3].LastSeen)
	}
}

func TestWANDoorsMultiPortDoorCarriesTheNewestArrival(t *testing.T) {
	s, ts := wanSeenServer(t, []ingest.FilterRule{
		{Ordinal: 1, Chain: "input", Action: "accept", Protocol: "tcp", DstPort: "22,8443", InInterface: "ether1"},
	}, nil)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	s.Store.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", ReceivedAt: older, InInterface: "ether1", SrcIP: "203.0.113.50", DstIP: "203.0.113.9", DstPort: 22})
	s.Store.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", ReceivedAt: newer, InInterface: "ether1", SrcIP: "203.0.113.60", DstIP: "203.0.113.9", DstPort: 8443})
	got := getWANDoors(t, ts, "?device=core&wan=ether1")
	doors := got.Devices[0].Doors
	if len(doors) != 1 || doors[0].LastSeen == nil {
		t.Fatalf("want one door with a LastSeen, got %+v", doors)
	}
	if !doors[0].LastSeen.Equal(newer) {
		t.Fatalf("LastSeen = %v, want the newest arrival %v (older was %v)", doors[0].LastSeen, newer, older)
	}
}
