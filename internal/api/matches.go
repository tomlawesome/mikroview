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

// handleMatchesQuery answers a windowed query over the persisted match
// log, in one of two modes.
//
// **By source device** (the default): the correlation surface #243
// section 3 exists for ("lookup by source IP over a time range is the
// birdcage correlation case"). Requires mac and/or ip --
// matchlog.Identity's own MAC-preferred rule applies, and
// matchlog.ErrEmptyIdentity (400) is the answer when neither is given.
//
// **Across every entry**, with entries=all: the most recent matches
// anywhere in the log, newest first, which is how an operator asks
// "what has broken recently" without already knowing which entry to ask
// about (#586, for the Matches tab of #584). It also reaches evidence no
// other request can: a non-inverted watchlist entry may have an empty
// Source ("any source"), and its matches, though recorded normally, are
// retrievable only by an identity nobody knows to ask for.
//
// entries=all is an explicit opt-in, and mac/ip may not accompany it,
// for the reason matchlog.RecentQuery's own doc comment gives at length:
// "no identity" must never quietly become "every device". A caller whose
// mac parameter failed to arrive gets a 400, not the whole log. Passing
// both is refused rather than resolved in one direction, on badQueryParam's
// reasoning -- a caller who believes they filtered and did not is the
// misreading that matters here.
//
// Both modes are bounded by matchlog's own clamp (0 -> 100, above 5000 ->
// 5000); limit is passed through, never trusted. Both carry the same
// gate: any signed-in user, and a read-only API token (see
// readOnlyRoutes in auth.go), the same tier as
// /api/events/flags/stats/devices. That is not a widening by accident --
// it is stated on this route's row in the authorization matrix. This is a
// read over evidence already collected, not a mutation, and a token that
// can already read the whole live event feed learns nothing new in kind
// from a bounded page of matches.
//
// Query parameters: entries (optional, "all" is the only accepted
// value), mac and/or ip (required unless entries=all, refused with it),
// since/until (RFC 3339, both optional -- an empty until means no upper
// bound), limit (optional, matchlog clamps it).
func (s *Server) handleMatchesQuery(w http.ResponseWriter, r *http.Request) {
	if s.MatchLog == nil {
		http.Error(w, "the match log is not available", http.StatusServiceUnavailable)
		return
	}

	qs := r.URL.Query()
	var since, until time.Time
	if v := qs.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "since must be RFC 3339", http.StatusBadRequest)
			return
		}
		since = t
	}
	if v := qs.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "until must be RFC 3339", http.StatusBadRequest)
			return
		}
		until = t
	}
	var limit int
	if v := qs.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "limit must be an integer", http.StatusBadRequest)
			return
		}
		limit = n
	}

	mac, ip := qs.Get("mac"), qs.Get("ip")
	results := []matchlog.Record{}
	yield := func(rec matchlog.Record) bool {
		results = append(results, rec)
		return true
	}

	var err error
	switch entries := qs.Get("entries"); entries {
	case "":
		err = s.MatchLog.Query(r.Context(), matchlog.Query{
			Source: matchlog.Identity{MAC: mac, IP: ip},
			Since:  since, Until: until, Limit: limit,
		}, yield)
	case "all":
		if mac != "" || ip != "" {
			http.Error(w, "entries=all queries every entry, so mac and ip must not be set -- drop entries=all to query one device", http.StatusBadRequest)
			return
		}
		err = s.MatchLog.Recent(r.Context(), matchlog.RecentQuery{
			Since: since, Until: until, Limit: limit,
		}, yield)
	default:
		http.Error(w, `entries must be "all", or absent to query one device by mac/ip`, http.StatusBadRequest)
		return
	}
	if err != nil {
		if errors.Is(err, matchlog.ErrEmptyIdentity) {
			http.Error(w, "mac or ip query parameter is required (or entries=all for every entry)", http.StatusBadRequest)
			return
		}
		apiLog.Warn("match query failed: " + err.Error())
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": results})
}
