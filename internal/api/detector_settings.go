package api

import (
	"net/http"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/detect"
)

// callerIsAdminOrOpen mirrors requireAuth's own "no-op while zero users
// exist" contract (see auth.go) -- while auth is inactive (mikroview's
// fully-open default), every other endpoint stays reachable, so
// detector settings do too, and there's no admin to gate against yet
// anyway. Once an account exists, only an admin session may read or
// write them.
func (s *Server) callerIsAdminOrOpen(r *http.Request) bool {
	if s.Auth.Count() == 0 {
		return true
	}
	caller := userFromContext(r)
	return caller != nil && caller.Role == auth.RoleAdmin
}

type detectorEntry struct {
	Name    detect.DetectorName `json:"name"`
	Enabled bool                `json:"enabled"`
	Scope   detect.Scope        `json:"scope"`
}

// handleDetectorSettingsList serves every detector's current live
// on/off + scope settings, always all of them (from
// detect.AllDetectorNames) regardless of whether each has ever been
// customized -- see docs/configuration.md's "Per-detector toggles"
// section.
func (s *Server) handleDetectorSettingsList(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	all := s.DetectorSettings.List()
	out := make([]detectorEntry, 0, len(detect.AllDetectorNames))
	for _, name := range detect.AllDetectorNames {
		st := all[name]
		out = append(out, detectorEntry{Name: name, Enabled: st.Enabled, Scope: st.Scope})
	}
	writeJSON(w, http.StatusOK, map[string]any{"detectors": out})
}

type updateDetectorSettingsRequest struct {
	Enabled bool         `json:"enabled"`
	Scope   detect.Scope `json:"scope"`
}

// handleDetectorSettingsUpdate replaces one detector's enabled+scope
// wholesale and persists it (see detect.SettingsStore.Set) -- takes
// effect on the very next ingested event, no restart needed.
func (s *Server) handleDetectorSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	name := detect.DetectorName(r.PathValue("name"))
	if !detect.IsValidDetectorName(name) {
		http.Error(w, "unknown detector name", http.StatusNotFound)
		return
	}

	var req updateDetectorSettingsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := detect.ValidateScope(req.Scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.DetectorSettings.Set(name, detect.Settings{Enabled: req.Enabled, Scope: req.Scope})
	writeJSON(w, http.StatusOK, detectorEntry{Name: name, Enabled: req.Enabled, Scope: req.Scope})
}
