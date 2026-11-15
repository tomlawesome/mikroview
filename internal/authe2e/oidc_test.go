// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B5 (OIDC), tests 27 to 29, and B3.20 (an SSO-only admin under
// the passkey rule), which needs the same fake provider.

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a minimal OIDC provider over https (the mikroview process
// trusts its certificate through SSL_CERT_FILE) whose next identity the
// test sets.
type fakeIdP struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	caFile string

	mu       sync.Mutex
	subject  string
	username string
	groups   []string
	nonce    string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(josejwt.JSONWebKeySet{Keys: []josejwt.JSONWebKey{
			{Key: &f.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token", "token_type": "Bearer", "id_token": f.idToken(), "expires_in": 3600,
		})
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)

	f.caFile = writeCert(t, f.srv)
	return f
}

// writeCert writes a test server's certificate where SSL_CERT_FILE can
// point at it, so the process under test trusts that server.
func writeCert(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	must(t, os.WriteFile(path, pemBytes, 0o600))
	return path
}

func (f *fakeIdP) idToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": f.srv.URL, "sub": f.subject, "aud": "test-client",
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": f.nonce,
		"email": f.username + "@example.com", "email_verified": true,
		"preferred_username": f.username, "groups": f.groups,
	}
	sig, err := josejwt.NewSigner(josejwt.SigningKey{Algorithm: josejwt.RS256, Key: f.key}, (&josejwt.SignerOptions{}).WithType("JWT"))
	if err != nil {
		f.t.Fatal(err)
	}
	tok, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

// opts adds this provider to an instance's options: its certificate for
// the process to trust, and the oidc block. allowedGroups may be empty.
func (f *fakeIdP) opts(o Opts, allowedGroups ...string) Opts {
	if o.Env == nil {
		o.Env = map[string]string{}
	}
	o.Env["SSL_CERT_FILE"] = f.caFile
	block := fmt.Sprintf("oidc:\n  issuerUrl: %q\n  clientId: \"test-client\"\n  clientSecret: \"test-secret\"\n  publicBaseUrl: \"https://mikroview.example.test\"\n", f.srv.URL)
	if len(allowedGroups) > 0 {
		quoted := make([]string, len(allowedGroups))
		for i, g := range allowedGroups {
			quoted[i] = fmt.Sprintf("%q", g)
		}
		block += "  allowedGroups: [" + strings.Join(quoted, ", ") + "]\n"
	}
	o.Extra += block
	return o
}

func (f *fakeIdP) who(subject, username string, groups ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subject, f.username, f.groups = subject, username, groups
}

// finish runs the provider's half of a flow whose authorization URL is
// loc, then the callback in c, and returns the callback's response.
func (f *fakeIdP) finish(t *testing.T, c *client, loc string) resp {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("authorization URL %q: %v", loc, err)
	}
	f.mu.Lock()
	f.nonce = u.Query().Get("nonce")
	f.mu.Unlock()
	state := u.Query().Get("state")
	if state == "" || f.nonce == "" {
		t.Fatalf("authorization URL %q has no state or nonce", loc)
	}
	return c.get("/api/auth/oidc/callback?code=test-code&state=" + url.QueryEscape(state))
}

// signIn runs a whole SSO sign-in with c: login, provider, callback.
func (f *fakeIdP) signIn(t *testing.T, c *client) resp {
	t.Helper()
	r := c.get("/api/auth/oidc/login")
	if r.Status != http.StatusFound {
		t.Fatalf("GET /api/auth/oidc/login = %s, want a 302 to the provider", r)
	}
	return f.finish(t, c, r.H.Get("Location"))
}

func wantRedirect(t *testing.T, what string, r resp, location string) {
	t.Helper()
	if r.Status != http.StatusFound {
		t.Errorf("%s: status = %d, want 302 (%s)", what, r.Status, r.Body)
		return
	}
	if got := r.H.Get("Location"); got != location {
		t.Errorf("%s: Location = %q, want %q", what, got, location)
	}
}

func userRow(t *testing.T, c *client, username string) map[string]any {
	t.Helper()
	r := c.get("/api/auth/users")
	wantStatus(t, "list users", r, 200)
	var rows []map[string]any
	r.json(t, &rows)
	for _, row := range rows {
		if row["username"] == username {
			return row
		}
	}
	return nil
}

// #1202 B5.27: with no accounts /api/auth/oidc/login answers 503
// setup-required. With an admin, a provider sign-in for an unknown
// (issuer, subject) creates a user with hasLocalPassword false; the same
// subject signs in to the same account; Policy.AllowedGroups not met
// redirects to /?ssoError=not_permitted.
func TestSSOSignInProvisionsAUserReusesItAndHonoursAllowedGroups(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	a := mustStart(t, idp.opts(Opts{}, "netops"))
	anon := a.client()
	wantProblem(t, "SSO login before the first account", anon.get("/api/auth/oidc/login"), 503, "setup-required")

	w := bootstrapOn(t, a)
	if s := a.client().session(t); !s.SSOAvailable {
		t.Fatal("ssoAvailable is false although a provider is configured")
	}

	idp.who("subject-person", "person", "netops")
	c := a.client()
	r := idp.signIn(t, c)
	wantRedirect(t, "first SSO sign-in", r, "/")
	if r.cookie("mikroview_session") == nil {
		t.Error("the callback set no session cookie")
	}
	row := userRow(t, w.c, "person")
	if row == nil {
		t.Fatal("the SSO sign-in created no account named person")
	}
	if row["role"] != "user" || row["hasLocalPassword"] != false || row["sso"] != true {
		t.Errorf("SSO account = %v, want role user, hasLocalPassword false, sso true", row)
	}
	id := row["id"]
	if s := c.session(t); !s.Authenticated || s.Username != "person" || s.HasLocalPassword {
		t.Errorf("session after SSO = %+v", s)
	}
	// An SSO-only account is exempt from the second-factor door.
	wantStatus(t, "SSO user on a route", c.get("/api/devices"), 200)

	// The same subject signs in to the same account.
	wantRedirect(t, "second SSO sign-in", idp.signIn(t, a.client()), "/")
	if again := userRow(t, w.c, "person"); again == nil || again["id"] != id {
		t.Errorf("the same subject did not reuse the account: %v", again)
	}

	// Paired: a person outside the allowed groups is refused, creates nothing.
	idp.who("subject-outsider", "outsider", "guests")
	r = idp.signIn(t, a.client())
	wantRedirect(t, "SSO sign-in outside the allowed groups", r, "/?ssoError=not_permitted")
	if r.cookie("mikroview_session") != nil {
		t.Error("a refused SSO sign-in still got a session cookie")
	}
	if userRow(t, w.c, "outsider") != nil {
		t.Error("a refused SSO sign-in provisioned an account")
	}
}

// #1202 B5.28: POST /api/auth/oidc/link then the callback links the
// caller; a non-admin loses their password (hasLocalPassword false, the
// second factor cleared); an admin keeps both; a subject already linked
// elsewhere redirects with ssoError=link_identity_taken.
func TestSSOLinkRemovesANonAdminsPasswordKeepsAnAdminsAndRefusesATakenIdentity(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{}))
	u := w.enrolled(t, "linker", "user")
	other := w.enrolled(t, "linker-two", "user")

	link := func(c *client, pass string) string {
		r := c.post("/api/auth/oidc/link", map[string]string{"password": pass})
		wantStatus(t, "oidc/link", r, 200)
		var out struct{ URL string }
		r.json(t, &out)
		if out.URL == "" {
			t.Fatalf("oidc/link returned no url: %s", r)
		}
		return out.URL
	}

	// A non-admin.
	c, r := w.signIn(t, u, u.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	if s := c.session(t); !s.HasLocalPassword || !s.HasTOTP {
		t.Fatalf("test setup: session = %+v, want a local password and an authenticator app", s)
	}
	idp.who("subject-linked", "linker")
	wantRedirect(t, "link callback", idp.finish(t, c, link(c, u.pass)), "/?ssoLinked=1")
	s := c.session(t)
	if !s.Authenticated || s.HasLocalPassword || s.HasTOTP || !s.SSOConnected {
		t.Errorf("after linking, session = %+v, want hasLocalPassword false, hasTOTP false, ssoConnected true", s)
	}

	// A subject that is already linked elsewhere.
	c2, r := w.signIn(t, other, other.recovery(t))
	wantStatus(t, "second user sign-in", r, 200)
	idp.who("subject-linked", "linker-two")
	wantRedirect(t, "link to an identity taken by another account", idp.finish(t, c2, link(c2, other.pass)), "/?ssoError=link_identity_taken")
	if s := c2.session(t); !s.HasLocalPassword || !s.HasTOTP {
		t.Errorf("a refused link still changed the account: %+v", s)
	}

	// An admin keeps the password and the authenticator app.
	idp.who("subject-admin", "the-admin")
	wantRedirect(t, "admin link callback", idp.finish(t, w.c, link(w.c, w.admin.pass)), "/?ssoLinked=1")
	s = w.c.session(t)
	if !s.Authenticated || !s.HasLocalPassword || !s.HasTOTP || !s.SSOConnected {
		t.Errorf("after the admin linked, session = %+v, want the password and authenticator app kept and ssoConnected true", s)
	}
}

// #1202 B5.29: a multi-tenant issuer in config leaves SSO off at start-up
// with the existing log line (the existing "is a multi-tenant provider and is not supported" line, CFG-0062). Paired with a single-tenant
// provider turning it on.
func TestMultiTenantIssuerLeavesSSOOff(t *testing.T) {
	t.Parallel()
	a := mustStart(t, Opts{Extra: "oidc:\n  issuerUrl: \"https://accounts.google.com\"\n  clientId: \"x\"\n  clientSecret: \"y\"\n  publicBaseUrl: \"https://mikroview.example.test\"\n"})
	const line = "is a multi-tenant provider and is not supported"
	if !strings.Contains(a.log(), line) {
		t.Errorf("the log does not carry the multi-tenant line (%q):\n%s", line, a.log())
	}
	w := bootstrapOn(t, a)
	if s := w.c.session(t); s.SSOAvailable {
		t.Error("ssoAvailable is true for a multi-tenant issuer")
	}
	wantStatus(t, "SSO login with the multi-tenant issuer configured", a.client().get("/api/auth/oidc/login"), 404)

	idp := newFakeIdP(t)
	b := mustStart(t, idp.opts(Opts{}))
	if strings.Contains(b.log(), "is a multi-tenant provider") {
		t.Error("a single-tenant provider was reported as multi-tenant")
	}
	if s := b.client().session(t); !s.SSOAvailable {
		t.Error("ssoAvailable is false for a working single-tenant provider")
	}
}

// #1202 B3.20: an SSO-only admin under the rule "required" is held at
// must-change-password until a local password is set.
func TestSSOOnlyAdminUnderTheRequiredPasskeyRuleIsHeldAtMustChangePassword(t *testing.T) {
	t.Parallel()
	idp := newFakeIdP(t)
	w := bootstrap(t, idp.opts(Opts{PublicURL: passkeyURL, AdminPasskey: "required"}))
	wantStatus(t, "admin passkey", registerPasskey(t, w.c, w.admin.pass, NewFakeAuthenticator(passkeyDomain, passkeyURL), "laptop"), 200)

	idp.who("subject-promoted", "promoted")
	c := w.a.client()
	wantRedirect(t, "first SSO sign-in (as a user)", idp.signIn(t, c), "/")
	wantStatus(t, "the SSO user, still a user, on a route", c.get("/api/devices"), 200)

	row := userRow(t, w.c, "promoted")
	if row == nil {
		t.Fatal("no SSO account")
	}
	r := w.c.put("/api/auth/users/"+row["id"].(string)+"/role", map[string]string{"role": "admin", "password": w.admin.pass, "code": w.admin.code(t, w.a, 0)})
	wantStatus(t, "promote the SSO user to admin", r, 200)

	c = w.a.client()
	wantRedirect(t, "SSO sign-in as the new admin", idp.signIn(t, c), "/")
	s := c.session(t)
	if !s.Authenticated || s.Role != "admin" || s.HasLocalPassword || !s.MustChangePassword {
		t.Errorf("session = %+v, want an admin with no local password and mustChangePassword true", s)
	}
	r = c.get("/api/devices")
	wantProblem(t, "SSO-only admin on a route", r, 403, "must-change-password")
	if got := r.H.Get("X-Auth-Gate"); got != "must-change-password" {
		t.Errorf("X-Auth-Gate = %q, want must-change-password", got)
	}
}
