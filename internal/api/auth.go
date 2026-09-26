// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/logging"
)

var authLog = logging.New("auth-api")

const sessionCookieName = "mikroview_session"

// forcedAuthGateHeader marks a 403 that means "sign-in worked, but this
// session is stuck at a door" (MustChangePassword or the missing-second-
// factor gate below) rather than an ordinary refusal (wrong role, missing
// CSRF header). #1362: the frontend used to have no way to tell these
// apart from any other 403 except by matching the prose in the body, so
// an open tab across an upgrade that turned an existing session into one
// of these just showed the refusal as an error instead of routing to the
// door that gets it out. Read once, in api.ts's shared fetch wrapper, and
// matched on this header's value -- never on the message text below,
// which stays free to reword.
const forcedAuthGateHeader = "X-Mikroview-Auth-Gate"

const (
	forcedAuthGateMustChangePassword = "must-change-password"
	forcedAuthGateMustEnrolFactor    = "must-enrol-factor"
)

// writeForcedAuthGate is writeUnauthorized's (rest.go) sibling for this
// pair of doors: sets the machine-readable header before the human-
// readable body, same shape as that helper's WWW-Authenticate header.
func writeForcedAuthGate(w http.ResponseWriter, gate, msg string) {
	w.Header().Set(forcedAuthGateHeader, gate)
	http.Error(w, msg, http.StatusForbidden)
}

// changePasswordPath is the one route a session flagged
// MustChangePassword may reach (#1251) -- named once here rather than
// written as a literal in requireAuth, so the gate and the route table
// cannot drift apart silently.
const changePasswordPath = "/api/auth/password"

// secondFactorEnrolPaths are the routes a session may still reach while
// stuck at the forced-enrolment door (#1253) -- every step of enrolling
// either kind of second factor, and nothing else. Named once here, the
// same reasoning as changePasswordPath above, so requireAuth's gate and
// this list cannot drift apart silently.
//
// Four routes, not two, because #1250 gave an account two different
// factors to choose between: the authenticator-app pair
// (enrol generates the secret and shows the QR code, confirm activates
// it once the owner proves they can produce a code) and the passkey
// pair (register/begin starts the WebAuthn ceremony, register/finish
// completes it). Either pair alone satisfies the door -- see
// requireAuth's check, which asks HasSecondFactor, not "has TOTP". Both
// handlers mint recovery codes themselves on success (handleTOTPConfirm,
// handleAuthPasskeysRegisterFinish), so no separate route is needed for
// that.
//
// Deliberately excludes DELETE /api/auth/totp and the passkey
// rename/delete routes: there is nothing yet enrolled for those to act
// on while this gate holds, and admitting them would be surface this
// door has no reason to open.
var secondFactorEnrolPaths = map[string]bool{
	"/api/auth/totp/enrol":               true,
	"/api/auth/totp/confirm":             true,
	"/api/auth/passkeys/register/begin":  true,
	"/api/auth/passkeys/register/finish": true,
}

// cookieMaxAge is how long the browser itself remembers the cookie --
// deliberately longer than Auth.SessionTTL (the server-side idle
// timeout, which slides forward on use): the cookie value doesn't
// change on renewal, only the server's internal expiry does, so the
// browser just needs to hold onto it comfortably longer than any
// realistic idle gap.
const cookieMaxAge = 30 * 24 * time.Hour

// csrfHeaderName/Value: a lightweight CSRF mitigation, required on every
// mutating (non-GET/HEAD) request once auth is active. SameSite=Lax
// cookies already block a cross-site *form* POST from carrying the
// session cookie at all in modern browsers, so this is mostly defense-
// in-depth (and a safety net if a future mutating endpoint is ever
// added as something other than POST) rather than filling an active
// gap -- trivial for the real frontend to send on every fetch() call,
// impossible for a cross-site <form> to set.
const (
	csrfHeaderName  = "X-Requested-With"
	csrfHeaderValue = "mikroview"
)

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

type contextKey int

const (
	userContextKey contextKey = iota
	// ingestTokenContextKey carries the authenticated *auth.Token (kind
	// ingest) through to handleIngestRouterOS -- see requireAuth's
	// bearer-token branch and ingestTokenFromContext below. Device
	// identity for a push comes from this token, never from the request
	// body, so a payload cannot claim to be from a router other than the
	// one its own credential is scoped to.
	ingestTokenContextKey
	// droplistTokenContextKey is ingestTokenContextKey's counterpart for
	// the droplist-pull kind (#1224) -- carries the authenticated
	// *auth.Token through to handleDroplistPull, which needs its ID for
	// the ingest-limiter reservation and the LastUsedAt touch, and needs
	// it from the token that authenticated this exact request rather
	// than looking it up a second time.
	droplistTokenContextKey
)

// exemptPaths lists routes reachable without a session once auth is
// active -- either because they must work before one exists (register,
// login, session-status polling) or because they're already established
// precedent for staying open regardless of auth (healthz, hit by
// Docker's own HEALTHCHECK). Logout is included too: calling it without
// a session is a harmless no-op, not worth a 401 for.
var exemptPaths = map[string]bool{
	"/api/healthz":       true,
	"/api/auth/session":  true,
	"/api/auth/register": true,
	"/api/auth/login":    true,
	"/api/auth/logout":   true,
	// Both are GET (a top-level browser redirect/navigation the provider
	// issues, not a fetch() the frontend controls) so isSafeMethod
	// already exempts them from the CSRF-header check above -- being
	// listed here is what exempts them from requiring an existing
	// session, which is the actual point: a login has to work before a
	// session exists. State/nonce/PKCE (see oidc.go) are the callback's
	// real protection against a forged request, not the session check.
	"/api/auth/oidc/login":    true,
	"/api/auth/oidc/callback": true,
	// POST /api/auth/login/factor (#1249) is the second half of a login
	// that stopped at handleAuthLogin because the account holds an
	// active TOTP factor -- reached with the short-lived pending-login
	// cookie, never a session, so it has to work before one exists, same
	// reasoning as /api/auth/login itself.
	"/api/auth/login/factor": true,
	// POST /api/auth/login/factor/begin (#1250) is the passkey half's own
	// "begin" step, reached with the identical pending-login cookie --
	// same reasoning as /api/auth/login/factor directly above.
	"/api/auth/login/factor/begin": true,
}

// bootstrapExemptPaths lists the (smaller) set of routes reachable
// while no account exists yet *and* auth hasn't been explicitly
// disabled -- only what's needed to show and complete the one-time
// account-creation screen.
// Deliberately narrower than exemptPaths: everything else 401s during
// this window, closing the gap where live data (events/flags/stats)
// used to be readable by anyone who reached mikroview before a decision
// was made (see requireAuth's doc comment).
var bootstrapExemptPaths = map[string]bool{
	"/api/healthz":       true,
	"/api/auth/session":  true,
	"/api/auth/register": true,
	// So the very first-ever login can happen via SSO -- symmetric with
	// /api/auth/register already being bootstrap-exempt for the local-
	// password path.
	"/api/auth/oidc/login":    true,
	"/api/auth/oidc/callback": true,
}

// sessionUser resolves r's session cookie to a user, if any -- shared by
// requireAuth and handleAuthSession so the invalidation rules (expiry,
// unknown user, and a session issued before the user's last password
// reset -- see User.PasswordChangedAt) live in exactly one place. A
// session that fails the PasswordChangedAt check is proactively revoked
// here rather than left to expire naturally, since it's already known
// to be invalid.
func (s *Server) sessionUser(r *http.Request, now time.Time) (*auth.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, false
	}
	sess, ok := s.Sessions.Validate(cookie.Value, now)
	if !ok {
		return nil, false
	}
	user, ok := s.Auth.Get(sess.UserID)
	if !ok {
		return nil, false
	}
	if sess.IssuedAt.Before(user.PasswordChangedAt) {
		s.Sessions.Revoke(sess.ID)
		return nil, false
	}
	return user, true
}

// readOnlyRoutes is the only handler set a bearer API token (issue
// #101) can ever reach -- deliberately its own separate *http.ServeMux
// with just these five GET routes registered, rather than a per-request
// allowlist check layered in front of the real mux. When this list
// changes, update the route list in TokensOverlay.svelte's hint text
// too -- it went stale once already (#326: four routes listed, five
// served). That's what makes
// "a token can never reach a write/clear/config endpoint" structural:
// there is no code path from a bearer-authenticated request to
// handleFlagsClear, handleDefinitionsUpdate, handleAuthCreateUser,
// etc. -- those handlers are simply never registered on this mux, so a
// request for any of them (regardless of method) falls through to
// ServeMux's own 404, the same as a route that never existed.
func (s *Server) readOnlyRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/flags", s.handleFlagsList)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/matches", s.handleMatchesQuery)
	return mux
}

// ingestRoutes is the only handler set an ingest bearer token (issue
// #186) can ever reach -- its own separate *http.ServeMux, the same
// structural reasoning readOnlyRoutes documents above: there is no code
// path from an ingest-authenticated request to anything else registered
// on the real mux, including readOnlyRoutes' own GETs. That
// separation is exactly what stops an ingest token -- readable by any
// `read`-capable user on the router it came from, per #186 step 5 --
// from becoming a read-everything credential the way a stolen one
// reaching readOnlyRoutes would.
func (s *Server) ingestRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/ingest/routeros", s.handleIngestRouterOS)
	mux.HandleFunc("POST /api/ingest/router-backup", s.handleIngestRouterBackup)
	return mux
}

// droplistPullRoutes is the third and narrowest bearer mux -- one route,
// for the droplist-pull key (#1224) a router's own scheduled
// `/tool fetch` presents. This is the whole blast radius of a leaked
// pull key: it can read the generated .rsc drop-list feed and nothing
// else, the same structural guarantee readOnlyRoutes and ingestRoutes
// document above -- there is no other handler registered on this mux for
// a bearer-authenticated request to fall through to.
func (s *Server) droplistPullRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/droplist.rsc", s.handleDroplistPull)
	return mux
}

const bearerPrefix = "Bearer "

// bearerToken extracts the raw token value from an Authorization: Bearer
// <token> header, if present in that exact form -- any other
// Authorization scheme (or none at all) is not this package's concern
// and falls through to the normal session-cookie path.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return "", false
	}
	return strings.TrimPrefix(h, bearerPrefix), true
}

// requireAuth has two states, checked in order, plus a third check
// (bearer tokens) nested inside the second:
//
//  1. Undecided (Count()==0): only bootstrapExemptPaths stay reachable
//     -- just enough to show and complete account creation. Everything
//     else 401s, closing the window where live data (events/flags/
//     stats) used to be readable by anyone who reached mikroview before
//     an account existed. Tokens can't meaningfully exist yet either
//     (creating one requires an admin, and there is none), so this
//     stays unchanged.
//  2. Active (Count()>0): CSRF header + exemptPaths + (session cookie OR
//     bearer token). A bearer token is checked first, before the CSRF/
//     exempt-path logic below, since it identifies a non-browser,
//     service-to-service caller -- CSRF is a browser-cookie-specific
//     mitigation that doesn't apply to it. A valid *read-only API* token
//     is dispatched to readOnlyRoutes, never to next (the full mux); a
//     valid *ingest* token (#186) is dispatched to ingestRoutes instead;
//     a valid *droplist-pull* token (#1224) is dispatched to
//     droplistPullRoutes instead -- the three kinds reach three disjoint
//     muxes, none of which is the real one, so a token of any kind is
//     structurally incapable of reaching a session-gated route. An
//     invalid or revoked token is rejected outright with 401, not
//     silently treated as "no token" and passed through to the
//     session-cookie check.
//
// There is deliberately no third state for "this deployment opted out of
// authentication". That mode existed and was removed: an unauthenticated
// mikroview shows which hosts are being scanned, which rules fire, and
// which accounts matter, and no amount of "it's only for five minutes"
// survives contact with a deployment nobody got round to changing.
// Creating a local account takes one screen, and it is the floor now.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	readOnly := s.readOnlyRoutes()
	ingest := s.ingestRoutes()
	droplistPull := s.droplistPullRoutes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth.Count() == 0 {
			if !bootstrapExemptPaths[r.URL.Path] {
				http.Error(w, "setup required", http.StatusServiceUnavailable)
				return
			}
			// No session exists yet to carry a CSRF check the normal
			// way, but /api/auth/register is the highest-consequence
			// endpoint in the app -- it creates the permanent admin
			// account -- and without this, a bare cross-site <form>
			// POST (no
			// SameSite cookie needed, since none exists yet) could
			// make that irreversible choice on a victim's behalf.
			// Every other bootstrap-exempt path is GET, so
			// isSafeMethod already exempts it here without needing a
			// path-specific check.
			if !isSafeMethod(r.Method) && r.Header.Get(csrfHeaderName) != csrfHeaderValue {
				http.Error(w, "missing required header", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if raw, ok := bearerToken(r); ok {
			// auth.TokenKindAPI, named explicitly: an ingest token
			// (#186) is a bearer credential too, and it lives in a
			// script on a router where any `read` user can read it. If
			// this branch accepted any valid token it would quietly
			// promote that credential into read access to every event,
			// flag and device mikroview holds. Naming the kind makes an
			// ingest token fall through to the 401 below, exactly like a
			// revoked one.
			if _, valid := s.Tokens.Authenticate(raw, auth.TokenKindAPI, time.Now()); valid {
				readOnly.ServeHTTP(w, r)
				return
			}
			// Tried second, not first: TokenKindAPI is the far more
			// common credential (every read-only integration uses one),
			// and Authenticate's own lookup cost doesn't depend on which
			// kind is tried first, so this ordering costs nothing and
			// keeps the more-common path textually first.
			if tok, valid := s.Tokens.Authenticate(raw, auth.TokenKindIngest, time.Now()); valid {
				ingest.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ingestTokenContextKey, tok)))
				return
			}
			// Tried last: a droplist-pull key exists once per deployment
			// (minted from Settings, not handed out per-integration the
			// way API/ingest tokens are), so it is by far the rarest of
			// the three to actually be presented here.
			if tok, valid := s.Tokens.Authenticate(raw, auth.TokenKindDroplistPull, time.Now()); valid {
				droplistPull.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), droplistTokenContextKey, tok)))
				return
			}
			writeUnauthorized(w, "invalid or revoked token")
			return
		}

		if !isSafeMethod(r.Method) && r.Header.Get(csrfHeaderName) != csrfHeaderValue {
			http.Error(w, "missing required header", http.StatusForbidden)
			return
		}
		if exemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		user, ok := s.sessionUser(r, time.Now())
		if !ok {
			writeUnauthorized(w, "unauthorized")
			return
		}
		// An account an admin has just reset (#1251) signed in with a
		// code somebody else chose and read out over the phone. Until it
		// is traded for a password only its owner knows, the session can
		// reach exactly one route: the one that does the trading. Every
		// other route -- read or write, viewer-tier or admin -- is 403.
		//
		// Enforced here rather than in each handler for the same reason
		// requireAuth exists at all: a gate that has to be remembered per
		// endpoint is one forgotten endpoint away from not being a gate.
		// GET /api/auth/session and /api/auth/logout stay reachable
		// without a special case, because exemptPaths above has already
		// returned by the time this runs. /api/auth/logout-all is not on
		// that list and gets no special case here either: ending every
		// session but this one needs to trust whose sessions they are,
		// which is exactly what a reset-code session does not have yet
		// -- it 403s like everything else until the password is changed.
		if user.MustChangePassword && r.URL.Path != changePasswordPath {
			writeForcedAuthGate(w, forcedAuthGateMustChangePassword, "an administrator reset this account -- set a new password before going any further")
			return
		}
		// The forced-enrolment door (#1253): a second factor is mandatory
		// on every local account (owner, 2026-09-18, "58 every local
		// account - sso handles its own auth, we just respect it"), and
		// this is where that's enforced -- at the door, on every request,
		// not as a one-off migration. Any local account that reaches
		// sign-in without one is sent to enrolment and can reach nothing
		// else, whatever left it in that state: never enrolled, cleared
		// by an admin (ClearAllSecondFactors/-clear-second-factor), or
		// hand-edited into existence. The same shape
		// MustChangePassword's gate above uses -- checked here rather
		// than per-handler, an allowlist rather than a denylist, so a
		// forgotten new route defaults to blocked, not open.
		//
		// LocalPassword() -- HasLocalPassword -- is the SSO-only escape:
		// an account provisioned or linked away to SSO (LinkOIDCIdentity)
		// has no local password for a factor to protect, and its
		// identity provider does its own authentication. An admin that
		// has linked SSO keeps both its password and its factor (see
		// LinkOIDCIdentity), so it still satisfies this and is never
		// caught by it on that account alone.
		//
		// Checked after MustChangePassword deliberately: a session
		// holding a reset code has no credential only its owner knows
		// yet, so it is sent to set one first: MustChangePassword's own
		// check above already returned by the time this runs for that
		// session. Once the password is set, MustChangePassword clears
		// and the very next request lands here instead if a factor is
		// still missing -- the two doors run in sequence, never both
		// open at once.
		//
		// The !user.MustChangePassword guard is what actually makes
		// that sequencing hold: MustChangePassword's own gate above lets
		// exactly one path through while it's set -- changePasswordPath
		// -- and that path is not in secondFactorEnrolPaths (see its own
		// doc comment: nothing is enrolled yet for those routes to act
		// on). Without this guard, a reset-code account with no second
		// factor would fall through to this gate on its one admitted
		// path and be refused that too -- 403 on the only route that
		// could ever get it out of MustChangePassword, a deadlock no
		// request from that account could ever escape.
		if !user.MustChangePassword && user.LocalPassword() && !user.HasSecondFactor() && !secondFactorEnrolPaths[r.URL.Path] {
			writeForcedAuthGate(w, forcedAuthGateMustEnrolFactor, "this account has no second factor -- enrol one before going any further")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	})
}

// userFromContext returns the authenticated user for this request, or
// nil if auth is inactive (zero users exist) -- handlers that need to
// know who's calling (handleAuthCreateUser's admin check) use this;
// most handlers don't need to.
func userFromContext(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userContextKey).(*auth.User)
	return u
}

// ingestTokenFromContext returns the authenticated ingest token for this
// request -- only ever non-nil inside handleIngestRouterOS, which is the
// sole handler ingestRoutes registers and therefore the only one
// requireAuth's ingest-token branch ever dispatches to.
func ingestTokenFromContext(r *http.Request) *auth.Token {
	t, _ := r.Context().Value(ingestTokenContextKey).(*auth.Token)
	return t
}

// droplistTokenFromContext is ingestTokenFromContext's counterpart --
// only ever non-nil inside handleDroplistPull, the sole handler
// droplistPullRoutes registers and therefore the only one requireAuth's
// droplist-pull-token branch ever dispatches to.
func droplistTokenFromContext(r *http.Request) *auth.Token {
	t, _ := r.Context().Value(droplistTokenContextKey).(*auth.Token)
	return t
}

// writeCookie is the only place in the package a cookie is handed to the
// client, so the three security attributes are decided once instead of
// being repeated at four call sites that would then have to be kept in
// step by hand.
//
// SameSite is Lax rather than Strict because of the OIDC flow cookie:
// the provider's redirect back to /callback is a top-level cross-site
// GET navigation, and Strict would drop the cookie on it, breaking
// login. The session cookie is happy either way.
func (s *Server) writeCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	// Secure is read from auth.secureCookie, which defaults to true and
	// which validate.go reports as CFG-0021 when it is off while TLS is
	// on. The rule looks for a literal true and cannot follow a config
	// field, so it cannot see that the deployment already decides this.
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   s.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sessionID string) {
	s.writeCookie(w, sessionCookieName, sessionID, "/", int(cookieMaxAge.Seconds()))
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	s.writeCookie(w, sessionCookieName, "", "/", -1)
}

// pendingLoginCookieName carries a login that has proven the password
// but not yet the second factor (#1249) -- a "nearly in" ticket, not a
// session. The only thing it is worth to whoever holds it is one attempt
// per LoginLimiter window at the named account's code or a recovery
// code; see handleAuthLoginFactor.
const pendingLoginCookieName = "mikroview_pending_login"

// pendingLoginCookiePath scopes the cookie to the two routes that ever
// need it: the login that sets it and the factor step that reads it.
// "/api/auth/login" is a prefix of "/api/auth/login/factor" too (RFC
// 6265's path-match rule), so one Path value covers both without
// widening it to the whole API the way the session cookie's "/" does.
const pendingLoginCookiePath = "/api/auth/login"

// pendingLoginCookieMaxAge bounds both the cookie's own Max-Age and the
// tolerance pendingLoginCodec.decode checks IssuedAt against -- kept as
// one constant, same reasoning as oidcFlowCookieMaxAge, so the two can
// never drift apart. Five minutes is #1249's own number: long enough to
// open an authenticator app and read off six digits, short enough that
// an abandoned login does not leave a live "one attempt away" ticket
// sitting in a browser.
const pendingLoginCookieMaxAge = 5 * time.Minute

// pendingLoginState is everything the pending cookie carries: which
// account proved its password, and when. Deliberately nothing else -- no
// role, no session ID, nothing a forged or replayed cookie could spend
// for more than "try one code against this one account's already-proven
// password".
type pendingLoginState struct {
	UserID   string
	IssuedAt time.Time
}

// errPendingLoginInvalid covers every way a pending-login cookie can
// fail to decode: tampered, corrupt, or expired -- one error rather than
// several, the same stance oidc.ErrFlowStateInvalid takes and for the
// same reason: which specific failure occurred isn't something a caller
// (or an attacker probing the endpoint) needs to be able to tell apart.
var errPendingLoginInvalid = errors.New("api: pending login expired or was tampered with")

// pendingLoginStateCodec seals/opens a pendingLoginState the same way
// oidc.StateCodec seals an oidc.FlowState, per #1249's own instruction
// ("sealed the same way the OIDC flow cookie already is"): AES-256-GCM,
// stdlib only, authenticated so a tampered cookie fails the tag check
// rather than decoding into a different account, with a key generated
// once via crypto/rand and held only in memory.
//
// A second implementation rather than reusing oidc.StateCodec directly.
// That type is hard-coded to oidc.FlowState's fields, and this package
// has no other reason to depend on internal/oidc's cookie-sealing
// internals -- widening a codec that belongs to one login flow to also
// carry a second, unrelated flow's payload would leave neither flow's
// cookie shape visible from its own file. The two codecs share a
// construction (mustNewPendingLoginCodec mirrors oidc.NewStateCodec
// almost line for line) rather than a type.
type pendingLoginStateCodec struct {
	aead cipher.AEAD
}

// pendingLoginCodec is built once, at package load, and shared by every
// Server this process runs -- the same "generated once, held only in
// memory, lost on restart" lifetime SessionStore and oidc.StateCodec
// already have. A restart simply fails any login stuck mid-second-step
// cleanly (the browser gets a cookie the new process can't open, and
// tries the password step again), which is an entirely acceptable cost
// for state that was never meant to outlive five minutes anyway.
var pendingLoginCodec = mustNewPendingLoginCodec()

func mustNewPendingLoginCodec() *pendingLoginStateCodec {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Same stance internal/auth's newID/newRecoveryCode take: a
		// CSPRNG that cannot produce bytes is not a condition to degrade
		// from gracefully here -- every login on an account with a
		// second factor depends on this codec existing.
		panic("api: crypto/rand unavailable: " + err.Error())
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic("api: constructing pending-login cipher: " + err.Error())
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic("api: constructing pending-login AEAD: " + err.Error())
	}
	return &pendingLoginStateCodec{aead: aead}
}

func (c *pendingLoginStateCodec) encode(st pendingLoginState) (string, error) {
	plaintext, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("api: encoding pending login state: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("api: generating pending login seal nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// decode reverses encode, refusing (errPendingLoginInvalid) anything
// malformed, tampered, or older than pendingLoginCookieMaxAge as
// measured from the sealed IssuedAt against now.
func (c *pendingLoginStateCodec) decode(cookieValue string, now time.Time) (pendingLoginState, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(cookieValue)
	if err != nil {
		return pendingLoginState{}, errPendingLoginInvalid
	}
	ns := c.aead.NonceSize()
	if len(sealed) < ns {
		return pendingLoginState{}, errPendingLoginInvalid
	}
	nonce, ciphertext := sealed[:ns], sealed[ns:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return pendingLoginState{}, errPendingLoginInvalid
	}
	var st pendingLoginState
	if err := json.Unmarshal(plaintext, &st); err != nil {
		return pendingLoginState{}, errPendingLoginInvalid
	}
	if now.Sub(st.IssuedAt) > pendingLoginCookieMaxAge {
		return pendingLoginState{}, errPendingLoginInvalid
	}
	return st, nil
}

// setPendingLoginCookie seals a fresh pendingLoginState for userID and
// writes it -- called from handleAuthLogin the moment a password checks
// out against an account holding an active factor, in place of creating
// a session.
func (s *Server) setPendingLoginCookie(w http.ResponseWriter, userID string, now time.Time) error {
	encoded, err := pendingLoginCodec.encode(pendingLoginState{UserID: userID, IssuedAt: now})
	if err != nil {
		return err
	}
	s.writeCookie(w, pendingLoginCookieName, encoded, pendingLoginCookiePath, int(pendingLoginCookieMaxAge.Seconds()))
	return nil
}

func (s *Server) clearPendingLoginCookie(w http.ResponseWriter) {
	s.writeCookie(w, pendingLoginCookieName, "", pendingLoginCookiePath, -1)
}

type sessionResponse struct {
	SetupRequired bool   `json:"setupRequired"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	Role          string `json:"role,omitempty"`
	// HasLocalPassword is false once the account signs in only through
	// the identity provider -- either provisioned that way, or converted
	// by linking. The frontend uses it to decide whether "Connect SSO"
	// is offered at all: there is nothing left to convert otherwise.
	HasLocalPassword bool `json:"hasLocalPassword"`
	// SSOConnected is true once this account has an SSO identity
	// attached. Separate from HasLocalPassword since #1252: the admin
	// keeps its password through a link, so "has a password" no longer
	// answers "is there anything left to connect". The frontend uses
	// both to decide whether to offer "Connect SSO".
	SSOConnected bool `json:"ssoConnected"`
	// HasTOTP is true once this account holds a confirmed
	// authenticator-app factor (#1249). The Account menu needs it to
	// know which of two things to offer -- enrol, or turn it off -- and
	// there is no other way for it to find out: the enrolment routes
	// answer only the request that started them, so a page reload would
	// otherwise forget the factor exists.
	HasTOTP bool `json:"hasTOTP"`
	// Passkeys reports the caller's own passkey count and this
	// deployment's availability (#1250) -- the Account menu's second
	// "Passkeys · N" row needs both: the count for its badge, and
	// status/origin (set only when ready) to explain an unavailable
	// deployment instead of just hiding the row, which the design
	// forbids. nil (omitted) while unauthenticated, same as HasTOTP.
	Passkeys *sessionPasskeysInfo `json:"passkeys,omitempty"`
	// MustChangePassword is true while this session may reach nothing
	// but POST /api/auth/password -- an admin reset the account and it
	// signed in with the one-time code (#1251). The frontend draws the
	// "Set a new password" screen and nothing else while it holds.
	//
	// Read from the account, not from anything recorded on the session:
	// the flag is cleared by setting a password, which can happen in
	// another process (the CLI recovery tool), and a copy on the session
	// would keep a person locked out of an account that is no longer
	// flagged.
	MustChangePassword bool `json:"mustChangePassword"`
	// MustEnrolSecondFactor is true while this session may reach nothing
	// but the enrolment routes on secondFactorEnrolPaths (#1253) -- a
	// local account (HasLocalPassword) with no active second factor,
	// whatever left it in that state. The frontend draws the forced
	// enrolment screen and nothing else while it holds, the same way it
	// already does for MustChangePassword above -- see requireAuth's
	// matching gate in this file for the enforcement this only reports.
	//
	// Computed the same way MustChangePassword is, from the account
	// rather than the session, for the same reason: clearing it can
	// happen without this process's session ever changing (enrolling via
	// a different tab, or -- for the "no factor left" case -- the CLI's
	// `-clear-second-factor` never applies here, since that always
	// produces exactly the state this flags).
	MustEnrolSecondFactor bool `json:"mustEnrolSecondFactor"`
	// SSOAvailable tells the frontend whether to render the "Sign in
	// with SSO" link at all -- true whenever s.OIDC is configured,
	// regardless of the other fields above.
	SSOAvailable bool `json:"ssoAvailable"`
	// SignedInSince is this session's own auth.Session.IssuedAt (issue
	// #677's sessions row: "signed in 4 d"), RFC3339 -- when *this*
	// login happened, not when the account was created. Empty when
	// unauthenticated.
	SignedInSince string `json:"signedInSince,omitempty"`
}

// sessionPasskeysInfo is GET /api/auth/session's passkeys object -- see
// sessionResponse.Passkeys' doc comment.
type sessionPasskeysInfo struct {
	Count  int           `json:"count"`
	Status PasskeyStatus `json:"status"`
	// Origin is set only when Status is PasskeyStatusReady -- see
	// RelyingParty.Origin's own doc comment in webauthn.go.
	Origin string `json:"origin,omitempty"`
}

// passkeysSessionInfo builds userID's sessionPasskeysInfo. s.RelyingParty
// is nil on a Server built without one (an older test, or a deployment
// path that never wires one up), which reads the same as
// PasskeyStatusUnset -- the same nil-means-disabled convention every
// other optional Server field on this struct already follows.
func (s *Server) passkeysSessionInfo(userID string) *sessionPasskeysInfo {
	info := &sessionPasskeysInfo{Count: s.Auth.PasskeyCount(userID), Status: PasskeyStatusUnset}
	if s.RelyingParty != nil {
		info.Status = s.RelyingParty.Status
		if s.RelyingParty.Status == PasskeyStatusReady {
			info.Origin = s.RelyingParty.Origin
		}
	}
	return info
}

// handleAuthSession always returns 200 -- it reports state, it doesn't
// gate access (requireAuth exempts it for exactly this reason). The
// frontend calls this once on load to decide whether to render the
// first-run choice screen, a login form, or the live app.
func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	resp := sessionResponse{
		SetupRequired: s.Auth.Count() == 0,
		SSOAvailable:  s.OIDC != nil,
	}
	if user, ok := s.sessionUser(r, now); ok {
		resp.Authenticated = true
		resp.Username = user.Username
		resp.Role = string(user.Role)
		resp.HasLocalPassword = user.LocalPassword()
		resp.SSOConnected = user.OIDCSubject != ""
		resp.MustChangePassword = user.MustChangePassword
		resp.MustEnrolSecondFactor = user.LocalPassword() && !user.HasSecondFactor()
		resp.HasTOTP = user.HasActiveTOTP()
		resp.Passkeys = s.passkeysSessionInfo(user.ID)
		// sessionUser already validated the cookie once (that is how
		// user was resolved); re-reading it here just for IssuedAt
		// rather than widening sessionUser's own signature, which
		// requireAuth and every other caller would then have to carry
		// too for a field only this endpoint needs.
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if sess, ok := s.Sessions.Validate(cookie.Value, now); ok {
				resp.SignedInSince = sess.IssuedAt.Format(time.RFC3339)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// authErrorMessages maps the internal/auth sentinel errors a client can
// plausibly trigger to a message worth showing them -- internal/auth's
// own error text is written for a developer reading Go source (e.g.
// ErrNotPersisted's "refusing to create a user that would not survive
// a restart"), not for a stranger looking at a login screen. Anything
// not listed here (including a genuinely unexpected error, e.g. a
// crypto/rand failure inside HashPassword) falls back to a generic
// message via writeAuthError -- never echoed verbatim to the client,
// only logged server-side.
var authErrorMessages = map[error]string{
	auth.ErrRegistrationClosed: "registration is closed -- an account already exists",
	auth.ErrNotPersisted:       "this deployment has no persistent storage configured -- an administrator needs to set one up before an account can be created",
	auth.ErrUsernameTaken:      "that username is already taken",
	auth.ErrPasswordTooShort:   auth.ErrPasswordTooShort.Error(), // already phrased for an end user
	auth.ErrUsernameInvalid:    "that username contains characters that aren't allowed -- no control characters, and no leading or trailing spaces",
	auth.ErrUsernameLength:     auth.ErrUsernameLength.Error(), // already phrased for an end user
	// #1252: names created here are kept clear of email addresses, which
	// is what identity providers send as preferred_username. The message
	// says the rule and what to do instead, since "invalid" alone would
	// read as a bug to someone typing the name they use everywhere.
	auth.ErrUsernameIsEmail: "a MikroView username can't be an email address -- pick a plain name (SSO accounts are the ones named by their email)",
	auth.ErrInvalidRole:     `role must be "user" or "viewer"`,
	// #1249: enrolling replaces a pending secret, but a *confirmed*
	// factor has to be removed deliberately (the password-gated DELETE,
	// or an admin/CLI clear) before enrolling again -- see
	// ErrTOTPAlreadyActive's own doc comment for why silently replacing
	// one is a lockout waiting to happen.
	auth.ErrTOTPAlreadyActive: "this account already has an authenticator app -- remove it before enrolling another",
	auth.ErrNoPendingTOTP:     "no authenticator app enrolment is waiting to be confirmed -- start enrolling again",
	// #1250: the three passkey-store refusals a client can plausibly
	// trigger through the ordinary registration/rename/delete routes.
	auth.ErrPasskeyDuplicate:    "this passkey is already registered to this account",
	auth.ErrPasskeyLimitReached: auth.ErrPasskeyLimitReached.Error(), // already phrased for an end user
	auth.ErrPasskeyNotFound:     "no such passkey on this account",
}

// writeAuthError translates err into a safe, user-facing message via
// authErrorMessages (falling back to a generic one for anything not
// listed, logging the real error server-side so it's still
// diagnosable) and writes it with status.
func writeAuthError(w http.ResponseWriter, r *http.Request, err error, status int) {
	msg, ok := authErrorMessages[err]
	if !ok {
		authLog.Warn(authErrorLogLine(r.Method, r.URL.Path, err))
		msg = "unable to complete the request"
	}
	http.Error(w, msg, status)
}

// authErrorLogLine renders the server-side line for an auth error that
// has no user-facing message, naming the route so a bare error says
// where to look.
//
// Every field is quoted, and that is the point rather than styling.
// None of them is server-controlled: r.URL.Path is the *decoded*
// request target, so a request for `/api/x%0A...` arrives here carrying
// a real newline, and writing it raw let an unauthenticated caller
// append whatever it liked to mikroview's own log -- a forged INFO line
// is indistinguishable from a genuine one (#528). The error is quoted
// on the same reasoning: this branch is the one for errors we did not
// anticipate, which are the likeliest to carry something a caller sent.
//
// %q escapes the control characters, so the entry stays on one line and
// an injected newline shows up as the `\n` it really is.
func authErrorLogLine(method, path string, err error) string {
	return fmt.Sprintf("%q %q: %q", method, path, fmt.Sprint(err))
}

// credentialsRequest is the body of both login and first-run
// registration: the username and password, and nothing else.
//
// It briefly carried the doc comment of a deleted handleAuthSkip
// function, left behind when that handler was removed. Go attaches a
// preceding comment block to whatever declaration follows it, so `go
// doc` presented the struct that carries every password this
// application ever receives as "permanently disables authentication for
// this deployment", referring to an auth.Store.Disable that no longer
// exists. No runtime effect; the cost was to anyone auditing the auth
// surface, in the one file where being misled is most expensive.
type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAuthRegister creates the first (and only ever self-service)
// account, always as admin -- see auth.Store.Register.
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := s.Auth.Register(req.Username, req.Password, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrRegistrationClosed:
			status = http.StatusConflict
		case auth.ErrNotPersisted:
			status = http.StatusServiceUnavailable
		case auth.ErrPasswordTooShort, auth.ErrUsernameInvalid, auth.ErrUsernameLength, auth.ErrUsernameIsEmail:
			status = http.StatusBadRequest
		}
		writeAuthError(w, r, err, status)
		return
	}

	sess := s.Sessions.Create(user.ID, time.Now())
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role})
}

// passwordRecheckLimiterKey is the bucket for endpoints that re-verify
// a signed-in caller's own password -- changing it, and minting a
// device enrolment token (#1291). Those guesses have to be counted,
// but not in login's bucket: mikroview allows exactly one admin
// (ErrSingleAdmin), so spending login's allowance on a run of typos at
// one of these dialogs leaves nobody able to let them back in. If the
// session then goes -- a closed tab, cleared cookies, a second machine
// -- every sign-in is refused without the password being checked at
// all, until the window slides or someone reaches the console. One
// bucket for every re-check, so the rule stays in one place; the
// vault-unlock gate keys its own the same way (vaultUnlockLimiterKey).
func passwordRecheckLimiterKey(username string) string {
	return "password-recheck:" + strings.ToLower(username)
}

// handleAuthLogin is rate-limited independently by username and by
// source IP (see internal/auth.LoginLimiter) -- blocks either a single
// source hammering many usernames, or many sources hammering one
// username.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	ipKey := "ip:" + s.clientIP(r)
	userKey := "user:" + strings.ToLower(req.Username)
	// Reserve, not Allow: the attempt is claimed *before* the ~100ms
	// Argon2id verification rather than recorded after it. With Allow, a
	// simultaneous burst all passed the check before any failure landed,
	// so a threshold of 5 admitted as many attempts as the attacker cared
	// to send in parallel -- brute-force protection that a little
	// concurrency walked straight past, and 64 MiB of working memory per
	// admitted attempt.
	if !s.LoginLimiter.Reserve(ipKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if !s.LoginLimiter.Reserve(userKey, now) {
		s.LoginLimiter.Release(ipKey, now)
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}

	user, err := s.Auth.Authenticate(req.Username, req.Password, now)
	if err != nil {
		// Reservations stay claimed -- that is what counts the failure.
		writeUnauthorized(w, "invalid username or password")
		return
	}
	// Only a success releases, so ordinary repeated logins never
	// accumulate toward the threshold.
	s.LoginLimiter.Release(ipKey, now)
	s.LoginLimiter.Release(userKey, now)

	// #1249, and the plan doc that ordered this milestone calls it the
	// single most important test in it: a correct password on an
	// account holding an active second factor must NOT create a
	// session. What it gets instead is a short-lived pending-login
	// cookie naming the account, and the frontend is told which second
	// factor to ask for next. The real login only happens in
	// handleAuthLoginFactor below, once that code/assertion (or a
	// recovery code) checks out too.
	//
	// Widened from HasActiveTOTP to HasSecondFactor by #1250: an account
	// may now hold a passkey instead of, or alongside, an authenticator
	// app. The factor list is built with passkey first (per the design),
	// and only ever names a kind that is *currently usable* -- totp when
	// active, passkey only when at least one of the account's passkeys
	// isn't stale for this server's current RPID and the relying party is
	// actually ready. An account whose only factor is stale passkeys
	// still gets the pending cookie (so a recovery code can still
	// complete the login below) but an empty secondFactor list, per the
	// design's own "the password still never creates a session" rule --
	// #1249's shipped `{"secondFactor":["totp"]}` shape for a TOTP-only
	// account is unchanged, since passkeyOrigin is only ever added to the
	// response when a passkey is actually listed.
	if user.HasSecondFactor() {
		factors := []string{}
		passkeyOrigin := ""
		if s.passkeysReady() && len(s.nonStalePasskeys(user.Passkeys)) > 0 {
			factors = append(factors, "passkey")
			passkeyOrigin = s.RelyingParty.Origin
		}
		if user.HasActiveTOTP() {
			factors = append(factors, "totp")
		}
		if err := s.setPendingLoginCookie(w, user.ID, now); err != nil {
			authLog.Error(fmt.Sprintf("sealing pending-login cookie for %s: %v", user.Username, err))
			http.Error(w, "unable to complete sign-in", http.StatusInternalServerError)
			return
		}
		resp := map[string]any{"secondFactor": factors}
		if passkeyOrigin != "" {
			resp["passkeyOrigin"] = passkeyOrigin
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// A leftover pending-login cookie from an earlier, abandoned attempt
	// (this account or another one on the same browser) has no bearing
	// on a login that just completed through the ordinary one-step path
	// -- dropped here so it isn't left sitting around for
	// pendingLoginCookieMaxAge after the flow that made sense of it is
	// over.
	s.clearPendingLoginCookie(w)
	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "role": user.Role})
}

type loginFactorRequest struct {
	Code string `json:"code"`
	// Assertion is #1250's passkey branch: the raw JSON
	// PublicKeyCredential.toJSON() a navigator.credentials.get() call
	// produced. Mutually exclusive with Code in practice (the frontend
	// only ever sends one), and handleAuthLoginFactor branches on
	// whichever is present -- see its own doc comment.
	Assertion json.RawMessage `json:"assertion,omitempty"`
}

// handleAuthLoginFactor completes a login that handleAuthLogin stopped
// short of a session for (#1249): the pending-login cookie names the
// account whose password already checked out, and this checks one more
// credential against it -- a live TOTP code, a passkey assertion (#1250,
// against the SessionData POST /api/auth/login/factor/begin sealed), or,
// since either can be lost too, one of the account's ten recovery codes,
// burned the moment it works.
//
// Rate-limited on the exact same LoginLimiter buckets handleAuthLogin
// itself reserves against (ip: and user:, keyed the same way) -- a wrong
// code or a failed assertion here is exactly as good a brute-force move
// as a wrong password there, so all of them have to share one budget,
// not each get their own.
func (s *Server) handleAuthLoginFactor(w http.ResponseWriter, r *http.Request) {
	var req loginFactorRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cookie, err := r.Cookie(pendingLoginCookieName)
	if err != nil {
		writeUnauthorized(w, "sign in again")
		return
	}
	now := time.Now()
	st, err := pendingLoginCodec.decode(cookie.Value, now)
	if err != nil {
		s.clearPendingLoginCookie(w)
		writeUnauthorized(w, "sign in again")
		return
	}

	// Widened from HasActiveTOTP to HasSecondFactor by #1250 -- see
	// handleAuthLogin's own widening for the full reasoning. The account
	// was deleted, or every second factor it held (authenticator app and
	// passkeys alike) was cleared -- by an admin, the CLI recovery tool,
	// or the account owner themselves -- in the window between the
	// password step and this one. Either way there is nothing left this
	// cookie can complete.
	user, ok := s.Auth.Get(st.UserID)
	if !ok || !user.HasSecondFactor() {
		s.clearPendingLoginCookie(w)
		writeUnauthorized(w, "sign in again")
		return
	}

	ipKey := "ip:" + s.clientIP(r)
	userKey := "user:" + strings.ToLower(user.Username)
	if !s.LoginLimiter.Reserve(ipKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if !s.LoginLimiter.Reserve(userKey, now) {
		s.LoginLimiter.Release(ipKey, now)
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}

	// #1250: the body shape decides the branch -- an assertion is
	// checked by verifyPasskeyAssertion (passkey.go), which writes its
	// own refusal and leaves the reservations above claimed on failure,
	// exactly like a wrong TOTP code does below.
	if len(req.Assertion) > 0 {
		if s.verifyPasskeyAssertion(w, r, user, req.Assertion, now) {
			s.completeLoginFactor(w, user, ipKey, userKey, now)
		}
		return
	}

	// Verified and recorded in one call, under the store's lock, so two
	// concurrent submissions of the same code can't both check against
	// the same not-yet-advanced counter -- see VerifyAndRecordTOTP's doc
	// comment (store.go).
	if matched, err := s.Auth.VerifyAndRecordTOTP(user.ID, req.Code, now); matched {
		if err != nil {
			// The replay guard failing to advance doesn't undo the fact
			// that a correct, unreplayed code was just presented -- it
			// only means this exact code could be presented again inside
			// its own window if persistence keeps failing, the same
			// degraded-but-not-locked-out stance VerifyAndRecordTOTP's own
			// doc comment describes. Worth knowing about, not worth
			// refusing a legitimate sign-in over.
			authLog.Warn(fmt.Sprintf("advancing TOTP replay counter for %s: %v", user.Username, err))
		}
		s.completeLoginFactor(w, user, ipKey, userKey, now)
		return
	}

	if burned, err := s.Auth.BurnRecoveryCode(user.ID, req.Code, now); err != nil {
		authLog.Error(fmt.Sprintf("recording spent recovery code for %s: %v", user.Username, err))
	} else if burned {
		s.completeLoginFactor(w, user, ipKey, userKey, now)
		return
	}

	// Reservations stay claimed -- that is what counts the failure, same
	// as handleAuthLogin's own wrong-password path. Deliberately one
	// message regardless of whether the code looked like a TOTP guess or
	// a recovery-code guess: which kind was tried is not information a
	// caller needs back.
	writeUnauthorized(w, "invalid code")
}

// completeLoginFactor is handleAuthLoginFactor's success path, shared by
// the TOTP and recovery-code branches: release the reservations a wrong
// guess would have kept, drop the now-spent pending cookie, and issue
// the real session handleAuthLogin withheld.
func (s *Server) completeLoginFactor(w http.ResponseWriter, user *auth.User, ipKey, userKey string, now time.Time) {
	s.LoginLimiter.Release(ipKey, now)
	s.LoginLimiter.Release(userKey, now)
	s.clearPendingLoginCookie(w)
	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "role": user.Role})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleAuthChangePassword lets a signed-in user change their own
// password (#294 item 4).
//
// There was no route for this at all: changing a password meant
// -recover-admin-account, which needs host access. So an operator who
// suspected their credential was known had no way to act on it from the
// interface, and no way to end other sessions either -- the two halves
// of the same problem, since the session ceiling (#294 item 3) bounds
// how long a stolen session lives but does nothing about one right now.
//
// Changing the password is therefore also "sign out everywhere":
// SetPassword records PasswordChangedAt, which invalidates every session
// issued before it -- including this browser's, so a fresh one is issued
// here. Same pattern completeOIDCLink already uses for the same reason.
func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	user, ok := s.sessionUser(r, now)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	var req changePasswordRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// An SSO-only account has no local password to change, and inventing
	// one here would quietly create a second way into an account whose
	// owner believes it is federated.
	if !user.LocalPassword() {
		http.Error(w, "this account signs in through your identity provider and has no local password to change", http.StatusConflict)
		return
	}

	// After an admin reset (#1251) there is no current password to
	// supply: the account's stored hash is the unmatchable one
	// IssueResetCode left behind, and the credential this session was
	// established with was a one-time code that is already spent. So the
	// whole verify-the-old-one block below is skipped -- asking for
	// something that provably cannot be supplied would make the forced
	// change impossible to complete.
	//
	// Nothing is weakened by skipping it. Reaching this point at all
	// required signing in with a live code, which requireAuth's gate
	// above then confines to this single route; there is no credential
	// being guessed here for the limiter to count, and no username in
	// the body pointing anywhere but the caller's own account.
	if !user.MustChangePassword {
		// Rate-limited on the same limiter as login, in its own bucket:
		// the current password is a credential and this is a guess at
		// it, so an endpoint that verifies one without counting the
		// attempt is a brute-force oracle that happens to need a
		// session -- and a session is exactly what an attacker who has
		// stolen a cookie already has. See passwordRecheckLimiterKey
		// for why the bucket is not login's own.
		userKey := passwordRecheckLimiterKey(user.Username)
		if !s.LoginLimiter.Reserve(userKey, now) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}
		if _, err := s.Auth.Authenticate(user.Username, req.CurrentPassword, now); err != nil {
			// Reservation stays claimed: that is what counts the failure.
			writeUnauthorized(w, "current password is incorrect")
			return
		}
		s.LoginLimiter.Release(userKey, now)

		if req.NewPassword == req.CurrentPassword {
			http.Error(w, "the new password is the same as the current one", http.StatusBadRequest)
			return
		}
	}
	if err := s.Auth.SetPassword(user.Username, req.NewPassword, now); err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "could not change the password", http.StatusInternalServerError)
		return
	}

	// Every session issued before now is dead by PasswordChangedAt, but
	// that is only enforced on the next request each one makes. Dropping
	// them here means they are gone immediately, which is what an
	// operator acting on a suspected theft is actually asking for.
	s.Sessions.RevokeAllForUser(user.ID)
	// And the router-backup vault's key with them (#1120). Locking only
	// when the *calling* session held it open missed the case the button
	// is pressed for: the admin changing their password because they
	// think someone else has their session is the one whose other
	// session is holding the key. It is this account's unlock that goes,
	// though, not anyone else's -- this route is open to user-role
	// accounts, and one of those must not be able to end an admin's
	// unlock by changing its own password (#1124).
	s.lockVaultForUser(user.ID)

	detail := "sessions ended: all"
	if user.MustChangePassword {
		// Records that the forced change completed, so the audit trail
		// pairs the admin's reset entry with the moment the account came
		// back under its owner's own credential. The code is not in this
		// line, or in any other: it exists in clear exactly once, in the
		// response to the reset itself.
		detail += ", after an administrator's reset"
	}
	s.Audit.Record(user.Username, "account.password_changed", user.Username, detail)

	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"changed": true, "otherSessionsEnded": true})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.Sessions.Revoke(cookie.Value)
		// Signing out drops the router-backup vault's key if this
		// session is what was holding it open (#956): the unlock is
		// scoped to the session, so it cannot outlive it.
		s.lockVaultForSession(cookie.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}

// handleAuthLogoutAll is the settings page's "sign out everywhere"
// (issue #677): every session the caller holds, on every device, ends
// at once. It then issues the caller a fresh session and cookie so the
// tab the button was clicked from is not itself logged out by the call
// it just made -- the identical revoke-then-recreate shape
// handleAuthChangePassword already uses below, which is where
// SessionStore.RevokeAllForUser was introduced.
//
// User-tier (#653's viewer floor, same reasoning as POST
// /api/auth/password -- see authzMatrix's row for it): this acts only
// on the caller's own sessions, resolved from the session cookie, never
// from a body field naming someone else, so there is nothing here an
// admin-only gate would be protecting. Ending another account's
// sessions stays admin-only, via handleAuthDeleteUser.
func (s *Server) handleAuthLogoutAll(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	user, ok := s.sessionUser(r, now)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	s.Sessions.RevokeAllForUser(user.ID)
	// "Everywhere" includes the router-backup vault (#1120). This is the
	// panic button an operator presses on a suspected theft, and leaving
	// the vault's private key in memory afterwards -- until somebody
	// happened to request a download -- was the opposite of what it
	// says. Everywhere means this account's own sessions, here as in the
	// revocation above: another account's unlock is not this button's to
	// end (#1124).
	s.lockVaultForUser(user.ID)
	s.Audit.Record(user.Username, "account.sessions_ended", user.Username, "sessions ended: all, via sign out everywhere")

	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"signedOutEverywhere": true})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// callerAtLeast reports whether r's authenticated caller holds a role at
// or above min (auth.Role.AtLeast's stacked tiers: admin ⊇ user ⊇
// viewer). callerIsAdmin below is this specialized to RoleAdmin -- every
// gate in this package, admin or user tier, is defined in terms of this
// one function, so there is a single implementation of "who is allowed
// to reach this" rather than an admin check and a separately-maintained
// user check that could drift apart.
//
// While no account exists, caller is nil, so this returns false without
// needing to know why Count() is 0 -- see callerIsAdmin's doc comment
// for why that property matters.
func callerAtLeast(r *http.Request, min auth.Role) bool {
	caller := userFromContext(r)
	return caller != nil && caller.Role.AtLeast(min)
}

// callerIsAdmin reports whether r's authenticated caller is an admin --
// the strict check every admin-only mutation of *account-equivalent*
// state (user management, and internal/entities' admin-gated CRUD) uses.
// The single admin check for every admin-gated endpoint.
//
// There is deliberately no "no accounts yet" bypass. One existed, on the
// detector-settings and flags-exclusion endpoints, from when mikroview
// could run with authentication switched off -- it treated an anonymous
// caller as an admin while Count() was 0. That mode is gone, and
// requireAuth now refuses those paths outright before they route, so the
// bypass was unreachable. Unreachable is not the same as harmless: it
// read as "anonymous callers are admins under some condition", and would
// have become live again the moment requireAuth was loosened.
//
// While no account exists, caller is nil, so this returns false without
// needing to know why Count() is 0.
func callerIsAdmin(r *http.Request) bool {
	return callerAtLeast(r, auth.RoleAdmin)
}

// callerIsUser reports whether r's authenticated caller holds at least
// the user role -- the gate for #653's operational tier: the writes that
// affect what mikroview is watching or clearing (flag judgement/clearing,
// and every /api/definitions, /api/entities, /api/naming/provenance and
// /api/suggestions route), open to "user" and "admin" alike but not
// "viewer". See callerAtLeast for the shared nil-caller/no-bypass
// reasoning.
func callerIsUser(r *http.Request) bool {
	return callerAtLeast(r, auth.RoleUser)
}

// handleAuthCreateUser lets an existing admin add another account -- the
// only way to create a user once self-registration has closed (see
// auth.Store.Register's one-time-only behavior).
func (s *Server) handleAuthCreateUser(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req createUserRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// A request for an admin is refused outright rather than quietly
	// downgraded to a lesser role: the caller asked for something this
	// deployment does not have, and silently creating a lesser account
	// under the name they chose is worse than telling them. auth.Store
	// refuses it too -- this exists to give a usable status and message
	// instead of a 500.
	if req.Role == string(auth.RoleAdmin) {
		writeAuthError(w, r, auth.ErrSingleAdmin, http.StatusBadRequest)
		return
	}
	// Empty defaults to RoleUser (#653) -- the pre-existing behavior for
	// every caller of this endpoint before viewer existed, preserved so
	// an unmodified admin UI/script keeps creating the same accounts it
	// always did. Anything other than "", "user" or "viewer" is a request
	// for a role this deployment does not recognize, refused the same way
	// "admin" is above rather than silently coerced.
	role := auth.RoleUser
	switch req.Role {
	case "", string(auth.RoleUser):
		role = auth.RoleUser
	case string(auth.RoleViewer):
		role = auth.RoleViewer
	default:
		writeAuthError(w, r, auth.ErrInvalidRole, http.StatusBadRequest)
		return
	}

	user, err := s.Auth.CreateUser(req.Username, req.Password, role, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrUsernameTaken:
			status = http.StatusConflict
		case auth.ErrPasswordTooShort, auth.ErrSingleAdmin, auth.ErrInvalidRole, auth.ErrUsernameInvalid, auth.ErrUsernameLength, auth.ErrUsernameIsEmail:
			status = http.StatusBadRequest
		}
		writeAuthError(w, r, err, status)
		return
	}
	s.Audit.Record(auditActor(r), "user.create", user.Username, "role="+string(user.Role))
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role})
}

// userSummary is what the account list exposes. Deliberately not
// auth.User: that carries PasswordHash, and serializing it directly is
// exactly the mistake this type exists to make impossible.
type userSummary struct {
	ID               string    `json:"id"`
	Username         string    `json:"username"`
	Role             string    `json:"role"`
	CreatedAt        time.Time `json:"createdAt"`
	LastLogin        time.Time `json:"lastLogin,omitzero"`
	HasLocalPassword bool      `json:"hasLocalPassword"`
	SSO              bool      `json:"sso"`
	// HasTOTP is true once this account holds a confirmed
	// authenticator-app factor (#1249), which is what the admin list
	// shows as a pill and what decides whether the clear button is
	// worth offering on that row.
	HasTOTP bool `json:"hasTOTP"`
	// PasskeyCount is this account's registered passkey count (#1250),
	// the admin list's passkey pill and clear-button counterpart to
	// HasTOTP above. Read from the store's PasskeyCount, not from this
	// row's own copy -- see handleAuthListUsers' doc comment for why.
	PasskeyCount int `json:"passkeyCount"`
}

// handleAuthListUsers backs the admin-facing account list.
//
// Admin-only, and 403 for everyone else rather than an empty list. The
// usernames and which one is the admin are themselves worth having --
// they tell an attacker whose account is the high-value target -- so
// this is not information a signed-in non-admin should be handed. An
// empty-list response would also leave the route-authorization matrix
// unable to tell a correct refusal from a handler that leaks.
func (s *Server) handleAuthListUsers(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	users := s.Auth.List()
	out := make([]userSummary, 0, len(users))
	for _, u := range users {
		out = append(out, userSummary{
			ID:               u.ID,
			Username:         u.Username,
			Role:             string(u.Role),
			CreatedAt:        u.CreatedAt,
			LastLogin:        u.LastLogin,
			HasLocalPassword: u.LocalPassword(),
			SSO:              u.OIDCIssuer != "",
			// Asked of the store rather than of u, deliberately. List
			// blanks TOTPSecret on the copies it hands back (it is the
			// live shared secret, the one field here worth more than a
			// hash), and User.HasActiveTOTP tests that very field --
			// so calling it on one of these copies answers false for
			// every account, including the ones that do hold a factor.
			HasTOTP: s.Auth.HasActiveTOTP(u.ID),
			// Same trap, same fix, for passkeys (#1250): List blanks
			// Passkeys wholesale, so len(u.Passkeys) here would always
			// read zero -- see auth.Store.PasskeyCount's own doc comment,
			// which names this exact mistake shipping once already for
			// HasActiveTOTP.
			PasskeyCount: s.Auth.PasskeyCount(u.ID),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAuthDeleteUser removes an account and everything it can still be
// used with.
//
// Deleting the account alone is not enough. A live session cookie
// authenticates by session ID, and an API token by its own bearer value
// -- neither re-checks that the account still exists on every request,
// so both would outlive the deletion. Sessions and tokens go with it.
//
// The deletion happens first, and the revocations only once it has
// committed. The reverse order would sign a user out and destroy their
// tokens on the way to a deletion that can still be refused (the admin
// account, a stale ID) -- destructive work done for a request that
// never took effect. Neither revocation can fail, so nothing is left
// half-done by finishing in this order.
func (s *Server) handleAuthDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "user id is required", http.StatusBadRequest)
		return
	}

	user, err := s.Auth.DeleteUser(id)
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrUserNotFound:
			status = http.StatusNotFound
		case auth.ErrCannotDeleteAdmin:
			status = http.StatusConflict
		}
		writeAuthError(w, r, err, status)
		return
	}

	s.Sessions.RevokeAllForUser(user.ID)
	// Whoever is being removed may be the one holding the vault open, and
	// there is no session left to ask (#1120) -- the unlock records the
	// account it belongs to for exactly this. An unrelated deletion
	// leaves another admin's unlock where it is (#1124).
	s.lockVaultForUser(user.ID)
	// #1283's own ruling: preferences are cleared when the user is
	// deleted, not left behind for an id nothing will ever sign in as
	// again. Logged rather than failing the request for the same reason
	// the token revocation below is: the account deletion already
	// committed, so this request still succeeds, but a write that didn't
	// durably land is said out loud instead of reported as done (R6).
	if err := s.Prefs.Delete(user.ID); err != nil {
		authLog.Error(fmt.Sprintf("deleting preferences for deleted user %s: %v", user.ID, err))
	}
	revokedTokens := 0
	if s.Tokens != nil {
		var err error
		revokedTokens, err = s.Tokens.RevokeAllCreatedBy(user.ID)
		if err != nil {
			// The account deletion above already committed, so this
			// request still succeeds -- but a failed write here leaves
			// this user's tokens durably intact, so it's said out loud
			// server-side rather than reported as done (R6): the
			// response below reflects what actually persisted (zero),
			// not what was attempted.
			authLog.Error(fmt.Sprintf("revoking tokens for deleted user %s: %v", user.ID, err))
		}
	}

	s.Audit.Record(auditActor(r), "user.delete", user.Username,
		fmt.Sprintf("role=%s tokensRevoked=%d", user.Role, revokedTokens))
	writeJSON(w, http.StatusOK, map[string]any{
		"username":      user.Username,
		"tokensRevoked": revokedTokens,
	})
}

// resetPasswordResponse is the only place an issued reset code exists in
// clear (#1251). Nothing persists it, nothing logs it, and no later
// request can retrieve it: an admin who loses it issues another, which
// kills this one.
type resetPasswordResponse struct {
	Username string `json:"username"`
	// Code is grouped xxxx-xxxx-xxxx-xxxx for reading aloud. The server
	// accepts it back in any case, with or without the dashes.
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleAuthResetUserPassword is the admin's way back in for somebody
// who has lost their password (#1251, from #1245 decision 3). mikroview
// sends no mail, so there is no reset link: the admin resets the
// account, reads the returned code out to its owner in person or over a
// call they trust, and the owner types it into the password box once and
// chooses a new password on the spot.
//
// Two accounts are refused, both with 409:
//
//   - the caller's own. An admin locked out of their own account cannot
//     bootstrap themselves back in with a code they mint for themselves
//     -- that is POST /api/auth/password if they still know the current
//     one, and the recovery-key-gated `mikroview -recover-admin-account`
//     from the console if they do not. Since mikroview holds exactly one
//     admin, this is also what keeps the admin account out of this route
//     entirely.
//   - an SSO-only account. Its identity provider owns the credential;
//     see auth.ErrNoLocalPassword.
//
// The account's live sessions go with the reset, twice over: the store
// bumps PasswordChangedAt (which ends them across processes and
// restarts) and this drops the ones in memory immediately, the same
// pattern handleAuthDeleteUser and handleAuthChangePassword use. The
// router-backup vault's key goes too if that account is what was holding
// it open (#1120).
func (s *Server) handleAuthResetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "user id is required", http.StatusBadRequest)
		return
	}
	if caller := userFromContext(r); caller != nil && caller.ID == id {
		http.Error(w, "an administrator cannot reset their own password here -- change it from the account menu, "+
			"or use `mikroview -recover-admin-account` at the console", http.StatusConflict)
		return
	}

	now := time.Now()
	user, code, err := s.Auth.IssueResetCode(id, now)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, auth.ErrUserNotFound):
			status = http.StatusNotFound
		case errors.Is(err, auth.ErrNoLocalPassword):
			status = http.StatusConflict
		case errors.Is(err, auth.ErrNotPersisted):
			status = http.StatusServiceUnavailable
		}
		writeAuthError(w, r, err, status)
		return
	}

	s.Sessions.RevokeAllForUser(user.ID)
	s.lockVaultForUser(user.ID)

	// Who reset whom, and never the code -- not here, not in any log
	// line. The detail records the deadline instead, which is what an
	// operator reading this entry later actually needs.
	s.Audit.Record(auditActor(r), "user.password_reset", user.Username,
		fmt.Sprintf("one-time code issued, expires %s; sessions ended: all",
			user.ResetCodeExpiresAt.Format(time.RFC3339)))

	writeJSON(w, http.StatusOK, resetPasswordResponse{
		Username:  user.Username,
		Code:      code,
		ExpiresAt: user.ResetCodeExpiresAt,
	})
}

// -- Authenticator-app second factor (#1249) ---------------------------
//
// Four routes: enrol and confirm start and finish setting one up,
// DELETE is the account owner turning it off with their password, and
// the admin route at the very end is the Users-group path for a lost
// phone when the password still works. The fifth route this feature
// adds, POST /api/auth/login/factor, lives with handleAuthLogin above
// since it is a login-flow endpoint first and a TOTP endpoint second.
//
// Every handler here re-reads the account from s.Auth rather than
// trusting userFromContext's copy for anything beyond "who is calling" --
// TOTPSecret, TOTPConfirmedAt and TOTPLastCounter are written by a
// *different* request than the one that reads them (enrol writes what
// confirm reads; confirm writes what the next login reads), so a copy
// resolved at the top of this request's session check is never new
// enough to check a code against.

type totpEnrolResponse struct {
	// URI is the otpauth:// URI the frontend renders as a QR code.
	URI string `json:"uri"`
	// Secret is the same value URI carries, base32, for typing in by
	// hand -- #1249 requires the text secret to always be shown beside
	// the code, since not every authenticator app's camera flow is
	// reliable.
	Secret string `json:"secret"`
}

// handleTOTPEnrol starts authenticator-app enrolment for the signed-in
// caller's own account: a fresh secret is generated and stored pending,
// not active until handleTOTPConfirm proves a code was produced from it
// (see auth.Store.SetPendingTOTPSecret). Enrolling again before
// confirming simply replaces the pending secret -- that store method's
// own behaviour -- so this handler doesn't need to notice that case
// specially.
func (s *Server) handleTOTPEnrol(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	// SSO accounts are never offered a local factor (#1249's own rule):
	// their identity provider owns identity. Checked on LocalPassword(),
	// not on whether an SSO identity is linked -- the admin keeps its
	// password even once linked (#1252), and is still offered one; it's
	// an account with *no* local password (provisioned or converted to
	// SSO-only) that has nothing here to gate a factor behind.
	if !user.LocalPassword() {
		http.Error(w, "this account signs in through your identity provider -- an authenticator app is not offered", http.StatusConflict)
		return
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		authLog.Error(fmt.Sprintf("generating TOTP secret for %s: %v", user.Username, err))
		http.Error(w, "unable to start enrolment", http.StatusInternalServerError)
		return
	}
	encoded := auth.EncodeTOTPSecret(secret)
	if err := s.Auth.SetPendingTOTPSecret(user.ID, encoded); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrTOTPAlreadyActive) {
			status = http.StatusConflict
		}
		writeAuthError(w, r, err, status)
		return
	}

	writeJSON(w, http.StatusOK, totpEnrolResponse{
		URI:    auth.TOTPEnrollmentURI(user.Username, secret),
		Secret: encoded,
	})
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

type totpConfirmResponse struct {
	Enabled bool `json:"enabled"`
	// RecoveryCodes is the only place the ten codes ever exist in clear
	// outside a person's own saved copy -- see
	// auth.Store.GenerateRecoveryCodes. Nothing on this account can show
	// them again; losing this response before saving it means removing
	// the factor and enrolling again.
	//
	// nil (JSON null) when AlreadyIssued is true -- #1250's "mint if
	// absent, never re-mint" rule (see the design's "Recovery codes are
	// shared" section): a passkey-first account confirming TOTP as a
	// second factor must not have its existing codes silently replaced,
	// which would invalidate any copy already written down.
	RecoveryCodes []string `json:"recoveryCodes"`
	// AlreadyIssued is true when this account already held recovery
	// codes (minted by an earlier passkey registration) before this
	// confirm call -- AuthenticatorOverlay reads it to skip its `codes`
	// step with a one-line note that the existing codes still stand,
	// rather than implying new ones were just issued.
	AlreadyIssued bool `json:"alreadyIssued,omitempty"`
}

// handleTOTPConfirm activates the pending secret handleTOTPEnrol stored,
// checking one code against it, and is the only place the ten recovery
// codes are minted and returned.
//
// Confirming ends every other session on this account (#1249): turning
// on a second factor is exactly the moment a stale or forgotten session
// elsewhere should not get to ride along unchallenged without ever
// having to prove it. Same shape as handleAuthChangePassword just above
// -- RevokeAllForUser, then reissue this browser its own fresh session --
// since SessionStore.RevokeAllForUser has no notion of "except the
// caller".
func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}

	var req totpConfirmRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	// Re-read rather than trust userFromContext's copy -- see this
	// section's header comment. The pending secret being checked here
	// only exists because of a *previous* request (handleTOTPEnrol);
	// this one has to see that write.
	current, ok := s.Auth.Get(user.ID)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	matched, ok := auth.VerifyTOTP(current.TOTPSecret, req.Code, now, current.TOTPLastCounter)
	if !ok {
		http.Error(w, "that code didn't match -- check your authenticator app's clock and try again", http.StatusBadRequest)
		return
	}

	if err := s.Auth.ConfirmTOTP(user.ID, now, matched); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrNoPendingTOTP) {
			status = http.StatusConflict
		}
		writeAuthError(w, r, err, status)
		return
	}

	// Mint-if-absent, atomically under the store's lock (#1250's
	// "Recovery codes are shared" rule): a snapshot taken before this
	// call and a separate unconditional GenerateRecoveryCodes left a
	// window where two concurrent first enrolments (a passkey
	// registration racing this confirm, or two tabs confirming TOTP at
	// once) both saw no codes yet and both minted, the second silently
	// replacing what the first had already shown. See
	// GenerateRecoveryCodesIfAbsent's doc comment (recoverycodes.go).
	codes, alreadyIssued, err := s.Auth.GenerateRecoveryCodesIfAbsent(user.ID, now)
	if err != nil {
		// The factor is active at this point regardless -- ConfirmTOTP
		// already committed. Logged rather than swallowed (R6), and
		// told to the caller plainly rather than reported as a clean
		// success: they are about to be shown nothing to fall back on
		// if the app is ever lost. Recovering from here is
		// DELETE /api/auth/totp followed by enrolling again, same as
		// any other abandoned enrolment.
		authLog.Error(fmt.Sprintf("generating recovery codes for %s after confirming TOTP: %v", user.Username, err))
		http.Error(w, "the authenticator app is now active, but recovery codes could not be generated -- remove it and enrol again from account settings", http.StatusInternalServerError)
		return
	}
	detail := "authenticator app confirmed"
	if alreadyIssued {
		detail += "; existing recovery codes unchanged"
	} else {
		detail += "; recovery codes issued"
	}

	s.Sessions.RevokeAllForUser(user.ID)
	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)

	s.Audit.Record(user.Username, "account.totp_enabled", user.Username, detail)

	writeJSON(w, http.StatusOK, totpConfirmResponse{Enabled: true, RecoveryCodes: codes, AlreadyIssued: alreadyIssued})
}

type totpDeleteRequest struct {
	Password string `json:"password"`
}

// handleTOTPDelete turns off the signed-in caller's own authenticator-
// app factor, gated by their password -- the one self-service way to
// remove it; the admin route at the end of this file and the CLI
// recovery tool (`-clear-second-factor`) are the only other paths, for
// when the password is what's lost instead.
func (s *Server) handleTOTPDelete(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}

	var req totpDeleteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	// Rate-limited on passwordRecheckLimiterKey, same bucket and same
	// reasoning as handleAuthChangePassword's current-password check
	// just above: a guess at a live credential, made by a caller who --
	// unlike an ordinary login attempt -- already holds a session, which
	// is exactly the position a stolen-cookie attacker is in. Without
	// this, "turn off 2FA" would be an unthrottled password oracle
	// sitting behind nothing but a cookie.
	userKey := passwordRecheckLimiterKey(user.Username)
	if !s.LoginLimiter.Reserve(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if _, err := s.Auth.Authenticate(user.Username, req.Password, now); err != nil {
		writeUnauthorized(w, "incorrect password")
		return
	}
	s.LoginLimiter.Release(userKey, now)

	if err := s.Auth.ClearTOTP(user.ID); err != nil {
		writeAuthError(w, r, err, http.StatusInternalServerError)
		return
	}

	// Owner ruling: a user may remove their only second factor. When
	// doing so leaves the account with none, every session on it --
	// this one included -- is revoked at once, so the very next request
	// (this one's own response is still sent normally) goes through
	// requireAuth's forced-enrolment door instead of riding an existing
	// cookie past a requirement the account no longer satisfies.
	// signedOut tells the caller that happened -- the frontend's warning
	// and sign-out screen reads it.
	signedOut := false
	if updated, ok := s.Auth.Get(user.ID); ok && !updated.HasSecondFactor() {
		s.Sessions.RevokeAllForUser(user.ID)
		signedOut = true
	}

	s.Audit.Record(user.Username, "account.totp_disabled", user.Username, "removed by account owner")
	writeJSON(w, http.StatusOK, map[string]any{"disabled": true, "signedOut": signedOut})
}

type recoveryCodesRegenerateRequest struct {
	Password string `json:"password"`
}

type recoveryCodesRegenerateResponse struct {
	// RecoveryCodes is the fresh ten, in clear, exactly once -- the same
	// one-shot contract totpConfirmResponse.RecoveryCodes documents.
	// Nothing on this account can show them again once this response is
	// gone.
	RecoveryCodes []string `json:"recoveryCodes"`
}

// handleRecoveryCodesRegenerate mints a fresh set of ten recovery codes
// for the signed-in caller's own account, replacing whichever set stood
// before (#1331). Before this route the only way to a fresh set was
// removing a factor and adding it back -- tolerable for one authenticator
// app, but #1250 clears the shared set only when the *last* factor goes,
// so an account with several passkeys had to strip all of them to get
// here. Password-gated exactly like handleTOTPDelete above, and refused
// outright when the account has no second factor at all: recovery codes
// stand in for one, not for a password alone.
//
// Design lead ruling, 2026-09-26: unlike ConfirmTOTP and a first passkey
// registration, this does not end other sessions. Those moments end
// sessions because the set of factors protecting the account just
// changed and a session elsewhere might predate that change; regenerating
// codes changes nothing about which factors are active, so there is
// nothing for another session to have gotten away with. The codes are a
// spare key, not the lock.
func (s *Server) handleRecoveryCodesRegenerate(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}

	var req recoveryCodesRegenerateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	// Same passwordRecheckLimiterKey bucket and reasoning as
	// handleTOTPDelete just above: a caller who already holds a session
	// is exactly the position a stolen-cookie attacker is in, so this
	// cannot be left as an unthrottled password oracle behind a cookie.
	userKey := passwordRecheckLimiterKey(user.Username)
	if !s.LoginLimiter.Reserve(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	current, err := s.Auth.Authenticate(user.Username, req.Password, now)
	if err != nil {
		writeUnauthorized(w, "incorrect password")
		return
	}
	s.LoginLimiter.Release(userKey, now)

	// Re-checked against the freshly-authenticated copy, not the
	// context snapshot -- same reasoning handleTOTPConfirm's header
	// comment gives for re-reading rather than trusting userFromContext.
	if !current.HasSecondFactor() {
		http.Error(w, "this account has no second factor yet -- recovery codes stand in for one, not for a password alone", http.StatusConflict)
		return
	}

	codes, err := s.Auth.GenerateRecoveryCodes(user.ID, now)
	if err != nil {
		// GenerateRecoveryCodes' own restore-on-failure contract already
		// left the old set intact and reported nothing as issued -- this
		// is a clean refusal, not a half-done one. writeAuthError logs
		// the real error server-side (it isn't in authErrorMessages, so
		// the caller gets the generic message).
		writeAuthError(w, r, err, http.StatusInternalServerError)
		return
	}

	s.Audit.Record(user.Username, "account.recovery_codes_regenerated", user.Username, "")

	writeJSON(w, http.StatusOK, recoveryCodesRegenerateResponse{RecoveryCodes: codes})
}

// handleTOTPAdminClear lets an admin remove another user's authenticator-
// app factor from the Users group -- the path for a lost phone when the
// account owner still has their password (if they don't either, that's
// the reset-password route beside it, unrelated to this one: the two
// credentials are cleared independently since losing one says nothing
// about the other).
//
// Never on the caller's own account, same division
// handleAuthResetUserPassword draws for a lost password: an admin locked
// out of their own factor uses `mikroview -clear-second-factor` at the
// console instead. mikroview holds exactly one admin, so this is also
// what keeps the admin account out of this route entirely.
func (s *Server) handleTOTPAdminClear(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "user id is required", http.StatusBadRequest)
		return
	}
	if caller := userFromContext(r); caller != nil && caller.ID == id {
		http.Error(w, "an administrator cannot clear their own authenticator app here -- "+
			"use `mikroview -clear-second-factor` at the console", http.StatusConflict)
		return
	}

	target, ok := s.Auth.Get(id)
	if !ok {
		http.Error(w, "no such user", http.StatusNotFound)
		return
	}
	if err := s.Auth.ClearTOTP(id); err != nil {
		writeAuthError(w, r, err, http.StatusInternalServerError)
		return
	}

	s.Audit.Record(auditActor(r), "user.totp_cleared", target.Username, "authenticator app removed by admin")
	writeJSON(w, http.StatusOK, map[string]any{"username": target.Username, "cleared": true})
}
