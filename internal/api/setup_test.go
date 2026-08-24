// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSetupStatusOpenToViewer pins the viewer-readable settings widening
// (#490) for GET /api/setup/status: a signed-in non-admin now gets 200,
// and a signed-out caller is still refused. There is no write endpoint
// under /api/setup to check alongside it -- handleSetupStatus only ever
// reports observations mikroview made on its own side.
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
