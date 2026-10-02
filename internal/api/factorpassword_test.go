// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// The tests in this file pin #1418 and #1422: adding either kind of
// second factor needs the account's password, not just a session. A
// factor added to an account with none yet becomes its first factor,
// which mints the recovery codes and revokes every other session -- so a
// stolen session cookie that could add one could lock the owner out.

// TestPasskeyRegisterBeginNeedsThePassword is the stolen-session case for
// passkeys (#1418): a signed-in session without the password can neither
// start a registration nor, therefore, store a passkey.
func TestPasskeyRegisterBeginNeedsThePassword(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	for name, body := range map[string]any{
		"a wrong password": passkeyRegisterBeginRequest{Password: "not-the-password"},
		"no password":      struct{}{},
	} {
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/begin", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("register/begin with %s got %d, want 401", name, resp.StatusCode)
		}
	}

	// Nothing was begun, so there is no ceremony a finish could complete.
	resp := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/finish",
		passkeyRegisterFinishRequest{Credential: json.RawMessage(`{}`), Name: "planted"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("register/finish after refused begins got %d, want 401", resp.StatusCode)
	}
	if u, _ := s.Auth.Get(passkeyBilboID(t, s)); len(u.Passkeys) != 0 {
		t.Errorf("bilbo holds %d passkeys after refused begins, want 0", len(u.Passkeys))
	}
}

// TestPasskeyRegisterBeginIsRateLimited proves the password check is
// counted on the password re-check budget, not left as an unthrottled
// guessing oracle behind a stolen session: once the budget is spent,
// even the right password is refused.
func TestPasskeyRegisterBeginIsRateLimited(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	const threshold = 3
	s.LoginLimiter = auth.NewLoginLimiter(threshold, time.Minute)
	for i := 0; i < threshold; i++ {
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/begin",
			passkeyRegisterBeginRequest{Password: "not-the-password"})
		resp.Body.Close()
	}
	resp := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/begin",
		passkeyRegisterBeginRequest{Password: passkeyBilboPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("begin number %d got %d, want 429 once the re-check budget is spent", threshold+1, resp.StatusCode)
	}
}

// TestPasskeyRegisterBeginRefusedForSSOAccount: an SSO-only account is
// never offered a local factor, the same rule
// TestTOTPEnrolRefusedForSSOAccount pins for authenticator apps.
func TestPasskeyRegisterBeginRefusedForSSOAccount(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	u, _, err := s.Auth.FindOrCreateOIDCUser("https://idp.example", "subject-placeholder", "frodo", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sess := s.Sessions.Create(u.ID, time.Now())

	jar := mustCookieJar(t)
	base, _ := url.Parse(ts.URL)
	jar.SetCookies(base, []*http.Cookie{{Name: sessionCookieName, Value: sess.ID, Path: "/"}})
	resp := postJSON(t, &http.Client{Jar: jar}, ts.URL+"/api/auth/passkeys/register/begin",
		passkeyRegisterBeginRequest{Password: "anything"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("register/begin for an SSO-only account got %d, want 409", resp.StatusCode)
	}
}

// TestPasskeyRegisterCookieIsOneShot: one password-proved begin stores at
// most one passkey. A copy of the sealed registration cookie taken
// before the first finish cleared it -- what a cookie thief holds --
// cannot be replayed to store a second passkey of the thief's own.
func TestPasskeyRegisterCookieIsOneShot(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	creation := passkeyRegisterBegin(t, bilbo, ts, passkeyBilboPassword)
	ceremonyURL, _ := url.Parse(ts.URL + passkeyRegisterCookiePath + "/register/finish")
	var stolen *http.Cookie
	for _, c := range bilbo.Jar.Cookies(ceremonyURL) {
		if c.Name == passkeyRegisterCookieName {
			stolen = c
		}
	}
	if stolen == nil {
		t.Fatal("register/begin set no registration cookie to copy")
	}

	owner := NewFakeAuthenticator(s.RelyingParty.RPID, s.RelyingParty.Origin)
	passkeyRegisterFinishOK(t, bilbo, ts, owner, creation, "owner's key")

	// The first finish rotated bilbo's session (first factor), so the
	// thief is given the current one along with the stale ceremony.
	bilbo.Jar.SetCookies(ceremonyURL, []*http.Cookie{{Name: passkeyRegisterCookieName, Value: stolen.Value, Path: passkeyRegisterCookiePath}})
	thief := NewFakeAuthenticator(s.RelyingParty.RPID, s.RelyingParty.Origin)
	resp := passkeyRegisterFinishRaw(t, bilbo, ts, thief, creation, "thief's key")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("replaying a spent registration cookie got %d, want 401", resp.StatusCode)
	}
	if u, _ := s.Auth.Get(passkeyBilboID(t, s)); len(u.Passkeys) != 1 {
		t.Errorf("bilbo holds %d passkeys after one begin, want 1", len(u.Passkeys))
	}
}

// TestTOTPEnrolNeedsThePassword is #1422, the same hole on the other
// factor: without the password no secret is issued, so there is nothing
// for confirm to activate.
func TestTOTPEnrolNeedsThePassword(t *testing.T) {
	s, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)

	for name, body := range map[string]any{
		"a wrong password": totpEnrolRequest{Password: "not-the-password"},
		"no password":      struct{}{},
	} {
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/enrol", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("enrol with %s got %d, want 401", name, resp.StatusCode)
		}
	}
	u, ok := s.Auth.Get(totpBilboID(t, s))
	if !ok {
		t.Fatal("bilbo account vanished")
	}
	if u.HasSecondFactor() || u.TOTPSecret != "" {
		t.Error("a refused enrol must leave no secret, pending or active, on the account")
	}
}

// TestTOTPEnrolIsRateLimited mirrors TestPasskeyRegisterBeginIsRateLimited.
func TestTOTPEnrolIsRateLimited(t *testing.T) {
	s, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)

	const threshold = 3
	s.LoginLimiter = auth.NewLoginLimiter(threshold, time.Minute)
	for i := 0; i < threshold; i++ {
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/enrol", totpEnrolRequest{Password: "not-the-password"})
		resp.Body.Close()
	}
	resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/enrol", totpEnrolRequest{Password: totpBilboPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("enrol number %d got %d, want 429 once the re-check budget is spent", threshold+1, resp.StatusCode)
	}
}
