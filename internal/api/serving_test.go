// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func TestHandleServingAnswersHostsSeenAnswering(t *testing.T) {
	s, st := newTestServer(t)
	// tom-desktop reaches out to nas on 445: nas answered, tom-desktop
	// did not -- reaching out is never serving.
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstHostName: "nas", DstPort: 445})
	// A knock the router refused never reached anything: no answer either.
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionDrop, RuleLabel: "default drop", Protocol: "tcp", Time: time.Now(),
		SrcIP: "203.0.113.4", DstIP: "10.0.10.30", DstPort: 3389})

	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/ports/serving")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/ports/serving: %d", res.StatusCode)
	}
	var out servingResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Hosts) != 1 {
		t.Fatalf("want only the host actually seen answering, not the refused knock or the reaching-out end; got %+v", out.Hosts)
	}
	h := out.Hosts[0]
	if h.IP != "10.0.20.5" || h.Name != "nas" {
		t.Fatalf("want nas as the answering host, got %+v", h)
	}
	if len(h.Ports) != 1 || h.Ports[0].Port != 445 || h.Ports[0].Proto != "tcp" {
		t.Fatalf("want 445/tcp on the badge, got %+v", h.Ports)
	}
	if out.WindowSeconds <= 0 {
		t.Fatalf("want the window carried along, same as /api/ports, got %d", out.WindowSeconds)
	}
}

func TestHandleServingEmptyWindow(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/ports/serving")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out servingResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Hosts == nil || len(out.Hosts) != 0 {
		t.Fatalf("an empty window must answer with an empty list, not null or hosts; got %+v", out.Hosts)
	}
}

func TestHandleServingDeviceFilter(t *testing.T) {
	s, st := newTestServer(t)
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		SrcIP: "10.0.10.21", DstIP: "10.0.20.5", DstPort: 445})
	st.Insert(store.Event{DeviceID: "branch", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
		SrcIP: "10.0.10.22", DstIP: "10.0.30.5", DstPort: 445})

	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/api/ports/serving?device=core")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out servingResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Hosts) != 1 || out.Hosts[0].IP != "10.0.20.5" {
		t.Fatalf("want only core's own answering host, got %+v", out.Hosts)
	}
}
