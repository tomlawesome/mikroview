package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Only exercises the offline-safe path: private/invalid IPs are rejected
// before any outbound reputation.Client network call, so this doesn't
// depend on internet access being available in the test environment.
// The success path (real Shodan/AbuseIPDB responses) isn't covered here.
func TestHandleIPLookupRejectsNonPublicIP(t *testing.T) {
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	for _, ip := range []string{"192.168.1.1", "10.0.0.5", "127.0.0.1", "not-an-ip"} {
		resp, err := http.Get(ts.URL + "/api/lookup/ip/" + ip)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("lookup %q: status = %d, want 400", ip, resp.StatusCode)
		}
	}
}
