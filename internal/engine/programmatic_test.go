// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
)

// TestProgrammaticKindIsShippedOnly pins the invariant this package's
// own doc comment states beside "No DSL", at every layer that can
// enforce it. #401 ratified it as a decision; this is what makes it a
// property of the code rather than of the reader.
//
// It matters because the two kinds deliberately share one envelope. That
// sharing is only safe while a definition's Kind is not something a
// request can choose: a programmatic definition's behaviour is Go in
// this binary, so "create a programmatic definition" would mean "run
// code I named", and "turn this declarative definition programmatic"
// would mean the same thing by a different route.
func TestProgrammaticKindIsShippedOnly(t *testing.T) {
	t.Run("Validate refuses a custom programmatic definition", func(t *testing.T) {
		d := NewDefinition("mine", IntentDetection, KindProgrammatic)
		d.Provenance = Provenance{Origin: ProvenanceCustom}
		err := d.Validate()
		if err == nil {
			t.Fatal("Validate accepted provenance=custom with kind=programmatic")
		}
		if !strings.Contains(err.Error(), "shipped-only") {
			t.Errorf("error does not say why: %v", err)
		}
	})

	t.Run("the builder refuses a non-shipped programmatic definition", func(t *testing.T) {
		d := NewDefinition("mine", IntentDetection, KindProgrammatic)
		d.Provenance = Provenance{Origin: ProvenanceCustom}
		if _, err := BuildShippedProgrammaticDefinition(d, ShippedDeps{}); err == nil {
			t.Fatal("BuildShippedProgrammaticDefinition accepted a custom definition")
		}
	})

	t.Run("the builder refuses a declarative definition", func(t *testing.T) {
		d := NewDefinition("mine", IntentDetection, KindDeclarative)
		d.Provenance = Provenance{Origin: ProvenanceShipped}
		if _, err := BuildShippedProgrammaticDefinition(d, ShippedDeps{}); err == nil {
			t.Fatal("BuildShippedProgrammaticDefinition accepted a declarative definition")
		}
	})

	t.Run("an unknown shipped id has no logic to run", func(t *testing.T) {
		d := NewDefinition("not-a-shipped-detector", IntentDetection, KindProgrammatic)
		d.ID = "not-a-shipped-detector"
		d.Provenance = Provenance{Origin: ProvenanceShipped}
		_, err := BuildShippedProgrammaticDefinition(d, ShippedDeps{})
		if err == nil {
			t.Fatal("BuildShippedProgrammaticDefinition invented logic for an unknown id")
		}
		if !strings.Contains(err.Error(), "no shipped programmatic builder") {
			t.Errorf("error does not say why: %v", err)
		}
	})

	t.Run("the store refuses to replace a shipped definition wholesale", func(t *testing.T) {
		s, err := OpenDefinitionsStoreWithBackend(nil)
		if err != nil {
			t.Fatal(err)
		}
		shipped := NewDefinition("Rule spike", IntentDetection, KindProgrammatic)
		shipped.ID = "rule_spike"
		shipped.Provenance = Provenance{Origin: ProvenanceShipped}
		if err := s.Upsert(shipped); err != nil {
			t.Fatalf("seeding a shipped definition: %v", err)
		}
		// The attack this closes: re-Upsert the same id as a
		// custom/declarative definition to take ownership of a shipped
		// detector's identity.
		hijack := shipped
		hijack.Kind = KindDeclarative
		hijack.Provenance = Provenance{Origin: ProvenanceCustom}
		if err := s.Upsert(hijack); err == nil {
			t.Fatal("Upsert replaced a shipped definition wholesale")
		}
	})
}
