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

// handleCriticalPorts serves the configured control-port list (issue #34)
// -- read-only, no admin gate, since the tracking tab it feeds needs to
// work for any signed-in user, not just admins.
func (s *Server) handleCriticalPorts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ports": s.CriticalPorts})
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
		SrcScope:  parseScope(qs.Get("srcScope")),
		DstScope:  parseScope(qs.Get("dstScope")),
		Rule:      qs.Get("rule"),
		RuleRegex: qs.Get("ruleRegex") == "true",
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
	if v := qs.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Until = t
		}
	}
	// around+window (issue #29) is sugar for a bounded before/after
	// lookback centered on a timestamp -- overrides since/until if both
	// forms are present, since specifying a center point and specifying
	// explicit bounds are two ways of asking for the same thing.
	if v := qs.Get("around"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			window := 5 * time.Minute
			if wv := qs.Get("window"); wv != "" {
				if d, err := time.ParseDuration(wv); err == nil {
					window = d
				}
			}
			q.Since = t.Add(-window)
			q.Until = t.Add(window)
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

// parseScope accepts only the two recognized scope values -- anything
// else (including unset/empty) falls back to store.ScopeAny rather than
// erroring, the same "ignore rather than fail" treatment every other
// malformed query param here gets.
func parseScope(v string) store.Scope {
	switch store.Scope(v) {
	case store.ScopeInternal, store.ScopeExternal:
		return store.Scope(v)
	default:
		return store.ScopeAny
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
