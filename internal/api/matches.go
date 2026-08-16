// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

// newDefinitionEntryID returns a random 32-character hex string. Mirrors
// internal/auth's own newID(), internal/matchlog's own newID() and
// engine.newDefinitionID -- each package keeps a small private copy of
// this rather than sharing one, the same precedent internal/watchlist's
// own isTrackableConnState doc comment sets for this codebase. An
// expectation definition has no natural operator-chosen key the way
// internal/entities' (type, key) pairs do, so this generates one the same
// way a token or session ID would.
func newDefinitionEntryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("api: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// handleMatchesQuery answers a windowed query over the
// persisted match log for one source device -- the correlation surface
// #243 section 3 exists for ("lookup by source IP over a time range is
// the birdcage correlation case"). Reachable via a read-only API token
// as well as a session (see readOnlyRoutes in auth.go), the same tier as
// GET /api/events/flags/stats/devices: this is a read over evidence
// already collected, not a mutation, and external correlation is the
// point.
//
// Query parameters: mac and/or ip (matchlog.Identity's own MAC-preferred
// rule applies -- at least one is required, matchlog.ErrEmptyIdentity
// otherwise), since/until (RFC 3339, both optional -- an empty until
// means no upper bound), limit (optional, matchlog clamps it).
func (s *Server) handleMatchesQuery(w http.ResponseWriter, r *http.Request) {
	if s.MatchLog == nil {
		http.Error(w, "the match log is not available", http.StatusServiceUnavailable)
		return
	}

	q := matchlog.Query{
		Source: matchlog.Identity{MAC: r.URL.Query().Get("mac"), IP: r.URL.Query().Get("ip")},
	}
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "since must be RFC 3339", http.StatusBadRequest)
			return
		}
		q.Since = t
	}
	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "until must be RFC 3339", http.StatusBadRequest)
			return
		}
		q.Until = t
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "limit must be an integer", http.StatusBadRequest)
			return
		}
		q.Limit = n
	}

	results := []matchlog.Record{}
	err := s.MatchLog.Query(r.Context(), q, func(rec matchlog.Record) bool {
		results = append(results, rec)
		return true
	})
	if err != nil {
		if errors.Is(err, matchlog.ErrEmptyIdentity) {
			http.Error(w, "mac or ip query parameter is required", http.StatusBadRequest)
			return
		}
		apiLog.Warn("match query failed: " + err.Error())
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": results})
}
