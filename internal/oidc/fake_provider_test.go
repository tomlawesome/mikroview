package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeProvider is a minimal in-process OIDC provider for testing this
// package's Client against real HTTP + real JWT signing/verification,
// rather than mocking go-oidc's internals -- the whole point of these
// tests is proving the wiring (discovery, PKCE, algorithm allowlisting,
// nonce handling) actually works end to end.
type fakeProvider struct {
	t          *testing.T
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string

	// nextIDToken, if set, is returned verbatim by the token endpoint
	// instead of a freshly-signed one -- lets a test hand the token
	// endpoint an adversarial token (wrong algorithm, tampered, etc).
	nextIDToken string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	fp := &fakeProvider{t: t, privateKey: key, keyID: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", fp.serveDiscovery)
	mux.HandleFunc("/jwks", fp.serveJWKS)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented -- tests call Client.AuthCodeURL directly, never follow it", http.StatusNotImplemented)
	})
	mux.HandleFunc("/token", fp.serveToken)
	fp.server = httptest.NewServer(mux)
	t.Cleanup(fp.server.Close)
	return fp
}

func (fp *fakeProvider) issuer() string { return fp.server.URL }

func (fp *fakeProvider) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                fp.server.URL,
		"authorization_endpoint":                fp.server.URL + "/authorize",
		"token_endpoint":                        fp.server.URL + "/token",
		"jwks_uri":                              fp.server.URL + "/jwks",
		"userinfo_endpoint":                     fp.server.URL + "/userinfo",
		"id_token_signing_alg_values_supported": []string{"RS256", "ES256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (fp *fakeProvider) serveJWKS(w http.ResponseWriter, r *http.Request) {
	jwk := josejwt.JSONWebKey{Key: &fp.privateKey.PublicKey, KeyID: fp.keyID, Algorithm: "RS256", Use: "sig"}
	set := josejwt.JSONWebKeySet{Keys: []josejwt.JSONWebKey{jwk}}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(set)
}

func (fp *fakeProvider) serveToken(w http.ResponseWriter, r *http.Request) {
	idToken := fp.nextIDToken
	if idToken == "" {
		fp.t.Fatal("serveToken called but no ID token was queued -- call fp.signIDToken or set fp.nextIDToken first")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "test-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

// idTokenClaims is the standard shape of claims this package's tests
// need to control -- mirrors the real subset oidc.go's Identity reads
// plus the standard registered claims go-oidc validates.
type idTokenClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Audience          string `json:"aud"`
	Expiry            int64  `json:"exp"`
	IssuedAt          int64  `json:"iat"`
	Nonce             string `json:"nonce"`
	Email             string `json:"email,omitempty"`
	EmailVerified     bool   `json:"email_verified,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
}

func (fp *fakeProvider) defaultClaims(clientID, nonce string) idTokenClaims {
	now := time.Now()
	return idTokenClaims{
		Issuer:            fp.issuer(),
		Subject:           "test-subject-123",
		Audience:          clientID,
		Expiry:            now.Add(time.Hour).Unix(),
		IssuedAt:          now.Unix(),
		Nonce:             nonce,
		Email:             "person@example.com",
		EmailVerified:     true,
		PreferredUsername: "person",
	}
}

// signRS256 signs claims with the fake provider's own RSA key -- a
// legitimate token as far as the provider is concerned.
func (fp *fakeProvider) signRS256(t *testing.T, claims idTokenClaims) string {
	t.Helper()
	sig, err := josejwt.NewSigner(josejwt.SigningKey{Algorithm: josejwt.RS256, Key: fp.privateKey}, (&josejwt.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatalf("constructing RS256 signer: %v", err)
	}
	tok, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("signing RS256 token: %v", err)
	}
	return tok
}

// signHS256WithPublicKeyBytes crafts the classic algorithm-confusion
// attack: an attacker who only ever saw the provider's *public* RSA
// key (from the public JWKS endpoint -- not a secret) resigns a token
// using HS256, treating those same public bytes as an HMAC secret. A
// verifier that blindly used "whatever key type is convenient" for
// whatever alg the token header claims would accept this; one that
// pins an allowed algorithm set up front (see oidc.go's
// SupportedSigningAlgs) rejects it outright, before any HMAC
// computation happens at all.
func (fp *fakeProvider) signHS256WithPublicKeyBytes(t *testing.T, claims idTokenClaims) string {
	t.Helper()
	pubDER, err := josejwt.JSONWebKey{Key: &fp.privateKey.PublicKey}.MarshalJSON()
	if err != nil {
		t.Fatalf("marshaling public key: %v", err)
	}
	sig, err := josejwt.NewSigner(josejwt.SigningKey{Algorithm: josejwt.HS256, Key: pubDER}, (&josejwt.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatalf("constructing HS256 signer: %v", err)
	}
	tok, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("signing HS256 token: %v", err)
	}
	return tok
}

// signNoneAlgorithm hand-crafts a header.payload. token with alg=none
// and an empty signature segment -- go-jose's Signer deliberately has
// no way to produce this (it's not a real signing algorithm, it's the
// "don't bother checking" footgun the JWT spec allows), so this bypasses
// the library entirely to prove oidc.go's verifier rejects it too.
func (fp *fakeProvider) signNoneAlgorithm(t *testing.T, claims idTokenClaims) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}
