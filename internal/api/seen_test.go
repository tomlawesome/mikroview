// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/seen"
)

type seenResponse struct {
	Fields map[string][]seen.Value `json:"fields"`
}

func getSeenValues(t *testing.T, h http.Handler) (int, seenResponse) {
	t.Helper()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/seen-values")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body seenResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

func TestHandleSeenValues(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now()
	s.SeenValues.Observe("tcp", "ether1", "bridge-lan", now)
	s.SeenValues.Observe("udp", "ether1", "", now.Add(2*time.Minute))

	status, body := getSeenValues(t, asAdmin(s.mux()))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	protos := body.Fields["proto"]
	if len(protos) != 2 {
		t.Fatalf("proto = %v, want both protocols", protos)
	}
	// Most recently seen first, so the menu's top is what the network is
	// doing now.
	if protos[0].Value != "udp" {
		t.Errorf("proto[0] = %q, want the most recently seen first", protos[0].Value)
	}
	if protos[0].FirstSeen.IsZero() || protos[0].LastSeen.IsZero() {
		t.Errorf("proto[0] = %+v, want both seen times served", protos[0])
	}

	ifaces := body.Fields["interface"]
	if len(ifaces) != 2 {
		t.Errorf("interface = %v, want both interface names in one list", ifaces)
	}
}

// A viewer needs this list to set a filter at all -- refusing it would
// leave them with the free-text box #1226 exists to replace.
func TestHandleSeenValuesIsOpenToAViewer(t *testing.T) {
	s, _ := newTestServer(t)
	s.SeenValues.Observe("icmp", "", "", time.Now())

	status, body := getSeenValues(t, asViewer(s.mux()))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a viewer", status)
	}
	if len(body.Fields["proto"]) != 1 {
		t.Errorf("proto = %v, want the list served to a viewer", body.Fields["proto"])
	}
}

// Every field is present whether or not anything has been seen, so the
// frontend never has to tell "no values yet" apart from "no such field".
func TestHandleSeenValuesAlwaysCarriesEveryField(t *testing.T) {
	s, _ := newTestServer(t)

	status, body := getSeenValues(t, asAdmin(s.mux()))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, field := range []string{"proto", "interface"} {
		values, ok := body.Fields[field]
		if !ok {
			t.Errorf("response is missing %q", field)
			continue
		}
		if values == nil {
			t.Errorf("%q is null, want an empty list", field)
		}
	}
}

// The interface list is a partial inventory of the operator's network
// shape, so it stays off the bearer-token mux for the same reason
// GET /api/hosts does.
func TestSeenValuesIsNotReachableWithAReadOnlyToken(t *testing.T) {
	s, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	s.readOnlyRoutes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/seen-values", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 -- a read-only API token must not reach this list", rr.Code)
	}
}
