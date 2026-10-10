// SPDX-License-Identifier: AGPL-3.0-only

package api

// Authentication is gauntlet's (#1202): accounts, sessions, tokens,
// second factors, SSO and every /api/auth/* and /api/tokens route are
// gauntlet's gate, which this package mounts in front of its own routes.
// What stays here is what gauntlet's design leaves to the application:
// which of mikroview's own routes a role may reach (callerIsAdmin,
// callerIsUser), the three bearer muxes a token of each kind is confined
// to, the password re-checks some of mikroview's own routes ask for, and
// the side effects mikroview's state needs when an account changes (the
// router-backup vault's unlock, a deleted account's preferences).
//
// docs/decisions/gauntlet-adoption.md is the design; this file is its
// "HTTP wiring" paragraph.

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/gate"
	goidc "github.com/tomlawesome/gauntlet/oidc"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var authLog = logging.New("auth-api")

// ProductName is what gauntlet calls this deployment: in the URI an
// authenticator app scans, the name a browser shows for a new passkey,
// and the word no password may be.
const ProductName = "MikroView"

// sessionCookieName is gate.Config.CookieName. gate writes and reads it
// as "__Host-mikroview_session" while auth.secureCookie is on (see
// Server.sessionCookie), which no frontend code notices: the cookie is
// HttpOnly.
const sessionCookieName = "mikroview_session"

// csrfHeaderName/Value: gate requires the header on every request that
// changes data once an account exists (its name is gauntlet's, fixed;
// the value is this application's). Mikroview's frontend sends it on
// every fetch; a cross-site <form> cannot set it.
const (
	csrfHeaderName  = "X-Requested-With"
	csrfHeaderValue = "mikroview"
)

// TokenKinds is every kind the token store must be opened with:
// gauntlet's API and ingest kinds, and mikroview's droplist-pull
// (droplist.go). A kind left out is kept in the document but never
// authenticates -- every drop-list key a router already holds would
// stop working.
var TokenKinds = []gauntlet.TokenKind{gauntlet.TokenKindAPI, gauntlet.TokenKindIngest, TokenKindDroplistPull}

type contextKey int

const (
	// userContextKey carries the signed-in account to mikroview's own
	// handlers. gate puts it in its own context first; withCaller copies
	// it here, so every handler in this package asks one accessor
	// (userFromContext) and a handler test can supply a caller without
	// standing up a whole gate.
	userContextKey contextKey = iota
	// ingestTokenContextKey carries the authenticated ingest token
	// through to handleIngestRouterOS and handleIngestRouterBackup.
	// Device identity for a push comes from this token, never from the
	// request body, so a payload cannot claim to be from a router other
	// than the one its own credential is scoped to.
	ingestTokenContextKey
	// droplistTokenContextKey is its counterpart for the droplist-pull
	// kind -- handleDroplistPull needs the ID of the token that
	// authenticated this exact request for the ingest limiter.
	droplistTokenContextKey
)

// withCaller copies the account gate authenticated into this package's
// context. It sits inside Protect, so it only ever sees a request gate
// let through; a bearer-token request is dispatched by gate to one of
// the three token muxes and never reaches it.
func withCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := gate.UserFromContext(r); u != nil {
			r = r.WithContext(context.WithValue(r.Context(), userContextKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

// withToken is withCaller for a bearer mux: the token gate authenticated,
// under key.
func withToken(key contextKey, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := gate.TokenFromContext(r); t != nil {
			r = r.WithContext(context.WithValue(r.Context(), key, t))
		}
		next.ServeHTTP(w, r)
	})
}

// userFromContext returns the signed-in account for this request, or
// nil when there is none (a bearer-token request, or a route reachable
// without a session such as /api/healthz).
func userFromContext(r *http.Request) *gauntlet.User {
	u, _ := r.Context().Value(userContextKey).(*gauntlet.User)
	return u
}

// ingestTokenFromContext returns the authenticated ingest token -- only
// ever non-nil inside the handlers ingestRoutes registers.
func ingestTokenFromContext(r *http.Request) *gauntlet.Token {
	t, _ := r.Context().Value(ingestTokenContextKey).(*gauntlet.Token)
	return t
}

// droplistTokenFromContext returns the authenticated droplist-pull
// token -- only ever non-nil inside handleDroplistPull.
func droplistTokenFromContext(r *http.Request) *gauntlet.Token {
	t, _ := r.Context().Value(droplistTokenContextKey).(*gauntlet.Token)
	return t
}

// callerAtLeast reports whether r's signed-in caller holds a role at or
// above min (gauntlet.Role.AtLeast's stacked tiers: admin ⊇ user ⊇
// viewer). callerIsAdmin and callerIsUser below are this specialised:
// every gate on mikroview's own routes is defined in terms of this one
// function, so there is a single implementation of "who is allowed to
// reach this" rather than two that could drift apart.
//
// There is deliberately no "no accounts yet" bypass: with no account,
// gate refuses every route but the bootstrap ones before they route, and
// caller is nil here, so this is false either way.
func callerAtLeast(r *http.Request, min gauntlet.Role) bool {
	caller := userFromContext(r)
	return caller != nil && caller.Role.AtLeast(min)
}

// callerIsAdmin reports whether r's caller is an admin -- the check
// every admin-gated route of mikroview's own uses.
func callerIsAdmin(r *http.Request) bool {
	return callerAtLeast(r, gauntlet.RoleAdmin)
}

// callerIsUser reports whether r's caller holds at least the user role
// -- the gate for #653's operational tier: the writes that affect what
// mikroview is watching or clearing (flag judgement and clearing, and
// every /api/definitions, /api/entities, /api/naming/provenance and
// /api/suggestions route), open to user and admin alike but not viewer.
func callerIsUser(r *http.Request) bool {
	return callerAtLeast(r, gauntlet.RoleUser)
}

// readOnlyRoutes is the only handler set a bearer API token (issue
// #101) can ever reach -- deliberately its own separate *http.ServeMux
// with just these five GET routes registered, rather than a per-request
// allowlist check layered in front of the real mux. When this list
// changes, update the route list in TokensOverlay.svelte's hint text
// too -- it went stale once already (#326: four routes listed, five
// served). That's what makes "a token can never reach a write/clear/
// config endpoint" structural: there is no code path from a
// bearer-authenticated request to any other handler -- those are simply
// never registered on this mux, so a request for any of them
// (regardless of method) falls through to ServeMux's own 404, the same
// as a route that never existed.
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
// path from an ingest-authenticated request to anything else, including
// readOnlyRoutes' own GETs. That separation is exactly what stops an
// ingest token -- readable by any `read`-capable user on the router it
// came from, per #186 step 5 -- from becoming a read-everything
// credential.
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
// else.
func (s *Server) droplistPullRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/droplist.rsc", s.handleDroplistPull)
	return mux
}

// GateOptions are the parts of gate's configuration that main decides
// at start-up and the Server does not hold: whether SSO and passkeys are
// wired, the admin passkey rule, the sign-in history, and where a
// sign-in's country comes from.
type GateOptions struct {
	// SecureCookie is auth.secureCookie: the Secure attribute and, with
	// it, the __Host- prefix on the session cookie.
	SecureCookie bool
	// AdminPasskey is the resolved rule (main's adminPasskeyRule):
	// required or optional, never unset.
	AdminPasskey gate.AdminPasskeyRule
	// Passkeys is the relying party, or nil when passkeys are off.
	Passkeys gauntlet.PasskeyCeremony
	// OIDC, OIDCState and OIDCPolicy are SSO, all zero when it is off.
	OIDC       *goidc.Client
	OIDCState  *goidc.StateCodec
	OIDCPolicy goidc.Policy
	// SignIns is the sign-in history (#1202, ruling 8), or nil.
	SignIns *gauntlet.SignInHistory
	// Country resolves a sign-in's country (ruling 6), or nil.
	Country func(address string) (code string, ok bool)
	// Log receives gate's own lines. nil discards them.
	Log *slog.Logger
}

// NewGate builds the gate in front of this server from its stores
// (Auth, Sessions, Tokens, LoginLimiter), its client-address policy
// (ClientIP, the trusted-proxy rule), its audit log and its clock, plus
// what opts adds. The result goes in Server.Gate before Routes or
// SetupOnlyRoutes is called.
//
// The clock is s.now: time.Now on every deployment, and the clock
// POST /api/test/clock moves when the test hooks are on -- the one way a
// process-level test reaches session expiry, lockouts, held enrolments
// and token expiry without waiting for them.
func (s *Server) NewGate(opts GateOptions) (*gate.Gate, error) {
	if s.Auth == nil || s.Sessions == nil || s.Tokens == nil || s.LoginLimiter == nil {
		return nil, errors.New("api: NewGate needs Auth, Sessions, Tokens and LoginLimiter set first")
	}
	s.SecureCookie = opts.SecureCookie
	var audit gate.Auditor
	if s.Audit != nil {
		audit = auditRecorder{s}
	}
	return gate.New(gate.Config{
		CookieName:      sessionCookieName,
		SecureCookie:    opts.SecureCookie,
		CSRFHeaderValue: csrfHeaderValue,
		ProductName:     ProductName,
		// The frontend's own sign-in screen: where the SSO callback's
		// ?ssoError= redirect lands.
		LoginPath:    "/",
		ClientIP:     s.ClientIP,
		Country:      opts.Country,
		AdminPasskey: opts.AdminPasskey,
		Log:          opts.Log,
		Audit:        audit,
		Now:          s.now,
		// UnusualSignIns left zero: a sign-in from a new browser or
		// country is flagged on the session and in the audit log, never
		// refused (design record 6b). Notices nil: mikroview sends no
		// mail. PasskeySignIn off (ruling 9, #1443).
	}, gate.Deps{
		Users:      s.Auth,
		Sessions:   s.Sessions,
		Tokens:     s.Tokens,
		Limiter:    s.LoginLimiter,
		OIDC:       opts.OIDC,
		OIDCState:  opts.OIDCState,
		OIDCPolicy: opts.OIDCPolicy,
		Passkeys:   opts.Passkeys,
		SignIns:    opts.SignIns,
	})
}

// auditRecorder is gate.Auditor over the audit log: the same four
// fields, with the entry's own return value dropped -- gate records and
// moves on, as every handler here does. Read through the Server rather
// than captured, so a test that swaps s.Audit still sees its entries.
type auditRecorder struct{ s *Server }

func (a auditRecorder) Record(actor, action, target, detail string) {
	a.s.Audit.Record(actor, action, target, detail)
}

// sessionCookie is the name gate gives the session cookie: prefixed
// __Host- while the cookie is Secure, which a browser then refuses to
// accept from, or send to, anywhere but this exact host and port.
func (s *Server) sessionCookie() string {
	if s.SecureCookie {
		return "__Host-" + sessionCookieName
	}
	return sessionCookieName
}

// requestSessionID is the caller's session cookie value, or "" if there
// is none. Only ever compared against a session this server issued.
func (s *Server) requestSessionID(r *http.Request) string {
	cookie, err := r.Cookie(s.sessionCookie())
	if err != nil {
		return ""
	}
	return cookie.Value
}

// sessionUser resolves r's session cookie to an account by gate's own
// rules -- live, its account still there, and issued after that
// account's sessions were last ended (a password change, an admin reset,
// an SSO link: gauntlet.User.SessionCutoff). For the few places that ask
// again after gate let a request through: the WebSocket's per-ping
// re-check (#375) and the handlers that must act on the session's own
// account. A session that fails the cutoff is revoked here, as gate
// revokes it.
func (s *Server) sessionUser(r *http.Request, now time.Time) (*gauntlet.User, bool) {
	id := s.requestSessionID(r)
	if id == "" {
		return nil, false
	}
	sess, ok := s.Sessions.Validate(id, now)
	if !ok {
		return nil, false
	}
	user, ok := s.Auth.Get(sess.UserID)
	if !ok {
		return nil, false
	}
	if sess.IssuedAt.Before(user.SessionCutoff()) {
		s.Sessions.Revoke(sess.ID)
		return nil, false
	}
	return user, true
}

// recheckPassword is the password re-check mikroview's own routes ask
// for on top of a session -- the config editor, deleting the history
// files, opening the router-backup drop box and minting a device
// enrolment token: a guess at a live credential by somebody who already
// holds a session, which is exactly where a stolen cookie puts an
// attacker.
//
// Counted in gauntlet's per-account re-check budget (ReserveRecheck),
// not sign-in's: a run of typos at one of these dialogs must not lock
// the account out of signing in. Reserve, then release only on a match,
// so a wrong password keeps its reservation -- that is what counts it.
// It writes the refusal itself -- 429 past the budget, 401 with wrongMsg
// for a wrong password -- and reports whether the caller may go on.
//
// The attempt is stamped with the server's clock, s.now(), as gate
// stamps its own -- not the caller's time.Now(), which under the test
// clock would never age out of the window (#1202 review R9). A wrong
// password is audited, and a refusal by the limiter logged, as gate's
// own re-checks are (#1202 review R10, ASVS 16.3.1).
func (s *Server) recheckPassword(w http.ResponseWriter, r *http.Request, user *gauntlet.User, password, wrongMsg string) bool {
	now := s.now()
	if !s.LoginLimiter.ReserveRecheck(user.ID, now) {
		s.recheckRefused(user)
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return false
	}
	if !s.Auth.PasswordMatches(user.ID, password) {
		s.recheckFailed(r, user)
		writeUnauthorized(w, wrongMsg)
		return false
	}
	s.LoginLimiter.ReleaseRecheck(user.ID, now)
	return true
}

// recheckFailed records a wrong password at one of mikroview's re-checks
// exactly as gate records one at its own: user.login_failed, marked
// step=recheck, in gate's detail format, so the audit page reads the
// same whichever route the guess was made at. Never the password.
func (s *Server) recheckFailed(r *http.Request, user *gauntlet.User) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(user.Username, "user.login_failed", user.Username,
		fmt.Sprintf("outcome=%s method=%s step=recheck from=%q",
			gauntlet.SignInWrongPassword, gauntlet.SignInMethodPassword, s.clientIP(r)))
}

// recheckRefusedLog keeps recheckRefused's Warn to one line a minute: a
// session holder can hit a refused route as fast as they like.
var recheckRefusedLog = logging.NewLimiter(time.Minute)

// recheckRefused is the Warn for a re-check the limiter refused, as
// gate leaves one for its own.
func (s *Server) recheckRefused(user *gauntlet.User) {
	if total, ok := recheckRefusedLog.Allow(); ok {
		authLog.Warn(fmt.Sprintf("password re-check refused by the limiter: account=%q (%d refused re-check(s) since start-up)",
			logging.Printable(user.Username), total))
	}
}

// gateRoutes mounts gauntlet's route table, with the side effects
// mikroview's own state needs after the account routes that end
// sessions or accounts succeed. gauntlet ends the sessions; what it
// cannot know about is the router-backup vault's unlock, which belongs
// to a session (#956, #1120), or a deleted account's preferences record
// (#1283). Each follow-up runs only after a 2xx, so a refused request
// changes nothing here either -- with one exception, a deletion gauntlet
// carried out but answered 500 partially-completed (see below).
func (s *Server) gateRoutes(routes http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", routes)
	mux.Handle("/api/tokens", routes)
	mux.Handle("/api/tokens/", routes)

	// Signing out drops the vault's key if this session is what was
	// holding it open: the unlock is scoped to the session, so it cannot
	// outlive it. The cookie is read before gate clears it.
	mux.Handle("POST /api/auth/logout", s.afterSuccess(routes, func(r *http.Request) {
		s.lockVaultForSession(s.requestSessionID(r))
	}))
	// A password change and "sign out everywhere" end every session the
	// caller holds; the vault unlock goes with them when it is this
	// account's (#1124: never another account's -- these routes are open
	// to every role).
	ownAccount := func(r *http.Request) {
		if u := userFromContext(r); u != nil {
			s.lockVaultForUser(u.ID)
		}
	}
	mux.Handle("POST /api/auth/password", s.afterSuccess(routes, ownAccount))
	mux.Handle("POST /api/auth/logout-all", s.afterSuccess(routes, ownAccount))
	// An admin resetting an account, ending its sessions or deleting it
	// ends that account's unlock; a deleted account's preferences go too
	// (#1283's ruling: not left behind for an id nothing will ever sign
	// in as again).
	target := func(r *http.Request) { s.lockVaultForUser(r.PathValue("id")) }
	mux.Handle("POST /api/auth/users/{id}/reset-password", s.afterSuccess(routes, target))
	mux.Handle("POST /api/auth/users/{id}/logout-all", s.afterSuccess(routes, target))
	// A role change ends the account's sessions in gauntlet (a
	// downgrade), and whatever it was, the unlock was made under the
	// role it no longer has: it goes too (#1202 review R8).
	mux.Handle("PUT /api/auth/users/{id}/role", s.afterSuccess(routes, target))
	// Ending one session ends that session's unlock, as signing it out
	// does (#1202 review R8). The route names the session by its ref,
	// within the caller's own account; the holder's ref is worked out
	// from its ID, read before gate ends it.
	mux.Handle("DELETE /api/auth/sessions/{ref}", s.afterOutcome(routes, func(r *http.Request) func(int) {
		holder, holderUser := s.vaultUnlock.holding()
		caller := userFromContext(r)
		ref := r.PathValue("ref")
		return func(status int) {
			if !success(status) || holder == "" || caller == nil || holderUser != caller.ID {
				return
			}
			if subtle.ConstantTimeCompare([]byte(gauntlet.Session{ID: holder}.Ref()), []byte(ref)) == 1 {
				s.lockVaultForSession(holder)
			}
		}
	}))
	// A deleted account's preferences and unlock go with it. That
	// includes gauntlet's 500 partially-completed, where the account
	// was deleted and only revoking its tokens failed (#1202 review R7):
	// the account is gone either way, so the request is judged by
	// whether it is, not by the status alone.
	mux.Handle("DELETE /api/auth/users/{id}", s.afterOutcome(routes, func(r *http.Request) func(int) {
		id := r.PathValue("id")
		_, existed := s.Auth.Get(id)
		return func(status int) {
			if !success(status) && !(status == http.StatusInternalServerError && existed && accountGone(s.Auth, id)) {
				return
			}
			s.lockVaultForUser(id)
			if err := s.Prefs.Delete(id); err != nil {
				// The deletion already committed, so the request still
				// stands -- but a write that did not land is said out
				// loud rather than reported as done (R6).
				authLog.Error("deleting preferences for deleted user " + id + ": " + err.Error())
			}
		}
	}))
	return mux
}

// accountGone reports whether the account id no longer exists.
func accountGone(store *gauntlet.Store, id string) bool {
	_, ok := store.Get(id)
	return !ok
}

// success reports whether status is a 2xx.
func success(status int) bool { return status >= 200 && status < 300 }

// afterSuccess serves r through next and then, if the answer was a 2xx,
// runs then with the same request.
func (s *Server) afterSuccess(next http.Handler, then func(*http.Request)) http.Handler {
	return s.afterOutcome(next, func(r *http.Request) func(int) {
		return func(status int) {
			if success(status) {
				then(r)
			}
		}
	})
}

// afterOutcome serves r through next, with before called first -- to
// read what the request is about to change -- and the function it
// returns called afterwards with the status that was written.
func (s *Server) afterOutcome(next http.Handler, before func(*http.Request) func(status int)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := before(r)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		after(rec.status)
	})
}

// statusRecorder notes the status a handler wrote, passing everything
// through unchanged.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Routes is the whole /api/ surface: gate's Protect in front of
// mikroview's own routes, gauntlet's account and token routes beside
// them, and the three bearer muxes registered as gate's handlers for
// each token kind -- a token of any kind reaches its own mux and never
// the session-gated one. Server.Gate must be set.
//
// Protect runs first on every request: CSRF, the bootstrap window while
// no account exists, bearer dispatch, the session cookie, and the three
// doors (must-change-password, must-enrol-factor, must-enrol-passkey).
// Only then does a request reach a route here, and only then does that
// route's own role check (callerIsAdmin, callerIsUser) apply.
func (s *Server) Routes() http.Handler {
	s.registerBearerRoutes()
	mux := http.NewServeMux()
	mux.Handle("/", s.mux())
	auth := s.gateRoutes(s.Gate.Routes())
	mux.Handle("/api/auth/", auth)
	mux.Handle("/api/tokens", auth)
	mux.Handle("/api/tokens/", auth)
	return s.Gate.Protect(withCaller(mux))
}

// registerBearerRoutes gives gate the three bearer muxes, in the order
// tokens are tried: API tokens first, the far more common credential.
func (s *Server) registerBearerRoutes() {
	s.Gate.Handle(gauntlet.TokenKindAPI, s.readOnlyRoutes())
	s.Gate.Handle(gauntlet.TokenKindIngest, withToken(ingestTokenContextKey, s.ingestRoutes()))
	s.Gate.Handle(TokenKindDroplistPull, withToken(droplistTokenContextKey, s.droplistPullRoutes()))
}
