// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B4 (accounts and tokens as admin), tests 21 to 26.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

type mintedToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Device    string `json:"device"`
	Value     string `json:"value"`
	ExpiresAt string `json:"expiresAt"`
}

func (w *world) mint(t *testing.T, body map[string]any) (mintedToken, resp) {
	t.Helper()
	if _, ok := body["password"]; !ok {
		body["password"] = w.admin.pass
	}
	r := w.c.post("/api/tokens", body)
	var m mintedToken
	if r.Status == 201 {
		r.json(t, &m)
	}
	return m, r
}

// #1202 B4.21: POST /api/auth/users with role user or viewer works as today
// (201); role admin with no step-up is not 201 (and creates nothing); the
// users list rows carry heldForPasskey.
func TestCreateUserRolesAndTheAdminStepUp(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	for _, role := range []string{"user", "viewer"} {
		r := w.c.post("/api/auth/users", map[string]string{"username": "new-" + role, "password": "pw-" + role + "-9X7q!", "role": role})
		wantStatus(t, "create "+role, r, 201)
		var acc struct {
			Username, Role string
			HeldForPasskey bool
		}
		r.json(t, &acc)
		if acc.Username != "new-"+role || acc.Role != role {
			t.Errorf("created %+v", acc)
		}
	}

	r := w.c.post("/api/auth/users", map[string]string{"username": "sneaky-admin", "password": "pw-sneaky-9X7q!", "role": "admin"})
	if r.Status == 201 || r.Status < 400 || r.Status >= 500 {
		t.Errorf("creating an admin with no step-up = %s, want a 4xx refusal that is not 201", r)
	}

	// Paired: with the caller's password and a current code the same request is accepted.
	r = w.c.post("/api/auth/users", map[string]string{
		"username": "second-admin", "password": "pw-second-9X7q!", "role": "admin",
		"adminPassword": w.admin.pass, "adminCode": w.admin.code(t, w.a, 0),
	})
	wantStatus(t, "create an admin with a step-up", r, 201)

	list := w.c.get("/api/auth/users")
	wantStatus(t, "list users", list, 200)
	var rows []map[string]any
	list.json(t, &rows)
	names := map[string]bool{}
	for _, row := range rows {
		if _, ok := row["heldForPasskey"]; !ok {
			t.Errorf("user row %v has no heldForPasskey", row["username"])
		}
		names[row["username"].(string)] = true
	}
	if names["sneaky-admin"] {
		t.Error("the refused admin was created anyway")
	}
	for _, want := range []string{"mv-admin", "new-user", "new-viewer", "second-admin"} {
		if !names[want] {
			t.Errorf("users list lacks %s: %v", want, names)
		}
	}
}

// #1202 B4.22: a new password equal to the username with a digit appended,
// or mikroview1, or password123, is refused with 400 invalid-request; a
// 12-character random one is accepted.
func TestCommonOrContextPasswordsAreRefusedWithInvalidRequest(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	// Each attempt names a new account, so a refusal is never confused with
	// "that username is taken". The first password is the username with a
	// digit appended.
	for i, pw := range []string{"ctx-user-0" + "1", "mikroview1", "password123", "MikroView!", "Password1"} {
		name := "ctx-user-" + strconv.Itoa(i)
		if i == 0 {
			name = "ctx-user-0"
		}
		r := w.c.post("/api/auth/users", map[string]string{"username": name, "password": pw, "role": "user"})
		wantProblem(t, "creating "+name+" with password "+pw, r, 400, "invalid-request")
	}
	r := w.c.post("/api/auth/users", map[string]string{"username": "ctx-user-ok", "password": randHex(t, 6), "role": "user"})
	wantStatus(t, "creating an account with a 12-character random password", r, 201)

	// The change-password route applies the same rules.
	wantProblem(t, "change to password123", w.c.post("/api/auth/password", map[string]string{"currentPassword": w.admin.pass, "newPassword": "password123"}), 400, "invalid-request")
	wantStatus(t, "change to a random password", w.c.post("/api/auth/password", map[string]string{"currentPassword": w.admin.pass, "newPassword": randHex(t, 6)}), 200)
}

// #1202 B4.23: POST /api/tokens without password is refused; with the
// admin's password and {name, kind: "ingest", device, expiresAt: "never"}
// it answers 201 with a value starting gnt_ and no expiresAt; with {name,
// kind: "api"} the expiresAt is about a year ahead; an expired token
// (clock advanced) is refused on its route with 401.
func TestTokensNeedThePasswordCarryGntPrefixAndExpire(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	r := w.c.post("/api/tokens", map[string]any{"name": "no-password", "kind": "api"})
	if r.Status != 400 && r.Status != 401 {
		t.Errorf("POST /api/tokens without a password = %s, want 400 (the build notes) or 401 (gauntlet's contract)", r)
	}
	wrong := w.c.post("/api/tokens", map[string]any{"name": "wrong-password", "kind": "api", "password": "not-the-password"})
	if wrong.Status != 401 {
		t.Errorf("POST /api/tokens with a wrong password = %s, want 401", wrong)
	}
	if list := w.c.get("/api/tokens"); strings.Contains(string(list.Body), "no-password") || strings.Contains(string(list.Body), "wrong-password") {
		t.Errorf("a token was created without the right password: %s", list.Body)
	}

	ing, r := w.mint(t, map[string]any{"name": "router-1", "kind": "ingest", "device": "core-router", "expiresAt": "never"})
	wantStatus(t, "mint ingest token", r, 201)
	if !strings.HasPrefix(ing.Value, "gnt_") {
		t.Errorf("ingest token value %q does not start gnt_", ing.Value)
	}
	if ing.ExpiresAt != "" {
		t.Errorf("an ingest token minted with expiresAt \"never\" carries expiresAt %q", ing.ExpiresAt)
	}

	api, r := w.mint(t, map[string]any{"name": "grafana", "kind": "api"})
	wantStatus(t, "mint api token", r, 201)
	if !strings.HasPrefix(api.Value, "gnt_") {
		t.Errorf("api token value %q does not start gnt_", api.Value)
	}
	exp, err := time.Parse(time.RFC3339, api.ExpiresAt)
	if err != nil {
		t.Fatalf("api token expiresAt %q: %v", api.ExpiresAt, err)
	}
	if d := exp.Sub(w.a.serverNow()); d < 360*24*time.Hour || d > 370*24*time.Hour {
		t.Errorf("api token expires in %s, want about a year", d)
	}

	// An expired token is refused like an unknown one.
	short, r := w.mint(t, map[string]any{"name": "short-lived", "kind": "api", "expiresAt": w.a.serverNow().Add(2 * time.Hour).UTC().Format(time.RFC3339)})
	wantStatus(t, "mint a two-hour token", r, 201)
	anon := w.a.client()
	wantStatus(t, "short-lived token before it expires", anon.bearer(short.Value, "GET", "/api/stats"), 200)
	wantStatus(t, "year-long token", anon.bearer(api.Value, "GET", "/api/stats"), 200)
	w.advance(t, 3*time.Hour)
	wantStatus(t, "short-lived token after it expires", anon.bearer(short.Value, "GET", "/api/stats"), 401)
	wantStatus(t, "year-long token three hours on", anon.bearer(api.Value, "GET", "/api/stats"), 200)
}

// #1202 B4.24: the daily sweep removes a token unused for over a year and
// audits token.removed_unused; a token used within the year stays. (The
// first pass runs shortly after start-up; see harness_test.go.)
func TestTokenSweepRemovesATokenUnusedForAYearAndAuditsIt(t *testing.T) {
	t.Parallel()
	f := newLegacyInstallSealed(t)

	// Rewrite the sealed tokens document with last-used dates.
	now := time.Now().UTC()
	old := now.Add(-400 * 24 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	created := now.Add(-500 * 24 * time.Hour).Format(time.RFC3339)
	var doc []map[string]any
	must(t, json.Unmarshal(f.tokenDoc, &doc))
	doc[0]["name"], doc[0]["lastUsedAt"], doc[0]["createdAt"] = "stale-token", old, created
	doc[1]["name"], doc[1]["lastUsedAt"], doc[1]["createdAt"] = "fresh-token", recent, created
	doc = doc[:2]
	raw, err := json.Marshal(doc)
	must(t, err)
	key, err := retention.LoadKey(f.keyPath)
	must(t, err)
	path := f.root + "/data/tokens.json"
	b := persist.NewEncryptedFileBackend(path, "tokens", key)
	snap, err := b.Load(context.Background())
	must(t, err)
	if _, err := b.Save(context.Background(), raw, snap.Version); err != nil {
		t.Fatal(err)
	}

	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	w := &world{a: a, admin: f.admin}
	c, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "admin sign-in", r, 200)
	w.c = c

	var names map[string]bool
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		names = map[string]bool{}
		r := c.get("/api/tokens")
		wantStatus(t, "list tokens", r, 200)
		var out struct {
			Tokens []mintedToken `json:"tokens"`
		}
		r.json(t, &out)
		for _, tk := range out.Tokens {
			names[tk.Name] = true
		}
		if !names["stale-token"] {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if names["stale-token"] {
		t.Error("the token unused for 400 days is still listed 30 seconds after start-up; the sweep did not run")
	}
	if !names["fresh-token"] {
		t.Error("a token used 10 days ago was swept")
	}
	audit := c.get("/api/audit")
	wantStatus(t, "GET /api/audit", audit, 200)
	if !containsAll(string(audit.Body), "token.removed_unused", "stale-token") {
		t.Errorf("no token.removed_unused audit record naming stale-token:\n%s", audit.Body)
	}
}

// #1202 B4.25: bearer routing is unchanged: an api token reaches only the
// five read routes (404 elsewhere), an ingest token only the two ingest
// routes, a droplist-pull token only /api/droplist.rsc; an unknown token is
// 401 everywhere and never falls back to the cookie.
func TestBearerTokensReachOnlyTheirOwnRoutes(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	api, r := w.mint(t, map[string]any{"name": "grafana", "kind": "api"})
	wantStatus(t, "mint api", r, 201)
	ing, r := w.mint(t, map[string]any{"name": "router-1", "kind": "ingest", "device": "core-router", "expiresAt": "never"})
	wantStatus(t, "mint ingest", r, 201)
	wantStatus(t, "add a drop-list entry", w.c.post("/api/droplist", map[string]string{"cidr": "203.0.114.0/24", "reason": "test entry"}), 201)
	r = w.c.post("/api/droplist/key", map[string]any{})
	wantStatus(t, "mint droplist key", r, 201)
	var dk struct{ Key string }
	r.json(t, &dk)
	if dk.Key == "" {
		t.Fatalf("no droplist key: %s", r)
	}

	anon := w.a.client()
	reads := []string{"/api/events", "/api/flags", "/api/stats", "/api/devices", "/api/matches?entries=all"}
	elsewhere := []string{"/api/definitions", "/api/audit", "/api/auth/users", "/api/tokens", "/api/droplist.rsc", "/api/auth/session"}
	for _, p := range reads {
		wantStatus(t, "api token GET "+p, anon.bearer(api.Value, "GET", p), 200)
		wantStatus(t, "ingest token GET "+p, anon.bearer(ing.Value, "GET", p), 404)
		wantStatus(t, "droplist token GET "+p, anon.bearer(dk.Key, "GET", p), 404)
	}
	for _, p := range elsewhere {
		wantStatus(t, "api token GET "+p, anon.bearer(api.Value, "GET", p), 404)
	}
	for _, p := range []string{"/api/ingest/routeros", "/api/ingest/router-backup"} {
		// Whatever the empty push is answered with (an address not enrolled
		// for the device, say), the token itself is accepted.
		if r := anon.bearer(ing.Value, "POST", p); r.Status == 401 {
			t.Errorf("ingest token POST %s was refused as a credential: %s", p, r)
		}
		wantStatus(t, "api token POST "+p, anon.bearer(api.Value, "POST", p), 404)
		wantStatus(t, "droplist token POST "+p, anon.bearer(dk.Key, "POST", p), 404)
	}
	wantStatus(t, "droplist token GET /api/droplist.rsc", anon.bearer(dk.Key, "GET", "/api/droplist.rsc"), 200)
	wantStatus(t, "ingest token GET /api/droplist.rsc", anon.bearer(ing.Value, "GET", "/api/droplist.rsc"), 404)

	// An unknown token is 401 everywhere, and a session cookie does not
	// rescue it.
	unknown := "gnt_" + randHex(t, 16)
	for _, p := range append(reads, "/api/droplist.rsc", "/api/auth/users") {
		wantStatus(t, "unknown token GET "+p, anon.bearer(unknown, "GET", p), 401)
	}
	wantStatus(t, "unknown token POST /api/ingest/routeros", anon.bearer(unknown, "POST", "/api/ingest/routeros"), 401)
	withCookie := w.c.do("GET", "/api/events", nil, map[string]string{"Authorization": "Bearer " + unknown})
	wantStatus(t, "unknown token with a valid session cookie", withCookie, 401)
	wantStatus(t, "the same cookie without the bad token", w.c.get("/api/events"), 200)
}

// #1202 B4.26: deleting a user revokes their tokens and sessions
// ({username, tokensRevoked}); the admin account cannot be deleted while it
// is the last admin.
func TestDeletingAUserRevokesTheirTokensAndSessionsAndTheLastAdminStays(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	// A second admin who mints a token and holds a session.
	r := w.c.post("/api/auth/users", map[string]string{
		"username": "second-admin", "password": "pw-second-9X7q!", "role": "admin",
		"adminPassword": w.admin.pass, "adminCode": w.admin.code(t, w.a, 0),
	})
	wantStatus(t, "create the second admin", r, 201)
	second := &acct{name: "second-admin", pass: "pw-second-9X7q!"}
	sc, r := w.signIn(t, second, "")
	wantStatus(t, "second admin sign-in", r, 200)
	second.secret, second.codes = enrol(t, w.a, sc, second.pass)

	tok := sc.post("/api/tokens", map[string]any{"name": "seconds-token", "kind": "api", "password": second.pass})
	wantStatus(t, "second admin mints a token", tok, 201)
	var m mintedToken
	tok.json(t, &m)
	anon := w.a.client()
	wantStatus(t, "token before the owner is deleted", anon.bearer(m.Value, "GET", "/api/stats"), 200)
	if !sc.session(t).Authenticated {
		t.Fatal("test setup: the second admin's session should be live")
	}

	list := w.c.get("/api/auth/users")
	var rows []struct{ ID, Username string }
	list.json(t, &rows)
	var secondID, adminID string
	for _, u := range rows {
		switch u.Username {
		case "second-admin":
			secondID = u.ID
		case "mv-admin":
			adminID = u.ID
		}
	}

	del := w.c.del("/api/auth/users/"+secondID, map[string]string{"password": w.admin.pass})
	wantStatus(t, "delete the second admin", del, 200)
	var out struct {
		Username      string `json:"username"`
		TokensRevoked int    `json:"tokensRevoked"`
	}
	del.json(t, &out)
	if out.Username != "second-admin" || out.TokensRevoked != 1 {
		t.Errorf("delete answered %+v, want {second-admin 1}", out)
	}
	wantStatus(t, "the deleted user's token", anon.bearer(m.Value, "GET", "/api/stats"), 401)
	if sc.session(t).Authenticated {
		t.Error("the deleted user's session still works")
	}

	// The remaining (last) admin cannot be deleted.
	d := w.c.del("/api/auth/users/"+adminID, map[string]string{"password": w.admin.pass})
	if d.Status != 409 || (d.problemClass() != "last-admin" && d.problemClass() != "conflict") {
		t.Errorf("deleting the last admin = %s, want 409 last-admin (or conflict, for the signed-in account)", d)
	}
	if !w.c.session(t).Authenticated {
		t.Error("the last admin lost their session")
	}
}
