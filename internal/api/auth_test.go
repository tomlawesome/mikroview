package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// newAuthTestServer is newTestServer with a fresh, undecided (neither
// disabled nor already holding an account) auth store swapped in --
// newTestServer's own Auth defaults to disabled (matching every non-
// auth-specific test's assumption of a fully open API), so tests that
// actually exercise the register/login/bootstrap flow need this
// pristine state instead.
func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	authStore, err := auth.Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Auth = authStore
	return s
}

// postJSON sends the CSRF header the real frontend always sends (see
// csrfHeaderName) -- a request that deliberately omits it, to test the
// check itself, is built directly with http.NewRequest instead.
func postJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
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

func TestUnprotectedOnceAuthDisabled(t *testing.T) {
	// newTestServer's Auth already defaults to disabled -- see its own
	// doc comment.
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected the API to stay fully open once auth is disabled, got %d", resp.StatusCode)
	}
}

// TestUndecidedStateRestrictsToBootstrapPaths covers the gap the old
// blanket-open zero-account behavior left: before a real decision has
// been made (neither an account created nor auth explicitly skipped),
// live data must not be readable by whoever happens to reach mikroview
// first.
func TestUndecidedStateRestrictsToBootstrapPaths(t *testing.T) {
	s := newAuthTestServer(t) // fresh, undecided -- see its own doc comment
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected /api/events to be blocked while undecided, got %d", resp.StatusCode)
	}
}

func TestUndecidedStateAllowsBootstrapPaths(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	for _, path := range []string{"/api/healthz", "/api/auth/session"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected %s to stay reachable while undecided, got %d", path, resp.StatusCode)
		}
	}
}

func TestAuthSkipDisablesAuthPermanently(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/skip", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected skip to succeed, got %d", resp.StatusCode)
	}

	// Previously-blocked paths are now open, exactly like the disabled
	// default.
	events, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	if events.StatusCode != http.StatusOK {
		t.Errorf("expected /api/events to be open after skipping auth, got %d", events.StatusCode)
	}

	// Registration must not be usable afterward -- re-enabling is
	// CLI-only (see auth.Store.EnableSetup), not something a client can
	// trigger by just calling register directly.
	reg := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	defer reg.Body.Close()
	if reg.StatusCode != http.StatusConflict {
		t.Errorf("expected register to be refused once auth is disabled, got %d", reg.StatusCode)
	}
}

func TestAuthSessionReportsAuthDisabled(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body sessionResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if !body.AuthDisabled {
		t.Errorf("expected authDisabled=true, got %+v", body)
	}
}

func TestAuthSessionReportsSetupRequired(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body sessionResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if !body.SetupRequired || body.Authenticated {
		t.Errorf("expected setupRequired=true, authenticated=false, got %+v", body)
	}
}

func TestRegisterCreatesAdminAndStartsASession(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	resp := postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	sessResp, err := client.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer sessResp.Body.Close()
	var body sessionResponse
	json.NewDecoder(sessResp.Body).Decode(&body)
	if !body.Authenticated || body.Role != string(auth.RoleAdmin) {
		t.Errorf("expected an authenticated admin session after registering, got %+v", body)
	}
}

func TestRegisterClosesAfterFirstUser(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := &http.Client{Jar: mustCookieJar(t)}

	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "second", Password: "password456"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for a second registration attempt, got %d", resp.StatusCode)
	}
}

func TestAPIGatedOnceAUserExists(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for an unauthenticated request once a user exists, got %d", resp.StatusCode)
	}

	// healthz stays open regardless -- Docker's own HEALTHCHECK hits it unauthenticated.
	hz, err := http.Get(ts.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer hz.Body.Close()
	if hz.StatusCode != http.StatusOK {
		t.Errorf("expected /api/healthz to stay open, got %d", hz.StatusCode)
	}
}

func TestLoginThenAccessProtectedRoute(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login to succeed, got %d", loginResp.StatusCode)
	}

	resp, err := client.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected the session cookie to grant access, got %d", resp.StatusCode)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "wrong"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for a wrong password, got %d", resp.StatusCode)
	}
}

func TestLoginRateLimited(t *testing.T) {
	s := newAuthTestServer(t)
	s.LoginLimiter = auth.NewLoginLimiter(2, time.Minute)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	for i := 0; i < 2; i++ {
		postJSON(t, &http.Client{}, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "wrong"}).Body.Close()
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding the rate limit, got %d (even with the correct password)", resp.StatusCode)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	postJSON(t, client, ts.URL+"/api/auth/logout", map[string]any{}).Body.Close()

	resp, err := client.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected the session to no longer work after logout, got %d", resp.StatusCode)
	}
}

func TestAdminCanCreateAdditionalUsers(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, client, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected an admin to be able to create an additional user, got %d", resp.StatusCode)
	}
}

func TestNonAdminCannotCreateUsers(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp := postJSON(t, viewerClient, ts.URL+"/api/auth/users", createUserRequest{Username: "another", Password: "password789", Role: "user"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin to be forbidden from creating users, got %d", resp.StatusCode)
	}
}

func TestPasswordResetInvalidatesExistingSessions(t *testing.T) {
	// Simulates the CLI recovery tool (`-reset-password`) resetting a
	// password from a *separate process* -- it has no handle on the
	// running server's SessionStore, so this has to work purely off the
	// persisted User.PasswordChangedAt field (see Server.sessionUser).
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	// Confirm the session works before the reset.
	pre, err := client.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	pre.Body.Close()
	if pre.StatusCode != http.StatusOK {
		t.Fatalf("expected the session to work before the reset, got %d", pre.StatusCode)
	}

	// Reset the password directly via the store -- exactly what the CLI
	// tool does, bypassing the API/session layer entirely.
	if err := s.Auth.SetPassword("admin", "new-password", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	post, err := client.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected the pre-reset session to be invalidated, got %d", post.StatusCode)
	}
}

func TestMutatingRequestWithoutCSRFHeaderIsRejectedOnceAuthActive(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	// A plain POST with no X-Requested-With header -- what a cross-site
	// <form> submission would look like.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for a mutating request missing the CSRF header, got %d", resp.StatusCode)
	}
}

func TestMutatingRequestWithoutCSRFHeaderAllowedWhileAuthInactive(t *testing.T) {
	// Zero users -- must behave exactly like before this feature existed,
	// including for mutating requests (see requireAuth's doc comment).
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/flags/does-not-exist/clear", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Error("expected mutating requests to work without the CSRF header while auth is inactive")
	}
}

func mustCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}
