// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
)

// coverageDeclarationRequest is the wire shape PUT
// /api/coverage/declarations/{key} accepts -- just the reason. Key comes
// from the URL, DeclaredBy from the session, same "identity from the
// route/session, not the body" convention handleEntitiesUpsert's sibling
// endpoints don't quite follow (entities takes type/key in the body
// because an entity's Key can contain characters awkward to URL-encode
// reliably) -- a coverage-gap key is a boundary-direction pair the
// caller already has in hand from the coverage-gap list itself, so a
// path parameter is the natural fit here.
type coverageDeclarationRequest struct {
	Reason string `json:"reason"`
}

// handleCoverageList serves every persisted coverage-gap declaration --
// open to any authenticated user, unlike the admin-only writes below:
// seeing which gaps have already been explained is exactly what the
// non-admin viewer of a "why is this boundary quiet" list needs, the
// same asymmetry GET /api/definitions already carries relative to its
// own admin-only writes (#490).
func (s *Server) handleCoverageList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"declarations": s.Coverage.List()})
}

// handleCoveragePut creates or replaces the coverage-gap declaration at
// {key}, user tier and above: declaring a boundary intentionally quiet
// changes how a human reads an absence of detection there, so the
// decision carries the same weight as any other authored explanation
// (entities' labels, a definition's suppression) and is audit-logged the
// same way. Those neighbours are exactly why this is no longer
// admin-only -- #653 moved entity labels and the whole definitions
// surface to the user tier, and this row followed the reasoning it
// already rested on rather than being left behind alone.
func (s *Server) handleCoveragePut(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	key := r.PathValue("key")

	var req coverageDeclarationRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	d, err := s.Coverage.Put(key, req.Reason, auditActor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Audit.Record(auditActor(r), "coverage.declare", d.Key, "reason="+d.Reason)
	writeJSON(w, http.StatusOK, d)
}

// handleCoverageDelete removes the coverage-gap declaration at {key},
// User tier and above, same gate as handleCoveragePut (#653). 404 when no declaration
// exists at that key -- unlike flags.Store.Clear/entities.Store.Delete's
// "unknown ID is a no-op" convention (a caller with a stale list can't
// always tell which case applied), a coverage-gap key is something the
// caller looked up from the list this same endpoint's GET just served,
// so a 404 here is a meaningful, actionable signal rather than routine
// noise.
func (s *Server) handleCoverageDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	key := r.PathValue("key")
	if !s.Coverage.Delete(key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "coverage.undeclare", key, "")
	w.WriteHeader(http.StatusNoContent)
}
