// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

// TestSetDetectionRewritesStructure pins the door the conditions editor
// saves through. Before #829 nothing in the UI could author a condition,
// so a detector's structure was write-once at create time; the editor is
// what makes rewriting it a real operation.
//
// The two refusals matter as much as the write. A structure that does not
// compile must not reach disk -- a stored detector that lists and then
// fails when it should fire is the exact failure the whole feature had to
// avoid -- and a shipped detector's structure belongs to the binary.
func TestSetDetectionRewritesStructure(t *testing.T) {
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

	next := DetectionSpec{
		Conditions: []Condition{
			{Field: FieldSourceAddress, Operator: OpInCIDR, Values: []string{"10.0.70.0/24"}},
			{Field: FieldAction, Operator: OpEquals, Values: []string{"drop"}},
		},
		Key:            KeyPerSource,
		Counting:       CountingDistinct,
		DistinctField:  FieldDestinationAddress,
		DetailTemplate: "{Count} hosts probed by {SourceAddress}",
	}
	if err := s.SetDetection(d.ID, next); err != nil {
		t.Fatalf("SetDetection: %v", err)
	}
	got, ok := s.Get(d.ID)
	if !ok {
		t.Fatal("the detector vanished")
	}
	if len(got.Definition.Detection.Conditions) != 2 {
		t.Fatalf("conditions after the rewrite = %+v, want two", got.Definition.Detection.Conditions)
	}
	if got.Definition.Detection.DistinctField != FieldDestinationAddress {
		t.Errorf("distinctField = %q, want destinationAddress", got.Definition.Detection.DistinctField)
	}

	// The stored spec is a copy: mutating the caller's slice afterwards
	// must not reach into the store's own value.
	next.Conditions[0].Values[0] = "0.0.0.0/0"
	got, _ = s.Get(d.ID)
	if got.Definition.Detection.Conditions[0].Values[0] != "10.0.70.0/24" {
		t.Error("the store kept the caller's slice rather than a copy of it")
	}

	if err := s.SetDetection(d.ID, DetectionSpec{Key: KeyPerSource, Counting: CountingTotal, DetailTemplate: "{Count}"}); err == nil {
		t.Error("SetDetection stored a detector with no conditions, which matches nothing meaningfully")
	}
	if err := s.SetDetection("port_scan", next); err == nil {
		t.Error("SetDetection rewrote a shipped detector's structure")
	}
}
