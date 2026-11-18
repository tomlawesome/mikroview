// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func getNetworks(t *testing.T, s *Server) (int, []byte) {
	t.Helper()
	ts := httptest.NewServer(s.mux())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/networks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// Assumed API: Server.HomeNetworks []string, the configured networks.home
// CIDRs, served verbatim.
func TestGetNetworksReturnsTheConfiguredHomeRanges(t *testing.T) {
	s, _ := newTestServer(t)
	s.HomeNetworks = []string{"198.51.100.0/24", "192.0.2.32/27"}
	code, body := getNetworks(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET /api/networks = %d (%s), want 200", code, body)
	}
	var got struct {
		Home []string `json:"home"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if !reflect.DeepEqual(got.Home, s.HomeNetworks) {
		t.Errorf("home = %v, want %v", got.Home, s.HomeNetworks)
	}
}

func TestGetNetworksReturnsAnEmptyArrayNotNullWhenUnset(t *testing.T) {
	s, _ := newTestServer(t)
	code, body := getNetworks(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET /api/networks = %d (%s), want 200", code, body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["home"]) != "[]" {
		t.Errorf("home = %s, want [] (the frontend iterates it)", raw["home"])
	}
}
