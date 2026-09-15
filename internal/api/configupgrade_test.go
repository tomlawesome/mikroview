// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/configdrift"
)

// TestConfigUpgradeAdminOnly pins #1218's gate: unlike GET /api/setup/status
// (widened to any signed-in caller, #490), there is no read-only wizard
// for this notice to sit beside, so both routes stay admin-only, same as
// POST /api/setup/mark and /api/setup/address.
func TestConfigUpgradeAdminOnly(t *testing.T) {
	s := newAuthTestServer(t)
	s.Version = "v1.2.3"
	s.ConfigUpgradeSettings = []config.MissingSetting{{Key: "geoip", Block: "# geoip:\n#   dbPath: \"\""}}
	s.ConfigDrift = configdrift.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

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
	if got.Dismissed {
		t.Error("Dismissed should be false before anything is dismissed")
	}
}

// TestConfigUpgradeDismissPersistsAndAudits pins the write side: a
// dismissal shows up on the next GET, and is written to the audit log
// the same way every other admin-privileged mutation is (#1218).
func TestConfigUpgradeDismissPersistsAndAudits(t *testing.T) {
	s := newAuthTestServer(t)
	s.Version = "v1.2.3"
	s.ConfigDrift = configdrift.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/config/upgrade/dismiss", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/config/upgrade/dismiss = %d, want 200", resp.StatusCode)
	}
	var got configUpgradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Dismissed {
		t.Error("expected Dismissed to be true in the dismiss response itself")
	}

	statusResp, err := adminClient.Get(ts.URL + "/api/config/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var after configUpgradeResponse
	if err := json.NewDecoder(statusResp.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Dismissed {
		t.Error("expected the dismissal to still read back as true on a later GET")
	}

	entries := s.Audit.Query(audit.Query{}).Entries
	found := false
	for _, e := range entries {
		if e.Action == "config.upgrade_dismissed" && e.Target == "v1.2.3" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an audit entry for config.upgrade_dismissed against v1.2.3, got %+v", entries)
	}
}

func TestConfigUpgradeDismissRequiresAdmin(t *testing.T) {
	s := newAuthTestServer(t)
	s.Version = "v1.2.3"
	s.ConfigDrift = configdrift.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp := postJSON(t, viewerClient, ts.URL+"/api/config/upgrade/dismiss", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/config/upgrade/dismiss from a viewer = %d, want 403", resp.StatusCode)
	}
}
