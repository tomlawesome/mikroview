// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestHandleFlagsList(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "20 distinct ports in 60s", time.Now())

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Flags      []flags.Flag           `json:"flags"`
		TimeSeries []flags.FlagTimeBucket `json:"timeSeries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Flags) != 1 || body.Flags[0].Target != "203.0.113.9" || body.Flags[0].Type != flags.TypePortScan {
		t.Errorf("unexpected flags: %+v", body.Flags)
	}

	// Same 60-entry fixed-width-window shape as GET /api/stats's
	// timeSeries (internal/store/ring.go's Stats.TimeSeries), and the one
	// new episode raised above should land in the current minute's
	// bucket.
	if len(body.TimeSeries) != 60 {
		t.Fatalf("expected 60 time series buckets, got %d", len(body.TimeSeries))
	}
	last := body.TimeSeries[len(body.TimeSeries)-1]
	if last.ByType[flags.TypePortScan] != 1 {
		t.Errorf("expected the just-raised port_scan episode in the latest bucket, got %+v", last.ByType)
	}
}

func TestHandleFlagsClear(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypeActivitySpike, "198.51.100.4", "500 events in 60s", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/"+id+"/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Cleared bool `json:"cleared"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Cleared {
		t.Error("expected cleared=true for a known, active flag")
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared {
		t.Errorf("expected the flag to be marked cleared in the store, got %+v", list)
	}
}

func TestHandleFlagsClearUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/does-not-exist/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown ID is a no-op, not an error)", resp.StatusCode)
	}

	var body struct {
		Cleared bool `json:"cleared"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Cleared {
		t.Error("expected cleared=false for an unknown ID")
	}
}

// TestHandleFlagsClearPermanent proves the "Clear and never flag this
// again" endpoint both clears the current episode and durably suppresses
// future raises for the same (Type, Target) -- going through the real
// flags.Store, not a mock, so this also exercises add()'s exclusion
// check end-to-end via the HTTP layer.
func TestHandleFlagsClearPermanent(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "20 distinct ports in 60s", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/"+id+"/clear-permanent", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Cleared  bool `json:"cleared"`
		Excluded bool `json:"excluded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Cleared || !body.Excluded {
		t.Errorf("expected cleared=true, excluded=true for a known active flag, got %+v", body)
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared {
		t.Errorf("expected the flag to be marked cleared in the store, got %+v", list)
	}

	// The actual point: it must never raise again.
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "re-fire attempt", time.Now())
	list = s.Flags.List()
	if len(list) != 1 || list[0].Detail != "20 distinct ports in 60s" {
		t.Errorf("expected the excluded target to stay untouched by a further Add, got %+v", list)
	}
}

func TestHandleFlagsClearPermanentUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/does-not-exist/clear-permanent", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown ID is a no-op, not an error)", resp.StatusCode)
	}

	var body struct {
		Cleared  bool `json:"cleared"`
		Excluded bool `json:"excluded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Cleared || body.Excluded {
		t.Error("expected cleared=false, excluded=false for an unknown ID")
	}
}

// TestHandleExclusionsListAndRemove exercises the admin-only "undo a
// mistake" surface: list what's currently excluded, remove one, and
// confirm removal actually re-enables raising again -- while auth is
// inactive (newTestServer's default, zero users), callerIsAdminOrOpen
// treats every caller as admin-equivalent, same as detector settings.
func TestHandleExclusionsListAndRemove(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Exclude(flags.TypePortScan, "203.0.113.9")
	s.Flags.Exclude(flags.TypeCriticalPort, "198.51.100.4")

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/flags/exclusions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var listBody struct {
		Exclusions []flags.Exclusion `json:"exclusions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Exclusions) != 2 {
		t.Fatalf("expected 2 exclusions, got %+v", listBody.Exclusions)
	}

	var target string
	for _, e := range listBody.Exclusions {
		if e.Type == flags.TypePortScan && e.Target == "203.0.113.9" {
			target = e.ID
		}
	}
	if target == "" {
		t.Fatalf("expected to find the port_scan exclusion in the list, got %+v", listBody.Exclusions)
	}

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/flags/exclusions/"+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", delResp.StatusCode)
	}
	var delBody struct {
		Removed bool `json:"removed"`
	}
	if err := json.NewDecoder(delResp.Body).Decode(&delBody); err != nil {
		t.Fatal(err)
	}
	if !delBody.Removed {
		t.Error("expected removed=true for a known exclusion")
	}

	if s.Flags.Excluded(flags.TypePortScan, "203.0.113.9") {
		t.Error("expected the exclusion to actually be gone from the store")
	}

	// Removing it again is a no-op, not an error.
	delResp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp2.Body.Close()
	var delBody2 struct {
		Removed bool `json:"removed"`
	}
	if err := json.NewDecoder(delResp2.Body).Decode(&delBody2); err != nil {
		t.Fatal(err)
	}
	if delBody2.Removed {
		t.Error("expected removed=false for an already-removed exclusion")
	}
}

// TestHandleExclusionsListRequiresAdminOnceAccountExists mirrors
// TestHandleDetectorSettingsRequiresAdminOnceAccountExists -- the
// exclusions list/remove endpoints are the "undo a mistake" surface for
// a permanent exclusion, and the issue explicitly calls that out as
// admin-only.
func TestHandleExclusionsListRequiresAdminOnceAccountExists(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Exclude(flags.TypePortScan, "203.0.113.9")
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	if _, err := s.Auth.CreateUser("viewer", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected viewer login to succeed, got %d", loginResp.StatusCode)
	}

	resp, err := viewerClient.Get(ts.URL + "/api/flags/exclusions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for a non-admin session on the exclusions list, got %d", resp.StatusCode)
	}

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	adminLogin := postJSON(t, adminClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"})
	adminLogin.Body.Close()
	if adminLogin.StatusCode != http.StatusOK {
		t.Fatalf("expected admin login to succeed, got %d", adminLogin.StatusCode)
	}

	adminResp, err := adminClient.Get(ts.URL + "/api/flags/exclusions")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for an admin session on the exclusions list, got %d", adminResp.StatusCode)
	}
}

// TestHandleFlagsClearPermanentRequiresAdminOnceAccountExists is the
// regression test for the permission gap this endpoint used to have: it
// was open to any authenticated caller, so a plain user-role account --
// or one compromised credential -- could permanently suppress detection
// for a (Type, Target) of their choosing, unlogged. A plain Clear stays
// open (it's reversible; the flag simply raises again), which is the
// distinction the gate is drawn on.
func TestHandleFlagsClearPermanentRequiresAdminOnceAccountExists(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "port scan", time.Now())
	flagID := s.Flags.List()[0].ID
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	if _, err := s.Auth.CreateUser("viewer", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp := postJSON(t, viewerClient, ts.URL+"/api/flags/"+flagID+"/clear-permanent", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for a non-admin clear-permanent, got %d", resp.StatusCode)
	}
	if got := len(s.Flags.ListExclusions()); got != 0 {
		t.Errorf("a rejected non-admin request still created %d exclusion(s); it must have no effect", got)
	}

	// The same caller may still clear the flag the ordinary, reversible
	// way -- the gate is on permanence, not on touching flags at all.
	plainClear := postJSON(t, viewerClient, ts.URL+"/api/flags/"+flagID+"/clear", nil)
	plainClear.Body.Close()
	if plainClear.StatusCode != http.StatusOK {
		t.Errorf("expected a non-admin plain clear to still succeed, got %d", plainClear.StatusCode)
	}
}

// TestHandleFlagsClearPermanentIsAuditLogged pins the other half of the
// fix: now that this action is genuinely admin-gated, it belongs in the
// admin audit trail -- previously it was deliberately excluded on the
// grounds that it wasn't admin-only.
func TestHandleFlagsClearPermanentIsAuditLogged(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "port scan", time.Now())
	flagID := s.Flags.List()[0].ID
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/flags/"+flagID+"/clear-permanent", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected an admin clear-permanent to succeed, got %d", resp.StatusCode)
	}

	var found bool
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "flag.clear_permanent" && e.Target == flagID {
			found = true
			if e.Actor != "admin" {
				t.Errorf("audit entry actor = %q, want admin", e.Actor)
			}
		}
	}
	if !found {
		t.Errorf("expected a flag.clear_permanent audit entry for %s, got: %+v", flagID, s.Audit.Query(audit.Query{}).Entries)
	}
}
