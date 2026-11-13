// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B3 (doors), tests 16 to 19; 20 is in oidc_test.go.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

const (
	passkeyDomain = "mikroview.example.test"
	passkeyURL    = "https://" + passkeyDomain
)

// #1202 B3.16: a local-password account with no second factor is held:
// every route except totp/enrol, totp/confirm, passkeys/register/begin,
// register/finish and recovery-codes/confirm answers 403 must-enrol-factor
// with X-Auth-Gate: must-enrol-factor.
func TestAccountWithNoSecondFactorIsHeldAtTheDoorExceptOnTheEnrolmentRoutes(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.newUser(t, "held-user", "user")
	c, r := w.signIn(t, u, "")
	wantStatus(t, "sign-in", r, 200)

	for _, p := range []struct{ method, path string }{
		{"GET", "/api/devices"},
		{"GET", "/api/flags"},
		{"GET", "/api/auth/sessions"},
		{"POST", "/api/auth/password"},
	} {
		r := c.do(p.method, p.path, map[string]string{"currentPassword": u.pass, "newPassword": "a-new-pass-7Hq!"}, nil)
		wantProblem(t, p.method+" "+p.path+" while held", r, 403, "must-enrol-factor")
		if got := r.H.Get("X-Auth-Gate"); got != "must-enrol-factor" {
			t.Errorf("%s %s: X-Auth-Gate = %q, want must-enrol-factor", p.method, p.path, got)
		}
	}
	if s := c.session(t); !s.MustEnrolSecond {
		t.Errorf("session = %+v, want mustEnrolSecondFactor true", s)
	}

	// The enrolment routes are not behind the door (whatever else they
	// answer, it is not must-enrol-factor).
	for _, p := range []struct {
		path string
		body any
	}{
		{"/api/auth/totp/enrol", map[string]string{"password": u.pass}},
		{"/api/auth/totp/confirm", map[string]string{"code": "000000"}},
		{"/api/auth/passkeys/register/begin", map[string]string{"password": u.pass}},
		{"/api/auth/passkeys/register/finish", map[string]any{}},
		{"/api/auth/recovery-codes/confirm", map[string]any{}},
	} {
		r := c.post(p.path, p.body)
		if r.problemClass() == "must-enrol-factor" || r.H.Get("X-Auth-Gate") == "must-enrol-factor" {
			t.Errorf("POST %s is behind the must-enrol-factor door: %s", p.path, r)
		}
	}

	// Paired: an account with a factor reaches the same routes.
	wantStatus(t, "enrolled admin GET /api/devices", w.c.get("/api/devices"), 200)
}

// #1202 B3.17: after totp/confirm the response has ten recoveryCodes and
// pendingConfirmation true, and the door is still shut; after
// recovery-codes/confirm the door opens and the codes work for sign-in (one
// code once). An enrolment not confirmed within ten minutes is gone: the
// door is shut and hasTOTP is false.
func TestFirstSecondFactorIsHeldUntilTheRecoveryCodesAreConfirmed(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.newUser(t, "holder", "user")
	c, _ := w.signIn(t, u, "")

	r := c.post("/api/auth/totp/enrol", map[string]string{"password": u.pass})
	wantStatus(t, "totp/enrol", r, 200)
	var e struct{ Secret string }
	r.json(t, &e)
	u.secret = e.Secret

	awayFromStepEdge(w.a)
	r = c.post("/api/auth/totp/confirm", map[string]string{"code": totpCode(t, u.secret, w.a.serverNow().Add(-30*time.Second))})
	wantStatus(t, "totp/confirm", r, 200)
	var cf struct {
		Enabled             bool     `json:"enabled"`
		RecoveryCodes       []string `json:"recoveryCodes"`
		PendingConfirmation bool     `json:"pendingConfirmation"`
	}
	r.json(t, &cf)
	if len(cf.RecoveryCodes) != 10 || !cf.PendingConfirmation || cf.Enabled {
		t.Fatalf("totp/confirm = %+v, want ten codes, pendingConfirmation true, enabled false", cf)
	}
	u.codes = cf.RecoveryCodes

	// Still shut; the factor is not live; a second enrolment is refused.
	wantProblem(t, "route while the factor is on hold", c.get("/api/devices"), 403, "must-enrol-factor")
	if s := c.session(t); s.HasTOTP || !s.MustEnrolSecond {
		t.Errorf("session = %+v, want hasTOTP false and mustEnrolSecondFactor true while on hold", s)
	}
	wantProblem(t, "second enrolment while one is held", c.post("/api/auth/totp/enrol", map[string]string{"password": u.pass}), 409, "conflict")
	// Confirm: the door opens and the codes work, once each.
	wantStatus(t, "recovery-codes/confirm", c.post("/api/auth/recovery-codes/confirm", map[string]any{}), 200)
	wantStatus(t, "route after confirming", c.get("/api/devices"), 200)
	if s := c.session(t); !s.HasTOTP || s.MustEnrolSecond {
		t.Errorf("session = %+v, want hasTOTP true and mustEnrolSecondFactor false", s)
	}

	_, r = w.signIn(t, u, u.codes[3])
	wantStatus(t, "sign-in with a recovery code", r, 200)
	_, r = w.signIn(t, u, u.codes[3])
	wantProblem(t, "the same recovery code a second time", r, 401, "invalid-credentials")
}

func TestUnconfirmedFirstFactorIsDeletedAfterTenMinutes(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.newUser(t, "forgetful", "user")
	c, _ := w.signIn(t, u, "")

	r := c.post("/api/auth/totp/enrol", map[string]string{"password": u.pass})
	wantStatus(t, "totp/enrol", r, 200)
	var e struct{ Secret string }
	r.json(t, &e)
	awayFromStepEdge(w.a)
	wantStatus(t, "totp/confirm", c.post("/api/auth/totp/confirm", map[string]string{"code": totpCode(t, e.Secret, w.a.serverNow().Add(-30*time.Second))}), 200)

	w.advance(t, 11*time.Minute)

	if s := c.session(t); s.HasTOTP || !s.MustEnrolSecond {
		t.Errorf("session = %+v, want hasTOTP false and mustEnrolSecondFactor true once the hold has run out", s)
	}
	wantProblem(t, "route after the hold ran out", c.get("/api/devices"), 403, "must-enrol-factor")
	wantProblem(t, "confirming after the hold ran out", c.post("/api/auth/recovery-codes/confirm", map[string]any{}), 401, "step-expired")
	// The account can start again.
	wantStatus(t, "a fresh totp/enrol", c.post("/api/auth/totp/enrol", map[string]string{"password": u.pass}), 200)
}

// registerPasskey runs the begin/finish ceremony with a fake authenticator
// whose RP matches the server's publicUrl (or deliberately not).
func registerPasskey(t *testing.T, c *client, password string, fake *FakeAuthenticator, name string) resp {
	t.Helper()
	r := c.post("/api/auth/passkeys/register/begin", map[string]string{"password": password})
	wantStatus(t, "passkeys/register/begin", r, 200)
	var creation protocol.CredentialCreation
	r.json(t, &creation)
	body, err := fake.RegisterResponse(&creation)
	if err != nil {
		t.Fatal(err)
	}
	return c.post("/api/auth/passkeys/register/finish", map[string]any{"credential": json.RawMessage(body), "name": name})
}

// #1202 B3.18: rule required and an admin with no passkey: every route
// except the enrolment routes answers 403 must-enrol-passkey; the session
// body shows mustEnrolPasskey: true; after registering a passkey the door
// opens. A user or viewer is never held by this door.
func TestRequiredAdminPasskeyHoldsAnAdminWithoutOneAndNeverAUser(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{PublicURL: passkeyURL, AdminPasskey: "required"})

	// An authenticator app alone does not open the door.
	r := w.c.get("/api/devices")
	wantProblem(t, "admin with an authenticator app but no passkey", r, 403, "must-enrol-passkey")
	if got := r.H.Get("X-Auth-Gate"); got != "must-enrol-passkey" {
		t.Errorf("X-Auth-Gate = %q, want must-enrol-passkey", got)
	}
	s := w.c.session(t)
	if !s.MustEnrolPasskey || !s.AdminPasskeyRequired || s.MustEnrolSecond {
		t.Errorf("session = %+v, want mustEnrolPasskey and adminPasskeyRequired true, mustEnrolSecondFactor false", s)
	}

	fake := NewFakeAuthenticator(passkeyDomain, passkeyURL)
	r = registerPasskey(t, w.c, w.admin.pass, fake, "laptop")
	wantStatus(t, "passkeys/register/finish", r, 200)
	wantStatus(t, "route after registering a passkey", w.c.get("/api/devices"), 200)
	if s := w.c.session(t); s.MustEnrolPasskey {
		t.Error("mustEnrolPasskey is still true after registering a passkey")
	}

	// A user is held by the second-factor door, never the passkey one.
	u := w.newUser(t, "plain-user", "user")
	c, r := w.signIn(t, u, "")
	wantStatus(t, "user sign-in", r, 200)
	r = c.get("/api/devices")
	wantProblem(t, "user without a factor", r, 403, "must-enrol-factor")
	if s := c.session(t); s.MustEnrolPasskey {
		t.Error("a user was told to enrol a passkey")
	}
	eu := w.enrolled(t, "enrolled-user", "user")
	c, r = w.signIn(t, eu, eu.recovery(t))
	wantStatus(t, "enrolled user sign-in", r, 200)
	wantStatus(t, "enrolled user (authenticator app only) on a route", c.get("/api/devices"), 200)
	viewer := w.enrolled(t, "enrolled-viewer", "viewer")
	c, r = w.signIn(t, viewer, viewer.recovery(t))
	wantStatus(t, "enrolled viewer sign-in", r, 200)
	wantStatus(t, "enrolled viewer on a route", c.get("/api/devices"), 200)
}

// #1202 B3.19: with the rule required, an admin's last usable passkey
// cannot be deleted (409 conflict); a stale one (registered under an
// earlier public URL) can.
func TestAdminsLastUsablePasskeyCannotBeDeletedButAStaleOneCan(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{PublicURL: passkeyURL, AdminPasskey: "required"})
	fake1 := NewFakeAuthenticator(passkeyDomain, passkeyURL)
	wantStatus(t, "first passkey", registerPasskey(t, w.c, w.admin.pass, fake1, "first"), 200)

	// The public URL moves: the first passkey is now stale.
	const newDomain = "other.example.test"
	const newURL = "https://" + newDomain
	a2 := w.a.restart(t, func(o *Opts) { o.PublicURL = newURL })
	w.rebind(t, a2)
	wantProblem(t, "only a stale passkey left", w.c.get("/api/devices"), 403, "must-enrol-passkey")

	fake2 := NewFakeAuthenticator(newDomain, newURL)
	wantStatus(t, "second passkey under the new URL", registerPasskey(t, w.c, w.admin.pass, fake2, "second"), 200)
	wantStatus(t, "route after the second passkey", w.c.get("/api/devices"), 200)

	list := w.c.get("/api/auth/passkeys")
	wantStatus(t, "list passkeys", list, 200)
	var rows []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Stale bool   `json:"stale"`
	}
	list.json(t, &rows)
	var live, stale string
	for _, r := range rows {
		if r.Stale {
			stale = r.ID
		} else {
			live = r.ID
		}
	}
	if live == "" || stale == "" {
		t.Fatalf("passkeys = %+v, want one live and one stale", rows)
	}

	wantProblem(t, "deleting the last usable passkey", w.c.del("/api/auth/passkeys/"+live, map[string]string{"password": w.admin.pass}), 409, "conflict")
	d := w.c.del("/api/auth/passkeys/"+stale, map[string]string{"password": w.admin.pass})
	wantStatus(t, "deleting the stale passkey", d, 200)
	wantStatus(t, "route after removing the stale one", w.c.get("/api/devices"), 200)
}
