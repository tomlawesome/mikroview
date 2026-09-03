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
// active (an account exists and auth has not been disabled). #653
// introduced the third tier: viewer, sitting below user, replacing what
// used to be the single "any authenticated session" level of that name.
// Tiers stack (auth.Role.AtLeast): admin implies user implies viewer.
type access int

const (
	// accessPublic: reachable with no session at all. Every entry here
	// is a deliberate hole in the authentication wall and should be
	// scrutinised on sight.
	accessPublic access = iota
	// accessViewer: any authenticated session, regardless of role --
	// what accessUser used to mean before #653 split the non-admin space
	// in two. A viewer may see everything at this tier but change
	// nothing that affects the instance.
	accessViewer
	// accessUser: an authenticated session whose role is user or better
	// (i.e. user or admin) -- the operational tier #653 introduced for
	// writes that change what mikroview is watching or showing, without
	// touching the instance itself (accounts, tokens, config).
	accessUser
	// accessAdmin: an authenticated session whose role is admin.
	accessAdmin
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessViewer:
		return "viewer"
	case accessUser:
		return "user"
	default:
		return "admin"
	}
}

// allowsRole reports whether access level a permits a caller holding
// role, per auth.Role.AtLeast's stacked tiers. accessPublic permits
// every role including no session at all, which is why the anonymous
// caller in TestAuthorizationMatrixIsEnforced isn't driven through this
// method -- there's no auth.Role to hold when there's no session.
func (a access) allowsRole(role auth.Role) bool {
	switch a {
	case accessPublic:
		return true
	case accessViewer:
		return role.AtLeast(auth.RoleViewer)
	case accessUser:
		return role.AtLeast(auth.RoleUser)
	default:
		return role.AtLeast(auth.RoleAdmin)
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
	{http.MethodPost, "/api/auth/password", accessViewer,
		"changes the caller's own password, so any signed-in caller may reach it -- not admin-gated, because a " +
			"non-admin unable to change their own credential is the gap this closes (#294 item 4). It acts only on " +
			"the session's own account: there is no username in the body to point elsewhere, deliberately, the same " +
			"way /api/auth/oidc/link takes its target from the session rather than the request. #653 introduced the " +
			"viewer tier below user, and this stays open to it for the same reason -- even the lowest tier must be " +
			"able to change its own credential"},
	{http.MethodPost, "/api/auth/logout-all", accessViewer,
		"ends every session the caller holds, everywhere, then re-establishes the caller's own -- the settings " +
			"page's 'sign out everywhere' (#677). Same reasoning as /api/auth/password directly above: it acts only " +
			"on the session's own account (SessionStore.RevokeAllForUser(user.ID), the ID coming from the session, " +
			"never a request body), so there is nothing an admin-only gate would add, and the viewer tier must be " +
			"able to end its own stolen or forgotten sessions same as any other tier. Ending someone *else's* " +
			"sessions stays admin-only, via DELETE /api/auth/users/{id} below"},
	{http.MethodGet, "/api/auth/oidc/login", accessPublic,
		"starts the SSO redirect; a login must work before a session exists"},
	{http.MethodGet, "/api/auth/oidc/callback", accessPublic,
		"the provider redirects the browser here; protected by state/nonce/PKCE, not by a session"},
	{http.MethodPost, "/api/auth/register", accessPublic,
		"first-run only -- auth.Store.Register refuses once any account exists"},

	{http.MethodGet, "/api/config/problems", accessAdmin,
		"config key names, filesystem paths, the OIDC issuer URL and SMTP hosts are an infrastructure map; a non-admin gets an empty list rather than a 403, since whether problems exist is itself information"},
	{http.MethodGet, "/api/persistence", accessAdmin,
		"reports which backend (a JSON store's directory, or Postgres) this deployment's persisted state actually uses (#677's settings persistence row) -- a filesystem path is the same infrastructure-map disclosure /api/config/problems above is admin-gated for, so this follows it rather than defaulting to viewer the way most of Settings' other reads do"},

	{http.MethodPut, "/api/settings/store", accessAdmin,
		"sets the event buffer's size on the running instance (#796). Admin rather than user tier for two " +
			"separate reasons, either sufficient: it spends the host's memory, which is an instance-wide cost " +
			"nobody else can undo from inside the app, and shrinking it destroys held history -- the only route " +
			"in mikroview by which a caller can discard evidence a viewer or user was relying on. The read half " +
			"is deliberately not here: the figure and its bounds ride GET /api/stats, so a viewer sees the bar " +
			"and the number without being able to move it"},

	// -- Any authenticated session (viewer tier) ------------------------
	{http.MethodGet, "/api/events", accessViewer,
		"core read: the live firewall event feed"},
	{http.MethodGet, "/api/devices", accessViewer, "core read"},
	{http.MethodGet, "/api/devices/macs", accessViewer,
		"core read, same tier as /api/devices which it complements -- the persisted MAC-registry history " +
			"(first/last-seen, last-paired IP) backing the Entities page's named-things table (#675). No more " +
			"sensitive than the source IPs a viewer already reads off /api/events; it's a LAN client's MAC, not " +
			"credentials or config"},
	{http.MethodGet, "/api/matches", accessViewer,
		"a read over already-collected evidence, same tier as events/flags/stats/devices above -- also reachable via a read-only API token (readOnlyRoutes), since birdcage-style external correlation by source is the reason internal/matchlog exists. Renamed from /api/watchlist/matches by #407 when the watchlist noun was retired; the access decision is unchanged. " +
			"WIDENED by #586, and the widening is the part to scrutinise: entries=all serves the most recent matches across every entry, so a caller no longer needs to know a mac or ip to read from this log, and that includes a read-only token holder. Kept on this tier deliberately rather than promoted to admin, for three reasons. " +
			"The gate the issue asks for is the same gate the existing query carries. The mode learns nothing new in kind: a token that already reaches GET /api/events reads the live feed for every device, and a bounded page of matches is a strictly smaller view of the same traffic. And it is bounded by construction, not by the caller -- matchlog.RecentQuery clamps the limit to 5000 (100 by default) before either backend runs, which is the property that stops an all-entries read on an unrate-limited route being an arbitrarily large response. " +
			"What did NOT change: the per-identity path still refuses an empty identity (matchlog.ErrEmptyIdentity), and entries=all refuses to be combined with mac/ip, so 'no identity' can never silently mean 'every device'"},
	{http.MethodGet, "/api/rules", accessViewer, "core read"},
	{http.MethodGet, "/api/third-party-notices", accessViewer,
		"licence compliance: the copyright/licence texts of everything statically linked into this binary, which MIT/BSD/ISC/Apache-2.0 all require to accompany a binary distribution. Session-gated rather than public only because it is also a precise dependency-and-version inventory -- it withholds nothing, since the same file is in the public repo and the image"},
	{http.MethodGet, "/api/stats", accessViewer, "core read"},
	{http.MethodGet, "/api/stats/tops", accessViewer,
		"#644 round 21's per-minute top-port/top-talker columns -- the same tier as /api/stats above, since it is " +
			"a per-minute breakdown of data that endpoint already exposes in aggregate (byAction), not a new class " +
			"of read. #653's viewer tier took /api/stats down with it, and this follows for the same reason. " +
			"Deliberately NOT in readOnlyRoutes: HourTops' backward scan is heavier than anything else a " +
			"bearer token can already trigger on this tier, and nothing asked for that to be token-reachable"},
	{http.MethodGet, "/api/ws", accessViewer,
		"live tail; additionally same-origin checked (see checkOrigin)"},
	{http.MethodGet, "/api/lookup/ip/{ip}", accessViewer,
		"on-demand reputation lookup, proxied so no API key reaches the browser"},
	{http.MethodGet, "/api/routeros/{device}/rules", accessViewer,
		"the pushed firewall rule table (#186 step 4) -- same tier as the event stream it annotates: rule comments/chains are already visible in events, and the lookup button is a user-facing affordance"},
	{http.MethodGet, "/api/routeros/{device}/nat", accessViewer,
		"the pushed NAT table, same reasoning as the rules row above"},
	{http.MethodGet, "/api/routeros/{device}/addresses", accessViewer,
		"the pushed /ip/address table (#627), same tier as the rules/NAT rows above"},
	{http.MethodGet, "/api/routeros/{device}/wireguard", accessViewer,
		"the pushed WireGuard tables with derived per-tunnel state (#874), same tier as the rules/NAT/addresses rows above -- display data annotating what a viewer already sees on the topography"},
	{http.MethodGet, "/api/routeros/{device}/ppp-active", accessViewer,
		"the pushed /ppp/active table (#874), same tier and reasoning as the wireguard row above"},
	{http.MethodGet, "/api/flags", accessViewer, "core read"},
	{http.MethodGet, "/api/flags/expectations", accessViewer,
		"core read (#640's ledger): an expectation is the reason a firing is absent from the flags card above, so a caller who may read the flags but not the expectations behind them is reading half the story -- and reading the ledger changes nothing. Deliberately NOT in readOnlyRoutes: nothing asked for it to be token-reachable"},

	// -- Operational writes (user tier) ---------------------------------
	//
	// #653 introduced this tier below admin: these four flag actions used
	// to be open to any authenticated session (the old accessUser, now
	// called accessViewer above). The owner's ruling on #653 is that a
	// viewer -- who must not change anything that affects the instance --
	// may not make even a reversible change to what mikroview is
	// currently showing, so these tightened to require at least the user
	// role.
	{http.MethodPost, "/api/flags/clear-all", accessUser,
		"reversible: a cleared flag raises again on the next matching event, and a bulk clear records no expectation. Tightened from viewer to user tier by #653: reversible or not, this changes what mikroview is showing, which a viewer may not do"},
	{http.MethodPost, "/api/flags/{id}/verdict", accessUser,
		"#640: the four verdicts, and the only way one flag leaves the inbox now that the plain clear and " +
			"the admin-only clear-permanent are gone. User tier for all four, per the ratified design: the " +
			"expectation an expected verdict records is bounded by the firing the operator just looked at " +
			"and withdrawn by the undo below, where the exclude-forever it replaces was unbounded and " +
			"admin-only. Audit-logged, so who decided a pair stops being flagged stays answerable. " +
			"Tightened from viewer to user tier by #653, same reasoning as clear-all above"},
	{http.MethodDelete, "/api/flags/verdict/{id}", accessUser,
		"#638's undo affordance for the row above, and #640's withdrawal of the expectation an expected " +
			"verdict recorded -- same tier as judging in the first place, since reversing a judgement is no " +
			"more dangerous than making one. Not \"/{id}/verdict\": see the registration comment in " +
			"server.go for why that shape can't be registered here. Tightened from viewer to user tier by " +
			"#653, same reasoning as clear-all above"},
	{http.MethodDelete, "/api/flags/expectations/{id}", accessUser,
		"#640's Forget control on the ledger -- same tier as the verdict that records an expectation, since " +
			"the operator who can say \"expected\" can take it back, and an undo must not be harder to reach " +
			"than the thing it undoes. Forgetting only ever re-arms detection, which is the " +
			"safe direction"},

	// -- Admin only ----------------------------------------------------
	// The definitions surface (#407). #385 records the owner decision
	// that non-admins should eventually see settings surfaces read-only;
	// #490 was that phase 2's RBAC work, widening the list GET below one
	// row at a time while leaving every mutation here admin-only. #653
	// finished the job differently than #490 anticipated: rather than
	// widening each write to read-only-for-viewer, the owner's ruling
	// gave the whole surface -- reads and writes alike -- to the user
	// tier (the "watchers" bench gets full access), leaving only the
	// admin-only account/token/audit/config surfaces below untouched.
	{http.MethodGet, "/api/coverage/declarations", accessViewer,
		"a coverage-gap declaration (#630/#392) explains why a boundary-direction pair is intentionally quiet -- reading that explanation is the same viewer-tier read as GET /api/definitions below, not the user-tier write that authors one"},
	{http.MethodGet, "/api/definitions", accessViewer,
		"widened for the viewer-readable settings page (#490): a signed-in caller, even at the lowest tier, can see every definition's on/off state, scope and tuned params, same as an admin -- the design record's authz-matrix clause widens this GET deliberately. #653 went on to widen every write below it too, from admin to user tier, but left this one GET at viewer -- a viewer may see the whole surface, just not touch it"},
	{http.MethodPost, "/api/definitions", accessUser,
		"creates a definition. Was accessAdmin; #653's \"watchers\" bench ruling widened this to user tier -- a viewer still may not, since adding server-side traffic surveillance changes the instance"},
	{http.MethodGet, "/api/definitions/schema", accessUser,
		"the tunable knobs of every definition this deployment holds; same tier as the definitions themselves, and it enumerates the catalogue. Widened from admin to user tier by #653, same as the rest of this surface"},
	{http.MethodGet, "/api/definitions/{id}", accessUser,
		"one definition, same reasoning as the list. Widened from admin to user tier by #653"},
	{http.MethodPut, "/api/definitions/{id}", accessUser,
		"disabling a definition blinds the tool, and re-tuning one changes what fires. Was accessAdmin; #653's \"watchers\" bench ruling widened this to user tier"},
	{http.MethodDelete, "/api/definitions/{id}", accessUser,
		"removes an operator-authored definition entirely; same weight as creating it. Widened from admin to user tier by #653, same as POST above"},
	{http.MethodPost, "/api/definitions/{id}/clone", accessUser,
		"creates a definition, same reasoning as POST /api/definitions. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/reset", accessUser,
		"discards every param override in one call -- a configuration change, same tier as PUT. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/replay", accessUser,
		"re-runs a definition over the stored event corpus with candidate params: it reads every event in the ring and returns matching evidence, so it is at least as revealing as the definition list it belongs to. Widened from admin to user tier by #653, same as the rest of this surface"},
	{http.MethodPost, "/api/definitions/{id}/promote", accessUser,
		"changes what future traffic counts as expected for a device -- same weight as creating the definition. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/observing", accessUser,
		"same reasoning as promote. Widened from admin to user tier by #653"},
	{http.MethodGet, "/api/naming/provenance", accessUser,
		"says which layer supplies the name shown for one token, and whether a label saved here would be shadowed by a router-pushed one (#413). Was admin for two reasons: the editor it serves gave admins alone a pencil, so no non-admin ever called it; and the answer is a partial map of which router names which host, the same administrative metadata GET /api/entities is gated for. #653 widened both this and /api/entities to user tier together, so the reasoning now matches: a viewer still gets no pencil, but a user does, same as the entities surface it serves"},

	{http.MethodGet, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653's \"watchers\" bench ruling"},
	{http.MethodPost, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653"},
	{http.MethodDelete, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653"},
	{http.MethodPut, "/api/coverage/declarations/{key}", accessUser,
		"declaring a boundary intentionally quiet is an on-record explanation, same weight as an entity label -- and #653 moved entity labels to the user tier, so this row followed the reasoning its own justification already rested on rather than staying admin beside a neighbour that moved"},
	{http.MethodDelete, "/api/coverage/declarations/{key}", accessUser,
		"undeclares a coverage gap, re-exposing it as unexplained -- same tier as creating it"},

	{http.MethodGet, "/api/suggestions", accessUser,
		"a suggestion's Justification names a specific rule/device -- same tier as the expectation definitions it can become. Widened from admin to user tier by #653, same as the definitions surface"},
	{http.MethodPost, "/api/suggestions/{id}/accept", accessUser,
		"creates a real expectation definition -- same reasoning as POST /api/definitions. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/{id}/hide", accessUser,
		"same tier as accept: declining a suggestion is the same class of decision. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/{id}/unhide", accessUser,
		"same reasoning as hide. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/reset", accessUser,
		"destructively wipes every expectation definition -- the most dangerous single endpoint in this feature, and the one row in this whole surface the owner's #653 ruling considered keeping admin-only for that reason. It was widened to user tier anyway, on the view that the confirm:true body this handler requires is the real safeguard against an accidental call, not the role gate -- a safeguard user and admin are equally bound by"},
	{http.MethodPost, "/api/auth/oidc/link", accessViewer,
		"converts your OWN account to SSO-only; the target comes from the session, never the request, so a caller can only ever affect themselves, at any tier including viewer"},
	{http.MethodPost, "/api/auth/users", accessAdmin, "account creation"},
	{http.MethodGet, "/api/auth/users", accessAdmin,
		"who holds an account, and which one is the admin -- that is the map of whose account is worth attacking. #490 widened the other three settings GETs for the viewer-readable engine room and deliberately left this one closed: the owner's ruling, 2026-08-24, is that the account list stays admin-only, so the room's people door is absent for a viewer rather than read-only. #653 added a viewer role beneath that non-admin space and left this row exactly where it was -- account creation and the account list are the owner-level items #653's tiers deliberately keep out of user's reach too"},
	{http.MethodDelete, "/api/auth/users/{id}", accessAdmin,
		"removes an account and revokes its sessions and API tokens"},
	{http.MethodPost, "/api/tokens", accessAdmin, "mints a bearer credential"},
	{http.MethodGet, "/api/tokens", accessAdmin,
		"narrowed back from accessViewer (#657). #490 widened it to serve a viewer-readable settings page; #657 removed that page from a viewer's navigation, and ruled the doors station admin-only on the grounds that issuing keys is a setup task rather than using the product -- so the user tier deliberately loses metadata it could see before. The old reasoning (the raw value never appears here, so the read hands out no secret) is still true and no longer the point: the surface it was widened for is gone"},
	{http.MethodDelete, "/api/tokens/{id}", accessAdmin, "revokes a bearer credential"},
	{http.MethodGet, "/api/setup/status", accessViewer,
		"widened for the viewer-readable settings page (#490): a signed-in caller at any tier can see every device, source address and pushed table the setup wizard shows, same as an admin. It now also carries the ledger's marks (#487), for the same reason: an empty stream explains its own silence with the forced-past line that accounts for it, and a viewer looking at that stream needs the explanation as much as an admin does. The write side is a separate, admin-only route (POST /api/setup/mark)"},
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
// server in the state that actually matters -- auth active, with an
// admin and one account at each of the other two roles (#653) -- and
// asserts each of the four caller kinds gets what the matrix says.
func TestAuthorizationMatrixIsEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// Establish the admin via the real first-run path, then add one
	// account at each of the other two roles through the store
	// (self-registration is closed by then).
	postJSON(t, &http.Client{}, ts.URL+"/api/auth/register",
		credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	if _, err := s.Auth.CreateUser("operator", "password456", auth.RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Auth.CreateUser("watcher", "password789", auth.RoleViewer, time.Now()); err != nil {
		t.Fatal(err)
	}

	anon := &http.Client{}
	viewer := loggedInClient(t, ts.URL, "watcher", "password789")
	user := loggedInClient(t, ts.URL, "operator", "password456")
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
			// probing it would silently log the shared viewer/user/admin
			// out and every subsequent row would fail with a misleading
			// 401. (It did, while this test was being written.)
			viewerClient, userClient, adminClient := viewer, user, admin
			if r.path == "/api/auth/logout" {
				viewerClient = loggedInClient(t, ts.URL, "watcher", "password789")
				userClient = loggedInClient(t, ts.URL, "operator", "password456")
				adminClient = loggedInClient(t, ts.URL, "admin", "password123")
			}

			assertAccess(t, anon, ts.URL, r, "anonymous", r.want == accessPublic)
			assertAccess(t, viewerClient, ts.URL, r, "viewer", r.want.allowsRole(auth.RoleViewer))
			assertAccess(t, userClient, ts.URL, r, "user", r.want.allowsRole(auth.RoleUser))
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
