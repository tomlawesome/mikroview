// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

type Server struct {
	Store      *store.Store
	Devices    *device.Registry
	Hub        *hub.Hub
	Reputation *reputation.Client
	StartTime  time.Time
}

// Routes builds the /api/* handler. Static frontend asset serving is
// mounted separately once the embedded build exists.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/ws", s.handleWS)
	mux.HandleFunc("GET /api/lookup/ip/{ip}", s.handleIPLookup)
	return mux
}
