// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// Registry keeps what the engine evaluates in step with what the
// definitions store holds -- issue #407's first handover: an edit takes
// effect on the very next ingested event, not on the next restart.
//
// # Why this exists at all
//
// Before the engine, a detector's enabled flag and scope were read from
// a settings store on every single event, so a toggle was live by
// construction. #405's port replaced that with definitions built once at
// boot and registered, which is what makes a large definition set cost
// the ingest budget a dispatch lookup rather than a linear scan -- and
// which quietly made every detector toggle restart-effective. That
// regression was recorded rather than hidden (see PR #456's doc
// correction), with the fix deferred to this issue. This type is the fix.
//
// # Why it rebuilds selectively rather than wholesale
//
// The obvious implementation -- rebuild every definition on every change
// -- is wrong for a reason worth stating: a definition's live evaluation
// state (a threshold-over-window CountRing, a warming baseline) lives in
// the object built from the envelope, so rebuilding one discards it.
// Definition.Enabled's own doc comment promises the opposite ("Enabled
// off does not remove state already accumulated -- re-enabling resumes
// rather than starts cold"), and an operator renaming one expectation
// must not reset every other definition's half-full window. So Sync
// rebuilds exactly the definitions whose stored bytes actually changed,
// and carries every unchanged one forward as the same live object.
//
// The comparison is on the stored JSON rather than on a decoded value:
// the store's own canonical form for a definition is its bytes (see
// definitionsDocument), so "did this definition change" has a definitive
// answer that needs no field-by-field equality to be written down and
// kept in step with the envelope.
//
// # Concurrency
//
// Sync may run on any goroutine -- in production it runs on whichever
// request goroutine made the edit, via DefinitionsStore.SetOnChange --
// concurrently with the evaluation goroutine. It never mutates a live
// definition: it builds new objects, then replaces registrations through
// Engine.Register, which is the chassis's own documented safe-under-Run
// operation. Its own bookkeeping is behind a mutex so two concurrent
// edits cannot interleave into a half-updated view of what is
// registered.
type Registry struct {
	eng   *Engine
	store *DefinitionsStore
	deps  RegistrationDeps

	mu sync.Mutex
	// built is the live object for each definition id, and raw is the
	// stored bytes it was built from -- together, "what is registered and
	// what produced it". Both are keyed by definition id, including for
	// definitions that ride inside a set: a set is a dispatch index over
	// members, so carrying an unchanged member forward is exactly reusing
	// its pointer.
	built map[string]Evaluated
	raw   map[string]string
}

// RegistrationDeps is everything the definitions need beyond the event
// stream, injected once at construction -- the same nil-tolerant contract
// ShippedDeps and ExpectationDeps each state for their own halves: a
// deployment with no flag store, no reputation client, no match log and
// no router state still builds and registers the whole set, it simply
// produces nothing.
type RegistrationDeps struct {
	// Shipped is what the shipped programmatic definitions need.
	Shipped ShippedDeps
	// Expectations is what the expectation definitions need.
	Expectations ExpectationDeps
	// Flags and Reputation build each shipped definition's emission sink
	// (see ShippedDeclarativeSink). Kept here rather than being passed as
	// a ready-made sink because the sink is per-definition, and the
	// reputation policy it carries comes from the reputation definition's
	// own params -- which are themselves part of what Sync re-reads.
	Flags      *flags.Store
	Reputation ReputationLookup
}

// NewRegistry constructs a Registry over eng and store. Nothing is
// registered until Sync is called.
func NewRegistry(eng *Engine, store *DefinitionsStore, deps RegistrationDeps) *Registry {
	return &Registry{
		eng:   eng,
		store: store,
		deps:  deps,
		built: make(map[string]Evaluated),
		raw:   make(map[string]string),
	}
}

// ShippedDeclarativeSetID and CustomDeclarativeSetID are the ids the two
// declarative sets register under. Fixed, because re-registering a set
// under the same id replaces the previous one wholesale, which is exactly
// Engine.Register's contract -- and because a set is where dispatch
// narrowing lives (see NewDeclarativeSet: the chassis's own loop is a
// flat scan over registrations, so narrowing has to be one level below
// it).
const (
	ShippedDeclarativeSetID = "shipped-declarative"
	CustomDeclarativeSetID  = "custom-declarative"
)

// Sync rebuilds whatever changed and re-registers it.
//
// Errors are collected rather than returned at the first failure: one
// unbuildable definition must not stop every other definition being
// registered, because the consequence of stopping is a coverage hole
// across the whole set rather than for the one broken definition. The
// caller logs what came back; every definition that could be built is
// live regardless.
func (r *Registry) Sync() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored := r.store.List()
	policy := ReputationPolicyFrom(r.store)

	var problems []error
	seen := make(map[string]bool, len(stored))
	nextBuilt := make(map[string]Evaluated, len(stored))
	nextRaw := make(map[string]string, len(stored))

	var shippedDecl, customDecl, expectationDecl []*DeclarativeDefinition
	var programmatic []Evaluated
	var entries []watchlist.Entry

	for _, sd := range stored {
		if !sd.Available {
			continue
		}
		def := sd.Definition
		seen[def.ID] = true

		if def.Intent == IntentExpectation {
			e, err := EntryFromDefinition(def)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			entries = append(entries, e)
			if e.Invert {
				// The inverted set is one definition holding every
				// inverted entry (see InvertedExpectations), rebuilt
				// wholesale below -- it holds no per-window state to
				// preserve, since its state is the store's own observed/
				// permitted params.
				continue
			}
		}

		raw, err := r.store.rawFor(def.ID)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if prior, ok := r.built[def.ID]; ok && r.raw[def.ID] == raw {
			// Unchanged: carry the live object forward, state and all.
			nextBuilt[def.ID] = prior
			nextRaw[def.ID] = raw
			r.place(prior, def, &shippedDecl, &customDecl, &expectationDecl, &programmatic)
			continue
		}

		evaluated, err := r.build(def, policy)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		nextBuilt[def.ID] = evaluated
		nextRaw[def.ID] = raw
		r.place(evaluated, def, &shippedDecl, &customDecl, &expectationDecl, &programmatic)
	}

	// A definition that vanished (a deleted expectation, a shipped id
	// this binary no longer knows) stops being evaluated. Sets are
	// re-registered wholesale below, so this only has to unregister the
	// individually-registered programmatic definitions -- but it runs
	// over every id, since a definition that changed kind moves between
	// the two shapes.
	for id := range r.built {
		if !seen[id] {
			r.eng.Unregister(id)
		}
	}

	r.built = nextBuilt
	r.raw = nextRaw

	r.eng.Register(NewDeclarativeSet(ShippedDeclarativeSetID, shippedDecl))
	r.eng.Register(NewDeclarativeSet(CustomDeclarativeSetID, customDecl))
	r.eng.Register(NewDeclarativeSet(ExpectationSetID, expectationDecl))
	for _, pd := range programmatic {
		r.eng.Register(pd)
	}

	inverted, err := NewInvertedExpectations(entries, r.deps.Expectations.Observations)
	if err != nil {
		// The previous registration stays live rather than being
		// replaced by nothing: an expectation set that silently stopped
		// evaluating is the "absence of detection presented as absence of
		// threat" failure #380's first item describes.
		problems = append(problems, err)
	} else {
		inverted.OnRoutedEmission = r.deps.Expectations.Sink
		r.eng.Register(inverted)
	}
	return problems
}

// place files one built definition into the bucket its envelope calls
// for. A programmatic definition is registered individually (it has no
// dispatch pre-index to share, and one registration per definition is
// what gives each its own panic boundary and fault report); a declarative
// one rides in the set for its provenance and intent.
func (r *Registry) place(evaluated Evaluated, def Definition, shipped, custom, expectation *[]*DeclarativeDefinition, programmatic *[]Evaluated) {
	dd, ok := evaluated.(*DeclarativeDefinition)
	if !ok {
		*programmatic = append(*programmatic, evaluated)
		return
	}
	switch {
	case def.Intent == IntentExpectation:
		*expectation = append(*expectation, dd)
	case def.Provenance.Origin == ProvenanceShipped:
		*shipped = append(*shipped, dd)
	default:
		*custom = append(*custom, dd)
	}
}

// build constructs one definition's live logic and wires its sink.
func (r *Registry) build(def Definition, policy ReputationPolicy) (Evaluated, error) {
	if def.Intent == IntentExpectation {
		dd, err := BuildExpectationDefinition(def, r.deps.Expectations.Members)
		if err != nil {
			return nil, err
		}
		dd.OnRoutedEmission = r.deps.Expectations.Sink
		return dd, nil
	}
	if def.Kind == KindDeclarative {
		dd, err := BuildShippedDeclarativeDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
		}
		dd.OnRoutedEmission = ShippedDeclarativeSink(def, r.deps.Flags, r.deps.Reputation, policy)
		return dd, nil
	}
	pd, err := BuildShippedProgrammaticDefinition(def, r.deps.Shipped)
	if err != nil {
		return nil, err
	}
	if sink, ok := pd.(interface{ SetSink(func(RoutedEmission)) }); ok {
		sink.SetSink(ShippedDeclarativeSink(def, r.deps.Flags, r.deps.Reputation, policy))
	}
	return pd, nil
}

// rawFor returns the exact stored bytes for id, as a string, so a caller
// can tell whether a definition changed without decoding it -- see
// Registry's own doc comment on why the comparison is on bytes.
func (s *DefinitionsStore) rawFor(id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, ok := s.raw[id]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNoSuchDefinition, id)
	}
	return string(raw), nil
}
