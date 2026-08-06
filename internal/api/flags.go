package api

import (
	"net/http"
	"time"
)

// handleFlagsList serves every known flag, active and cleared -- the
// frontend decides how much cleared history to keep showing (see
// docs/configuration.md) -- plus the last hour of newly-raised-episode
// counts by Type at 1-minute resolution (flags.Store.TimeSeries), for
// FlagsChart. Same shape convention as GET /api/stats's timeSeries
// field (internal/store/ring.go's Stats.TimeSeries) -- added alongside
// the existing flags array rather than as a new endpoint.
func (s *Server) handleFlagsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"flags":      s.Flags.List(),
		"timeSeries": s.Flags.TimeSeries(),
	})
}

// handleFlagsClear marks one flag as cleared. Clearing an unknown or
// already-cleared ID is not an error (see flags.Store.Clear's doc
// comment) -- it just reports which case applied, so the frontend can
// still refresh its view either way.
func (s *Server) handleFlagsClear(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cleared := s.Flags.Clear(id, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared})
}

// handleFlagsClearPermanent is handleFlagsClear plus a permanent
// exclusion of the flag's (Type, Target) in the same step -- the
// "Clear and never flag this again" action (see flags.Store.
// ClearAndExclude). Open to any caller the same as handleFlagsClear;
// the exclusion itself isn't a sensitive read, and gating this one
// endpoint while leaving the plain clear open would be an inconsistent
// permission model for the same UI action. Reviewing/removing existing
// exclusions (handleExclusionsList/handleExclusionRemove below) is the
// admin-gated half of this feature, since that's the part where a
// mistake would otherwise be unrecoverable.
func (s *Server) handleFlagsClearPermanent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok := s.Flags.ClearAndExclude(id, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"cleared": ok, "excluded": ok})
}

// handleExclusionsList serves every permanently-excluded (Type, Target)
// pair -- admin-only (see callerIsAdminOrOpen), since this is the
// "undo a mistake" surface for handleFlagsClearPermanent.
func (s *Server) handleExclusionsList(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exclusions": s.Flags.ListExclusions()})
}

// handleExclusionRemove reverses one exclusion, letting its (Type,
// Target) raise again going forward -- admin-only, same gate as
// handleExclusionsList. Removing an unknown exclusion ID is not an
// error, same "no-op, not an error" reasoning as handleFlagsClear.
func (s *Server) handleExclusionRemove(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	removed := s.Flags.RemoveExclusionByID(id)
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}
