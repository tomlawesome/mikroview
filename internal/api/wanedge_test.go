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
