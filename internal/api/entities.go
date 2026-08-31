// SPDX-License-Identifier: AGPL-3.0-only

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

// handleEntitiesList serves every persisted entity -- user-tier (#653),
// the same tier as the rest of the "watchers" bench definitions/
// suggestions surface: aliases/tags are administrative metadata about the
// network, which a user may see and edit, but a viewer -- who may not
// change anything that affects the instance -- may not.
func (s *Server) handleEntitiesList(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": s.Entities.List()})
}

// handleEntitiesUpsert creates a new entity, or replaces an existing one
// in place, identified by (type, key) -- see entities.Store.Upsert.
// User-tier (#653), same as handleEntitiesList.
func (s *Server) handleEntitiesUpsert(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	var req entityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Upsert conflates create and replace, so which status is right
	// depends on which it was -- and every sibling "this created a new
	// thing" handler here (handleWatchlistEntriesCreate,
	// handleTokensCreate, handleAuthRegister, handleAuthCreateUser,
	// handleSuggestionsAccept) answers 201. Always answering 200 was
	// defensible but unexplained, and left a caller unable to tell a
	// create from an overwrite (#267 finding 19).
	existed := s.Entities.Exists(req.Type, req.Key)

	e, err := s.Entities.Upsert(entities.Entity{Type: req.Type, Key: req.Key, Label: req.Label, Tags: req.Tags})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Audit.Record(auditActor(r), "entity.upsert", e.Type+":"+e.Key, "label="+e.Label)
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, e)
}

// handleEntitiesDelete removes the entity identified by (type, key),
// supplied as a JSON body (like POST, not a path/query parameter) so an
// arbitrary Key -- which might contain characters awkward to URL-encode
// reliably across every client -- never has to round-trip through a URL
// at all. User-tier (#653), same as handleEntitiesList.
func (s *Server) handleEntitiesDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
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
	s.Audit.Record(auditActor(r), "entity.delete", req.Type+":"+req.Key, "")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
