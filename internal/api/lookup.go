// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/reputation"
)

// lookupResponse is the reputation result plus mikroview's own local
// network-class attribution (issue #114). NetClass is display-only
// context -- "AWS (eu-west-1)", "Tor exit" -- and is deliberately not
// folded into the reputation score: a datacenter label covers >10% of
// IPv4, so turning it into suspicion belongs behind direction and
// weighting decided elsewhere, not here.
type lookupResponse struct {
	reputation.Result
	NetClass *netClassView `json:"netClass,omitempty"`
}

type netClassView struct {
	Category string `json:"category"`
	Source   string `json:"source"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	// Display is the pre-rendered "Label (Detail)" string, so the UI does
	// not have to reassemble it.
	Display string `json:"display"`
}

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

	resp := lookupResponse{Result: result}
	if s.NetClass != nil {
		if class := s.NetClass.Lookup(ip); class.Matched {
			resp.NetClass = &netClassView{
				Category: string(class.Category),
				Source:   string(class.Source),
				Label:    class.Label,
				Detail:   class.Detail,
				Display:  class.String(),
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
