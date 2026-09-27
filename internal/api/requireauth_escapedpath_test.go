// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// escapedPathProbeMux stands in for next -- the real s.mux() -- with one
// wildcard route, the shape #1389 turns on: net/http's pattern matching
// for `{x}` works on the request's *escaped* path, so a single path
// segment can carry a literal "%2F" that decodes to a "/" without ever
// splitting the match. hit is set iff this handler actually runs, which
// is the "reaches the app handler" this issue is about.
func escapedPathProbeMux(hit *atomic.Bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/{x}", func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusTeapot) // a status no real route ever returns
	})
	return mux
}

// sessionCookieValue reads the session cookie client's jar holds for
// base -- the same cookie the real frontend would send back -- so it can
// be replayed against a second httptest.Server sharing the same *Server
// (same Sessions store) as the one that minted it.
func sessionCookieValue(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == sessionCookieName {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie found for %s", sessionCookieName, base)
	return ""
}

// getWithCookie issues a GET to url carrying, if non-empty, the session
// cookie value sessionID.
func getWithCookie(t *testing.T, url, sessionID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestRequireAuthBootstrapExemptionMatchesEscapedPath (#1389): while
// undecided (no account yet), everything but bootstrapExemptPaths must
// 401 -- including a request whose *decoded* path reads as one of those
// exempt routes but whose escaped path (what next actually routes on)
// is something else entirely.
func TestRequireAuthBootstrapExemptionMatchesEscapedPath(t *testing.T) {
	s := newAuthTestServer(t) // Count()==0: undecided
	var hit atomic.Bool
	ts := httptest.NewServer(s.requireAuth(escapedPathProbeMux(&hit)))
	defer ts.Close()

	// Decodes to "/api/auth/session" (bootstrap-exempt), but its escaped
	// form -- what the probe's "GET /api/{x}" pattern actually matches --
	// is "/api/auth%2Fsession", a single segment, not that exempt route.
	resp, err := http.Get(ts.URL + "/api/auth%2Fsession")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if hit.Load() {
		t.Error("an anonymous request reached the app handler during the undecided/bootstrap window " +
			"by decoding to an exempt path while escaping to something else")
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (setup required)", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestRequireAuthExemptPathsMatchesEscapedPath (#1389): once auth is
// active, an anonymous, unauthenticated request must still 401 unless
// its real, escaped path is one of exemptPaths -- not merely a path
// that decodes to one.
func TestRequireAuthExemptPathsMatchesEscapedPath(t *testing.T) {
	s := newAuthTestServer(t)
	if _, err := s.Auth.Register("placeholder-owner", "placeholder-password1", time.Now()); err != nil {
		t.Fatal(err)
	}
	var hit atomic.Bool
	ts := httptest.NewServer(s.requireAuth(escapedPathProbeMux(&hit)))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth%2Fsession")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if hit.Load() {
		t.Error("an anonymous request reached the app handler by decoding to an exempt path " +
			"while escaping to something else")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestRequireAuthChangePasswordGateMatchesEscapedPath (#1389): a session
// stuck at the forced-change door (MustChangePassword) may reach only
// changePasswordPath -- not a route that merely decodes to it while
// escaping to something the mux would actually dispatch elsewhere.
func TestRequireAuthChangePasswordGateMatchesEscapedPath(t *testing.T) {
	s, ts, admin, id := resetTestServer(t)
	out := resetPassword(t, admin, ts, id)
	withCode := loggedInClient(t, ts.URL, "bilbo", out.Code)
	sessionID := sessionCookieValue(t, withCode, ts.URL)

	var hit atomic.Bool
	probe := httptest.NewServer(s.requireAuth(escapedPathProbeMux(&hit)))
	defer probe.Close()

	resp := getWithCookie(t, probe.URL+"/api/auth%2Fpassword", sessionID)
	resp.Body.Close()

	if hit.Load() {
		t.Error("a session stuck at the must-change-password door reached the app handler " +
			"by decoding to changePasswordPath while escaping to something else")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// TestRequireAuthSecondFactorDoorMatchesEscapedPath (#1389): a session
// stuck at the forced-enrolment door may reach only the routes in
// secondFactorEnrolPaths -- not one that merely decodes to a member of
// that set.
func TestRequireAuthSecondFactorDoorMatchesEscapedPath(t *testing.T) {
	s, ts, c := unenrolledUser(t)
	sessionID := sessionCookieValue(t, c, ts.URL)

	var hit atomic.Bool
	probe := httptest.NewServer(s.requireAuth(escapedPathProbeMux(&hit)))
	defer probe.Close()

	resp := getWithCookie(t, probe.URL+"/api/auth%2Ftotp%2Fenrol", sessionID)
	resp.Body.Close()

	if hit.Load() {
		t.Error("a session with no second factor reached the app handler by decoding to a " +
			"secondFactorEnrolPaths member while escaping to something else")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}
