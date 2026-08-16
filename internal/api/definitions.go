// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is issue #407: one definitions surface, replacing the two
// the engine retired. `/api/detectors` and `/api/watchlist/entries` are
// gone -- no compatibility handler, no alias, no route that exists only
// to return a friendlier error (AGENTS.md's "removals are wholesale",
// and docs/decisions/evaluation-engine.md's ratified decision 3).
//
// What replaces them is not a rename. A detector toggle and a watchlist
// entry were two shapes over the same thing -- a definition the engine
// evaluates -- and the endpoints below expose that one thing uniformly:
// list/get, enable/disable, scope, param overrides with schema
// validation, suppressions, provenance and replayability on every
// response, plus the operator actions an expectation has of its own
// (promote, observing).
//
// Access is admin-only for every route here, exactly matching what the
// two removed surfaces enforced. #385 records the owner decision that
// non-admins should eventually see settings surfaces read-only, but that
// belongs to phase 2's RBAC work: shipping a read-open route now that
// phase 2 might have to narrow is worse than shipping it closed and
// widening it deliberately. Every row is recorded in
// authz_matrix_test.go, which is what forces the question to be answered
// rather than inherited.

// definitionView is one definition as this API serves it: the whole
// envelope, plus the four things a caller cannot derive from it --
// whether this binary can evaluate it at all, how far its params are
// from stock, whether it can answer a replay question, and (for an
// expectation) whether anything can actually feed it.
//
// The envelope fields are spelled out rather than embedding
// engine.Definition, so the wire shape is visible in one place and a
// field added to the engine's own struct cannot silently start appearing
// in an API response.
type definitionView struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Description  string                       `json:"description,omitempty"`
	Intent       engine.Intent                `json:"intent"`
	Kind         engine.Kind                  `json:"kind"`
	Enabled      bool                         `json:"enabled"`
	Scope        engine.Scope                 `json:"scope,omitzero"`
	Params       engine.Params                `json:"params,omitempty"`
	ParamSchema  []engine.ParamSchema         `json:"paramSchema,omitempty"`
	Provenance   engine.Provenance            `json:"provenance"`
	Suppressions []engine.Suppression         `json:"suppressions,omitempty"`
	Available    bool                         `json:"available"`
	Distance     map[string]engine.ParamDelta `json:"distance,omitempty"`
	Replay       replayabilityView            `json:"replay"`
	// Coverage is only ever set for an expectation definition -- the
	// question it answers ("can any pushed firewall rule produce an event
	// this would match?", #274 item 1) is about an entry's scope, and a
	// detection definition has no equivalent. Omitted rather than sent as
	// "unknown" for everything else, so a caller cannot mistake silence
	// for an answer.
	Coverage engine.CoverageState `json:"coverage,omitempty"`
	// Expectation is the operator-facing entry an expectation definition
	// converts back to -- ports, source, destination, and the
	// observed/permitted state an inverted entry accumulates. Absent for
	// a detection definition. This is what the Watchlist page renders;
	// the raw params it is stored as (JSON-in-a-string lists, see
	// watchlistInvertedParamSchema) are a storage detail no UI should
	// have to decode.
	Expectation *watchlist.Entry `json:"expectation,omitempty"`
}

// replayabilityView is issue #403's contract on the wire: a definition
// either produces a receipt or declares why it never can. Known is false
// only when this binary could not build the definition at all (an id
// from a newer build, an envelope whose params no longer satisfy its own
// schema) -- reported as its own case rather than as "not replayable",
// since "cannot build" and "can never answer" are different facts. See
// engine.ReplayabilityOf.
type replayabilityView struct {
	Known   bool   `json:"known"`
	Capable bool   `json:"capable"`
	Reason  string `json:"reason,omitempty"`
}

// definitionViewFor renders one stored definition. rulesByDevice is the
// pushed filter tables coverage is answered from, read once per request
// by the caller rather than per definition -- see definitionsCoverage.
func definitionViewFor(sd engine.StoredDefinition, rulesByDevice map[string][]ingest.FilterRule, evidenceComplete bool) definitionView {
	d := sd.Definition
	v := definitionView{
		ID:           d.ID,
		Name:         d.Name,
		Description:  d.Description,
		Intent:       d.Intent,
		Kind:         d.Kind,
		Enabled:      d.Enabled,
		Scope:        d.Scope,
		Params:       d.Params,
		ParamSchema:  d.ParamSchema,
		Provenance:   d.Provenance,
		Suppressions: d.Suppressions,
		Available:    sd.Available,
	}
	if !sd.Available {
		// Nothing below can be answered for a definition this binary
		// cannot identify, and guessing would be worse than the empty
		// answer: it is preserved, listed, and never evaluated (see
		// engine.StoredDefinition.Available).
		return v
	}
	if dist := d.Distance(); len(dist) > 0 {
		v.Distance = dist
	}
	capable, reason, known := engine.ReplayabilityOf(d)
	v.Replay = replayabilityView{Known: known, Capable: capable, Reason: reason}

	if d.Intent != engine.IntentExpectation {
		return v
	}
	if entry, err := engine.EntryFromDefinition(d); err == nil {
		v.Expectation = &entry
	}
	v.Coverage = definitionCoverage(d, rulesByDevice, evidenceComplete)
	return v
}

// definitionCoverage applies #367's evidence-completeness downgrade to
// one definition's coverage answer -- unchanged in meaning from the
// watchlist endpoint this replaces, only moved: with an incomplete
// evidence base, both definite negatives degrade to CoverageUnknown,
// which renders as silence rather than as a confident wrong claim.
// CoverageOK is untouched, because one router demonstrably logging the
// right traffic stays true however many others went unread.
func definitionCoverage(d engine.Definition, rulesByDevice map[string][]ingest.FilterRule, evidenceComplete bool) engine.CoverageState {
	state := d.Coverage(rulesByDevice)
	if !evidenceComplete {
		switch state {
		case engine.CoverageNoLogging, engine.CoverageOutOfScope:
			return engine.CoverageUnknown
		}
	}
	return state
}

// coverageEvidence records whether the pushed filter tables coverage is
// answered from can honestly be treated as *every* table that could feed
// an expectation (#367).
//
// Complete is false when at least one device has fed mikroview events but
// has never completed the optional filter-rule state push, so its rules
// were never read. MissingDevices names them, sorted, so the gap is
// visible rather than only implied by a downgraded answer.
type coverageEvidence struct {
	Complete       bool     `json:"complete"`
	MissingDevices []string `json:"missingDevices,omitempty"`
}

// definitionsCoverage reads the pushed filter tables once per request and
// reuses them across every definition, rather than per definition: the
// tables are small but the lock is shared with the ingest path, and this
// runs on every load of the settings pages.
//
// # Why a definite negative needs more than the pushed tables (#367)
//
// engine.Definition.Coverage answers only from the map it is handed, and
// cannot tell "no other router is watching" apart from "another router is
// watching, but its rules are not in this map" -- the map is built here,
// from RouterState, which holds only the routers that completed the
// *optional* filter-rule state push. A router that streams live syslog
// and is actively producing matches, but never pushed, is silently absent
// from it. If some other router happens to have pushed a table where
// nothing logs, coverage then returns CoverageNoLogging -- "no firewall
// rule on any router you have connected has logging turned on" -- for
// every definition, while the excluded router's matches are visible on
// the adjacent live-view page.
//
// coverage.go's own stated rule is that a negative answer "requires every
// relevant rule to have been read and understood." That is a property of
// the *evidence base*, not of any one definition, so it is checked once
// here and applied to every answer -- see definitionCoverage.
func (s *Server) definitionsCoverage() (map[string][]ingest.FilterRule, coverageEvidence) {
	if s.RouterState == nil {
		// Nothing pushed anything, so nothing can be said. Same answer an
		// empty map gives, spelled out here so a nil store does not have
		// to be a special case in the engine.
		return nil, s.coverageEvidenceFor(nil)
	}
	rulesByDevice := make(map[string][]ingest.FilterRule)
	for _, device := range s.RouterState.Devices() {
		if rules, _, ok := s.RouterState.FilterRules(device); ok {
			rulesByDevice[device] = rules
		}
	}
	return rulesByDevice, s.coverageEvidenceFor(rulesByDevice)
}

// coverageEvidenceFor reports whether rulesByDevice covers every device
// that has actually fed mikroview events -- the completeness check
// definitionsCoverage's own doc comment describes (#367).
//
// The event-feeding set comes from the device registry, and only entries
// that have really carried traffic count (EventCount > 0): a device
// declared in config.yaml but silent so far is not evidence of a gap,
// because nothing it could log is arriving anyway. A registry entry
// auto-discovered by source IP has that IP as its ID and so will never
// match a pushed device name, which correctly reads as incomplete -- an
// unregistered router streaming events is exactly the case #367 is about,
// and guessing that it "is probably" one of the pushed routers would be
// the same confident inference this whole area exists to refuse.
//
// A nil registry is likewise treated as incomplete rather than complete:
// with no way to ask which routers are feeding events, "the pushed set is
// all of them" is unknowable, not true.
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

// definitionOrder sorts a list response: this binary's shipped catalogue
// first, in the catalogue's own order, then everything else by name.
//
// The catalogue's order is a property of the binary rather than of what
// happens to be persisted or of map iteration, which is what makes the
// detector settings page's row order stable across restarts and across
// deployments. Everything after it -- an operator's expectations, a
// preserved definition this binary cannot identify -- sorts by name, then
// id, so a list is stable as entries are added and removed.
func definitionOrder(views []definitionView) {
	rank := make(map[string]int)
	for i, id := range engine.ShippedDefinitionIDs() {
		rank[id] = i
	}
	sort.SliceStable(views, func(i, j int) bool {
		ri, oki := rank[views[i].ID]
		rj, okj := rank[views[j].ID]
		switch {
		case oki && okj:
			return ri < rj
		case oki != okj:
			return oki
		case views[i].Name != views[j].Name:
			return views[i].Name < views[j].Name
		default:
			return views[i].ID < views[j].ID
		}
	})
}

// handleDefinitionsList serves every definition this deployment holds --
// shipped detectors, the operator's expectations, and anything preserved
// but unidentifiable -- always all of them, whether or not each has ever
// been edited.
//
// Deliberately one list rather than one endpoint per intent. The two
// endpoints this replaces were exactly that split, and it is what let a
// detector's enabled flag and an entry's scope drift into two answers to
// the same question. A caller wanting only detections filters on intent,
// which is a client concern; the server's job is to have one answer.
func (s *Server) handleDefinitionsList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	rulesByDevice, evidence := s.definitionsCoverage()
	stored := s.Definitions.List()
	out := make([]definitionView, 0, len(stored))
	for _, sd := range stored {
		out = append(out, definitionViewFor(sd, rulesByDevice, evidence.Complete))
	}
	definitionOrder(out)
	writeJSON(w, http.StatusOK, map[string]any{
		"definitions": out,
		// What the coverage answers above were derived from -- surfaced
		// rather than swallowed (#367): when the evidence base is
		// incomplete every negative degrades to "unknown", and this is
		// the only place a caller can see *why* it did.
		"coverageEvidence": evidence,
	})
}

// handleDefinitionsGet serves one definition by id.
func (s *Server) handleDefinitionsGet(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	sd, ok := s.Definitions.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such definition", http.StatusNotFound)
		return
	}
	rulesByDevice, evidence := s.definitionsCoverage()
	writeJSON(w, http.StatusOK, definitionViewFor(sd, rulesByDevice, evidence.Complete))
}

// handleDefinitionsSchema serves every param schema the definitions this
// deployment holds declare, keyed by definition id -- so the UI renders a
// tuning field from the server's own declaration rather than re-listing
// every definition's knobs in TypeScript.
//
// That duplication is the thing this whole epic exists to remove
// (docs/decisions/evaluation-engine.md section 4), and re-creating it one
// layer up in the frontend would be the same mistake with a different
// file extension. The schemas also ride on each definition in the list
// response; this endpoint exists so a caller that only wants to render
// controls does not have to fetch every definition's current state and
// coverage to get them.
func (s *Server) handleDefinitionsSchema(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	schemas := make(map[string][]engine.ParamSchema)
	for _, sd := range s.Definitions.List() {
		if !sd.Available || len(sd.Definition.ParamSchema) == 0 {
			continue
		}
		schemas[sd.Definition.ID] = sd.Definition.ParamSchema
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": schemas})
}

// expectationRequest is the operator-settable half of an expectation
// definition -- deliberately narrower than watchlist.Entry itself.
// Observing, Permitted and Observed are not settable here: Observing
// follows a fixed rule (see createDefinition/updateDefinition below), and
// Permitted/Observed are the observe/promote workflow's own state,
// changed only through their own dedicated endpoints, never by
// overwriting the whole definition -- a plain PUT must not be able to
// silently wipe an entry's accumulated observations.
type expectationRequest struct {
	Source                 matchlog.Identity        `json:"source"`
	SourceList             watchlist.AddressListRef `json:"sourceList"`
	DestIP                 string                   `json:"destIp"`
	Ports                  []int                    `json:"ports"`
	Invert                 bool                     `json:"invert"`
	IncludeStructuralNoise bool                     `json:"includeStructuralNoise"`
}

// applyTo copies req's operator-settable fields onto e in place.
func (req expectationRequest) applyTo(e *watchlist.Entry) {
	e.Source = req.Source
	e.SourceList = req.SourceList
	e.DestIP = req.DestIP
	e.Ports = req.Ports
	e.Invert = req.Invert
	e.IncludeStructuralNoise = req.IncludeStructuralNoise
}

// createDefinitionRequest is the wire shape for POST /api/definitions.
type createDefinitionRequest struct {
	Name        string              `json:"name"`
	Intent      engine.Intent       `json:"intent"`
	Kind        engine.Kind         `json:"kind"`
	Expectation *expectationRequest `json:"expectation"`
}

// ErrProgrammaticIsShippedOnly is the reasoning behind the one creation
// this API refuses outright, stated once so both the handler and its test
// quote the same sentence.
const errProgrammaticIsShippedOnly = "kind=programmatic cannot be created: programmatic logic is Go compiled into this binary, not data, so only a shipped definition can have it (see engine.Kind). A custom definition is always declarative."

// errCustomDetectionNotBuildable is the honest current limit on custom
// definitions, stated at the validation boundary rather than accepted and
// silently never evaluated.
const errCustomDetectionNotBuildable = "intent=detection cannot be created yet: a custom detection definition needs its match conditions stored on the envelope, and the envelope has nowhere to carry them -- the only declarative logic this binary builds from stored data is an expectation's. Accepting one would create a definition that exists, lists, and evaluates nothing, which is the failure this refusal exists to avoid."

// handleDefinitionsCreate creates a custom definition with a
// server-generated ID.
//
// Two refusals here are the API surface of invariants recorded on #401,
// and both are stated rather than silently coerced:
//
//   - kind=programmatic is refused. provenance=custom implies
//     kind=declarative (engine.Definition.Validate), because only
//     mikroview's own code can supply programmatic logic. No request
//     shape may express a custom programmatic definition, and this is
//     where that becomes a 400 rather than a validation error deeper
//     down.
//   - intent=detection is refused, for now, with its reason. See
//     errCustomDetectionNotBuildable.
//
// The stored definition's kind and provenance are decided by the server
// from the expectation's own shape, never by the request: a non-inverted
// expectation is declarative and custom (its matching is conditions over
// an event), an inverted one is programmatic and shipped-labelled (its
// matching is the observed/permitted state machine, which is built-in Go
// -- see convertInvertedEntry's own doc comment on why that label is
// about whose code evaluates it, not whose data it is).
//
// An inverted expectation always starts Observing (#243 section 5: "a new
// inverted entry starts in an observe state") -- not a request field,
// since starting anywhere else would mean shipping traffic decisions
// before the operator has seen any evidence.
func (s *Server) handleDefinitionsCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req createDefinitionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Kind == engine.KindProgrammatic {
		http.Error(w, errProgrammaticIsShippedOnly, http.StatusBadRequest)
		return
	}
	if req.Intent == engine.IntentDetection {
		http.Error(w, errCustomDetectionNotBuildable, http.StatusBadRequest)
		return
	}
	if req.Expectation == nil {
		http.Error(w, "an expectation block is required", http.StatusBadRequest)
		return
	}

	e := watchlist.Entry{ID: newDefinitionEntryID(), Name: req.Name, CreatedAt: time.Now()}
	req.Expectation.applyTo(&e)
	if e.Invert {
		e.Observing = true
	}
	if err := s.Definitions.UpsertExpectation(e); err != nil {
		writeDefinitionError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "definition.create", e.ID, e.Name)
	s.writeDefinition(w, http.StatusCreated, e.ID)
}

// updateDefinitionRequest is the wire shape for PUT
// /api/definitions/{id}. Every field is a pointer (or a nil-able map/
// slice) so an absent field means "leave this alone" rather than "set it
// to the zero value" -- a caller toggling `enabled` must not silently
// clear a definition's scope because it did not send one.
type updateDefinitionRequest struct {
	// Name renames an expectation definition. Refused for a shipped
	// definition: its display name is a property of the binary that
	// ships the logic, not of the deployment -- the same reasoning that
	// keeps kind, intent, schema and provenance untouchable there (see
	// engine.DefinitionsStore.SetParams).
	Name         *string               `json:"name"`
	Enabled      *bool                 `json:"enabled"`
	Scope        *engine.Scope         `json:"scope"`
	Params       engine.Params         `json:"params"`
	Suppressions *[]engine.Suppression `json:"suppressions"`
	Expectation  *expectationRequest   `json:"expectation"`
}

// handleDefinitionsUpdate applies whichever of enabled, scope, params,
// suppressions and expectation data the request actually carries.
//
// Params are validated against this definition's own declared
// ParamSchema before anything is stored (engine.DefinitionsStore.
// SetParams), so an out-of-range threshold is a 400 rather than a stored
// zero read back later as though it were configured.
//
// Raw params are refused for an expectation definition: an expectation's
// params are the encoded form of its entry (ports, source, the
// JSON-in-a-string permitted/observed lists), so writing them directly
// would be a second, unvalidated way to change the same data -- the
// expectation block is the one door.
//
// This takes effect on the very next ingested event, not on the next
// restart: the definitions store notifies its change hook, which
// re-registers the affected definitions on the engine (see main.go's
// registerDefinitions and engine.DefinitionsStore.SetOnChange). That is
// #407's first handover -- detector toggles became restart-effective as
// #405 ported each detector, and this is where next-event effect returns.
func (s *Server) handleDefinitionsUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	sd, ok := s.Definitions.Get(id)
	if !ok {
		http.Error(w, "no such definition", http.StatusNotFound)
		return
	}

	var req updateDefinitionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	isExpectation := sd.Available && sd.Definition.Intent == engine.IntentExpectation
	if req.Params != nil && isExpectation {
		http.Error(w, "an expectation's matching data is edited through the expectation block, not through raw params", http.StatusBadRequest)
		return
	}
	if req.Expectation != nil && !isExpectation {
		http.Error(w, "only an expectation definition takes an expectation block", http.StatusBadRequest)
		return
	}
	if req.Name != nil && !isExpectation {
		http.Error(w, "a shipped definition's name is a property of this binary, not of the deployment, and cannot be renamed", http.StatusBadRequest)
		return
	}

	if req.Enabled != nil || req.Scope != nil {
		enabled := sd.Definition.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		scope := sd.Definition.Scope
		if req.Scope != nil {
			scope = *req.Scope
		}
		if err := engine.ValidateScope(scope); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.Definitions.SetEnabledAndScope(id, enabled, scope); err != nil {
			writeDefinitionError(w, err)
			return
		}
	}
	if req.Params != nil {
		if err := s.Definitions.SetParams(id, req.Params); err != nil {
			writeDefinitionError(w, err)
			return
		}
	}
	if req.Suppressions != nil {
		if err := s.Definitions.SetSuppressions(id, *req.Suppressions); err != nil {
			writeDefinitionError(w, err)
			return
		}
	}
	if req.Expectation != nil || req.Name != nil {
		// Switching a non-inverted expectation to inverted starts it
		// Observing, the same rule create applies -- there is no
		// meaningful permitted set yet, so nothing else is coherent.
		// Switching the other way clears Observing/Permitted/Observed
		// entirely: none of them apply to a non-inverted expectation, and
		// leaving them would be stale data an operator could not see or
		// act on until switched back.
		err := s.Definitions.UpdateExpectation(id, func(e *watchlist.Entry) error {
			if req.Name != nil {
				e.Name = *req.Name
			}
			if req.Expectation == nil {
				return nil
			}
			wasInvert := e.Invert
			req.Expectation.applyTo(e)
			switch {
			case e.Invert && !wasInvert:
				e.Observing = true
			case !e.Invert && wasInvert:
				e.Observing = false
				e.Permitted = nil
				e.Observed = nil
			}
			return nil
		})
		if err != nil {
			writeDefinitionError(w, err)
			return
		}
	}

	s.Audit.Record(auditActor(r), "definition.update", id, definitionAuditDetail(req))
	s.writeDefinition(w, http.StatusOK, id)
}

// definitionAuditDetail summarizes what an update actually changed, so
// the audit row says more than that something was edited.
func definitionAuditDetail(req updateDefinitionRequest) string {
	var parts []string
	if req.Enabled != nil {
		parts = append(parts, fmt.Sprintf("enabled=%t", *req.Enabled))
	}
	if req.Scope != nil {
		parts = append(parts, "scope")
	}
	if req.Params != nil {
		parts = append(parts, "params")
	}
	if req.Suppressions != nil {
		parts = append(parts, fmt.Sprintf("suppressions=%d", len(*req.Suppressions)))
	}
	if req.Name != nil {
		parts = append(parts, "name")
	}
	if req.Expectation != nil {
		parts = append(parts, "expectation")
	}
	if len(parts) == 0 {
		return "no change"
	}
	return joinComma(parts)
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// handleDefinitionsDelete removes a custom definition. A shipped one is
// refused with the reason, not silently ignored: "shipped definitions are
// disabled, never deleted" is a ratified invariant (#401), and an
// operator who asked to delete one has to be told their instance is still
// there and still evaluating, rather than being left to assume otherwise.
//
// If this definition originated from a suggestion (#243 slice 5),
// deleting it here -- through the normal management page, not the
// suggestions table -- sends that candidate straight to Hide rather than
// back to Off: deleting something you explicitly created signals "I don't
// want this," not "reconsider me later" (settled in #243's slice 5 design
// conversation). A no-op when no candidate tracks this definition, which
// is the common case.
func (s *Server) handleDefinitionsDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	sd, ok := s.Definitions.Get(id)
	if !ok {
		http.Error(w, "no such definition", http.StatusNotFound)
		return
	}

	var err error
	if sd.Available && sd.Definition.Intent == engine.IntentExpectation {
		err = s.Definitions.DeleteExpectation(id)
	} else {
		err = s.Definitions.Delete(id)
	}
	if err != nil {
		writeDefinitionError(w, err)
		return
	}
	s.Suggest.MarkHiddenByEntry(id)
	s.Audit.Record(auditActor(r), "definition.delete", id, sd.Definition.Name)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// cloneRequest is the wire shape for POST /api/definitions/{id}/clone.
type cloneRequest struct {
	Name string `json:"name"`
}

// handleDefinitionsClone copies a definition into a new custom one with
// its own identity -- a fresh id, never the original's (see
// engine.Definition.ID's own doc comment on why a clone needs one).
//
// Supported for an expectation definition, which is data all the way
// down: cloning one produces a second entry the operator can then edit.
// Refused, with its reason, for a shipped detection definition -- its
// logic is Go keyed by its own id, so a "clone" of it could only be an
// envelope with no logic behind it: a definition that lists, looks
// configurable, and evaluates nothing. Overriding the original's params
// (PUT) is the operation that actually exists for those, and the refusal
// says so rather than leaving an operator to discover the clone never
// fires.
func (s *Server) handleDefinitionsClone(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	entry, ok, err := s.Definitions.GetExpectation(id)
	if err != nil {
		writeDefinitionError(w, err)
		return
	}
	if !ok {
		if _, exists := s.Definitions.Get(id); !exists {
			http.Error(w, "no such definition", http.StatusNotFound)
			return
		}
		http.Error(w, "a shipped definition cannot be cloned: its logic is compiled into this binary and keyed by its own id, so a copy would evaluate nothing. Override its params instead (PUT /api/definitions/{id}).", http.StatusBadRequest)
		return
	}

	var req cloneRequest
	if r.ContentLength > 0 {
		if err := decodeJSONBody(w, r, &req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}
	clone := entry
	clone.ID = newDefinitionEntryID()
	clone.CreatedAt = time.Now()
	// The observe/promote state is the original's own accumulated
	// evidence about a device, not a property of the expectation being
	// copied -- carrying it over would present observations nothing has
	// actually seen for the clone.
	clone.Observed = nil
	clone.Permitted = nil
	clone.Observing = clone.Invert
	if req.Name != "" {
		clone.Name = req.Name
	} else if clone.Name != "" {
		clone.Name = clone.Name + " (copy)"
	}
	if err := s.Definitions.UpsertExpectation(clone); err != nil {
		writeDefinitionError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "definition.clone", clone.ID, "from "+id)
	s.writeDefinition(w, http.StatusCreated, clone.ID)
}

// handleDefinitionsReset puts a shipped definition's params back to
// exactly what it shipped with. Clearing every override and "resetting to
// default" are the same state, not two operations that have to be kept in
// sync -- see engine.Definition.Distance, which reports an empty map
// afterwards.
func (s *Server) handleDefinitionsReset(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := s.Definitions.ResetParams(id); err != nil {
		writeDefinitionError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "definition.reset", id, "")
	s.writeDefinition(w, http.StatusOK, id)
}

// replayRequest is the wire shape for POST /api/definitions/{id}/replay:
// the candidate params to judge, overriding whichever of the definition's
// own tunable params they name. An absent/empty map replays the
// definition exactly as configured.
type replayRequest struct {
	Params engine.Params `json:"params"`
}

// replayResponse carries exactly one of receipt or decline, mirroring
// engine.Result's own structural either/or -- a caller must handle the
// decline case explicitly rather than reading a short corpus as an
// ordinary receipt with a suspiciously small count.
type replayResponse struct {
	Receipt *receiptView `json:"receipt,omitempty"`
	Decline *declineView `json:"decline,omitempty"`
}

// receiptView is engine.Receipt on the wire. The covered window is
// mandatory and non-omittable here exactly as it is on the type (#403):
// every field of it is spelled out, so a response cannot carry a count
// without the window it was counted over.
type receiptView struct {
	Window          windowView   `json:"window"`
	EmissionCount   int          `json:"emissionCount"`
	Sample          []sampleView `json:"sample"`
	SampleTruncated bool         `json:"sampleTruncated"`
	CorpusTruncated bool         `json:"corpusTruncated"`
	AnyProvisional  bool         `json:"anyProvisional"`
}

type windowView struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Duration   string    `json:"duration"`
	EventCount int       `json:"eventCount"`
}

type sampleView struct {
	At          time.Time `json:"at"`
	Target      string    `json:"target"`
	Detail      string    `json:"detail"`
	Ports       []int     `json:"ports,omitempty"`
	Hosts       []string  `json:"hosts,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	Provisional bool      `json:"provisional"`
}

type declineView struct {
	Reason           string `json:"reason"`
	CorpusSpan       string `json:"corpusSpan"`
	DefinitionWindow string `json:"definitionWindow"`
}

// handleDefinitionsReplay re-runs one definition's own logic over the
// event corpus with candidate params, and returns its receipt -- "what
// would this have done?" answered from evidence rather than from a
// guess (docs/decisions/evaluation-engine.md section 4).
//
// A definition that declares itself non-replayable is a 400 carrying its
// stated reason, never a receipt of zero: "it would have fired zero
// times" and "this question cannot honestly be asked of this definition"
// are different answers, and collapsing them is the overclaim #403's
// contract exists to rule out.
func (s *Server) handleDefinitionsReplay(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	sd, ok := s.Definitions.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such definition", http.StatusNotFound)
		return
	}
	if !sd.Available {
		http.Error(w, "this binary cannot identify that definition, so it cannot replay it", http.StatusBadRequest)
		return
	}
	if s.Store == nil {
		http.Error(w, "no event corpus is available to replay against", http.StatusServiceUnavailable)
		return
	}

	var req replayRequest
	if r.ContentLength > 0 {
		if err := decodeJSONBody(w, r, &req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	result, err := engine.ReplayDefinition(sd.Definition, engine.NewMemoryCorpus(s.Store), req.Params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, replayViewFor(result))
}

func replayViewFor(result engine.Result) replayResponse {
	var out replayResponse
	if d := result.Decline; d != nil {
		out.Decline = &declineView{
			Reason:           d.Reason,
			CorpusSpan:       d.CorpusSpan.String(),
			DefinitionWindow: d.DefinitionWindow.String(),
		}
	}
	if rec := result.Receipt; rec != nil {
		win := rec.Window()
		samples := rec.Sample()
		views := make([]sampleView, 0, len(samples))
		for _, sm := range samples {
			views = append(views, sampleView{
				At: sm.At, Target: sm.Target, Detail: sm.Detail,
				Ports: sm.Ports, Hosts: sm.Hosts, Labels: sm.Labels,
				Provisional: sm.Provisional,
			})
		}
		out.Receipt = &receiptView{
			Window: windowView{
				Start:      win.Start(),
				End:        win.End(),
				Duration:   win.Duration().String(),
				EventCount: win.EventCount(),
			},
			EmissionCount:   rec.EmissionCount(),
			Sample:          views,
			SampleTruncated: rec.SampleTruncated(),
			CorpusTruncated: rec.CorpusTruncated(),
			AnyProvisional:  rec.AnyProvisional(),
		}
	}
	return out
}

// promoteRequest is the wire shape for POST /api/definitions/{id}/promote.
type promoteRequest struct {
	Destinations []watchlist.PermittedDest `json:"destinations"`
}

// handleDefinitionsPromote moves the given destination/port pairs from an
// inverted expectation's observed candidate list into its permitted
// allow-list -- the inverted-expectation action carried over from the
// watchlist, re-expressed against a definition id.
//
// Admin-gated at the same tier as creating the definition: this changes
// what future traffic is treated as expected for a device.
func (s *Server) handleDefinitionsPromote(w http.ResponseWriter, r *http.Request) {
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
	err := s.Definitions.UpdateExpectation(id, func(e *watchlist.Entry) error {
		if !e.Invert {
			return watchlist.ErrNotInverted
		}
		e.Promote(req.Destinations)
		return nil
	})
	if err != nil {
		writeDefinitionError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "definition.promote", id, fmt.Sprintf("%d destination(s)", len(req.Destinations)))
	s.writeDefinition(w, http.StatusOK, id)
}

// setObservingRequest is the wire shape for POST
// /api/definitions/{id}/observing.
type setObservingRequest struct {
	Observing bool `json:"observing"`
}

// handleDefinitionsSetObserving flips whether an inverted expectation is
// in observe mode -- the raw mechanism only: this package (like the
// matching rules themselves) makes no judgement about when an operator
// should call it, #243 open question 3.
func (s *Server) handleDefinitionsSetObserving(w http.ResponseWriter, r *http.Request) {
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
	err := s.Definitions.UpdateExpectation(id, func(e *watchlist.Entry) error {
		if !e.Invert {
			return watchlist.ErrNotInverted
		}
		e.Observing = req.Observing
		return nil
	})
	if err != nil {
		writeDefinitionError(w, err)
		return
	}
	action := "definition.observing.stop"
	if req.Observing {
		action = "definition.observing.start"
	}
	s.Audit.Record(auditActor(r), action, id, "")
	s.writeDefinition(w, http.StatusOK, id)
}

// writeDefinition re-reads a definition and writes it as the response
// body, rather than echoing whatever the handler built. The store assigns
// and normalizes on the way in (params coerced to their canonical types,
// createdAt preserved across an edit), so only the stored copy reflects
// what was actually persisted.
func (s *Server) writeDefinition(w http.ResponseWriter, status int, id string) {
	sd, ok := s.Definitions.Get(id)
	if !ok {
		http.Error(w, "no such definition", http.StatusNotFound)
		return
	}
	rulesByDevice, evidence := s.definitionsCoverage()
	writeJSON(w, status, definitionViewFor(sd, rulesByDevice, evidence.Complete))
}

// writeDefinitionError maps the definitions store's sentinel errors onto
// the status each actually means, so a caller can tell "wrong id" from
// "right id, refused" from "malformed request" without parsing prose:
//
//   - 404: no definition with that id (ErrNoSuchDefinition,
//     watchlist.ErrEntryNotFound).
//   - 409: the id is real and this store refuses to touch it
//     (ErrDefinitionImmutable) -- a shipped definition, or one this
//     binary cannot identify. A refusal about the target, not the
//     request.
//   - 400: everything else -- a param outside its schema's bounds, an
//     entry with no ports, an action that does not apply to this
//     definition.
func writeDefinitionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNoSuchDefinition), errors.Is(err, watchlist.ErrEntryNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, engine.ErrDefinitionImmutable):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
