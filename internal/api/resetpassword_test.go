// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// Obvious placeholders throughout, never anything shaped like a
// credential somebody might really hold.
const (
	resetOldPassword = "bilbo-password-placeholder"
	resetNewPassword = "bilbo-new-password-placeholder"
)

// resetCodePattern is the grouped form an admin reads out:
// xxxx-xxxx-xxxx-xxxx over the unambiguous alphabet (no 0/O/1/I).
var resetCodePattern = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}(-[A-HJ-NP-Z2-9]{4}){3}$`)

// codeShapedText is the same shape found anywhere inside a body, for
// asserting that a refusal never carries one.
var codeShapedText = regexp.MustCompile(`[A-HJ-NP-Z2-9]{4}(-[A-HJ-NP-Z2-9]{4}){3}`)

// resetTestServer stands up an auth-enabled server holding an admin and
// one ordinary account ("bilbo"), returning the admin's client, bilbo's
// user ID, and the test server.
func resetTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client, string) {
	t.Helper()
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	admin := registerAdmin(t, s, ts)
	postJSON(t, admin, ts.URL+"/api/auth/users",
		createUserRequest{Username: "bilbo", Password: resetOldPassword, Role: "user"}).Body.Close()

	var id string
	for _, u := range s.Auth.List() {
		if u.Username == "bilbo" {
			id = u.ID
		}
	}
	if id == "" {
		t.Fatal("the account the reset tests act on was not created")
	}
	return s, ts, admin, id
}

// resetPassword posts the admin reset for id and returns the decoded
// response, failing the test if the status is not 200.
func resetPassword(t *testing.T, admin *http.Client, ts *httptest.Server, id string) resetPasswordResponse {
	t.Helper()
	resp := postJSON(t, admin, ts.URL+"/api/auth/users/"+id+"/reset-password", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("reset-password returned %d: %s", resp.StatusCode, body)
	}
	var out resetPasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sessionOf reads GET /api/auth/session through client.
func sessionOf(t *testing.T, client *http.Client, ts *httptest.Server) sessionResponse {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestAdminResetIssuesACodeAndKillsTheOldCredential is the positive
// path end to end: the code comes back once, in the form an admin reads
// aloud; the old password stops working immediately; the sessions that
// account already held are gone; and the code gets its owner back in
// with the forced-change flag set.
func TestAdminResetIssuesACodeAndKillsTheOldCredential(t *testing.T) {
	_, ts, admin, id := resetTestServer(t)

	// A session bilbo already holds, established before the reset.
	before := loggedInClient(t, ts.URL, "bilbo", resetOldPassword)

	out := resetPassword(t, admin, ts, id)
	if out.Username != "bilbo" {
		t.Errorf("username = %q, want %q", out.Username, "bilbo")
	}
	if !resetCodePattern.MatchString(out.Code) {
		t.Errorf("code %q is not the grouped, unambiguous form an admin reads aloud", out.Code)
	}
	if d := time.Until(out.ExpiresAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Errorf("code expires in %v, want roughly 24 hours", d)
	}

	// The session that existed before the reset is gone.
	resp, err := before.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session held before the reset got %d from /api/flags, want 401 -- the reset must end it", resp.StatusCode)
	}

	// The old password is dead the moment the admin clicks.
	dead := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, dead, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "bilbo", Password: resetOldPassword})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the old password got %d, want 401 -- it must not survive the reset", loginResp.StatusCode)
	}

	// The code goes in the password box and works.
	withCode := loggedInClient(t, ts.URL, "bilbo", out.Code)
	sess := sessionOf(t, withCode, ts)
	if !sess.Authenticated {
		t.Fatal("expected the code login to establish a session")
	}
	if !sess.MustChangePassword {
		t.Error("expected the session response to carry the forced-change flag")
	}
}

// TestAdminResetRefusals covers every caller and target this route has
// to turn away, and the status each gets.
func TestAdminResetRefusals(t *testing.T) {
	tests := []struct {
		name string
		// caller returns the client making the request; nil jar means
		// anonymous.
		caller func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client
		// target returns the user ID in the path.
		target func(t *testing.T, s *Server, bilboID string) string
		want   int
	}{
		{
			name:   "an admin cannot reset their own account",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client { return admin },
			target: func(t *testing.T, s *Server, bilboID string) string {
				for _, u := range s.Auth.List() {
					if u.Role == auth.RoleAdmin {
						return u.ID
					}
				}
				t.Fatal("no admin account")
				return ""
			},
			want: http.StatusConflict,
		},
		{
			name:   "an SSO-only account belongs to its provider",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client { return admin },
			target: func(t *testing.T, s *Server, bilboID string) string {
				u, _, err := s.Auth.FindOrCreateOIDCUser("https://idp.example", "subject-placeholder", "frodo", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				return u.ID
			},
			want: http.StatusConflict,
		},
		{
			name:   "no such account",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client { return admin },
			target: func(t *testing.T, s *Server, bilboID string) string { return "not-a-real-user-id" },
			want:   http.StatusNotFound,
		},
		{
			name: "a user-tier caller may not mint a credential for someone else",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client {
				if _, err := s.Auth.CreateUser("operator", "operator-password-placeholder", auth.RoleUser, time.Now()); err != nil {
					t.Fatal(err)
				}
				return loggedInClient(t, ts.URL, "operator", "operator-password-placeholder")
			},
			target: func(t *testing.T, s *Server, bilboID string) string { return bilboID },
			want:   http.StatusForbidden,
		},
		{
			name: "nor may a viewer",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client {
				if _, err := s.Auth.CreateUser("watcher", "watcher-password-placeholder", auth.RoleViewer, time.Now()); err != nil {
					t.Fatal(err)
				}
				return loggedInClient(t, ts.URL, "watcher", "watcher-password-placeholder")
			},
			target: func(t *testing.T, s *Server, bilboID string) string { return bilboID },
			want:   http.StatusForbidden,
		},
		{
			name: "nor may an anonymous caller",
			caller: func(t *testing.T, s *Server, ts *httptest.Server, admin *http.Client) *http.Client {
				return &http.Client{Jar: mustCookieJar(t)}
			},
			target: func(t *testing.T, s *Server, bilboID string) string { return bilboID },
			want:   http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, ts, admin, bilboID := resetTestServer(t)
			client := tc.caller(t, s, ts, admin)
			id := tc.target(t, s, bilboID)

			resp := postJSON(t, client, ts.URL+"/api/auth/users/"+id+"/reset-password", nil)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d (%s)", resp.StatusCode, tc.want, body)
			}
			// Whatever the refusal, no credential may come back with it.
			if codeShapedText.Match(body) {
				t.Errorf("a refusal carried something shaped like a reset code: %s", body)
			}
		})
	}
}

// TestSecondResetKillsTheFirstCodeOverHTTP is the "a second click
// issues a new code and kills the first" half of the route's contract,
// exercised the way an admin would actually hit it.
func TestSecondResetKillsTheFirstCodeOverHTTP(t *testing.T) {
	_, ts, admin, id := resetTestServer(t)

	first := resetPassword(t, admin, ts, id)
	second := resetPassword(t, admin, ts, id)
	if first.Code == second.Code {
		t.Fatal("the second reset returned the same code as the first")
	}

	stale := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, stale, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "bilbo", Password: first.Code})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the superseded code got %d, want 401", resp.StatusCode)
	}

	live := loggedInClient(t, ts.URL, "bilbo", second.Code)
	if !sessionOf(t, live, ts).Authenticated {
		t.Error("expected the newest code to still work")
	}
}

// TestForcedChangeTakesOnlyTheNewPasswordAndOpensTheApp is the far end
// of the flow: the restricted session trades the code for a password of
// the person's own choosing, without being asked for a current one it
// cannot have, and comes out of it an ordinary session.
func TestForcedChangeTakesOnlyTheNewPasswordAndOpensTheApp(t *testing.T) {
	_, ts, admin, id := resetTestServer(t)
	// #1253: a fresh account can reach nothing but the enrolment routes,
	// so in real use bilbo would have enrolled a factor at first sign-in,
	// long before an admin ever reset the password -- IssueResetCode
	// doesn't touch it (only the password and every other session), so
	// it carries over across the reset. Enrolled here, before the reset,
	// to put bilbo in the state a real account reaching this flow would
	// already be in; without it, the forced change below 403s at the
	// second-factor door, which changePasswordPath's own MustChangePassword
	// allowance does nothing about.
	bilbo := loggedInClient(t, ts.URL, "bilbo", resetOldPassword)
	// enrolAndRememberFactor, not a bare totpEnrolAndConfirm: the
	// loggedInClient call two lines down signs bilbo in again with the
	// reset code (Authenticate treats it as the password), which now
	// also only reaches the pending-factor step -- the remembered secret
	// is what lets that second loggedInClient call complete it.
	enrolAndRememberFactor(t, bilbo, ts, "bilbo")

	out := resetPassword(t, admin, ts, id)
	client := loggedInClient(t, ts.URL, "bilbo", out.Code)

	// Blocked before the change.
	blocked, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusForbidden {
		t.Errorf("a flagged session got %d from /api/flags, want 403", blocked.StatusCode)
	}

	// No current password supplied -- there is none to supply.
	resp := postJSON(t, client, ts.URL+"/api/auth/password",
		changePasswordRequest{NewPassword: resetNewPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the forced change returned %d: %s", resp.StatusCode, body)
	}

	sess := sessionOf(t, client, ts)
	if !sess.Authenticated || sess.MustChangePassword {
		t.Errorf("after the change: authenticated=%v mustChangePassword=%v, want true/false",
			sess.Authenticated, sess.MustChangePassword)
	}

	open, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	open.Body.Close()
	if open.StatusCode != http.StatusOK {
		t.Errorf("the session got %d from /api/flags after the change, want 200", open.StatusCode)
	}

	// The spent code is worthless, and the new password works.
	stale := &http.Client{Jar: mustCookieJar(t)}
	again := postJSON(t, stale, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "bilbo", Password: out.Code})
	again.Body.Close()
	if again.StatusCode != http.StatusUnauthorized {
		t.Errorf("the spent code got %d, want 401", again.StatusCode)
	}
	loggedInClient(t, ts.URL, "bilbo", resetNewPassword)
}

// TestForcedChangeThenForcedEnrolmentBothComplete is the deadlock the
// second-factor gate could cause before requireAuth's !user.
// MustChangePassword guard was added: an admin-reset account that has
// never enrolled a second factor must be able to clear both doors in
// sequence -- change its password first, then enrol -- and never get
// stuck at either. Deliberately does not pre-enrol bilbo the way
// TestForcedChangeTakesOnlyTheNewPasswordAndOpensTheApp does; that
// test's own comment explains why it has to (the second-factor door
// would otherwise 403 the forced change itself), which is exactly the
// bug this test exists to catch a regression of.
func TestForcedChangeThenForcedEnrolmentBothComplete(t *testing.T) {
	_, ts, admin, id := resetTestServer(t)

	out := resetPassword(t, admin, ts, id)
	client := loggedInClient(t, ts.URL, "bilbo", out.Code)

	// Blocked before the change, same as the enrolled case.
	blocked, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusForbidden {
		t.Errorf("a flagged session got %d from /api/flags, want 403", blocked.StatusCode)
	}

	// The change itself must succeed: this is exactly the request the
	// deadlock refused -- no second factor yet, and changePasswordPath
	// was not on the second-factor gate's own allowlist.
	resp := postJSON(t, client, ts.URL+"/api/auth/password",
		changePasswordRequest{NewPassword: resetNewPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the forced change returned %d: %s", resp.StatusCode, body)
	}

	sess := sessionOf(t, client, ts)
	if !sess.Authenticated || sess.MustChangePassword {
		t.Errorf("after the change: authenticated=%v mustChangePassword=%v, want true/false",
			sess.Authenticated, sess.MustChangePassword)
	}

	// The password door is clear, but the enrolment door now holds --
	// the app itself is still unreachable until a factor is enrolled.
	stillBlocked, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	stillBlocked.Body.Close()
	if stillBlocked.StatusCode != http.StatusForbidden {
		t.Errorf("a session with no second factor got %d from /api/flags, want 403", stillBlocked.StatusCode)
	}

	// Enrolling clears the second door.
	enrolAndRememberFactor(t, client, ts, "bilbo")

	open, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	open.Body.Close()
	if open.StatusCode != http.StatusOK {
		t.Errorf("a fully enrolled session got %d from /api/flags, want 200", open.StatusCode)
	}
}

// TestResetCodeNeverReachesTheAuditLogOrServerLogs is the one rule with
// no acceptable failure mode: the code exists in clear in exactly one
// place, the response to the admin's own request.
func TestResetCodeNeverReachesTheAuditLogOrServerLogs(t *testing.T) {
	_, ts, admin, id := resetTestServer(t)

	// authLog is the package's own logger and the only one anything on
	// this path writes through -- writeAuthError's fallback line for an
	// error with no user-facing message is the single log call the reset
	// route can reach. Swapped for a buffer rather than redirecting the
	// process's stdout, which the shared handler captured at init and
	// would not follow. internal/api's tests do not run in parallel, so
	// nothing else is logging into this buffer.
	var logged strings.Builder
	realLog := authLog
	authLog = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { authLog = realLog })

	out := resetPassword(t, admin, ts, id)
	loggedInClient(t, ts.URL, "bilbo", out.Code)

	bare := strings.ReplaceAll(out.Code, "-", "")
	for _, form := range []string{out.Code, bare, strings.ToLower(out.Code), strings.ToLower(bare)} {
		if strings.Contains(logged.String(), form) {
			t.Errorf("the issued code appeared in the server log as %q", form)
		}
	}

	res := fetchAudit(t, admin, ts)
	var entry string
	for _, e := range res.Entries {
		line := e.Actor + " " + e.Action + " " + e.Target + " " + e.Detail
		if e.Action == "user.password_reset" {
			entry = line
		}
		for _, form := range []string{out.Code, bare} {
			if strings.Contains(line, form) {
				t.Errorf("the issued code appeared in an audit entry: %q", line)
			}
		}
	}
	if entry == "" {
		t.Fatal("no user.password_reset audit entry was recorded -- who reset whom has to be on the record")
	}
	if !strings.Contains(entry, "admin") || !strings.Contains(entry, "bilbo") {
		t.Errorf("audit entry %q does not say who reset whom", entry)
	}
}
