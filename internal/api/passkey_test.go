// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// Obvious placeholder, never anything shaped like a credential somebody
// might really hold -- same convention totp_test.go's own constants
// follow.
const (
	passkeyBilboUsername = "bilbo"
	passkeyBilboPassword = "bilbo-passkey-password-placeholder"
)

// passkeyTestServer stands up an auth-enabled server holding an admin and
// one ordinary account ("bilbo", user role, no factor yet), with a ready
// RelyingParty -- mirroring totp_test.go's totpTestServer, plus the
// #1250 piece TOTP never needed.
func passkeyTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	rp, err := NewRelyingParty("https://passkeys.example.org")
	if err != nil {
		t.Fatal(err)
	}
	if rp.Status != PasskeyStatusReady {
		t.Fatalf("test relying party Status = %q, want ready", rp.Status)
	}
	s.RelyingParty = rp

	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	admin := registerAdmin(t, ts)
	postJSON(t, admin, ts.URL+"/api/auth/users",
		createUserRequest{Username: passkeyBilboUsername, Password: passkeyBilboPassword, Role: "user"}).Body.Close()
	return s, ts, admin
}

func passkeyBilboID(t *testing.T, s *Server) string {
	t.Helper()
	for _, u := range s.Auth.List() {
		if u.Username == passkeyBilboUsername {
			return u.ID
		}
	}
	t.Fatal("the bilbo account the passkey tests act on was not created")
	return ""
}

// passkeyRegisterBegin posts register/begin and decodes the returned
// creation options, failing the test on anything but 200.
func passkeyRegisterBegin(t *testing.T, client *http.Client, ts *httptest.Server) *protocol.CredentialCreation {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/auth/passkeys/register/begin", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register/begin returned %d: %s", resp.StatusCode, body)
	}
	var out protocol.CredentialCreation
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return &out
}

// passkeyRegisterFinish posts register/finish for a body fake produces
// from creation, returning the raw *http.Response so callers can check
// non-200 outcomes too -- passkeyRegisterFinishOK below wraps this for
// the common happy-path case.
func passkeyRegisterFinishRaw(t *testing.T, client *http.Client, ts *httptest.Server, fake *FakeAuthenticator, creation *protocol.CredentialCreation, name string) *http.Response {
	t.Helper()
	body, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, client, ts.URL+"/api/auth/passkeys/register/finish",
		passkeyRegisterFinishRequest{Credential: json.RawMessage(body), Name: name})
}

func passkeyRegisterFinishOK(t *testing.T, client *http.Client, ts *httptest.Server, fake *FakeAuthenticator, creation *protocol.CredentialCreation, name string) passkeyRegisterFinishResponse {
	t.Helper()
	resp := passkeyRegisterFinishRaw(t, client, ts, fake, creation, name)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register/finish returned %d: %s", resp.StatusCode, body)
	}
	var out passkeyRegisterFinishResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// registerPasskey drives register/begin+finish end to end against a
// fresh FakeAuthenticator built for rp, returning both so callers can go
// on to drive a login (which needs the same fake) or a second ceremony.
func registerPasskey(t *testing.T, client *http.Client, ts *httptest.Server, rp *RelyingParty, name string) (*FakeAuthenticator, passkeyRegisterFinishResponse) {
	t.Helper()
	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)
	creation := passkeyRegisterBegin(t, client, ts)
	out := passkeyRegisterFinishOK(t, client, ts, fake, creation, name)
	return fake, out
}

// startPasskeyLogin does the password step for an account whose only
// usable factor is a passkey, asserting the shape the design requires --
// no session, `secondFactor` lists "passkey" with `passkeyOrigin` set --
// then returns the client for the caller to carry on with against
// /api/auth/login/factor/begin. Mirrors totp_test.go's startTOTPLogin.
func startPasskeyLogin(t *testing.T, ts *httptest.Server, username, password string) *http.Client {
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
	found := false
	for _, f := range factors {
		if f == "passkey" {
			found = true
		}
	}
	if !found {
		t.Fatalf("password step response = %v, want secondFactor to list \"passkey\"", out)
	}
	if origin, ok := out["passkeyOrigin"].(string); !ok || origin == "" {
		t.Errorf("password step response = %v, want a non-empty passkeyOrigin", out)
	}
	return client
}

func passkeyLoginFactorBegin(t *testing.T, client *http.Client, ts *httptest.Server) *protocol.CredentialAssertion {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/auth/login/factor/begin", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login/factor/begin returned %d: %s", resp.StatusCode, body)
	}
	var out protocol.CredentialAssertion
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func submitPasskeyAssertion(t *testing.T, client *http.Client, ts *httptest.Server, fake *FakeAuthenticator, assertion *protocol.CredentialAssertion) *http.Response {
	t.Helper()
	body, err := fake.AssertionResponse(assertion)
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, client, ts.URL+"/api/auth/login/factor", loginFactorRequest{Assertion: json.RawMessage(body)})
}

func passkeysList(t *testing.T, client *http.Client, ts *httptest.Server) []passkeySummary {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/auth/passkeys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("passkeys list returned %d: %s", resp.StatusCode, body)
	}
	var out []passkeySummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPasskeyRegisterLoginRoundTrip is the happy path end to end:
// register (which mints ten recovery codes and does not sign the
// enrolling browser out) and a fresh browser completing a login with the
// same authenticator.
func TestPasskeyRegisterLoginRoundTrip(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	fake, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "YubiKey")
	if out.Passkey.Name != "YubiKey" {
		t.Errorf("stored passkey name = %q, want %q", out.Passkey.Name, "YubiKey")
	}
	if len(out.RecoveryCodes) != 10 {
		t.Fatalf("registering the first factor should mint 10 recovery codes, got %d", len(out.RecoveryCodes))
	}

	// register/finish for the account's first factor revokes and
	// reissues this browser's own session -- it must still read as
	// signed in, not logged out by its own factor going live.
	if sess := sessionOf(t, bilbo, ts); !sess.Authenticated {
		t.Fatal("registering a passkey should not have signed this browser out")
	}

	pending := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
	protected, err := pending.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	protected.Body.Close()
	if protected.StatusCode != http.StatusUnauthorized {
		t.Errorf("the password-only step reached a protected route with %d, want 401", protected.StatusCode)
	}

	assertion := passkeyLoginFactorBegin(t, pending, ts)
	resp := submitPasskeyAssertion(t, pending, ts, fake, assertion)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login/factor with a passkey assertion returned %d: %s", resp.StatusCode, body)
	}
	if sess := sessionOf(t, pending, ts); !sess.Authenticated {
		t.Fatal("expected the assertion step to establish a session")
	}
}

// TestPasswordOnlyLoginOnPasskeyOnlyAccountNeverCreatesSession is
// #1250's own version of #1249's "single most important test": a
// correct password on an account whose only factor is a passkey must
// not, under any circumstances, establish a session.
//
// Proved able to fail: temporarily changing handleAuthLogin's
// `if user.HasSecondFactor()` guard back to `if user.HasActiveTOTP()`
// (#1249's original condition) made this test fail with "a correct
// password on a passkey-only account established a session" -- a
// passkey-only account has no active TOTP, so the old condition let it
// straight through to session creation. Restored before committing.
func TestPasswordOnlyLoginOnPasskeyOnlyAccountNeverCreatesSession(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	registerPasskey(t, bilbo, ts, s.RelyingParty, "key")

	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: passkeyBilboUsername, Password: passkeyBilboPassword})
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
		t.Fatal("a correct password on a passkey-only account established a session")
	}

	protected, err := client.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	protected.Body.Close()
	if protected.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d from a protected route after a password-only login on a passkey-only account, want 401", protected.StatusCode)
	}
}

// TestPasskeyRegisterFinishWrongOriginRefused proves the route surfaces
// go-webauthn's own origin check as a refusal, paired with the identical
// ceremony succeeding at the right origin -- the design's own warning
// against a wrong-origin test that only asserts err != nil, since a
// malformed body fails too.
//
// Proved able to fail: temporarily changing
// `if err != nil { http.Error(...); return }` after CreateCredential in
// handleAuthPasskeysRegisterFinish to `if false && err != nil` made the
// handler panic on the nil *Credential CreateCredential returns
// alongside its error (a stronger failure than a mismatched status code
// -- the test still fails, on the resulting connection-reset EOF).
// Restored before committing.
func TestPasskeyRegisterFinishWrongOriginRefused(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	creation := passkeyRegisterBegin(t, bilbo, ts)
	fake := NewFakeAuthenticator(s.RelyingParty.RPID, s.RelyingParty.Origin)
	fake.Origin = "https://not-the-relying-party.example"
	resp := passkeyRegisterFinishRaw(t, bilbo, ts, fake, creation, "wrong origin")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong-origin register/finish got %d, want 400", resp.StatusCode)
	}

	// Pairing: the identical shape of request, at the right origin,
	// succeeds -- proving the refusal above is really about the origin
	// and not some other malformation.
	goodFake, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "right origin")
	if out.Passkey.Name != "right origin" {
		t.Errorf("the paired successful registration = %+v", out)
	}
	_ = goodFake
}

// TestPasskeyLoginFactorWrongRPIDRefused mirrors the wrong-origin test
// above for the login ceremony and the RP ID rather than the origin --
// the two are checked independently by go-webauthn (RPID hashes the
// authenticator data, origin is only in the client data), so a
// wrong-origin test proves nothing about RPID enforcement and vice
// versa.
//
// Proved able to fail: temporarily changing the `if err != nil` check
// after ValidateLogin in verifyPasskeyAssertion to ignore the error made
// this test fail with "wrong-RPID assertion got 200, want 401" (and,
// since the code proceeded into the CloneWarning branch against a nil
// credential, it also panicked -- restored immediately after confirming
// the failure).
func TestPasskeyLoginFactorWrongRPIDRefused(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	fake, _ := registerPasskey(t, bilbo, ts, s.RelyingParty, "key")

	fake.RPID = "not-the-relying-party.example"
	pending := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
	assertion := passkeyLoginFactorBegin(t, pending, ts)
	resp := submitPasskeyAssertion(t, pending, ts, fake, assertion)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong-RPID assertion got %d, want 401", resp.StatusCode)
	}
	if sess := sessionOf(t, pending, ts); sess.Authenticated {
		t.Error("a wrong-RPID assertion must not establish a session")
	}

	// Pairing: the identical fake, RPID restored, completes a login --
	// proving the refusal above is really about the RP ID.
	fake.RPID = s.RelyingParty.RPID
	pending2 := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
	assertion2 := passkeyLoginFactorBegin(t, pending2, ts)
	resp2 := submitPasskeyAssertion(t, pending2, ts, fake, assertion2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Errorf("the paired correct-RPID login got %d, want 200: %s", resp2.StatusCode, body)
	}
}

// TestPasskeyCloneWarningRefusesRegressedSignCount proves the sign-count
// regression refusal end to end: a first login advances the stored
// count to 5, and a second login reporting 3 is refused, audited, and
// leaves the stored count at 5 -- per the design's explicit warning
// against a clone-warning test that checks the 401 but not that the
// stored count stayed put.
//
// Proved able to fail: temporarily removing the
// `if cred.Authenticator.CloneWarning { ...; return false }` block from
// verifyPasskeyAssertion (so a clone-suspected assertion fell through to
// RecordPasskeyAssertion and success) made this test fail with "a
// regressed sign count (5 -> 3) got 200, want 401". Restored before
// committing.
func TestPasskeyCloneWarningRefusesRegressedSignCount(t *testing.T) {
	s, ts, admin := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	fake, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "yubikey")
	id := passkeyBilboID(t, s)

	storedSignCount := func() uint32 {
		u, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}
		for _, pk := range u.Passkeys {
			if base64.RawURLEncoding.EncodeToString(pk.ID) == out.Passkey.ID {
				return pk.SignCount
			}
		}
		t.Fatal("registered passkey not found on the account")
		return 0
	}

	fake.SignCount = 5
	pending := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
	assertion := passkeyLoginFactorBegin(t, pending, ts)
	resp := submitPasskeyAssertion(t, pending, ts, fake, assertion)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the first login (sign count 0 -> 5) got %d, want 200", resp.StatusCode)
	}
	if got := storedSignCount(); got != 5 {
		t.Fatalf("stored sign count after the first login = %d, want 5", got)
	}

	fake.SignCount = 3 // regresses from the 5 just recorded.
	pending2 := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
	assertion2 := passkeyLoginFactorBegin(t, pending2, ts)
	resp2 := submitPasskeyAssertion(t, pending2, ts, fake, assertion2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("a regressed sign count (5 -> 3) got %d, want 401", resp2.StatusCode)
	}
	if sess := sessionOf(t, pending2, ts); sess.Authenticated {
		t.Error("a clone-suspected assertion must not establish a session")
	}
	if got := storedSignCount(); got != 5 {
		t.Errorf("stored sign count after the regressed attempt = %d, want unchanged at 5", got)
	}

	entry := findAuditEntry(t, admin, ts, "account.passkey_clone_suspected")
	if entry.Target != passkeyBilboUsername {
		t.Errorf("account.passkey_clone_suspected entry target = %q, want %q", entry.Target, passkeyBilboUsername)
	}
}

// TestPasskeyZeroReportingAuthenticatorSignsInFine proves the flip side
// of the clone-warning test above: a platform authenticator that always
// reports a sign count of zero (the common passkey case) must never be
// treated as a clone, across repeated logins.
func TestPasskeyZeroReportingAuthenticatorSignsInFine(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	fake, _ := registerPasskey(t, bilbo, ts, s.RelyingParty, "platform authenticator")

	for i := 0; i < 2; i++ {
		pending := startPasskeyLogin(t, ts, passkeyBilboUsername, passkeyBilboPassword)
		assertion := passkeyLoginFactorBegin(t, pending, ts)
		resp := submitPasskeyAssertion(t, pending, ts, fake, assertion)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("0 -> 0 login attempt %d got %d, want 200", i, resp.StatusCode)
		}
	}
}

// TestStalePasskeyExcludedFromLoginButListedAndRemovable proves the
// design's "when publicUrl is set but later changes" story: a passkey
// registered under an old RPID is excluded from a login ceremony, but
// still shown (flagged stale, naming the old RPID) and still removable.
//
// Proved able to fail: temporarily making nonStalePasskeys return every
// passkey unfiltered (`return passkeys` as its first line) made
// login/factor/begin succeed with 200 against the account's only,
// now-stale passkey instead of refusing with 409 -- and made the
// Stale/RPID assertions below fail too, since the list itself stayed
// correct (it doesn't call nonStalePasskeys) but the login half no
// longer excluded anything. Restored before committing.
func TestStalePasskeyExcludedFromLoginButListedAndRemovable(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	originalRP := s.RelyingParty
	_, out := registerPasskey(t, bilbo, ts, originalRP, "Old Phone")

	// publicUrl changes to a new host -- swapped onto the live server the
	// same way a restart with a new setting would produce a differently
	// configured RelyingParty.
	newRP, err := NewRelyingParty("https://new-passkeys.example.org")
	if err != nil {
		t.Fatal(err)
	}
	if newRP.Status != PasskeyStatusReady {
		t.Fatalf("new relying party Status = %q, want ready", newRP.Status)
	}
	s.RelyingParty = newRP

	list := passkeysList(t, bilbo, ts)
	if len(list) != 1 {
		t.Fatalf("passkey list after the RP changed has %d entries, want 1", len(list))
	}
	if !list[0].Stale {
		t.Error("the passkey should be flagged stale after publicUrl changed")
	}
	if list[0].RPID != originalRP.RPID {
		t.Errorf("the passkey's recorded RPID = %q, want the original %q", list[0].RPID, originalRP.RPID)
	}

	// The password step's factor list no longer offers "passkey".
	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: passkeyBilboUsername, Password: passkeyBilboPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the password step returned %d, want 200", resp.StatusCode)
	}
	var loginOut map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&loginOut); err != nil {
		t.Fatal(err)
	}
	factors, _ := loginOut["secondFactor"].([]any)
	for _, f := range factors {
		if f == "passkey" {
			t.Errorf("secondFactor still lists passkey after the only passkey went stale: %v", factors)
		}
	}
	if _, hasUsername := loginOut["username"]; hasUsername {
		t.Errorf("a stale-passkey-only account's password step created a session: %v", loginOut)
	}

	beginResp := postJSON(t, client, ts.URL+"/api/auth/login/factor/begin", struct{}{})
	defer beginResp.Body.Close()
	if beginResp.StatusCode != http.StatusConflict {
		t.Errorf("login/factor/begin with only a stale passkey got %d, want 409", beginResp.StatusCode)
	}

	delResp := deleteJSON(t, bilbo, ts.URL+"/api/auth/passkeys/"+out.Passkey.ID, passkeyDeleteRequest{Password: passkeyBilboPassword})
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(delResp.Body)
		t.Errorf("deleting a stale passkey returned %d, want 200: %s", delResp.StatusCode, body)
	}
}

// TestPasskeyRoutesRefusedWhenRelyingPartyNotReady covers the two
// ceremony-starting routes with s.RelyingParty swapped to nil after the
// account already holds a passkey (registered while the RP was ready) --
// the same "publicUrl was removed from config" scenario the stale test
// above approaches from the RPID-change angle instead.
//
// Proved able to fail: temporarily changing passkeysReady to
// unconditionally `return true` made the register/begin subtest panic
// (s.RelyingParty.WebAuthn on a nil s.RelyingParty) instead of returning
// 503 -- about as decisively "able to fail" as a guard gets. Restored
// before committing.
func TestPasskeyRoutesRefusedWhenRelyingPartyNotReady(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	registerPasskey(t, bilbo, ts, s.RelyingParty, "key")
	s.RelyingParty = nil

	t.Run("register/begin", func(t *testing.T) {
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/begin", struct{}{})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("register/begin got %d, want 503", resp.StatusCode)
		}
	})

	t.Run("login/factor/begin", func(t *testing.T) {
		pending := &http.Client{Jar: mustCookieJar(t)}
		loginResp := postJSON(t, pending, ts.URL+"/api/auth/login", credentialsRequest{Username: passkeyBilboUsername, Password: passkeyBilboPassword})
		loginResp.Body.Close()

		beginResp := postJSON(t, pending, ts.URL+"/api/auth/login/factor/begin", struct{}{})
		defer beginResp.Body.Close()
		if beginResp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("login/factor/begin got %d, want 503", beginResp.StatusCode)
		}
	})
}

// TestPasskeyRegisterFinishReplayRefused proves the single-use claim in
// handleAuthPasskeysRegisterFinish's own doc comment: a second finish
// with the exact same body (and the same, now-spent cookie) is refused,
// because the cookie is cleared only on success and gone by the second
// attempt.
func TestPasskeyRegisterFinishReplayRefused(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	creation := passkeyRegisterBegin(t, bilbo, ts)
	fake := NewFakeAuthenticator(s.RelyingParty.RPID, s.RelyingParty.Origin)
	body, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatal(err)
	}
	req := passkeyRegisterFinishRequest{Credential: json.RawMessage(body), Name: "once"}

	first := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/finish", req)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("the first finish got %d, want 200", first.StatusCode)
	}

	replay := postJSON(t, bilbo, ts.URL+"/api/auth/passkeys/register/finish", req)
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusUnauthorized {
		t.Errorf("replaying the same register/finish request got %d, want 401", replay.StatusCode)
	}
}

// TestPasskeyDeleteWrongPassword proves the passkey survives a wrong-
// password attempt to remove it, and that the attempt is rate-limited on
// passwordRecheckLimiterKey rather than left as an unthrottled oracle
// behind a stolen session -- mirrors totp_test.go's
// TestTOTPDeleteWrongPassword.
func TestPasskeyDeleteWrongPassword(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "key")
	id := passkeyBilboID(t, s)

	resp := deleteJSON(t, bilbo, ts.URL+"/api/auth/passkeys/"+out.Passkey.ID, passkeyDeleteRequest{Password: "not-the-password"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong password got %d, want 401", resp.StatusCode)
	}
	u, ok := s.Auth.Get(id)
	if !ok {
		t.Fatal("bilbo account vanished")
	}
	if len(u.Passkeys) != 1 {
		t.Error("the passkey must survive a wrong-password delete attempt")
	}
}

// TestPasskeyRenameIsCosmeticNoPassword proves PATCH needs no password
// (RenamePasskey's own doc comment) and actually changes the stored
// name.
func TestPasskeyRenameIsCosmeticNoPassword(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "old name")

	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/api/auth/passkeys/"+out.Passkey.ID,
		strings.NewReader(`{"name":"new name"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := bilbo.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rename returned %d: %s", resp.StatusCode, body)
	}
	var renamed passkeySummary
	if err := json.NewDecoder(resp.Body).Decode(&renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "new name" {
		t.Errorf("renamed passkey name = %q, want %q", renamed.Name, "new name")
	}

	list := passkeysList(t, bilbo, ts)
	if len(list) != 1 || list[0].Name != "new name" {
		t.Errorf("passkey list after rename = %+v, want the new name to stick", list)
	}
}

// TestPasskeyCapAndNameBound proves the store's own limits (auth.Store.
// AddPasskey's 10-per-account cap, 64-rune name bound) surface correctly
// through the route: an 11th passkey is refused with 409, and a too-long
// name is truncated rather than refused.
func TestPasskeyCapAndNameBound(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	for i := 0; i < 10; i++ {
		registerPasskey(t, bilbo, ts, s.RelyingParty, fmt.Sprintf("key-%d", i))
	}

	creation := passkeyRegisterBegin(t, bilbo, ts)
	fake := NewFakeAuthenticator(s.RelyingParty.RPID, s.RelyingParty.Origin)
	resp := passkeyRegisterFinishRaw(t, bilbo, ts, fake, creation, "eleventh")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("an 11th passkey got %d, want 409", resp.StatusCode)
	}
}

func TestPasskeyNameIsTruncatedNotRefused(t *testing.T) {
	s, ts, _ := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	long := strings.Repeat("x", 65)
	_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, long)
	if got := len([]rune(out.Passkey.Name)); got != 64 {
		t.Errorf("a 65-rune name was stored as %d runes, want 64", got)
	}
}

// TestPasskeyRecoveryCodeMintOnce covers both orders the design's
// "Recovery codes are shared" section names: whichever factor activates
// first mints the ten codes, and the second activation reuses them
// (recoveryCodes: null, alreadyIssued: true for TOTP's response;
// recoveryCodes: null for the passkey response, which has no
// alreadyIssued field of its own since it only ever mints once per
// account by construction).
//
// Proved able to fail: temporarily changing handleAuthPasskeysRegister
// Finish's `if len(current.RecoveryCodes) == 0` to unconditionally mint
// made the "TOTP first" subtest below fail both assertions -- the
// passkey response carried a fresh set of 10 codes instead of null, and
// the before/after RecoveryCodes hash comparison showed they'd changed.
// Restored before committing.
func TestPasskeyRecoveryCodeMintOnce(t *testing.T) {
	t.Run("passkey first, TOTP confirm reuses the same codes", func(t *testing.T) {
		s, ts, _ := passkeyTestServer(t)
		bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
		_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "first")
		if len(out.RecoveryCodes) != 10 {
			t.Fatalf("passkey-first registration should mint codes, got %d", len(out.RecoveryCodes))
		}
		id := passkeyBilboID(t, s)
		before, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}

		enrolled := totpEnrol(t, bilbo, ts)
		secret, err := auth.DecodeTOTPSecret(enrolled.Secret)
		if err != nil {
			t.Fatal(err)
		}
		code := auth.GenerateTOTPCode(secret, totpCounterNow(time.Now()))
		resp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/confirm", totpConfirmRequest{Code: code})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("TOTP confirm returned %d: %s", resp.StatusCode, body)
		}
		var confirmOut totpConfirmResponse
		if err := json.NewDecoder(resp.Body).Decode(&confirmOut); err != nil {
			t.Fatal(err)
		}
		if !confirmOut.AlreadyIssued || confirmOut.RecoveryCodes != nil {
			t.Errorf("TOTP confirm after an existing passkey = %+v, want alreadyIssued true and recoveryCodes null", confirmOut)
		}

		after, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}
		if !reflect.DeepEqual(before.RecoveryCodes, after.RecoveryCodes) {
			t.Error("the original recovery-code hashes changed after a second factor activation")
		}
	})

	t.Run("TOTP first, passkey registration reuses the same codes", func(t *testing.T) {
		s, ts, _ := passkeyTestServer(t)
		bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
		_, codes, _ := totpEnrolAndConfirm(t, bilbo, ts)
		if len(codes) != 10 {
			t.Fatalf("TOTP-first confirm should mint codes, got %d", len(codes))
		}
		id := passkeyBilboID(t, s)
		before, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}

		_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "second")
		if out.RecoveryCodes != nil {
			t.Errorf("passkey registration after an existing TOTP factor minted new codes, want null: %v", out.RecoveryCodes)
		}

		after, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}
		if !reflect.DeepEqual(before.RecoveryCodes, after.RecoveryCodes) {
			t.Error("the original recovery-code hashes changed after a second factor activation")
		}
	})
}

// TestPasskeyClearConditionalKeepsRecoveryCodes covers both directions
// of #1250's shared-recovery-codes clearing rule: removing one factor
// while the other remains active must not strip the codes backing it.
// internal/auth's own tests (wave 1) cover DeletePasskey/ClearTOTP as
// store methods directly; this is the route-level version, proving
// handleTOTPDelete and handleAuthPasskeyDelete actually reach that
// behaviour rather than some other clearing path.
func TestPasskeyClearConditionalKeepsRecoveryCodes(t *testing.T) {
	t.Run("clearing TOTP keeps codes while a passkey remains", func(t *testing.T) {
		s, ts, _ := passkeyTestServer(t)
		bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
		totpEnrolAndConfirm(t, bilbo, ts)
		registerPasskey(t, bilbo, ts, s.RelyingParty, "backup key")
		id := passkeyBilboID(t, s)

		del := deleteJSON(t, bilbo, ts.URL+"/api/auth/totp", totpDeleteRequest{Password: passkeyBilboPassword})
		defer del.Body.Close()
		if del.StatusCode != http.StatusOK {
			t.Fatalf("clearing TOTP returned %d, want 200", del.StatusCode)
		}

		u, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}
		if len(u.RecoveryCodes) == 0 {
			t.Error("recovery codes were cleared even though a passkey remains")
		}
	})

	t.Run("deleting the last passkey keeps codes while TOTP remains active", func(t *testing.T) {
		s, ts, _ := passkeyTestServer(t)
		bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
		_, out := registerPasskey(t, bilbo, ts, s.RelyingParty, "only key")
		id := passkeyBilboID(t, s)

		// Not totpEnrolAndConfirm: that helper asserts 10 *fresh* recovery
		// codes come back, which does not hold here -- the passkey
		// registration above already minted them, so this confirm is the
		// mint-if-absent branch's "already issued" case (recoveryCodes:
		// null). Confirming the factor is still what this subtest needs,
		// just not through a helper that assumes it's the first factor.
		enrolled := totpEnrol(t, bilbo, ts)
		secret, err := auth.DecodeTOTPSecret(enrolled.Secret)
		if err != nil {
			t.Fatal(err)
		}
		code := auth.GenerateTOTPCode(secret, totpCounterNow(time.Now()))
		confirmResp := postJSON(t, bilbo, ts.URL+"/api/auth/totp/confirm", totpConfirmRequest{Code: code})
		defer confirmResp.Body.Close()
		if confirmResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(confirmResp.Body)
			t.Fatalf("TOTP confirm returned %d: %s", confirmResp.StatusCode, body)
		}

		del := deleteJSON(t, bilbo, ts.URL+"/api/auth/passkeys/"+out.Passkey.ID, passkeyDeleteRequest{Password: passkeyBilboPassword})
		defer del.Body.Close()
		if del.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(del.Body)
			t.Fatalf("deleting the passkey returned %d: %s", del.StatusCode, body)
		}

		u, ok := s.Auth.Get(id)
		if !ok {
			t.Fatal("bilbo account vanished")
		}
		if len(u.RecoveryCodes) == 0 {
			t.Error("recovery codes were cleared even though TOTP remains active")
		}
	})
}

// TestPasskeyAdminClear covers the Users-group admin-clear route: it
// removes every one of a colleague's passkeys, is audited, refuses the
// caller's own account, and leaves the account able to sign in with just
// its password afterward.
func TestPasskeyAdminClear(t *testing.T) {
	s, ts, admin := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	registerPasskey(t, bilbo, ts, s.RelyingParty, "key")
	id := passkeyBilboID(t, s)

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+id+"/passkeys", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin clear returned %d: %s", resp.StatusCode, body)
	}
	if s.Auth.PasskeyCount(id) != 0 {
		t.Error("expected every passkey to be cleared")
	}

	entry := findAuditEntry(t, admin, ts, "user.passkeys_cleared")
	if entry.Target != passkeyBilboUsername {
		t.Errorf("user.passkeys_cleared entry target = %q, want %q", entry.Target, passkeyBilboUsername)
	}

	plain := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)
	if sess := sessionOf(t, plain, ts); !sess.Authenticated {
		t.Error("expected a plain password login to work once the admin cleared every passkey")
	}
}

func TestPasskeyAdminCannotClearOwnPasskeys(t *testing.T) {
	s, ts, admin := passkeyTestServer(t)
	var adminID string
	for _, u := range s.Auth.List() {
		if u.Role == auth.RoleAdmin {
			adminID = u.ID
		}
	}
	if adminID == "" {
		t.Fatal("no admin account")
	}

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+adminID+"/passkeys", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("an admin clearing their own passkeys got %d, want 409", resp.StatusCode)
	}
}

// TestPasskeySessionAndUserListSurfaces covers the seam between the
// routes and the frontend (#1250's version of totp_test.go's
// TestTheFactorIsVisibleToTheFrontend): GET /api/auth/session's
// passkeys.count and GET /api/auth/users' passkeyCount both have to ask
// the store (auth.Store.PasskeyCount), not read a List()/session copy
// whose Passkeys field List() blanks -- the exact trap already caught
// once for HasActiveTOTP.
func TestPasskeySessionAndUserListSurfaces(t *testing.T) {
	s, ts, admin := passkeyTestServer(t)
	bilbo := loggedInClient(t, ts.URL, passkeyBilboUsername, passkeyBilboPassword)

	before := sessionOf(t, bilbo, ts)
	if before.Passkeys == nil || before.Passkeys.Count != 0 {
		t.Errorf("session passkeys before registering = %+v, want count 0", before.Passkeys)
	}
	if before.Passkeys.Status != PasskeyStatusReady || before.Passkeys.Origin == "" {
		t.Errorf("session passkeys status/origin = %+v, want ready with a non-empty origin", before.Passkeys)
	}

	registerPasskey(t, bilbo, ts, s.RelyingParty, "key one")
	registerPasskey(t, bilbo, ts, s.RelyingParty, "key two")

	after := sessionOf(t, bilbo, ts)
	if after.Passkeys == nil || after.Passkeys.Count != 2 {
		t.Errorf("session passkeys after registering two = %+v, want count 2", after.Passkeys)
	}

	listed := listedPasskeyCount(t, admin, ts, passkeyBilboUsername)
	if listed != 2 {
		t.Errorf("admin user list passkeyCount for %s = %d, want 2", passkeyBilboUsername, listed)
	}
	if adminCount := listedPasskeyCount(t, admin, ts, "admin"); adminCount != 0 {
		t.Errorf("admin user list reports %d passkeys for the admin, who never registered one", adminCount)
	}
}

func listedPasskeyCount(t *testing.T, admin *http.Client, ts *httptest.Server, username string) int {
	t.Helper()
	resp, err := admin.Get(ts.URL + "/api/auth/users")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []userSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, u := range out {
		if u.Username == username {
			return u.PasskeyCount
		}
	}
	t.Fatalf("no row for %q in the user list", username)
	return -1
}
