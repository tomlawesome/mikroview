// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// access is the level of caller a route requires once authentication is
// active (an account exists and auth has not been disabled).
type access int

const (
	// accessPublic: reachable with no session at all. Every entry here
	// is a deliberate hole in the authentication wall and should be
	// scrutinised on sight.
	accessPublic access = iota
	// accessUser: any authenticated session, regardless of role.
	accessUser
	// accessAdmin: an authenticated session whose role is admin.
	accessAdmin
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessUser:
		return "user"
	default:
		return "admin"
	}
}

// routeExpectation is one row of the authorization matrix: a route, the
// access level it must enforce, and why.
type routeExpectation struct {
	method string
	path   string
	want   access
	// why documents the intent, so a future change that flips a level
	// has to consciously rewrite the justification rather than quietly
	// edit a constant.
	why string
}

// authzMatrix is the single source of truth for who may reach what.
//
// This exists because per-endpoint spot-check tests only cover the
// endpoints somebody remembered to write a test for. A real permission
// gap shipped exactly that way: POST /api/flags/{id}/clear-permanent
// was reachable by any authenticated non-admin and could permanently
// suppress detection for a chosen target, unlogged, because no test
// asserted otherwise and the omission looked like a deliberate choice.
//
// TestEveryRouteIsInTheAuthorizationMatrix below fails when a route is
// added to Routes() without a row here, so the next endpoint cannot
// reach main without someone stating its access level on the record.
var authzMatrix = []routeExpectation{
	// -- Public by necessity -------------------------------------------
	{http.MethodGet, "/api/healthz", accessPublic,
		"liveness probe -- hit by Docker's HEALTHCHECK, which has no session"},
	{http.MethodGet, "/api/auth/session", accessPublic,
		"reports auth state so the frontend can decide which screen to draw; must answer before a session exists"},
	{http.MethodPost, "/api/auth/login", accessPublic,
		"the login endpoint itself"},
	{http.MethodPost, "/api/auth/logout", accessPublic,
		"calling it without a session is a harmless no-op, not worth a 401"},
	{http.MethodGet, "/api/auth/oidc/login", accessPublic,
		"starts the SSO redirect; a login must work before a session exists"},
	{http.MethodGet, "/api/auth/oidc/callback", accessPublic,
		"the provider redirects the browser here; protected by state/nonce/PKCE, not by a session"},
	{http.MethodPost, "/api/auth/register", accessPublic,
		"first-run only -- auth.Store.Register refuses once any account exists"},
	{http.MethodPost, "/api/auth/skip", accessPublic,
		"first-run only -- auth.Store.Disable refuses once any account exists"},

	{http.MethodGet, "/api/config/problems", accessAdmin,
		"config key names, filesystem paths, the OIDC issuer URL and SMTP hosts are an infrastructure map; a non-admin gets an empty list rather than a 403, since whether problems exist is itself information"},

	// -- Any authenticated user ----------------------------------------
	{http.MethodGet, "/api/events", accessUser,
		"core read: the live firewall event feed"},
	{http.MethodGet, "/api/devices", accessUser, "core read"},
	{http.MethodGet, "/api/rules", accessUser, "core read"},
	{http.MethodGet, "/api/critical-ports", accessUser, "core read"},
	{http.MethodGet, "/api/stats", accessUser, "core read"},
	{http.MethodGet, "/api/ws", accessUser,
		"live tail; additionally same-origin checked (see checkOrigin)"},
	{http.MethodGet, "/api/lookup/ip/{ip}", accessUser,
		"on-demand reputation lookup, proxied so no API key reaches the browser"},
	{http.MethodGet, "/api/flags", accessUser, "core read"},
	{http.MethodPost, "/api/flags/{id}/clear", accessUser,
		"reversible: a cleared flag raises again on the next matching event, so any user may dismiss noise"},

	// -- Admin only ----------------------------------------------------
	{http.MethodPost, "/api/flags/{id}/clear-permanent", accessAdmin,
		"NOT reversible without an admin: permanently suppresses detection for a (type, target) until someone undoes it"},
	{http.MethodGet, "/api/flags/exclusions", accessAdmin,
		"the review surface for permanent exclusions"},
	{http.MethodDelete, "/api/flags/exclusions/{id}", accessAdmin,
		"undoes an exclusion, re-arming detection"},
	{http.MethodGet, "/api/detectors", accessAdmin,
		"detector on/off and scope -- reveals exactly what this deployment is and isn't watching"},
	{http.MethodPut, "/api/detectors/{name}", accessAdmin,
		"disabling a detector blinds the tool; strictly admin"},
	{http.MethodGet, "/api/entities", accessAdmin, "admin-managed labels/tags"},
	{http.MethodPost, "/api/entities", accessAdmin, "admin-managed labels/tags"},
	{http.MethodDelete, "/api/entities", accessAdmin, "admin-managed labels/tags"},
	{http.MethodPost, "/api/auth/users", accessAdmin, "account creation"},
	{http.MethodPost, "/api/tokens", accessAdmin, "mints a bearer credential"},
	{http.MethodGet, "/api/tokens", accessAdmin, "lists issued bearer credentials"},
	{http.MethodDelete, "/api/tokens/{id}", accessAdmin, "revokes a bearer credential"},
	{http.MethodGet, "/api/audit", accessAdmin,
		"the admin action trail; also the record an attacker would want to read to see whether they were noticed"},
}

// TestEveryRouteIsInTheAuthorizationMatrix is the guard that makes the
// matrix meaningful: it walks the real mux's registered patterns and
// fails if any route lacks a row above. Without it, the matrix would
// silently describe an ever-smaller fraction of the API as routes get
// added.
func TestEveryRouteIsInTheAuthorizationMatrix(t *testing.T) {
	declared := make(map[string]bool, len(authzMatrix))
	for _, r := range authzMatrix {
		declared[r.method+" "+r.path] = true
	}

	for _, pattern := range registeredRoutePatterns(t) {
		if !declared[pattern] {
			t.Errorf("route %q is registered but has no row in authzMatrix.\n"+
				"Add one stating who may reach it and why -- a new endpoint must not ship without that decision "+
				"being made explicitly. (This check exists because clear-permanent shipped unguarded exactly this way.)",
				pattern)
		}
	}

	// The reverse direction: a stale row for a route that no longer
	// exists would give false confidence that something is still being
	// checked.
	registered := make(map[string]bool)
	for _, p := range registeredRoutePatterns(t) {
		registered[p] = true
	}
	for _, r := range authzMatrix {
		if !registered[r.method+" "+r.path] {
			t.Errorf("authzMatrix has a row for %s %s, but no such route is registered -- remove the stale row",
				r.method, r.path)
		}
	}
}

// TestAuthorizationMatrixIsEnforced drives every row against a running
// server in the state that actually matters -- auth active, with both an
// admin and a plain user -- and asserts each of the three caller kinds
// gets what the matrix says.
func TestAuthorizationMatrixIsEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// Establish the admin via the real first-run path, then add a plain
	// user through the store (self-registration is closed by then).
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register",
		credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	if _, err := s.Auth.CreateUser("viewer", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	anon := &http.Client{}
	user := loggedInClient(t, ts.URL, "viewer", "password456")
	admin := loggedInClient(t, ts.URL, "admin", "password123")

	for _, r := range authzMatrix {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			// Skip the two first-run endpoints: an account now exists,
			// so they legitimately refuse everyone. Their "public"
			// classification is about reachability before setup, which
			// their own dedicated tests cover.
			if r.path == "/api/auth/register" || r.path == "/api/auth/skip" {
				t.Skip("first-run only; refuses all callers once an account exists (covered separately)")
			}

			// Logout destroys the session it is called with, so it gets
			// throwaway clients rather than the shared ones -- otherwise
			// probing it would silently log the shared user and admin
			// out and every subsequent row would fail with a misleading
			// 401. (It did, while this test was being written.)
			userClient, adminClient := user, admin
			if r.path == "/api/auth/logout" {
				userClient = loggedInClient(t, ts.URL, "viewer", "password456")
				adminClient = loggedInClient(t, ts.URL, "admin", "password123")
			}

			assertAccess(t, anon, ts.URL, r, "anonymous", r.want == accessPublic)
			assertAccess(t, userClient, ts.URL, r, "user", r.want != accessAdmin)
			assertAccess(t, adminClient, ts.URL, r, "admin", true)
		})
	}
}

// assertAccess issues one request and checks only whether it was let
// through the authorization layer -- not whether the handler behind it
// succeeded. A 404 for a placeholder ID, or a 400 for a body this test
// deliberately doesn't construct, both mean "you got past the gate",
// which is the only thing under test here. 401/403 mean refused.
func assertAccess(t *testing.T, c *http.Client, base string, r routeExpectation, who string, wantAllowed bool) {
	t.Helper()

	resp := doRouteRequest(t, c, base, r)
	defer resp.Body.Close()

	refused := resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	allowed := !refused

	if allowed != wantAllowed {
		verb := "was refused"
		if allowed {
			verb = "was allowed through"
		}
		t.Errorf("%s caller %s on %s %s (status %d), but the matrix says access is %q.\nReason on record: %s",
			who, verb, r.method, r.path, resp.StatusCode, r.want, r.why)
	}
}

// doRouteRequest turns a matrix row into a concrete request: wildcard
// segments get a placeholder that will not match real data (the handler
// behind it is expected to 404, which still proves the gate allowed the
// call through), and every non-GET carries the CSRF header so that
// check never masquerades as an authorization failure.
func doRouteRequest(t *testing.T, c *http.Client, base string, r routeExpectation) *http.Response {
	t.Helper()

	path := r.path
	for _, wildcard := range []string{"{id}", "{ip}", "{name}"} {
		path = strings.Replace(path, wildcard, "matrix-probe-nonexistent", 1)
	}

	req, err := http.NewRequest(r.method, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.method != http.MethodGet {
		req.Header.Set(csrfHeaderName, csrfHeaderValue)
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", r.method, path, err)
	}
	return resp
}

// loggedInClient returns a client holding a live session cookie for the
// given credentials.
func loggedInClient(t *testing.T, base, username, password string) *http.Client {
	t.Helper()
	c := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, c, base+"/api/auth/login", credentialsRequest{Username: username, Password: password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login as %q failed with %d", username, resp.StatusCode)
	}
	return c
}

// registeredRoutePatterns reads the real route table -- the reason
// Server.routes() exists as data (see server.go).
func registeredRoutePatterns(t *testing.T) []string {
	t.Helper()
	s, _ := newTestServer(t)
	out := make([]string, 0, len(s.routes()))
	for _, r := range s.routes() {
		out = append(out, r.method+" "+r.path)
	}
	return out
}
