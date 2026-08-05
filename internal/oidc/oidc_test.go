package oidc

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, fp *fakeProvider) *Client {
	t.Helper()
	c, err := New(context.Background(), Config{
		IssuerURL:    fp.issuer(),
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://mikroview.example/api/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewDiscoversProviderAndBuildsAuthCodeURL(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	raw := c.AuthCodeURL("state-123", "nonce-456", "verifier-789")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthCodeURL produced an unparseable URL: %v", err)
	}
	q := u.Query()

	if got := q.Get("client_id"); got != "test-client" {
		t.Errorf("client_id = %q, want test-client", got)
	}
	if got := q.Get("state"); got != "state-123" {
		t.Errorf("state = %q, want state-123", got)
	}
	if got := q.Get("nonce"); got != "nonce-456" {
		t.Errorf("nonce = %q, want nonce-456", got)
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256 -- PKCE must be present", got)
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty -- PKCE challenge missing from the auth URL")
	}
	if !strings.Contains(raw, fp.issuer()) {
		t.Errorf("AuthCodeURL %q doesn't point at the discovered provider %q", raw, fp.issuer())
	}
}

func TestExchangeAndVerifyIDTokenRoundTrip(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	nonce := "nonce-abc"
	claims := fp.defaultClaims("test-client", nonce)
	fp.nextIDToken = fp.signRS256(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	id, err := c.VerifyIDToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}

	if id.Issuer != fp.issuer() {
		t.Errorf("Issuer = %q, want %q", id.Issuer, fp.issuer())
	}
	if id.Subject != "test-subject-123" {
		t.Errorf("Subject = %q, want test-subject-123", id.Subject)
	}
	if id.Nonce != nonce {
		t.Errorf("Nonce = %q, want %q", id.Nonce, nonce)
	}
	if id.Email != "person@example.com" || !id.EmailVerified {
		t.Errorf("Email/EmailVerified = %q/%v, want person@example.com/true", id.Email, id.EmailVerified)
	}
	if id.PreferredUsername != "person" {
		t.Errorf("PreferredUsername = %q, want person", id.PreferredUsername)
	}
	if !VerifyNonce(id.Nonce, nonce) {
		t.Error("VerifyNonce rejected a matching nonce")
	}
}

func TestVerifyNonceMismatchDetected(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("test-client", "nonce-the-provider-actually-returned")
	fp.nextIDToken = fp.signRS256(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	id, err := c.VerifyIDToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}

	// A caller comparing against a *different* nonce (e.g. the one from
	// a stale or attacker-supplied flow state) must be told it doesn't
	// match -- VerifyIDToken succeeding on its own is not enough proof
	// of a legitimate, non-replayed login.
	if VerifyNonce(id.Nonce, "nonce-the-flow-state-actually-expected") {
		t.Fatal("VerifyNonce accepted a mismatched nonce")
	}
}

func TestVerifyIDTokenRejectsHS256AlgorithmConfusion(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("test-client", "nonce-1")
	fp.nextIDToken = fp.signHS256WithPublicKeyBytes(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := c.VerifyIDToken(context.Background(), tok); err == nil {
		t.Fatal("VerifyIDToken accepted an HS256-signed token (algorithm-confusion attack using the provider's public key as an HMAC secret) -- SupportedSigningAlgs is not being enforced")
	}
}

func TestVerifyIDTokenRejectsNoneAlgorithm(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("test-client", "nonce-1")
	fp.nextIDToken = fp.signNoneAlgorithm(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := c.VerifyIDToken(context.Background(), tok); err == nil {
		t.Fatal("VerifyIDToken accepted an alg=none, unsigned token")
	}
}

func TestVerifyIDTokenRejectsWrongAudience(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("some-other-client", "nonce-1")
	fp.nextIDToken = fp.signRS256(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := c.VerifyIDToken(context.Background(), tok); err == nil {
		t.Fatal("VerifyIDToken accepted a token issued for a different client_id (aud mismatch)")
	}
}

func TestVerifyIDTokenRejectsExpiredToken(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("test-client", "nonce-1")
	claims.Expiry = time.Now().Add(-time.Hour).Unix()
	claims.IssuedAt = time.Now().Add(-2 * time.Hour).Unix()
	fp.nextIDToken = fp.signRS256(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := c.VerifyIDToken(context.Background(), tok); err == nil {
		t.Fatal("VerifyIDToken accepted an expired token")
	}
}

func TestVerifyIDTokenRejectsWrongIssuer(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)

	claims := fp.defaultClaims("test-client", "nonce-1")
	claims.Issuer = "https://not-the-real-provider.example"
	fp.nextIDToken = fp.signRS256(t, claims)

	tok, err := c.Exchange(context.Background(), "any-code", "any-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := c.VerifyIDToken(context.Background(), tok); err == nil {
		t.Fatal("VerifyIDToken accepted a token with a mismatched issuer claim")
	}
}

func TestExchangeSendsCodeVerifier(t *testing.T) {
	fp := newFakeProvider(t)
	c := testClient(t, fp)
	claims := fp.defaultClaims("test-client", "nonce-1")
	fp.nextIDToken = fp.signRS256(t, claims)

	// oauth2.VerifierOption's actual wire behavior is x/oauth2's own
	// well-tested responsibility -- this just confirms Exchange is
	// wired to actually pass the verifier through at all, since a
	// no-op here would silently defeat PKCE while every other test
	// still passed.
	inner := fp.server.Config.Handler
	var capturedBody string
	fp.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			capturedBody = string(b)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		inner.ServeHTTP(w, r)
	})

	if _, err := c.Exchange(context.Background(), "any-code", "the-real-verifier"); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !strings.Contains(capturedBody, "code_verifier=the-real-verifier") {
		t.Errorf("token request body %q did not include the PKCE code_verifier", capturedBody)
	}
}
