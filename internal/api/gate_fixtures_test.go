// SPDX-License-Identifier: AGPL-3.0-only

package api

// The fixtures every test in this package that goes through Routes
// shares, since authentication moved onto gauntlet (#1202): stores
// sealed in memory, the gate built over them, an admin and other
// accounts that hold a second factor (so they pass the must-enrol-factor
// door), and sessions minted straight into the session store for tests
// that only need a signed-in caller. The routes those accounts sign in
// through are gauntlet's, and gauntlet's own suite covers them; what this
// package tests through them is mikroview's own routes and the wiring
// around the gate (gauntlet.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/gate"
	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// The fixture accounts' passwords. Obvious placeholders, never anything
// shaped like a credential somebody might really hold -- and long enough,
// and off gauntlet's common-password list, that its password rules
// accept them.
const (
	fixtureAdminUsername = "admin"
	fixtureAdminPassword = "fixture-admin-pass-7Qx!"
	// fixtureUserPassword and fixtureOtherPassword are for the second
	// and third accounts a test creates.
	fixtureUserPassword  = "fixture-user-pass-4Kd!"
	fixtureOtherPassword = "fixture-other-pass-9Wm!"
)

// testSealKey seals every fixture store. gauntlet refuses to hold
// accounts in the clear, and this package never asks it to
// (AllowPlaintextAtRest is set nowhere): the fixtures are sealed in
// memory exactly as the server's are on disk.
var testSealKey = bytes.Repeat([]byte("k"), gpersist.MinKeyBytes)

// testSetupCodes is the setup code each fixture accounts store announced,
// keyed by the store, for setupRequest.
var testSetupCodes sync.Map // *gauntlet.Store -> *setupCodeBox

type setupCodeBox struct {
	mu   sync.Mutex
	code string
}

// newTestAuthStores opens an empty accounts store and token store, both
// persisted (in memory) so accounts and tokens can be created.
func newTestAuthStores(t *testing.T) (*gauntlet.Store, *gauntlet.TokenStore) {
	t.Helper()
	accounts, err := gpersist.Encrypt(gpersist.NewMemory(), testSealKey, gpersist.EncryptOptions{Label: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	box := &setupCodeBox{}
	st, err := gauntlet.OpenStore(accounts, gauntlet.Options{
		ProductName: ProductName,
		OnSetupCode: gauntlet.SetupCodeFunc(func(code string) {
			box.mu.Lock()
			box.code = code
			box.mu.Unlock()
		}),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
	})
	if err != nil {
		t.Fatal(err)
	}
	testSetupCodes.Store(st, box)
	tokens, err := gauntlet.OpenTokenStore(gpersist.NewMemory(), gauntlet.TokenOptions{Kinds: TokenKinds})
	if err != nil {
		t.Fatal(err)
	}
	return st, tokens
}

// mustGate (re)builds s.Gate over s's stores as they stand now. Call it
// again after replacing a store or the limiter: gate holds the ones it
// was built with.
func mustGate(t *testing.T, s *Server) {
	t.Helper()
	g, err := s.NewGate(GateOptions{AdminPasskey: gate.AdminPasskeyOptional})
	if err != nil {
		t.Fatalf("building the gate: %v", err)
	}
	s.Gate = g
}

// newAuthTestServer is newTestServer: every fixture now starts with an
// empty, persisted accounts store and a gate in front of it.
func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	return s
}

// registerRequest is POST /api/auth/register's body.
type registerRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	SetupCode string `json:"setupCode"`
}

// setupRequest is a register body carrying the setup code s's store
// announced.
func setupRequest(t *testing.T, s *Server, username, password string) registerRequest {
	t.Helper()
	v, ok := testSetupCodes.Load(s.Auth)
	if !ok {
		t.Fatal("this accounts store was not opened by newTestAuthStores, so its setup code is unknown")
	}
	box := v.(*setupCodeBox)
	box.mu.Lock()
	defer box.mu.Unlock()
	return registerRequest{Username: username, Password: password, SetupCode: box.code}
}

// credentialsRequest is POST /api/auth/login's body.
type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// createUserRequest is POST /api/auth/users' body.
type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// changePasswordRequest is POST /api/auth/password's body.
type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// loginFactorRequest is POST /api/auth/login/factor's body.
type loginFactorRequest struct {
	Code string `json:"code"`
}

// createTokenRequest is POST /api/tokens' body: the admin's own password
// is required to mint one since #1202.
type createTokenRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Device    string `json:"device,omitempty"`
	Password  string `json:"password"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// tokenResponse is the part of POST /api/tokens' answer these tests read.
type tokenResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// userSummary is one row of GET /api/auth/users, as far as these tests
// read it.
type userSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// registerAdmin gives s an admin holding an authenticator app (so it is
// past the must-enrol-factor door) and returns a client signed in as it.
// The account is created in the store, not through the routes: those
// are gauntlet's, and this is setup, not coverage.
func registerAdmin(t *testing.T, s *Server, ts *httptest.Server) *http.Client {
	t.Helper()
	if _, ok := s.Auth.ByUsername(fixtureAdminUsername); !ok {
		if _, err := s.Auth.Register(fixtureAdminUsername, fixtureAdminPassword, time.Now()); err != nil {
			t.Fatalf("registering the fixture admin: %v", err)
		}
	}
	seedFactor(t, s, ts, fixtureAdminUsername)
	return sessionClient(t, s, ts, fixtureAdminUsername)
}

// setUpAdmin is registerAdmin, under the name the token and definitions
// tests use.
func setUpAdmin(t *testing.T, s *Server, ts *httptest.Server) *http.Client {
	t.Helper()
	return registerAdmin(t, s, ts)
}

// createAccount adds an account at role, holding a factor, and returns a
// client signed in as it.
func createAccount(t *testing.T, s *Server, ts *httptest.Server, username, password string, role gauntlet.Role) *http.Client {
	t.Helper()
	if _, err := s.Auth.CreateUser(username, password, role, time.Now()); err != nil {
		t.Fatalf("creating %q: %v", username, err)
	}
	seedFactor(t, s, ts, username)
	return sessionClient(t, s, ts, username)
}

// sessionClient returns a client signed in as username on s without
// going through the sign-in routes: the session gate's own sign-in ends
// with, minted straight into the store, and its cookie under the name
// gate reads.
func sessionClient(t *testing.T, s *Server, ts *httptest.Server, username string) *http.Client {
	t.Helper()
	u, ok := s.Auth.ByUsername(username)
	if !ok {
		t.Fatalf("no account named %q to mint a session for", username)
	}
	sess := s.Sessions.Create(u.ID, time.Now())
	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: mustCookieJar(t)}
	client.Jar.SetCookies(base, []*http.Cookie{{Name: s.sessionCookie(), Value: sess.ID, Path: "/"}})
	return client
}

// totpCounterNow is the RFC 6238 step now falls in.
func totpCounterNow(now time.Time) uint64 { return uint64(now.Unix() / 30) }

// totpFixtureFactor is what seedFactor confirmed for one account on one
// server, so a later sign-in through the routes (loggedInClient) can
// finish the second step with a code the replay guard has not seen.
type totpFixtureFactor struct {
	secret      []byte
	lastCounter uint64
}

var (
	totpFixtureFactorsMu sync.Mutex
	totpFixtureFactors   = map[string]map[string]*totpFixtureFactor{}
)

func rememberTOTPFactor(base, username string, secret []byte, counter uint64) {
	totpFixtureFactorsMu.Lock()
	defer totpFixtureFactorsMu.Unlock()
	if totpFixtureFactors[base] == nil {
		totpFixtureFactors[base] = map[string]*totpFixtureFactor{}
	}
	totpFixtureFactors[base][username] = &totpFixtureFactor{secret: secret, lastCounter: counter}
}

// totpFixtureCode returns an authenticator code for username's
// remembered factor that the server has not accepted yet: the next step
// after the last one used, within the one step either side of now the
// server accepts.
func totpFixtureCode(base, username string) (string, bool) {
	totpFixtureFactorsMu.Lock()
	defer totpFixtureFactorsMu.Unlock()
	f := totpFixtureFactors[base][username]
	if f == nil {
		return "", false
	}
	counter := totpCounterNow(time.Now()) - 1
	if counter <= f.lastCounter {
		counter = f.lastCounter + 1
	}
	if counter > totpCounterNow(time.Now())+1 {
		return "", false
	}
	f.lastCounter = counter
	return gauntlet.GenerateTOTPCode(f.secret, counter), true
}

// seedFactor gives username a confirmed authenticator app through the
// store, and remembers it for ts. Confirmed as of two steps ago, so the
// three steps the server accepts around now are all still unused.
func seedFactor(t *testing.T, s *Server, ts *httptest.Server, username string) {
	t.Helper()
	u, ok := s.Auth.ByUsername(username)
	if !ok {
		t.Fatalf("no account named %q to seed a factor for", username)
	}
	if u.HasActiveTOTP() {
		return
	}
	secret, err := gauntlet.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.SetPendingTOTPSecret(u.ID, gauntlet.EncodeTOTPSecret(secret)); err != nil {
		t.Fatalf("seeding a factor for %q: %v", username, err)
	}
	now := time.Now()
	counter := totpCounterNow(now) - 2
	if err := s.Auth.ConfirmTOTP(u.ID, now, counter); err != nil {
		t.Fatalf("confirming the seeded factor for %q: %v", username, err)
	}
	rememberTOTPFactor(ts.URL, username, secret, counter)
}

// totpEnrolAndConfirm enrols an authenticator app on client's account
// through the routes, the way the screens do: enrol, confirm with a
// code, then confirm the recovery codes are saved, which is when a first
// factor goes live (gauntlet's held enrolment). It returns the secret,
// the ten codes and the step the confirming code was for.
func totpEnrolAndConfirm(t *testing.T, client *http.Client, ts *httptest.Server, password string) ([]byte, []string, uint64) {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/auth/totp/enrol", map[string]string{"password": password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("totp/enrol = %d: %s", resp.StatusCode, body)
	}
	var enrol struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&enrol); err != nil {
		t.Fatal(err)
	}
	secret, err := gauntlet.DecodeTOTPSecret(enrol.Secret)
	if err != nil {
		t.Fatal(err)
	}
	counter := totpCounterNow(time.Now())
	confirm := postJSON(t, client, ts.URL+"/api/auth/totp/confirm", map[string]string{"code": gauntlet.GenerateTOTPCode(secret, counter)})
	defer confirm.Body.Close()
	if confirm.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(confirm.Body)
		t.Fatalf("totp/confirm = %d: %s", confirm.StatusCode, body)
	}
	var out struct {
		RecoveryCodes       []string `json:"recoveryCodes"`
		PendingConfirmation bool     `json:"pendingConfirmation"`
	}
	if err := json.NewDecoder(confirm.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.PendingConfirmation {
		saved := postJSON(t, client, ts.URL+"/api/auth/recovery-codes/confirm", map[string]any{})
		saved.Body.Close()
		if saved.StatusCode != http.StatusOK {
			t.Fatalf("recovery-codes/confirm = %d", saved.StatusCode)
		}
	}
	return secret, out.RecoveryCodes, counter
}

// postJSON sends the CSRF header the real frontend always sends (see
// csrfHeaderName) -- a request that deliberately omits it, to test the
// check itself, is built directly with http.NewRequest instead.
func postJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	return sendJSON(t, client, http.MethodPost, url, body)
}

// putJSON mirrors postJSON but for PUT.
func putJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	return sendJSON(t, client, http.MethodPut, url, body)
}

func sendJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

// mustLimiter is gauntlet.NewLoginLimiter for a test, which never
// passes it a bad configuration.
func mustLimiter(t *testing.T, threshold int, window time.Duration) *gauntlet.LoginLimiter {
	t.Helper()
	l, err := gauntlet.NewLoginLimiter(threshold, window)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// saveBudgetBackend is a store backend whose saves start failing once
// left runs out -- for driving a write failure through a real handler.
type saveBudgetBackend struct{ left int }

func (b *saveBudgetBackend) Load(context.Context) (gpersist.Snapshot, error) {
	return gpersist.Snapshot{}, nil
}

func (b *saveBudgetBackend) Save(_ context.Context, _ []byte, expect int64) (int64, error) {
	if b.left <= 0 {
		return 0, errors.New("backend unavailable")
	}
	b.left--
	return expect + 1, nil
}

func (b *saveBudgetBackend) Close() error     { return nil }
func (b *saveBudgetBackend) Describe() string { return "save-budget test backend" }

// findAuditEntry is the first audit entry for action, read through
// GET /api/audit as admin.
func findAuditEntry(t *testing.T, admin *http.Client, ts *httptest.Server, action string) audit.Entry {
	t.Helper()
	for _, e := range fetchAudit(t, admin, ts).Entries {
		if e.Action == action {
			return e
		}
	}
	t.Fatalf("no %s audit entry was recorded", action)
	return audit.Entry{}
}

// bearerGet is GET url with raw as its bearer token and no cookie.
func bearerGet(t *testing.T, url, raw string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// createToken mints an API token through POST /api/tokens as the
// fixture admin client and returns its raw value.
func createToken(t *testing.T, ts *httptest.Server, client *http.Client, name string) string {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/tokens", createTokenRequest{Name: name, Password: fixtureAdminPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected token creation to succeed, got %d: %s", resp.StatusCode, body)
	}
	var created tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created.Value
}

// sessionIDFromJar is the session cookie client holds for rawURL, under
// the name s gives it.
func sessionIDFromJar(t *testing.T, s *Server, client *http.Client, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == s.sessionCookie() {
			return c.Value
		}
	}
	t.Fatal("no session cookie in the jar")
	return ""
}

// loggedInClient signs username in through the routes, finishing the
// second step with the fixture's remembered factor (seedFactor) when the
// account holds one.
func loggedInClient(t *testing.T, base, username, password string) *http.Client {
	t.Helper()
	c := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, c, base+"/api/auth/login", credentialsRequest{Username: username, Password: password})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("login as %q failed with %d: %s", username, resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, pending := out["secondFactor"]; pending {
		code, ok := totpFixtureCode(base, username)
		if !ok {
			t.Fatalf("login as %q stopped at the second step, and the fixture has no unused code for it", username)
		}
		factorResp := postJSON(t, c, base+"/api/auth/login/factor", loginFactorRequest{Code: code})
		defer factorResp.Body.Close()
		if factorResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(factorResp.Body)
			t.Fatalf("completing the second step for %q failed with %d: %s", username, factorResp.StatusCode, body)
		}
	}
	return c
}
