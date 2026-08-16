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

// EvidenceField names one category a declarative definition accumulates
// evidence for. Declared explicitly per definition (see
// DeclarativeSpec.Evidence) rather than inferred from KeyMode or from the
// Detail template, because neither infers it correctly: two definitions
// keyed identically can honestly claim different evidence
// (port_scan's per-source window is about the *ports* it touched;
// repeated_drops', per #379, is about the *destinations* it hit), and a
// definition may legitimately carry evidence its Detail sentence never
// names (distributed_brute_force's Detail counts sources while its
// Evidence lists them).
//
// Declaring is what makes the #379 class of defect structural: an
// emission can only ever carry -- and its Detail template can only ever
// reference -- a category this definition actually accumulates. A
// template naming an undeclared category is a hard render error
// (RenderEmission, emission.go), not a silently empty value.
type EvidenceField string

const (
	// EvidencePorts accumulates the destination port of every matching
	// event (port 0 excluded -- see recordEvidence).
	EvidencePorts EvidenceField = "ports"
	// EvidenceHosts accumulates one address per matching event, chosen by
	// the definition's KeyMode: the destination address for a
	// per-source/per-source-port definition (what the source touched),
	// the source address for a per-target/global one (who touched it).
	EvidenceHosts EvidenceField = "hosts"
	// EvidenceLabels accumulates the rule label of every matching event.
	EvidenceLabels EvidenceField = "labels"
)

func validateEvidenceField(f EvidenceField) error {
	switch f {
	case EvidencePorts, EvidenceHosts, EvidenceLabels:
		return nil
	default:
		return fmt.Errorf("engine: invalid evidence field %q", f)
	}
}

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
	// evidencePorts/Hosts/Labels are DeclarativeSpec.Evidence resolved to
	// three booleans once, at construction, so recordEvidence's per-event
	// path is three field reads rather than a slice scan.
	evidencePorts  bool
	evidenceHosts  bool
	evidenceLabels bool

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

// DeclarativeSpec is everything a declarative definition needs beyond
// its own envelope (Definition): the match language, the key/window/
// threshold/counting shape, the emission template, and the evidence
// categories it accumulates. A struct rather than a positional argument
// list because #405's ports made the list long enough that call sites
// stopped being readable -- three consecutive string/Field arguments
// where an empty one means "not applicable" is exactly the shape a
// mis-ordered argument hides in.
type DeclarativeSpec struct {
	// Conditions is this definition's structured match language -- AND
	// across fields, OR within a field. Empty matches every event.
	Conditions []Condition
	// Key selects what this definition's window/threshold state is keyed
	// by -- see KeyMode.
	Key KeyMode
	// Window and Threshold are the crossing this definition fires on:
	// Threshold or more counted things within Window. Both required
	// (positive).
	Window    time.Duration
	Threshold int
	// CountingMode selects CountRing vs DistinctRing; DistinctField is
	// required (and must be distinct-countable) for CountingDistinct,
	// ignored for CountingTotal.
	CountingMode  CountingMode
	DistinctField Field
	// DetailTemplate is the emission's Detail text -- see RenderEmission
	// for the token set, which is bounded by Evidence below.
	DetailTemplate string
	// Evidence declares which categories this definition accumulates --
	// see EvidenceField. Empty means this definition accumulates no
	// evidence at all, which is legitimate (its Detail template then
	// references only {Count}) and is what keeps an emission from
	// carrying a category the definition has no business claiming.
	Evidence []EvidenceField
	// Members resolves FieldAddressListMembership conditions. nil is
	// valid: a definition with no membership condition never touches it,
	// and one that does simply never matches (see
	// compileMembershipCondition's safe-direction handling).
	Members AddressListMembership
}

// NewDeclarativeDefinition validates and compiles everything a
// declarative definition needs up front -- conditions (compileConditions),
// the envelope (Definition.Validate), key mode, window/threshold bounds,
// counting-mode/DistinctField compatibility, and the declared evidence
// fields -- so Evaluate's hot path never re-validates or re-parses
// anything per event.
func NewDeclarativeDefinition(def Definition, spec DeclarativeSpec) (*DeclarativeDefinition, error) {
	if def.Kind != KindDeclarative {
		return nil, fmt.Errorf("engine: declarative definition %q has kind %q, want %q", def.ID, def.Kind, KindDeclarative)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	compiled, err := compileConditions(spec.Conditions)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: %w", def.ID, err)
	}
	if err := validateKeyMode(spec.Key); err != nil {
		return nil, fmt.Errorf("engine: definition %q: %w", def.ID, err)
	}
	if spec.Window <= 0 {
		return nil, fmt.Errorf("engine: definition %q: window must be positive, got %s", def.ID, spec.Window)
	}
	if spec.Threshold <= 0 {
		return nil, fmt.Errorf("engine: definition %q: threshold must be positive, got %d", def.ID, spec.Threshold)
	}
	switch spec.CountingMode {
	case CountingTotal:
	case CountingDistinct:
		if !distinctCountableFields[spec.DistinctField] {
			return nil, fmt.Errorf("engine: definition %q: countingMode=distinct requires a countable distinctField, got %q", def.ID, spec.DistinctField)
		}
	default:
		return nil, fmt.Errorf("engine: definition %q: invalid countingMode %q", def.ID, spec.CountingMode)
	}
	if spec.DetailTemplate == "" {
		return nil, fmt.Errorf("engine: definition %q: detailTemplate is required", def.ID)
	}

	d := &DeclarativeDefinition{
		def:            def,
		conditions:     spec.Conditions,
		compiled:       compiled,
		key:            spec.Key,
		window:         spec.Window,
		threshold:      spec.Threshold,
		countingMode:   spec.CountingMode,
		distinctField:  spec.DistinctField,
		detailTemplate: spec.DetailTemplate,
		members:        spec.Members,
		state:          NewKeyed[*declState](),
	}
	for _, f := range spec.Evidence {
		if err := validateEvidenceField(f); err != nil {
			return nil, fmt.Errorf("engine: definition %q: %w", def.ID, err)
		}
		switch f {
		case EvidencePorts:
			d.evidencePorts = true
		case EvidenceHosts:
			d.evidenceHosts = true
		case EvidenceLabels:
			d.evidenceLabels = true
		}
	}
	return d, nil
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
	return d.newStateForWindow(d.window)
}

// newStateForWindow is newState, sized to window rather than
// unconditionally to d.window -- the seam Replay (replay_declarative.go)
// uses to build fresh per-key state against a candidate window override
// without touching d.window itself.
func (d *DeclarativeDefinition) newStateForWindow(window time.Duration) *declState {
	s := &declState{evidence: NewEvidenceSet()}
	switch d.countingMode {
	case CountingTotal:
		s.count = NewCountRing(window)
	case CountingDistinct:
		s.distinct = NewDistinctRing[string](window)
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

// recordEvidence folds e into st.evidence, but only for the categories
// this definition declared (DeclarativeSpec.Evidence) -- an undeclared
// category is never accumulated, so an emission can never carry, and its
// Detail template can never reference, evidence the definition has no
// business claiming. That gate is the point: it is what makes #379's
// wrong-naming class structural rather than a matter of care, and #405
// added it after the port_scan port showed the cost of not having it (a
// per-source definition silently accumulating destination addresses it
// never claimed before, purely because the shared helper recorded them
// unconditionally).
//
// Which address EvidenceHosts records is still decided by KeyMode, since
// that is genuinely a property of what the key means: a per-source/
// per-source-port definition's evidence records what the source touched
// (destination addresses), while a per-target/global definition's
// records who touched it (source addresses) -- the same asymmetry
// internal/detect.observeDistributedBruteForce (keyed by port, evidence =
// distinct source IPs) and dest_spread (keyed by source, evidence =
// distinct destination IPs) already drew by hand.
func (d *DeclarativeDefinition) recordEvidence(st *declState, e store.Event) {
	if d.evidencePorts && e.DstPort != 0 {
		st.evidence.AddPort(e.DstPort)
	}
	if d.evidenceLabels && e.RuleLabel != "" {
		st.evidence.AddLabel(e.RuleLabel)
	}
	if !d.evidenceHosts {
		return
	}
	switch d.key {
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
	// Scope (docs/decisions/evaluation-engine.md section 2's envelope,
	// "scope (hosts AND netclass, the existing #44 model)") gates
	// evaluation before this definition's own conditions ever run -- #402
	// deliberately left this unenforced (see scope_match.go's own doc
	// comment); #405 is what wires it in.
	if !scopeMatches(d.def.Scope, e) {
		return
	}
	if !d.compiled.match(e, d.members) {
		return
	}

	now := e.ReceivedAt
	key := d.keyFor(e)
	st := d.state.GetOrCreate(key, now, d.newState)

	d.recordEvidence(st, e)

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

	em, err := RenderEmission(st.evidence, count, d.detailTemplate, false)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: RenderEmission failed: %v", d.def.ID, err))
		return
	}
	em.DefinitionID = d.def.ID
	em.Target = d.targetFor(key)
	// Confidence: every declarative definition's firing shape is a plain
	// threshold-over-window crossing (docs/decisions/evaluation-engine.md
	// section 2), which is exactly what overshootConfidence
	// (scope_match.go) scores -- "how far over the line" count is, unlike
	// a programmatic definition's statistical Baseline/Snapshot.ZScore.
	// #402 left this unset entirely (see scope_match.go's own doc
	// comment); #405 is this package's first real producer of a firing
	// Emission, so it is what fills it in.
	conf := overshootConfidence(count, d.threshold)
	em.Confidence = &conf
	// Country/EventTime: see Emission's own doc comment on why these are
	// set here, from the triggering event, rather than by RenderEmission.
	em.Country = e.SrcCountry
	em.EventTime = e.ReceivedAt

	routed, err := Route(d.def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: Route failed: %v", d.def.ID, err))
		return
	}
	if d.OnRoutedEmission != nil {
		d.OnRoutedEmission(routed)
	}
}
