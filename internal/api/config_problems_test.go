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
// Named TestConfigProblemsRefusesNonAdmins until #164. It never tested a
// refusal -- it asserted that an *open* deployment returned 200, which
// was true because callerIsAdminOrOpen treated an anonymous caller as an
// admin while zero accounts existed. The name has been wrong since it was
// written. The refusal it claimed to cover is now
// TestConfigProblemsRefusesACallerWithNoSession, below.
func TestConfigProblemsServesAnAdmin(t *testing.T) {
	s, _ := newTestServer(t)
	s.ConfigProblems = []ConfigProblem{{
		Code: "CFG-0010", Key: "store.retention",
		Message: "-5m0s is not a usable retention window", Applied: "24h0m0s",
	}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config/problems")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an admin got %d, want 200 -- config problems are exactly what the operator needs to see", resp.StatusCode)
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
	ts := httptest.NewServer(asAdmin(s.mux()))
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

// The refusal the test above was named for, now that it can actually be
// expressed: with no session there is no admin, so the endpoint gives up
// nothing. Config problems name config keys, filesystem paths, the OIDC
// issuer and SMTP hosts -- an infrastructure map.
func TestConfigProblemsRefusesACallerWithNoSession(t *testing.T) {
	s, _ := newTestServer(t)
	s.ConfigProblems = []ConfigProblem{{
		Code: "CFG-0010", Key: "store.retention",
		Message: "-5m0s is not a usable retention window", Applied: "24h0m0s",
	}}
	ts := httptest.NewServer(s.mux()) // no admin injected
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config/problems")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("an anonymous caller was served the config problems: %s", body)
	}
}

// #1354: history off with files still on disk is a live entry, worked
// out on every request from the control's own figures -- present while
// that is true, gone the moment the files are deleted or history is
// turned back on.
func TestConfigProblemsReportHistoryHeldWhileOff(t *testing.T) {
	s, _ := newTestServer(t)
	ctl := offWithFilesHistory()
	s.HistoryControl = ctl
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	problems := func() []ConfigProblem {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/config/problems")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got struct {
			Problems []ConfigProblem `json:"problems"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got.Problems
	}

	got := problems()
	if len(got) != 1 {
		t.Fatalf("history off with 27 days on disk gave %d entries, want 1: %+v", len(got), got)
	}
	p := got[0]
	if p.Code != HistoryHeldWhileOffCode || p.Severity != "warn" || p.Key != "history.enabled" {
		t.Errorf("the entry is %+v, want code %s, severity warn, key history.enabled", p, HistoryHeldWhileOffCode)
	}
	for _, want := range []string{"History is off", "27 days", "2026-08-07 → 2026-09-02", "812.0MiB", "Nothing new is kept"} {
		if !strings.Contains(p.Message, want) {
			t.Errorf("the message %q does not say %q", p.Message, want)
		}
	}
	if !strings.Contains(p.Remediation, "same key") || !strings.Contains(p.Remediation, "delete") {
		t.Errorf("the remediation %q does not offer both ways out", p.Remediation)
	}

	// Turned back on: gone.
	if err := ctl.ApplyHistory(true, 30, 1<<30); err != nil {
		t.Fatal(err)
	}
	if got := problems(); len(got) != 0 {
		t.Errorf("with history on again the entry is still reported: %+v", got)
	}

	// Off again, then deleted: gone.
	if err := ctl.ApplyHistory(false, 30, 1<<30); err != nil {
		t.Fatal(err)
	}
	if got := problems(); len(got) != 1 {
		t.Fatalf("off again with the files kept gave %d entries, want 1", len(got))
	}
	if err := ctl.DeleteHistoryFiles(); err != nil {
		t.Fatal(err)
	}
	if got := problems(); len(got) != 0 {
		t.Errorf("with the files deleted the entry is still reported: %+v", got)
	}
}
