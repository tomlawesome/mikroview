package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
		"uptime": time.Since(s.StartTime).String(),
	})
}

// handleEvents serves a filtered, windowed slice of the retained buffer —
// used for the initial page load and "load older" pagination. The live
// WebSocket tail (handleWS) intentionally applies no server-side filtering;
// see ws.go.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	res := s.Store.Query(parseQuery(r))
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.Devices.List()})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.Store.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"total":            stats.Total,
		"byAction":         stats.ByAction,
		"topRules":         stats.TopRules,
		"timeSeries":       stats.TimeSeries,
		"eventsPerSecond":  stats.EventsPerSecond,
		"capacity":         stats.Capacity,
		"count":            stats.Count,
		"windowSeconds":    int(stats.Window.Seconds()),
		"connectedClients": s.Hub.ClientCount(),
	})
}

func parseQuery(r *http.Request) store.Query {
	qs := r.URL.Query()
	q := store.Query{
		Device:    qs.Get("device"),
		Action:    store.Action(qs.Get("action")),
		Protocol:  qs.Get("protocol"),
		Chain:     qs.Get("chain"),
		Interface: qs.Get("interface"),
		IP:        qs.Get("ip"),
		Rule:      qs.Get("rule"),
	}
	if v := qs.Get("port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Port = n
		}
	}
	if v := qs.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Since = t
		}
	}
	if v := qs.Get("sinceId"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			q.SinceID = n
		}
	}
	if v := qs.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Limit = n
		}
	}
	return q
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
