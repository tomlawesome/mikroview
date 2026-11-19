// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1426 MOVES-M4, M5 and M6: behaviour of the external gauntlet gate that
// mikroview ships and that no other test on this side checked. Driven through
// the real binary.

import (
	"strings"
	"testing"
)

// MOVES-M4: a one-time reset code never reaches the audit entry or a log
// line, but the reset is audited with the admin and the target named.
func TestResetCodeNeverReachesTheAuditLogOrServerLogs(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{LogLevel: "debug"})
	u := w.enrolled(t, "reset-target", "user")
	id := userRow(t, w.c, u.name)["id"].(string)

	r := w.c.post("/api/auth/users/"+id+"/reset-password", map[string]string{"password": w.admin.pass})
	wantStatus(t, "reset password", r, 200)
	var out struct {
		Code string `json:"code"`
	}
	r.json(t, &out)
	if len(out.Code) < 8 {
		t.Fatalf("test setup: reset returned code %q", out.Code)
	}

	// The code works as the new password.
	c := w.a.client()
	lr := c.post("/api/auth/login", map[string]string{"username": u.name, "password": out.Code})
	wantStatus(t, "sign in with the reset code", lr, 200)

	bare := strings.ReplaceAll(out.Code, "-", "")
	spellings := []string{out.Code, strings.ToLower(out.Code), bare, strings.ToLower(bare)}

	audit := w.c.get("/api/audit?limit=500")
	wantStatus(t, "GET /api/audit", audit, 200)
	var res struct {
		Entries []map[string]any `json:"entries"`
	}
	audit.json(t, &res)
	found := false
	for _, e := range res.Entries {
		if e["action"] == "user.password_reset" && e["actor"] == w.admin.name && e["target"] == u.name {
			found = true
		}
	}
	if !found {
		t.Errorf("no user.password_reset audit entry with actor %s and target %s:\n%s", w.admin.name, u.name, audit.Body)
	}
	log := w.a.log()
	for _, s := range spellings {
		if strings.Contains(string(audit.Body), s) {
			t.Errorf("the reset code (as %q) is in the audit log", s)
		}
		if strings.Contains(log, s) {
			t.Errorf("the reset code (as %q) is in the server log", s)
		}
	}
}

// MOVES-M5: when a gate request fails with an error that has no user-facing
// message, the server log line quotes method, path and error, so a request
// path carrying an encoded newline cannot forge a second log line.
func TestGateErrorLogLineQuotesMethodPathAndError(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	u := w.enrolled(t, "logline-target", "user")
	id := userRow(t, w.c, u.name)["id"].(string)
	readOnlyData(t, w.a)

	// An encoded-newline path on gate routes: whatever they answer, no line
	// of the log may carry the injected text outside a quoted path.
	const forged = "forged-entry-marker"
	for _, p := range []string{
		"/api/auth/users/zz%0A12:00:00%20ERROR%20gate%20" + forged + "/reset-password",
		"/api/auth/sessions/zz%0A12:00:00%20ERROR%20gate%20" + forged,
	} {
		if strings.HasSuffix(p, "reset-password") {
			w.c.post(p, map[string]string{"password": w.admin.pass})
		} else {
			w.c.del(p, nil)
		}
	}

	// A gate route with a real resource whose save fails: the unmapped
	// error is logged on one line with method and path quoted.
	r := w.c.post("/api/auth/users/"+id+"/reset-password", map[string]string{"password": w.admin.pass})
	if r.Status != 500 {
		t.Fatalf("test premise: reset with a read-only data directory = %s, want 500", r)
	}
	want := `"POST" "/api/auth/users/` + id + `/reset-password": "`
	if !strings.Contains(w.a.log(), want) {
		t.Errorf("the log has no line with the method, path and error quoted (%s):\n%s", want, w.a.log())
	}
	// The marker may appear only inside the one real error line, quoted
	// along with its request path; a forged second line carries the marker
	// without that path and fails here.
	for _, l := range strings.Split(w.a.log(), "\n") {
		if !strings.Contains(l, forged) {
			continue
		}
		if !strings.Contains(l, `"/api/auth/`) {
			t.Errorf("a log line carries request-supplied text outside a quoted path: %q", l)
		}
	}
}

// MOVES-M6: removing the authenticator app while a passkey remains keeps the
// sessions and answers signedOut=false; removing the last factor signs the
// account out everywhere (signedOut=true).
func TestTOTPDeleteSignsOutOnlyWhenNoOtherFactorRemains(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{PublicURL: passkeyURL, AdminPasskey: "optional"})

	// A passkey still stands.
	u := w.enrolled(t, "totp-with-passkey", "user")
	c, r := w.signIn(t, u, u.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	wantStatus(t, "register a passkey", registerPasskey(t, c, u.pass, NewFakeAuthenticator(passkeyDomain, passkeyURL), "key"), 200)
	r = c.do("DELETE", "/api/auth/totp", map[string]string{"password": u.pass}, nil)
	wantStatus(t, "remove the authenticator app", r, 200)
	var out struct {
		SignedOut bool `json:"signedOut"`
	}
	r.json(t, &out)
	if out.SignedOut {
		t.Errorf("signedOut = true although a passkey remains: %s", r)
	}
	if s := c.session(t); !s.Authenticated || s.HasTOTP {
		t.Errorf("session after removing the app with a passkey = %+v, want signed in, hasTOTP false", s)
	}

	// Neighbour: no other factor, so every session ends.
	solo := w.enrolled(t, "totp-only", "user")
	c2, r := w.signIn(t, solo, solo.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	r = c2.do("DELETE", "/api/auth/totp", map[string]string{"password": solo.pass}, nil)
	wantStatus(t, "remove the only factor", r, 200)
	out.SignedOut = false
	r.json(t, &out)
	if !out.SignedOut {
		t.Errorf("signedOut = false after removing the only factor: %s", r)
	}
	if c2.session(t).Authenticated {
		t.Error("the session survived removing the only second factor")
	}
}
