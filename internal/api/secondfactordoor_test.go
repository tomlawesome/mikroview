// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The forced-enrolment door (#1253): a second factor is mandatory on
// every local account (owner, 2026-09-18, "58 every local account - sso
// handles its own auth, we just respect it"), enforced in requireAuth
// on every request rather than as a one-off migration.
//
// These drive the real HTTP flow rather than calling the middleware, so
// what is pinned is what a browser actually gets. The fixtures in the
// rest of this package enrol a factor precisely so they get past this
// door; these are the tests that stand in front of it on purpose.

const doorRefusal = "this account has no second factor"

// unenrolledUser registers the admin, creates an ordinary account, and
// signs that account in -- leaving it in the one state this door exists
// for: a local password, no factor, nothing enrolled.
func unenrolledUser(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	admin := registerAdmin(t, s, ts)
	postJSON(t, admin, ts.URL+"/api/auth/users",
		createUserRequest{Username: "bilbo", Password: "password12345", Role: "user"}).Body.Close()

	// Not loggedInClient: that helper completes a pending-factor step
	// from the fixture's remembered secret, and the whole point here is
	// an account that has no factor to remember.
	c := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, c, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "bilbo", Password: "password12345"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signing in the unenrolled account = %d, want 200", resp.StatusCode)
	}
	return s, ts, c
}

func TestSecondFactorDoorBlocksALocalAccountWithNoFactor(t *testing.T) {
	_, ts, c := unenrolledUser(t)

	resp, err := c.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("/api/flags for an account with no second factor = %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), doorRefusal) {
		t.Errorf("the refusal was %q, want it to name the missing second factor", strings.TrimSpace(string(body)))
	}
}

// TestSecondFactorDoorAdmitsEveryEnrolmentRoute is the other half of
// the gate, and the one that actually matters: a door nobody can get
// through is not a door. Each of the four routes on
// secondFactorEnrolPaths must be reachable from exactly the state above
// -- otherwise an account in it could never enrol and would be locked
// out of its own deployment for good.
//
// Asserts only that the door did not refuse them. What each route makes
// of the (deliberately empty) body is its own handler's business and is
// covered in totp_test.go and passkey_test.go; a 400 here is a route
// that was reached, which is the whole question.
func TestSecondFactorDoorAdmitsEveryEnrolmentRoute(t *testing.T) {
	_, ts, c := unenrolledUser(t)

	// Spelled out rather than ranged over secondFactorEnrolPaths: a test
	// that reads the same map the gate reads cannot notice a route
	// dropped from it -- the loop simply stops checking that one and
	// still passes. This list is the independent statement of what the
	// door owes an unenrolled account.
	want := []string{
		"/api/auth/totp/enrol",
		"/api/auth/totp/confirm",
		"/api/auth/passkeys/register/begin",
		"/api/auth/passkeys/register/finish",
	}
	for _, path := range want {
		resp := postJSON(t, c, ts.URL+path, map[string]any{})
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), doorRefusal) {
			t.Errorf("%s was refused by the second-factor door (%d) -- an account in this state could never enrol",
				path, resp.StatusCode)
		}
	}
	if len(secondFactorEnrolPaths) != len(want) {
		t.Errorf("secondFactorEnrolPaths holds %d routes, this test knows about %d -- one of them is out of date",
			len(secondFactorEnrolPaths), len(want))
	}
}

func TestSecondFactorDoorOpensOnceAFactorIsEnrolled(t *testing.T) {
	_, ts, c := unenrolledUser(t)

	blocked, err := c.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("precondition: /api/flags = %d before enrolling, want 403", blocked.StatusCode)
	}

	totpEnrolAndConfirm(t, c, ts)

	open, err := c.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Body.Close()
	if open.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(open.Body)
		t.Errorf("/api/flags after enrolling = %d (%s), want 200",
			open.StatusCode, strings.TrimSpace(string(body)))
	}
}

func TestSessionReportsMustEnrolSecondFactor(t *testing.T) {
	_, ts, c := unenrolledUser(t)

	// sessionOf goes through /api/auth/session, which the door has to
	// leave reachable for the frontend to be able to draw the enrolment
	// screen at all -- so this doubles as a check that it does.
	before := sessionOf(t, c, ts)
	if !before.Authenticated {
		t.Fatal("the session is not authenticated, so mustEnrolSecondFactor below would prove nothing")
	}
	if !before.MustEnrolSecondFactor {
		t.Error("mustEnrolSecondFactor = false for a local account with no factor, want true")
	}

	totpEnrolAndConfirm(t, c, ts)

	after := sessionOf(t, c, ts)
	if after.MustEnrolSecondFactor {
		t.Error("mustEnrolSecondFactor stayed true after the account enrolled a factor")
	}
}

// TestSecondFactorDoorExemptsAnSSOProvisionedAccount pins the escape
// the gate reads as user.LocalPassword(). An account provisioned
// through the identity provider has no local password for a factor to
// protect, and the provider does its own authentication -- so the door
// must not hold it, or SSO sign-in would dead-end on an enrolment
// screen the account can never satisfy.
func TestSecondFactorDoorExemptsAnSSOProvisionedAccount(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// doFullOIDCLogin's own client is not returned, and this needs to
	// make a second request as the account it just signed in, so the
	// flow is driven here with a jar this test keeps.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	c := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	loginResp, err := c.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	loc, err := url.Parse(loginResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	idTokenNonce = loc.Query().Get("nonce") // see fakeOIDCProvider.signIDToken
	cb, err := c.Get(ts.URL + "/api/auth/oidc/callback?code=test-code&state=" + loc.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	cb.Body.Close()
	if cb.StatusCode != http.StatusFound {
		t.Fatalf("the SSO callback = %d, want 302 -- no session to test the door with", cb.StatusCode)
	}

	sess := sessionOf(t, c, ts)
	if !sess.Authenticated {
		t.Fatal("the SSO sign-in produced no authenticated session")
	}
	if sess.HasLocalPassword {
		t.Fatal("the SSO-provisioned account has a local password, so it is not the case this test means to cover")
	}
	if sess.MustEnrolSecondFactor {
		t.Error("mustEnrolSecondFactor = true for an SSO-provisioned account, which has no local password to protect")
	}

	resp, err := c.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), doorRefusal) {
			t.Error("the second-factor door held an SSO-provisioned account, which can never satisfy it")
		}
	}
}
