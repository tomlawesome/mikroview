package api

import (
	"errors"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/reputation"
)

// handleIPLookup proxies an on-demand reputation/threat-intel lookup for
// a single public IP (see internal/reputation) -- kept server-side so any
// configured API key never reaches the browser, and so results can be
// cached briefly to conserve free-tier quotas.
func (s *Server) handleIPLookup(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")

	result, err := s.Reputation.Lookup(r.Context(), ip)
	if errors.Is(err, reputation.ErrNotPublic) {
		http.Error(w, "not a public IP address", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
