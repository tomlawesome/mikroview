// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// auditActor resolves the acting username for an audit entry. Every
// handler that calls s.Audit.Record is already gated by either
// callerIsAdmin or callerIsAdminOrOpen, so userFromContext is non-nil in
// the overwhelmingly common case -- the one exception is
// callerIsAdminOrOpen's deliberate zero-account bootstrap bypass (see
// that function's doc comment in detector_settings.go), where a mutation
// can legitimately happen before any account -- and so any username --
// exists. "unauthenticated" makes that window visible in the log rather
// than silently attributing the action to an empty string.
func auditActor(r *http.Request) string {
	if u := userFromContext(r); u != nil {
		return u.Username
	}
	return "unauthenticated"
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
	res := s.Audit.Query(parseAuditQuery(r))
	writeJSON(w, http.StatusOK, res)
}

// parseAuditQuery mirrors parseQuery's (rest.go) since/until/limit
// parsing -- the subset of store.Query's windowed-query convention that
// still applies here (audit.Query has no per-event filters to parse).
func parseAuditQuery(r *http.Request) audit.Query {
	qs := r.URL.Query()
	var q audit.Query
	if v := qs.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Since = t
		}
	}
	if v := qs.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Until = t
		}
	}
	if v := qs.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.Limit = n
		}
	}
	return q
}
