// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// TestSetupStatusOpenToViewer pins the viewer-readable settings widening
// (#490) for GET /api/setup/status: a signed-in non-admin now gets 200,
// and a signed-out caller is still refused. handleSetupStatus only ever
// reports observations mikroview made on its own side; the one write
// endpoint under /api/setup (POST /api/setup/mark, #487) is admin-only
// and pinned separately below.
func TestSetupStatusOpenToViewer(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp, err := viewerClient.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected a viewer to read setup status (#490), got %d", resp.StatusCode)
	}

	anonResp, err := http.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected a signed-out caller to still be refused, got %d", anonResp.StatusCode)
	}
}

// TestSetupMarkRecordsLedgerAndAudit pins the two writes the claim
// ledger's forced-past record depends on (#487): the mark itself, which
// every surface with a silence to explain reads back off
// GET /api/setup/status, and the audit entry, which is where the design
// record sends diagnostics to look and where the line stays as history
// after evidence arrives.
func TestSetupMarkRecordsLedgerAndAudit(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{
		Step: 2, Outcome: "forced", Note: "no router has opened a syslog connection",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/mark = %d, want 200", resp.StatusCode)
	}

	statusResp, err := adminClient.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var got setupStatus
	if err := json.NewDecoder(statusResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Marks) != 1 {
		t.Fatalf("setup status carried %d marks, want 1 -- the ledger is what every empty state reads", len(got.Marks))
	}
	m := got.Marks[0]
	if m.Step != 2 || m.Outcome != setup.MarkForced {
		t.Errorf("mark = step %d %q, want step 2 forced", m.Step, m.Outcome)
	}
	if m.Actor != "admin" {
		t.Errorf("mark actor = %q, want the session's own username -- the body must never be able to sign for somebody else", m.Actor)
	}
	if !strings.Contains(m.Note, "syslog") {
		t.Errorf("mark note = %q, want what was not observed", m.Note)
	}

	entries := s.Audit.Query(audit.Query{})
	var found bool
	for _, e := range entries.Entries {
		if e.Action == "setup.step_forced" && e.Target == "step 2" && e.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Errorf("no setup.step_forced audit entry for step 2 in %+v -- \"visibly recorded where diagnostics can reach it\" is the issue's done-when", entries.Entries)
	}
}

// TestSetupMarkRejectsNonsense keeps the ledger to the five steps the
// ratified design has and the two outcomes it defines. A mark outside
// that is a client bug or a probe; either way it has nothing to
// describe, and must not reach the audit log as though it did.
func TestSetupMarkRejectsNonsense(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)

	for _, tc := range []struct {
		name string
		req  setupMarkRequest
	}{
		{"step zero", setupMarkRequest{Step: 0, Outcome: "skipped"}},
		{"step past the last", setupMarkRequest{Step: 6, Outcome: "skipped"}},
		{"unknown outcome", setupMarkRequest{Step: 1, Outcome: "finished"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", tc.req)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("POST %+v = %d, want 400", tc.req, resp.StatusCode)
			}
		})
	}

	if n := len(s.Setup.Marks()); n != 0 {
		t.Errorf("%d marks recorded from refused requests, want 0", n)
	}
	if n := len(s.Audit.Query(audit.Query{}).Entries); n != 0 {
		t.Errorf("%d audit entries written for refused requests, want 0", n)
	}
}
