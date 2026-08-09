// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

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
// changed only through their own dedicated endpoints (#243 slice 4's
// next piece), never by overwriting the whole entry -- a plain PUT here
// must not be able to silently wipe an entry's accumulated observations.
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
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.Watchlist.List()})
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
	s.Audit.Record(auditActor(r), "watchlist.delete", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
