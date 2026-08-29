// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// DetectionSpec is what a custom detection definition carries on its
// envelope that no other definition needs: the structure of the detector
// itself.
//
// The split it embodies (issue #502, and the addendum to
// docs/decisions/evaluation-engine.md) is between *structure* and
// *tunables*. Structure -- what the detector is -- lives here, and
// changes by editing the definition. Tunables -- how sensitive it is --
// stay in Params behind SetParams, exactly as a shipped declarative
// detector's threshold and window already do, so one params editor tunes
// every detector and refuseIdentityChange's guarantees keep applying
// uniformly. A shipped detector draws the same line; the only difference
// is that its structure is hard-coded in Go, because it has a builder to
// hold it and a custom detection has none.
//
// Set only on a definition that is all three of intent=detection,
// kind=declarative and provenance=custom -- Definition.Validate enforces
// both directions of that, so a shipped definition can never carry one
// and a custom detection can never lack one.
//
// Every field serialises deterministically: ordered slices and strings,
// no maps, no floats, and no durations (the one duration a detector
// needs, its window, is a Param). That is a hard requirement rather than
// a preference -- Registry.Sync decides whether a definition changed by
// byte-comparing its stored JSON, so a field whose encoding varied
// between two saves of the same value would silently drop the detector's
// accumulated window state on an unrelated edit.
type DetectionSpec struct {
	// Conditions is the match language, unchanged from the one
	// expectations and shipped declarative detectors already use --
	// there is no second condition language in this codebase, and the
	// evaluation-engine ADR's "No DSL" decision forbids inventing a
	// detection-only one.
	Conditions []Condition `json:"conditions"`
	// Key, Counting and DistinctField are the aggregation around those
	// conditions -- the part that actually differs between an
	// expectation (which pins all three in Go) and a detection (which
	// must be able to vary them).
	Key           KeyMode      `json:"key"`
	Counting      CountingMode `json:"counting"`
	DistinctField Field        `json:"distinctField,omitempty"`
	// DetailTemplate is the sentence a raised flag shows. It is operator
	// input rendered into the UI, so its placeholders are a closed,
	// validated set resolved when the definition is created -- see
	// ValidateDetectionDetailTemplate.
	DetailTemplate string `json:"detailTemplate"`
}

// detectionCountToken is the one always-available placeholder: the
// threshold-crossing tally, which every detector has by construction
// because it only emits once its threshold has been crossed.
const detectionCountToken = "Count"

// Validate checks everything about a DetectionSpec that does not depend
// on a live engine: the conditions compile, the key and counting modes
// are known, DistinctField is present exactly when counting is distinct,
// and the detail template names only placeholders this detector can
// actually resolve.
//
// Deliberately callable on stored bytes alone. DefinitionsStore uses it
// to decide whether this binary can make sense of a stored detection at
// all (see decodeStored): a detection naming a condition field, an
// operator or a key mode from a newer build is surfaced as unavailable
// -- preserved, listed, never evaluated -- rather than failing to build
// on every sync.
func (s *DetectionSpec) Validate() error {
	if s == nil {
		return fmt.Errorf("engine: a custom detection requires a detection block")
	}
	if _, err := compileConditions(s.Conditions); err != nil {
		return err
	}
	if err := validateKeyMode(s.Key); err != nil {
		return err
	}
	switch s.Counting {
	case CountingTotal:
		// Rejected rather than ignored: DeclarativeSpec tolerates a
		// stray DistinctField for CountingTotal, but a stored one would
		// be a field an operator set and the detector never reads, which
		// is the kind of quietly-meaningless state this envelope should
		// not be able to hold.
		if s.DistinctField != "" {
			return fmt.Errorf("engine: countingMode=total takes no distinctField, got %q", s.DistinctField)
		}
	case CountingDistinct:
		if !distinctCountableFields[s.DistinctField] {
			return fmt.Errorf("engine: countingMode=distinct requires a countable distinctField, got %q", s.DistinctField)
		}
	default:
		return fmt.Errorf("engine: invalid countingMode %q", s.Counting)
	}
	return ValidateDetectionDetailTemplate(s.Key, s.DetailTemplate)
}

// ValidateDetectionDetailTemplate checks that tmpl names only
// placeholders a custom detection can resolve: {Count}, plus the
// key-component tokens key actually supplies ({SourceAddress},
// {DestinationAddress}, {DestinationPort} -- see keyFieldValues).
//
// The set is closed, and checked here at create time rather than at
// emission time, for two reasons. A template naming something that never
// resolves is RenderEmission's hard error, which would mean a detector
// that stores and lists and then fails the moment it should have fired
// -- the "exists but evaluates nothing" failure this whole feature had
// to avoid. And this is the one genuinely new attack surface the feature
// adds: the template is operator text that reaches the interface, so the
// placeholder vocabulary is a fixed list of names, never an expression
// to evaluate. Nothing here interpolates event data by itself; the
// substitution is RenderEmission's existing hand-rolled one over a fixed
// field set, which docs/decisions/injection-audit.md already covers.
//
// The evidence tokens ({Ports}, {Hosts}, {Labels} and their counts) are
// deliberately absent: a DetectionSpec declares no evidence categories,
// so nothing would ever accumulate them.
func ValidateDetectionDetailTemplate(key KeyMode, tmpl string) error {
	if tmpl == "" {
		return fmt.Errorf("engine: detailTemplate is required")
	}
	allowed := detectionTemplateTokens(key)
	var unknown []string
	seen := make(map[string]struct{})
	for _, m := range emissionToken.FindAllStringSubmatch(tmpl, -1) {
		name := m[1]
		if _, ok := allowed[name]; ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		unknown = append(unknown, name)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf(
		"engine: detailTemplate names %v, which a custom detection keyed %q cannot resolve -- the placeholders available to it are %v",
		unknown, key, detectionTemplateTokenNames(key))
}

// detectionTemplateTokens is the closed placeholder set for one key
// mode. Derived from keyFieldValues rather than restating its switch, so
// a key mode that gains or loses a component can never leave this
// validation describing the old shape.
func detectionTemplateTokens(key KeyMode) map[string]struct{} {
	d := &DeclarativeDefinition{key: key}
	out := map[string]struct{}{detectionCountToken: {}}
	for name := range d.keyFieldValues(store.Event{}) {
		out[name] = struct{}{}
	}
	return out
}

// detectionTemplateTokenNames is detectionTemplateTokens sorted, for an
// error message that tells an operator what they may write instead of
// only what they may not.
func detectionTemplateTokenNames(key KeyMode) []string {
	tokens := detectionTemplateTokens(key)
	out := make([]string, 0, len(tokens))
	for name := range tokens {
		out = append(out, "{"+name+"}")
	}
	sort.Strings(out)
	return out
}

// customDetectionParamSchema is the schema every custom detection
// carries. Two params, the same two every shipped declarative detector
// already exposes under the same names, so the existing params editor
// and auto-tune see nothing new.
var customDetectionParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "events", Min: floatBound(countParamMin), Required: true,
		Description: "How many counted events within the window raise this detector."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the count is measured over."},
}

// CustomDetectionParamSchema returns the param schema a custom detection
// is created with -- a fresh copy per call, per this package's
// copy-on-read contract, so a caller storing it on a Definition can
// never reach back into the package's own value.
func CustomDetectionParamSchema() []ParamSchema {
	out := make([]ParamSchema, len(customDetectionParamSchema))
	copy(out, customDetectionParamSchema)
	return out
}

// BuildCustomDetectionDefinition builds the live logic for an
// operator-authored detection: the conditions and aggregation come from
// the envelope's Detection block, the threshold and window from its
// Params.
//
// This is the one builder that reads its whole shape from stored data.
// Shipped builders keep hard-coding theirs in Go deliberately (see
// shipped_declarative.go) -- what is generalised here is assembling a
// DeclarativeSpec, not the builders themselves.
//
// Evidence: none, so the emission claims no port, host or label set it
// did not accumulate. members is passed through for the same reason
// BuildExpectationDefinition takes it: a FieldAddressListMembership
// condition needs a resolver, and nil simply never matches.
func BuildCustomDetectionDefinition(def Definition, members AddressListMembership) (*DeclarativeDefinition, error) {
	if def.Intent != IntentDetection {
		return nil, fmt.Errorf("engine: custom detection %q has intent %q, want %q", def.ID, def.Intent, IntentDetection)
	}
	if def.Provenance.Origin != ProvenanceCustom {
		return nil, fmt.Errorf("engine: definition %q is not custom, so it has no detection block to build from", def.ID)
	}
	if err := def.Detection.Validate(); err != nil {
		return nil, fmt.Errorf("engine: custom detection %q: %w", def.ID, err)
	}
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: custom detection %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: custom detection %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: custom detection %q: %w", def.ID, err)
	}

	return NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     def.Detection.Conditions,
		Key:            def.Detection.Key,
		Window:         window,
		Threshold:      threshold,
		CountingMode:   def.Detection.Counting,
		DistinctField:  def.Detection.DistinctField,
		DetailTemplate: def.Detection.DetailTemplate,
		Members:        members,
	})
}

// CanNarrowDispatch reports whether conds give the dispatch pre-index
// something to narrow on -- a positive equals/inSet on destination port,
// chain, rule label or address classification (see discriminantFor).
//
// Exported so the API can tell an operator the truth at create time. A
// detector that cannot be narrowed is accepted, not refused: watching
// one source address is a legitimate question, and refusing it to
// protect an ingest budget the operator may not care about is the wrong
// default for an observe-only tool. But it is consulted on every single
// event, and that is worth saying out loud rather than absorbing in
// silence.
func CanNarrowDispatch(conds []Condition) bool {
	_, _, ok := discriminantFor(conds)
	return ok
}
