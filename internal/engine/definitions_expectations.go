// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is issue #407's third handover: internal/watchlist.Store is
// deleted, and the entry set it held lives in the definitions document
// like everything else the engine evaluates.
//
// #406 already made an entry an expectation *definition* -- what stayed
// behind was a second persisted document holding the same entries, read
// by the API and converted on every registration. That is the
// two-sources-of-truth shape docs/decisions/evaluation-engine.md's
// Migration section exists to remove: an entry's enabled flag, its scope
// and its params lived on the definition, while the entry itself lived
// somewhere else, and nothing structurally stopped the two disagreeing.
//
// The conversion is not new. MigrateDefinitions already writes every
// entry into this store (definitions_migrate.go's convertWatchlistEntry),
// and #406's ExpectationDefinitionFor calls the same function so a live
// entry and a migrated one are the same value. What this file adds is
// the other direction -- EntryFromDefinition -- plus the operator-facing
// entry-set API (list, get, upsert, update, delete, reset) and the
// observation recorder, all against the one document.
//
// What is deliberately NOT here: the matching rules. watchlist.Match,
// watchlist.Coverage's successor (coverage.go in this package) and the
// inverted state machine's own predicates stay where #397's
// characterization suite pins them -- see expectation.go's own note on
// why reproducing a pinned rule in new words is a port that has to be
// re-proved for nothing.

// maxExpectations bounds how many expectation definitions this store
// will create -- internal/watchlist.maxEntries, moved with the entry set
// it bounded, at the same value and for the same reason: entries are
// operator-created one at a time through a UI (#243 slice 4), so this is
// a safety net rather than a limit expected to be hit.
var maxExpectations = 10000

// maxObservedPerEntry bounds how many distinct destination/port pairs one
// inverted expectation's Observed candidate list holds -- moved unchanged
// from internal/watchlist.maxObservedPerEntry with RecordObservation
// itself. See #243 open question 7: "an inverted entry in observe state
// would collect enormous volume before anyone promotes anything."
var maxObservedPerEntry = 1000

// observeDropLogInterval rate-limits the "observation capacity reached"
// warning, the same way dropLogInterval rate-limits the chassis's own
// overload log: logging every dropped observation would add load during
// exactly the condition being reported.
const observeDropLogInterval = 30 * time.Second

var (
	droppedObservations atomic.Uint64
	observeDropGate     = logging.NewLimiter(observeDropLogInterval)
)

// ErrNotAnExpectation is returned by every method in this file for an id
// that exists but does not hold an expectation-intent definition --
// distinct from ErrNoSuchDefinition (the id is simply wrong) and from
// ErrDefinitionImmutable (the id is real and this store refuses to touch
// it), so a caller can answer 404, 400 and 409 apart.
var ErrNotAnExpectation = errors.New("engine: that definition is not an expectation")

// EntryFromDefinition is the inverse of ExpectationDefinitionFor: it
// reads an expectation definition's envelope back out as the
// operator-facing watchlist.Entry the UI, the suggestion-accept flow and
// watchlist.Match all still speak.
//
// Kind is what tells the two shapes apart, exactly as
// convertWatchlistEntry writes them (definitions_migrate.go): a
// non-inverted entry converts to KindDeclarative (its matching is
// conditions over an event), an inverted one to KindProgrammatic (its
// matching is the observed/permitted state machine, which is Go). Nothing
// re-derives that from the params.
//
// Params are read through ValidateParams first, never straight off the
// stored map: a definition decoded from the document carries
// json.Unmarshal's shapes (float64, []any), and every reader below
// assumes the normalized Go types -- the same contract paramInt and
// friends document (shipped_declarative.go).
func EntryFromDefinition(d Definition) (watchlist.Entry, error) {
	if d.Intent != IntentExpectation {
		return watchlist.Entry{}, fmt.Errorf("%w: definition %q has intent %q", ErrNotAnExpectation, d.ID, d.Intent)
	}
	params, err := ValidateParams(d.ParamSchema, d.Params)
	if err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}

	e := watchlist.Entry{ID: d.ID, Name: d.Name}
	if e.Source.MAC, err = paramOptionalString(params, "sourceMac"); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if e.Source.IP, err = paramOptionalString(params, "sourceIp"); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	createdAt, err := paramOptionalString(params, "createdAt")
	if err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if createdAt != "" {
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: param \"createdAt\": %w", d.ID, err)
		}
		e.CreatedAt = t
	}

	// The watch window and its nightly history (#680) ride on both
	// shapes: an inverted entry is watched on a schedule the same way a
	// non-inverted one is, so this is read before the split rather than
	// twice after it.
	if err := decodeJSONParam(params, "windowJSON", &e.Window); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if err := decodeJSONParam(params, "nightsJSON", &e.Nights); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if err := decodeJSONParam(params, "ringJSON", &e.Ring); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if err := decodeJSONParam(params, "silentJSON", &e.SilentOccurrences); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}

	if d.Kind == KindProgrammatic {
		e.Invert = true
		if e.IncludeStructuralNoise, err = paramBool(params, "includeStructuralNoise"); err != nil {
			return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
		}
		if e.Observing, err = paramBool(params, "observing"); err != nil {
			return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
		}
		if err := decodeJSONParam(params, "permittedJSON", &e.Permitted); err != nil {
			return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
		}
		if err := decodeJSONParam(params, "observedJSON", &e.Observed); err != nil {
			return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
		}
		return e, nil
	}

	if e.DestIP, err = paramOptionalString(params, "destIp"); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if e.SourceList.Device, err = paramOptionalString(params, "sourceListDevice"); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	if e.SourceList.List, err = paramOptionalString(params, "sourceListList"); err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	ports, err := paramPortList(params, "ports")
	if err != nil {
		return watchlist.Entry{}, fmt.Errorf("engine: expectation %q: %w", d.ID, err)
	}
	e.Ports = ports
	return e, nil
}

// paramBool reads an already-ValidateParams-normalized ParamTypeBool
// value. An absent param is false rather than an error: every bool an
// expectation carries is an opt-in whose absence means "not opted in",
// the same "zero means absent" convention optionalStringList follows on
// the way in (definitions_migrate.go).
func paramBool(params Params, name string) (bool, error) {
	raw, ok := params[name]
	if !ok {
		return false, nil
	}
	v, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("engine: param %q is not a boolean (got %T)", name, raw)
	}
	return v, nil
}

// decodeJSONParam decodes one of the JSON-in-a-string params an inverted
// expectation carries its promoted/observed destination lists in --
// permittedJSON/observedJSON, whose shape has no ParamType (see
// watchlistInvertedParamSchema's own doc comment). An absent or empty
// param leaves out at its zero value.
func decodeJSONParam(params Params, name string, out any) error {
	vs, err := paramStringList(params, name)
	if err != nil {
		return err
	}
	if len(vs) == 0 || vs[0] == "" || vs[0] == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(vs[0]), out); err != nil {
		return fmt.Errorf("param %q: %w", name, err)
	}
	return nil
}

// SetOnChange registers fn to be called after any change to what this
// store's definitions actually evaluate: an upsert, a delete, a reset, an
// enabled/scope/params/suppressions edit, a promotion, an observing
// toggle.
//
// This is what makes an edit take effect on the very next ingested event
// rather than on the next restart (#407's first handover). Definitions are
// built from this store once and registered on the engine -- they do not
// re-read it per event, deliberately, because that is the whole point of
// the dispatch pre-index -- so without a notification an operator's edit
// would sit inert. internal/watchlist.Store.SetOnChange carried exactly
// this contract for the entry set before it was deleted; it now covers
// every definition, which is what returns next-event effect to the
// detector toggles that became restart-effective during #405's port.
//
// RecordObservation deliberately does NOT notify: an observation changes
// only the Observed candidate list, which no matching rule consults
// (matchInverted gates on Permitted), so a rebuild would be pure cost on
// the one path here that runs at event rate.
//
// fn is called with no lock held, so it is free to call back into this
// store, and it runs on whichever goroutine made the change -- an
// operator's request goroutine -- so a slow fn delays that request, not
// evaluation.
func (s *DefinitionsStore) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// notifyChange calls the change hook, if any. Must be called with s.mu
// NOT held -- see SetOnChange.
func (s *DefinitionsStore) notifyChange() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ListExpectations returns every expectation definition as the entry it
// converts back to, sorted by ID for a stable, deterministic order --
// the same contract watchlist.Store.List had, against the one document.
//
// A definition that cannot be read back as an entry is a hard error
// naming it, never a silently dropped entry: an entry that exists but
// evaluates nothing is precisely the "absence of detection presented as
// absence of threat" failure #380's first item describes, and
// BuildExpectations already refuses the same way.
func (s *DefinitionsStore) ListExpectations() ([]watchlist.Entry, error) {
	out := make([]watchlist.Entry, 0)
	for _, sd := range s.List() {
		if !sd.Available || sd.Definition.Intent != IntentExpectation {
			continue
		}
		e, err := EntryFromDefinition(sd.Definition)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// GetExpectation returns the entry stored at id, and whether one exists.
// An id that exists but does not hold an available expectation reports
// false, the same as an unknown id: a caller asking for an entry has
// nothing to do with a detection definition that happens to share the id
// space.
func (s *DefinitionsStore) GetExpectation(id string) (watchlist.Entry, bool, error) {
	sd, ok := s.Get(id)
	if !ok || !sd.Available || sd.Definition.Intent != IntentExpectation {
		return watchlist.Entry{}, false, nil
	}
	e, err := EntryFromDefinition(sd.Definition)
	if err != nil {
		return watchlist.Entry{}, false, err
	}
	return e, true, nil
}

// UpsertExpectation creates or replaces the expectation definition for e,
// setting CreatedAt only on first creation -- an update must not reset
// how long an entry has existed, the same rule watchlist.Store.Upsert
// applied.
//
// This is a second door into the store beside Upsert, for the same reason
// SetEnabledAndScope is one beside it: Upsert refuses to replace a
// shipped-provenance definition wholesale, and an *inverted* expectation
// is shipped-provenance by design -- its evaluating logic is built-in Go
// (see convertInvertedEntry's own doc comment on why). That label is
// about whose *code* evaluates the entry, never about whose *data* it is:
// an operator authored it through the watchlist UI and has always been
// able to edit and delete it. Refusing here would make the shipped label
// mean something it was never intended to mean.
//
// What the label does still protect is this binary's own catalogue: an id
// in ShippedDefinitionIDs is refused outright, so nothing reachable from
// the API can overwrite a shipped detector by naming its id, and the
// "shipped definitions are disabled-never-deleted" invariant (#401) holds
// exactly where it was written to hold.
//
// Enabled, Scope, Suppressions and Description are carried over from the
// existing definition when one exists: they are envelope properties an
// operator sets through the definitions API, not properties of the entry,
// and an entry edit must not silently reset them.
func (s *DefinitionsStore) UpsertExpectation(e watchlist.Entry) error {
	if err := watchlist.ValidateEntry(e); err != nil {
		return err
	}
	if err := s.upsertExpectationLocking(e); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *DefinitionsStore) upsertExpectationLocking(e watchlist.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeExpectationLocked(e, true)
}

// writeExpectationLocked converts e and writes it into s.raw. countAsNew
// is false for a mutation of an entry already known to exist (see
// updateExpectationLocked), so the entry-limit check only ever runs where
// a new entry could actually be created. Must be called with s.mu held.
func (s *DefinitionsStore) writeExpectationLocked(e watchlist.Entry, countAsNew bool) error {
	if IsShippedDefinitionID(e.ID) {
		return fmt.Errorf("%w: %q is a shipped definition's id", ErrDefinitionImmutable, e.ID)
	}

	existing, exists := s.raw[e.ID]
	var previous Definition
	if exists {
		sd := decodeStored(e.ID, existing)
		if !sd.Available {
			return fmt.Errorf("%w: %q is a definition this binary cannot identify -- refusing to overwrite it (see StoredDefinition.Available)", ErrDefinitionImmutable, e.ID)
		}
		if sd.Definition.Intent != IntentExpectation {
			return fmt.Errorf("%w: %q holds a %s definition", ErrNotAnExpectation, e.ID, sd.Definition.Intent)
		}
		previous = sd.Definition
	} else if countAsNew {
		if s.expectationCountLocked() >= maxExpectations {
			return fmt.Errorf("engine: at the %d-expectation limit", maxExpectations)
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now()
		}
	}
	if exists {
		e.CreatedAt = previousCreatedAt(previous, e.CreatedAt)
	}

	def, err := ExpectationDefinitionFor(e)
	if err != nil {
		return err
	}
	if exists {
		// Envelope properties the entry does not carry -- see
		// UpsertExpectation's own doc comment.
		def.Enabled = previous.Enabled
		def.Scope = previous.Scope
		def.Suppressions = previous.Suppressions
		if previous.Description != "" {
			def.Description = previous.Description
		}
	}
	if err := def.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("engine: encoding expectation %q: %w", e.ID, err)
	}
	s.raw[e.ID] = raw
	s.persistLocked()
	return nil
}

// previousCreatedAt keeps an existing expectation's creation time across
// an edit -- read back out of the stored definition rather than trusted
// from the request, since createdAt is a param a caller could otherwise
// rewrite by hand.
func previousCreatedAt(previous Definition, fallback time.Time) time.Time {
	prior, err := EntryFromDefinition(previous)
	if err != nil || prior.CreatedAt.IsZero() {
		return fallback
	}
	return prior.CreatedAt
}

// expectationCountLocked counts stored expectation definitions. Must be
// called with s.mu held.
func (s *DefinitionsStore) expectationCountLocked() int {
	n := 0
	for id, entry := range s.raw {
		sd := decodeStored(id, entry)
		if sd.Available && sd.Definition.Intent == IntentExpectation {
			n++
		}
	}
	return n
}

// UpdateExpectation applies mutate to the entry stored at id and writes
// the result back, under one lock -- the read-modify-write door every
// operator action that changes part of an entry goes through (promote,
// observing, the API's own PUT), so none of them has to re-implement the
// decode/convert/encode round trip or race another one halfway through
// it.
//
// mutate may not change the entry's ID; doing so is refused rather than
// silently creating a second entry.
func (s *DefinitionsStore) UpdateExpectation(id string, mutate func(*watchlist.Entry) error) error {
	if err := s.updateExpectationLocking(id, mutate); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *DefinitionsStore) updateExpectationLocking(id string, mutate func(*watchlist.Entry) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.raw[id]
	if !ok {
		return fmt.Errorf("%w: %q", watchlist.ErrEntryNotFound, id)
	}
	sd := decodeStored(id, entry)
	if !sd.Available || sd.Definition.Intent != IntentExpectation {
		return fmt.Errorf("%w: %q", ErrNotAnExpectation, id)
	}
	e, err := EntryFromDefinition(sd.Definition)
	if err != nil {
		return err
	}
	if err := mutate(&e); err != nil {
		return err
	}
	if e.ID != id {
		return fmt.Errorf("engine: an expectation's id may not change (%q -> %q)", id, e.ID)
	}
	if err := watchlist.ValidateEntry(e); err != nil {
		return err
	}
	return s.writeExpectationLocked(e, false)
}

// DeleteExpectation removes the expectation definition at id. Deleting an
// id that does not exist is a no-op, not an error -- the caller's intent
// (this entry should not be in the store) is already satisfied, the same
// convention Delete and watchlist.Store.Delete both follow. Refuses an id
// that holds something other than an expectation, rather than falling
// through to Delete's own shipped/unavailable refusal with a confusing
// message.
func (s *DefinitionsStore) DeleteExpectation(id string) error {
	deleted, err := s.deleteExpectationLocking(id)
	if err != nil || !deleted {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *DefinitionsStore) deleteExpectationLocking(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.raw[id]
	if !ok {
		return false, nil
	}
	sd := decodeStored(id, entry)
	if !sd.Available || sd.Definition.Intent != IntentExpectation {
		return false, fmt.Errorf("%w: %q", ErrNotAnExpectation, id)
	}
	delete(s.raw, id)
	s.persistLocked()
	return true, nil
}

// ResetExpectations removes every expectation definition and reports how
// many were removed -- the watchlist half of #243 slice 5's "nuke"
// action, moved onto this store with the entry set. Detection definitions
// are untouched: this wipes the operator's own entries, not the shipped
// catalogue.
//
// The suggestion candidate tracking (internal/suggest) is a separate
// store; the caller (internal/api) is responsible for wiping both
// together, since nuking one without the other would leave every
// candidate pointing at an EntryID that no longer exists.
func (s *DefinitionsStore) ResetExpectations() int {
	n := s.resetExpectationsLocking()
	if n > 0 {
		s.notifyChange()
	}
	return n
}

func (s *DefinitionsStore) resetExpectationsLocking() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	for id, entry := range s.raw {
		sd := decodeStored(id, entry)
		if !sd.Available || sd.Definition.Intent != IntentExpectation {
			continue
		}
		delete(s.raw, id)
		n++
	}
	if n > 0 {
		s.persistLocked()
	}
	return n
}

// RecordObservation upserts (or bumps) an observed candidate for the
// inverted expectation id -- ObservationRecorder (expectation.go), called
// from the engine's inverted expectation definition on every Observed
// outcome. Moved from watchlist.Store.RecordObservation with its
// behaviour intact, including the two things that behaviour is careful
// about:
//
//   - It silently no-ops for an unknown, non-expectation or non-inverted
//     id rather than erroring. This runs on the evaluation goroutine,
//     which has no reasonable action to take on an error, and an entry
//     that was inverted when Match ran but was edited a moment later is a
//     real, harmless race.
//   - A repeat of an already-observed pair updates LastSeen/Count in the
//     stored definition but does NOT persist. persistLocked rewrites the
//     whole document, and a busy device in observe mode can repeat-match
//     the same destination at the ingest rate; persisting every bump
//     would mean a full-document rewrite per event. The trade-off is
//     unchanged from the store this replaces: an unclean shutdown can
//     lose a repeat's latest Count/LastSeen, never the fact that the
//     destination was seen at all.
//
// Once the Observed list is at maxObservedPerEntry, a genuinely new pair
// is dropped rather than growing without bound -- logged, rate-limited.
func (s *DefinitionsStore) RecordObservation(id, destIP string, port int, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.raw[id]
	if !ok {
		return
	}
	sd := decodeStored(id, entry)
	if !sd.Available || sd.Definition.Intent != IntentExpectation || sd.Definition.Kind != KindProgrammatic {
		return
	}
	e, err := EntryFromDefinition(sd.Definition)
	if err != nil || !e.Invert {
		return
	}

	isNew := true
	for i := range e.Observed {
		if e.Observed[i].DestIP == destIP && e.Observed[i].Port == port {
			e.Observed[i].LastSeen = t
			e.Observed[i].Count++
			isNew = false
			break
		}
	}
	if isNew {
		if len(e.Observed) >= maxObservedPerEntry {
			recordDroppedObservation()
			return
		}
		e.Observed = append(e.Observed, watchlist.ObservedDest{DestIP: destIP, Port: port, FirstSeen: t, LastSeen: t, Count: 1})
	}

	def := sd.Definition
	if err := setObservedParam(&def, e.Observed); err != nil {
		definitionsLog.Error(fmt.Sprintf("expectation %q: encoding an observation failed: %v -- the observation is lost", id, err))
		return
	}
	raw, err := json.Marshal(def)
	if err != nil {
		definitionsLog.Error(fmt.Sprintf("expectation %q: encoding the definition failed: %v -- the observation is lost", id, err))
		return
	}
	s.raw[id] = raw
	if isNew {
		s.persistLocked()
	}
}

// setObservedParam writes observed back into d's observedJSON param --
// the JSON-in-a-string shape watchlistInvertedParamSchema declares.
func setObservedParam(d *Definition, observed []watchlist.ObservedDest) error {
	encoded, err := json.Marshal(observed)
	if err != nil {
		return err
	}
	params := make(Params, len(d.Params))
	for k, v := range d.Params {
		params[k] = v
	}
	params["observedJSON"] = []string{string(encoded)}
	d.Params = params
	return nil
}

// recordDroppedObservation logs the capacity warning, rate-limited.
func recordDroppedObservation() {
	total := droppedObservations.Add(1)
	if _, ok := observeDropGate.Allow(); ok {
		definitionsLog.Warn(fmt.Sprintf("expectation observation capacity reached -- %d new destination(s) not recorded so far (existing observations still update normally)", total))
	}
}
