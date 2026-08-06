// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
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
	Store   *store.Store
	Devices *device.Registry
	Hub     *hub.Hub
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
	// (see that handler's callerIsAdminOrOpen) since a non-admin user
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

	// Auth/Sessions/LoginLimiter/SecureCookie: see auth.go. Auth is
	// always non-nil (internal/auth.Open("") returns a usable, empty,
	// unpersisted store) -- mikroview stays fully open as long as it has
	// zero users, exactly like every other request path today.
	Auth         *auth.Store
	Sessions     *auth.SessionStore
	LoginLimiter *auth.LoginLimiter
	SecureCookie bool

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
}

// Routes builds the /api/* handler. Static frontend asset serving is
// mounted separately once the embedded build exists. requireAuth wraps
// every route except the ones that must work before a session exists
// (healthz, and the specific auth endpoints that are unauthenticated by
// nature -- see auth.go's exemptPaths).
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/rules", s.handleRules)
	mux.HandleFunc("GET /api/critical-ports", s.handleCriticalPorts)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/ws", s.handleWS)
	mux.HandleFunc("GET /api/lookup/ip/{ip}", s.handleIPLookup)
	mux.HandleFunc("GET /api/flags", s.handleFlagsList)
	mux.HandleFunc("POST /api/flags/{id}/clear", s.handleFlagsClear)
	mux.HandleFunc("POST /api/flags/{id}/clear-permanent", s.handleFlagsClearPermanent)
	mux.HandleFunc("GET /api/flags/exclusions", s.handleExclusionsList)
	mux.HandleFunc("DELETE /api/flags/exclusions/{id}", s.handleExclusionRemove)

	mux.HandleFunc("GET /api/detectors", s.handleDetectorSettingsList)
	mux.HandleFunc("PUT /api/detectors/{name}", s.handleDetectorSettingsUpdate)

	mux.HandleFunc("GET /api/entities", s.handleEntitiesList)
	mux.HandleFunc("POST /api/entities", s.handleEntitiesUpsert)
	mux.HandleFunc("DELETE /api/entities", s.handleEntitiesDelete)

	mux.HandleFunc("GET /api/audit", s.handleAuditList)

	mux.HandleFunc("GET /api/auth/session", s.handleAuthSession)
	mux.HandleFunc("POST /api/auth/register", s.handleAuthRegister)
	mux.HandleFunc("POST /api/auth/skip", s.handleAuthSkip)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("POST /api/auth/users", s.handleAuthCreateUser)

	// Admin-only token management (issue #101) -- gated the same way
	// POST /api/auth/users is (see handleTokensCreate/handleTokensList/
	// handleTokensRevoke). The tokens themselves grant access through a
	// completely separate, deliberately minimal mux -- see requireAuth's
	// bearer-token branch in auth.go -- not through anything registered
	// here.
	mux.HandleFunc("POST /api/tokens", s.handleTokensCreate)
	mux.HandleFunc("GET /api/tokens", s.handleTokensList)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleTokensRevoke)

	mux.HandleFunc("GET /api/auth/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("GET /api/auth/oidc/callback", s.handleOIDCCallback)

	return s.requireAuth(mux)
}
