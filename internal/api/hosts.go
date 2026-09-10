// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/hosts"
)

// hostMarkRequest is the wire shape PUT /api/hosts/{key}/mark accepts.
// Key comes from the URL and the actor from the session, the same
// "identity from the route/session, not the body" convention
// coverageDeclarationRequest documents next door -- a host key is a
// boundary-address pair the caller already has in hand from GET
// /api/hosts, so a path parameter is the natural fit.
type hostMarkRequest struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// handleHostsList serves the whole presence register (issue #1016):
// every host the feed has shown, when it was first and last seen, how
// many events it accounts for, and any mark on it.
//
// Viewer tier, the same asymmetry GET /api/coverage/declarations
// already carries relative to its own user-tier writes: seeing which
// hosts have gone quiet, and which of those somebody has already
// explained, is exactly what a non-admin looking at the map needs.
//
// Deliberately not on readOnlyRoutes: a bearer token's blast radius
// stays where it is, and this list is a partial inventory of the
// operator's private address space, which is not something a
// service-to-service credential has ever been able to read.
func (s *Server) handleHostsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": s.Hosts.List()})
}

// handleHostMarkPut records what an operator says about the quiet host
// at {key}: `intended` (quiet on purpose, and it stays said) or
// `dismissed` (take it off the map until it speaks again).
//
// User tier and above, the same gate and the same reasoning as
// handleCoveragePut: saying a silence is deliberate changes how the
// next human reads it, so it carries the weight of any other authored
// explanation and is audit-logged like one.
//
// 404 rather than 400 for a key no event has ever registered: the
// caller looked this key up from the list this endpoint's own GET
// served, so "no such host" is a meaningful, actionable signal about a
// stale page rather than a malformed request. Every other refusal --
// an unknown kind, a reason that is empty when it must not be, text too
// long or carrying control characters -- is the register's own
// validation, surfaced as a 400.
func (s *Server) handleHostMarkPut(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	// The key is caller-controlled input from the URL and reaches the
	// audit trail below, so it is checked before it is used for anything
	// -- not left to the register's own validation further in.
	key := r.PathValue("key")
	if err := hosts.ValidateKey(key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req hostMarkRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	h, err := s.Hosts.Mark(key, hosts.MarkKind(req.Kind), req.Reason, auditActor(r))
	if errors.Is(err, hosts.ErrUnknownHost) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The stored kind and reason, not the request's: what went on the
	// record is what the audit line should say.
	s.Audit.Record(auditActor(r), "hosts.mark", h.Key, "kind="+string(h.Mark.Kind)+" reason="+h.Mark.Reason)
	writeJSON(w, http.StatusOK, h)
}

// handleHostMarkDelete takes the mark off the host at {key}, putting it
// back to whatever its own last-seen time says it is. User tier and
// above, same gate as handleHostMarkPut -- withdrawing an explanation
// is the same class of decision as making one, the same way
// handleCoverageDelete sits at its sibling's tier.
//
// 404 when there is no mark to remove, for the same
// "the caller is working from a list this endpoint served" reason
// handleCoverageDelete documents: an unknown key and an already-unmarked
// host are both a stale page, which is worth saying rather than
// swallowing.
func (s *Server) handleHostMarkDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	key := r.PathValue("key")
	if err := hosts.ValidateKey(key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.Hosts.Unmark(key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "hosts.unmark", key, "")
	w.WriteHeader(http.StatusNoContent)
}
