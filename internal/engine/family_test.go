// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
)

// TestValidateFamilyClosedSet pins the vocabulary. The seven names are
// the frontend palette's own (frontend/src/lib/flagPalette.ts), and an
// eighth arriving from a request is refused rather than stored: an ink
// nothing can draw would leave a detector filed under a family the
// docket has no colour for.
func TestValidateFamilyClosedSet(t *testing.T) {
	for _, f := range []Family{FamilyHostile, FamilyScan, FamilyOutbound, FamilyRepeat, FamilySurge, FamilyPresence, FamilyCustom} {
		if err := ValidateFamily(f); err != nil {
			t.Errorf("ValidateFamily(%q) = %v, want nil", f, err)
		}
	}
	// Empty is "not filed", which is what every custom detector was
	// before the field existed, so it has to keep validating.
	if err := ValidateFamily(""); err != nil {
		t.Errorf("ValidateFamily(\"\") = %v, want nil", err)
	}
	err := ValidateFamily("chartreuse")
	if err == nil {
		t.Fatal("ValidateFamily accepted a family nothing can draw")
	}
	// The refusal lists what would have been taken: an operator reading
	// it should learn what to write, not only that they wrote the wrong
	// thing.
	if !strings.Contains(err.Error(), "presence") {
		t.Errorf("refusal did not name the families it would take: %v", err)
	}
}

// TestDefinitionFamilyBelongsToCustomOnly pins who may be filed. A
// shipped detector's family is the design record's own classification of
// the built-ins, held in the frontend palette by definition id, so a
// stored one here would be a second answer to a settled question -- and
// the two could disagree after an upgrade retuned the record.
func TestDefinitionFamilyBelongsToCustomOnly(t *testing.T) {
	custom := NewDefinition("Garage probes", IntentDetection, KindDeclarative)
	custom.Provenance = Provenance{Origin: ProvenanceCustom}
	custom.Family = FamilyScan
	custom.Detection = &DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}
	if err := custom.Validate(); err != nil {
		t.Fatalf("a filed custom detector was refused: %v", err)
	}

	shipped := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	shipped.Provenance = Provenance{Origin: ProvenanceShipped}
	shipped.Family = FamilyScan
	if err := shipped.Validate(); err == nil {
		t.Error("a shipped definition was filed under a family; its family is the design record's")
	}

	// An unfiled custom detector is ordinary, not a half-built one.
	custom.Family = ""
	if err := custom.Validate(); err != nil {
		t.Errorf("an unfiled custom detector was refused: %v", err)
	}
}

// TestSetFamilyRefilesCustomOnly pins the store door the editor's family
// picker writes through, including the clearing case: choosing nothing is
// a state an operator can get back to, not a one-way trip.
func TestSetFamilyRefilesCustomOnly(t *testing.T) {
	s := seededStore(t)

	d := NewDefinition("Garage probes", IntentDetection, KindDeclarative)
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	d.ParamSchema = CustomDetectionParamSchema()
	d.Params = Params{"threshold": 8, "window": "300s"}
	d.Detection = &DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}
	if err := s.Upsert(d); err != nil {
		t.Fatal(err)
	}

	if err := s.SetFamily(d.ID, FamilyScan); err != nil {
		t.Fatalf("SetFamily: %v", err)
	}
	got, ok := s.Get(d.ID)
	if !ok || got.Definition.Family != FamilyScan {
		t.Fatalf("family after filing = %q, want scan", got.Definition.Family)
	}

	if err := s.SetFamily(d.ID, ""); err != nil {
		t.Fatalf("clearing a filing: %v", err)
	}
	got, _ = s.Get(d.ID)
	if got.Definition.Family != "" {
		t.Errorf("family after clearing = %q, want empty", got.Definition.Family)
	}

	if err := s.SetFamily(d.ID, "chartreuse"); err == nil {
		t.Error("SetFamily stored a family nothing can draw")
	}
	if err := s.SetFamily("port_scan", FamilyScan); err == nil {
		t.Error("SetFamily re-filed a shipped detector")
	}
}

// seededStore is an in-memory definitions store carrying the shipped
// detectors, so a test can assert both halves of a refusal -- what it
// does to an operator's own detector and what it refuses to do to the
// binary's.
func seededStore(t *testing.T) *DefinitionsStore {
	t.Helper()
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatalf("OpenDefinitionsStore(\"\"): %v", err)
	}
	if err := SeedShippedDefinitions(s, DefaultDetectorSettings(), DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	return s
}
