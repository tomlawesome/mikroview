// SPDX-License-Identifier: AGPL-3.0-only

// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/oidc"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/store"
)

type Server struct {
	Store            *store.Store
	Devices          *device.Registry
	Hub              *hub.Hub
	Reputation       *reputation.Client
	Flags            *flags.Store
	DetectorSettings *detect.SettingsStore
	// Entities is the persisted, admin-manageable (type, key) -> label/
	// tags store backing GET/POST/DELETE /api/entities (issue #107) --
	// the shared foundation for a future mail-sender allowlist and
	// UI-managed IP/port/rule aliasing. Always non-nil (internal/entities.
	// Open("") returns a usable, empty, unpersisted store), same
	// always-usable convention as Flags/DetectorSettings above.
	Entities *entities.Store
	// Rules is the persisted, long-lived per-rule-label usage record
	// (issue #103's internal/rules.Store) -- exposed read-only via GET
	// /api/rules (issue #109) as the "discovered but unnamed rules"
	// source for the Entities admin panel: every rule label ever seen
	// firing, independent of the store's retention window, mirroring
	// device.Registry's own "auto-discovered, shown even before
	// configured/labeled" pattern. Always non-nil (internal/rules.
	// Open("") returns a usable, empty, unpersisted store), same
	// always-usable convention as Entities/Flags/DetectorSettings above.
	Rules *rules.Store
	// Audit is the persisted, admin-only accountability log of every
	// admin-privileged mutation (issue #112) -- who created a user,
	// changed a detector setting, upserted/deleted an entity, created or
	// revoked an API token, or removed a permanent flag exclusion (see
	// flags.go's handleExclusionRemove; the flag-clear handlers
	// deliberately don't record here -- see their own doc comments).
	// Deliberately separate from Flags: this is about actions taken *in*
	// mikroview, not behavior mikroview observes on the network. Always
	// non-nil (internal/audit.Open("") returns a usable, empty,
	// unpersisted store), same always-usable convention as Entities/
	// Flags/DetectorSettings above.
	Audit *audit.Store
	// CriticalPorts is the configured control-port list (issue #34's
	// tracking tab) -- exposed read-only via GET /api/critical-ports,
	// deliberately not behind handleDetectorSettingsList's admin gate
	// (see that handler's callerIsAdmin) since a non-admin user
	// account still needs it to render the tab.
	CriticalPorts []int
	// DeviceStaleAfter (issue #98) is how long a device's LastSeen may go
	// without updating before GET /api/devices reports it as "stale" --
	// same threshold detect.DeviceSilenceDetector uses to raise an actual
	// flag (see internal/detect/device_silence.go), duplicated here
	// purely so this read-time status computation doesn't need to import
	// internal/detect for one number. Zero means "not configured": every
	// device with at least one event is always reported "live".
	DeviceStaleAfter time.Duration
	StartTime        time.Time
	// Version is main.version (the build-time-stamped short commit SHA,
	// "dev" for a plain local build) -- passed in rather than read
	// directly since internal/api can't import main. Surfaced on
	// GET /api/healthz, the one endpoint reachable with no auth and no
	// session regardless of deployment state, so "which build am I
	// running" is checkable without any special access.
	Version string
	// ConfigProblems are non-fatal configuration problems found at
	// startup, where a safe default was substituted for a bad value.
	// Surfaced to admins in the UI because a startup log line is seen
	// once, by whoever ran `docker compose up`, and never again -- which
	// is not good enough for a setting the operator believes is in
	// effect. See config_problems.go.
	ConfigProblems []ConfigProblem

	// Auth/Sessions/LoginLimiter/SecureCookie: see auth.go. Auth is
	// always non-nil (internal/auth.Open("") returns a usable, empty,
	// unpersisted store) -- mikroview stays fully open as long as it has
	// zero users, exactly like every other request path today.
	Auth         *auth.Store
	Sessions     *auth.SessionStore
	LoginLimiter *auth.LoginLimiter
	SecureCookie bool

	// TrustedProxies/ClientIPHeader control how the login rate limiter
	// attributes a request to a source address when mikroview sits behind
	// a reverse proxy -- see clientip.go, and config.Listen's fields of
	// the same names for why the empty default ignores forwarding headers
	// rather than trusting them.
	TrustedProxies []netip.Prefix
	ClientIPHeader string

	// Tokens holds read-only API bearer tokens (issue #101) -- always
	// non-nil (internal/auth.OpenTokenStore("") returns a usable, empty,
	// unpersisted store), same nil-never convention as Auth above.
	Tokens *auth.TokenStore

	// OIDC/OIDCState: see oidc.go. Both nil unless cfg.OIDC.IssuerURL was
	// set and provider discovery succeeded at startup -- every OIDC
	// handler checks for nil and 404s, so a misconfigured or absent OIDC
	// block never affects local auth, the same nil-means-disabled
	// convention Reputation already uses elsewhere on this struct.
	OIDC      *oidc.Client
	OIDCState *oidc.StateCodec
	// OIDCPolicy restricts which accounts at the issuer may sign in. The
	// zero value permits everyone the issuer vouches for, which is the
	// correct answer for a self-hosted IdP and refused at startup for a
	// multi-tenant one -- see internal/oidc.Policy and main.go.
	OIDCPolicy oidc.Policy
}

// route is one registered endpoint. Routes are declared as data rather
// than as direct mux.HandleFunc calls so the full set is enumerable at
// runtime -- http.ServeMux keeps its patterns in unexported fields, so
// without this there is no way for a test to ask "what endpoints exist?"
// and therefore no way to assert that every one of them has a
// deliberate access level. internal/api's authorization-matrix test
// depends on that enumeration; see authz_matrix_test.go for why (a real
// permission gap shipped precisely because nothing forced that question
// to be answered for a new route).
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// routes returns every /api/* endpoint. Order is irrelevant to
// ServeMux's matching (it is longest-pattern-wins, not first-match), so
// these stay grouped by area for readability.
func (s *Server) routes() []route {
	return []route{
		{http.MethodGet, "/api/healthz", s.handleHealthz},
		{http.MethodGet, "/api/events", s.handleEvents},
		{http.MethodGet, "/api/devices", s.handleDevices},
		{http.MethodGet, "/api/rules", s.handleRules},
		{http.MethodGet, "/api/critical-ports", s.handleCriticalPorts},
		{http.MethodGet, "/api/stats", s.handleStats},
		{http.MethodGet, "/api/ws", s.handleWS},
		{http.MethodGet, "/api/lookup/ip/{ip}", s.handleIPLookup},
		{http.MethodGet, "/api/flags", s.handleFlagsList},
		{http.MethodPost, "/api/flags/{id}/clear", s.handleFlagsClear},
		{http.MethodPost, "/api/flags/{id}/clear-permanent", s.handleFlagsClearPermanent},
		{http.MethodGet, "/api/flags/exclusions", s.handleExclusionsList},
		{http.MethodDelete, "/api/flags/exclusions/{id}", s.handleExclusionRemove},

		{http.MethodGet, "/api/detectors", s.handleDetectorSettingsList},
		{http.MethodPut, "/api/detectors/{name}", s.handleDetectorSettingsUpdate},

		{http.MethodGet, "/api/entities", s.handleEntitiesList},
		{http.MethodPost, "/api/entities", s.handleEntitiesUpsert},
		{http.MethodDelete, "/api/entities", s.handleEntitiesDelete},

		{http.MethodGet, "/api/audit", s.handleAuditList},
		{http.MethodGet, "/api/config/problems", s.handleConfigProblems},

		{http.MethodGet, "/api/auth/session", s.handleAuthSession},
		{http.MethodPost, "/api/auth/register", s.handleAuthRegister},
		{http.MethodPost, "/api/auth/login", s.handleAuthLogin},
		{http.MethodPost, "/api/auth/logout", s.handleAuthLogout},
		{http.MethodPost, "/api/auth/users", s.handleAuthCreateUser},
		{http.MethodGet, "/api/auth/users", s.handleAuthListUsers},
		{http.MethodDelete, "/api/auth/users/{id}", s.handleAuthDeleteUser},

		// Admin-only token management (issue #101) -- gated the same way
		// POST /api/auth/users is (see handleTokensCreate/
		// handleTokensList/handleTokensRevoke). The tokens themselves
		// grant access through a completely separate, deliberately
		// minimal mux -- see requireAuth's bearer-token branch in
		// auth.go -- not through anything registered here.
		{http.MethodPost, "/api/tokens", s.handleTokensCreate},
		{http.MethodGet, "/api/tokens", s.handleTokensList},
		{http.MethodDelete, "/api/tokens/{id}", s.handleTokensRevoke},

		{http.MethodGet, "/api/auth/oidc/login", s.handleOIDCLogin},
		{http.MethodPost, "/api/auth/oidc/link", s.handleOIDCLinkStart},
		{http.MethodGet, "/api/auth/oidc/callback", s.handleOIDCCallback},
	}
}

// Routes builds the /api/* handler. Static frontend asset serving is
// mounted separately once the embedded build exists. requireAuth wraps
// every route except the ones that must work before a session exists
// (healthz, and the specific auth endpoints that are unauthenticated by
// nature -- see auth.go's exemptPaths).
func (s *Server) Routes() http.Handler {
	return s.requireAuth(s.mux())
}

// mux is Routes without the authentication gate in front of it.
//
// It exists for the tests that exercise a handler's own behaviour rather
// than who is allowed to reach it. Those used to get an ungated API by
// standing the fixture up with authentication disabled, which is not a
// state that exists any more. Reaching for the inner mux says what those
// tests actually mean, and keeps the gate itself covered in one place --
// auth_test.go and the authzMatrix guard, which both mount Routes.
func (s *Server) mux() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.HandleFunc(r.method+" "+r.path, r.handler)
	}
	return mux
}
