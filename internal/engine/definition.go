// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"

	"github.com/tomlawesome/mikroview/internal/store"
)

// Intent decides what an Evaluate call's emission feeds -- nothing else.
// docs/decisions/evaluation-engine.md section 3: the UI keeps Detect and
// Expect visually distinct because operators reason in those two modes,
// but the engine itself does not branch on Intent anywhere except the
// router (see Route) -- a definition is a definition.
type Intent string

const (
	// IntentDetection feeds the flag lifecycle (internal/flags):
	// raise/re-fire/clear, count, confidence, reputation floor,
	// exclusions.
	IntentDetection Intent = "detection"
	// IntentExpectation feeds the match log (internal/matchlog):
	// observations, promotions, violations.
	IntentExpectation Intent = "expectation"
)

// Kind is how a definition's firing logic is expressed. Both kinds
// satisfy the same Evaluated interface (see engine.go) and carry the
// same envelope (Definition, below) -- the chassis branches on Kind
// nowhere except at construction time, when something builds the
// concrete Evaluated a Definition's Kind calls for.
//
// docs/decisions/evaluation-engine.md section 2 records why there are
// two, and why Programmatic is permanent rather than a stepping stone
// to "eventually everything is declarative":
//
//   - Declarative: conditions + window + threshold + emission,
//     expressed as data (structured match conditions -- field, operator,
//     value -- never a DSL, per the ADR's "No DSL" decision). What the
//     builder UI edits; what "fully custom detectors" means.
//   - Programmatic: built-in Go, wearing the same envelope, whose logic
//     stays code because some of what mikroview does cannot honestly be
//     a form: statistical baselines (the EMA confidence machinery),
//     absence-of-events detectors ("nothing arrived" is not a predicate
//     over an event -- there is no event for a condition to match
//     against), external-data lookups (reputation), and the inverted
//     watchlist's observed/permitted/violation state machine with live
//     SourceList resolution. Pretending these are declarative would
//     either dumb them down or turn the condition language into a
//     programming language -- both worse than saying plainly that some
//     definitions are built in.
//
// A definition's Kind is fixed at creation (see the custom-implies-
// declarative invariant on Provenance/Definition.Validate): only a
// shipped definition may be Programmatic, because only mikroview's own
// code -- not an operator authoring through the builder UI -- can supply
// the Go logic a Programmatic Kind requires.
type Kind string

const (
	KindDeclarative  Kind = "declarative"
	KindProgrammatic Kind = "programmatic"
)

// ListMode selects allow-vs-deny semantics for one Scope list axis --
// moved from internal/detect.ListMode unchanged in meaning (see that
// package's own copy, kept until #405 ports it onto this one). The zero
// value ("") behaves like ListModeAllow on an empty list: mode is
// irrelevant when the list itself is empty, since allow-of-nothing would
// suppress everything.
type ListMode string

const (
	ListModeAllow ListMode = "allow"
	ListModeDeny  ListMode = "deny"
)

func isValidListMode(m ListMode) bool {
	return m == "" || m == ListModeAllow || m == ListModeDeny
}

// Scope restricts which events a definition reacts to, beyond its own
// match/threshold logic -- moved from internal/detect.Scope unchanged in
// meaning (issue #401; internal/detect keeps its own copy untouched
// until #405 ports detectors onto this package). One deliberate
// superset struct covering every axis (hosts, ports, source
// classification, rule labels) rather than one bespoke struct per
// definition shape -- a definition's own evaluation code consults only
// the fields meaningful to its own signature and ignores the rest.
// Multiple active axes combine with AND (issue #44's decision, carried
// forward here); within one axis, Mode == ListModeDeny excludes a
// match, ListModeAllow (or unset, on a non-empty list) means only
// listed entries are admitted.
//
// Hosts entries accept a bare IP or a CIDR, mirroring store.Query.IP's
// existing convention.
type Scope struct {
	Hosts          []string    `json:"hosts,omitempty"`
	HostsMode      ListMode    `json:"hostsMode,omitempty"`
	Ports          []int       `json:"ports,omitempty"`
	PortsMode      ListMode    `json:"portsMode,omitempty"`
	Classification store.Scope `json:"classification,omitempty"`
	Rules          []string    `json:"rules,omitempty"`
	RulesMode      ListMode    `json:"rulesMode,omitempty"`
}

// ValidateScope rejects an unrecognized mode or classification value --
// the API layer's guard against a malformed request being silently
// stored as "no restriction" (every field's zero value). Moved from
// internal/detect.ValidateScope unchanged in meaning.
func ValidateScope(sc Scope) error {
	if !isValidListMode(sc.HostsMode) {
		return fmt.Errorf("engine: invalid hostsMode %q", sc.HostsMode)
	}
	if !isValidListMode(sc.PortsMode) {
		return fmt.Errorf("engine: invalid portsMode %q", sc.PortsMode)
	}
	if !isValidListMode(sc.RulesMode) {
		return fmt.Errorf("engine: invalid rulesMode %q", sc.RulesMode)
	}
	switch sc.Classification {
	case store.ScopeAny, store.ScopeInternal, store.ScopeExternal:
	default:
		return fmt.Errorf("engine: invalid classification %q", sc.Classification)
	}
	return nil
}

// hostEntryValid reports whether entry is a bare IP or CIDR -- the same
// acceptance rule hostEntryMatches (internal/detect/settings.go) applies
// at match time, used here only to validate a hostList param value (see
// params.go), not to evaluate anything.
func hostEntryValid(entry string) bool {
	if _, _, err := net.ParseCIDR(entry); err == nil {
		return true
	}
	return net.ParseIP(entry) != nil
}

// ProvenanceOrigin records where a definition came from.
type ProvenanceOrigin string

const (
	// ProvenanceShipped is a definition mikroview ships -- disabled by
	// default (see Definition's own doc comment on that policy),
	// editable, and never deleted, only ever disabled. Its
	// Provenance.ShippedParams holds the params it shipped with, so
	// Definition.Distance is always answerable even after an operator
	// edits it.
	ProvenanceShipped ProvenanceOrigin = "shipped"
	// ProvenanceCustom is a definition an operator authored from
	// scratch (the builder UI). Always Kind == KindDeclarative -- see
	// Definition.Validate's custom-implies-declarative invariant.
	ProvenanceCustom ProvenanceOrigin = "custom"
)

// Provenance is a definition's origin, and -- for a shipped definition
// -- the params it shipped with. This is deliberately the *only* place
// "stock" is recorded: Definition.Distance is a pure function over
// ShippedParams and the definition's current Params, not a separately
// maintained override list that could drift from what it claims to
// describe. Clearing every override (setting Params back to exactly
// ShippedParams) and "resetting to default" are therefore the same
// state, not two operations that have to be kept in sync -- see
// Distance's own doc comment.
type Provenance struct {
	Origin ProvenanceOrigin `json:"origin"`
	// ShippedParams is empty/omitted for ProvenanceCustom (a custom
	// definition has no stock to diff against) and holds the as-shipped
	// default Params for ProvenanceShipped.
	ShippedParams Params `json:"shippedParams,omitempty"`
}

// Definition is the one envelope every evaluated thing carries,
// whatever its Kind or Intent -- docs/decisions/evaluation-engine.md
// section 2. This issue (#401) is the envelope's contract: no
// evaluation semantics ride with it yet (no match conditions, no
// programmatic logic is wired to a Definition here) -- see Evaluated
// (engine.go) for the minimal interface a concrete, kind-specific
// implementation satisfies once #404/#405/#406 exist.
//
// Two invariants that do not yet have anywhere else to live are recorded
// here rather than silently assumed, per owner decisions recorded on
// #401 (2026-08-16):
//
//  1. Shipped definitions are never deleted, only ever disabled
//     (Enabled = false). There is no Delete operation on this envelope
//     -- #404's store is what enforces this (refusing, or no-op'ing, a
//     delete request against a Provenance.Origin == ProvenanceShipped
//     definition) and #407's API is what surfaces that refusal to a
//     caller. Deleting a ProvenanceCustom definition is unconstrained by
//     this invariant.
//  2. provenance=custom implies kind=declarative -- see Validate. No
//     request shape may express a custom programmatic definition: only
//     mikroview's own shipped code can supply Programmatic logic (see
//     Kind's doc comment), so a custom definition is always Declarative.
type Definition struct {
	// ID is stable, opaque and generated (see NewDefinition) -- never
	// the display name. A clone of a shipped definition needs its own
	// identity: cloning is #404's job, but whatever does it must call
	// NewDefinition (or equivalent) for the clone's ID rather than
	// reusing the original's, the same way any other entity in this
	// codebase with a generated ID works (internal/auth.newID,
	// internal/api's own watchlist entry ID).
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Intent decides only what an emission feeds -- see Intent's own
	// doc comment. Never consulted for anything else.
	Intent Intent `json:"intent"`
	Kind   Kind   `json:"kind"`
	// Enabled off does not remove state (baseline, evidence) already
	// accumulated -- re-enabling resumes rather than starts cold. That
	// behavior belongs to whatever wires a Definition onto the engine
	// (#404/#405); this envelope only carries the flag.
	Enabled bool  `json:"enabled"`
	Scope   Scope `json:"scope,omitzero"`
	// Params is this definition's current, typed configuration -- what
	// the UI renders and what auto-tune adjusts (docs/decisions/
	// evaluation-engine.md section 2), validated against ParamSchema at
	// the boundary (see ValidateParams) rather than being trusted
	// as-is.
	Params Params `json:"params,omitempty"`
	// ParamSchema declares every param this definition accepts -- "per-
	// definition schema" per the ADR. See params.go.
	ParamSchema []ParamSchema `json:"paramSchema,omitempty"`
	Provenance  Provenance    `json:"provenance"`
	// Detection is the structure an operator-authored detector needs and
	// nothing else does: its conditions and the aggregation around them
	// (issue #502). Set on exactly the definitions that are all three of
	// intent=detection, kind=declarative and provenance=custom, and
	// absent everywhere else -- Validate enforces both directions, which
	// is why this is an intent-specific block rather than five top-level
	// fields that would be meaningless on every other definition. See
	// DetectionSpec for the structure/tunable split and why nothing in
	// it serialises non-deterministically.
	Detection *DetectionSpec `json:"detection,omitempty"`
}

// newDefinitionID returns a random 32-character hex string. Mirrors
// internal/auth's own newID() and internal/api's own
// newWatchlistEntryID() -- each package keeps a small private copy of
// this rather than sharing one, the existing precedent in this
// codebase for an ID with no natural operator-chosen key.
func newDefinitionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("engine: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// NewDefinition constructs a Definition with a freshly generated ID --
// the only sanctioned way to obtain one: never the display name, never
// copied from another definition (see Definition.ID's own doc comment
// on why a clone needs its own identity too). The result still needs
// Scope/Params/Provenance/etc. filled in and Validate called before use.
func NewDefinition(name string, intent Intent, kind Kind) Definition {
	return Definition{
		ID:     newDefinitionID(),
		Name:   name,
		Intent: intent,
		Kind:   kind,
	}
}

// Validate checks d's structural invariants -- everything this envelope
// itself is responsible for enforcing, independent of any store or API
// layer built on top of it (#404/#407 call this, they do not
// re-implement it):
//
//   - Scope is well-formed (ValidateScope).
//   - Intent is one of the two known values.
//   - provenance=custom implies kind=declarative -- pinned by
//     TestValidateRejectsCustomProgrammatic. No request shape can
//     express a custom programmatic definition: this is the one place
//     that invariant is actually enforced, so nothing downstream can
//     bypass it by constructing a Definition value directly.
//   - Params and Provenance.ShippedParams (when set) both validate
//     against ParamSchema -- malformed values are rejected here, never
//     stored to be read as a zero value later.
//   - A Detection block, where one is present, belongs to a custom
//     detection and is itself well-formed -- see validateDetectionBlock.
func (d Definition) Validate() error {
	if err := ValidateScope(d.Scope); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.ID, err)
	}
	switch d.Intent {
	case IntentDetection, IntentExpectation:
	default:
		return fmt.Errorf("engine: definition %q: invalid intent %q", d.ID, d.Intent)
	}
	switch d.Kind {
	case KindDeclarative, KindProgrammatic:
	default:
		return fmt.Errorf("engine: definition %q: invalid kind %q", d.ID, d.Kind)
	}
	if d.Provenance.Origin == ProvenanceCustom && d.Kind != KindDeclarative {
		return fmt.Errorf("engine: definition %q: provenance=custom requires kind=declarative -- programmatic definitions are shipped-only (see Kind's doc comment); no request shape may express a custom programmatic definition", d.ID)
	}
	if err := d.validateDetectionBlock(); err != nil {
		return err
	}
	if _, err := ValidateParams(d.ParamSchema, d.Params); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.ID, err)
	}
	if d.Provenance.Origin == ProvenanceShipped && len(d.Provenance.ShippedParams) > 0 {
		if _, err := ValidateParams(d.ParamSchema, d.Provenance.ShippedParams); err != nil {
			return fmt.Errorf("engine: definition %q: shipped defaults: %w", d.ID, err)
		}
	}
	return nil
}

// validateDetectionBlock checks who may carry a Detection block, and
// that the one they carry is well-formed.
//
// Only a custom detection may: on a shipped definition the structure is
// the Go builder's, and on an expectation it is fixed by
// BuildExpectationDefinition, so a block on either would be data that
// looked authoritative and was never read.
//
// The other direction -- that a custom detection *must* carry one -- is
// deliberately not enforced here. It binds where a definition is stored
// (DefinitionsStore.Upsert), not on the envelope, because a definition
// built in-process is handed its structure directly as a DeclarativeSpec
// and the block would be a second copy of it. What makes the block
// mandatory is persistence: a stored definition is rebuilt from its
// bytes alone, so a stored custom detection without one would list and
// evaluate nothing.
func (d Definition) validateDetectionBlock() error {
	if d.Detection == nil {
		return nil
	}
	if d.Provenance.Origin != ProvenanceCustom || d.Intent != IntentDetection {
		return fmt.Errorf("engine: definition %q: a detection block belongs only to a custom detection definition, and this one is intent=%q provenance=%q", d.ID, d.Intent, d.Provenance.Origin)
	}
	if err := d.Detection.Validate(); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.ID, err)
	}
	return nil
}

// ParamDelta is one param's distance from its shipped default -- see
// Definition.Distance. Shipped/Current are nil when the param is absent
// on that side (added or removed since shipping), not merely zero-
// valued, so "never configured" and "explicitly set to the zero value"
// stay distinguishable.
type ParamDelta struct {
	Shipped any `json:"shipped,omitempty"`
	Current any `json:"current,omitempty"`
}

// Distance reports how far d's current Params are from its shipped
// defaults, keyed by param name -- "how far am I from stock" as a pure
// function over the envelope (docs/decisions/evaluation-engine.md
// section 2), computed fresh from Provenance.ShippedParams and Params
// rather than read from a separately maintained override list that
// could drift from the state it's supposed to describe.
//
// Only meaningful for d.Provenance.Origin == ProvenanceShipped; a
// custom definition has no stock to diff against, and Distance returns
// an empty (non-nil) map for it rather than an error -- there is
// nothing wrong with asking, the answer is just "nothing to compare."
//
// A param present on only one side (added or removed since shipping) is
// reported too. Clearing every override -- setting Params back to
// exactly ShippedParams -- makes Distance return an empty map: that IS
// what "reset to default" means here, not a separate operation that
// could fall out of sync with this one.
func (d Definition) Distance() map[string]ParamDelta {
	out := map[string]ParamDelta{}
	if d.Provenance.Origin != ProvenanceShipped {
		return out
	}

	names := make(map[string]struct{}, len(d.Params)+len(d.Provenance.ShippedParams))
	for name := range d.Params {
		names[name] = struct{}{}
	}
	for name := range d.Provenance.ShippedParams {
		names[name] = struct{}{}
	}

	for name := range names {
		shipped, hasShipped := d.Provenance.ShippedParams[name]
		current, hasCurrent := d.Params[name]
		if hasShipped && hasCurrent && paramValueEqual(shipped, current) {
			continue
		}
		delta := ParamDelta{}
		if hasShipped {
			delta.Shipped = shipped
		}
		if hasCurrent {
			delta.Current = current
		}
		out[name] = delta
	}
	return out
}

// paramValueEqual compares two param values for Distance's purposes via
// their canonical JSON encoding rather than reflect.DeepEqual -- Params
// values may arrive either as native Go values (int, []int, ...), set
// programmatically by whatever seeds a shipped definition's
// ShippedParams, or as the float64/[]interface{}/... shapes
// encoding/json produces when a definition is decoded off the wire or
// out of a store; DeepEqual would treat int(5) and float64(5) as
// different when they are not, for this purpose. json.Marshal already
// produces a stable byte order for map keys, so two values compare
// equal here exactly when they would be indistinguishable on the wire.
func paramValueEqual(a, b any) bool {
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	if aerr != nil || berr != nil {
		return false
	}
	return string(ab) == string(bb)
}
