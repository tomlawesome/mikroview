// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/base64"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ---- RP construction ----

// Proved by breaking it, twice: disabling the IP-literal check (`if false &&
// net.ParseIP(hostname) != nil`) made NewRelyingParty return an *error* rather than silently
// misclassifying the "IP literal host" cases as ready -- go-webauthn's own Config.validate()
// also refuses an IP-literal RPID, so this check isn't the only thing standing between a bad
// publicUrl and a broken RP, but without it NewRelyingParty would break its own "never
// returns an error for a bad publicURL" contract for that one case. Disabling the
// insecure-scheme check (`secure := true`) made the "http on a real host" and "non-http(s)
// scheme" cases return PasskeyStatusReady instead of PasskeyStatusInsecure with no error at
// all -- unlike the IP case, nothing else in the call chain catches that, confirming this
// check is the only thing enforcing it. Both were restored immediately after.
func TestNewRelyingParty(t *testing.T) {
	cases := []struct {
		name       string
		publicURL  string
		wantStatus PasskeyStatus
		wantRPID   string
		wantOrigin string
	}{
		{name: "unset", publicURL: "", wantStatus: PasskeyStatusUnset},
		{name: "unparsable is ignored same as unset", publicURL: "://not a url", wantStatus: PasskeyStatusUnset},
		{name: "relative URL has no host, ignored", publicURL: "/just/a/path", wantStatus: PasskeyStatusUnset},
		{name: "IP literal host", publicURL: "https://192.0.2.10:8443", wantStatus: PasskeyStatusIP},
		{name: "IPv6 literal host", publicURL: "https://[2001:db8::1]", wantStatus: PasskeyStatusIP},
		{name: "http on a real host is insecure", publicURL: "http://mikroview.example", wantStatus: PasskeyStatusInsecure},
		{name: "non-http(s) scheme is insecure", publicURL: "ftp://mikroview.example", wantStatus: PasskeyStatusInsecure},
		{
			name: "http on localhost is allowed", publicURL: "http://localhost:5173",
			wantStatus: PasskeyStatusReady, wantRPID: "localhost", wantOrigin: "http://localhost:5173",
		},
		{
			name: "https ready, plain", publicURL: "https://mikroview.home.lan:8443",
			wantStatus: PasskeyStatusReady, wantRPID: "mikroview.home.lan", wantOrigin: "https://mikroview.home.lan:8443",
		},
		{
			name: "path, query and fragment are stripped but still usable", publicURL: "https://mikroview.home.lan:8443/setup?x=1#y",
			wantStatus: PasskeyStatusReady, wantRPID: "mikroview.home.lan", wantOrigin: "https://mikroview.home.lan:8443",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rp, err := NewRelyingParty(tc.publicURL)
			if err != nil {
				t.Fatalf("NewRelyingParty(%q): unexpected error: %v", tc.publicURL, err)
			}

			if rp.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q", rp.Status, tc.wantStatus)
			}

			if tc.wantStatus != PasskeyStatusReady {
				if rp.WebAuthn != nil {
					t.Fatalf("WebAuthn should be nil when Status is %q", rp.Status)
				}

				return
			}

			if rp.WebAuthn == nil {
				t.Fatal("WebAuthn is nil despite Status being ready")
			}

			if rp.RPID != tc.wantRPID {
				t.Fatalf("RPID = %q, want %q", rp.RPID, tc.wantRPID)
			}

			if rp.Origin != tc.wantOrigin {
				t.Fatalf("Origin = %q, want %q", rp.Origin, tc.wantOrigin)
			}

			if got := rp.WebAuthn.Config.RPID; got != tc.wantRPID {
				t.Fatalf("webauthn.Config.RPID = %q, want %q", got, tc.wantRPID)
			}

			if got := rp.WebAuthn.Config.RPOrigins; len(got) != 1 || got[0] != tc.wantOrigin {
				t.Fatalf("webauthn.Config.RPOrigins = %v, want [%q]", got, tc.wantOrigin)
			}
		})
	}
}

// ---- session cookie codecs ----

// testSessionData is a representative webauthn.SessionData: every field populated with a
// value that will actually reveal a round-trip bug (a zero Challenge or nil UserID would
// round-trip "successfully" even if the codec silently dropped it). Expires is a fixed,
// already-UTC time.Time rather than time.Now(), so DeepEqual against the decoded copy isn't
// at the mercy of time.Now()'s monotonic reading, which json.Marshal/Unmarshal strips.
func testSessionData() webauthn.SessionData {
	return webauthn.SessionData{
		Challenge:            "test-challenge-000000000000000000",
		RelyingPartyID:       "mikroview.home.lan",
		Origin:               "https://mikroview.home.lan:8443",
		UserID:               []byte("user-000000000000000000000000000000000000000000000000000000000001"),
		AllowedCredentialIDs: [][]byte{[]byte("cred-one"), []byte("cred-two")},
		Expires:              time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		UserVerification:     protocol.VerificationRequired,
		CredParams:           []protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}},
		Mediation:            protocol.MediationDefault,
	}
}

func TestWebAuthnSessionCodecRoundTrip(t *testing.T) {
	for _, codec := range []*webauthnSessionCodec{passkeyRegisterSessionCodec, passkeyAssertSessionCodec} {
		want := testSessionData()

		encoded, err := codec.encode(want)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}

		got, err := codec.decode(encoded)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}

		if !reflect.DeepEqual(want, got) {
			t.Fatalf("round trip changed the session data:\n got:  %#v\n want: %#v", got, want)
		}
	}
}

// TestWebAuthnSessionCodecRejectsTampering proves the AEAD authentication tag, not just the
// base64 framing, is what decode relies on: it flips one bit inside the sealed ciphertext
// (after the leading nonce, so the corruption lands in the part GCM actually authenticates)
// and checks decode refuses it rather than returning a plausible-looking but wrong
// SessionData.
//
// Proved by breaking it: discarding aead.Open's error alone (`plaintext, _ :=
// c.aead.Open(...)`) was not enough to make this test fail -- a failed Open leaves plaintext
// nil, and decode's separate json.Unmarshal error check catches that on its own, so the two
// checks in decode overlap here rather than either one being individually load-bearing for
// this exact corruption. Discarding *both* errors (Open's and Unmarshal's) did make this test
// fail, decode(tampered) returning (webauthn.SessionData{}, nil) instead of
// errWebAuthnSessionInvalid -- and made TestWebAuthnSessionCodecsAreIndependent fail the same
// way. Both checks were restored immediately after confirming that.
func TestWebAuthnSessionCodecRejectsTampering(t *testing.T) {
	codec := passkeyRegisterSessionCodec

	encoded, err := codec.encode(testSessionData())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	sealed, err := base64URLDecode(t, encoded)
	if err != nil {
		t.Fatalf("decoding test fixture: %v", err)
	}

	ns := codec.aead.NonceSize()
	if len(sealed) <= ns {
		t.Fatalf("sealed value too short to tamper with: %d bytes", len(sealed))
	}

	sealed[ns] ^= 0xFF // flip a bit just past the nonce, inside the authenticated ciphertext.
	tampered := base64URLEncode(sealed)

	if _, err := codec.decode(tampered); err != errWebAuthnSessionInvalid {
		t.Fatalf("decode(tampered) error = %v, want errWebAuthnSessionInvalid", err)
	}
}

// TestWebAuthnSessionCodecsAreIndependent proves a value sealed for one ceremony cannot be
// opened as the other -- the property the design relies on to guarantee a register
// ceremony's SessionData can never be replayed to finish a login (or vice versa), see the
// codecs' doc comment in webauthn.go.
func TestWebAuthnSessionCodecsAreIndependent(t *testing.T) {
	registerSealed, err := passkeyRegisterSessionCodec.encode(testSessionData())
	if err != nil {
		t.Fatalf("encode with register codec: %v", err)
	}

	if _, err := passkeyAssertSessionCodec.decode(registerSealed); err != errWebAuthnSessionInvalid {
		t.Fatalf("assert codec opened a register-sealed value: err = %v, want errWebAuthnSessionInvalid", err)
	}

	assertSealed, err := passkeyAssertSessionCodec.encode(testSessionData())
	if err != nil {
		t.Fatalf("encode with assert codec: %v", err)
	}

	if _, err := passkeyRegisterSessionCodec.decode(assertSealed); err != errWebAuthnSessionInvalid {
		t.Fatalf("register codec opened an assert-sealed value: err = %v, want errWebAuthnSessionInvalid", err)
	}
}

func TestWebAuthnSessionCodecRejectsMalformedInput(t *testing.T) {
	codec := passkeyRegisterSessionCodec

	cases := map[string]string{
		"not base64url at all":                  "not valid base64url!!!",
		"valid base64 but shorter than a nonce": base64URLEncode([]byte("x")),
		"empty string":                          "",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.decode(value); err != errWebAuthnSessionInvalid {
				t.Fatalf("decode(%q) error = %v, want errWebAuthnSessionInvalid", value, err)
			}
		})
	}
}

// ---- fake authenticator: proving the real library accepts and rejects what it should ----

// testWebAuthnUser is the minimal webauthn.User implementation the ceremony functions need.
// Credentials is a pointer-free slice field the test mutates directly between register and
// login steps, standing in for internal/auth's User.Passkeys the way a real caller would
// reload and persist it.
type testWebAuthnUser struct {
	id          []byte
	credentials []webauthn.Credential
}

func (u *testWebAuthnUser) WebAuthnID() []byte                         { return u.id }
func (u *testWebAuthnUser) WebAuthnName() string                       { return "test-user" }
func (u *testWebAuthnUser) WebAuthnDisplayName() string                { return "Test User" }
func (u *testWebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func mustReadyRelyingParty(t *testing.T) *RelyingParty {
	t.Helper()

	rp, err := NewRelyingParty("https://passkeys.example.org")
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}

	if rp.Status != PasskeyStatusReady {
		t.Fatalf("Status = %q, want ready", rp.Status)
	}

	return rp
}

func TestFakeAuthenticatorRegistrationAcceptedByRealLibrary(t *testing.T) {
	rp := mustReadyRelyingParty(t)
	user := &testWebAuthnUser{id: []byte("user-id-for-registration-test-0001")}

	creation, session, err := rp.WebAuthn.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)

	body, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatalf("RegisterResponse: %v", err)
	}

	req := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/passkeys/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	cred, err := rp.WebAuthn.FinishRegistration(user, *session, req)
	if err != nil {
		t.Fatalf("FinishRegistration: unexpected error: %v", err)
	}

	if !bytes.Equal(cred.ID, fake.CredentialID()) {
		t.Fatalf("credential ID = %x, want %x", cred.ID, fake.CredentialID())
	}

	if cred.AttestationType != "basic_surrogate" {
		t.Fatalf("AttestationType = %q, want basic_surrogate (self-attested)", cred.AttestationType)
	}
}

// TestFakeAuthenticatorRegistrationRejectedOnWrongOrigin proves the fake can produce input the
// real library refuses, not only input it accepts -- pointed at an origin that does not match
// the Relying Party's configured origin, paired with the identical request succeeding at the
// right origin in TestFakeAuthenticatorRegistrationAcceptedByRealLibrary above (the design
// doc's own warning against a wrong-origin test that only checks err != nil, since a
// malformed body fails too).
//
// Proved by breaking it: with the `fake.Origin = "https://not-the-relying-party.example"`
// line below removed (leaving fake.Origin at rp.Origin, the value NewFakeAuthenticator set),
// this test failed with "FinishRegistration succeeded with a response from the wrong origin,
// want an error" -- confirming the assertion is actually exercising the origin check and not
// something else. Restored immediately after.
func TestFakeAuthenticatorRegistrationRejectedOnWrongOrigin(t *testing.T) {
	rp := mustReadyRelyingParty(t)
	user := &testWebAuthnUser{id: []byte("user-id-for-wrong-origin-test-0001")}

	creation, session, err := rp.WebAuthn.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)
	fake.Origin = "https://not-the-relying-party.example"

	body, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatalf("RegisterResponse: %v", err)
	}

	req := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/passkeys/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if _, err := rp.WebAuthn.FinishRegistration(user, *session, req); err == nil {
		t.Fatal("FinishRegistration succeeded with a response from the wrong origin, want an error")
	}
}

// registerAndLogin runs a full register-then-login round trip through the real library,
// returning the credential as stored after login so callers can inspect SignCount/CloneWarning.
// Factored out because the clone-warning test below needs two logins in a row against the
// same stored credential, which is most of this function twice.
func registerAndLogin(t *testing.T, fake *FakeAuthenticator, rp *RelyingParty, user *testWebAuthnUser) (*webauthn.Credential, error) {
	t.Helper()

	assertion, session, err := rp.WebAuthn.BeginLogin(user)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}

	body, err := fake.AssertionResponse(assertion)
	if err != nil {
		t.Fatalf("AssertionResponse: %v", err)
	}

	req := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/login/factor", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	return rp.WebAuthn.FinishLogin(user, *session, req)
}

func TestFakeAuthenticatorLoginRoundTrip(t *testing.T) {
	rp := mustReadyRelyingParty(t)
	user := &testWebAuthnUser{id: []byte("user-id-for-login-round-trip-0001")}

	creation, regSession, err := rp.WebAuthn.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)

	regBody, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatalf("RegisterResponse: %v", err)
	}

	regReq := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/passkeys/register/finish", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")

	cred, err := rp.WebAuthn.FinishRegistration(user, *regSession, regReq)
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}

	user.credentials = []webauthn.Credential{*cred}

	// Zero-reporting platform authenticator: SignCount stays at its zero value across the
	// whole test, deliberately, per the design's "zero-reporting authenticator (0 -> 0)
	// signs in fine" requirement -- go-webauthn's UpdateCounter must not treat this as a
	// clone.
	got, err := registerAndLogin(t, fake, rp, user)
	if err != nil {
		t.Fatalf("FinishLogin: unexpected error: %v", err)
	}

	if got.Authenticator.CloneWarning {
		t.Fatal("CloneWarning is true for a 0 -> 0 sign count, want false")
	}

	if got.Authenticator.SignCount != 0 {
		t.Fatalf("SignCount = %d, want 0", got.Authenticator.SignCount)
	}
}

// TestFakeAuthenticatorLoginRejectedOnWrongRPID mirrors the wrong-origin registration test for
// the login ceremony and the RP ID rather than the origin -- the two are checked
// independently by the library (RPID hashes the authenticator data, origin is only in the
// client data), so a wrong-origin test proves nothing about RPID enforcement and vice versa.
//
// Proved by breaking it: with the `fake.RPID = "not-the-relying-party.example"` line removed
// (leaving fake.RPID at rp.RPID), this test failed with "FinishLogin succeeded with an
// assertion signed for the wrong RP ID, want an error", confirming the assertion is actually
// exercising the RP ID check. Restored immediately after.
func TestFakeAuthenticatorLoginRejectedOnWrongRPID(t *testing.T) {
	rp := mustReadyRelyingParty(t)
	user := &testWebAuthnUser{id: []byte("user-id-for-wrong-rpid-test-00001")}

	creation, regSession, err := rp.WebAuthn.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)

	regBody, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatalf("RegisterResponse: %v", err)
	}

	regReq := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/passkeys/register/finish", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")

	cred, err := rp.WebAuthn.FinishRegistration(user, *regSession, regReq)
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}

	user.credentials = []webauthn.Credential{*cred}

	fake.RPID = "not-the-relying-party.example"

	if _, err := registerAndLogin(t, fake, rp, user); err == nil {
		t.Fatal("FinishLogin succeeded with an assertion signed for the wrong RP ID, want an error")
	}
}

// TestFakeAuthenticatorCloneWarningOnRegressedSignCount proves the clone-warning refusal the
// routes (wave 2) will implement actually has something to refuse: two logins in a row, the
// second reporting a lower sign count than the first, must come back with CloneWarning true
// -- and, per the design doc's explicit warning against a clone-warning test that checks the
// 401 but not that the stored count stayed put, this test also asserts SignCount is still 5
// (the first login's value), not 3 (the regressed value FinishLogin refused to adopt).
//
// Proved by breaking it: changing the second `fake.SignCount = 3` (below) to `fake.SignCount =
// 6` -- an increase rather than a regression -- made this test fail with "CloneWarning is
// false after a regressed sign count (5 -> 3), want true", confirming the assertion is
// actually exercising go-webauthn's clone detection and depends on the fake being able to
// report a specific, regressed count on demand. Restored immediately after.
func TestFakeAuthenticatorCloneWarningOnRegressedSignCount(t *testing.T) {
	rp := mustReadyRelyingParty(t)
	user := &testWebAuthnUser{id: []byte("user-id-for-clone-warning-test-01")}

	creation, regSession, err := rp.WebAuthn.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	fake := NewFakeAuthenticator(rp.RPID, rp.Origin)

	regBody, err := fake.RegisterResponse(creation)
	if err != nil {
		t.Fatalf("RegisterResponse: %v", err)
	}

	regReq := httptest.NewRequest("POST", "https://passkeys.example.org/api/auth/passkeys/register/finish", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")

	cred, err := rp.WebAuthn.FinishRegistration(user, *regSession, regReq)
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}

	user.credentials = []webauthn.Credential{*cred}

	fake.SignCount = 5

	afterFirst, err := registerAndLogin(t, fake, rp, user)
	if err != nil {
		t.Fatalf("first FinishLogin: unexpected error: %v", err)
	}

	if afterFirst.Authenticator.CloneWarning {
		t.Fatal("CloneWarning is true after the first login (5, an increase from registration's 0), want false")
	}

	if afterFirst.Authenticator.SignCount != 5 {
		t.Fatalf("SignCount after first login = %d, want 5", afterFirst.Authenticator.SignCount)
	}

	user.credentials = []webauthn.Credential{*afterFirst}
	fake.SignCount = 3 // regresses from the 5 just recorded.

	afterSecond, err := registerAndLogin(t, fake, rp, user)
	if err != nil {
		t.Fatalf("second FinishLogin: unexpected error: %v", err)
	}

	if !afterSecond.Authenticator.CloneWarning {
		t.Fatal("CloneWarning is false after a regressed sign count (5 -> 3), want true")
	}

	if afterSecond.Authenticator.SignCount != 5 {
		t.Fatalf("SignCount after the regressed login = %d, want 5 (unchanged from before the regression)", afterSecond.Authenticator.SignCount)
	}
}

// base64URLEncode/base64URLDecode are tiny local wrappers kept only so
// TestWebAuthnSessionCodecRejectsTampering reads as "decode the cookie value, flip a bit,
// re-encode it" without spelling out encoding/base64's RawURLEncoding at each call site.
func base64URLEncode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func base64URLDecode(t *testing.T, s string) ([]byte, error) {
	t.Helper()

	return base64.RawURLEncoding.DecodeString(s)
}
