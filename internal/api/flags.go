package api

import (
	"net/http"
	"time"
)

// handleFlagsList serves every known flag, active and cleared -- the
// frontend decides how much cleared history to keep showing (see
// docs/configuration.md).
func (s *Server) handleFlagsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"flags": s.Flags.List()})
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
