// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/suggest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// handleSuggestionsList serves every suggestion candidate (#243 slice
// 5), admin-gated the same as the watchlist itself -- a candidate's
// Justification (which rule, which device) is the same class of
// administrative network information as an entry's own scope. An
// optional ?status= filter (off/on/hide) narrows the result to one of
// the three review views; anything else (including no candidates
// matching) returns an empty list, never a 400 -- filtering to nothing
// is a valid, unremarkable answer.
func (s *Server) handleSuggestionsList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	status := r.URL.Query().Get("status")
	candidates := []suggest.Candidate{}
	for _, c := range s.Suggest.List() {
		if status != "" && string(c.Status) != status {
			continue
		}
		candidates = append(candidates, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

// suggestErrorStatus translates internal/suggest's sentinel errors into
// the HTTP status the mutation handlers below share -- ErrNotFound is a
// 404 (the id is simply wrong), anything else (ErrNotOff, ErrNotHidden)
// is a 400 (the id is real but not in the state the action requires),
// mirroring watchlistErrorStatus's own reasoning above.
func suggestErrorStatus(err error) int {
	if errors.Is(err, suggest.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// handleSuggestionsAccept turns an Off candidate into a real expectation
// definition -- the only way a suggestion becomes something that
// actually watches traffic (#243 slice 5: "every generated candidate is
// one the operator reviews and explicitly acts on -- nothing here ever
// creates a watchlist.Entry by itself", see internal/suggest's own doc
// comment). Admin-gated at the same tier as creating a definition
// directly (handleDefinitionsCreate): this is exactly that action, just
// pre-filled from what the router already reported.
//
// A device candidate (KindDevice) always becomes an inverted entry that
// starts Observing with an empty Permitted set -- the same safe,
// observe-first default handleDefinitionsCreate already applies to every
// new inverted expectation. #243's slice 5 design conversation raised a
// second option (pre-filling Permitted from what the router's rules
// currently allow) as an admin-chosen, sticky-after-first-use setting,
// but the pushed RouterOS schema carries no destination-address data to
// pre-fill from -- the same gap #243 section 7 already records against
// per-entry visibility. Offering a setting that could never do anything
// different from this default would be worse than not offering it, so
// it's deferred until that data exists, not built half-right.
func (s *Server) handleSuggestionsAccept(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	candidate, ok := s.Suggest.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if candidate.Status != suggest.StatusOff {
		http.Error(w, suggest.ErrNotOff.Error(), http.StatusBadRequest)
		return
	}

	e := watchlist.Entry{ID: newDefinitionEntryID(), Name: candidate.Name}
	switch candidate.Kind {
	case suggest.KindDevice:
		e.Invert = true
		e.Source = candidate.Source
		e.Observing = true
	case suggest.KindPort:
		e.Ports = candidate.Ports
	case suggest.KindAddressList:
		// Scoped to the list itself, resolved live at match time (#274
		// item 2) -- not expanded into the addresses currently in it,
		// which would be stale the moment the router edited the list.
		e.SourceList = watchlist.AddressListRef{Device: candidate.RouterDevice, List: candidate.AddressList}
		// A non-inverted entry needs ports, and the candidate carries
		// none deliberately: a list rule says which *hosts* matter, not
		// which ports. The operator's own configured critical ports are
		// the honest starting point -- the same set the critical_port
		// detector already watches -- and the entry is editable from
		// the moment it exists.
		e.Ports = s.DefaultWatchPorts
		if len(e.Ports) == 0 {
			http.Error(w, "no default ports are configured to watch, so this suggestion cannot be turned into an entry -- "+
				"set flags.criticalPorts, or create the entry by hand with the ports you want", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "this suggestion cannot be accepted yet", http.StatusBadRequest)
		return
	}

	if err := s.Definitions.UpsertExpectation(e); err != nil {
		writeDefinitionError(w, err)
		return
	}
	if err := s.Suggest.Accept(id, e.ID); err != nil {
		// The candidate changed state between Get and here (another
		// admin accepted or hid it first) -- undo the definition just
		// created rather than leave a real expectation no candidate
		// points back to.
		if delErr := s.Definitions.DeleteExpectation(e.ID); delErr != nil {
			apiLog.Warn("rolling back an accepted suggestion's expectation failed: " + delErr.Error())
		}
		http.Error(w, err.Error(), suggestErrorStatus(err))
		return
	}

	stored, _, _ := s.Definitions.GetExpectation(e.ID)
	updated, _ := s.Suggest.Get(id)
	s.Audit.Record(auditActor(r), "definition.suggestion.accept", stored.ID, stored.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"candidate": updated, "entry": stored})
}

// handleSuggestionsHide moves a candidate from Off to Hide -- see
// suggest.Store.Hide. Admin-gated the same as accept: declining a
// suggestion is the same tier of decision as accepting one.
func (s *Server) handleSuggestionsHide(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	if err := s.Suggest.Hide(id); err != nil {
		http.Error(w, err.Error(), suggestErrorStatus(err))
		return
	}
	c, _ := s.Suggest.Get(id)
	s.Audit.Record(auditActor(r), "definition.suggestion.hide", id, c.Name)
	writeJSON(w, http.StatusOK, c)
}

// handleSuggestionsUnhide moves a candidate from Hide back to Off -- the
// only way a hidden candidate is ever seen again (see suggest.Store.
// Unhide and this package's own doc comment on why that's deliberate).
func (s *Server) handleSuggestionsUnhide(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	if err := s.Suggest.Unhide(id); err != nil {
		http.Error(w, err.Error(), suggestErrorStatus(err))
		return
	}
	c, _ := s.Suggest.Get(id)
	s.Audit.Record(auditActor(r), "definition.suggestion.unhide", id, c.Name)
	writeJSON(w, http.StatusOK, c)
}

// suggestResetRequest is the wire shape for POST /api/suggestions/reset.
type suggestResetRequest struct {
	Confirm bool `json:"confirm"`
}

// handleSuggestionsReset is #243 slice 5's "nuke" action: wipes the
// entire watchlist -- every real entry, not just suggestion-tracking
// state -- and immediately regenerates a fresh set of Off candidates
// from what the router has pushed, rather than leaving the operator
// looking at an empty page until the next background sync. Deliberately
// separate from every other mutation in this file: this is the one
// action in the whole feature that can destroy real, hand-tuned
// operator work, so it requires an explicit confirm:true body in
// addition to the admin gate every other handler here already enforces
// -- a UI confirm dialog alone cannot guarantee that step actually
// reached the server (#243 slice 5 design: "gated behind a real confirm
// step and an unmistakably serious warning -- this is the one part of
// the design that can destroy real, hand-tuned work").
func (s *Server) handleSuggestionsReset(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req suggestResetRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Confirm {
		http.Error(w, `this permanently deletes every watchlist entry -- resend with {"confirm": true} to proceed`, http.StatusBadRequest)
		return
	}

	wiped := s.Definitions.ResetExpectations()
	s.Suggest.Reset()
	s.Suggest.Sync(suggest.Generate(s.RouterState))

	s.Audit.Record(auditActor(r), "definition.suggestion.reset", "", fmt.Sprintf("%d entries removed", wiped))
	writeJSON(w, http.StatusOK, map[string]any{"candidates": s.Suggest.List()})
}
