// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

func intPtr(v int) *int { return &v }

// TestRouteProducesSymmetricOutputForBothIntents is the router test the
// issue asks for: the same emission shape (same Target/Detail/
// Confidence/Provisional/evidence), routed through a detection-intent
// definition and, separately, through an otherwise-identical
// expectation-intent definition, must produce outputs that carry the
// same information through each intent's own shape. This is what proves
// "two kinds" (declarative/programmatic) stays one system rather than
// decaying into "two subsystems" the way internal/detect and
// internal/watchlist did -- see docs/decisions/evaluation-engine.md
// section 3.
func TestRouteProducesSymmetricOutputForBothIntents(t *testing.T) {
	detectionDef := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	expectationDef := NewDefinition("Expected DNS", IntentExpectation, KindDeclarative)

	detectionEm := Emission{
		DefinitionID: detectionDef.ID,
		Target:       "203.0.113.5",
		Detail:       "23 distinct ports in 60s",
		Ports:        []int{22, 80, 443},
		Confidence:   intPtr(70),
		Provisional:  true,
	}
	expectationEm := Emission{
		DefinitionID: expectationDef.ID,
		Target:       "203.0.113.5",
		Detail:       "23 distinct ports in 60s",
		Ports:        []int{22, 80, 443},
		Confidence:   intPtr(70),
		Provisional:  true,
	}

	detectionRouted, err := Route(detectionDef, detectionEm)
	if err != nil {
		t.Fatalf("Route (detection): %v", err)
	}
	expectationRouted, err := Route(expectationDef, expectationEm)
	if err != nil {
		t.Fatalf("Route (expectation): %v", err)
	}

	if detectionRouted.Detection == nil || detectionRouted.Expectation != nil {
		t.Fatalf("detection-intent Route() = %+v, want only Detection set", detectionRouted)
	}
	if expectationRouted.Expectation == nil || expectationRouted.Detection != nil {
		t.Fatalf("expectation-intent Route() = %+v, want only Expectation set", expectationRouted)
	}

	flag := detectionRouted.Detection
	write := expectationRouted.Expectation

	if flag.Target != write.Target {
		t.Errorf("Target: flag=%q matchlog=%q, want equal", flag.Target, write.Target)
	}
	if flag.Detail != write.Detail {
		t.Errorf("Detail: flag=%q matchlog=%q, want equal", flag.Detail, write.Detail)
	}
	if flag.Provisional != write.Provisional {
		t.Errorf("Provisional: flag=%v matchlog=%v, want equal", flag.Provisional, write.Provisional)
	}
	if *flag.Confidence != *write.Confidence {
		t.Errorf("Confidence: flag=%v matchlog=%v, want equal", *flag.Confidence, *write.Confidence)
	}
}

func TestRouteDetectionPopulatesFlagFields(t *testing.T) {
	def := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	em := Emission{
		DefinitionID: def.ID,
		Target:       "203.0.113.5",
		Detail:       "23 distinct ports in 60s",
		Ports:        []int{22, 443},
		Hosts:        []string{"198.51.100.1"},
	}

	routed, err := Route(def, em)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	flag := routed.Detection
	if flag == nil {
		t.Fatal("Route: Detection is nil for a detection-intent definition")
	}
	if string(flag.Type) != def.ID {
		t.Errorf("flag.Type = %q, want the definition's ID %q", flag.Type, def.ID)
	}
	if flag.Target != em.Target || flag.Detail != em.Detail {
		t.Errorf("flag = %+v, did not carry through Target/Detail", flag)
	}
	if len(flag.Evidence.Ports) != 2 || len(flag.Evidence.Hosts) != 1 {
		t.Errorf("flag.Evidence = %+v, evidence did not carry through", flag.Evidence)
	}
}

func TestRouteExpectationPopulatesMatchlogWriteFields(t *testing.T) {
	def := NewDefinition("Expected DNS", IntentExpectation, KindDeclarative)
	em := Emission{
		DefinitionID: def.ID,
		Target:       "203.0.113.5",
		Detail:       "unexpected destination",
	}

	routed, err := Route(def, em)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	write := routed.Expectation
	if write == nil {
		t.Fatal("Route: Expectation is nil for an expectation-intent definition")
	}
	if write.EntryID != def.ID {
		t.Errorf("write.EntryID = %q, want the definition's ID %q", write.EntryID, def.ID)
	}
	if write.Target != em.Target || write.Detail != em.Detail {
		t.Errorf("write = %+v, did not carry through Target/Detail", write)
	}
}

func TestRouteRejectsEmissionForDifferentDefinition(t *testing.T) {
	def := NewDefinition("Port scan", IntentDetection, KindDeclarative)
	em := Emission{DefinitionID: "some-other-id", Target: "203.0.113.5", Detail: "x"}

	if _, err := Route(def, em); err == nil {
		t.Fatal("Route succeeded with em.DefinitionID != def.ID, want a hard failure")
	}
}

func TestRouteRejectsUnknownIntent(t *testing.T) {
	def := NewDefinition("Weird", Intent("sideways"), KindDeclarative)
	em := Emission{DefinitionID: def.ID}

	if _, err := Route(def, em); err == nil {
		t.Fatal("Route succeeded on a definition with an unknown Intent, want a hard failure")
	}
}
