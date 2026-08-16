// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strconv"
	"time"
)

// This file is issue #405's declarative half of internal/detect's port
// onto this chassis: docs/decisions/evaluation-engine.md section 2 names
// port_scan (among others) as a detector whose logic already is
// threshold-over-window, so it becomes a shipped DeclarativeDefinition
// rather than staying built-in Go.
//
// Why a builder function per shipped ID, not a generic "any declarative
// definition round-trips through Definition's JSON alone": Definition
// (definition.go) has no Conditions/KeyMode/CountingMode/DistinctField/
// DetailTemplate fields -- those are DeclarativeDefinition's own
// construction arguments (declarative.go), not part of the envelope every
// definition carries. A *custom*, operator-authored declarative
// definition (the builder UI, docs/decisions/evaluation-engine.md's v2)
// will need that data to actually be persisted -- a real design question
// for whichever issue builds that UI, deliberately not pre-empted here
// (per the ADR's "no DSL" carefulness: adding fields to the shared
// envelope before there is a real consumer risks guessing the wrong
// shape). A *shipped* declarative definition doesn't have that problem:
// its match language is fixed at compile time, the same way a shipped
// programmatic definition's Go logic is -- only its threshold/window
// (and its Scope) are operator-tunable, and those already round-trip
// through Definition.Params/Definition.Scope. So each shipped declarative
// ID gets a small Go function, keyed by ID exactly the way a shipped
// programmatic definition's evaluation logic is keyed by ID, that reads
// the tunable parts from the stored Definition and supplies the fixed
// parts itself.
var shippedDeclarativeBuilders = map[string]func(Definition) (*DeclarativeDefinition, error){
	"port_scan":     buildPortScanDefinition,
	"critical_port": buildCriticalPortDefinition,
}

// BuildShippedDeclarativeDefinition constructs the live *DeclarativeDefinition
// for a shipped declarative Definition (def.Provenance.Origin ==
// ProvenanceShipped, def.Kind == KindDeclarative), keyed by def.ID -- see
// this file's own doc comment for why the match conditions/key mode/
// counting mode/detail template are not part of the persisted envelope.
// Returns an error naming def.ID if no builder is registered for it (a
// definition this binary's shipped catalogue does not recognize --
// structurally the declarative-kind counterpart to
// StoredDefinition.Available == false, though callers are expected to
// have already filtered on Available before reaching here).
func BuildShippedDeclarativeDefinition(def Definition) (*DeclarativeDefinition, error) {
	build, ok := shippedDeclarativeBuilders[def.ID]
	if !ok {
		return nil, fmt.Errorf("engine: %q has no shipped declarative builder registered", def.ID)
	}
	return build(def)
}

// paramInt/paramDuration read one already-ValidateParams-normalized param
// value back out of a Definition's Params -- ValidateParams's own doc
// comment (params.go) is what guarantees the shapes these two assume:
// ParamTypeInt normalizes to a Go int, ParamTypeDuration normalizes to
// its canonical time.Duration.String() form (a Go string), never the
// json.Unmarshal-produced float64/string a caller reading Params straight
// off a DefinitionsStore.Get/List result (decodeStored never calls
// ValidateParams -- see that function's own doc comment) would see for an
// unvalidated value. Both builders below call ValidateParams themselves
// first specifically so these two helpers can assume the normalized
// shape rather than re-deriving it.
func paramInt(params Params, name string) (int, error) {
	v, ok := params[name].(int)
	if !ok {
		return 0, fmt.Errorf("engine: param %q is not an int (got %T)", name, params[name])
	}
	return v, nil
}

func paramDuration(params Params, name string) (time.Duration, error) {
	s, ok := params[name].(string)
	if !ok {
		return 0, fmt.Errorf("engine: param %q is not a duration string (got %T)", name, params[name])
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("engine: param %q: invalid duration %q: %w", name, s, err)
	}
	return d, nil
}

// buildPortScanDefinition builds port_scan's live DeclarativeDefinition
// from a stored Definition -- the shipped declarative counterpart of
// internal/detect's old observeScanAndSpike's port-scan half
// (detect.go, before issue #405 removed it): "N distinct destination
// ports from one source within the window."
//
//   - Conditions: connectionState in {"", "new"} -- internal/detect's own
//     isTrackableConnState filter (detect.go), expressed as data: RouterOS
//     commonly logs both directions of an established connection on one
//     stateful accept rule, and without this filter a busy server's
//     ordinary *return* traffic trivially crosses a distinct-port
//     threshold meant to catch new connection attempts (see
//     isTrackableConnState's own doc comment, and this issue's
//     characterization test TestScanAndSpikeIgnoreEstablishedTraffic in
//     internal/detect, which now pins only activity_spike's half of the
//     same guarantee).
//   - Key: KeyPerSource (e.SrcIP) -- one window per source, matching
//     detect.go's sourceWindow.
//   - CountingMode: distinct on FieldDestinationPort -- distinctFieldValue
//     (conditions.go) already excludes DstPort == 0 from ever being added
//     (returns ok=false), which is exactly the "port 0 ... a query-time
//     filter" exclusion detect.go's own portFilter used to apply by hand.
//   - DetailTemplate: "{PortCount} distinct destination ports in <window>"
//     -- RenderEmission has no {Window} token (emission.go's own doc
//     comment: only Ports/Hosts/Labels and their counts), so window's
//     literal String() form is baked into the template text at
//     construction time, exactly reproducing detect.go's
//     `fmt.Sprintf("%d distinct destination ports in %s", portCount,
//     window)`.
//
// Scope enforcement (Hosts/HostsMode, Classification) is handled
// uniformly by DeclarativeDefinition.Evaluate itself (see
// scope_match.go), not by anything in this function.
func buildPortScanDefinition(def Definition) (*DeclarativeDefinition, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}

	conds := []Condition{
		{Field: FieldConnectionState, Operator: OpInSet, Values: []string{"", "new"}},
	}
	template := fmt.Sprintf("{PortCount} distinct destination ports in %s", window)

	return NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         window,
		Threshold:      threshold,
		CountingMode:   CountingDistinct,
		DistinctField:  FieldDestinationPort,
		DetailTemplate: template,
		// Ports only: internal/detect's old port_scan raised
		// flags.Evidence{Ports: ...} and nothing else. Declaring the
		// category explicitly is what keeps that true -- see
		// DeclarativeSpec.Evidence.
		Evidence: []EvidenceField{EvidencePorts},
	})
}

// paramPortList reads an already-ValidateParams-normalized ParamTypePortList
// value -- validateIntListParam (params.go) normalizes it to a Go []int.
func paramPortList(params Params, name string) ([]int, error) {
	v, ok := params[name].([]int)
	if !ok {
		return nil, fmt.Errorf("engine: param %q is not a port list (got %T)", name, params[name])
	}
	if len(v) == 0 {
		return nil, fmt.Errorf("engine: param %q is empty -- a declarative definition requires at least one condition value", name)
	}
	return v, nil
}

// buildCriticalPortDefinition builds critical_port's live
// DeclarativeDefinition from a stored Definition -- the shipped
// declarative counterpart of internal/detect's old observeCriticalPort
// (detect.go, before issue #405 removed it): "N attempts against a
// critical port from one external source within the window."
//
//   - Conditions: connectionState in {"", "new"} (see
//     buildPortScanDefinition's own doc comment); sourceAddress
//     matchesClassification "external" -- internal/detect's own
//     isPublic(e.SrcIP) gate, which classificationMatches (conditions.go)
//     reproduces exactly (same isPublicIP formula); destinationPort in
//     the operator-configured critical-ports list (the "ports" param,
//     CriticalPortParamSchema) -- Scope.Ports/PortsMode (scope_match.go)
//     then layers a further, operator-narrowable restriction on top of
//     this list, exactly matching Scope's own doc comment ("restricts the
//     *effective* subset of the global Config.CriticalPorts list").
//   - Key: KeyPerSource (e.SrcIP) -- one window per source, matching
//     detect.go's criticalHits map, so attempts against several different
//     critical ports from the same source still aggregate into one
//     episode (settings.go's own field-usage table documents this as
//     deliberate).
//   - CountingMode: total -- every matching attempt counts, regardless of
//     which critical port it targeted (unlike port_scan, this is not
//     about port *breadth*).
//   - DetailTemplate: "{Count} attempts against critical ports {Ports} in
//     <window>" -- issue #405/#379's fix: recordEvidence (declarative.go)
//     already accumulates every distinct critical port this source
//     touched into the same per-source EvidenceSet regardless of
//     CountingMode, so {Ports} is the honest set, not (as
//     internal/detect's old Detail string wrote) only the single
//     triggering event's port -- see
//     TestCharacterizationCriticalPort_DetailNamesTheSetOfPortsTouched in
//     internal/detect/characterization_test.go, which replaces the
//     old TestCharacterizationCriticalPort_DetailNamesOnlyTheLastPort
//     pin of this exact wrong behaviour.
func buildCriticalPortDefinition(def Definition) (*DeclarativeDefinition, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	ports, err := paramPortList(params, "ports")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped declarative definition %q: %w", def.ID, err)
	}
	portValues := make([]string, len(ports))
	for i, p := range ports {
		portValues[i] = strconv.Itoa(p)
	}

	conds := []Condition{
		{Field: FieldConnectionState, Operator: OpInSet, Values: []string{"", "new"}},
		{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"external"}},
		{Field: FieldDestinationPort, Operator: OpInSet, Values: portValues},
	}
	template := fmt.Sprintf("{Count} attempts against critical ports {Ports} in %s", window)

	return NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         window,
		Threshold:      threshold,
		CountingMode:   CountingTotal,
		DetailTemplate: template,
		// Ports: #379's fix. internal/detect's old critical_port raised
		// flags.Evidence{} (empty) while naming a single port in its
		// Detail; the accumulated port set is what both the sentence and
		// the Evidence now carry.
		Evidence: []EvidenceField{EvidencePorts},
	})
}
