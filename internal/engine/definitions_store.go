// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var definitionsLog = logging.New("definitions")

// definitionsDocumentVersion is definitionsDocument's current shape
// version -- the "document version bump" docs/decisions/evaluation-
// engine.md's Migration section calls for: this is a brand new document
// (nothing before it shared this shape), versioned from its first byte
// so a future *structural* change has somewhere to record itself, the
// same "versioned, forward-growable" contract engine.stateDocument
// already follows (see state.go's own doc comment).
const definitionsDocumentVersion = 1

// definitionsDocument is DefinitionsStore's on-disk shape: every
// definition keyed by its ID, carried as raw JSON rather than decoded
// into Definition at this layer.
//
// That choice is the whole mechanism behind the unavailable-definition
// guarantee (see StoredDefinition.Available's doc comment, and the
// decision recorded on issue #404, 2026-08-16): a definition this binary
// cannot make sense of -- an unrecognized Kind/Intent from a newer
// version, hit on a downgrade, or a shipped definition this binary has
// retired -- still round-trips through Load/Save exactly, because this
// layer never asks json.RawMessage to understand it. Only an entry a
// caller actually rewrites (DefinitionsStore.Upsert) ever has its bytes
// replaced; every other entry is carried forward unexamined.
type definitionsDocument struct {
	Version     int                        `json:"version"`
	Definitions map[string]json.RawMessage `json:"definitions"`
}

// definitionsPersistMinInterval rate-limits the write-behind writer's
// actual backend attempts -- same reasoning and same value as
// detect.settingsPersistMinInterval/watchlist.watchlistPersistMinInterval:
// this store's mutations are admin-interactive (a settings toggle, a
// builder-UI save), not a hot path, so a short interval coalesces a
// burst of edits without meaningfully delaying anything a human is
// watching. A var so a test that needs every call to persist immediately
// can shrink it, same convention as every other persist interval in this
// codebase.
var definitionsPersistMinInterval = 200 * time.Millisecond

// DefinitionsStore holds every definition mikroview knows about -- the
// one document docs/decisions/evaluation-engine.md's Migration section
// describes: shipped detectors, an operator's watchlist expectations,
// and (once #407's API exists) anything a builder UI authors from
// scratch. Per-definition settings (enabled, scope, params, provenance,
// suppressions) all ride inside the envelope (see Definition) -- there
// is deliberately no second keyed document alongside this one, unlike
// internal/detect.SettingsStore/internal/watchlist.Store today.
//
// Opens fail-closed via persist.WriteBehind/persist.Open (#378), like
// every other store in this codebase: an unreadable document refuses to
// start rather than silently beginning cold. The zero value is not
// usable; construct with OpenDefinitionsStore or
// OpenDefinitionsStoreWithBackend.
type DefinitionsStore struct {
	mu sync.RWMutex
	// wb is nil when persistence isn't configured -- see
	// persist.WriteBehind. Every method on it is a safe no-op on a nil
	// receiver.
	wb *persist.WriteBehind
	// raw is the canonical, last-known-good bytes for every definition,
	// keyed by ID -- see definitionsDocument's own doc comment for why
	// this layer stores bytes rather than decoded Definition values.
	raw map[string]json.RawMessage

	// watchingSince is when this process opened the store, and so the
	// earliest watch window it could have observed end to end. Read by
	// the nightly fill (definitions_nights.go) to keep a night nobody was
	// running for out of the "empty" bucket -- see
	// watchlist.Observation.
	watchingSince time.Time

	// onChange is notified after any change to what this store's
	// definitions evaluate -- see SetOnChange
	// (definitions_expectations.go). Guarded by mu for writes, read under
	// mu and then called outside it.
	onChange func()
}

// OpenDefinitionsStore loads path if it exists (a missing file is the
// expected first-run case, not an error) and returns a store that
// persists to it from then on. An empty path is the expected
// "persistence not configured" case: a fully usable, in-memory-only
// store is returned. A document that exists but cannot be read or
// parsed is a hard error (issue #378): the caller gets (nil, err) rather
// than a store whose live backend would overwrite that document on the
// first write. See persist.Open.
func OpenDefinitionsStore(path string) (*DefinitionsStore, error) {
	if path == "" {
		return OpenDefinitionsStoreWithBackend(nil)
	}
	return OpenDefinitionsStoreWithBackend(persist.NewFileBackend(path))
}

// OpenDefinitionsStoreWithBackend is OpenDefinitionsStore against any
// persist.Backend -- a JSON file by default, or Postgres when
// configured. Deliberately seeds nothing: filling an empty store with
// this binary's shipped catalogue is SeedShippedDefinitions' job
// (definitions_convert.go), run on every boot once this store is open.
func OpenDefinitionsStoreWithBackend(b persist.Backend) (*DefinitionsStore, error) {
	s := &DefinitionsStore{raw: make(map[string]json.RawMessage), watchingSince: time.Now()}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the definitions store", persist.WriteBehindOptions{
		MinInterval: definitionsPersistMinInterval,
		OnSaveError: func(msg string) { definitionsLog.Error(msg) },
		OnConflict:  func(msg string) { definitionsLog.Warn(msg) },
	}, func(data []byte) error {
		var doc definitionsDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		for id, entry := range doc.Definitions {
			// A JSON object containing `null` for one key is
			// syntactically valid and unmarshals to a nil RawMessage --
			// skipped here so a malformed entry can't produce an empty
			// definition later (same guard every sibling store applies
			// to a nil slice element).
			if id == "" || len(entry) == 0 {
				continue
			}
			s.raw[id] = entry
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// Flush forces this store's write-behind writer to persist whatever is
// currently dirty now, without waiting out its usual debounce interval,
// and blocks until that attempt finishes or ctx expires -- see
// flags.Store.Flush's own doc comment for when this is the right call (a
// test, or a `-backup` CLI invocation racing a still-running process). A
// store with no backend configured (wb == nil) is a safe no-op.
func (s *DefinitionsStore) Flush(ctx context.Context) error {
	return s.wb.Flush(ctx)
}

// Close stops this store's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A store with no backend configured (wb == nil)
// is a safe no-op. Not safe to call a mutating method after Close.
func (s *DefinitionsStore) Close(ctx context.Context) error {
	return s.wb.Close(ctx)
}

// StoredDefinition is one definition as handed out by this store's read
// API: the decoded envelope, plus whether this binary can make sense of
// it.
//
// Available is false when Kind or Intent is not one this binary's
// engine package recognizes -- the "a stored definition whose id/kind
// this binary cannot service" case decided on issue #404 (2026-08-16):
// a downgrade from a version that shipped a Kind this binary predates,
// or a shipped definition this binary has since retired. Deliberately a
// *weaker* check than Definition.Validate (which also enforces
// ParamSchema/Params agreement and the custom-implies-declarative
// invariant): those are API-boundary concerns for whoever accepts a
// live edit (#407), not this store's own "can I identify what kind of
// thing this is" question -- a definition can be fully Available here
// and still fail Validate if, say, its ParamSchema has drifted from a
// value it no longer describes, which is a different problem from not
// knowing what it is at all.
//
// An unavailable definition is never evaluated (nothing in this package
// dispatches on an unrecognized Kind), but it is never dropped either:
// Get and List still return it, and DefinitionsStore never rewrites or
// removes its stored bytes except in response to an explicit, targeted
// mutation of that same ID -- which Upsert/Delete both refuse for an
// unavailable entry, conservatively, since this binary cannot confirm
// what replacing or discarding it would actually do. See
// TestDefinitionsStorePreservesUnknownDefinitionByteForByte.
type StoredDefinition struct {
	Definition Definition
	Available  bool
}

// decodeStored decodes one entry's raw bytes and classifies it -- shared
// by Get, List and the availability checks Upsert/Delete perform. A
// decode failure here should not happen in practice: the only paths that
// ever populate s.raw are OpenDefinitionsStoreWithBackend's own decode
// closure (which already round-tripped this exact JSON once) and Upsert
// (which marshals a value this package produced) -- but a defensive
// fallback still classifies as unavailable rather than panicking, should
// disk-level corruption ever slip past those layers.
func decodeStored(id string, entry json.RawMessage) StoredDefinition {
	var d Definition
	if err := json.Unmarshal(entry, &d); err != nil {
		definitionsLog.Error(fmt.Sprintf("definition %q: stored document did not decode as a definition envelope: %v -- surfacing as unavailable rather than dropping it", id, err))
		return StoredDefinition{Definition: Definition{ID: id}, Available: false}
	}
	available := true
	switch d.Kind {
	case KindDeclarative, KindProgrammatic:
	default:
		available = false
	}
	switch d.Intent {
	case IntentDetection, IntentExpectation:
	default:
		available = false
	}
	// A custom detection carries its own logic in its Detection block
	// (issue #502), so "can this binary make sense of it" extends to
	// that block: a condition field, an operator or a key mode from a
	// newer build is exactly the downgrade case Available already
	// exists for. Marking it unavailable shelves the one definition --
	// preserved byte-for-byte, listed, never evaluated, and refused for
	// edit or delete -- where leaving it available would instead have it
	// fail to build on every single sync. Not logged: this runs on every
	// Get and List.
	if available && d.Provenance.Origin == ProvenanceCustom && d.Intent == IntentDetection {
		if err := d.Detection.Validate(); err != nil {
			available = false
		}
	}
	return StoredDefinition{Definition: d, Available: available}
}

// Get returns the definition stored at id, and whether one exists.
//
// The returned Definition is decoded fresh from this store's own copy of
// its bytes on every call -- the copy-on-read contract docs/decisions/
// evaluation-engine.md states once for this package: nothing in the
// result shares backing storage with s.raw, so a caller mutating its own
// copy (a slice append, a map write) can never reach back into this
// store's state.
func (s *DefinitionsStore) Get(id string) (StoredDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.raw[id]
	if !ok {
		return StoredDefinition{}, false
	}
	return decodeStored(id, entry), true
}

// List returns every definition currently stored, available and
// unavailable alike, sorted by ID for a stable, deterministic order --
// same "sort in List, skip it on the hot internal path" split
// watchlist.Store.List/entriesSnapshot uses, kept here even though
// nothing in this package has a hot path over these yet (#405/#406's
// job), for the same reason: a caller comparing two List results, or
// paging through them, should see a stable order.
func (s *DefinitionsStore) List() []StoredDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]StoredDefinition, 0, len(s.raw))
	for id, entry := range s.raw {
		out = append(out, decodeStored(id, entry))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Definition.ID < out[j].Definition.ID })
	return out
}

// ErrDefinitionImmutable is returned by Upsert/Delete when a caller asks
// to overwrite or remove a definition this store refuses to touch: a
// shipped definition (Definition's own doc comment: "Shipped definitions
// are never deleted, only ever disabled -- there is no Delete operation
// on this envelope -- #404's store is what enforces this"), or a
// definition this binary cannot identify at all (StoredDefinition.
// Available == false) -- conservatively refused for the same reason an
// unavailable entry is never dropped: this binary cannot confirm what a
// blind overwrite or delete of it would discard.
var ErrDefinitionImmutable = errors.New("engine: this definition cannot be modified or deleted through this store")

// Upsert creates or replaces the definition at d.ID. d must validate
// (Definition.Validate) and must not collide with an existing shipped or
// unavailable definition at the same ID -- see ErrDefinitionImmutable.
// Encodes and persists on success; a store with no backend configured
// keeps the change in memory only, same optional-persistence contract as
// every other store here.
func (s *DefinitionsStore) Upsert(d Definition) error {
	if err := s.upsertLocking(d); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

// upsertLocking is Upsert's critical section, split out so notifyChange
// runs after the lock is released -- see SetOnChange.
func (s *DefinitionsStore) upsertLocking(d Definition) error {
	if d.ID == "" {
		return fmt.Errorf("engine: a definition must have an id")
	}
	if err := d.Validate(); err != nil {
		return err
	}
	// A stored definition is rebuilt from its bytes alone, so a custom
	// detection has to carry its own structure: without a Detection
	// block there is nowhere for its match conditions to live, and it
	// would store, list and evaluate nothing. Refused here rather than
	// on the envelope because an in-process definition is handed its
	// structure directly (see Definition.validateDetectionBlock), and
	// persistence is what makes the block load-bearing.
	if d.Provenance.Origin == ProvenanceCustom && d.Intent == IntentDetection && d.Detection == nil {
		return fmt.Errorf("engine: definition %q: a stored custom detection requires a detection block to carry its match conditions", d.ID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.raw[d.ID]; ok {
		if err := refuseIfImmutable(d.ID, existing); err != nil {
			return err
		}
	}

	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("engine: encoding definition %q: %w", d.ID, err)
	}
	s.raw[d.ID] = raw
	s.persistLocked()
	return nil
}

// SetEnabledAndScope changes only the two fields an operator is allowed
// to change on any definition, shipped included -- what
// ErrDefinitionImmutable's own message means by "disable it instead
// (Enabled=false)". Until issue #405 there was no way to actually do
// that: Upsert refused a shipped definition wholesale, so the advice the
// error gave could not be followed. This is the narrow door that advice
// always implied.
//
// Narrow deliberately. Params, kind, intent, schema and provenance are
// untouchable on a shipped definition because they are properties of the
// binary, not of the deployment -- a shipped definition whose params
// were replaced wholesale would silently stop matching the Go logic
// built for it. Enabled and Scope are the two the ADR names as
// operator-owned on every definition ("enabled, scope, its typed
// params"), and they are the two internal/detect's settings store
// carried before it was deleted, which is what this replaces.
//
// Returns ErrNoSuchDefinition for an unknown id and ErrDefinitionImmutable
// for one this binary cannot identify (see StoredDefinition.Available):
// toggling something whose meaning is unknown is exactly as unsafe as
// overwriting it.
func (s *DefinitionsStore) SetEnabledAndScope(id string, enabled bool, scope Scope) error {
	if err := ValidateScope(scope); err != nil {
		return err
	}
	return s.mutate(id, func(d *Definition) error {
		d.Enabled = enabled
		d.Scope = scope
		return nil
	})
}

// SetParams replaces a definition's params wholesale, validated against
// that definition's own declared ParamSchema -- the param-override door
// docs/decisions/evaluation-engine.md section 4 describes ("every
// definition exposes, uniformly: enabled, scope, its typed params"), and
// the reason an out-of-bounds value from the API is a 400 rather than a
// stored zero (#407): ValidateParams runs here, on the way in, so a
// rejected value never reaches the document at all.
//
// Open to a shipped definition as well as a custom one, deliberately and
// narrowly: overriding a shipped definition's declared params is exactly
// what "editing a shipped definition keeps it shipped, with overrides"
// means (#401's ratified invariant), and Provenance.ShippedParams is
// untouched by this, so Distance -- and therefore Reset -- keeps
// answering from the values the binary actually shipped rather than from
// whatever was last written. What stays refused is replacing a shipped
// definition's kind, intent, schema or provenance (see Upsert): those are
// properties of the binary, not of the deployment.
func (s *DefinitionsStore) SetParams(id string, params Params) error {
	return s.mutate(id, func(d *Definition) error {
		normalized, err := ValidateParams(d.ParamSchema, params)
		if err != nil {
			return err
		}
		d.Params = normalized
		return nil
	})
}

// ErrNoShippedDefaults is returned by ResetParams for a definition with
// nothing to reset to -- a custom definition, which has no stock to diff
// against (see Definition.Distance's own doc comment).
var ErrNoShippedDefaults = errors.New("engine: this definition has no shipped defaults to reset to")

// SetName renames a custom definition.
//
// The narrow door for the one field an operator owns on a definition they
// authored themselves, in the same shape as SetEnabledAndScope: through
// mutate, so it inherits the unavailable-definition refusal, the
// identity-change refusal and the validate-before-write round trip rather
// than re-implementing any of them.
//
// Refused for a shipped definition. Its display name is a property of the
// binary that ships the logic, not of the deployment -- the same reasoning
// that keeps kind, intent, schema and provenance untouchable there.
//
// An expectation has its own rename path (UpdateExpectation), because its
// name lives on the watchlist entry it converts back to; this is for
// everything else custom, which since issue #502 means operator-authored
// detections.
//
// Note that renaming rebuilds the definition, so a detector's in-flight
// counting window starts again: Registry.Sync carries live state forward
// only for a definition whose stored bytes did not change. That is already
// true of every other edit, tuning a threshold included.
func (s *DefinitionsStore) SetName(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("engine: definition %q: a name may not be empty", id)
	}
	return s.mutate(id, func(d *Definition) error {
		if d.Provenance.Origin != ProvenanceCustom {
			return fmt.Errorf("%w: %q is shipped, and its name is a property of this binary rather than of the deployment", ErrDefinitionImmutable, id)
		}
		d.Name = name
		return nil
	})
}

// ResetParams puts a shipped definition's params back to exactly the
// values it shipped with (Provenance.ShippedParams), which is what makes
// "reset to default" and "clear every override" the same state rather
// than two operations that could fall out of sync -- see
// Definition.Distance, which reports an empty map afterwards.
func (s *DefinitionsStore) ResetParams(id string) error {
	return s.mutate(id, func(d *Definition) error {
		if d.Provenance.Origin != ProvenanceShipped || len(d.Provenance.ShippedParams) == 0 {
			return fmt.Errorf("%w: %q", ErrNoShippedDefaults, id)
		}
		reset := make(Params, len(d.Provenance.ShippedParams))
		for k, v := range d.Provenance.ShippedParams {
			reset[k] = v
		}
		d.Params = reset
		return nil
	})
}

// mutate applies fn to the definition stored at id and writes the result
// back, under one lock -- the shared read-modify-write door every narrow
// mutator above goes through, so none of them re-implements the
// decode/validate/encode round trip or races another one halfway through
// it. Notifies the change hook after the lock is released.
//
// fn may not change the definition's ID, Kind, Intent, ParamSchema or
// Provenance: those are what an id *is*, and a mutator that could change
// them would be Upsert with the immutability check bypassed. Refused
// rather than silently ignored.
func (s *DefinitionsStore) mutate(id string, fn func(*Definition) error) error {
	if err := s.mutateLocking(id, fn); err != nil {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *DefinitionsStore) mutateLocking(id string, fn func(*Definition) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.raw[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoSuchDefinition, id)
	}
	sd := decodeStored(id, existing)
	if !sd.Available {
		return fmt.Errorf("%w: %q is a definition this binary cannot identify -- refusing to edit something whose meaning it cannot read (see StoredDefinition.Available)", ErrDefinitionImmutable, id)
	}

	updated := sd.Definition
	if err := fn(&updated); err != nil {
		return err
	}
	if err := refuseIdentityChange(sd.Definition, updated); err != nil {
		return err
	}
	if err := updated.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(updated)
	if err != nil {
		return fmt.Errorf("engine: encoding definition %q: %w", id, err)
	}
	s.raw[id] = raw
	s.persistLocked()
	return nil
}

// refuseIdentityChange reports an error when a mutator changed one of the
// fields that decide what a definition *is* -- see mutate.
func refuseIdentityChange(before, after Definition) error {
	switch {
	case before.ID != after.ID:
		return fmt.Errorf("engine: a definition's id may not change (%q -> %q)", before.ID, after.ID)
	case before.Kind != after.Kind:
		return fmt.Errorf("engine: definition %q: kind may not change (%q -> %q)", before.ID, before.Kind, after.Kind)
	case before.Intent != after.Intent:
		return fmt.Errorf("engine: definition %q: intent may not change (%q -> %q)", before.ID, before.Intent, after.Intent)
	case before.Provenance.Origin != after.Provenance.Origin:
		return fmt.Errorf("engine: definition %q: provenance may not change (%q -> %q)", before.ID, before.Provenance.Origin, after.Provenance.Origin)
	}
	return nil
}

// ErrNoSuchDefinition is returned by SetEnabledAndScope for an id this
// store does not hold -- distinct from ErrDefinitionImmutable so a
// caller (see internal/api) can answer 404 rather than 409.
var ErrNoSuchDefinition = errors.New("engine: no such definition")

// Delete removes the definition at id. Deleting an ID that doesn't exist
// is a no-op, not an error -- the caller's intent (this ID should not be
// in the store) is already satisfied, same convention as
// watchlist.Store.Delete. Refuses (ErrDefinitionImmutable) for a shipped
// or unavailable definition -- see that error's own doc comment.
func (s *DefinitionsStore) Delete(id string) error {
	deleted, err := s.deleteLocking(id)
	if err != nil || !deleted {
		return err
	}
	s.notifyChange()
	return nil
}

func (s *DefinitionsStore) deleteLocking(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.raw[id]
	if !ok {
		return false, nil
	}
	if err := refuseIfImmutable(id, existing); err != nil {
		return false, err
	}
	delete(s.raw, id)
	s.persistLocked()
	return true, nil
}

// refuseIfImmutable reports ErrDefinitionImmutable when existing decodes
// to a shipped or unavailable definition -- shared by Upsert (refusing
// to overwrite one) and Delete (refusing to remove one). Must be called
// with s.mu already held.
func refuseIfImmutable(id string, existing json.RawMessage) error {
	sd := decodeStored(id, existing)
	if !sd.Available {
		return fmt.Errorf("%w: %q is a definition this binary cannot identify -- refusing to overwrite or discard it rather than risk losing operator-authored state it cannot decode (see StoredDefinition.Available)", ErrDefinitionImmutable, id)
	}
	if sd.Definition.Provenance.Origin == ProvenanceShipped {
		return fmt.Errorf("%w: %q is a shipped definition -- disable it instead (Enabled=false); shipped definitions are never deleted or replaced wholesale", ErrDefinitionImmutable, id)
	}
	return nil
}

// persistLocked encodes the current document and hands it to the
// write-behind writer (see persist.WriteBehind), which coalesces it with
// whatever else is pending and persists it off this goroutine, under its
// own deadline and rate limit. Marshal failures are swallowed rather
// than surfaced to the caller: the in-memory state (which every read
// goes through) stays correct either way, so a transient encoding issue
// degrades to "won't survive a restart right now" rather than breaking
// live use -- same contract every sibling store's persistLocked
// documents. Must be called with s.mu already held.
//
// json.MarshalIndent re-tokenizes (compacts, then re-indents) every
// embedded json.RawMessage's bytes as part of producing readable
// top-level formatting -- so an untouched entry's on-disk *whitespace*
// can change across a persist that touched a different entry, even
// though its JSON *value* (every key, every value, key order) never
// does, because MarshalIndent never reparses object key order, only
// re-inserts insignificant whitespace. That is what "round-trips
// byte-for-byte" means for an unavailable definition in this package's
// tests: the value survives untouched; incidental whitespace
// normalization across the whole document is not a mutation of it.
func (s *DefinitionsStore) persistLocked() {
	if s.wb == nil {
		return
	}
	doc := definitionsDocument{Version: definitionsDocumentVersion, Definitions: s.raw}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		definitionsLog.Error(fmt.Sprintf("encoding the definitions store for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	s.wb.MarkDirty(data)
}
