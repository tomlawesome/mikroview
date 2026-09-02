// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strconv"
	"strings"
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
	// KeyPerDestinationPort tracks one window per destination port,
	// regardless of who the source or target was --
	// distributed_brute_force's shape: "many different sources hammering
	// this one service." Naturally bounded by the number of distinct
	// ports the definition's own conditions admit, which is why
	// internal/detect's criticalPortIPs map needed no eviction.
	KeyPerDestinationPort KeyMode = "perDestinationPort"
	// KeyGlobal tracks a single shared window across every matching
	// event, regardless of source or target -- distributed_brute_force's
	// shape (many distinct sources against one condition-selected port),
	// global_spike's shape.
	KeyGlobal KeyMode = "global"
)

func validateKeyMode(k KeyMode) error {
	switch k {
	case KeyPerSource, KeyPerSourcePort, KeyPerTarget, KeyPerDestinationPort, KeyGlobal:
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
	// EvidenceNAT records the NAT translation detail of the matching
	// event -- last-writer-wins rather than a set, see
	// EvidenceSet.SetNAT for why this one category works that way.
	EvidenceNAT EvidenceField = "nat"
	// EvidencePairs accumulates the (destination host, destination port)
	// combination of every matching event, always from e.DstIP/e.DstPort
	// regardless of KeyMode -- unlike EvidenceHosts, whose asymmetry is
	// about *whose* address a per-source vs. per-target definition
	// records (see recordEvidence's own doc comment), a pair is always
	// about what got touched, which is the destination side by
	// definition. #654: declared only where both a destination address
	// and a destination port are independently meaningful for this
	// definition -- port_scan (ports without a meaningful destination
	// set) and dest_spread (destinations without ports) never declare
	// this, and neither does any definition whose KeyMode already fixes
	// the destination port for the whole window (repeated_drops,
	// distributed_brute_force), since a pair there would just repeat the
	// key's own port against each host, adding nothing Hosts and the
	// Target don't already say together.
	EvidencePairs EvidenceField = "pairs"
	// EvidenceMAC records the matching event's source MAC address,
	// last-writer-wins (see EvidenceSet.SetSrcMAC) and only when the
	// source is a local device -- recordEvidence enforces both the
	// presence check and the locality check, not this declaration.
	// #654: a MAC-identified device survives a DHCP lease change an
	// IP-identified one would silently stop matching (the same
	// MAC-preferred identity matchlog.Identity already uses, see
	// eventIdentity in router.go). Declared only for definitions whose
	// source can genuinely be local -- never for critical_port or
	// distributed_brute_force, both of which condition on
	// sourceAddress matchesClassification "external", so their source
	// MAC would never pass the locality check anyway.
	EvidenceMAC EvidenceField = "mac"
)

func validateEvidenceField(f EvidenceField) error {
	switch f {
	case EvidencePorts, EvidenceHosts, EvidenceLabels, EvidenceNAT, EvidencePairs, EvidenceMAC:
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
	// detailTemplate/target are this key's own resolved text: the
	// definition's templates with every key-component token already
	// substituted (see newStateForWindow). Computed once per key rather
	// than per emission, and constant for the key's whole life by
	// construction -- a key token IS the key.
	detailTemplate string
	target         string
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
	targetTemplate string
	carryCountry   bool
	// evidencePorts/Hosts/Labels/NAT/Pairs/MAC are DeclarativeSpec.Evidence
	// resolved to booleans once, at construction, so recordEvidence's
	// per-event path is a few field reads rather than a slice scan.
	evidencePorts  bool
	evidenceHosts  bool
	evidenceLabels bool
	evidenceNAT    bool
	evidencePairs  bool
	evidenceMAC    bool

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
	// DetailTemplate is the emission's Detail text. Two token families
	// resolve in it: the key-component tokens this definition's KeyMode
	// supplies ({SourceAddress}, {DestinationAddress},
	// {DestinationPort} -- see keyFieldValues), substituted once per key,
	// and RenderEmission's evidence/count tokens, bounded by Evidence
	// below.
	DetailTemplate string
	// TargetTemplate is the emission's Target text, over the same
	// key-component token set as DetailTemplate. Empty (the default)
	// means the Target is the key itself -- a source address, a target
	// address, the literal "global". Only a definition whose
	// operator-facing target has a shape of its own needs this:
	// repeated_drops' "<source> -> port <N>", distributed_brute_force's
	// "port <N>". Deliberately restricted to key components, so a Target
	// can never name something that varies within the window it
	// describes.
	TargetTemplate string
	// Evidence declares which categories this definition accumulates --
	// see EvidenceField. Empty means this definition accumulates no
	// evidence at all, which is legitimate (its Detail template then
	// references only {Count}) and is what keeps an emission from
	// carrying a category the definition has no business claiming.
	Evidence []EvidenceField
	// CarrySourceCountry puts the triggering event's SrcCountry on the
	// emission (and so on the raised flag's country badge). Only honest
	// for a definition whose emission is *about* one source: a
	// per-destination-port or global definition aggregates many sources,
	// so naming one of their countries would be the same
	// single-event-stands-for-the-window claim #379 found in two Detail
	// strings. internal/detect drew this line by hand, passing e.SrcCountry
	// for its source-keyed detectors and "" for
	// distributed_brute_force/dest_spread; declaring it keeps that
	// deliberate rather than incidental.
	CarrySourceCountry bool
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
		targetTemplate: spec.TargetTemplate,
		carryCountry:   spec.CarrySourceCountry,
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
		case EvidenceNAT:
			d.evidenceNAT = true
		case EvidencePairs:
			d.evidencePairs = true
		case EvidenceMAC:
			d.evidenceMAC = true
		}
	}
	if err := d.validateKeyTokens("detailTemplate", spec.DetailTemplate); err != nil {
		return nil, err
	}
	if err := d.validateKeyTokens("targetTemplate", spec.TargetTemplate); err != nil {
		return nil, err
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

// newStateForWindow builds one key's fresh state, sized to window (rather
// than unconditionally to d.window -- the seam Replay,
// replay_declarative.go, uses to build state against a candidate window
// override without touching d.window itself) and with this key's own
// detail/target text resolved once here rather than on every emission.
//
// Resolving the key tokens per key, at state creation, is what makes
// them honest: a key token's value is constant for the whole life of
// that key's window *by construction* -- it is what the key is -- so
// interpolating it into a sentence describing the whole window cannot
// misdescribe it, which is precisely the property e.DstPort lacked in
// critical_port's pre-#379 Detail string.
func (d *DeclarativeDefinition) newStateForWindow(window time.Duration, e store.Event) *declState {
	s := &declState{evidence: NewEvidenceSet()}
	switch d.countingMode {
	case CountingTotal:
		s.count = NewCountRing(window)
	case CountingDistinct:
		s.distinct = NewDistinctRing[string](window)
	}
	kv := d.keyFieldValues(e)
	s.detailTemplate = substituteKeyTokens(d.detailTemplate, kv)
	s.target = d.keyFor(e)
	if d.targetTemplate != "" {
		s.target = substituteKeyTokens(d.targetTemplate, kv)
	}
	return s
}

// keyFieldValues returns the event fields that make up this definition's
// key -- the closed token vocabulary a Detail or Target template may
// interpolate (see keyTokenNames). Only the fields the KeyMode actually
// keys on are present: a per-source definition has no honest way to name
// "the" destination port, because its window spans every port that
// source touched.
func (d *DeclarativeDefinition) keyFieldValues(e store.Event) map[string]string {
	switch d.key {
	case KeyPerSource:
		return map[string]string{"SourceAddress": e.SrcIP}
	case KeyPerSourcePort:
		return map[string]string{"SourceAddress": e.SrcIP, "DestinationPort": strconv.Itoa(e.DstPort)}
	case KeyPerTarget:
		return map[string]string{"DestinationAddress": e.DstIP}
	case KeyPerDestinationPort:
		return map[string]string{"DestinationPort": strconv.Itoa(e.DstPort)}
	default: // KeyGlobal
		return map[string]string{}
	}
}

// keyTokenNames is the closed set of key-component token names, listed
// once so NewDeclarativeDefinition can reject a template naming one its
// KeyMode cannot supply -- a construction-time error rather than a
// per-emission render failure, since the mismatch is a property of the
// definition, not of any one event.
var keyTokenNames = []string{"SourceAddress", "DestinationAddress", "DestinationPort"}

// substituteKeyTokens replaces every {Name} in tmpl that kv holds a value
// for, leaving every other token untouched for RenderEmission to resolve
// (or reject) against the evidence set.
func substituteKeyTokens(tmpl string, kv map[string]string) string {
	return emissionToken.ReplaceAllStringFunc(tmpl, func(tok string) string {
		if v, ok := kv[tok[1:len(tok)-1]]; ok {
			return v
		}
		return tok
	})
}

// validateKeyTokens rejects a template naming a key-component token this
// KeyMode does not supply -- see keyTokenNames.
func (d *DeclarativeDefinition) validateKeyTokens(what, tmpl string) error {
	available := d.keyFieldValues(store.Event{})
	for _, name := range keyTokenNames {
		if _, ok := available[name]; ok {
			continue
		}
		if strings.Contains(tmpl, "{"+name+"}") {
			return fmt.Errorf("engine: definition %q: %s names {%s}, which key mode %q does not supply", d.def.ID, what, name, d.key)
		}
	}
	return nil
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
	case KeyPerDestinationPort:
		return strconv.Itoa(e.DstPort)
	default: // KeyGlobal
		return "global"
	}
}

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
	if d.evidenceNAT {
		st.evidence.SetNAT(NATInfo{IP: e.NatIP, Port: e.NatPort, Raw: e.NatRaw})
	}
	// #654: pairs and MAC each have their own event fields and their own
	// gate, independent of evidenceHosts below -- a definition can (and
	// critical_port does) want the destination pair without wanting the
	// standalone Hosts set, and MAC has no relationship to KeyMode at
	// all. Neither reads d.evidenceHosts.
	if d.evidencePairs && e.DstIP != "" && e.DstPort != 0 {
		st.evidence.AddPair(HostPort{Host: e.DstIP, Port: e.DstPort})
	}
	// isPublicIPAddress(e.SrcIP) is the same classification
	// OpMatchesClassification's "external"/"internal" conditions use
	// (conditions.go) -- a MAC only ever accompanies a frame RouterOS
	// captured off a local L2 segment, so an external source's SrcMAC is
	// either empty or the router's own upstream-facing MAC, and #654's
	// design explicitly calls carrying either "worse than useless": it
	// would look like a device identity but silently misidentify one.
	if d.evidenceMAC && e.SrcMAC != "" && !isPublicIPAddress(e.SrcIP) {
		st.evidence.SetSrcMAC(e.SrcMAC)
	}
	if !d.evidenceHosts {
		return
	}
	switch d.key {
	case KeyPerSource, KeyPerSourcePort:
		if e.DstIP != "" {
			st.evidence.AddHost(e.DstIP)
		}
	default: // KeyPerTarget, KeyPerDestinationPort, KeyGlobal
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
	st := d.state.GetOrCreate(key, now, func() *declState { return d.newStateForWindow(d.window, e) })

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

	em, err := RenderEmission(st.evidence, count, st.detailTemplate, false)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: RenderEmission failed: %v", d.def.ID, err))
		return
	}
	em.DefinitionID = d.def.ID
	em.Target = st.target
	// SourceIP is the reputation-lookup candidate, carried separately from
	// Target because they are not always the same string: repeated_drops'
	// Target is a "<source> -> port <N>" composite, and a lookup needs the
	// address, not the composite -- exactly the split
	// internal/detect.maybeCheckReputation's own (target, ip) parameter
	// pair drew by hand.
	em.SourceIP = e.SrcIP
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
	// Size: a declarative definition's size is always the counting-mode
	// tally that crossed the threshold -- distinct destination ports for
	// port_scan, attempts for critical_port, and so on -- because
	// "threshold-over-window" is the whole of what this kind evaluates
	// (docs/decisions/evaluation-engine.md section 2). So it is set here,
	// once, rather than per shipped builder: there is no declarative
	// definition, shipped or operator-authored, whose size is anything
	// else. See Emission.Size and #640.
	size := count
	em.Size = &size
	// Country/EventTime: see Emission's own doc comment on why these are
	// set here, from the triggering event, rather than by RenderEmission.
	// Country only where the definition declared it honest -- see
	// DeclarativeSpec.CarrySourceCountry.
	if d.carryCountry {
		em.Country = e.SrcCountry
	}
	em.EventTime = e.ReceivedAt
	// TriggeringEvent: what an expectation-intent definition's match log
	// record embeds as evidence, and what its matchlog.Tuple is derived
	// from (#406). Set unconditionally rather than only for
	// IntentExpectation, because a definition's own evaluation code
	// branching on its Intent is exactly what Intent is not for -- Route
	// is the one place that branch lives, and routeToFlag ignores this
	// field. See Emission.TriggeringEvent.
	//
	// A local copy rather than &e: taking the address of the parameter
	// makes Go's escape analysis heap-allocate it on *every* Evaluate
	// call, emitting or not -- a per-event allocation of a whole
	// store.Event, per declarative definition, on the ingest path.
	// Copying here confines that allocation to the threshold crossings
	// that actually emit, which are rare by construction. Confirmed with
	// `go build -gcflags=-m`: with &e the compiler reports
	// "moved to heap: e" for this function; with the copy it does not.
	triggering := e
	em.TriggeringEvent = &triggering

	routed, err := Route(d.def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("declarative definition %q: Route failed: %v", d.def.ID, err))
		return
	}
	if d.OnRoutedEmission != nil {
		d.OnRoutedEmission(routed)
	}
}
