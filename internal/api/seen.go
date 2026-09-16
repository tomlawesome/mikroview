// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/tomlawesome/mikroview/internal/seen"
)

// handleSeenValues serves the values this instance has actually observed
// for the two filter fields that have no option list anywhere else
// (issue #1226): protocol, and interface.
//
// One response carrying both fields rather than a field per route. The
// two lists are tiny, they are always wanted together -- the filter
// strip draws both menus at once -- and a `fields` object keyed by name
// means a third field later is a new key rather than a new endpoint.
//
// Viewer tier, the same reasoning as GET /api/hosts next door: this is
// what a non-admin looking at the stream needs to set a filter at all,
// and refusing it would leave them with the free-text box #1226 exists
// to replace.
//
// Deliberately not on readOnlyRoutes: the interface list is a partial
// inventory of the operator's network shape, which is not something a
// service-to-service bearer token has ever been able to read, and the
// filter strip is a browser surface with a session behind it.
func (s *Server) handleSeenValues(w http.ResponseWriter, r *http.Request) {
	fields := s.SeenValues.All()
	// Marshalled through a plain map keyed by the field's own name, so
	// the wire shape is {"fields":{"proto":[...],"interface":[...]}} --
	// the field names are contract, see internal/seen.Field.
	body := make(map[string][]seen.Value, len(fields))
	for field, values := range fields {
		body[string(field)] = values
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": body})
}
