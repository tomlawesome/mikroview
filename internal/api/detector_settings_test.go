// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/detect"
)

// putJSON mirrors postJSON but for PUT, sending the same CSRF header the
// real frontend always sends (see csrfHeaderName).
func putJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHandleDetectorSettingsListDefaultsAllEnabled(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/detectors")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Detectors []detectorEntry `json:"detectors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Detectors) != len(detect.AllDetectorNames) {
		t.Fatalf("expected %d detectors, got %d", len(detect.AllDetectorNames), len(body.Detectors))
	}
	for _, d := range body.Detectors {
		if !d.Enabled {
			t.Errorf("expected %s to default to enabled, got disabled", d.Name)
		}
	}
}

func TestHandleDetectorSettingsUpdateThenListReflectsIt(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := updateDetectorSettingsRequest{
		Enabled: false,
		Scope: detect.Scope{
			Hosts:     []string{"203.0.113.0/24"},
			HostsMode: detect.ListModeDeny,
		},
	}
	resp := putJSON(t, &http.Client{}, ts.URL+"/api/detectors/rule_spike", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got := s.DetectorSettings.Get(detect.DetectorRuleSpike)
	if got.Enabled {
		t.Error("expected rule_spike to now be disabled")
	}
	if len(got.Scope.Hosts) != 1 || got.Scope.Hosts[0] != "203.0.113.0/24" {
		t.Errorf("expected the host scope to be stored, got %+v", got.Scope)
	}
}

func TestHandleDetectorSettingsUpdateUnknownName(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/detectors/not_a_real_detector", updateDetectorSettingsRequest{Enabled: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown detector name, got %d", resp.StatusCode)
	}
}

func TestHandleDetectorSettingsUpdateInvalidMode(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	body := map[string]any{
		"enabled": true,
		"scope":   map[string]any{"hostsMode": "maybe"},
	}
	resp := putJSON(t, &http.Client{}, ts.URL+"/api/detectors/port_scan", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an invalid hostsMode, got %d", resp.StatusCode)
	}
}

func TestHandleDetectorSettingsRequiresAdminOnceAccountExists(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// First account is always admin (auth.Store.Register).
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	// A second, non-admin account, created directly against the store
	// (no HTTP path creates a non-admin account in these tests).
	if _, err := s.Auth.CreateUser("viewer", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected viewer login to succeed, got %d", loginResp.StatusCode)
	}

	resp, err := viewerClient.Get(ts.URL + "/api/detectors")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for a non-admin session, got %d", resp.StatusCode)
	}

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	adminLogin := postJSON(t, adminClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"})
	adminLogin.Body.Close()
	if adminLogin.StatusCode != http.StatusOK {
		t.Fatalf("expected admin login to succeed, got %d", adminLogin.StatusCode)
	}

	adminResp, err := adminClient.Get(ts.URL + "/api/detectors")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for an admin session, got %d", adminResp.StatusCode)
	}
}
