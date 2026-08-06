package api

import (
	"net/http"

	"github.com/tomlawesome/mikroview/internal/entities"
)

// entityRequest is the wire shape for both POST (create/update) and
// DELETE (identify which record to remove) -- Label/Tags are simply
// ignored by the delete handler, which only needs Type/Key to identify
// the record. One shared struct rather than two nearly-identical ones,
// same reasoning credentialsRequest is reused across register/login in
// auth.go.
type entityRequest struct {
	Type  string   `json:"type"`
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Tags  []string `json:"tags"`
}

// handleEntitiesList serves every persisted entity -- admin-gated the
// same way user management is (see callerIsAdmin's doc comment):
// aliases/tags are administrative metadata about the network, not
// something every signed-in viewer needs to see or edit.
func (s *Server) handleEntitiesList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": s.Entities.List()})
}

// handleEntitiesUpsert creates a new entity, or replaces an existing one
// in place, identified by (type, key) -- see entities.Store.Upsert.
func (s *Server) handleEntitiesUpsert(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req entityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	e, err := s.Entities.Upsert(entities.Entity{Type: req.Type, Key: req.Key, Label: req.Label, Tags: req.Tags})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleEntitiesDelete removes the entity identified by (type, key),
// supplied as a JSON body (like POST, not a path/query parameter) so an
// arbitrary Key -- which might contain characters awkward to URL-encode
// reliably across every client -- never has to round-trip through a URL
// at all.
func (s *Server) handleEntitiesDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req entityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !s.Entities.Delete(req.Type, req.Key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
