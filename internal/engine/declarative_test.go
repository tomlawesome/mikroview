// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// declTestDef builds a minimal, valid Definition suitable for
// NewDeclarativeDefinition -- enabled, custom provenance (so
// Definition.Validate's custom-implies-declarative invariant is
// trivially satisfied), no params.
func declTestDef(intent Intent) Definition {
	d := NewDefinition("Test declarative", intent, KindDeclarative)
	d.Enabled = true
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	return d
}

func evtAt(srcIP, dstIP string, dstPort int, when time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, ReceivedAt: when, Action: store.ActionDrop, ConnState: "new"}
}

// --- construction / validation ---

func TestNewDeclarativeDefinitionRejectsWrongKind(t *testing.T) {
	d := declTestDef(IntentDetection)
	d.Kind = KindProgrammatic
	_, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err == nil {
		t.Fatal("NewDeclarativeDefinition succeeded with Kind=programmatic, want a hard failure")
	}
}

func TestNewDeclarativeDefinitionRejectsBadConditions(t *testing.T) {
	d := declTestDef(IntentDetection)
	_, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     nil,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "x",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err == nil {
		t.Fatal("NewDeclarativeDefinition succeeded with no conditions, want a hard failure")
	}
}

func TestNewDeclarativeDefinitionRejectsNonPositiveWindowOrThreshold(t *testing.T) {
	d := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}

	if _, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         0,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "x",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	}); err == nil {
		t.Fatal("want a hard failure for a zero window")
	}
	if _, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      0,
		CountingMode:   CountingTotal,
		DetailTemplate: "x",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	}); err == nil {
		t.Fatal("want a hard failure for a zero threshold")
	}
}

func TestNewDeclarativeDefinitionRejectsDistinctWithoutCountableField(t *testing.T) {
	d := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	_, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyGlobal,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingDistinct,
		DistinctField:  FieldTimeOfDay,
		DetailTemplate: "x",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err == nil {
		t.Fatal("NewDeclarativeDefinition succeeded with countingMode=distinct on a non-countable field, want a hard failure")
	}
}

func TestNewDeclarativeDefinitionRejectsEmptyDetailTemplate(t *testing.T) {
	d := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	_, err := NewDeclarativeDefinition(d, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err == nil {
		t.Fatal("NewDeclarativeDefinition succeeded with an empty detail template, want a hard failure")
	}
}

// --- Evaluate: end to end ---

// TestDeclarativeDefinitionEndToEnd is #402's own end-to-end scenario:
// conditions match -> window counts -> threshold crosses -> emission
// produced with populated evidence, routed to a flags.Flag.
func TestDeclarativeDefinitionEndToEnd(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}},
		{Field: FieldAction, Operator: OpEquals, Values: []string{string(store.ActionDrop)}},
	}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits on watched ports from this source ({HostCount} hosts touched)",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}

	var routed []RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = append(routed, r) }

	now := time.Now()
	for i := 0; i < 4; i++ {
		dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(routed) != 0 {
		t.Fatalf("got %d emission(s) before the threshold was crossed, want 0", len(routed))
	}

	// 5th matching event crosses threshold=5.
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.2", 22, now.Add(4*time.Second)))

	if len(routed) != 1 {
		t.Fatalf("got %d emission(s) after crossing threshold, want exactly 1", len(routed))
	}
	r := routed[0]
	if r.Detection == nil {
		t.Fatal("want a Detection (flags.Flag) for a detection-intent definition")
	}
	flag := r.Detection
	if flag.Target != "198.51.100.7" {
		t.Errorf("flag.Target = %q, want the per-source key %q", flag.Target, "198.51.100.7")
	}
	if len(flag.Evidence.Ports) == 0 {
		t.Error("flag.Evidence.Ports is empty, want the accumulated destination port(s)")
	}
	if len(flag.Evidence.Hosts) == 0 {
		t.Error("flag.Evidence.Hosts is empty, want the accumulated destination host(s)")
	}
	if flag.Detail == "" {
		t.Error("flag.Detail is empty, want the rendered template")
	}

	// Further matching events keep re-crossing (re-firing is the
	// caller's own policy, not this definition's job -- but Evaluate
	// itself has no reason to stop producing emissions once threshold
	// stays crossed).
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.3", 22, now.Add(5*time.Second)))
	if len(routed) != 2 {
		t.Fatalf("got %d emission(s) after a 6th matching event, want 2", len(routed))
	}
}

func TestDeclarativeDefinitionNonMatchingEventsNeverCount(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      3,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var fired bool
	dd.OnRoutedEmission = func(RoutedEmission) { fired = true }

	now := time.Now()
	for i := 0; i < 10; i++ {
		dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 80, now.Add(time.Duration(i)*time.Second))) // wrong port
	}
	if fired {
		t.Fatal("a definition fired from events that never matched its own conditions")
	}
}

func TestDeclarativeDefinitionDisabledIsInert(t *testing.T) {
	def := declTestDef(IntentDetection)
	def.Enabled = false
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      1,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var fired bool
	dd.OnRoutedEmission = func(RoutedEmission) { fired = true }

	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.1", 22, time.Now()))
	if fired {
		t.Fatal("a disabled definition produced an emission")
	}
}

// TestDeclarativeDistinctCountingModeIsNotSatisfiableByOneSource is
// #402's own required characterization: a distinct-keyed definition must
// not be satisfiable by repeats from one source -- the same distinction
// internal/detect's
// TestCharacterizationDistributedBruteForceRequiresDistinctSources pins
// for the hand-written detector this kind is meant to eventually
// replace (#405), reproduced here against the new declarative model
// instead of against internal/detect (which this change does not
// touch).
func TestDeclarativeDistinctCountingModeIsNotSatisfiableByOneSource(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyGlobal,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingDistinct,
		DistinctField:  FieldSourceAddress,
		DetailTemplate: "{HostCount} distinct sources",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var fireCount int
	dd.OnRoutedEmission = func(RoutedEmission) { fireCount++ }

	now := time.Now()
	// 20 repeats from a single source must never cross a threshold of 5
	// distinct sources.
	for i := 0; i < 20; i++ {
		dd.Evaluate(evtAt("198.51.100.1", "10.0.0.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if fireCount != 0 {
		t.Fatalf("repeats from one source alone fired %d time(s), want 0", fireCount)
	}

	// 4 more distinct sources (the repeated one above plus these 4 makes
	// exactly 5 distinct) crosses the threshold=5.
	for i := 0; i < 4; i++ {
		dd.Evaluate(evtAt("198.51.100.10"+string(rune('0'+i)), "10.0.0.1", 22, now))
	}
	if fireCount != 1 {
		t.Fatalf("reaching 5 distinct sources fired %d time(s), want exactly 1", fireCount)
	}
}

// TestDeclarativeCountingTotalIsSatisfiableByOneSource is the contrast
// case: the same shape under CountingTotal DOES fire from repeats alone
// -- proving the distinct/total split actually changes behavior, not
// just bookkeeping.
func TestDeclarativeCountingTotalIsSatisfiableByOneSource(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyGlobal,
		Window:         time.Minute,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var fireCount int
	dd.OnRoutedEmission = func(RoutedEmission) { fireCount++ }

	now := time.Now()
	for i := 0; i < 5; i++ {
		dd.Evaluate(evtAt("198.51.100.1", "10.0.0.1", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if fireCount != 1 {
		t.Fatalf("5 repeats under countingMode=total fired %d time(s), want exactly 1", fireCount)
	}
}

func TestDeclarativeDefinitionExpectationIntentRoutesToMatchlog(t *testing.T) {
	def := declTestDef(IntentExpectation)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"53"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         time.Minute,
		Threshold:      1,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var routed *RoutedEmission
	dd.OnRoutedEmission = func(r RoutedEmission) { routed = &r }

	dd.Evaluate(evtAt("198.51.100.1", "10.0.0.1", 53, time.Now()))
	if routed == nil {
		t.Fatal("no emission produced")
	}
	if routed.Expectation == nil || routed.Detection != nil {
		t.Fatalf("routed = %+v, want only Expectation set for an expectation-intent definition", routed)
	}
}

func TestDeclarativeDefinitionKeyPerSourcePort(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldProtocol, Operator: OpEquals, Values: []string{"tcp"}}}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSourcePort,
		Window:         time.Minute,
		Threshold:      3,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	var targets []string
	dd.OnRoutedEmission = func(r RoutedEmission) { targets = append(targets, r.Detection.Target) }

	now := time.Now()
	mk := func(port int, when time.Time) store.Event {
		return store.Event{SrcIP: "198.51.100.1", DstIP: "10.0.0.1", DstPort: port, Protocol: "tcp", ReceivedAt: when}
	}
	// Three hits on port 22 and three on port 80 from the same source --
	// perSourcePort must track them as two independent windows, each
	// crossing its own threshold=3 independently.
	for i := 0; i < 3; i++ {
		dd.Evaluate(mk(22, now.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 3; i++ {
		dd.Evaluate(mk(80, now.Add(time.Duration(i)*time.Second)))
	}
	if len(targets) != 2 {
		t.Fatalf("got %d emissions, want 2 (one per source+port key)", len(targets))
	}
	if targets[0] == targets[1] {
		t.Errorf("both emissions used the same target %q, want distinct per-port keys", targets[0])
	}
}
