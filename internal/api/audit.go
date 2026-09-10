// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// auditActorInvariantViolation is what auditActor records when it is
// reached with no caller in context. Every handler that records an audit
// entry is gated by callerIsAdmin, which requires a non-nil caller, so
// this path is unreachable through the routed API today -- but the audit
// trail must never carry an empty actor, so the fallback stays as a
// guard against a future handler that records an entry without that
// gate.
//
// It is deliberately not "unauthenticated": that reads as a plausible
// anonymous action, which is the wrong story for code that should never
// run at all. Bracketed and impossible to mistake for a username, so a
// caller reading the audit log sees a code defect on sight rather than a
// believable actor. See TestAuditActorInvariantViolationValue.
const auditActorInvariantViolation = "[bug: auditActor reached with no caller in context]"

// auditActor resolves the acting username for an audit entry -- see
// auditActorInvariantViolation for what it records when that invariant
// (a caller is always present by the time a handler records an entry)
// does not hold.
func auditActor(r *http.Request) string {
	if u := userFromContext(r); u != nil {
		return u.Username
	}
	return auditActorInvariantViolation
}

// handleAuditList serves a windowed slice of the admin audit log --
// admin-only via the same strict callerIsAdmin check as GET /api/entities
// and GET /api/tokens: this is a plain historical record of admin
// actions, not something that makes sense to expose while auth itself is
// disabled (there's no "admin" concept in that state either -- see
// callerIsAdmin's own doc comment).
func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	q, ok := parseAuditQuery(w, r)
	if !ok {
		return
	}
	res := s.Audit.Query(q)
	writeJSON(w, http.StatusOK, res)
}

// parseAuditQuery mirrors parseQuery's (rest.go) since/until/limit
// parsing -- the subset of store.Query's windowed-query convention that
// still applies here (audit.Query has no per-event filters to parse).
// That includes refusing a malformed value rather than ignoring it; see
// badQueryParam for why.
func parseAuditQuery(w http.ResponseWriter, r *http.Request) (audit.Query, bool) {
	qs := r.URL.Query()
	var q audit.Query
	if v := qs.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "since", "RFC 3339")
			return audit.Query{}, false
		}
		q.Since = t
	}
	if v := qs.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "until", "RFC 3339")
			return audit.Query{}, false
		}
		q.Until = t
	}
	if v := qs.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			badQueryParam(w, "limit", "an integer")
			return audit.Query{}, false
		}
		q.Limit = n
	}
	return q, true
}
