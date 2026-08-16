// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// KeyMode is the closed set of ways a declarative definition keys its
// per-key window/threshold state (issue #402) -- what #399's Keyed[V] is
// keyed by for this definition. Every real detector this kind is meant
// to eventually replace (per docs/decisions/evaluation-engine.md
// section 2, port_scan/critical_port/repeated_drops/dest_spread/
// mail_sender/off_hours/known_bad_ip and kin) fits one of these four.
type KeyMode string

const (
	// KeyPerSource tracks one window per source address (e.SrcIP) --
	// port_scan, critical_port's shape.
	KeyPerSource KeyMode = "perSource"
	// KeyPerSourcePort tracks one window per (source address,
	// destination port) pair -- for a signature that only makes sense
	// scoped to both, e.g. "this source repeatedly hitting this one
	// port" as distinct from "this source touching many ports."
	KeyPerSourcePort KeyMode = "perSourcePort"
	// KeyPerTarget tracks one window per destination address (e.DstIP)
	// -- repeated_drops against one locally-hosted service, dest_spread
	// as seen from the target's side.
	KeyPerTarget KeyMode = "perTarget"
	// KeyGlobal tracks a single shared window across every matching
	// event, regardless of source or target -- distributed_brute_force's
	// shape (many distinct sources against one condition-selected port),
	// global_spike's shape.
	KeyGlobal KeyMode = "global"
)

func validateKeyMode(k KeyMode) error {
	switch k {
	case KeyPerSource, KeyPerSourcePort, KeyPerTarget, KeyGlobal:
		return nil
	default:
		return fmt.Errorf("engine: invalid key mode %q", k)
	}
}

// CountingMode selects which of #399's two ring primitives
// (window.go's CountRing vs DistinctRing) a declarative definition's
// window is built on.
type CountingMode string

const (
	// CountingTotal counts every matching event, via CountRing --
	// "15 events in 60s."
	CountingTotal CountingMode = "total"
	// CountingDistinct counts distinct values of the definition's
	// DistinctField, via DistinctRing[string] -- "5 distinct source
	// addresses in 60s." See
	// TestDeclarativeDistinctCountingModeIsNotSatisfiableByOneSource for
	// the characterization this mode exists to make possible: repeats
	// from a single value must never satisfy a distinct-mode threshold
	// the way they would satisfy a total-mode one -- the same
	// distinction internal/detect's
	// TestCharacterizationDistributedBruteForceRequiresDistinctSources
	// pins for the hand-written detector this kind is meant to replace.
	CountingDistinct CountingMode = "distinct"
)

// declState is one key's tracked window state -- exactly one of count/
// distinct is non-nil, selected by the owning DeclarativeDefinition's
// CountingMode at construction (see newState). evidence accumulates
// across every matching event for this key, independent of counting
// mode, so an emission's Detail/Ports/Hosts/Labels are always populated
// from what was actually seen (docs/decisions/evaluation-engine.md
// section 1's evidence-accumulation contract), not derived from the
// ring alone.
type declState struct {
	count    *CountRing
	distinct *DistinctRing[string]
	evidence *EvidenceSet
}

// DeclarativeDefinition is one declarative definition (issue #402):
// structured conditions, a key, a window, a threshold, a counting mode
// and an emission template, wired onto the same Evaluated interface
// (engine.go) any other definition kind implements -- Register(d) on an
// Engine works exactly the same way for this as for a fakeDef in the
// chassis's own tests or a future programmatic definition. Nothing here
// constructs one against real traffic: that is #405's job
// (docs/decisions/evaluation-engine.md, "Migration"). See
// NewDeclarativeDefinition.
type DeclarativeDefinition struct {
	def        Definition
	conditions []Condition
	compiled   compiledConditionSet

	key            KeyMode
	window         time.Duration
	threshold      int
	countingMode   CountingMode
	distinctField  Field
	detailTemplate string

	members AddressListMembership

	state *Keyed[*declState]

	// OnRoutedEmission, if set, receives every RoutedEmission this
	// definition produces after Route (router.go) -- the seam #405/#406
	// wire a real flags.Store/matchlog.Store onto (see Route's own doc
	// comment: "No production call site exists yet"). nil is a valid
	// no-op: an evaluated definition with no sink still updates its
	// window/evidence state and simply produces nothing observable, the
	// same way a definition below threshold does. Exactly what an
	// end-to-end test wires to capture what was emitted -- see
	// TestDeclarativeDefinitionEndToEnd.
	OnRoutedEmission func(RoutedEmission)
}

// NewDeclarativeDefinition validates and compiles everything a
// declarative definition needs up front -- conditions (compileConditions),
// the envelope (Definition.Validate), key mode, window/threshold bounds,
// and counting-mode/DistinctField compatibility -- so Evaluate's hot path
// never re-validates or re-parses anything per event. members may be nil
// (a definition with no FieldAddressListMembership condition never
// touches it; one that does simply never matches -- see
// compileMembershipCondition's safe-direction handling).
func NewDeclarativeDefinition(
	def Definition,
	conditions []Condition,
	key KeyMode,
	window time.Duration,
	threshold int,
	countingMode CountingMode,
	distinctField Field,
	detailTemplate string,
	members AddressListMembership,
) (*DeclarativeDefinition, error) {
	if def.Kind != KindDeclarative {
		return nil, fmt.Errorf("engine: declarative definition %q has kind %q, want %q", def.ID, def.Kind, KindDeclarative)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	compiled, err := compileConditions(conditions)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: %w", def.ID, err)
	}
	if err := validateKeyMode(key); err != nil {
		return nil, fmt.Errorf("engine: definition %q: %w", def.ID, err)
	}
	if window <= 0 {
		return nil, fmt.Errorf("engine: definition %q: window must be positive, got %s", def.ID, window)
	}
	if threshold <= 0 {
		return nil, fmt.Errorf("engine: definition %q: threshold must be positive, got %d", def.ID, threshold)
	}
	switch countingMode {
	case CountingTotal:
	case CountingDistinct:
		if !distinctCountableFields[distinctField] {
			return nil, fmt.Errorf("engine: definition %q: countingMode=distinct requires a countable distinctField, got %q", def.ID, distinctField)
		}
	default:
		return nil, fmt.Errorf("engine: definition %q: invalid countingMode %q", def.ID, countingMode)
	}
	if detailTemplate == "" {
		return nil, fmt.Errorf("engine: definition %q: detailTemplate is required", def.ID)
	}

	return &DeclarativeDefinition{
		def:            def,
		conditions:     conditions,
		compiled:       compiled,
		key:            key,
		window:         window,
		threshold:      threshold,
		countingMode:   countingMode,
		distinctField:  distinctField,
		detailTemplate: detailTemplate,
		members:        members,
		state:          NewKeyed[*declState](),
	}, nil
}

// ID satisfies Evaluated.
func (d *DeclarativeDefinition) ID() string { return d.def.ID }

// Kind satisfies Evaluated -- always KindDeclarative's string form.
func (d *DeclarativeDefinition) Kind() string { return string(d.def.Kind) }

// Definition returns a copy of the envelope this definition wears.
func (d *DeclarativeDefinition) Definition() Definition { return d.def }

// Conditions returns a copy of this definition's structured match
// conditions -- consulted by BuildDispatchIndex (dispatch.go) to choose
// a discriminating field; exported so a caller building an index over
// definitions it does not itself own (e.g. a store's own list) can do so
// without reaching into an unexported field.
func (d *DeclarativeDefinition) Conditions() []Condition {
	return append([]Condition(nil), d.conditions...)
}

func (d *DeclarativeDefinition) newState() *declState {
	s := &declState{evidence: NewEvidenceSet()}
	switch d.countingMode {
	case CountingTotal:
		s.count = NewCountRing(d.window)
	case CountingDistinct:
		s.distinct = NewDistinctRing[string](d.window)
	}
	return s
}

// keyFor computes this definition's state key for e, per KeyMode.
func (d *DeclarativeDefinition) keyFor(e store.Event) string {
	switch d.key {
	case KeyPerSource:
		return e.SrcIP
	case KeyPerSourcePort:
		return fmt.Sprintf("%s:%d", e.SrcIP, e.DstPort)
	case KeyPerTarget:
		return e.DstIP
	default: // KeyGlobal
		return "global"
	}
}

// targetFor computes an Emission's Target -- the key itself for every
// per-* mode (a source address, a source+port pair, a target address),
// or the literal "global" for KeyGlobal, mirroring
// internal/detect.observeDistributedBruteForce's own "port %d" / IP
// targets: what a definition's Target names is whatever its own key
// means, and KeyGlobal's key IS "global" already (see keyFor).
func (d *DeclarativeDefinition) targetFor(key string) string { return key }

// recordEvidence folds e into st.evidence -- ports and rule labels
// unconditionally (they mean the same thing regardless of key), and
// hosts on whichever side is NOT what this definition is keyed on: a
// per-source/per-source-port definition's evidence records what the
// source touched (destination addresses), while a per-target/global
// definition's evidence records who touched it (source addresses) --
// the same asymmetry internal/detect.observeDistributedBruteForce (keyed
// by port, evidence = distinct source IPs) and port_scan (keyed by
// source, evidence = distinct destination ports) already draw by hand.
func recordEvidence(key KeyMode, st *declState, e store.Event) {
	if e.DstPort != 0 {
		st.evidence.AddPort(e.DstPort)
	}
	if e.RuleLabel != "" {
		st.evidence.AddLabel(e.RuleLabel)
	}
	switch key {
	case KeyPerSource, KeyPerSourcePort:
		if e.DstIP != "" {
			st.evidence.AddHost(e.DstIP)
		}
	default: // KeyPerTarget, KeyGlobal
		if e.SrcIP != "" {
			st.evidence.AddHost(e.SrcIP)
		}
	}
}

// Evaluate satisfies Evaluated: conditions must match (AND across
// fields, OR within a field -- compiled.match), then the matching event
// folds into this key's window (CountRing or DistinctRing, per
// CountingMode) and evidence; a threshold crossing renders an Emission
// (RenderEmission, emission.go) from the accumulated evidence, routes it
// (Route, router.go) and hands the result to OnRoutedEmission, if set.
//
// Disabled definitions (def.Enabled == false) are inert -- Evaluate
// returns immediately, touching no state, mirroring
// detect/watchlist's own per-definition enabled gates.
func (d *DeclarativeDefinition) Evaluate(e store.Event) {
	if !d.def.Enabled {
		return
	}
	if !d.compiled.match(e, d.members) {
		return
	}

	now := e.ReceivedAt
	key := d.keyFor(e)
	st := d.state.GetOrCreate(key, now, d.newState)

	recordEvidence(d.key, st, e)

	var count int
	switch d.countingMode {
	case CountingTotal:
		st.count.Add(now, true)
		count = st.count.Count(now, d.window)
	case CountingDistinct:
		v, ok := distinctFieldValue(d.distinctField, e)
		if !ok {
			return
		}
		st.distinct.Add(now, v)
		count = st.distinct.Count(now, d.window, nil)
	}

	if count < d.threshold {
		return
	}

	em, err := RenderEmission(st.evidence, d.detailTemplate, false)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: RenderEmission failed: %v", d.def.ID, err))
		return
	}
	em.DefinitionID = d.def.ID
	em.Target = d.targetFor(key)

	routed, err := Route(d.def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: Route failed: %v", d.def.ID, err))
		return
	}
	if d.OnRoutedEmission != nil {
		d.OnRoutedEmission(routed)
	}
}
