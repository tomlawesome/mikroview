// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// customDetection returns a stored-shape custom detection: the envelope
// an operator's create request produces, structure in the block and
// tunables in Params.
func customDetection(spec *DetectionSpec, threshold int, window string) Definition {
	d := NewDefinition("operator's detector", IntentDetection, KindDeclarative)
	d.Enabled = true
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	d.ParamSchema = CustomDetectionParamSchema()
	d.Params = Params{"threshold": threshold, "window": window}
	d.Detection = spec
	return d
}

// The point of the whole feature: a detector described entirely by
// stored data evaluates real events and fires when its own threshold is
// crossed.
func TestBuildCustomDetectionDefinitionFires(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} attempts against port 22 from {SourceAddress}",
	}, 3, "60s")

	dd, err := BuildCustomDetectionDefinition(def, nil)
	if err != nil {
		t.Fatalf("BuildCustomDetectionDefinition: %v", err)
	}
	var routed []RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = append(routed, r) }

	now := time.Now()
	// A non-matching event must not count toward the threshold, or the
	// conditions are not actually being applied.
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 443, now))
	for i := range 2 {
		dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(routed) != 0 {
		t.Fatalf("got %d emission(s) below the threshold, want 0", len(routed))
	}

	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, now.Add(2*time.Second)))
	if len(routed) != 1 {
		t.Fatalf("got %d emission(s) after crossing threshold=3, want 1", len(routed))
	}
	if routed[0].Detection == nil {
		t.Fatal("a detection-intent definition must route a Detection")
	}
	if got, want := routed[0].Detection.Detail, "3 attempts against port 22 from 198.51.100.7"; got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// Keyed per source, so two sources accumulate separately -- the
// aggregation the operator chose is the aggregation that runs.
func TestBuildCustomDetectionDefinitionHonoursKeyMode(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 2, "60s")

	dd, err := BuildCustomDetectionDefinition(def, nil)
	if err != nil {
		t.Fatalf("BuildCustomDetectionDefinition: %v", err)
	}
	var routed []RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = append(routed, r) }

	now := time.Now()
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, now))
	dd.Evaluate(evtAt("198.51.100.8", "10.0.0.1", 22, now.Add(time.Second)))
	if len(routed) != 0 {
		t.Fatalf("two sources at one event each crossed a threshold of 2: %d emission(s)", len(routed))
	}
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, now.Add(2*time.Second)))
	if len(routed) != 1 {
		t.Fatalf("got %d emission(s), want 1 once one source reached the threshold", len(routed))
	}
}

// Distinct counting over a chosen field, the aggregation port_scan uses
// and the one an operator most plausibly wants.
func TestBuildCustomDetectionDefinitionCountsDistinct(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
		Key:            KeyPerSource,
		Counting:       CountingDistinct,
		DistinctField:  FieldDestinationPort,
		DetailTemplate: "{Count} distinct ports from {SourceAddress}",
	}, 3, "60s")

	dd, err := BuildCustomDetectionDefinition(def, nil)
	if err != nil {
		t.Fatalf("BuildCustomDetectionDefinition: %v", err)
	}
	var routed []RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = append(routed, r) }

	now := time.Now()
	// The same port five times is one distinct value, not five.
	for i := range 5 {
		e := evtAt("198.51.100.7", "10.0.0.1", 22, now.Add(time.Duration(i)*time.Second))
		e.Chain = "forward"
		dd.Evaluate(e)
	}
	if len(routed) != 0 {
		t.Fatalf("repeated hits on one port crossed a distinct threshold of 3: %d emission(s)", len(routed))
	}
	for i, port := range []int{23, 25} {
		e := evtAt("198.51.100.7", "10.0.0.1", port, now.Add(time.Duration(10+i)*time.Second))
		e.Chain = "forward"
		dd.Evaluate(e)
	}
	if len(routed) != 1 {
		t.Fatalf("got %d emission(s), want 1 at three distinct ports", len(routed))
	}
}

func TestDetectionSpecValidateRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec *DetectionSpec
		want string
	}{
		{
			name: "no block at all",
			spec: nil,
			want: "requires a detection block",
		},
		{
			name: "no conditions",
			spec: &DetectionSpec{Key: KeyPerSource, Counting: CountingTotal, DetailTemplate: "{Count}"},
			want: "at least one condition",
		},
		{
			name: "unknown condition field",
			spec: &DetectionSpec{
				Conditions:     []Condition{{Field: Field("whenTheMoonIsFull"), Operator: OpEquals, Values: []string{"yes"}}},
				Key:            KeyPerSource,
				Counting:       CountingTotal,
				DetailTemplate: "{Count}",
			},
			want: "whenTheMoonIsFull",
		},
		{
			name: "operator the field does not support",
			spec: &DetectionSpec{
				Conditions:     []Condition{{Field: FieldChain, Operator: OpInCIDR, Values: []string{"10.0.0.0/8"}}},
				Key:            KeyPerSource,
				Counting:       CountingTotal,
				DetailTemplate: "{Count}",
			},
			want: "chain",
		},
		{
			name: "unknown key mode",
			spec: &DetectionSpec{
				Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
				Key:            KeyMode("perFullMoon"),
				Counting:       CountingTotal,
				DetailTemplate: "{Count}",
			},
			want: "perFullMoon",
		},
		{
			name: "distinct with no field to count",
			spec: &DetectionSpec{
				Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
				Key:            KeyPerSource,
				Counting:       CountingDistinct,
				DetailTemplate: "{Count}",
			},
			want: "countable distinctField",
		},
		{
			// Tolerated by DeclarativeSpec, refused here: stored, it
			// would be a field the operator set and the detector never
			// reads.
			name: "total with a distinct field",
			spec: &DetectionSpec{
				Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
				Key:            KeyPerSource,
				Counting:       CountingTotal,
				DistinctField:  FieldDestinationPort,
				DetailTemplate: "{Count}",
			},
			want: "takes no distinctField",
		},
		{
			name: "no detail template",
			spec: &DetectionSpec{
				Conditions: []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
				Key:        KeyPerSource,
				Counting:   CountingTotal,
			},
			want: "detailTemplate is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v, want a refusal", tc.spec)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The detail template is operator input rendered into the interface. Its
// placeholder set is closed and resolved at create time, so a template
// that could never render is refused before the detector exists rather
// than failing at the moment it should have fired.
func TestValidateDetectionDetailTemplate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     KeyMode
		tmpl    string
		wantErr bool
	}{
		{name: "count alone is always available", key: KeyGlobal, tmpl: "{Count} events"},
		{name: "key component the mode supplies", key: KeyPerSource, tmpl: "{Count} from {SourceAddress}"},
		{name: "both components of a compound key", key: KeyPerSourcePort, tmpl: "{Count} from {SourceAddress} to {DestinationPort}"},
		{name: "plain text with no placeholder at all", key: KeyGlobal, tmpl: "something happened"},
		{name: "key component another mode supplies", key: KeyPerSource, tmpl: "{Count} to {DestinationAddress}", wantErr: true},
		{name: "global supplies no key component", key: KeyGlobal, tmpl: "{Count} from {SourceAddress}", wantErr: true},
		// A custom detection declares no evidence categories, so
		// RenderEmission would never resolve these.
		{name: "evidence token", key: KeyPerSource, tmpl: "{Count} across {Ports}", wantErr: true},
		{name: "evidence count token", key: KeyPerSource, tmpl: "{PortCount} ports", wantErr: true},
		{name: "invented token", key: KeyPerSource, tmpl: "{Count} {Nonsense}", wantErr: true},
		{name: "empty", key: KeyPerSource, tmpl: "", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDetectionDetailTemplate(tc.key, tc.tmpl)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateDetectionDetailTemplate(%q, %q) accepted it, want a refusal", tc.key, tc.tmpl)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateDetectionDetailTemplate(%q, %q) = %v, want it accepted", tc.key, tc.tmpl, err)
			}
		})
	}
}

// A template that validates must also render, or the validation is
// describing a different set than the one RenderEmission enforces.
func TestValidatedDetailTemplateActuallyRenders(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSourcePort,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} attempts from {SourceAddress} against port {DestinationPort}",
	}, 1, "60s")

	dd, err := BuildCustomDetectionDefinition(def, nil)
	if err != nil {
		t.Fatalf("BuildCustomDetectionDefinition: %v", err)
	}
	var routed []RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = append(routed, r) }
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, time.Now()))

	if len(routed) != 1 {
		t.Fatalf("got %d emission(s), want 1", len(routed))
	}
	if got, want := routed[0].Detection.Detail, "1 attempts from 198.51.100.7 against port 22"; got != want {
		t.Errorf("Detail = %q, want %q -- a validated template must render", got, want)
	}
}

// Registry.Sync decides whether a definition changed by byte-comparing
// its stored JSON, so the same detection encoded twice has to produce
// the same bytes. A block that serialised unstably would silently drop a
// detector's accumulated window state on an unrelated save.
func TestDetectionSpecSerialisesDeterministically(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions: []Condition{
			{Field: FieldDestinationPort, Operator: OpInSet, Values: []string{"22", "23", "3389"}},
			{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}},
		},
		Key:            KeyPerSource,
		Counting:       CountingDistinct,
		DistinctField:  FieldDestinationPort,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 5, "90s")

	first, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("re-encoding the same detection produced different bytes:\n%s\n%s", first, again)
		}
	}

	// And it survives a round trip unchanged, which is what carrying
	// state forward across a sync actually depends on.
	var back Definition
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatal(err)
	}
	round, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(round) != string(first) {
		t.Errorf("round trip changed the encoding:\n%s\n%s", first, round)
	}
}

// A detection block belongs only to a custom detection: anywhere else it
// would be data that looked authoritative and was never read.
func TestValidateRejectsDetectionBlockWhereItDoesNotBelong(t *testing.T) {
	spec := &DetectionSpec{
		Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}

	shipped := NewDefinition("shipped", IntentDetection, KindDeclarative)
	shipped.Provenance = Provenance{Origin: ProvenanceShipped}
	shipped.Detection = spec
	if err := shipped.Validate(); err == nil {
		t.Error("a shipped definition carrying a detection block validated, want a refusal")
	}

	expectation := NewDefinition("expectation", IntentExpectation, KindDeclarative)
	expectation.Provenance = Provenance{Origin: ProvenanceCustom}
	expectation.Detection = spec
	if err := expectation.Validate(); err == nil {
		t.Error("an expectation carrying a detection block validated, want a refusal")
	}
}

// A stored custom detection this binary cannot make sense of -- a
// condition field or key mode from a newer build -- is shelved, not
// evaluated and not dropped. Without this a rollback would leave a
// definition that fails to build on every single sync.
func TestStoredCustomDetectionFromANewerBuildIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
	}{
		{
			name: "condition field this binary does not know",
			json: `{"id":"future-detector","name":"from a newer build","intent":"detection","kind":"declarative","enabled":true,"provenance":{"origin":"custom"},"detection":{"conditions":[{"field":"quantumEntanglement","operator":"equals","values":["yes"]}],"key":"perSource","counting":"total","detailTemplate":"{Count} from {SourceAddress}"}}`,
		},
		{
			name: "key mode this binary does not know",
			json: `{"id":"future-detector","name":"from a newer build","intent":"detection","kind":"declarative","enabled":true,"provenance":{"origin":"custom"},"detection":{"conditions":[{"field":"chain","operator":"equals","values":["forward"]}],"key":"perAutonomousSystem","counting":"total","detailTemplate":"{Count}"}}`,
		},
		{
			name: "no detection block at all",
			json: `{"id":"future-detector","name":"nothing to rebuild from","intent":"detection","kind":"declarative","enabled":true,"provenance":{"origin":"custom"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd := decodeStored("future-detector", []byte(tc.json))
			if sd.Available {
				t.Fatal("a detection this binary cannot build reported Available, so the registry would try to build it on every sync")
			}
			if sd.Definition.ID != "future-detector" {
				t.Errorf("the shelved definition lost its identity: %+v", sd.Definition)
			}
		})
	}
}

// A well-formed one is available, or the check above would shelve every
// custom detection rather than only the ones it cannot read.
func TestStoredCustomDetectionThisBinaryUnderstandsIsAvailable(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 4, "60s")
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if sd := decodeStored(def.ID, raw); !sd.Available {
		t.Fatal("a detection this binary can build reported unavailable")
	}
}

// A stored custom detection has to carry its structure: it is rebuilt
// from its bytes alone.
func TestDefinitionsStoreRefusesCustomDetectionWithNoBlock(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDefinition("nothing to match on", IntentDetection, KindDeclarative)
	d.Enabled = true
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	if err := s.Upsert(d); err == nil {
		t.Fatal("stored a custom detection with no detection block -- it would list and evaluate nothing")
	}
}

// The dispatch answer the API discloses has to be the one the index
// actually reaches, or the disclosure is decoration.
func TestCanNarrowDispatchMatchesTheIndex(t *testing.T) {
	narrowable := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	if !CanNarrowDispatch(narrowable) {
		t.Error("a positive destination-port condition should narrow")
	}
	// Negation narrows nothing: it says what the definition is not
	// interested in, which excludes no bucket.
	notNarrowable := []Condition{{Field: FieldSourceAddress, Operator: OpEquals, Values: []string{"198.51.100.7"}}}
	if CanNarrowDispatch(notNarrowable) {
		t.Error("a source-address condition is not a dispatch discriminant")
	}

	def := customDetection(&DetectionSpec{
		Conditions:     notNarrowable,
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 2, "60s")
	dd, err := BuildCustomDetectionDefinition(def, nil)
	if err != nil {
		t.Fatal(err)
	}
	idx := BuildDispatchIndex([]*DeclarativeDefinition{dd})
	if len(idx.global) != 1 {
		t.Errorf("the index placed %d definition(s) in the always-consulted bucket, want 1 -- CanNarrowDispatch and the index disagree", len(idx.global))
	}
}

// Replayability is meant to come free: a custom detection produces a
// *DeclarativeDefinition, which already satisfies the replay contract.
// It only does if the inspection path builds it, rather than handing it
// to the shipped builder that has no entry for its id.
func TestCustomDetectionIsReplayable(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 3, "60s")

	built, err := BuildForInspection(def)
	if err != nil {
		t.Fatalf("BuildForInspection: %v -- a custom detection must be inspectable", err)
	}
	if _, ok := built.(*DeclarativeDefinition); !ok {
		t.Fatalf("BuildForInspection returned %T, want a *DeclarativeDefinition", built)
	}
	capable, reason, known := ReplayabilityOf(def)
	if !known {
		t.Fatalf("replayability is unknown (%s), so the definition's view would report that it could not be built", reason)
	}
	if !capable {
		t.Errorf("a custom declarative detection should be replay-capable, got not capable (%s)", reason)
	}
}

// A detector an operator authored is theirs to rename: the alternative
// is deleting it and creating it again, which throws away the id every
// flag it already raised points at (#612).
func TestSetNameRenamesACustomDetection(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 3, "60s")
	def.Scope.Hosts = []string{"10.0.0.1"}
	if err := s.Upsert(def); err != nil {
		t.Fatal(err)
	}

	if err := s.SetName(def.ID, "  renamed detector  "); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	got, ok := s.Get(def.ID)
	if !ok {
		t.Fatal("the definition vanished on rename")
	}
	if got.Definition.Name != "renamed detector" {
		t.Errorf("Name = %q, want it renamed and trimmed", got.Definition.Name)
	}

	// A rename is a rename: nothing else about the detector moves, and
	// in particular it keeps the id every raised flag refers to.
	if got.Definition.ID != def.ID {
		t.Errorf("id changed on rename: %q -> %q", def.ID, got.Definition.ID)
	}
	if got.Definition.Detection == nil || len(got.Definition.Detection.Conditions) != 1 {
		t.Errorf("the detection block did not survive the rename: %+v", got.Definition.Detection)
	}
	// Through ValidateParams, because stored params come back in their
	// JSON representation (a float64 for an int) until they are
	// normalised -- comparing the raw map would be asserting on the
	// encoding rather than on the value.
	params, err := ValidateParams(got.Definition.ParamSchema, got.Definition.Params)
	if err != nil {
		t.Fatalf("params no longer validate after a rename: %v", err)
	}
	window, err := time.ParseDuration(params["window"].(string))
	if err != nil {
		t.Fatalf("window is no longer a duration after a rename: %v", err)
	}
	if params["threshold"] != 3 || window != time.Minute {
		t.Errorf("params changed on rename: threshold=%v window=%s", params["threshold"], window)
	}
	if len(got.Definition.Scope.Hosts) != 1 || got.Definition.Scope.Hosts[0] != "10.0.0.1" {
		t.Errorf("scope changed on rename: %+v", got.Definition.Scope)
	}

	if err := s.SetName(def.ID, "   "); err == nil {
		t.Error("an empty name was accepted")
	}
	if after, _ := s.Get(def.ID); after.Definition.Name != "renamed detector" {
		t.Errorf("the refused rename was applied anyway: %q", after.Definition.Name)
	}
}
