// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// The first admin is created only with the one-time setup code the
// server announced in its log (#1415): an empty accounts store is not
// only a fresh install, and the first visitor to reach one must not
// become admin just by getting there first.

func TestRegisterWithoutSetupCodeIsRefused(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a first-run registration without the setup code", resp.StatusCode)
	}
	if n := s.Auth.Count(); n != 0 {
		t.Errorf("Count() = %d after a refused registration, want 0 -- the first visitor took admin without the code", n)
	}
}

func TestOIDCLoginRefusedDuringSetup(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/api/auth/oidc/login", "/api/auth/oidc/callback?code=x&state=y"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s during setup: status = %d, want 503 -- SSO must not create the first account", path, resp.StatusCode)
		}
	}
	if n := s.Auth.Count(); n != 0 {
		t.Errorf("Count() = %d, want 0", n)
	}
}

// testSetupCodes remembers the setup code each test store announced,
// keyed by the store, so a test can register its first admin the way a
// real operator does: with the code from the log (#1415).
var testSetupCodes sync.Map // *auth.Store -> *atomic.Pointer[string]

// openTestAuthStore opens a persisted accounts store on b and records
// the setup code it announces -- at open, and again if a reload applies
// an emptied document.
func openTestAuthStore(t *testing.T, b persist.Backend) *auth.Store {
	t.Helper()
	code := new(atomic.Pointer[string])
	st, err := auth.OpenStore(b, auth.Options{OnSetupCode: auth.SetupCodeFunc(func(c string) { code.Store(&c) })})
	if err != nil {
		t.Fatal(err)
	}
	testSetupCodes.Store(st, code)
	return st
}

// testSetupCode is the code st last announced, or "" when it announced
// none (a store opened another way, or one that already held accounts).
func testSetupCode(st *auth.Store) string {
	v, ok := testSetupCodes.Load(st)
	if !ok {
		return ""
	}
	if c := v.(*atomic.Pointer[string]).Load(); c != nil {
		return *c
	}
	return ""
}

// setupRequest is the first-run registration body for s: the
// credentials plus the setup code s's accounts store announced.
func setupRequest(t *testing.T, s *Server, username, password string) registerRequest {
	t.Helper()
	return registerRequest{Username: username, Password: password, SetupCode: testSetupCode(s.Auth)}
}

func TestRegisterWithTheSetupCodeCreatesTheAdmin(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	req := setupRequest(t, s, "admin", "password123")
	if req.SetupCode == "" {
		t.Fatal("test setup: the store announced no setup code")
	}
	// Typed the way a person might copy it out of a log line.
	req.SetupCode = " " + strings.ToLower(req.SetupCode) + " "
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 with the right setup code", resp.StatusCode)
	}
	if u, ok := s.Auth.ByUsername("admin"); !ok || u.Role != auth.RoleAdmin {
		t.Errorf("the first account = %+v, want admin", u)
	}

	// The code is dead now: registration is closed, whatever code is shown.
	again := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", setupRequest(t, s, "second", "password123"))
	defer again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Errorf("a second registration with the same code = %d, want 409", again.StatusCode)
	}
}

func TestRegisterWithAWrongSetupCodeIs401(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register",
		registerRequest{Username: "admin", Password: "password123", SetupCode: "AAAA-AAAA-AAAA-AAAA"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(string(body), "server's log") {
		t.Errorf("body = %q, want it to say where the code is", body)
	}
	if s.Auth.Count() != 0 {
		t.Error("a wrong setup code created an account")
	}
}

// Probing for the code spends the same per-address budget as login:
// once it is gone even the right code is refused until the window
// slides, and the refusal is a 429.
func TestRegisterSetupCodeGuessesAreRateLimited(t *testing.T) {
	s := newAuthTestServer(t)
	s.LoginLimiter = auth.NewLoginLimiter(2, time.Minute)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	wrong := registerRequest{Username: "admin", Password: "password123", SetupCode: "AAAA-AAAA-AAAA-AAAA"}
	for i := 0; i < 2; i++ {
		resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", wrong)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("guess %d = %d, want 401", i+1, resp.StatusCode)
		}
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", setupRequest(t, s, "admin", "password123"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("after the budget is spent = %d, want 429", resp.StatusCode)
	}
	if s.Auth.Count() != 0 {
		t.Error("an account was created past the limit")
	}
}

// The budget guards the code, not the operator's typing: a right code
// with a password that is too short gives the attempt back.
func TestRegisterRightSetupCodeReleasesTheAttempt(t *testing.T) {
	s := newAuthTestServer(t)
	s.LoginLimiter = auth.NewLoginLimiter(1, time.Minute)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	short := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", setupRequest(t, s, "admin", "short"))
	short.Body.Close()
	if short.StatusCode != http.StatusBadRequest {
		t.Fatalf("too-short password = %d, want 400", short.StatusCode)
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", setupRequest(t, s, "admin", "password123"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("retry with the right code = %d, want 201 -- the first attempt was not given back", resp.StatusCode)
	}
}

func TestSessionReportsSetupRequiredUntilTheAdminExists(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	setupRequired := func() bool {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/auth/session")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body sessionResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.SetupRequired
	}
	if !setupRequired() {
		t.Fatal("setupRequired = false on an empty store")
	}
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", registerRequest{Username: "admin", Password: "password123"}).Body.Close()
	if !setupRequired() {
		t.Error("setupRequired = false after a registration without the code")
	}
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register", setupRequest(t, s, "admin", "password123")).Body.Close()
	if setupRequired() {
		t.Error("setupRequired = true after the admin was created")
	}
}
