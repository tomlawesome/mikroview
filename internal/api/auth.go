// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
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

// changePasswordPath is the one route a session flagged
// MustChangePassword may reach (#1251) -- named once here rather than
// written as a literal in requireAuth, so the gate and the route table
// cannot drift apart silently.
const changePasswordPath = "/api/auth/password"

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
			http.Error(w, "an administrator reset this account -- set a new password before going any further", http.StatusForbidden)
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
