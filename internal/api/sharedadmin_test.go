// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// The first (always-admin) account every admin-gated fixture below runs
// as. Obvious placeholders, never anything shaped like a credential
// somebody might really hold.
const (
	fixtureAdminUsername = "admin"
	fixtureAdminPassword = "password123"
)

// sharedAdminFixture is the one admin account this package registers and
// enrols through the real routes -- POST /api/auth/register, then
// /api/auth/totp/enrol and /confirm -- exactly once per test binary,
// and then hands to every test that merely needs *an* admin (#1338).
//
// Each of those steps runs Argon2id at production cost (64 MiB, three
// passes -- internal/auth/password.go), and confirm runs it ten more
// times to hash the recovery codes, so registering a fresh admin per
// test cost about a dozen hashes each, several hundred times over,
// which is what pushed this package past go test's ten-minute limit
// under -race. The KDF's cost is right and deliberately untouchable;
// it is the fixtures that should not pay it hundreds of times for an
// account whose only job is to be signed in.
//
// usersJSON is the accounts store exactly as the server persisted it
// after enrolment: every later fixture gets its own copy in its own
// temp dir, so tests stay as isolated from each other as they were when
// each registered its own admin. The tests that prove registration,
// sign-in, enrolment and #1253's forced-enrolment door still drive
// those routes themselves (auth_test.go, totp_test.go, passkey_test.go,
// secondfactordoor_test.go), and building this fixture drives them once
// more -- a break in any of them still fails the run.
type sharedAdminFixture struct {
	usersJSON []byte
	secret    []byte
	codes     []string
	counter   uint64
	built     bool
}

var (
	sharedAdminOnce sync.Once
	sharedAdmin     sharedAdminFixture
)

// buildSharedAdmin runs the real registration and enrolment against a
// throwaway server and keeps what came out of it. A failure here fails
// the test that happened to go first; every later caller then fails
// with a pointer back to it rather than repeating the work.
func buildSharedAdmin(t *testing.T) {
	t.Helper()
	s := newAuthTestServer(t)
	path := filepath.Join(t.TempDir(), "users.json")
	st, err := auth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Auth = st
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: fixtureAdminUsername, Password: fixtureAdminPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("registering the shared admin account failed: %d", resp.StatusCode)
	}
	secret, codes, counter := totpEnrolAndConfirm(t, client, ts)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sharedAdmin = sharedAdminFixture{usersJSON: data, secret: secret, codes: codes, counter: counter, built: true}
}

// registerAdmin gives s the shared admin account (see sharedAdminFixture)
// and returns a client whose cookie jar carries a session for it -- the
// shared setup every admin-gated test needs, already past #1253's
// forced-enrolment door since the account holds a confirmed factor.
// Also records that factor for ts (rememberTOTPFactor), so a later
// loggedInClient re-login as the admin can finish the second step.
//
// s must be the server ts is serving: the fixture replaces s.Auth with
// a store opened on this test's own copy of the persisted accounts.
func registerAdmin(t *testing.T, s *Server, ts *httptest.Server) *http.Client {
	t.Helper()
	sharedAdminOnce.Do(func() { buildSharedAdmin(t) })
	if !sharedAdmin.built {
		t.Fatal("the shared admin fixture failed to build in an earlier test; see that failure")
	}
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, sharedAdmin.usersJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := auth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Auth = st
	rememberTOTPFactor(ts.URL, fixtureAdminUsername, sharedAdmin.secret, sharedAdmin.codes, sharedAdmin.counter)
	return sessionClient(t, s, ts, fixtureAdminUsername)
}

// sessionClient returns a client signed in as username on s without
// going through the login routes: the same Sessions.Create and cookie
// that handleAuthLoginFactor's final step performs, minus the password
// and second-factor checks a real sign-in proves first. For a test that
// only needs an authenticated caller those checks are setup, not
// coverage -- the tests of sign-in itself still use loggedInClient and
// the routes.
func sessionClient(t *testing.T, s *Server, ts *httptest.Server, username string) *http.Client {
	t.Helper()
	var userID string
	for _, u := range s.Auth.List() {
		if u.Username == username {
			userID = u.ID
		}
	}
	if userID == "" {
		t.Fatalf("no account named %q to mint a session for", username)
	}
	sess := s.Sessions.Create(userID, time.Now())
	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: mustCookieJar(t)}
	client.Jar.SetCookies(base, []*http.Cookie{{Name: sessionCookieName, Value: sess.ID, Path: "/"}})
	return client
}

// seedFactor gives username's account on s a confirmed TOTP factor
// through the store rather than the enrol/confirm routes, and records it
// for ts so a later loggedInClient re-login can finish the second step.
// Confirming through the routes also mints ten recovery codes, each an
// Argon2id hash at production cost (#1338); for an account that only
// needs to be past #1253's forced-enrolment door before the test gets
// to what it is actually about, that is setup, not coverage. The route
// path -- and what else it does, rotating the session and recording
// account.totp_enabled -- has its own tests in totp_test.go and
// secondfactordoor_test.go, which keep using totpEnrolAndConfirm.
func seedFactor(t *testing.T, s *Server, ts *httptest.Server, username string) {
	t.Helper()
	var userID string
	for _, u := range s.Auth.List() {
		if u.Username == username {
			userID = u.ID
		}
	}
	if userID == "" {
		t.Fatalf("no account named %q to seed a factor for", username)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.SetPendingTOTPSecret(userID, auth.EncodeTOTPSecret(secret)); err != nil {
		t.Fatalf("seeding a factor for %q: %v", username, err)
	}
	now := time.Now()
	counter := totpCounterNow(now)
	if err := s.Auth.ConfirmTOTP(userID, now, counter); err != nil {
		t.Fatalf("confirming the seeded factor for %q: %v", username, err)
	}
	rememberTOTPFactor(ts.URL, username, secret, nil, counter)
}
