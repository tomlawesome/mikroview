// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
)

// Obvious placeholder, never anything shaped like a credential somebody
// might really hold -- same convention as resetpassword_test.go's own
// constants.
const totpBilboPassword = "bilbo-totp-password-placeholder"

const totpBilboUsername = "bilbo"

// totpCounterNow mirrors totp.go's own unexported totpStep (30 seconds,
// RFC 6238's default and the only value #1249 builds against) without
// reaching into internal/auth for it -- this package only ever needs the
// counter to hand to auth.GenerateTOTPCode/auth.VerifyTOTP, both of
// which are already exported for exactly this.
func totpCounterNow(now time.Time) uint64 {
	return uint64(now.Unix()) / 30
}

// totpTestServer stands up an auth-enabled server holding an admin and
// one ordinary account ("bilbo", user role, no factor yet), mirroring
// resetpassword_test.go's resetTestServer.
func totpTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	admin := registerAdmin(t, ts)
	postJSON(t, admin, ts.URL+"/api/auth/users",
		createUserRequest{Username: totpBilboUsername, Password: totpBilboPassword, Role: "user"}).Body.Close()
	return s, ts, admin
}

func totpBilboID(t *testing.T, s *Server) string {
	t.Helper()
	for _, u := range s.Auth.List() {
		if u.Username == totpBilboUsername {
			return u.ID
		}
	}
	t.Fatal("the bilbo account the TOTP tests act on was not created")
	return ""
}

// totpEnrol posts the enrol step and decodes its response, failing the
// test on anything but 200.
func totpEnrol(t *testing.T, client *http.Client, ts *httptest.Server) totpEnrolResponse {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/auth/totp/enrol", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("enrol returned %d: %s", resp.StatusCode, body)
	}
	var out totpEnrolResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// totpEnrolAndConfirm drives enrol+confirm end to end for client
// (already signed in, holding no active factor yet), computing a code
// from the just-returned secret for whatever moment the test happens to
// run at. Returns the decoded secret, the ten recovery codes confirm
// hands back, and the counter the confirming code was generated at.
//
// That counter matters to callers that go on to drive a login/factor
// call of their own: TOTPLastCounter is now set to it, so a *further*
// code has to be generated at counter+1 (or later), never by reading the
// wall clock again -- confirm and the next call typically land in the
// same 30-second step, and a code regenerated from "now" a second time
// is then numerically identical to the one that just confirmed the
// factor, which the replay guard correctly (and confusingly, for a test
// that didn't expect it) refuses.
func totpEnrolAndConfirm(t *testing.T, client *http.Client, ts *httptest.Server) ([]byte, []string, uint64) {
	t.Helper()
	enrolled := totpEnrol(t, client, ts)
	secret, err := auth.DecodeTOTPSecret(enrolled.Secret)
	if err != nil {
		t.Fatalf("decoding the enrolled secret: %v", err)
	}
	counter := totpCounterNow(time.Now())
	code := auth.GenerateTOTPCode(secret, counter)

	resp := postJSON(t, client, ts.URL+"/api/auth/totp/confirm", totpConfirmRequest{Code: code})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("confirm returned %d: %s", resp.StatusCode, body)
	}
	var out totpConfirmResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || len(out.RecoveryCodes) != 10 {
		t.Fatalf("confirm response = %+v, want enabled with 10 recovery codes", out)
	}
	return secret, out.RecoveryCodes, counter
}

// startTOTPLogin posts the password step for an account that holds an
// active factor and asserts the shape #1249 requires -- no session,
// {"secondFactor":["totp"]} -- returning the client for the caller to
// carry on with against /api/auth/login/factor.
func startTOTPLogin(t *testing.T, ts *httptest.Server, username, password string) *http.Client {
	t.Helper()
	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: username, Password: password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("password step returned %d: %s", resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	factors, _ := out["secondFactor"].([]any)
	if len(factors) != 1 || factors[0] != "totp" {
		t.Fatalf("password step response = %v, want exactly {\"secondFactor\":[\"totp\"]}", out)
	}
	return client
}

func submitLoginFactor(t *testing.T, client *http.Client, ts *httptest.Server, code string) *http.Response {
	t.Helper()
	return postJSON(t, client, ts.URL+"/api/auth/login/factor", loginFactorRequest{Code: code})
}

// findAuditEntry returns the first entry recording action, failing the
// test if none exists.
func findAuditEntry(t *testing.T, admin *http.Client, ts *httptest.Server, action string) audit.Entry {
	t.Helper()
	res := fetchAudit(t, admin, ts)
	for _, e := range res.Entries {
		if e.Action == action {
			return e
		}
	}
	t.Fatalf("no %s audit entry was recorded", action)
	return audit.Entry{}
}

// TestTOTPEnrolConfirmLoginFactorAndDelete is the happy path end to end:
// enrol, confirm (which hands back ten recovery codes and does not sign
// the enrolling browser out), a fresh browser stopping at the password
// step and then completing with a code, and finally turning the factor
// off again with the password.
func TestTOTPEnrolConfirmLoginFactorAndDelete(t *testing.T) {
	s, ts, admin := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)

	secret, codes, confirmCounter := totpEnrolAndConfirm(t, bilbo, ts)
	if len(codes) != 10 {
		t.Fatalf("got %d recovery codes, want 10", len(codes))
	}

	// Confirming reissues this browser's own session (SessionStore.
	// RevokeAllForUser has no notion of "except the caller") -- it must
	// still read as signed in, not logged out by its own factor going
	// live.
	if sess := sessionOf(t, bilbo, ts); !sess.Authenticated {
		t.Fatal("confirming TOTP should not have signed this browser out")
	}
	entry := findAuditEntry(t, admin, ts, "account.totp_enabled")
	if entry.Actor != totpBilboUsername || entry.Target != totpBilboUsername {
		t.Errorf("account.totp_enabled entry = %+v, want actor/target %q", entry, totpBilboUsername)
	}

	// A fresh browser presenting only the password stops short of a
	// session.
	pending := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	protected, err := pending.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	protected.Body.Close()
	if protected.StatusCode != http.StatusUnauthorized {
		t.Errorf("the password-only step reached a protected route with %d, want 401", protected.StatusCode)
	}

	code := auth.GenerateTOTPCode(secret, confirmCounter+1)
	resp := submitLoginFactor(t, pending, ts, code)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login/factor returned %d, want 200", resp.StatusCode)
	}
	if sess := sessionOf(t, pending, ts); !sess.Authenticated {
		t.Fatal("expected the code step to establish a session")
	}

	del := deleteJSON(t, bilbo, ts.URL+"/api/auth/totp", totpDeleteRequest{Password: totpBilboPassword})
	del.Body.Close()
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete returned %d", del.StatusCode)
	}
	if s.Auth.HasActiveTOTP(totpBilboID(t, s)) {
		t.Error("expected the factor to be gone after DELETE /api/auth/totp")
	}
	deleteEntry := findAuditEntry(t, admin, ts, "account.totp_disabled")
	if deleteEntry.Actor != totpBilboUsername {
		t.Errorf("account.totp_disabled entry = %+v, want actor %q", deleteEntry, totpBilboUsername)
	}

	// An ordinary password login works again, one step, no factor
	// requested.
	plain := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	if sess := sessionOf(t, plain, ts); !sess.Authenticated {
		t.Error("expected a plain password login to work once the factor was removed")
	}
}

// TestPasswordOnlyLoginOnFactorAccountNeverCreatesSession is #1249's own
// "single most important test in the milestone": a correct password on
// an account holding an active factor must not, under any
// circumstances, establish a session.
//
// Proved able to fail: temporarily replacing handleAuthLogin's
// `if user.HasActiveTOTP() { ...; return }` branch with nothing (so the
// handler always fell through to creating a session) made this test fail
// with "a correct password on a factor account established a session",
// exactly the defect it exists to catch. Restored before committing.
func TestPasswordOnlyLoginOnFactorAccountNeverCreatesSession(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)

	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/login",
		credentialsRequest{Username: totpBilboUsername, Password: totpBilboPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the password step itself returned %d, want 200: %s", resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if _, hasUsername := out["username"]; hasUsername {
		t.Errorf("the password step's response carried a username -- that shape means a session was created: %v", out)
	}

	sess := sessionOf(t, client, ts)
	if sess.Authenticated {
		t.Fatal("a correct password on an account with an active factor established a session -- #1249's own most important test")
	}

	protected, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	protected.Body.Close()
	if protected.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d from a protected route after a password-only login on a factor account, want 401", protected.StatusCode)
	}
}

// TestTOTPReplayOfSameCodeRefused proves the replay guard actually holds
// end to end -- not just that VerifyTOTP refuses a spent counter in
// isolation, but that handleAuthLoginFactor really calls
// RecordTOTPCounter and persists it.
//
// Proved able to fail: temporarily removing the
// `s.Auth.RecordTOTPCounter(user.ID, matched)` call from
// handleAuthLoginFactor's success branch made the second (replay) login
// in this test succeed with 200 instead of being refused. Restored
// before committing.
func TestTOTPReplayOfSameCodeRefused(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	secret, _, confirmCounter := totpEnrolAndConfirm(t, bilbo, ts)

	code := auth.GenerateTOTPCode(secret, confirmCounter+1)

	first := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	resp := submitLoginFactor(t, first, ts, code)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the first use of the code returned %d, want 200", resp.StatusCode)
	}

	second := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	replay := submitLoginFactor(t, second, ts, code)
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusUnauthorized {
		t.Errorf("replaying the same code returned %d, want 401", replay.StatusCode)
	}
	if sess := sessionOf(t, second, ts); sess.Authenticated {
		t.Error("a replayed code must not establish a session")
	}
}

// TestTOTPWindowToleranceContract pins the exact tolerance
// handleAuthLoginFactor and handleTOTPConfirm both depend on:
// auth.VerifyTOTP accepts a code one 30-second step either side of now,
// and refuses one two steps out. internal/auth's own tests already cover
// VerifyTOTP as a unit; this is the consumer-side check that the
// contract the routes are built on hasn't silently moved.
func TestTOTPWindowToleranceContract(t *testing.T) {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	encoded := auth.EncodeTOTPSecret(secret)
	now := time.Now()
	cur := int64(totpCounterNow(now))

	for _, delta := range []int64{-1, 0, 1} {
		code := auth.GenerateTOTPCode(secret, uint64(cur+delta))
		if _, ok := auth.VerifyTOTP(encoded, code, now, 0); !ok {
			t.Errorf("a code %+d step(s) from now was refused, want accepted", delta)
		}
	}
	for _, delta := range []int64{-2, 2} {
		code := auth.GenerateTOTPCode(secret, uint64(cur+delta))
		if _, ok := auth.VerifyTOTP(encoded, code, now, 0); ok {
			t.Errorf("a code %+d steps from now was accepted, want refused", delta)
		}
	}
}

// TestTOTPRecoveryCodeSingleUse proves a recovery code works once and is
// then dead, driven through the real login/factor route rather than
// internal/auth's store API directly.
//
// Proved able to fail: temporarily changing the recovery branch in
// handleAuthLoginFactor from `else if burned` to unconditionally
// complete the login whenever BurnRecoveryCode returned no error
// (ignoring its bool) made the second use of the same code in this test
// succeed. Restored before committing.
func TestTOTPRecoveryCodeSingleUse(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	_, codes, _ := totpEnrolAndConfirm(t, bilbo, ts)
	code := codes[0]

	first := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	resp := submitLoginFactor(t, first, ts, code)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the first use of the recovery code returned %d, want 200", resp.StatusCode)
	}

	second := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	reuse := submitLoginFactor(t, second, ts, code)
	defer reuse.Body.Close()
	if reuse.StatusCode != http.StatusUnauthorized {
		t.Errorf("reusing a spent recovery code returned %d, want 401", reuse.StatusCode)
	}
	if sess := sessionOf(t, second, ts); sess.Authenticated {
		t.Error("a spent recovery code must not establish a session")
	}
}

// TestPendingLoginCookieExpiry proves the 5-minute bound is enforced by
// the route, not just documented -- a cookie forged with an IssuedAt
// outside the window is refused even with an otherwise-untouched, live
// factor behind it; a cookie forged with a fresh IssuedAt and the right
// code still works, which is what proves the first refusal is really
// about age and not a broken codec.
//
// Proved able to fail: temporarily changing the age check in
// pendingLoginStateCodec.decode from
// `now.Sub(st.IssuedAt) > pendingLoginCookieMaxAge` to `false` made the
// stale-cookie request below succeed with 200 instead of 401. Restored
// before committing.
func TestPendingLoginCookieExpiry(t *testing.T) {
	s, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	secret, _, confirmCounter := totpEnrolAndConfirm(t, bilbo, ts)
	id := totpBilboID(t, s)

	// The stale request carries a genuinely correct code -- generated at
	// confirmCounter+1, same as the fresh check below, just never spent
	// -- so a 401 here can only be about the cookie's age. Using a
	// deliberately wrong code instead would pass this assertion for the
	// wrong reason: a bad code is refused regardless of whether the
	// expiry check does anything at all, which is exactly the gap that
	// let this test pass once already while the guard-break comment
	// above proves it should not have.
	staleCode := auth.GenerateTOTPCode(secret, confirmCounter+1)
	stale, err := pendingLoginCodec.encode(pendingLoginState{UserID: id, IssuedAt: time.Now().Add(-6 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	staleReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login/factor",
		strings.NewReader(`{"code":"`+staleCode+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	staleReq.AddCookie(&http.Cookie{Name: pendingLoginCookieName, Value: stale})
	staleResp, err := (&http.Client{}).Do(staleReq)
	if err != nil {
		t.Fatal(err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a 6-minute-old pending-login cookie carrying a correct code got %d, want 401", staleResp.StatusCode)
	}

	// Same counter (+1) as staleCode above: with the expiry guard
	// working, the stale request is refused before the code is ever
	// checked, so it is never actually consumed and this one is still
	// good. (It only needs to differ from confirmCounter itself, which
	// confirm already spent.)
	fresh, err := pendingLoginCodec.encode(pendingLoginState{UserID: id, IssuedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	code := auth.GenerateTOTPCode(secret, confirmCounter+1)
	freshReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login/factor",
		strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	freshReq.Header.Set("Content-Type", "application/json")
	freshReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	freshReq.AddCookie(&http.Cookie{Name: pendingLoginCookieName, Value: fresh})
	freshResp, err := (&http.Client{}).Do(freshReq)
	if err != nil {
		t.Fatal(err)
	}
	defer freshResp.Body.Close()
	if freshResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(freshResp.Body)
		t.Errorf("a fresh pending-login cookie with the right code got %d, want 200: %s", freshResp.StatusCode, body)
	}
}

// TestLoginFactorWithoutPendingCookie covers the case of hitting the
// second step cold, with no cookie at all -- not exercised by anything
// above, which always goes through startTOTPLogin first.
func TestLoginFactorWithoutPendingCookie(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/login/factor", loginFactorRequest{Code: "000000"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no pending cookie at all got %d, want 401", resp.StatusCode)
	}
}

// TestAdminCannotClearOwnTOTP proves the self-clear refusal #1249 asks
// for: `mikroview -clear-second-factor` at the console is the only path
// for an admin's own lost phone.
//
// Proved able to fail: temporarily removing the
// `caller.ID == id` check from handleTOTPAdminClear made this request
// return 200 instead of 409. Restored before committing.
func TestAdminCannotClearOwnTOTP(t *testing.T) {
	s, ts, admin := totpTestServer(t)
	var adminID string
	for _, u := range s.Auth.List() {
		if u.Role == auth.RoleAdmin {
			adminID = u.ID
		}
	}
	if adminID == "" {
		t.Fatal("no admin account")
	}

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+adminID+"/totp", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("an admin clearing their own factor got %d, want 409", resp.StatusCode)
	}
}

// TestTOTPAdminClearHappyPath is the Users-group path for a colleague's
// lost phone: an admin clears it, the removal is audited by name, and
// the account signs in with just its password afterward.
func TestTOTPAdminClearHappyPath(t *testing.T) {
	s, ts, admin := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)
	id := totpBilboID(t, s)

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+id+"/totp", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin clear returned %d: %s", resp.StatusCode, body)
	}
	if s.Auth.HasActiveTOTP(id) {
		t.Error("expected the factor to be cleared")
	}

	entry := findAuditEntry(t, admin, ts, "user.totp_cleared")
	if entry.Target != totpBilboUsername {
		t.Errorf("user.totp_cleared entry target = %q, want %q", entry.Target, totpBilboUsername)
	}

	plain := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	if sess := sessionOf(t, plain, ts); !sess.Authenticated {
		t.Error("expected a plain password login to work once the admin cleared the factor")
	}
}

// TestTOTPAdminClearRefusals covers the ordinary caller-tier and target
// refusals, mirroring resetpassword_test.go's TestAdminResetRefusals for
// the sibling route.
func TestTOTPAdminClearRefusals(t *testing.T) {
	t.Run("a user-tier caller may not clear someone else's factor", func(t *testing.T) {
		s, ts, _ := totpTestServer(t)
		bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
		totpEnrolAndConfirm(t, bilbo, ts)
		id := totpBilboID(t, s)

		if _, err := s.Auth.CreateUser("operator", "operator-password-placeholder", auth.RoleUser, time.Now()); err != nil {
			t.Fatal(err)
		}
		operator := loggedInClient(t, ts.URL, "operator", "operator-password-placeholder")

		resp := deleteJSON(t, operator, ts.URL+"/api/auth/users/"+id+"/totp", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("a user-tier caller got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("no such account", func(t *testing.T) {
		_, ts, admin := totpTestServer(t)
		resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/not-a-real-user-id/totp", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("a nonexistent target got %d, want 404", resp.StatusCode)
		}
	})
}

// TestTOTPEnrolRefusedForSSOAccount proves #1249's rule that SSO
// accounts are never offered a local factor: their identity provider
// owns identity, and an SSO-only account has no local password for
// DELETE /api/auth/totp to gate removal behind, which is exactly the
// lockout ErrTOTPAlreadyActive-style reasoning warns about.
func TestTOTPEnrolRefusedForSSOAccount(t *testing.T) {
	s, ts, _ := totpTestServer(t)
	// A second (issuer, subject) so this doesn't become the first
	// account and get RoleAdmin -- totpTestServer already registered the
	// admin.
	u, _, err := s.Auth.FindOrCreateOIDCUser("https://idp.example", "subject-placeholder", "frodo", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sess := s.Sessions.Create(u.ID, time.Now())

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/totp/enrol", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("enrol for an SSO-only account got %d, want 409", resp.StatusCode)
	}
}

// TestTOTPEnrolConflictWhenAlreadyActive proves re-enrolling over a
// confirmed factor is refused rather than silently replacing it --
// ErrTOTPAlreadyActive's own doc comment explains why a silent replace
// is a lockout waiting to happen.
func TestTOTPEnrolConflictWhenAlreadyActive(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)

	resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/enrol", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("enrolling again while active got %d, want 409", resp.StatusCode)
	}
}

// TestTOTPConfirmRejectsBadCode covers the ordinary wrong-code case
// (distinct from ErrNoPendingTOTP, which needs no pending secret at
// all). "000000" is not derived from the account's real secret, so it
// is wrong with overwhelming probability -- the same one-in-a-million
// acceptable flake every fixed-code TOTP test carries.
func TestTOTPConfirmRejectsBadCode(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrol(t, bilbo, ts)

	resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/confirm", totpConfirmRequest{Code: "000000"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a wrong code got %d, want 400", resp.StatusCode)
	}
}

// TestTOTPDeleteWrongPassword proves the factor survives a wrong-
// password attempt to remove it, and that the attempt is rate-limited on
// passwordRecheckLimiterKey rather than left as an unthrottled oracle
// behind a stolen session.
func TestTOTPDeleteWrongPassword(t *testing.T) {
	s, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)
	id := totpBilboID(t, s)

	resp := deleteJSON(t, bilbo, ts.URL+"/api/auth/totp", totpDeleteRequest{Password: "not-the-password"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong password got %d, want 401", resp.StatusCode)
	}
	if !s.Auth.HasActiveTOTP(id) {
		t.Error("the factor must survive a wrong-password attempt to remove it")
	}
}
