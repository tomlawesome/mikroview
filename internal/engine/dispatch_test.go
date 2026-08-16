// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// mkDeclDef builds a minimal, valid, enabled declarative definition with
// a single condition and a no-op window (threshold 1, total counting) --
// this file's tests are about dispatch selection, not window/threshold
// behavior (see declarative_test.go for that).
func mkDeclDef(t *testing.T, id string, cond Condition) *DeclarativeDefinition {
	t.Helper()
	def := NewDefinition(id, IntentDetection, KindDeclarative)
	def.ID = id
	def.Enabled = true
	def.Provenance = Provenance{Origin: ProvenanceCustom}
	dd, err := NewDeclarativeDefinition(def, []Condition{cond}, KeyGlobal, time.Minute, 1, CountingTotal, "", "{PortCount} hits", nil)
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition(%s): %v", id, err)
	}
	return dd
}

// --- discriminant selection ---

func TestBuildDispatchIndexBucketsByDestinationPort(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})

	if idx.AlwaysConsultedCount() != 0 {
		t.Fatalf("AlwaysConsultedCount() = %d, want 0 -- destinationPort is a discriminating field", idx.AlwaysConsultedCount())
	}
	got := idx.Candidates(store.Event{DstPort: 22})
	if len(got) != 1 || got[0].ID() != "d1" {
		t.Fatalf("Candidates(dstPort=22) = %v, want [d1]", got)
	}
	if got := idx.Candidates(store.Event{DstPort: 80}); len(got) != 0 {
		t.Fatalf("Candidates(dstPort=80) = %v, want none", got)
	}
}

func TestBuildDispatchIndexBucketsByChain(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})

	if got := idx.Candidates(store.Event{Chain: "forward"}); len(got) != 1 {
		t.Fatalf("Candidates(chain=forward) = %v, want [d1]", got)
	}
	if got := idx.Candidates(store.Event{Chain: "input"}); len(got) != 0 {
		t.Fatalf("Candidates(chain=input) = %v, want none", got)
	}
}

func TestBuildDispatchIndexBucketsByRuleLabel(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldRuleLabel, Operator: OpEquals, Values: []string{"r13"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})

	if got := idx.Candidates(store.Event{RuleLabel: "r13"}); len(got) != 1 {
		t.Fatalf("Candidates(ruleLabel=r13) = %v, want [d1]", got)
	}
	if got := idx.Candidates(store.Event{RuleLabel: "r14"}); len(got) != 0 {
		t.Fatalf("Candidates(ruleLabel=r14) = %v, want none", got)
	}
}

func TestBuildDispatchIndexBucketsByAddressClass(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"external"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})

	if got := idx.Candidates(store.Event{SrcIP: "8.8.8.8"}); len(got) != 1 {
		t.Fatalf("Candidates(external src) = %v, want [d1]", got)
	}
	if got := idx.Candidates(store.Event{SrcIP: "192.168.1.1"}); len(got) != 0 {
		t.Fatalf("Candidates(internal src) = %v, want none", got)
	}
}

// TestBuildDispatchIndexPortTakesPriorityOverChain pins the documented
// priority order: destination port beats chain when a definition's
// conditions offer both.
func TestBuildDispatchIndexPortTakesPriorityOverChain(t *testing.T) {
	def := NewDefinition("both", IntentDetection, KindDeclarative)
	def.ID = "both"
	def.Enabled = true
	def.Provenance = Provenance{Origin: ProvenanceCustom}
	dd, err := NewDeclarativeDefinition(def, []Condition{
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}},
		{Field: FieldChain, Operator: OpEquals, Values: []string{"forward"}},
	}, KeyGlobal, time.Minute, 1, CountingTotal, "", "{PortCount} hits", nil)
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	idx := BuildDispatchIndex([]*DeclarativeDefinition{dd})

	// A wrong chain but the right port must still hit it (proves port,
	// not chain, was chosen as the discriminant).
	if got := idx.Candidates(store.Event{DstPort: 22, Chain: "input"}); len(got) != 1 {
		t.Fatalf("Candidates(dstPort=22, chain=input) = %v, want [both] (port is the discriminant)", got)
	}
}

// --- always-consulted bucket ---

func TestBuildDispatchIndexNoDiscriminatingFieldGoesGlobal(t *testing.T) {
	// notEquals never discriminates (it matches almost everything), so
	// this definition has no usable discriminating condition.
	d := mkDeclDef(t, "d1", Condition{Field: FieldAction, Operator: OpNotEquals, Values: []string{string(store.ActionAccept)}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})

	if idx.AlwaysConsultedCount() != 1 {
		t.Fatalf("AlwaysConsultedCount() = %d, want 1", idx.AlwaysConsultedCount())
	}
	// Every event must see it, regardless of shape.
	if got := idx.Candidates(store.Event{DstPort: 1, Chain: "whatever"}); len(got) != 1 {
		t.Fatalf("Candidates() = %v, want the always-consulted definition present for every event", got)
	}
}

func TestBuildDispatchIndexEveryDefinitionGlobalIsObservable(t *testing.T) {
	d1 := mkDeclDef(t, "d1", Condition{Field: FieldAction, Operator: OpNotEquals, Values: []string{string(store.ActionAccept)}})
	d2 := mkDeclDef(t, "d2", Condition{Field: FieldConnectionState, Operator: OpNotEquals, Values: []string{"established"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d1, d2})

	if idx.AlwaysConsultedCount() != 2 {
		t.Fatalf("AlwaysConsultedCount() = %d, want 2 -- neither definition has a discriminating field", idx.AlwaysConsultedCount())
	}
}

// --- consulted-subset (bounded) proof ---

// TestDispatchIndexCandidatesIsABoundedSubset is #402's required
// instrumented proof: with many definitions spread across discriminating
// fields, a single event consults far fewer than the full set.
func TestDispatchIndexCandidatesIsABoundedSubset(t *testing.T) {
	const perBucket = 20
	var defs []*DeclarativeDefinition
	for i := 0; i < perBucket; i++ {
		defs = append(defs, mkDeclDef(t, fmt.Sprintf("port-%d", i),
			Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{fmt.Sprintf("%d", 20000+i)}}))
	}
	for i := 0; i < perBucket; i++ {
		defs = append(defs, mkDeclDef(t, fmt.Sprintf("chain-%d", i),
			Condition{Field: FieldChain, Operator: OpEquals, Values: []string{fmt.Sprintf("chain%d", i)}}))
	}
	for i := 0; i < perBucket; i++ {
		defs = append(defs, mkDeclDef(t, fmt.Sprintf("rule-%d", i),
			Condition{Field: FieldRuleLabel, Operator: OpEquals, Values: []string{fmt.Sprintf("rule%d", i)}}))
	}
	total := len(defs)

	idx := BuildDispatchIndex(defs)
	if idx.AlwaysConsultedCount() != 0 {
		t.Fatalf("AlwaysConsultedCount() = %d, want 0 -- every definition here has a discriminating field", idx.AlwaysConsultedCount())
	}

	// An event matching exactly one port-bucket definition's discriminant
	// must not touch anywhere near the other 59.
	got := idx.Candidates(store.Event{DstPort: 20005, Chain: "no-such-chain", RuleLabel: "no-such-rule"})
	if len(got) != 1 {
		t.Fatalf("Candidates() returned %d definitions, want exactly 1 out of %d total", len(got), total)
	}
	if got[0].ID() != "port-5" {
		t.Fatalf("Candidates() = %v, want [port-5]", got)
	}
}

// TestDispatchIndexCandidatesDoesNotRebuild proves the index is
// immutable once built: repeated Candidates calls (across many distinct
// events) never change what a later, identical event's Candidates call
// returns, and the bucket sizes never grow -- there is no code path that
// mutates a DispatchIndex after BuildDispatchIndex returns it.
func TestDispatchIndexCandidatesDoesNotRebuild(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{d})
	before := idx.AlwaysConsultedCount()

	for i := 0; i < 1000; i++ {
		idx.Candidates(store.Event{DstPort: i})
	}

	after := idx.AlwaysConsultedCount()
	if before != after {
		t.Fatalf("AlwaysConsultedCount() changed from %d to %d after many Candidates() calls -- the index must never rebuild itself", before, after)
	}
	if got := idx.Candidates(store.Event{DstPort: 22}); len(got) != 1 || got[0].ID() != "d1" {
		t.Fatalf("Candidates(dstPort=22) after many other calls = %v, want [d1] still", got)
	}
}

// --- DeclarativeSet as Evaluated ---

func TestDeclarativeSetSatisfiesEvaluated(t *testing.T) {
	d := mkDeclDef(t, "d1", Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}})
	var _ Evaluated = NewDeclarativeSet("set1", []*DeclarativeDefinition{d})
}

func TestDeclarativeSetEvaluateOnlyConsultsCandidates(t *testing.T) {
	var matchedCalls, unmatchedCalls int

	matched := mkDeclDef(t, "matched", Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}})
	matched.OnRoutedEmission = func(RoutedEmission) { matchedCalls++ }

	unmatched := mkDeclDef(t, "unmatched", Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"9999"}})
	unmatched.OnRoutedEmission = func(RoutedEmission) { unmatchedCalls++ }

	set := NewDeclarativeSet("set1", []*DeclarativeDefinition{matched, unmatched})
	set.Evaluate(store.Event{DstPort: 22, ReceivedAt: time.Now()})

	if matchedCalls != 1 {
		t.Fatalf("matched definition fired %d time(s), want 1", matchedCalls)
	}
	if unmatchedCalls != 0 {
		t.Fatalf("unmatched definition (wrong dispatch bucket for this event) fired %d time(s), want 0", unmatchedCalls)
	}
}
