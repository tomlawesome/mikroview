package api

import (
	"context"
	"net"
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

const userContextKey contextKey = iota

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
// choice screen (create an account, or skip auth for this deployment).
// Deliberately narrower than exemptPaths: everything else 401s during
// this window, closing the gap where live data (events/flags/stats)
// used to be readable by anyone who reached mikroview before a decision
// was made (see requireAuth's doc comment).
var bootstrapExemptPaths = map[string]bool{
	"/api/healthz":       true,
	"/api/auth/session":  true,
	"/api/auth/register": true,
	"/api/auth/skip":     true,
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

// requireAuth has three states, checked in order:
//
//  1. Disabled (s.Auth.Disabled()): a deliberate, permanent opt-out --
//     everyone reached the same "skip auth" choice this deployment made
//     on first boot. Fully open, indefinitely, same shape as (2) below
//     but without the path restriction, since there's no pending
//     decision left to protect.
//  2. Undecided (Count()==0, not disabled): only bootstrapExemptPaths
//     stay reachable -- just enough to show and complete the one-time
//     choice screen. Everything else 401s, closing the window where
//     live data (events/flags/stats) used to be readable by anyone who
//     reached mikroview before a decision was made.
//  3. Active (Count()>0): today's exact behavior -- CSRF header +
//     exemptPaths + session cookie.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth.Disabled() {
			next.ServeHTTP(w, r)
			return
		}
		if s.Auth.Count() == 0 {
			if !bootstrapExemptPaths[r.URL.Path] {
				http.Error(w, "setup required", http.StatusServiceUnavailable)
				return
			}
			// No session exists yet to carry a CSRF check the normal
			// way, but /api/auth/register and /api/auth/skip are the
			// two highest-consequence endpoints in the app -- one
			// creates the permanent admin account, the other
			// permanently disables auth for the deployment -- and
			// without this, a bare cross-site <form> POST (no
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

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
	// AuthDisabled: this deployment permanently opted out of auth (see
	// auth.Store.Disable) -- checked first by the frontend, since it
	// takes priority over SetupRequired (Count()==0 no longer implies
	// "show the choice screen" once a choice has actually been made).
	AuthDisabled  bool   `json:"authDisabled"`
	SetupRequired bool   `json:"setupRequired"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	Role          string `json:"role,omitempty"`
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
		AuthDisabled:  s.Auth.Disabled(),
		SetupRequired: s.Auth.Count() == 0,
		SSOAvailable:  s.OIDC != nil,
	}
	if user, ok := s.sessionUser(r, time.Now()); ok {
		resp.Authenticated = true
		resp.Username = user.Username
		resp.Role = string(user.Role)
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
	auth.ErrAuthDisabled:       "authentication has been disabled for this deployment",
	auth.ErrNotPersisted:       "this deployment has no persistent storage configured -- an administrator needs to set one up before an account can be created",
	auth.ErrUsernameTaken:      "that username is already taken",
	auth.ErrPasswordTooShort:   auth.ErrPasswordTooShort.Error(), // already phrased for an end user
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

// handleAuthSkip permanently disables authentication for this
// deployment (see auth.Store.Disable) -- only reachable while
// Count()==0 (requireAuth's bootstrap-exempt window; Disable itself
// also refuses otherwise, as a second guard). No session is created;
// there's nothing to log into.
func (s *Server) handleAuthSkip(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.Disable(); err != nil {
		status := http.StatusInternalServerError
		if err == auth.ErrRegistrationClosed {
			status = http.StatusConflict
		}
		writeAuthError(w, err, status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": true})
}

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
		case auth.ErrRegistrationClosed, auth.ErrAuthDisabled:
			status = http.StatusConflict
		case auth.ErrNotPersisted:
			status = http.StatusServiceUnavailable
		case auth.ErrPasswordTooShort:
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
	ipKey := "ip:" + clientIP(r)
	userKey := "user:" + strings.ToLower(req.Username)
	if !s.LoginLimiter.Allow(ipKey, now) || !s.LoginLimiter.Allow(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}

	user, err := s.Auth.Authenticate(req.Username, req.Password, now)
	if err != nil {
		s.LoginLimiter.RecordFailure(ipKey, now)
		s.LoginLimiter.RecordFailure(userKey, now)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

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
// While no account exists (undecided or disabled), there's no admin to
// have called this as -- caller is nil either way (requireAuth's
// bootstrap-exempt window blocks this path entirely during "undecided,"
// and "disabled" bypasses the session check that would otherwise set
// it), so this one check covers every zero-account case without needing
// to special-case why Count() is still 0. Unlike
// callerIsAdminOrOpen (detector_settings.go), there is deliberately no
// "no users yet" bypass here: creating a user, or editing an entity
// record, has no meaning before an admin exists.
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
	role := auth.RoleUser
	if req.Role == string(auth.RoleAdmin) {
		role = auth.RoleAdmin
	}

	user, err := s.Auth.CreateUser(req.Username, req.Password, role, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrUsernameTaken:
			status = http.StatusConflict
		case auth.ErrPasswordTooShort:
			status = http.StatusBadRequest
		}
		writeAuthError(w, err, status)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role})
}
