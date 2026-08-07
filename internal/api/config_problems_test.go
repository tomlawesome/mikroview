// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestConfigProblemsRefusesNonAdmins pins the gate on the server side.
// The frontend hiding a banner is not a control -- the payload would sit
// in devtools regardless -- so the data must not leave the server.
func TestConfigProblemsRefusesNonAdmins(t *testing.T) {
	s, _ := newTestServer(t)
	s.ConfigProblems = []ConfigProblem{{
		Code: "CFG-0010", Key: "store.retention",
		Message: "-5m0s is not a usable retention window", Applied: "24h0m0s",
	}}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// newTestServer's auth store is disabled, so callerIsAdminOrOpen is
	// true and an admin-equivalent caller sees the data.
	resp, err := http.Get(ts.URL + "/api/config/problems")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open deployment got %d, want 200 -- on a fresh install there is no admin to be, "+
			"and the operator in front of it is exactly who needs to see this", resp.StatusCode)
	}
	var got struct {
		Problems []ConfigProblem `json:"problems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(got.Problems))
	}
	if got.Problems[0].Applied == "" {
		t.Error("Applied is empty -- clamping is only defensible if the substitution is reported")
	}
}

// TestConfigProblemsNeverEchoesSecrets is the end-to-end half of
// internal/config's canary test: even if a future rule started quoting a
// value, this asserts it doesn't reach the wire.
func TestConfigProblemsNeverEchoesSecrets(t *testing.T) {
	const canary = "CANARY-d34db33f"

	s, _ := newTestServer(t)
	s.ConfigProblems = []ConfigProblem{
		{Code: "CFG-9999", Key: "oidc.clientSecret", Message: "is too short", Remediation: "use a longer value"},
	}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config/problems")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, canary) {
		t.Errorf("a secret value reached the response body: %s", body)
	}
	// The key name is expected -- that's how the operator finds the
	// setting. The *value* is what must never appear.
	if !strings.Contains(body, "oidc.clientSecret") {
		t.Error("the config key was not reported, so the operator can't find the setting")
	}
}
