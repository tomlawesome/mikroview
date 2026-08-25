// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// access is the level of caller a route requires once authentication is
// active (an account exists and auth has not been disabled).
type access int

const (
	// accessPublic: reachable with no session at all. Every entry here
	// is a deliberate hole in the authentication wall and should be
	// scrutinised on sight.
	accessPublic access = iota
	// accessUser: any authenticated session, regardless of role.
	accessUser
	// accessAdmin: an authenticated session whose role is admin.
	accessAdmin
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessUser:
		return "user"
	default:
		return "admin"
	}
}

// routeExpectation is one row of the authorization matrix: a route, the
// access level it must enforce, and why.
type routeExpectation struct {
	method string
	path   string
	want   access
	// why documents the intent, so a future change that flips a level
	// has to consciously rewrite the justification rather than quietly
	// edit a constant.
	why string
}

// authzMatrix is the single source of truth for who may reach what.
//
// This exists because per-endpoint spot-check tests only cover the
// endpoints somebody remembered to write a test for. A real permission
// gap shipped exactly that way: POST /api/flags/{id}/clear-permanent
// was reachable by any authenticated non-admin and could permanently
// suppress detection for a chosen target, unlogged, because no test
// asserted otherwise and the omission looked like a deliberate choice.
//
// TestEveryRouteIsInTheAuthorizationMatrix below fails when a route is
// added to Routes() without a row here, so the next endpoint cannot
// reach main without someone stating its access level on the record.
var authzMatrix = []routeExpectation{
	// -- Public by necessity -------------------------------------------
	{http.MethodGet, "/api/healthz", accessPublic,
		"liveness probe -- hit by Docker's HEALTHCHECK, which has no session"},
	{http.MethodGet, "/api/auth/session", accessPublic,
		"reports auth state so the frontend can decide which screen to draw; must answer before a session exists"},
	{http.MethodPost, "/api/auth/login", accessPublic,
		"the login endpoint itself"},
	{http.MethodPost, "/api/auth/logout", accessPublic,
		"calling it without a session is a harmless no-op, not worth a 401"},
	{http.MethodPost, "/api/auth/password", accessUser,
		"changes the caller's own password, so any signed-in user may reach it -- not admin-gated, because a " +
			"non-admin unable to change their own credential is the gap this closes (#294 item 4). It acts only on " +
			"the session's own account: there is no username in the body to point elsewhere, deliberately, the same " +
			"way /api/auth/oidc/link takes its target from the session rather than the request"},
	{http.MethodGet, "/api/auth/oidc/login", accessPublic,
		"starts the SSO redirect; a login must work before a session exists"},
	{http.MethodGet, "/api/auth/oidc/callback", accessPublic,
		"the provider redirects the browser here; protected by state/nonce/PKCE, not by a session"},
	{http.MethodPost, "/api/auth/register", accessPublic,
		"first-run only -- auth.Store.Register refuses once any account exists"},

	{http.MethodGet, "/api/config/problems", accessAdmin,
		"config key names, filesystem paths, the OIDC issuer URL and SMTP hosts are an infrastructure map; a non-admin gets an empty list rather than a 403, since whether problems exist is itself information"},

	// -- Any authenticated user ----------------------------------------
	{http.MethodGet, "/api/events", accessUser,
		"core read: the live firewall event feed"},
	{http.MethodGet, "/api/devices", accessUser, "core read"},
	{http.MethodGet, "/api/matches", accessUser,
		"a read over already-collected evidence, same tier as events/flags/stats/devices above -- also reachable via a read-only API token (readOnlyRoutes), since birdcage-style external correlation by source is the reason internal/matchlog exists. Renamed from /api/watchlist/matches by #407 when the watchlist noun was retired; the access decision is unchanged. " +
			"WIDENED by #586, and the widening is the part to scrutinise: entries=all serves the most recent matches across every entry, so a caller no longer needs to know a mac or ip to read from this log, and that includes a read-only token holder. Kept on this tier deliberately rather than promoted to admin, for three reasons. " +
			"The gate the issue asks for is the same gate the existing query carries. The mode learns nothing new in kind: a token that already reaches GET /api/events reads the live feed for every device, and a bounded page of matches is a strictly smaller view of the same traffic. And it is bounded by construction, not by the caller -- matchlog.RecentQuery clamps the limit to 5000 (100 by default) before either backend runs, which is the property that stops an all-entries read on an unrate-limited route being an arbitrarily large response. " +
			"What did NOT change: the per-identity path still refuses an empty identity (matchlog.ErrEmptyIdentity), and entries=all refuses to be combined with mac/ip, so 'no identity' can never silently mean 'every device'"},
	{http.MethodGet, "/api/rules", accessUser, "core read"},
	{http.MethodGet, "/api/third-party-notices", accessUser,
		"licence compliance: the copyright/licence texts of everything statically linked into this binary, which MIT/BSD/ISC/Apache-2.0 all require to accompany a binary distribution. Session-gated rather than public only because it is also a precise dependency-and-version inventory -- it withholds nothing, since the same file is in the public repo and the image"},
	{http.MethodGet, "/api/stats", accessUser, "core read"},
	{http.MethodGet, "/api/ws", accessUser,
		"live tail; additionally same-origin checked (see checkOrigin)"},
	{http.MethodGet, "/api/lookup/ip/{ip}", accessUser,
		"on-demand reputation lookup, proxied so no API key reaches the browser"},
	{http.MethodGet, "/api/routeros/{device}/rules", accessUser,
		"the pushed firewall rule table (#186 step 4) -- same tier as the event stream it annotates: rule comments/chains are already visible in events, and the lookup button is a user-facing affordance"},
	{http.MethodGet, "/api/routeros/{device}/nat", accessUser,
		"the pushed NAT table, same reasoning as the rules row above"},
	{http.MethodGet, "/api/flags", accessUser, "core read"},
	{http.MethodPost, "/api/flags/clear-all", accessUser,
		"same reversibility as the per-flag clear below, at bulk -- regular clears only, never creates an exclusion"},
	{http.MethodPost, "/api/flags/{id}/clear", accessUser,
		"reversible: a cleared flag raises again on the next matching event, so any user may dismiss noise"},

	// -- Admin only ----------------------------------------------------
	{http.MethodPost, "/api/flags/{id}/clear-permanent", accessAdmin,
		"NOT reversible without an admin: permanently suppresses detection for a (type, target) until someone undoes it"},
	{http.MethodGet, "/api/flags/exclusions", accessAdmin,
		"the review surface for permanent exclusions"},
	{http.MethodDelete, "/api/flags/exclusions/{id}", accessAdmin,
		"undoes an exclusion, re-arming detection"},
	// The definitions surface (#407), writes still strictly admin --
	// exactly matching what /api/detectors and /api/watchlist/entries
	// each enforced before it replaced them. #385 records the owner
	// decision that non-admins should eventually see settings surfaces
	// read-only; #490 is that phase 2's RBAC work, widening the list GET
	// below one row at a time while leaving every mutation here closed.
	{http.MethodGet, "/api/definitions", accessUser,
		"widened for the viewer-readable settings page (#490): a signed-in non-admin can see every definition's on/off state, scope and tuned params, same as an admin -- the design record's authz-matrix clause widens this GET deliberately, one row at a time, while every write below it stays admin-only"},
	{http.MethodPost, "/api/definitions", accessAdmin,
		"creates a definition -- a non-admin should not be able to add new server-side traffic surveillance"},
	{http.MethodGet, "/api/definitions/schema", accessAdmin,
		"the tunable knobs of every definition this deployment holds; same tier as the definitions themselves, and it enumerates the catalogue"},
	{http.MethodGet, "/api/definitions/{id}", accessAdmin,
		"one definition, same reasoning as the list"},
	{http.MethodPut, "/api/definitions/{id}", accessAdmin,
		"disabling a definition blinds the tool, and re-tuning one changes what fires; strictly admin"},
	{http.MethodDelete, "/api/definitions/{id}", accessAdmin,
		"removes an operator-authored definition entirely; same weight as creating it"},
	{http.MethodPost, "/api/definitions/{id}/clone", accessAdmin,
		"creates a definition, same reasoning as POST /api/definitions"},
	{http.MethodPost, "/api/definitions/{id}/reset", accessAdmin,
		"discards every param override in one call -- a configuration change, same tier as PUT"},
	{http.MethodPost, "/api/definitions/{id}/replay", accessAdmin,
		"re-runs a definition over the stored event corpus with candidate params: it reads every event in the ring and returns matching evidence, so it is at least as revealing as the definition list it belongs to"},
	{http.MethodPost, "/api/definitions/{id}/promote", accessAdmin,
		"changes what future traffic counts as expected for a device -- same weight as creating the definition"},
	{http.MethodPost, "/api/definitions/{id}/observing", accessAdmin,
		"same reasoning as promote"},
	{http.MethodGet, "/api/naming/provenance", accessAdmin,
		"says which layer supplies the name shown for one token, and whether a label saved here would be shadowed by a router-pushed one (#413). Admin for two reasons: the editor it serves gives viewers no pencil at all, so no viewer ever calls it; and the answer is a partial map of which router names which host, the same administrative metadata GET /api/entities is gated for"},

	{http.MethodGet, "/api/entities", accessAdmin, "admin-managed labels/tags"},
	{http.MethodPost, "/api/entities", accessAdmin, "admin-managed labels/tags"},
	{http.MethodDelete, "/api/entities", accessAdmin, "admin-managed labels/tags"},

	{http.MethodGet, "/api/suggestions", accessAdmin,
		"a suggestion's Justification names a specific rule/device -- same tier as the expectation definitions it can become"},
	{http.MethodPost, "/api/suggestions/{id}/accept", accessAdmin,
		"creates a real expectation definition -- same reasoning as POST /api/definitions"},
	{http.MethodPost, "/api/suggestions/{id}/hide", accessAdmin,
		"same tier as accept: declining a suggestion is the same class of decision"},
	{http.MethodPost, "/api/suggestions/{id}/unhide", accessAdmin,
		"same reasoning as hide"},
	{http.MethodPost, "/api/suggestions/reset", accessAdmin,
		"destructively wipes every expectation definition -- the most dangerous single endpoint in this feature, strictly admin"},
	{http.MethodPost, "/api/auth/oidc/link", accessUser,
		"converts your OWN account to SSO-only; the target comes from the session, never the request, so a user can only ever affect themselves"},
	{http.MethodPost, "/api/auth/users", accessAdmin, "account creation"},
	{http.MethodGet, "/api/auth/users", accessAdmin,
		"who holds an account, and which one is the admin -- that is the map of whose account is worth attacking. #490 widened the other three settings GETs for the viewer-readable engine room and deliberately left this one closed: the owner's ruling, 2026-08-24, is that the account list stays admin-only, so the room's people door is absent for a viewer rather than read-only"},
	{http.MethodDelete, "/api/auth/users/{id}", accessAdmin,
		"removes an account and revokes its sessions and API tokens"},
	{http.MethodPost, "/api/tokens", accessAdmin, "mints a bearer credential"},
	{http.MethodGet, "/api/tokens", accessUser,
		"widened for the viewer-readable settings page (#490): a signed-in non-admin can see issued bearer credentials' metadata, same as an admin -- safe because the raw value never appears here, only in the one-time mint response, and minting/revoking below stay admin-only"},
	{http.MethodDelete, "/api/tokens/{id}", accessAdmin, "revokes a bearer credential"},
	{http.MethodGet, "/api/setup/status", accessUser,
		"widened for the viewer-readable settings page (#490): a signed-in non-admin can see every device, source address and pushed table the setup wizard shows, same as an admin. It now also carries the ledger's marks (#487), for the same reason: an empty stream explains its own silence with the forced-past line that accounts for it, and a viewer looking at that stream needs the explanation as much as an admin does. The write side is a separate, admin-only route (POST /api/setup/mark)"},
	{http.MethodPost, "/api/setup/mark", accessAdmin,
		"writes to the setup wizard's claim ledger and to the audit log (#487) -- #490 keeps \"Run setup…\" absent for viewers and there is no read-only wizard, so a viewer has neither a way to reach this nor any business recording a decision under their own name"},
	{http.MethodGet, "/api/audit", accessAdmin,
		"the admin action trail; also the record an attacker would want to read to see whether they were noticed"},
}

// TestEveryRouteIsInTheAuthorizationMatrix is the guard that makes the
// matrix meaningful: it walks the real mux's registered patterns and
// fails if any route lacks a row above. Without it, the matrix would
// silently describe an ever-smaller fraction of the API as routes get
// added.
func TestEveryRouteIsInTheAuthorizationMatrix(t *testing.T) {
	declared := make(map[string]bool, len(authzMatrix))
	for _, r := range authzMatrix {
		declared[r.method+" "+r.path] = true
	}

	for _, pattern := range registeredRoutePatterns(t) {
		if !declared[pattern] {
			t.Errorf("route %q is registered but has no row in authzMatrix.\n"+
				"Add one stating who may reach it and why -- a new endpoint must not ship without that decision "+
				"being made explicitly. (This check exists because clear-permanent shipped unguarded exactly this way.)",
				pattern)
		}
	}

	// The reverse direction: a stale row for a route that no longer
	// exists would give false confidence that something is still being
	// checked.
	registered := make(map[string]bool)
	for _, p := range registeredRoutePatterns(t) {
		registered[p] = true
	}
	for _, r := range authzMatrix {
		if !registered[r.method+" "+r.path] {
			t.Errorf("authzMatrix has a row for %s %s, but no such route is registered -- remove the stale row",
				r.method, r.path)
		}
	}
}

// TestAuthorizationMatrixIsEnforced drives every row against a running
// server in the state that actually matters -- auth active, with both an
// admin and a plain user -- and asserts each of the three caller kinds
// gets what the matrix says.
func TestAuthorizationMatrixIsEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// Establish the admin via the real first-run path, then add a plain
	// user through the store (self-registration is closed by then).
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register",
		credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	if _, err := s.Auth.CreateUser("viewer", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	anon := &http.Client{}
	user := loggedInClient(t, ts.URL, "viewer", "password456")
	admin := loggedInClient(t, ts.URL, "admin", "password123")

	for _, r := range authzMatrix {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			// Skip the first-run endpoint: an account now exists, so it
			// legitimately refuses everyone. Its "public"
			// classification is about reachability before setup, which
			// its own dedicated tests cover.
			if r.path == "/api/auth/register" {
				t.Skip("first-run only; refuses all callers once an account exists (covered separately)")
			}

			// Logout destroys the session it is called with, so it gets
			// throwaway clients rather than the shared ones -- otherwise
			// probing it would silently log the shared user and admin
			// out and every subsequent row would fail with a misleading
			// 401. (It did, while this test was being written.)
			userClient, adminClient := user, admin
			if r.path == "/api/auth/logout" {
				userClient = loggedInClient(t, ts.URL, "viewer", "password456")
				adminClient = loggedInClient(t, ts.URL, "admin", "password123")
			}

			assertAccess(t, anon, ts.URL, r, "anonymous", r.want == accessPublic)
			assertAccess(t, userClient, ts.URL, r, "user", r.want != accessAdmin)
			assertAccess(t, adminClient, ts.URL, r, "admin", true)
		})
	}
}

// assertAccess issues one request and checks only whether it was let
// through the authorization layer -- not whether the handler behind it
// succeeded. A 404 for a placeholder ID, or a 400 for a body this test
// deliberately doesn't construct, both mean "you got past the gate",
// which is the only thing under test here. 401/403 mean refused.
func assertAccess(t *testing.T, c *http.Client, base string, r routeExpectation, who string, wantAllowed bool) {
	t.Helper()

	resp := doRouteRequest(t, c, base, r)
	defer resp.Body.Close()

	refused := resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	allowed := !refused

	if allowed != wantAllowed {
		verb := "was refused"
		if allowed {
			verb = "was allowed through"
		}
		t.Errorf("%s caller %s on %s %s (status %d), but the matrix says access is %q.\nReason on record: %s",
			who, verb, r.method, r.path, resp.StatusCode, r.want, r.why)
	}
}

// doRouteRequest turns a matrix row into a concrete request: wildcard
// segments get a placeholder that will not match real data (the handler
// behind it is expected to 404, which still proves the gate allowed the
// call through), and every non-GET carries the CSRF header so that
// check never masquerades as an authorization failure.
func doRouteRequest(t *testing.T, c *http.Client, base string, r routeExpectation) *http.Response {
	t.Helper()

	path := r.path
	for _, wildcard := range []string{"{id}", "{ip}", "{name}", "{device}"} {
		path = strings.Replace(path, wildcard, "matrix-probe-nonexistent", 1)
	}

	req, err := http.NewRequest(r.method, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.method != http.MethodGet {
		req.Header.Set(csrfHeaderName, csrfHeaderValue)
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", r.method, path, err)
	}
	return resp
}

// loggedInClient returns a client holding a live session cookie for the
// given credentials.
func loggedInClient(t *testing.T, base, username, password string) *http.Client {
	t.Helper()
	c := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, c, base+"/api/auth/login", credentialsRequest{Username: username, Password: password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login as %q failed with %d", username, resp.StatusCode)
	}
	return c
}

// registeredRoutePatterns reads the real route table -- the reason
// Server.routes() exists as data (see server.go).
func registeredRoutePatterns(t *testing.T) []string {
	t.Helper()
	s, _ := newTestServer(t)
	out := make([]string, 0, len(s.routes()))
	for _, r := range s.routes() {
		out = append(out, r.method+" "+r.path)
	}
	return out
}

// bearerMuxRoutes pins what the two bearer-token muxes serve. The
// matrix guard above walks Server.routes(), which is the session-
// authenticated mux only -- readOnlyRoutes() and ingestRoutes() are
// deliberately separate ServeMuxes (that separation is what makes "a
// token cannot reach a write endpoint" structural rather than checked),
// and nothing failed if a route were added to either one.
//
// These are the muxes a stolen credential dispatches to, so their
// contents are exactly what the blast radius of a leaked token is.
// Adding a route here is a decision to widen that; this test makes it a
// deliberate one.
var bearerMuxRoutes = map[string][]string{
	"read-only": {
		"GET /api/events",
		"GET /api/flags",
		"GET /api/stats",
		"GET /api/devices",
		"GET /api/matches",
	},
	"ingest": {
		"POST /api/ingest/routeros",
	},
}

// TestBearerMuxesServeOnlyTheirDeclaredRoutes reads the two
// constructors' source rather than probing the muxes: net/http's
// ServeMux exposes no way to enumerate what was registered, and probing
// can only confirm the routes we already thought of -- it cannot notice
// one that was added. Source is where "a route was added" is visible.
func TestBearerMuxesServeOnlyTheirDeclaredRoutes(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "auth.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing auth.go: %v", err)
	}

	found := map[string][]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		var name string
		switch fn.Name.Name {
		case "readOnlyRoutes":
			name = "read-only"
		case "ingestRoutes":
			name = "ingest"
		default:
			continue
		}
		found[name] = []string{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HandleFunc" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s registers a non-literal pattern -- this guard can no longer read it", name)
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquoting %s: %v", lit.Value, err)
			}
			found[name] = append(found[name], pattern)
			return true
		})
	}

	for name, want := range bearerMuxRoutes {
		got, ok := found[name]
		if !ok {
			t.Errorf("could not find the %s mux constructor in auth.go", name)
			continue
		}
		sort.Strings(got)
		expect := append([]string(nil), want...)
		sort.Strings(expect)
		if !reflect.DeepEqual(got, expect) {
			t.Errorf("the %s bearer mux serves %v, declared %v.\n"+
				"If this is intended, update bearerMuxRoutes -- and note that every route here is "+
				"reachable by anyone holding that kind of token, including a router-side script's "+
				"ingest token, which internal/auth.Token documents as readable by any RouterOS "+
				"'read' user.", name, got, expect)
		}
	}
}
