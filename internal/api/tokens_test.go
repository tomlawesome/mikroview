// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setUpAdmin registers the first (admin) account on s and returns a
// cookie-jar client already logged in as that admin -- shared setup for
// every token-management test below.
func setUpAdmin(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected registering the admin to succeed, got %d", resp.StatusCode)
	}
	return client
}

func TestTokensCreateRequiresAdmin(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp := postJSON(t, viewerClient, ts.URL+"/api/tokens", createTokenRequest{Name: "birdcage"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin to be forbidden from creating a token, got %d", resp.StatusCode)
	}
}

func TestAdminCanCreateListAndRevokeTokens(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, ts)

	createResp := postJSON(t, client, ts.URL+"/api/tokens", createTokenRequest{Name: "birdcage"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected token creation to succeed, got %d", createResp.StatusCode)
	}
	var created tokenResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Value == "" {
		t.Fatal("expected the creation response to include the raw token value")
	}
	if created.ID == "" || created.Name != "birdcage" {
		t.Errorf("unexpected created token metadata: %+v", created)
	}

	listResp, err := client.Get(ts.URL + "/api/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected listing tokens to succeed, got %d", listResp.StatusCode)
	}
	var listBody struct {
		Tokens []tokenResponse `json:"tokens"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Tokens) != 1 || listBody.Tokens[0].ID != created.ID {
		t.Fatalf("expected exactly the one created token to be listed, got %+v", listBody.Tokens)
	}
	if listBody.Tokens[0].Value != "" {
		t.Error("expected the list response to never include the raw token value")
	}
	rawListBody, _ := json.Marshal(listBody)
	if strings.Contains(string(rawListBody), created.Value) {
		t.Error("expected the list response to never leak the raw token value")
	}

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/tokens/"+created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	revokeResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected revoking the token to succeed, got %d", revokeResp.StatusCode)
	}

	afterResp, err := client.Get(ts.URL + "/api/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer afterResp.Body.Close()
	var afterBody struct {
		Tokens []tokenResponse `json:"tokens"`
	}
	json.NewDecoder(afterResp.Body).Decode(&afterBody)
	if len(afterBody.Tokens) != 0 {
		t.Errorf("expected no tokens listed after revocation, got %+v", afterBody.Tokens)
	}
}

// createToken is a small helper shared by the bearer-auth tests below --
// registers an admin (if not already present via client) and creates one
// token, returning its raw value.
func createToken(t *testing.T, ts *httptest.Server, client *http.Client, name string) string {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/tokens", createTokenRequest{Name: name})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected token creation to succeed, got %d", resp.StatusCode)
	}
	var created tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created.Value
}

func bearerGet(t *testing.T, url, raw string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestBearerTokenGrantsReadOnlyAccess proves the happy path: a valid
// token reaches every one of the four allowed read-only routes with no
// session cookie at all.
func TestBearerTokenGrantsReadOnlyAccess(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	for _, path := range []string{"/api/events", "/api/flags", "/api/stats", "/api/devices"} {
		resp := bearerGet(t, ts.URL+path, raw)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected a valid bearer token to reach %s, got %d", path, resp.StatusCode)
		}
	}
}

// TestBearerTokenCannotReachWriteEndpoint is the critical boundary test:
// a valid, unrevoked read-only token must never be able to clear a
// flag -- proving the read-only subrouter enforces this structurally
// (the route simply isn't registered on it), not by convention.
func TestBearerTokenCannotReachWriteEndpoint(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/flags/does-not-exist/clear", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	// Deliberately also send the CSRF header a real forged request might
	// include -- this must still fail, and must fail because the route
	// isn't reachable at all through the bearer path, not because of a
	// missing CSRF header (bearer requests aren't cookie-based, so CSRF
	// is not the mechanism protecting this boundary).
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a valid read-only bearer token must never be able to reach a write endpoint")
	}
}

// TestBearerTokenCannotReachAdminEndpoints extends the boundary test to
// config/admin surfaces beyond the obvious "clear a flag" write case.
func TestBearerTokenCannotReachAdminEndpoints(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	for _, path := range []string{"/api/detectors", "/api/tokens", "/api/critical-ports"} {
		resp := bearerGet(t, ts.URL+path, raw)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("expected a read-only bearer token to be unable to reach %s, got %d", path, resp.StatusCode)
		}
	}
}

func TestBearerTokenInvalidValueRejected(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	setUpAdmin(t, ts)

	resp := bearerGet(t, ts.URL+"/api/events", "not-a-real-token")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected an invalid bearer token to be rejected with 401, got %d", resp.StatusCode)
	}
}

func TestBearerTokenRevokedRejected(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	// Look the token back up to revoke it via the store directly (same
	// as the DELETE endpoint's effect, already covered by
	// TestAdminCanCreateListAndRevokeTokens).
	tokens := s.Tokens.List()
	if len(tokens) != 1 {
		t.Fatalf("expected exactly one token, got %d", len(tokens))
	}
	if err := s.Tokens.Revoke(tokens[0].ID); err != nil {
		t.Fatal(err)
	}

	resp := bearerGet(t, ts.URL+"/api/events", raw)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected a revoked bearer token to be rejected with 401, got %d", resp.StatusCode)
	}
}
