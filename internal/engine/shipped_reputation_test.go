// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"
)

func newShippedReputationDefinition(t *testing.T, params Params) *reputationDefinition {
	t.Helper()
	p := DefaultReputationPolicy()
	full := Params{
		"lookupConcurrency":          p.Concurrency,
		"lookupTimeout":              p.Timeout.String(),
		"groupSampleSize":            p.GroupSampleSize,
		"groupMinSignificantSamples": p.GroupMinSignificantSamples,
	}
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          "reputation",
		Name:        "Reputation enrichment",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      full,
		ParamSchema: ReputationParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(reputation): %v", err)
	}
	return built.(*reputationDefinition)
}

// TestShippedReputationDefaultsMatchInternalDetectsConstants is the pin
// that makes turning four hard-coded constants into params a
// no-behaviour-change move: the shipped defaults are exactly the values
// internal/detect's reputation.go was compiled with
// (reputationLookupConcurrency 8, reputationLookupTimeout 10s,
// reputationGroupSampleSize 10, reputationGroupMinSignificantSamples 3),
// and main.go's own hand-synced literal 8.
func TestShippedReputationDefaultsMatchInternalDetectsConstants(t *testing.T) {
	p := DefaultReputationPolicy()
	if p.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", p.Concurrency)
	}
	if p.Timeout != 10*time.Second {
		t.Errorf("Timeout = %s, want 10s", p.Timeout)
	}
	if p.GroupSampleSize != 10 {
		t.Errorf("GroupSampleSize = %d, want 10", p.GroupSampleSize)
	}
	if p.GroupMinSignificantSamples != 3 {
		t.Errorf("GroupMinSignificantSamples = %d, want 3", p.GroupMinSignificantSamples)
	}
}

// TestShippedReputationPolicyComesFromItsParams pins that the params are
// load-bearing rather than decorative.
func TestShippedReputationPolicyComesFromItsParams(t *testing.T) {
	d := newShippedReputationDefinition(t, Params{
		"lookupConcurrency":          3,
		"lookupTimeout":              (4 * time.Second).String(),
		"groupSampleSize":            5,
		"groupMinSignificantSamples": 2,
	})
	got := d.Policy()
	want := ReputationPolicy{Concurrency: 3, Timeout: 4 * time.Second, GroupSampleSize: 5, GroupMinSignificantSamples: 2}
	if got != want {
		t.Errorf("Policy() = %+v, want %+v", got, want)
	}
}

// TestReputationPolicyFromReadsTheSeededDefinition drives the accessor
// main.go actually uses, through a real definitions store seeded exactly
// as boot seeds it.
func TestReputationPolicyFromReadsTheSeededDefinition(t *testing.T) {
	store, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedShippedDefinitions(store, nil, DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	if got := ReputationPolicyFrom(store); got != DefaultReputationPolicy() {
		t.Errorf("ReputationPolicyFrom = %+v, want the shipped default %+v", got, DefaultReputationPolicy())
	}

	// The definition is where the numbers live, so the sinks read them
	// from here rather than from a literal in process wiring. Editing a
	// shipped definition's params is DefinitionsStore's own refusal today
	// (see ErrDefinitionImmutable) and becomes possible with the
	// definitions API; what this pins is that the read path is the
	// definition, which is the half that had to exist first.
	stored, ok := store.Get("reputation")
	if !ok {
		t.Fatal("expected seeding to create the reputation definition")
	}
	if stored.Definition.Params["lookupConcurrency"] == nil {
		t.Errorf("expected the seeded definition to carry the policy in its params, got %+v", stored.Definition.Params)
	}
}

// TestReputationPolicyFromFallsBackRatherThanDisablingLookups pins the
// degrade-don't-disable choice: a definitions store with no reputation
// entry (a document predating this definition) must still look things
// up, at the shipped policy.
func TestReputationPolicyFromFallsBackRatherThanDisablingLookups(t *testing.T) {
	store, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if got := ReputationPolicyFrom(store); got != DefaultReputationPolicy() {
		t.Errorf("ReputationPolicyFrom on an empty store = %+v, want the shipped default", got)
	}
	if got := ReputationPolicyFrom(nil); got != DefaultReputationPolicy() {
		t.Errorf("ReputationPolicyFrom(nil) = %+v, want the shipped default", got)
	}
}

// TestShippedReputationIsNonReplayable pins #403's first-named
// non-replayable case.
func TestShippedReputationIsNonReplayable(t *testing.T) {
	d := newShippedReputationDefinition(t, nil)

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify reputation")
	}
	if receiptCapable {
		t.Fatal("expected reputation to declare itself non-replayable")
	}
	if reason == "" {
		t.Error("a non-replayable declaration with no reason is the thing the contract exists to prevent")
	}
}
