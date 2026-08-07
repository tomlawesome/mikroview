// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/tomlawesome/mikroview/internal/oidc"
)

// fakeOIDCProvider is a minimal in-process OIDC provider, just enough
// to drive internal/api's login->callback flow end to end over real
// HTTP with real JWT signing/verification. internal/oidc's own test
// suite already covers the adversarial signing cases (HS256 confusion,
// alg=none, wrong audience/issuer/expiry) in detail -- this only needs
// one legitimate RS256 identity.
type fakeOIDCProvider struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	fp := &fakeOIDCProvider{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 fp.server.URL,
			"authorization_endpoint": fp.server.URL + "/authorize",
			"token_endpoint":         fp.server.URL + "/token",
			"jwks_uri":               fp.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		set := josejwt.JSONWebKeySet{Keys: []josejwt.JSONWebKey{
			{Key: &fp.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"},
		}}
		json.NewEncoder(w).Encode(set)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"id_token":     fp.signIDToken(t),
			"expires_in":   3600,
		})
	})
	fp.server = httptest.NewServer(mux)
	t.Cleanup(fp.server.Close)
	return fp
}

// idTokenNonce is set by the test just before triggering the callback
// request, so signIDToken (called from the /token handler above,
// which has no other way to know the nonce the client is expecting)
// can embed the right value. Fine for a single-flow-at-a-time test.
var idTokenNonce string

func (fp *fakeOIDCProvider) signIDToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := struct {
		Issuer            string `json:"iss"`
		Subject           string `json:"sub"`
		Audience          string `json:"aud"`
		Expiry            int64  `json:"exp"`
		IssuedAt          int64  `json:"iat"`
		Nonce             string `json:"nonce"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
	}{
		Issuer:            fp.server.URL,
		Subject:           "test-subject-1",
		Audience:          "test-client",
		Expiry:            now.Add(time.Hour).Unix(),
		IssuedAt:          now.Unix(),
		Nonce:             idTokenNonce,
		Email:             "person@example.com",
		EmailVerified:     true,
		PreferredUsername: "person",
	}
	sig, err := josejwt.NewSigner(josejwt.SigningKey{Algorithm: josejwt.RS256, Key: fp.key}, (&josejwt.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatalf("constructing signer: %v", err)
	}
	tok, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return tok
}

// newOIDCTestServer builds a Server with a real internal/oidc.Client
// wired against fp, and a fresh, undecided auth store so a successful
// login provisions the first (admin) account.
func newOIDCTestServer(t *testing.T, fp *fakeOIDCProvider) *Server {
	t.Helper()
	s := newAuthTestServer(t)

	client, err := oidc.New(t.Context(), oidc.Config{
		IssuerURL:    fp.server.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://mikroview.example/api/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("oidc.New: %v", err)
	}
	codec, err := oidc.NewStateCodec()
	if err != nil {
		t.Fatalf("oidc.NewStateCodec: %v", err)
	}
	s.OIDC = client
	s.OIDCState = codec
	return s
}

func TestOIDCLoginNotFoundWhenNotConfigured(t *testing.T) {
	s, _ := newTestServer(t) // s.OIDC is nil
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when OIDC isn't configured", resp.StatusCode)
	}
}

func TestOIDCCallbackNotFoundWhenNotConfigured(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth/oidc/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when OIDC isn't configured", resp.StatusCode)
	}
}

func TestOIDCLoginRedirectsToProviderWithPKCEAndSetsFlowCookie(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	if loc.Query().Get("code_challenge_method") != "S256" {
		t.Error("redirect URL missing PKCE code_challenge_method=S256")
	}
	if loc.Query().Get("state") == "" || loc.Query().Get("nonce") == "" {
		t.Error("redirect URL missing state or nonce")
	}

	var flowCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == oidcFlowCookieName {
			flowCookie = c
		}
	}
	if flowCookie == nil || flowCookie.Value == "" {
		t.Fatal("expected the OIDC flow cookie to be set")
	}
}

// doFullOIDCLogin drives login -> (fake provider) -> callback end to
// end using a real cookie jar, and returns the callback's response
// (without following its final redirect) plus the state/nonce it used.
func doFullOIDCLogin(t *testing.T, ts *httptest.Server) (*http.Response, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	loginResp, err := client.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	loc, err := url.Parse(loginResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	state := loc.Query().Get("state")
	idTokenNonce = loc.Query().Get("nonce") // see fakeOIDCProvider.signIDToken

	callbackURL := ts.URL + "/api/auth/oidc/callback?code=test-code&state=" + state
	resp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	return resp, state
}

func TestOIDCCallbackFullFlowCreatesSessionAndProvisionsUser(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, _ := doFullOIDCLogin(t, ts)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback response = %d %q, want 302 to /", resp.StatusCode, resp.Header.Get("Location"))
	}

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected a session cookie to be set after a successful OIDC login")
	}

	u, ok := s.Auth.ByOIDCIdentity(fp.server.URL, "test-subject-1")
	if !ok {
		t.Fatal("expected a user to be provisioned for the OIDC identity")
	}
	if u.Role != "admin" {
		t.Errorf("Role = %q, want admin (first-ever account)", u.Role)
	}
}

func TestOIDCCallbackRejectsMissingFlowCookie(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/api/auth/oidc/callback?code=whatever&state=whatever")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/?ssoError=state_mismatch" {
		t.Errorf("response = %d %q, want a redirect to /?ssoError=state_mismatch", resp.StatusCode, resp.Header.Get("Location"))
	}
	if s.Auth.Count() != 0 {
		t.Error("no account should have been created for a callback with no flow cookie")
	}
}

func TestOIDCCallbackRejectsStateMismatch(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	loginResp, err := client.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()

	// The flow cookie is now set correctly in the jar, but the ?state=
	// presented to the callback does not match what was issued -- must
	// be rejected even though the cookie itself is valid.
	resp, err := client.Get(ts.URL + "/api/auth/oidc/callback?code=whatever&state=not-the-real-state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Location") != "/?ssoError=state_mismatch" {
		t.Errorf("Location = %q, want /?ssoError=state_mismatch", resp.Header.Get("Location"))
	}
	if s.Auth.Count() != 0 {
		t.Error("no account should have been created for a state mismatch")
	}
}

func TestOIDCCallbackRejectsProviderError(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	loginResp, err := client.Get(ts.URL + "/api/auth/oidc/login")
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	loc, _ := url.Parse(loginResp.Header.Get("Location"))
	state := loc.Query().Get("state")

	resp, err := client.Get(ts.URL + "/api/auth/oidc/callback?error=access_denied&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Location") != "/?ssoError=provider_error" {
		t.Errorf("Location = %q, want /?ssoError=provider_error", resp.Header.Get("Location"))
	}
}

func TestOIDCCallbackClearsFlowCookieOnFailure(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/api/auth/oidc/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var cleared bool
	for _, c := range resp.Cookies() {
		if c.Name == oidcFlowCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected the flow cookie to be cleared even on a failed callback")
	}
}

func TestSessionResponseReportsSSOAvailability(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		SSOAvailable bool `json:"ssoAvailable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.SSOAvailable {
		t.Error("expected ssoAvailable=true when s.OIDC is configured")
	}
}

// --- Account linking (issue #133 Part 4) -------------------------------

// Linking must be POST. A GET that starts the flow is triggerable
// cross-site: an attacker embeds it, the victim's identity provider
// silently re-authenticates them, and the callback completes a link
// they never asked for -- permanently destroying their local password.
// POST puts it behind the CSRF header a cross-site request can't set.
func TestOIDCLinkStartRequiresTheCSRFHeader(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	// Deliberately built by hand, without csrfHeaderName.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/oidc/link", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a link flow started without the CSRF header")
	}
}

func TestOIDCLinkStartRequiresASession(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// An account exists, so auth is active, but this client has no session.
	setup := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, setup, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	anon := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, anon, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", resp.StatusCode)
	}
}

func TestOIDCLinkStartRefusesAnAlreadySSOOnlyAccount(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	alice, _ := s.Auth.ByUsername("alice")
	if err := s.Auth.LinkOIDCIdentity(alice.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}
	// The link above invalidated the session, so sign in via SSO-less
	// means is no longer possible -- use a fresh session for the user.
	sess := s.Sessions.Create(alice.ID, time.Now())
	linked := &http.Client{Jar: mustCookieJar(t)}
	u, _ := url.Parse(ts.URL)
	linked.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookieName, Value: sess.ID}})

	resp := postJSON(t, linked, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409 for an account with no local password", resp.StatusCode)
	}
}

// The account a link targets comes from the session and is sealed into
// the flow state. A request body naming another account must have no
// effect whatsoever.
func TestOIDCLinkTargetsTheSessionAccountNotTheRequestBody(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	admin := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, admin, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "bob", Password: "password456", Role: "user"}).Body.Close()

	bobClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, bobClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "bob", Password: "password456"}).Body.Close()

	alice, _ := s.Auth.ByUsername("alice")
	resp := postJSON(t, bobClient, ts.URL+"/api/auth/oidc/link", map[string]any{
		"userId":   alice.ID,
		"username": "alice",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Decode the flow cookie the server just set and confirm it targets
	// bob, not the alice the body asked for.
	var flow string
	for _, c := range resp.Cookies() {
		if c.Name == oidcFlowCookieName {
			flow = c.Value
		}
	}
	if flow == "" {
		t.Fatal("no flow cookie was set")
	}
	fs, err := s.OIDCState.Decode(flow, oidcFlowCookieMaxAge, time.Now())
	if err != nil {
		t.Fatalf("decoding flow state: %v", err)
	}
	bob, _ := s.Auth.ByUsername("bob")
	if fs.LinkUserID != bob.ID {
		t.Errorf("flow targets %q, want bob (%q) -- the request body chose the account", fs.LinkUserID, bob.ID)
	}
	if !fs.IsLink() {
		t.Error("flow is not marked as a link")
	}
}
