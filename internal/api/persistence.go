// SPDX-License-Identifier: AGPL-3.0-only

package api

import "net/http"

// PersistenceInfo answers "where does mikroview keep its persisted state
// right now" (issue #677's settings persistence row) -- which backend
// internal/persist actually resolved to at boot for the stores it
// covers: flags, definitions, watchlist entries, entities, and tokens/
// accounts (see internal/persist's own package doc for the full list).
//
// Deliberately not the live event stream (internal/store's ring
// buffer): that package never touches internal/persist at all and is
// unconditionally memory-only regardless of config, which the frontend
// states on its own without needing this endpoint.
type PersistenceInfo struct {
	// Backend is "file" or "postgres" -- whichever main.go's storage.
	// backendFor actually resolved to, keyed on cfg.Postgres.DSNFile
	// being set. Never guessed independently of that decision.
	Backend string `json:"backend"`
	// Dir is the directory the JSON documents live under, for the file
	// backend -- one representative store's own configured path
	// (internal/auth's, since accounts are the one store mikroview
	// insists on persisting -- see main.go), directory-only rather than
	// a specific filename since several documents share it. Absent for
	// postgres, which has no filesystem path to report.
	Dir string `json:"dir,omitempty"`
}

// handlePersistence reports which backend this deployment's persisted
// stores use.
//
// Admin-gated like GET /api/config/problems, and for the same reason
// (see that handler's own doc comment): Dir is a filesystem path, the
// same infrastructure-map disclosure #131 already judged admin-only
// recon value. A non-admin gets a 403, not a 200 with Dir withheld --
// a partial-but-200 response would blur "refused" from "allowed but
// happens to be empty" for the authorization matrix, the same
// distinction handleConfigProblems' own doc comment explains.
func (s *Server) handlePersistence(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, s.Persistence)
}
