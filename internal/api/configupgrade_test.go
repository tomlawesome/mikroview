// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestConfigUpgradeAdminOnly pins #1218's gate: unlike GET /api/setup/status
// (widened to any signed-in caller, #490), there is no read-only wizard
// for this notice to sit beside, so the route stays admin-only, same as
// POST /api/setup/mark and /api/setup/address.
func TestConfigUpgradeAdminOnly(t *testing.T) {
	s := newAuthTestServer(t)
	s.Version = "v1.2.3"
	s.ConfigUpgradeSettings = []config.MissingSetting{{Key: "geoip", Block: "# geoip:\n#   dbPath: \"\""}}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()
	totpEnrolAndConfirm(t, viewerClient, ts) // #1253: needed before /api/config/upgrade below

	viewerResp, err := viewerClient.Get(ts.URL + "/api/config/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	viewerResp.Body.Close()
	if viewerResp.StatusCode != http.StatusForbidden {
		t.Errorf("GET /api/config/upgrade from a viewer = %d, want 403", viewerResp.StatusCode)
	}

	anonResp, err := http.Get(ts.URL + "/api/config/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/config/upgrade signed out = %d, want 401", anonResp.StatusCode)
	}

	adminResp, err := adminClient.Get(ts.URL + "/api/config/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/config/upgrade from an admin = %d, want 200", adminResp.StatusCode)
	}
	var got configUpgradeResponse
	if err := json.NewDecoder(adminResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", got.Version)
	}
	if len(got.Settings) != 1 || got.Settings[0].Key != "geoip" {
		t.Errorf("Settings = %+v, want one entry for geoip", got.Settings)
	}
}
