// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// newWatchlistEntryID returns a random 32-character hex string. Mirrors
// internal/auth's own newID() and internal/matchlog's own newID() --
// each package keeps a small private copy of this rather than sharing
// one, the same precedent internal/watchlist's own isTrackableConnState
// doc comment sets. A watchlist entry has no natural operator-chosen key
// the way internal/entities' (type, key) pairs do, so this generates one
// the same way a token or session ID would.
func newWatchlistEntryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("api: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// watchlistEntryRequest is the wire shape for creating or updating a
// watchlist entry -- deliberately narrower than watchlist.Entry itself.
// Observing, Permitted and Observed are not settable here: Observing
// follows a fixed rule (see handleWatchlistEntriesCreate/Update below),
// and Permitted/Observed are the observe/promote workflow's own state,
// changed only through their own dedicated endpoints
// (handleWatchlistEntriesPromote, handleWatchlistEntriesSetObserving
// below), never by overwriting the whole entry -- a plain PUT here must
// not be able to silently wipe an entry's accumulated observations.
type watchlistEntryRequest struct {
	Name                   string            `json:"name"`
	Source                 matchlog.Identity `json:"source"`
	DestIP                 string            `json:"destIp"`
	Ports                  []int             `json:"ports"`
	Invert                 bool              `json:"invert"`
	IncludeStructuralNoise bool              `json:"includeStructuralNoise"`
}

// applyTo copies req's operator-settable fields onto e in place.
func (req watchlistEntryRequest) applyTo(e *watchlist.Entry) {
	e.Name = req.Name
	e.Source = req.Source
	e.DestIP = req.DestIP
	e.Ports = req.Ports
	e.Invert = req.Invert
	e.IncludeStructuralNoise = req.IncludeStructuralNoise
}

// handleWatchlistEntriesList serves every persisted watchlist entry --
// admin-gated the same way Entities is (issue #243 grows Control Ports
// into this): watchlist scope is administrative configuration about the
// network, not something every signed-in viewer needs to see or edit.
// Matches (what an entry has actually recorded) are a separate,
// still-to-come read surface -- not this endpoint, and not necessarily
// admin-only, since birdcage-style correlation is the whole reason
// internal/matchlog exists.
func (s *Server) handleWatchlistEntriesList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	entries := s.Watchlist.List()
	coverage, evidence := s.watchlistCoverage(entries)
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		// Per-entry, keyed by entry ID rather than added to Entry
		// itself: coverage is derived from what routers have pushed at
		// this moment, not a property of the entry, and putting it on
		// the stored struct would mean persisting an answer that goes
		// stale the moment a rule changes. See watchlist.Coverage.
		"coverage": coverage,
		// What the coverage answers above were actually derived from --
		// see coverageEvidence. Surfaced rather than swallowed (#367):
		// when the evidence base is incomplete every answer above
		// degrades to "unknown", and this is the only place a caller can
		// see *why* it did.
		"coverageEvidence": evidence,
	})
}

// coverageEvidence records whether the pushed filter tables
// watchlistCoverage read can honestly be treated as *every* table that
// could feed the watchlist (#367).
//
// Complete is false when at least one device has fed mikroview events
// but has never completed the optional filter-rule state push, so its
// rules were never read. MissingDevices names them, sorted, so the gap
// is visible rather than only implied by a downgraded answer.
type coverageEvidence struct {
	Complete       bool     `json:"complete"`
	MissingDevices []string `json:"missingDevices,omitempty"`
}

// watchlistCoverage answers, per entry, whether anything can actually
// feed it (#274 item 1) -- the difference between "no matches because
// nothing happened" and "no matches because nothing is watching".
//
// Reads the pushed filter tables once and reuses them across every
// entry, rather than per entry: the tables are small but the lock is
// shared with the ingest path, and this runs on every load of the
// Watchlist page.
//
// # Why a definite negative needs more than the pushed tables (#367)
//
// watchlist.Coverage answers only from the map it is handed, and cannot
// tell "no other router is watching" apart from "another router is
// watching, but its rules are not in this map" -- the map is built here,
// from RouterState, which holds only the routers that completed the
// *optional* filter-rule state push. A router that streams live syslog
// and is actively producing matches, but never pushed, is silently
// absent from it. If some other router happens to have pushed a table
// where nothing logs, watchlist.Coverage then returns CoverageNoLogging
// -- "no firewall rule on any router you have connected has logging
// turned on" -- for every entry, while the excluded router's matches are
// visible on the adjacent live-view page.
//
// coverage.go's own stated rule is that a negative answer "requires
// every relevant rule to have been read and understood." That is a
// property of the *evidence base*, not of any one entry, so it is
// checked once here (see coverageEvidenceFor) and applied to every
// answer: with an incomplete evidence base, both definite negatives --
// CoverageNoLogging ("nothing anywhere logs") and CoverageOutOfScope
// ("every logging rule was read, and each excludes this entry") --
// degrade to CoverageUnknown, which renders as silence rather than as a
// confident wrong claim. CoverageOK is untouched: it is a positive, and
// one router demonstrably logging the right traffic stays true however
// many other routers went unread.
func (s *Server) watchlistCoverage(entries []watchlist.Entry) (map[string]watchlist.CoverageState, coverageEvidence) {
	out := make(map[string]watchlist.CoverageState, len(entries))
	if s.RouterState == nil {
		// Nothing pushed anything, so nothing can be said. Same answer
		// watchlist.Coverage gives an empty map, spelled out here so a
		// nil store does not have to be a special case there.
		for _, e := range entries {
			out[e.ID] = watchlist.CoverageUnknown
		}
		return out, s.coverageEvidenceFor(nil)
	}

	rulesByDevice := make(map[string][]ingest.FilterRule)
	for _, device := range s.RouterState.Devices() {
		if rules, _, ok := s.RouterState.FilterRules(device); ok {
			rulesByDevice[device] = rules
		}
	}
	evidence := s.coverageEvidenceFor(rulesByDevice)

	for _, e := range entries {
		state := watchlist.Coverage(e, rulesByDevice)
		if !evidence.Complete {
			switch state {
			case watchlist.CoverageNoLogging, watchlist.CoverageOutOfScope:
				state = watchlist.CoverageUnknown
			}
		}
		out[e.ID] = state
	}
	return out, evidence
}

// coverageEvidenceFor reports whether rulesByDevice covers every device
// that has actually fed mikroview events -- the completeness check
// watchlistCoverage's own doc comment describes (#367).
//
// The event-feeding set comes from the device registry, and only
// entries that have really carried traffic count (EventCount > 0): a
// device declared in config.yaml but silent so far is not evidence of a
// gap, because nothing it could log is arriving anyway. A registry entry
// auto-discovered by source IP has that IP as its ID and so will never
// match a pushed device name, which correctly reads as incomplete -- an
// unregistered router streaming events is exactly the case #367 is
// about, and guessing that it "is probably" one of the pushed routers
// would be the same confident inference this whole file exists to
// refuse.
//
// A nil registry is likewise treated as incomplete rather than complete:
// with no way to ask which routers are feeding events, "the pushed set
// is all of them" is unknowable, not true.
func (s *Server) coverageEvidenceFor(rulesByDevice map[string][]ingest.FilterRule) coverageEvidence {
	if s.Devices == nil {
		return coverageEvidence{}
	}
	var missing []string
	for _, d := range s.Devices.List() {
		if d.EventCount == 0 {
			continue
		}
		if _, ok := rulesByDevice[d.ID]; !ok {
			missing = append(missing, d.ID)
		}
	}
	sort.Strings(missing)
	return coverageEvidence{Complete: len(missing) == 0, MissingDevices: missing}
}

// handleWatchlistEntriesCreate creates a new entry with a server-generated
// ID -- unlike internal/entities' operator-chosen (type, key) identity,
// a watchlist entry has no natural key an operator would want to type,
// so this mirrors internal/auth's newID()/internal/matchlog's own ID
// scheme instead.
//
// An inverted entry always starts Observing (#243 section 5: "a new
// inverted entry starts in an observe state") -- not a request field,
// since starting anywhere else would mean shipping traffic decisions
// before the operator has seen any evidence.
func (s *Server) handleWatchlistEntriesCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req watchlistEntryRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	e := watchlist.Entry{ID: newWatchlistEntryID()}
	req.applyTo(&e)
	if e.Invert {
		e.Observing = true
	}

	if err := s.Watchlist.Upsert(e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Re-fetched rather than echoing the local e: Upsert takes Entry by
	// value and assigns CreatedAt internally (see its own doc comment),
	// so the local variable here never sees that assignment -- only the
	// stored copy reflects what was actually persisted.
	stored, _ := s.Watchlist.Get(e.ID)
	s.Audit.Record(auditActor(r), "watchlist.create", stored.ID, stored.Name)
	writeJSON(w, http.StatusCreated, stored)
}

// handleWatchlistEntriesUpdate replaces an existing entry's
// operator-settable fields (see watchlistEntryRequest), preserving its
// Observing/Permitted/Observed state -- except when Invert itself
// changes, which has to reset that state coherently rather than leaving
// it in a shape that no longer means anything:
//
//   - Switching a non-inverted entry to inverted starts it Observing,
//     the same rule Create applies -- there is no meaningful permitted
//     set yet, so nothing else is coherent.
//   - Switching an inverted entry to non-inverted clears
//     Observing/Permitted/Observed entirely -- none of them apply to a
//     non-inverted entry, and leaving them would be stale data an
//     operator could not see or act on until switched back.
func (s *Server) handleWatchlistEntriesUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	e, ok := s.Watchlist.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var req watchlistEntryRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	wasInvert := e.Invert
	req.applyTo(&e)
	if e.Invert && !wasInvert {
		e.Observing = true
	} else if !e.Invert && wasInvert {
		e.Observing = false
		e.Permitted = nil
		e.Observed = nil
	}

	if err := s.Watchlist.Upsert(e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Audit.Record(auditActor(r), "watchlist.update", e.ID, e.Name)
	writeJSON(w, http.StatusOK, e)
}

// handleWatchlistEntriesDelete removes the entry identified by the {id}
// path segment -- safe to put directly in a URL, unlike
// internal/entities' operator-chosen keys (see entities.go's own doc
// comment on why that one is body-based instead): a watchlist entry's ID
// is server-generated hex, never operator input.
//
// If this entry originated from a suggestion (#243 slice 5), deleting it
// here -- through the normal entry-management page, not the suggestions
// table -- sends that candidate straight to Hide rather than back to
// Off: deleting something you explicitly created signals "I don't want
// this," not "reconsider me later" (settled in #243's slice 5 design
// conversation). A no-op when no candidate tracks this entry, which is
// the common case for an entry created directly.
func (s *Server) handleWatchlistEntriesDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	if _, ok := s.Watchlist.Get(id); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.Watchlist.Delete(id)
	s.Suggest.MarkHiddenByEntry(id)
	s.Audit.Record(auditActor(r), "watchlist.delete", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// watchlistErrorStatus translates watchlist.Store's sentinel errors into
// the HTTP status the mutation handlers below share -- ErrEntryNotFound
// is a 404 (the id is simply wrong), anything else (ErrNotInverted) is a
// 400 (the id is real but the request doesn't apply to it).
func watchlistErrorStatus(err error) int {
	if errors.Is(err, watchlist.ErrEntryNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// promoteRequest is the wire shape for POST .../promote.
type promoteRequest struct {
	Destinations []watchlist.PermittedDest `json:"destinations"`
}

// handleWatchlistEntriesPromote moves the given destination/port pairs
// from an inverted entry's Observed candidate list into its Permitted
// allow-list -- see watchlist.Store.Promote. Admin-gated, same tier as
// entry CRUD: this changes what future traffic is treated as expected
// for a device, the same weight as creating the entry in the first
// place.
func (s *Server) handleWatchlistEntriesPromote(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	var req promoteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.Watchlist.Promote(id, req.Destinations); err != nil {
		http.Error(w, err.Error(), watchlistErrorStatus(err))
		return
	}
	e, _ := s.Watchlist.Get(id)
	s.Audit.Record(auditActor(r), "watchlist.promote", id, strconv.Itoa(len(req.Destinations))+" destination(s)")
	writeJSON(w, http.StatusOK, e)
}

// setObservingRequest is the wire shape for POST .../observing.
type setObservingRequest struct {
	Observing bool `json:"observing"`
}

// handleWatchlistEntriesSetObserving flips whether an inverted entry is
// in observe mode -- see watchlist.Store.SetObserving. The raw
// mechanism only: this package (like internal/watchlist itself) makes
// no judgement about when an operator should call it, #243 open
// question 3.
func (s *Server) handleWatchlistEntriesSetObserving(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	var req setObservingRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.Watchlist.SetObserving(id, req.Observing); err != nil {
		http.Error(w, err.Error(), watchlistErrorStatus(err))
		return
	}
	e, _ := s.Watchlist.Get(id)
	action := "watchlist.observing.stop"
	if req.Observing {
		action = "watchlist.observing.start"
	}
	s.Audit.Record(auditActor(r), action, id, "")
	writeJSON(w, http.StatusOK, e)
}

// handleWatchlistMatchesQuery answers a windowed query over the
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
func (s *Server) handleWatchlistMatchesQuery(w http.ResponseWriter, r *http.Request) {
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
		apiLog.Warn("watchlist match query failed: " + err.Error())
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": results})
}
