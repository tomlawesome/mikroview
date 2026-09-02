// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/entities"
)

// fetchAudit is a small test helper: GET /api/audit via client, decoded
// into an audit.Result.
func fetchAudit(t *testing.T, client *http.Client, ts *httptest.Server) audit.Result {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from GET /api/audit, got %d", resp.StatusCode)
	}
	var res audit.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

// TestAuditRequiresAdminWithoutASession mirrors
// TestEntitiesRequireAdminWithoutASession -- GET /api/audit uses the
// same strict callerIsAdmin gate, so it stays forbidden to an anonymous
// caller even once the request has reached the handler.
func TestAuditRequiresAdminWithoutASession(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected GET /api/audit to require an admin even without a session, got %d", resp.StatusCode)
	}
}

func TestAuditRequiresAdminNotJustAnyUser(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := registerAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp, err := viewerClient.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin GET /api/audit to be forbidden, got %d", resp.StatusCode)
	}
}

// TestCreateUserRecordsAuditEntry proves handleAuthCreateUser -- gated by
// callerIsAdmin -- actually calls s.Audit.Record, not just that the
// handler still creates the user.
func TestCreateUserRecordsAuditEntry(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, ts)

	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	res := fetchAudit(t, admin, ts)
	var found *audit.Entry
	for i, e := range res.Entries {
		if e.Action == "user.create" && e.Target == "viewer" {
			found = &res.Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a user.create audit entry for 'viewer', got %+v", res.Entries)
	}
	if found.Actor != "admin" {
		t.Errorf("expected the audit entry's actor to be the admin who created the account, got %q", found.Actor)
	}
	if found.Detail != "role=user" {
		t.Errorf("expected the audit entry's detail to record the assigned role, got %q", found.Detail)
	}
}

// TestEntityUpsertAndDeleteRecordAuditEntries covers both
// handleEntitiesUpsert and handleEntitiesDelete.
func TestEntityUpsertAndDeleteRecordAuditEntries(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, ts)

	postJSON(t, admin, ts.URL+"/api/entities", entityRequest{
		Type: entities.TypeHost, Key: "192.168.1.50", Label: "mail relay",
	}).Body.Close()

	res := fetchAudit(t, admin, ts)
	var upsertFound bool
	for _, e := range res.Entries {
		if e.Action == "entity.upsert" && e.Target == "host:192.168.1.50" {
			upsertFound = true
			if e.Detail != "label=mail relay" {
				t.Errorf("expected the upsert entry's detail to record the label, got %q", e.Detail)
			}
		}
	}
	if !upsertFound {
		t.Fatalf("expected an entity.upsert audit entry, got %+v", res.Entries)
	}

	deleteJSON(t, admin, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "192.168.1.50"}).Body.Close()

	res2 := fetchAudit(t, admin, ts)
	var deleteFound bool
	for _, e := range res2.Entries {
		if e.Action == "entity.delete" && e.Target == "host:192.168.1.50" {
			deleteFound = true
		}
	}
	if !deleteFound {
		t.Fatalf("expected an entity.delete audit entry, got %+v", res2.Entries)
	}

	// Deleting an unknown entity is a 404 no-op -- must NOT record an
	// audit entry, since nothing was actually deleted.
	deleteJSON(t, admin, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "nonexistent"}).Body.Close()
	res3 := fetchAudit(t, admin, ts)
	deleteCount := 0
	for _, e := range res3.Entries {
		if e.Action == "entity.delete" {
			deleteCount++
		}
	}
	if deleteCount != 1 {
		t.Errorf("expected a no-op delete on an unknown entity to not add a new audit entry, got %d entity.delete entries", deleteCount)
	}
}

// TestTokenCreateAndRevokeRecordAuditEntries covers handleTokensCreate
// and handleTokensRevoke.
func TestTokenCreateAndRevokeRecordAuditEntries(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, ts)

	createResp := postJSON(t, admin, ts.URL+"/api/tokens", createTokenRequest{Name: "birdcage"})
	var created tokenResponse
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	res := fetchAudit(t, admin, ts)
	var createFound bool
	for _, e := range res.Entries {
		if e.Action == "token.create" && e.Target == "birdcage" {
			createFound = true
		}
	}
	if !createFound {
		t.Fatalf("expected a token.create audit entry, got %+v", res.Entries)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/tokens/"+created.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	revokeResp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	revokeResp.Body.Close()

	res2 := fetchAudit(t, admin, ts)
	var revokeFound bool
	for _, e := range res2.Entries {
		if e.Action == "token.revoke" && e.Target == created.ID {
			revokeFound = true
		}
	}
	if !revokeFound {
		t.Fatalf("expected a token.revoke audit entry, got %+v", res2.Entries)
	}
}

// TestClearAllDoesNotRecordAPerFlagAuditEntry documents what the audit
// log carries for flags after #640: the per-flag judgement writes one
// flag.verdict entry (see TestHandleFlagsVerdictIsAuditLogged), and the
// bulk clear writes one flag.clear_all entry for the whole call -- never
// one per flag.
func TestClearAllDoesNotRecordAPerFlagAuditEntry(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add("port_scan", "1.2.3.4", "d1", time.Now())
	s.Flags.Add("port_scan", "1.2.3.5", "d2", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, ts)

	postFlagsAction(t, admin, ts.URL+"/api/flags/clear-all")

	res := fetchAudit(t, admin, ts)
	var clearAlls int
	for _, e := range res.Entries {
		if e.Action == "flag.clear_all" {
			clearAlls++
		}
		if e.Target == "port_scan:1.2.3.4" {
			t.Errorf("expected no per-flag audit entry from a bulk clear, got %+v", e)
		}
	}
	if clearAlls != 1 {
		t.Errorf("expected exactly 1 flag.clear_all entry, got %d", clearAlls)
	}
}

// TestShippedDefinitionUpdateRecordsAuditEntry covers
// handleDefinitionsUpdate, gated by callerIsAdmin -- the #407 successor
// to handleDetectorSettingsUpdate, whose action string this test used to
// pin as "detector.update". Every definition update now records under
// one action, "definition.update", whichever kind of definition it
// touches (see definitionAuditDetail).
func TestShippedDefinitionUpdateRecordsAuditEntry(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, ts)

	resp := putJSON(t, admin, ts.URL+"/api/definitions/port_scan", updateDefinitionRequest{Enabled: boolPtr(false)})
	resp.Body.Close()

	res := fetchAudit(t, admin, ts)
	var found bool
	for _, e := range res.Entries {
		if e.Action == "definition.update" && e.Target == "port_scan" {
			found = true
			if e.Detail != "enabled=false" {
				t.Errorf("expected the definition.update entry's detail to record the new enabled state, got %q", e.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("expected a definition.update audit entry, got %+v", res.Entries)
	}
}

// postFlagsAction sends a bodyless, CSRF-headered POST -- the shape both
// handleFlagsClear and handleFlagsClearPermanent expect (neither reads a
// request body).
func postFlagsAction(t *testing.T, client *http.Client, url string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
