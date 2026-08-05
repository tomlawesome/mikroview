// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

type Server struct {
	Store            *store.Store
	Devices          *device.Registry
	Hub              *hub.Hub
	Reputation       *reputation.Client
	Flags            *flags.Store
	DetectorSettings *detect.SettingsStore
	StartTime        time.Time

	// Auth/Sessions/LoginLimiter/SecureCookie: see auth.go. Auth is
	// always non-nil (internal/auth.Open("") returns a usable, empty,
	// unpersisted store) -- mikroview stays fully open as long as it has
	// zero users, exactly like every other request path today.
	Auth         *auth.Store
	Sessions     *auth.SessionStore
	LoginLimiter *auth.LoginLimiter
	SecureCookie bool
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
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/ws", s.handleWS)
	mux.HandleFunc("GET /api/lookup/ip/{ip}", s.handleIPLookup)
	mux.HandleFunc("GET /api/flags", s.handleFlagsList)
	mux.HandleFunc("POST /api/flags/{id}/clear", s.handleFlagsClear)

	mux.HandleFunc("GET /api/detectors", s.handleDetectorSettingsList)
	mux.HandleFunc("PUT /api/detectors/{name}", s.handleDetectorSettingsUpdate)

	mux.HandleFunc("GET /api/auth/session", s.handleAuthSession)
	mux.HandleFunc("POST /api/auth/register", s.handleAuthRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("POST /api/auth/users", s.handleAuthCreateUser)

	return s.requireAuth(mux)
}
