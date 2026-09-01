// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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

// internal/auth's sentinel errors are written for a developer (e.g.
// ErrNotPersisted's "refusing to create a user that would not survive
// a restart"), not a stranger looking at a login screen -- this proves
// that text never reaches the HTTP response body, regardless of which
// auth error is triggered.
func TestAuthErrorsNeverLeakInternalErrorText(t *testing.T) {
	s, _ := newTestServer(t)
	unpersisted, err := auth.Open("") // triggers ErrNotPersisted on Register
	if err != nil {
		t.Fatal(err)
	}
	s.Auth = unpersisted
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "auth:") {
		t.Errorf("expected internal/auth's raw error text never to reach the client, got body: %q", body)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for ErrNotPersisted, got %d", resp.StatusCode)
	}
}

// A request body decoded with no size cap means memory allocation
// proportional to body size, on an endpoint reachable with no
// credentials at all (login) -- this proves an oversized body is
// rejected rather than read in full.
func TestOversizedJSONBodyIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	oversized := strings.Repeat("a", maxJSONBodyBytes+1)
	body := `{"username":"` + oversized + `","password":"x"}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected an oversized body to be rejected with 400, got %d", resp.StatusCode)
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

// A bare cross-site <form method=POST> (or a JSON-CSRF fetch) can't set
// a custom header, so a request missing csrfHeaderName during the
// undecided (Count()==0) bootstrap window must be rejected for the
// endpoint that makes an irreversible, deployment-wide choice --
// otherwise a tricked victim's browser could plant an attacker-
// controlled admin account with no direct network access required from
// the attacker at all.
func TestBootstrapMutatingEndpointsRequireCSRFHeader(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body any
	}{
		{"register", "/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthTestServer(t)
			ts := httptest.NewServer(s.Routes())
			defer ts.Close()

			b, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPost, ts.URL+tc.path, bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			// Deliberately no csrfHeaderName -- this is the request
			// shape a cross-site form/fetch can actually produce.
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("expected %s without the CSRF header to be rejected with 403 during the bootstrap window, got %d", tc.path, resp.StatusCode)
			}
			if s.Auth.Count() != 0 {
				t.Error("the forged request must not have taken effect")
			}
		})
	}
}

func TestAuthSessionReportsSetupRequired(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
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

// TestAuthSessionReportsSignedInSince covers #677's sessions row ("this
// device ... signed in 4 d"): the field must be present and parseable
// once authenticated, and absent while not.
func TestAuthSessionReportsSignedInSince(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp, err := client.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Authenticated {
		t.Fatalf("expected an authenticated session, got %+v", body)
	}
	since, err := time.Parse(time.RFC3339, body.SignedInSince)
	if err != nil {
		t.Fatalf("signedInSince %q did not parse as RFC3339: %v", body.SignedInSince, err)
	}
	if time.Since(since) > time.Minute {
		t.Errorf("expected signedInSince to be about now (just logged in), got %v", since)
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

// TestLogoutAllRejectsAnonymousCaller is #677's negative case for the
// new self-serve "sign out everywhere" route: with no session cookie at
// all there is nothing to revoke, and the endpoint must refuse rather
// than silently no-op the way plain logout does (see
// authzMatrix's accessViewer row for why -- unlike /api/auth/logout,
// this one performs a real account-scoped action and needs a caller to
// act on).
func TestLogoutAllRejectsAnonymousCaller(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/logout-all", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected an anonymous caller to be refused, got %d", resp.StatusCode)
	}
}

// TestLogoutAllEndsEverySessionButTheCallers is the positive case: two
// devices signed in as the same user, "sign out everywhere" called from
// one of them, and the result checked from both -- the caller that made
// the call keeps working (issued a fresh session, #677's requirement
// that the tab you clicked from doesn't itself get logged out), while
// the other device's session is dead.
func TestLogoutAllEndsEverySessionButTheCallers(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	deviceA := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, deviceA, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	deviceB := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, deviceB, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	callResp := postJSON(t, deviceA, ts.URL+"/api/auth/logout-all", map[string]any{})
	callResp.Body.Close()
	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("expected sign-out-everywhere to succeed, got %d", callResp.StatusCode)
	}

	aResp, err := deviceA.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer aResp.Body.Close()
	if aResp.StatusCode != http.StatusOK {
		t.Errorf("expected the calling device's session to keep working (it was reissued a fresh one), got %d", aResp.StatusCode)
	}

	bResp, err := deviceB.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer bResp.Body.Close()
	if bResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected the other device's session to be revoked, got %d", bResp.StatusCode)
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

// TestAdminCanCreateViewerAndSessionReportsIt pins two things #653 adds:
// handleAuthCreateUser accepts role "viewer", and GET /api/auth/session
// -- which already reported "admin"/"user" -- reports "viewer" for such
// an account correctly too, since sessionResponse.Role is just
// string(user.Role) with no role-specific casing to miss.
func TestAdminCanCreateViewerAndSessionReportsIt(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "watcher", Password: "password456", Role: "viewer"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected an admin to be able to create a viewer, got %d", resp.StatusCode)
	}

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "watcher", Password: "password456"}).Body.Close()

	sessResp, err := viewerClient.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer sessResp.Body.Close()
	var body sessionResponse
	json.NewDecoder(sessResp.Body).Decode(&body)
	if !body.Authenticated || body.Role != string(auth.RoleViewer) {
		t.Errorf("expected an authenticated viewer session reporting role %q, got %+v", auth.RoleViewer, body)
	}
}

// TestCreateUserDefaultsToUserRole pins handleAuthCreateUser's
// pre-#653 default (an empty role means "user"), preserved so an
// unmodified admin UI/script that never sends a role field keeps
// creating the same accounts it always did.
func TestCreateUserDefaultsToUserRole(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected creation with no role field to succeed, got %d", resp.StatusCode)
	}

	u, ok := s.Auth.ByUsername("operator")
	if !ok {
		t.Fatal("expected the account to exist")
	}
	if u.Role != auth.RoleUser {
		t.Errorf("expected an empty role field to default to RoleUser, got %v", u.Role)
	}
}

// TestCreateUserRejectsUnrecognizedRole pins the 400 for a role this
// deployment doesn't have -- "admin" is refused separately (see
// TestAdminCannotCreateASecondAdmin), so this covers what's left:
// anything that isn't "", "user" or "viewer".
func TestCreateUserRejectsUnrecognizedRole(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "someone", Password: "password456", Role: "owner"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an unrecognized role, got %d", resp.StatusCode)
	}
	if _, ok := s.Auth.ByUsername("someone"); ok {
		t.Error("expected the account to not have been created")
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
	// Simulates the CLI recovery tool (`-recover-admin-account`) resetting a
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
	//
	// s.mux() is the raw handler set with requireAuth (and its CSRF
	// check) deliberately out of the picture -- that is what "no CSRF
	// header" means to test here. But handleFlagsClear has its own gate
	// now (#653: user tier, not admin), unrelated to CSRF, which a nil
	// caller would trip and turn into an unrelated 403 -- so this needs
	// asUser to supply the identity requireAuth would have, the same way
	// every admin-gated handler's direct-mux test already uses asAdmin.
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
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

func TestAdminCannotCreateASecondAdmin(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, client, ts.URL+"/api/auth/users", createUserRequest{Username: "second", Password: "password456", Role: "admin"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an admin-role request, got %d", resp.StatusCode)
	}
	// The account must not exist at all. A silent downgrade to RoleUser
	// would leave an account the caller never asked for, under a name
	// they chose for an admin.
	if _, ok := s.Auth.ByUsername("second"); ok {
		t.Error("a refused admin-role request created the account anyway")
	}
}

func TestUserListIsAdminOnly(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	// 403, not an empty list: who holds an account and which one is the
	// admin is exactly what an attacker wants in order to pick a target.
	resp, err := viewerClient.Get(ts.URL + "/api/auth/users")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-admin, got %d", resp.StatusCode)
	}

	adminResp, err := adminClient.Get(ts.URL + "/api/auth/users")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	var users []userSummary
	if err := json.NewDecoder(adminResp.Body).Decode(&users); err != nil {
		t.Fatalf("decoding the user list: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(users))
	}
	// The response type has no password-hash field at all, but assert on
	// the raw shape too so a future switch to serializing auth.User
	// directly is caught here.
	for _, u := range users {
		if u.ID == "" || u.Username == "" {
			t.Errorf("incomplete user summary: %+v", u)
		}
	}
}

func TestDeletingAUserRevokesTheirSessionAndTokens(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewer, ok := s.Auth.ByUsername("viewer")
	if !ok {
		t.Fatal("the account under test was not created")
	}

	// A token attributed to the account being deleted. Created directly
	// rather than through the API, since only an admin may mint one --
	// this stands in for a token issued while that account still held
	// admin, before a transfer.
	rawToken, _, err := s.Tokens.Create("viewers-token", auth.TokenKindAPI, "", viewer, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()
	live, err := viewerClient.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	live.Body.Close()
	if live.StatusCode != http.StatusOK {
		t.Fatalf("the session under test does not work before the delete: %d", live.StatusCode)
	}
	viewerSessionID := sessionIDFromJar(t, viewerClient, ts.URL)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/users/"+viewer.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the delete, got %d", resp.StatusCode)
	}

	if _, ok := s.Auth.ByUsername("viewer"); ok {
		t.Error("the account still exists after a successful delete")
	}

	after, err := viewerClient.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("a deleted account's session still works: got %d, want 401", after.StatusCode)
	}

	// The 401 above would happen anyway -- sessionUser fails to resolve
	// an account that no longer exists. So assert on the session record
	// itself, which is what the explicit revocation is for: it must not
	// be left sitting in the store, valid, waiting on that one lookup to
	// keep failing.
	if _, ok := s.Sessions.Validate(viewerSessionID, time.Now()); ok {
		t.Error("the deleted account's session is still live in the session store")
	}

	if _, ok := s.Tokens.Authenticate(rawToken, auth.TokenKindAPI, time.Now()); ok {
		t.Error("a deleted account's API token still authenticates")
	}
}

func TestDeletingTheAdminIsRefused(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	admin, _ := s.Auth.ByUsername("admin")

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/users/"+admin.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 when deleting the admin, got %d", resp.StatusCode)
	}

	// And the admin must still be able to act -- a refused delete that
	// revoked the session anyway would be its own lockout.
	check, err := adminClient.Get(ts.URL + "/api/auth/users")
	if err != nil {
		t.Fatal(err)
	}
	check.Body.Close()
	if check.StatusCode != http.StatusOK {
		t.Errorf("the admin lost access after a refused self-delete: %d", check.StatusCode)
	}
}

// sessionIDFromJar pulls the session cookie's value out of a client's
// jar, so a test can assert on the server-side session record directly
// rather than only on what a request happens to return.
func sessionIDFromJar(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == sessionCookieName {
			return c.Value
		}
	}
	t.Fatal("no session cookie in the jar")
	return ""
}

// TestIngestTokenCannotReachReadOnlyRoutes is the HTTP-level half of the
// kind separation asserted in internal/auth. The store refusing a
// wrong-kind Authenticate is necessary but not sufficient: what matters
// operationally is that a real request carrying an ingest token in an
// Authorization header does not come back with data.
//
// The direction matters. An ingest token lives in a script on a router
// where, per #186's Step 0, any RouterOS user holding `read` can print
// it. If that value reached readOnlyRoutes it would turn into a
// read-everything credential for every event, flag, stat and device
// mikroview holds -- a far larger prize than the router it came from.
//
// Expects 404, not 401: an ingest token authenticates successfully (it
// is a valid token, just not this kind of route) and dispatches to
// ingestRoutes -- its own mux with only POST /api/ingest/routeros
// registered, see requireAuth's bearer-token branch. These paths simply
// aren't on that mux, the same reason a valid read-only API token
// hitting a write route like POST /api/tokens also 404s rather than
// 401s (readOnlyRoutes doesn't register it either) -- a structural "no
// route", not an authentication failure.
func TestIngestTokenCannotReachReadOnlyRoutes(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	admin, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("the admin account was not created")
	}

	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create ingest: %v", err)
	}
	apiRaw, _, err := s.Tokens.Create("birdcage", auth.TokenKindAPI, "", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create api: %v", err)
	}

	get := func(t *testing.T, path, token string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Every route a read-only token can reach, not just one: a single
	// spot check is how the endpoint nobody remembered ships reachable
	// -- the same reasoning authzMatrix exists for.
	for _, path := range []string{"/api/events", "/api/flags", "/api/stats", "/api/devices"} {
		if got := get(t, path, ingestRaw); got != http.StatusNotFound {
			t.Errorf("GET %s with an ingest token: got %d, want 404 -- ingestRoutes doesn't register this path", path, got)
		}
		if got := get(t, path, apiRaw); got != http.StatusOK {
			t.Errorf("GET %s with a read-only API token: got %d, want 200 -- the check above must be about kind, not a broken bearer path", path, got)
		}
	}
}

// TestCreateTokenRejectsAnUnscopedIngestToken checks the scope rule is
// enforced at the HTTP boundary too, and that the caller is told which
// field is wrong rather than being handed a generic 500.
func TestCreateTokenRejectsAnUnscopedIngestToken(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	cases := []struct {
		name string
		body createTokenRequest
		want int
	}{
		{"ingest with no device", createTokenRequest{Name: "t", Kind: "ingest"}, http.StatusBadRequest},
		{"api with a device", createTokenRequest{Name: "t", Kind: "api", Device: "router-1"}, http.StatusBadRequest},
		{"unknown kind", createTokenRequest{Name: "t", Kind: "admin"}, http.StatusBadRequest},
		{"ingest with a device", createTokenRequest{Name: "t", Kind: "ingest", Device: "router-1"}, http.StatusCreated},
		{"kind omitted defaults to the read-only one", createTokenRequest{Name: "t"}, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, adminClient, ts.URL+"/api/tokens", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("POST /api/tokens: got %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// Changing your own password (#294 item 4). Before this there was no
// route at all: it meant -recover-admin-account, which needs host
// access, so an operator who suspected their credential was known could
// do nothing from the interface.
func TestChangePasswordRotatesTheSessionAndEndsOthers(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := registerAdmin(t, ts)
	user, _ := s.Auth.ByUsername("admin")

	// A second signed-in browser, which must not survive the change --
	// "sign out everywhere" is the point, not a side effect.
	other := loggedInClient(t, ts.URL, "admin", "password123")
	if resp, err := other.Get(ts.URL + "/api/definitions"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: the second session is not usable (%v)", err)
	}

	resp := postJSON(t, client, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "a-brand-new-password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password returned %d, want 200", resp.StatusCode)
	}

	// The old password no longer works, the new one does.
	if _, err := s.Auth.Authenticate("admin", "password123", time.Now()); err == nil {
		t.Error("the old password still authenticates")
	}
	if _, err := s.Auth.Authenticate("admin", "a-brand-new-password", time.Now()); err != nil {
		t.Errorf("the new password does not authenticate: %v", err)
	}

	// This browser stays signed in: being signed out by your own
	// password change is the behaviour that stops people doing it.
	if got, err := client.Get(ts.URL + "/api/definitions"); err != nil || got.StatusCode != http.StatusOK {
		t.Errorf("the browser that changed the password was signed out (%v)", err)
	}

	// The other one is gone, immediately -- not merely doomed on its
	// next PasswordChangedAt check.
	if got, err := other.Get(ts.URL + "/api/definitions"); err == nil && got.StatusCode == http.StatusOK {
		t.Error("a session opened before the password change is still usable")
	}
	if _, ok := s.Auth.Get(user.ID); !ok {
		t.Error("the account itself went missing")
	}
}

func TestChangePasswordRefusesAWrongCurrentPassword(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	resp := postJSON(t, client, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "not-the-password",
		NewPassword:     "a-brand-new-password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	// And the password is untouched.
	if _, err := s.Auth.Authenticate("admin", "password123", time.Now()); err != nil {
		t.Error("a failed change altered the password anyway")
	}
}

func TestChangePasswordRefusesAShortOrUnchangedPassword(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	short := postJSON(t, client, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "short",
	})
	short.Body.Close()
	if short.StatusCode != http.StatusBadRequest {
		t.Errorf("a too-short password returned %d, want 400", short.StatusCode)
	}

	same := postJSON(t, client, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "password123",
	})
	same.Body.Close()
	if same.StatusCode != http.StatusBadRequest {
		t.Errorf("reusing the current password returned %d, want 400", same.StatusCode)
	}
}

// Without a session this is just an oracle for guessing a password that
// happens to need a cookie -- and a stolen cookie is exactly what an
// attacker reaching this would have.
func TestChangePasswordRequiresASession(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	registerAdmin(t, ts)

	anon := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, anon, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "a-brand-new-password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// A request target is attacker-controlled and arrives decoded, so a
// %0A in it becomes a real newline by the time it reaches a log call.
// Before #528 the auth log wrote it raw, which let anyone who could
// reach the login route -- no credentials needed -- append convincing
// lines to mikroview's own log. Same shape as authzMatrix: the decision
// is recorded as a test so removing the quoting fails here rather than
// being noticed in an incident.
func TestAuthErrorLogLineCannotForgeALogEntry(t *testing.T) {
	forged := "/api/x\n2026-08-23 INFO  auth │ forged entry"

	line := authErrorLogLine("GET", forged, errors.New("bad credentials"))

	if strings.ContainsAny(line, "\n\r") {
		t.Errorf("log line carries a raw newline, so a request can forge entries:\n%s", line)
	}
	if !strings.Contains(line, `\n`) {
		t.Errorf("expected the injected newline to survive as an escape, got %q", line)
	}
	if !strings.Contains(line, "forged entry") {
		t.Errorf("expected the path to still be readable for diagnosis, got %q", line)
	}
}

// The error is quoted for the same reason as the path: this branch runs
// for errors we did not anticipate, so its text is the most likely to
// have come from something a caller sent.
func TestAuthErrorLogLineQuotesTheError(t *testing.T) {
	line := authErrorLogLine("POST", "/api/login", errors.New("boom\nWARN auth │ fake"))

	if strings.ContainsAny(line, "\n\r") {
		t.Errorf("error text broke out of its line:\n%s", line)
	}
}
