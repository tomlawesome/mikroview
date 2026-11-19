// SPDX-License-Identifier: AGPL-3.0-only

package api

// The parts of authentication that stay mikroview's after #1202 (see
// gauntlet.go): the three bearer muxes a token of each kind is confined
// to, mikroview's own refusals, the follow-ups mikroview's state needs
// after gauntlet's account routes, and setup-only mode's narrower table.
// Sign-in, sessions, doors and the account and token routes themselves
// are gauntlet's, and its own suite covers them; the guards of theirs
// that gauntlet's suite does not pin (last admin, SSO link and callback,
// reset code kept out of logs, quoted error log line, TOTP removal) are
// driven through the real binary in internal/authe2e (#1426).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/gate"
)

// maxEndReason is the longest reason gate accepts for
// ending another account's sessions.
const maxEndReason = gate.MaxSessionEndReason

// bearerFixture is a server with an admin and one token of each kind,
// minted through the store -- the mint route is gauntlet's.
type bearerFixture struct {
	s                *Server
	ts               *httptest.Server
	admin            *http.Client
	api, ingest, key string
}

func newBearerFixture(t *testing.T) *bearerFixture {
	t.Helper()
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	f := &bearerFixture{s: s, ts: ts, admin: registerAdmin(t, s, ts)}
	creator, _ := s.Auth.ByUsername(fixtureAdminUsername)
	mint := func(name string, kind gauntlet.TokenKind, device string) string {
		raw, _, err := s.Tokens.CreateWithExpiry(name, kind, device, creator, time.Now(), time.Time{})
		if err != nil {
			t.Fatalf("minting a %s token: %v", kind, err)
		}
		return raw
	}
	f.api = mint("birdcage", gauntlet.TokenKindAPI, "")
	f.ingest = mint("router-1", gauntlet.TokenKindIngest, "router-1")
	f.key = mint("droplist-pull", TokenKindDroplistPull, "")
	return f
}

func (f *bearerFixture) status(t *testing.T, method, path, raw string, withCookie bool) int {
	t.Helper()
	req, err := http.NewRequest(method, f.ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	// A forged request might carry the CSRF header too; the boundary
	// must hold because the route is not on the token's mux at all, not
	// because a header is missing.
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	client := http.DefaultClient
	if withCookie {
		client = f.admin
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// readOnlyReads are the five routes readOnlyRoutes registers.
var readOnlyReads = []string{"/api/events", "/api/flags", "/api/stats", "/api/devices", "/api/matches?entries=all"}

// Each kind of token reaches its own mux and nothing else: an API token
// the five reads (and no write, no admin surface), an ingest token only
// the two ingest routes, a drop-list key only the feed. A token on a
// route its mux does not register gets ServeMux's own 404 -- the route
// does not exist for it -- never a 200 and never the session-gated
// table. This is what makes "a token can never reach a write" structural
// rather than a check somebody has to remember (#101, #186, #1224).
func TestBearerTokensReachOnlyTheirOwnMux(t *testing.T) {
	f := newBearerFixture(t)

	for _, p := range readOnlyReads {
		if got := f.status(t, http.MethodGet, p, f.api, false); got != http.StatusOK {
			t.Errorf("API token on GET %s = %d, want 200", p, got)
		}
		if got := f.status(t, http.MethodGet, p, f.ingest, false); got != http.StatusNotFound {
			t.Errorf("ingest token on GET %s = %d, want 404 -- a router-side token reads nothing", p, got)
		}
		if got := f.status(t, http.MethodGet, p, f.key, false); got != http.StatusNotFound {
			t.Errorf("drop-list key on GET %s = %d, want 404", p, got)
		}
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/flags/clear-all"},
		{http.MethodGet, "/api/definitions"},
		{http.MethodGet, "/api/audit"},
		{http.MethodGet, "/api/tokens"},
		{http.MethodGet, "/api/auth/users"},
		{http.MethodGet, "/api/droplist.rsc"},
	} {
		if got := f.status(t, c.method, c.path, f.api, false); got != http.StatusNotFound {
			t.Errorf("API token on %s %s = %d, want 404", c.method, c.path, got)
		}
	}
	for _, p := range []string{"/api/ingest/routeros", "/api/ingest/router-backup"} {
		if got := f.status(t, http.MethodPost, p, f.ingest, false); got == http.StatusUnauthorized || got == http.StatusNotFound {
			t.Errorf("ingest token on POST %s = %d: refused as a credential or unrouted", p, got)
		}
		if got := f.status(t, http.MethodPost, p, f.api, false); got != http.StatusNotFound {
			t.Errorf("API token on POST %s = %d, want 404", p, got)
		}
	}
	if got := f.status(t, http.MethodGet, "/api/droplist.rsc", f.ingest, false); got != http.StatusNotFound {
		t.Errorf("ingest token on the drop-list feed = %d, want 404", got)
	}
	if got := f.status(t, http.MethodGet, "/api/droplist.rsc", f.key, false); got == http.StatusUnauthorized || got == http.StatusNotFound {
		t.Errorf("drop-list key on its own feed = %d", got)
	}
}

// An unknown or revoked token is refused outright, and a session cookie
// sent alongside it does not rescue the request: a bearer request is
// never treated as "no token" and passed to the cookie check.
func TestUnknownOrRevokedBearerTokenIsRefusedEvenWithACookie(t *testing.T) {
	f := newBearerFixture(t)

	for _, withCookie := range []bool{false, true} {
		if got := f.status(t, http.MethodGet, "/api/events", "gnt_not-a-real-token", withCookie); got != http.StatusUnauthorized {
			t.Errorf("unknown token (cookie=%v) = %d, want 401", withCookie, got)
		}
	}

	var id string
	for _, tok := range f.s.Tokens.List() {
		if tok.Name == "birdcage" {
			id = tok.ID
		}
	}
	if err := f.s.Tokens.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, http.MethodGet, "/api/events", f.api, false); got != http.StatusUnauthorized {
		t.Errorf("revoked token = %d, want 401", got)
	}
	// Paired: the same cookie alone still works.
	resp, err := f.admin.Get(f.ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the admin's own session on /api/events = %d, want 200", resp.StatusCode)
	}
}

// A refusal of mikroview's own -- here a viewer reaching an admin-only
// route -- must not carry gate's door header, or the frontend would send
// a correctly refused viewer to an enrolment screen instead of showing
// the refusal.
func TestOrdinaryForbiddenRefusalCarriesNoAuthGateHeader(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	registerAdmin(t, s, ts)
	viewer := createAccount(t, s, ts, "viewer1", fixtureUserPassword, gauntlet.RoleViewer)

	resp, err := viewer.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a viewer reading the audit log = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Auth-Gate"); got != "" {
		t.Errorf("X-Auth-Gate = %q on an ordinary refusal, want unset", got)
	}
}

// An admin resetting an account, or ending its sessions, ends that
// account's hold on the router-backup vault, as deleting it does (#1120,
// #1124) -- gauntlet ends the sessions, and the vault unlock is
// mikroview's to drop (gateRoutes). A request gate refuses drops
// nothing.
func TestResettingOrSigningOutAnAccountDropsItsVaultUnlock(t *testing.T) {
	for _, c := range []struct {
		name, path      string
		refused, accept any
	}{
		{"reset-password", "/reset-password",
			map[string]string{"password": "not-the-admins-password"}, map[string]string{"password": fixtureAdminPassword}},
		{"logout-all", "/logout-all",
			map[string]string{"reason": strings.Repeat("x", maxEndReason+1)}, map[string]string{"reason": "test"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, ts, admin, _ := vaultLockFixture(t)
			setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
			holder, err := s.Auth.CreateUser("keyholder", fixtureUserPassword, gauntlet.RoleUser, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			s.vaultUnlock.claim("a-session-of-theirs", holder.ID, time.Now())

			// Refused -- a wrong password, a reason too long -- changes
			// nothing.
			resp := postJSON(t, admin, ts.URL+"/api/auth/users/"+holder.ID+c.path, c.refused)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Fatalf("%s with a wrong password = %d, want a refusal", c.name, resp.StatusCode)
			}
			if s.Vault.Locked() {
				t.Fatal("a refused request dropped the vault unlock")
			}

			resp = postJSON(t, admin, ts.URL+"/api/auth/users/"+holder.ID+c.path, c.accept)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s = %d, want 200", c.name, resp.StatusCode)
			}
			if !s.Vault.Locked() {
				t.Errorf("%s left the account's vault unlock in memory", c.name)
			}
		})
	}
}

// A deletion gate refuses leaves the account's preferences where they
// are: the follow-up runs only after a 2xx.
func TestARefusedDeletionKeepsThePreferences(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)
	other, err := s.Auth.CreateUser("operator", fixtureUserPassword, gauntlet.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prefs.Merge(other.ID, []byte(`{"altitude":"street"}`)); err != nil {
		t.Fatal(err)
	}

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+other.ID, map[string]string{"password": "not-the-admins-password"})
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("deletion with a wrong password = %d, want a refusal", resp.StatusCode)
	}
	if _, ok := s.Prefs.Get(other.ID); !ok {
		t.Error("a refused deletion removed the account's preferences")
	}
}

// Setup-only mode serves sign-in and the config editor and nothing a
// token reaches: the token routes answer the setup-only 503, and a
// bearer token is refused rather than dispatched anywhere.
func TestSetupOnlyServesNoTokenRoutes(t *testing.T) {
	// A server whose gate only ever serves setup-only mode, as in the
	// process: Routes, which registers the bearer muxes, is never called.
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.SetupOnlyRoutes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)

	resp, err := admin.Get(ts.URL + "/api/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /api/tokens in setup-only mode = %d, want 503", resp.StatusCode)
	}

	creator, _ := s.Auth.ByUsername(fixtureAdminUsername)
	raw, _, err := s.Tokens.Create("birdcage", gauntlet.TokenKindAPI, "", creator, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resp = bearerGet(t, ts.URL+"/api/auth/session", raw)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a bearer token in setup-only mode = %d, want 401", resp.StatusCode)
	}
}
