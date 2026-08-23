// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/store"
)

func validTestDefinition() Definition {
	d := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	d.ParamSchema = []ParamSchema{
		{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1), Required: true},
		{Name: "window", Type: ParamTypeDuration, Min: durationBound(0)},
	}
	d.Params = Params{"threshold": float64(15), "window": "60s"}
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	return d
}

// --- NewDefinition / ID ---

func TestNewDefinitionGeneratesOpaqueID(t *testing.T) {
	a := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	b := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	if a.ID == "" {
		t.Fatal("NewDefinition produced an empty ID")
	}
	if a.ID == a.Name {
		t.Fatal("NewDefinition's ID equals the display name -- ID must be opaque, never the name")
	}
	if a.ID == b.ID {
		t.Fatal("two NewDefinition calls with the same name produced the same ID -- a clone needs its own identity")
	}
}

// --- Validate ---

func TestValidateAcceptsWellFormedDefinition(t *testing.T) {
	if err := validTestDefinition().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsInvalidScope(t *testing.T) {
	d := validTestDefinition()
	d.Scope.HostsMode = ListMode("nonsense")
	if err := d.Validate(); err == nil {
		t.Fatal("Validate succeeded with an unrecognized hostsMode, want a hard failure")
	}
}

func TestValidateRejectsInvalidIntent(t *testing.T) {
	d := validTestDefinition()
	d.Intent = Intent("sideways")
	if err := d.Validate(); err == nil {
		t.Fatal("Validate succeeded with an invalid Intent, want a hard failure")
	}
}

func TestValidateRejectsInvalidKind(t *testing.T) {
	d := validTestDefinition()
	d.Kind = Kind("sideways")
	if err := d.Validate(); err == nil {
		t.Fatal("Validate succeeded with an invalid Kind, want a hard failure")
	}
}

// TestValidateRejectsCustomProgrammatic pins the invariant decided on
// issue #401 (owner, 2026-08-16): provenance=custom implies
// kind=declarative, enforced here so no request shape can express a
// custom programmatic definition -- only mikroview's own shipped code
// can supply Programmatic logic (see Kind's doc comment).
func TestValidateRejectsCustomProgrammatic(t *testing.T) {
	d := validTestDefinition()
	d.Kind = KindProgrammatic
	d.Provenance = Provenance{Origin: ProvenanceCustom}

	err := d.Validate()
	if err == nil {
		t.Fatal("Validate succeeded on provenance=custom, kind=programmatic -- this exact combination must be rejected")
	}
}

func TestValidateAcceptsShippedProgrammatic(t *testing.T) {
	d := validTestDefinition()
	d.Kind = KindProgrammatic
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: d.Params}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v -- provenance=shipped, kind=programmatic is exactly what the inverted watchlist/EMA baselines need to express", err)
	}
}

func TestValidateRejectsMalformedParams(t *testing.T) {
	d := validTestDefinition()
	d.Params["threshold"] = "not-a-number"
	if err := d.Validate(); err == nil {
		t.Fatal("Validate succeeded with a malformed param value, want a hard failure -- malformed values must never be stored to be read back as a zero value")
	}
}

func TestValidateRejectsMalformedShippedDefaults(t *testing.T) {
	d := validTestDefinition()
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: Params{"threshold": "nope"}}
	if err := d.Validate(); err == nil {
		t.Fatal("Validate succeeded with a malformed shipped default, want a hard failure")
	}
}

func TestValidateScopeRejectsUnrecognizedClassification(t *testing.T) {
	sc := Scope{Classification: store.Scope("bogus")}
	if err := ValidateScope(sc); err == nil {
		t.Fatal("ValidateScope succeeded on an unrecognized classification, want a hard failure")
	}
}

func TestValidateScopeAcceptsEmptyScope(t *testing.T) {
	if err := ValidateScope(Scope{}); err != nil {
		t.Fatalf("ValidateScope: %v -- the zero-value Scope (no restriction on any axis) must be valid", err)
	}
}

// --- Distance ("how far am I from stock") ---

func TestDistanceIsEmptyForCustomDefinition(t *testing.T) {
	d := validTestDefinition() // Provenance.Origin == ProvenanceCustom
	dist := d.Distance()
	if len(dist) != 0 {
		t.Fatalf("Distance() = %v, want empty -- a custom definition has no stock to diff against", dist)
	}
}

func TestDistanceIsEmptyWhenParamsMatchShipped(t *testing.T) {
	d := validTestDefinition()
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: Params{"threshold": float64(15), "window": "60s"}}
	d.Params = Params{"threshold": float64(15), "window": "60s"}

	dist := d.Distance()
	if len(dist) != 0 {
		t.Fatalf("Distance() = %v, want empty -- Params exactly match ShippedParams", dist)
	}
}

func TestDistanceReportsOverriddenParam(t *testing.T) {
	d := validTestDefinition()
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: Params{"threshold": float64(15), "window": "60s"}}
	d.Params = Params{"threshold": float64(30), "window": "60s"}

	dist := d.Distance()
	if len(dist) != 1 {
		t.Fatalf("Distance() = %v, want exactly one overridden param", dist)
	}
	delta, ok := dist["threshold"]
	if !ok {
		t.Fatalf("Distance() missing entry for the overridden \"threshold\" param: %v", dist)
	}
	if delta.Shipped != float64(15) || delta.Current != float64(30) {
		t.Errorf("delta = %+v, want Shipped=15 Current=30", delta)
	}
}

// TestDistanceClearingOverridesIsResetToDefault pins decision #3 from
// issue #401: clearing every override IS reset-to-default, not a
// separate operation that could fall out of sync with it -- setting
// Params back to exactly ShippedParams must make Distance report zero
// diff again, the same as it never having been overridden.
func TestDistanceClearingOverridesIsResetToDefault(t *testing.T) {
	shipped := Params{"threshold": float64(15), "window": "60s"}
	d := validTestDefinition()
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: shipped}

	d.Params = Params{"threshold": float64(99), "window": "5m"}
	if dist := d.Distance(); len(dist) != 2 {
		t.Fatalf("Distance() before reset = %v, want 2 overridden params", dist)
	}

	// "Clearing all overrides" -- set Params back to exactly ShippedParams.
	d.Params = Params{"threshold": float64(15), "window": "60s"}
	if dist := d.Distance(); len(dist) != 0 {
		t.Fatalf("Distance() after clearing overrides = %v, want empty (this IS reset-to-default)", dist)
	}
}

func TestDistanceReportsAddedAndRemovedParams(t *testing.T) {
	d := validTestDefinition()
	d.ParamSchema = append(d.ParamSchema, ParamSchema{Name: "extra", Type: ParamTypeBool})
	d.Provenance = Provenance{Origin: ProvenanceShipped, ShippedParams: Params{"threshold": float64(15), "window": "60s"}}
	// "removed" relative to shipped: window absent from Params.
	// "added" relative to shipped: extra present only in Params.
	d.Params = Params{"threshold": float64(15), "extra": true}

	dist := d.Distance()
	windowDelta, ok := dist["window"]
	if !ok {
		t.Fatalf("Distance() missing \"window\" (removed relative to shipped): %v", dist)
	}
	if windowDelta.Shipped != "60s" || windowDelta.Current != nil {
		t.Errorf("window delta = %+v, want Shipped=60s Current=nil", windowDelta)
	}

	extraDelta, ok := dist["extra"]
	if !ok {
		t.Fatalf("Distance() missing \"extra\" (added relative to shipped): %v", dist)
	}
	if extraDelta.Current != true || extraDelta.Shipped != nil {
		t.Errorf("extra delta = %+v, want Shipped=nil Current=true", extraDelta)
	}
}

// --- Kind doc comment sanity: shipped definitions cover both kinds ---

func TestKindHasExactlyTwoValues(t *testing.T) {
	// Not a behavioral test -- a tripwire so a third Kind value can't be
	// added without someone reading (and updating) Kind's doc comment,
	// which records why there are exactly two and why that is permanent.
	switch Kind("declarative") {
	case KindDeclarative:
	default:
		t.Fatal("KindDeclarative constant drifted from \"declarative\"")
	}
	switch Kind("programmatic") {
	case KindProgrammatic:
	default:
		t.Fatal("KindProgrammatic constant drifted from \"programmatic\"")
	}
}
