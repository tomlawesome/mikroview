// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/engine"
)

// detectorEntry is one detector's operator-facing state. The JSON shape
// is unchanged from when this was backed by internal/detect's settings
// store (issue #405 deleted it): the UI's contract is name + enabled +
// scope, and moving where those live must not move what they look like.
//
// Name is the shipped definition's id, which is the same string
// internal/detect.DetectorName used -- so every detector this endpoint
// has ever listed keeps its name, and the set it lists is unchanged too:
// see engine.LegacyDetectorIDs for why the five definitions the port
// newly made toggleable are deliberately not surfaced here yet.
type detectorEntry struct {
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Scope   engine.Scope `json:"scope"`
}

// handleDetectorSettingsList serves every shipped detection definition's
// current enabled + scope, always all of them regardless of whether each
// has ever been customized -- see docs/configuration.md's "Per-detector
// toggles" section.
//
// Read from the definitions store rather than from a separate settings
// document: a definition's envelope is where enabled and scope live now
// (docs/decisions/evaluation-engine.md section 4), so there is one answer
// to "is this detector on" rather than two that can disagree.
func (s *Server) handleDetectorSettingsList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	byID := make(map[string]detectorEntry)
	for _, sd := range s.Definitions.List() {
		if !sd.Available || sd.Definition.Provenance.Origin != engine.ProvenanceShipped {
			continue
		}
		byID[sd.Definition.ID] = detectorEntry{
			Name:    sd.Definition.ID,
			Enabled: sd.Definition.Enabled,
			Scope:   sd.Definition.Scope,
		}
	}
	// Iterated over the catalogue's own stable order rather than over the
	// store, so the list order is a property of the binary and not of map
	// iteration or of what happens to be persisted.
	ids := engine.LegacyDetectorIDs()
	out := make([]detectorEntry, 0, len(ids))
	for _, id := range ids {
		entry, ok := byID[id]
		if !ok {
			// In the catalogue but not yet in the store -- possible only
			// between a definitions-store write failure and the next boot's
			// seeding. Reported at its shipped default rather than omitted,
			// so the list is always the whole catalogue.
			entry = detectorEntry{Name: id, Enabled: true}
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"detectors": out})
}

type updateDetectorSettingsRequest struct {
	Enabled bool         `json:"enabled"`
	Scope   engine.Scope `json:"scope"`
}

// handleDetectorSettingsUpdate replaces one detector's enabled + scope
// wholesale and persists it, through the one narrow door a shipped
// definition allows (engine.DefinitionsStore.SetEnabledAndScope).
//
// Worth stating plainly, because the old handler's doc comment claimed
// otherwise: this takes effect on the next restart, not on the very next
// ingested event. A shipped definition reads its enabled/scope at
// construction, and main.go constructs the set once at boot. That became
// true for each detector as it was ported (issue #405), not here -- this
// handler is where the claim is corrected, and re-registering an edited
// definition live belongs to the definitions API (#407) along with the
// rest of the edit surface.
func (s *Server) handleDetectorSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	name := r.PathValue("name")
	if !engine.IsLegacyDetectorID(name) {
		http.Error(w, "unknown detector name", http.StatusNotFound)
		return
	}

	var req updateDetectorSettingsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := engine.ValidateScope(req.Scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch err := s.Definitions.SetEnabledAndScope(name, req.Enabled, req.Scope); {
	case err == nil:
	case errors.Is(err, engine.ErrNoSuchDefinition):
		http.Error(w, "unknown detector name", http.StatusNotFound)
		return
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.Audit.Record(auditActor(r), "detector.update", name, fmt.Sprintf("enabled=%t", req.Enabled))
	writeJSON(w, http.StatusOK, detectorEntry{Name: name, Enabled: req.Enabled, Scope: req.Scope})
}
