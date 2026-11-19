// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1426 MOVES-M1: the SSO link and callback guards that used to be pinned in
// mikroview and now live in the external gauntlet/oidc package. They are
// asserted here through the real binary so a change to mikroview's wiring
// (CSRF middleware order, the second-factor door, session rotation) that
// breaks one fails CI on this side.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (c *client) sessionCookieValue() string {
	u, _ := url.Parse(c.a.base)
	for _, ck := range c.jar.Cookies(u) {
		if ck.Name == "mikroview_session" {
			return ck.Value
		}
	}
	return ""
}

// withSessionCookie is a fresh client carrying only the given session cookie.
func (a *app) withSessionCookie(value string) *client {
	c := a.client()
	u, _ := url.Parse(a.base)
	c.jar.SetCookies(u, []*http.Cookie{{Name: "mikroview_session", Value: value, Path: "/"}})
	return c
}

func linkURL(t *testing.T, c *client, pass, query string) string {
	t.Helper()
	r := c.post("/api/auth/oidc/link"+query, map[string]string{"password": pass})
	wantStatus(t, "oidc/link", r, 200)
	var out struct{ URL string }
	r.json(t, &out)
	if out.URL == "" {
		t.Fatalf("oidc/link returned no url: %s", r)
	}
	return out.URL
}

// The link door: refused without the CSRF header, without a session, and for
// an account that is already SSO-only; and it links the session's account,
// never one named in the body.
func TestSSOLinkDoorRefusesNoCSRFNoSessionAndSSOOnlyAndLinksTheSessionAccount(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	u := w.enrolled(t, "door-user", "user")
	victim := w.enrolled(t, "door-victim", "user")

	c, r := w.signIn(t, u, u.recovery(t))
	wantStatus(t, "sign-in", r, 200)

	// Without the CSRF header.
	c.noCSRF = true
	r = c.post("/api/auth/oidc/link", map[string]string{"password": u.pass})
	c.noCSRF = false
	if r.Status != 403 {
		t.Errorf("link without the CSRF header = %s, want 403", r)
	}
	// Without a session.
	r = w.a.client().post("/api/auth/oidc/link", map[string]string{"password": u.pass})
	wantStatus(t, "link without a session", r, 401)

	// The target is the session's account; a name in the request changes nothing.
	idp.who("subject-door", "door-user")
	loc := linkURL(t, c, u.pass, "?username="+victim.name+"&userId=x&id=x")
	wantRedirect(t, "link callback", idp.finish(t, c, loc), "/?ssoLinked=1")
	if row := userRow(t, w.c, "door-user"); row == nil || row["sso"] != true {
		t.Errorf("the session's own account was not linked: %v", row)
	}
	if row := userRow(t, w.c, "door-victim"); row == nil || row["sso"] == true {
		t.Errorf("an account named in the body was linked: %v", row)
	}

	// Now SSO-only: a second link is refused with 409.
	r = c.post("/api/auth/oidc/link", map[string]string{"password": u.pass})
	if r.Status != 409 {
		t.Errorf("link for an already SSO-only account = %s, want 409", r)
	}
}

// The browser signs in as someone else between link and callback: the
// identity is linked to neither account.
func TestSSOLinkSessionChangedMidFlowLinksNeitherAccount(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	a := w.enrolled(t, "flow-a", "user")
	b := w.enrolled(t, "flow-b", "user")

	c, r := w.signIn(t, a, a.recovery(t))
	wantStatus(t, "sign-in as a", r, 200)
	loc := linkURL(t, c, a.pass, "")

	// The same browser signs in as b.
	r = c.post("/api/auth/login", map[string]string{"username": b.name, "password": b.pass})
	wantStatus(t, "sign-in as b in the same browser", r, 200)
	r = c.post("/api/auth/login/factor", map[string]string{"code": b.recovery(t)})
	wantStatus(t, "b second factor", r, 200)

	idp.who("subject-flow", "flow-a")
	wantRedirect(t, "callback after the session changed", idp.finish(t, c, loc), "/?ssoError=link_session_changed")
	for _, n := range []string{"flow-a", "flow-b"} {
		if row := userRow(t, w.c, n); row == nil || row["sso"] == true {
			t.Errorf("%s was linked after a changed session: %v", n, row)
		}
	}
}

// A link rotates the session: the cookie from before the link is dead.
func TestSSOLinkRotatesTheSessionCookie(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	u := w.enrolled(t, "rotate-user", "user")
	c, r := w.signIn(t, u, u.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	before := c.sessionCookieValue()
	if before == "" {
		t.Fatal("test setup: no session cookie after sign-in")
	}
	idp.who("subject-rotate", "rotate-user")
	wantRedirect(t, "link", idp.finish(t, c, linkURL(t, c, u.pass, "")), "/?ssoLinked=1")
	if after := c.sessionCookieValue(); after == "" || after == before {
		t.Errorf("session cookie after the link = %q, want a new value (was %q)", after, before)
	}
	if !c.session(t).Authenticated {
		t.Error("the new session is not live")
	}
	if w.a.withSessionCookie(before).session(t).Authenticated {
		t.Error("the pre-link session cookie still works")
	}
}

// The callback guards: no flow cookie, wrong state, a provider error; each
// redirects with its code, creates no account and clears the flow cookie.
func TestSSOCallbackRefusesMissingFlowCookieStateMismatchAndProviderError(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	idp.who("subject-guard", "guard-person")

	// No flow cookie at all (a callback in a browser that never began).
	r := w.a.client().get("/api/auth/oidc/callback?code=test-code&state=whatever")
	wantRedirect(t, "callback with no flow cookie", r, "/?ssoError=state_mismatch")
	if r.cookie("mikroview_session") != nil {
		t.Error("a callback with no flow cookie set a session")
	}

	// Wrong state.
	c := w.a.client()
	lr := c.get("/api/auth/oidc/login")
	wantStatus(t, "login", lr, 302)
	if len(lr.Cookies) == 0 {
		t.Fatal("test setup: the login redirect set no flow cookie")
	}
	r = c.get("/api/auth/oidc/callback?code=test-code&state=not-the-state")
	wantRedirect(t, "callback with the wrong state", r, "/?ssoError=state_mismatch")
	if r.cookie("mikroview_session") != nil {
		t.Error("a state mismatch set a session")
	}
	cleared := false
	for _, ck := range r.Cookies {
		for _, fc := range lr.Cookies {
			if ck.Name == fc.Name && (ck.MaxAge < 0 || ck.Value == "") {
				cleared = true
			}
		}
	}
	if !cleared {
		t.Errorf("the flow cookie was not cleared on failure: %v", r.Cookies)
	}

	// Provider error.
	c = w.a.client()
	lr = c.get("/api/auth/oidc/login")
	wantStatus(t, "login", lr, 302)
	state := mustQuery(t, lr.H.Get("Location")).Get("state")
	r = c.get("/api/auth/oidc/callback?error=access_denied&state=" + url.QueryEscape(state))
	wantRedirect(t, "callback with a provider error", r, "/?ssoError=provider_error")

	if userRow(t, w.c, "guard-person") != nil {
		t.Error("a refused callback provisioned an account")
	}
}

func mustQuery(t *testing.T, loc string) url.Values {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// The login redirect carries PKCE S256, a state and a nonce, and goes to the
// provider; each login gets fresh ones.
func TestSSOLoginRedirectCarriesPKCEStateAndNonce(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	r1 := w.a.client().get("/api/auth/oidc/login")
	wantStatus(t, "login", r1, 302)
	loc := r1.H.Get("Location")
	if !strings.HasPrefix(loc, idp.srv.URL+"/authorize") {
		t.Errorf("Location = %q, want the provider's authorize endpoint", loc)
	}
	q := mustQuery(t, loc)
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Errorf("PKCE parameters = %v, want code_challenge_method=S256 and a code_challenge", q)
	}
	if q.Get("state") == "" || q.Get("nonce") == "" {
		t.Errorf("state/nonce missing in %v", q)
	}
	q2 := mustQuery(t, w.a.client().get("/api/auth/oidc/login").H.Get("Location"))
	if q2.Get("state") == q.Get("state") || q2.Get("nonce") == q.Get("nonce") || q2.Get("code_challenge") == q.Get("code_challenge") {
		t.Error("two logins reused a state, nonce or PKCE challenge")
	}
}

// With no provider configured the callback answers 404, like login.
func TestSSOCallbackIs404WhenOIDCIsNotConfigured(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	wantStatus(t, "callback with no provider", w.a.client().get("/api/auth/oidc/callback?code=x&state=y"), 404)
	wantStatus(t, "login with no provider", w.a.client().get("/api/auth/oidc/login"), 404)
}
