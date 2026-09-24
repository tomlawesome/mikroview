// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// TestPreferencesGetWithNoRecordReturnsEmptyPrefs is #1283's documented
// default: a user who has never saved anything gets version 1 and an
// empty object, not a 404 -- an empty record is a normal state, not an
// error.
func TestPreferencesGetWithNoRecordReturnsEmptyPrefs(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/me/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got preferencesDocument
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
	if string(got.Prefs) != "{}" {
		t.Errorf("prefs = %s, want {}", got.Prefs)
	}
}

// TestPreferencesPutThenGetRoundTrips is the round trip the API contract
// promises: a PUT's exact prefs object comes back from the next GET.
func TestPreferencesPutThenGetRoundTrips(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	put := putJSON(t, client, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1,
		"prefs":   map[string]any{"colorway": "teal", "altitudeStop": 3},
	})
	put.Body.Close()
	if put.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", put.StatusCode)
	}

	resp, err := client.Get(ts.URL + "/api/me/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got preferencesDocument
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	if err := json.Unmarshal(got.Prefs, &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["colorway"] != "teal" {
		t.Errorf("colorway = %v, want teal", prefs["colorway"])
	}
	if prefs["altitudeStop"] != float64(3) {
		t.Errorf("altitudeStop = %v, want 3", prefs["altitudeStop"])
	}
}

// TestPreferencesPutReplacesTheWholeRecord pins PUT as a replace, not a
// merge -- a second PUT with a different shape must not leave anything
// from the first behind.
func TestPreferencesPutReplacesTheWholeRecord(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	putJSON(t, client, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1, "prefs": map[string]any{"a": 1, "b": 2},
	}).Body.Close()
	putJSON(t, client, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1, "prefs": map[string]any{"c": 3},
	}).Body.Close()

	resp, err := client.Get(ts.URL + "/api/me/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got preferencesDocument
	json.NewDecoder(resp.Body).Decode(&got)
	if strings.Contains(string(got.Prefs), `"a"`) || strings.Contains(string(got.Prefs), `"b"`) {
		t.Errorf("prefs = %s, still carries the first PUT's fields -- a PUT must replace, not merge", got.Prefs)
	}
	if !strings.Contains(string(got.Prefs), `"c"`) {
		t.Errorf("prefs = %s, missing the second PUT's field", got.Prefs)
	}
}

func TestPreferencesPutRefusesWrongVersion(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	resp := putJSON(t, client, ts.URL+"/api/me/preferences", map[string]any{
		"version": 2, "prefs": map[string]any{},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a wrong version", resp.StatusCode)
	}
}

func TestPreferencesPutRefusesNonObjectPrefs(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	for _, body := range []string{
		`{"version":1,"prefs":"not an object"}`,
		`{"version":1,"prefs":[1,2,3]}`,
		`{"version":1,"prefs":42}`,
		`{"version":1,"prefs":null}`,
	} {
		req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/me/preferences", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(csrfHeaderName, csrfHeaderValue)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, resp.StatusCode)
		}
	}
}

// TestPreferencesPutRefusesAnOversizedBody pins the shared 64 KiB cap
// every JSON body on this API is held to (decodeJSONBody).
func TestPreferencesPutRefusesAnOversizedBody(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	huge := `{"version":1,"prefs":{"pad":"` + strings.Repeat("x", 70*1024) + `"}}`
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/me/preferences", bytes.NewReader([]byte(huge)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a body over the 64 KiB cap", resp.StatusCode)
	}
}

// TestPreferencesAreIsolatedPerUser proves user A cannot read user B's
// record: each session's GET must answer only its own account's PUT.
func TestPreferencesAreIsolatedPerUser(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)
	if _, err := s.Auth.CreateUser("operator", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}
	other := loggedInClient(t, ts.URL, "operator", "password456")
	seedFactor(t, s, ts, "operator") // #1253: needed before /api/me/preferences below

	putJSON(t, admin, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1, "prefs": map[string]any{"who": "admin"},
	}).Body.Close()
	putJSON(t, other, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1, "prefs": map[string]any{"who": "operator"},
	}).Body.Close()

	adminResp, err := admin.Get(ts.URL + "/api/me/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	var adminDoc preferencesDocument
	json.NewDecoder(adminResp.Body).Decode(&adminDoc)
	if !strings.Contains(string(adminDoc.Prefs), `"admin"`) {
		t.Errorf("admin's own record = %s, wants its own value, not the other user's", adminDoc.Prefs)
	}

	otherResp, err := other.Get(ts.URL + "/api/me/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer otherResp.Body.Close()
	var otherDoc preferencesDocument
	json.NewDecoder(otherResp.Body).Decode(&otherDoc)
	if !strings.Contains(string(otherDoc.Prefs), `"operator"`) {
		t.Errorf("operator's own record = %s, wants its own value, not the admin's", otherDoc.Prefs)
	}
	if strings.Contains(string(otherDoc.Prefs), `"admin"`) {
		t.Error("operator's GET returned the admin's record")
	}
}

// TestDeletingUserRemovesPreferences is #1283's own ruling: a
// preferences record is cleared when the account is deleted, not left
// behind under an id nothing will ever sign in as again.
func TestDeletingUserRemovesPreferences(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)

	operator, err := s.Auth.CreateUser("operator", "password456", auth.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other := loggedInClient(t, ts.URL, "operator", "password456")
	seedFactor(t, s, ts, "operator") // #1253: needed before /api/me/preferences below
	putJSON(t, other, ts.URL+"/api/me/preferences", map[string]any{
		"version": 1, "prefs": map[string]any{"who": "operator"},
	}).Body.Close()

	if _, ok := s.Prefs.Get(operator.ID); !ok {
		t.Fatal("setup: the operator's preferences were never stored")
	}

	del, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/users/"+operator.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	del.Header.Set(csrfHeaderName, csrfHeaderValue)
	delResp, err := admin.Do(del)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("deleting the user returned %d, want 200", delResp.StatusCode)
	}

	if _, ok := s.Prefs.Get(operator.ID); ok {
		t.Error("the deleted user's preferences record is still stored")
	}
}
