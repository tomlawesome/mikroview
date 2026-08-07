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
// ClearAndExclude).
//
// Admin-only, and audit-logged. This deliberately does NOT match
// handleFlagsClear's open-to-any-caller model, despite the two being
// adjacent buttons on the same UI row, because they differ in the one
// way that matters here: a plain clear is reversible (the flag can
// simply raise again on the next matching event), while an exclusion
// permanently suppresses detection for that (Type, Target) until an
// admin notices and undoes it. Leaving it open let any authenticated
// non-admin -- or a single compromised low-privilege account -- blind
// this deployment's detection for a target of their choosing, with no
// record of who did it. For a tool whose entire purpose is surfacing
// suspicious activity, silently losing that coverage is the more
// expensive failure than a bit of permission-model asymmetry.
//
// The exclusion's reviewability (handleExclusionsList) and its undo
// (handleExclusionRemove) were already admin-gated; this brings the
// action that creates one into line with them.
func (s *Server) handleFlagsClearPermanent(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	ok := s.Flags.ClearAndExclude(id, time.Now())
	if ok {
		s.Audit.Record(auditActor(r), "flag.clear_permanent", id, "")
	}
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
// error, same "no-op, not an error" reasoning as handleFlagsClear --
// only logged to the audit trail when an exclusion was actually found
// and removed, since a no-op on an unknown ID isn't a meaningful action.
func (s *Server) handleExclusionRemove(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	removed := s.Flags.RemoveExclusionByID(id)
	if removed {
		s.Audit.Record(auditActor(r), "flag.exclusion_remove", id, "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}
