// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B2 (sign-in and sessions), tests 9 to 15.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// #1202 B2.9: five wrong passwords within five minutes; the sixth attempt
// is 429 rate-limited; the correct password is also 429 until the lockout
// ends (the clock is advanced); the lockout survives a restart.
func TestFiveWrongPasswordsLockTheAccountThroughARestartUntilTheLockoutEnds(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.newUser(t, "lockee", "user")
	c := w.a.client()
	login := func(c *client, pass string) resp {
		return c.post("/api/auth/login", map[string]string{"username": u.name, "password": pass})
	}

	for i := 1; i <= 5; i++ {
		wantProblem(t, "wrong password attempt "+string(rune('0'+i)), login(c, "not-"+u.pass), 401, "invalid-credentials")
	}
	wantProblem(t, "sixth attempt", login(c, "not-"+u.pass), 429, "rate-limited")
	wantProblem(t, "correct password while locked", login(w.a.client(), u.pass), 429, "rate-limited")

	// The lockout is saved on the account: a restart does not lift it.
	a2 := w.a.restart(t, nil)
	w.rebind(t, a2)
	wantProblem(t, "correct password after a restart, still locked", login(a2.client(), u.pass), 429, "rate-limited")

	// Paired: once the lockout has run its five minutes the right password works.
	w.advance(t, 6*time.Minute)
	r := login(a2.client(), u.pass)
	wantStatus(t, "correct password after the lockout ended", r, 200)
	var acc struct{ Username, Role string }
	r.json(t, &acc)
	if acc.Username != u.name || acc.Role != "user" {
		t.Errorf("sign-in answered %+v", acc)
	}
}

// #1202 B2.10: password right and a second factor needed answers 200
// {secondFactor:["totp"]} with a pending-login cookie and no session; the
// factor step with a valid code gives 200 {username, role} and the session
// cookie; a wrong code is 401 invalid-credentials; an expired pending login
// is 401 step-expired.
func TestSecondFactorStepAcceptsAValidCodeAndRefusesWrongAndExpired(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	admin := w.admin
	first := func() (*client, resp) {
		c := w.a.client()
		return c, c.post("/api/auth/login", map[string]string{"username": admin.name, "password": admin.pass})
	}

	c, r := first()
	wantStatus(t, "password step", r, 200)
	var ch struct {
		SecondFactor []string `json:"secondFactor"`
	}
	r.json(t, &ch)
	if len(ch.SecondFactor) != 1 || ch.SecondFactor[0] != "totp" {
		t.Errorf("secondFactor = %v, want [totp]", ch.SecondFactor)
	}
	if len(r.Cookies) == 0 {
		t.Error("the password step set no pending-login cookie")
	}
	if r.cookie("mikroview_session") != nil {
		t.Error("the password step already set a session cookie")
	}
	if s := c.session(t); s.Authenticated {
		t.Error("the caller is signed in after the password step alone")
	}

	// Wrong code.
	wantProblem(t, "wrong code", c.post("/api/auth/login/factor", map[string]string{"code": "000000"}), 401, "invalid-credentials")

	// Right code on a fresh sign-in.
	c, _ = first()
	r = c.post("/api/auth/login/factor", map[string]string{"code": admin.code(t, w.a, 1)})
	wantStatus(t, "valid code", r, 200)
	var acc struct{ Username, Role string }
	r.json(t, &acc)
	if acc.Username != admin.name || acc.Role != "admin" {
		t.Errorf("factor step answered %+v", acc)
	}
	if r.cookie("mikroview_session") == nil {
		t.Error("the factor step set no session cookie")
	}
	if s := c.session(t); !s.Authenticated {
		t.Error("not signed in after a valid code")
	}

	// Expired pending login: the cookie outlives its welcome.
	c, _ = first()
	w.advance(t, 11*time.Minute)
	wantProblem(t, "pending login past its time", c.post("/api/auth/login/factor", map[string]string{"code": admin.code(t, w.a, 0)}), 401, "step-expired")
}

// #1202 B2.11: five wrong second-factor codes in a row set mustChangePassword
// on the account (the session body shows it; X-Auth-Gate:
// must-change-password on the next request). Paired with the account not
// being held before.
func TestFiveWrongSecondFactorCodesForceAPasswordChange(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.enrolled(t, "factor-user", "user")

	// Before: an ordinary sign-in is not held.
	c, r := w.signIn(t, u, u.code(t, w.a, 1))
	wantStatus(t, "ordinary sign-in", r, 200)
	if s := c.session(t); s.MustChangePassword {
		t.Fatal("the account is already marked must-change before any wrong code")
	}

	c = w.a.client()
	wantStatus(t, "password step", c.post("/api/auth/login", map[string]string{"username": u.name, "password": u.pass}), 200)
	for i := 0; i < 5; i++ {
		r := c.post("/api/auth/login/factor", map[string]string{"code": "000000"})
		if r.Status != 401 && r.Status != 429 {
			t.Fatalf("wrong code %d: %s, want 401 (or 429 at the limit)", i+1, r)
		}
	}

	// The five failures may also lock the sign-in; let any lockout pass.
	w.advance(t, 6*time.Minute)
	c, r = w.signIn(t, u, u.recovery(t))
	wantStatus(t, "sign-in with a recovery code afterwards", r, 200)
	s := c.session(t)
	if !s.MustChangePassword {
		t.Errorf("session = %+v, want mustChangePassword true after five wrong codes", s)
	}
	r = c.get("/api/devices")
	wantProblem(t, "any other route", r, 403, "must-change-password")
	if got := r.H.Get("X-Auth-Gate"); got != "must-change-password" {
		t.Errorf("X-Auth-Gate = %q, want must-change-password", got)
	}
}

// #1202 B2.12: with secureCookie true the session cookie is named
// __Host-mikroview_session, Secure, HttpOnly, SameSite=Lax, Path=/, Max-Age
// equal to the session ceiling in seconds; with secureCookie false it is
// mikroview_session.
func TestSessionCookieAttributesFollowSecureCookieAndTheCeiling(t *testing.T) {
	t.Run("secure cookie on, over https", func(t *testing.T) {
		t.Parallel()
		a := mustStart(t, Opts{TLS: true})
		_, r := register(t, a, "mv-admin", adminPassword)
		wantStatus(t, "register", r, 201)
		c := r.cookie("__Host-mikroview_session")
		if c == nil {
			t.Fatalf("no __Host-mikroview_session cookie; got %v", r.Cookies)
		}
		if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
			t.Errorf("cookie = %+v, want Secure, HttpOnly, SameSite=Lax, Path=/", c)
		}
		if c.Domain != "" {
			t.Errorf("cookie Domain = %q; a __Host- cookie has none", c.Domain)
		}
		if c.MaxAge != 24*60*60 {
			t.Errorf("Max-Age = %d, want the 24h session ceiling (86400)", c.MaxAge)
		}
		if r.cookie("mikroview_session") != nil {
			t.Error("the unprefixed cookie was also set")
		}
	})
	t.Run("secure cookie off, plain http, shorter ceiling", func(t *testing.T) {
		t.Parallel()
		a := mustStart(t, Opts{SessionMax: "2h"})
		_, r := register(t, a, "mv-admin", adminPassword)
		wantStatus(t, "register", r, 201)
		c := r.cookie("mikroview_session")
		if c == nil {
			t.Fatalf("no mikroview_session cookie; got %v", r.Cookies)
		}
		if c.Secure {
			t.Error("the cookie is Secure although secureCookie is false")
		}
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
			t.Errorf("cookie = %+v, want HttpOnly, SameSite=Lax, Path=/", c)
		}
		if c.MaxAge != 2*60*60 {
			t.Errorf("Max-Age = %d, want 7200 (the configured 2h ceiling), not the old 30 days", c.MaxAge)
		}
		if r.cookie("__Host-mikroview_session") != nil {
			t.Error("a __Host- cookie was set with secureCookie off")
		}
	})
}

// #1202 B2.13: a request without a session to a protected route is 401
// sign-in-required with WWW-Authenticate: Bearer realm="gate"; a non-GET
// without X-Requested-With: mikroview is 403 csrf-required even on
// /api/auth/login.
func TestUnauthenticatedAndCSRFRefusalsAreProblemDetails(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	anon := w.a.client()

	r := anon.get("/api/devices")
	wantProblem(t, "GET /api/devices without a session", r, 401, "sign-in-required")
	if got := r.H.Get("WWW-Authenticate"); got != `Bearer realm="gate"` {
		t.Errorf(`WWW-Authenticate = %q, want Bearer realm="gate"`, got)
	}

	noHeader := w.a.client()
	noHeader.noCSRF = true
	wantProblem(t, "POST /api/auth/login without the CSRF header",
		noHeader.post("/api/auth/login", map[string]string{"username": w.admin.name, "password": w.admin.pass}), 403, "csrf-required")
	wantProblem(t, "POST /api/tokens without the CSRF header",
		noHeader.post("/api/tokens", map[string]string{"name": "x", "password": "x"}), 403, "csrf-required")

	// Paired: the same login with the header is judged on its merits.
	wantProblem(t, "POST /api/auth/login with the header, wrong password",
		anon.post("/api/auth/login", map[string]string{"username": w.admin.name, "password": "wrong-password-1"}), 401, "invalid-credentials")
}

// #1202 B2.14: a session issued before a password change is refused after
// the change; POST /api/auth/logout-all ends the user's other sessions and
// issues a new cookie to the caller.
func TestPasswordChangeAndLogoutAllEndTheOtherSessions(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.enrolled(t, "walker", "user")

	signIn := func() *client {
		c, r := w.signIn(t, u, u.recovery(t))
		wantStatus(t, "sign-in", r, 200)
		return c
	}
	oldSession, keeper := signIn(), signIn()
	if !oldSession.session(t).Authenticated || !keeper.session(t).Authenticated {
		t.Fatal("test setup: both sessions should be live")
	}

	newPass := "a-brand-new-pass-4Kd!"
	r := keeper.post("/api/auth/password", map[string]string{"currentPassword": u.pass, "newPassword": newPass})
	wantStatus(t, "change password", r, 200)
	if r.cookie("mikroview_session") == nil {
		t.Error("the password change issued the caller no fresh session cookie")
	}
	u.pass = newPass
	if oldSession.session(t).Authenticated {
		t.Error("a session issued before the password change still works")
	}
	wantProblem(t, "old session on a protected route", oldSession.get("/api/devices"), 401, "sign-in-required")
	if !keeper.session(t).Authenticated {
		t.Error("the caller was signed out by their own password change")
	}

	// logout-all.
	other, caller := signIn(), signIn()
	r = caller.post("/api/auth/logout-all", map[string]string{"password": u.pass})
	wantStatus(t, "logout-all", r, 200)
	if r.cookie("mikroview_session") == nil {
		t.Error("logout-all issued the caller no new session cookie")
	}
	if other.session(t).Authenticated {
		t.Error("another session of the same account survived logout-all")
	}
	if !caller.session(t).Authenticated {
		t.Error("the caller of logout-all was signed out")
	}
	wantProblem(t, "logout-all with a wrong password", caller.post("/api/auth/logout-all", map[string]string{"password": "nope-nope-nope"}), 401, "invalid-credentials")
}

// #1202 B2.15: sign-in from a new browser succeeds and sets a
// gate_known_browser cookie; the audit log gains a user.login record whose
// detail mentions the unusual signal; nothing is refused.
func TestSignInFromANewBrowserIsFlaggedInTheAuditLogAndNotRefused(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.enrolled(t, "traveller", "user")

	c1, r := w.signIn(t, u, u.recovery(t))
	wantStatus(t, "first browser", r, 200)
	if r.cookie("gate_known_browser") == nil {
		t.Error("no gate_known_browser cookie after sign-in")
	}
	_ = c1

	// A second browser: an empty cookie jar.
	_, r = w.signIn(t, u, u.recovery(t))
	wantStatus(t, "second, unfamiliar browser (nothing is refused)", r, 200)
	if r.cookie("gate_known_browser") == nil {
		t.Error("no gate_known_browser cookie for the new browser")
	}

	a := w.c.get("/api/audit")
	wantStatus(t, "GET /api/audit", a, 200)
	var any_ any
	if err := json.Unmarshal(a.Body, &any_); err != nil {
		t.Fatalf("audit body: %v\n%s", err, a.Body)
	}
	found := false
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			var text []string
			for _, f := range x {
				if s, ok := f.(string); ok {
					text = append(text, s)
				}
			}
			joined := strings.Join(text, " | ")
			if containsAll(joined, "user.login", u.name, "unusual=") {
				found = true
			}
			for _, f := range x {
				walk(f)
			}
		}
	}
	walk(any_)
	if !found {
		t.Errorf("no user.login audit record for %s mentioning the unusual signal (unusual=...):\n%s", u.name, a.Body)
	}
}
