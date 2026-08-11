// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/logging"
)

var authLog = logging.New("auth-api")

const sessionCookieName = "mikroview_session"

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
// with just these four GET routes registered, rather than a per-request
// allowlist check layered in front of the real mux. That's what makes
// "a token can never reach a write/clear/config endpoint" structural:
// there is no code path from a bearer-authenticated request to
// handleFlagsClear, handleDetectorSettingsUpdate, handleAuthCreateUser,
// etc. -- those handlers are simply never registered on this mux, so a
// request for any of them (regardless of method) falls through to
// ServeMux's own 404, the same as a route that never existed.
func (s *Server) readOnlyRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/flags", s.handleFlagsList)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/watchlist/matches", s.handleWatchlistMatchesQuery)
	return mux
}

// ingestRoutes is the only handler set an ingest bearer token (issue
// #186) can ever reach -- its own separate *http.ServeMux, the same
// structural reasoning readOnlyRoutes documents above: there is no code
// path from an ingest-authenticated request to anything else registered
// on the real mux, including readOnlyRoutes' own four GETs. That
// separation is exactly what stops an ingest token -- readable by any
// `read`-capable user on the router it came from, per #186 step 5 --
// from becoming a read-everything credential the way a stolen one
// reaching readOnlyRoutes would.
func (s *Server) ingestRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/ingest/routeros", s.handleIngestRouterOS)
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
//     valid *ingest* token (#186) is dispatched to ingestRoutes instead,
//     equally never to next -- the two kinds reach two disjoint muxes,
//     neither of which is the real one, so a token of either kind is
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
			http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
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
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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

func (s *Server) setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cookieMaxAge.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
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
	// SSOAvailable tells the frontend whether to render the "Sign in
	// with SSO" link at all -- true whenever s.OIDC is configured,
	// regardless of the other fields above.
	SSOAvailable bool `json:"ssoAvailable"`
}

// handleAuthSession always returns 200 -- it reports state, it doesn't
// gate access (requireAuth exempts it for exactly this reason). The
// frontend calls this once on load to decide whether to render the
// first-run choice screen, a login form, or the live app.
func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	resp := sessionResponse{
		SetupRequired: s.Auth.Count() == 0,
		SSOAvailable:  s.OIDC != nil,
	}
	if user, ok := s.sessionUser(r, time.Now()); ok {
		resp.Authenticated = true
		resp.Username = user.Username
		resp.Role = string(user.Role)
		resp.HasLocalPassword = user.LocalPassword()
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
}

// writeAuthError translates err into a safe, user-facing message via
// authErrorMessages (falling back to a generic one for anything not
// listed, logging the real error server-side so it's still
// diagnosable) and writes it with status.
func writeAuthError(w http.ResponseWriter, err error, status int) {
	msg, ok := authErrorMessages[err]
	if !ok {
		authLog.Warn(err.Error())
		msg = "unable to complete the request"
	}
	http.Error(w, msg, status)
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
		case auth.ErrPasswordTooShort, auth.ErrUsernameInvalid, auth.ErrUsernameLength:
			status = http.StatusBadRequest
		}
		writeAuthError(w, err, status)
		return
	}

	sess := s.Sessions.Create(user.ID, time.Now())
	s.setSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role})
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
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
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

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.Sessions.Revoke(cookie.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
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
	caller := userFromContext(r)
	return caller != nil && caller.Role == auth.RoleAdmin
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
	// downgraded to a user: the caller asked for something this
	// deployment does not have, and silently creating a lesser account
	// under the name they chose is worse than telling them. auth.Store
	// refuses it too -- this exists to give a usable status and message
	// instead of a 500.
	if req.Role == string(auth.RoleAdmin) {
		writeAuthError(w, auth.ErrSingleAdmin, http.StatusBadRequest)
		return
	}

	user, err := s.Auth.CreateUser(req.Username, req.Password, auth.RoleUser, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrUsernameTaken:
			status = http.StatusConflict
		case auth.ErrPasswordTooShort, auth.ErrSingleAdmin, auth.ErrUsernameInvalid, auth.ErrUsernameLength:
			status = http.StatusBadRequest
		}
		writeAuthError(w, err, status)
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
		writeAuthError(w, err, status)
		return
	}

	s.Sessions.RevokeAllForUser(user.ID)
	revokedTokens := 0
	if s.Tokens != nil {
		revokedTokens = s.Tokens.RevokeAllCreatedBy(user.ID)
	}

	s.Audit.Record(auditActor(r), "user.delete", user.Username,
		fmt.Sprintf("role=%s tokensRevoked=%d", user.Role, revokedTokens))
	writeJSON(w, http.StatusOK, map[string]any{
		"username":      user.Username,
		"tokensRevoked": revokedTokens,
	})
}
