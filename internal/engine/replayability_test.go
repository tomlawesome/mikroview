// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"
)

// fakeReplayableOnlyDef is a minimal Evaluated implementing Replayable
// but not NonReplayable, for exercising Replayability's classification
// without depending on DeclarativeDefinition's own, heavier construction
// path.
type fakeReplayableOnlyDef struct{ fakeDef }

func (f *fakeReplayableOnlyDef) Replay(corpus Corpus, candidate Params) (Result, error) {
	return Result{}, nil
}

// fakeNonReplayableDef is a minimal Evaluated implementing NonReplayable
// but not Replayable -- the shape #405/#406's reputation/absence-of-
// events/floor-exceeds-corpus definitions will eventually take (see
// NonReplayable's own doc comment for those three cases).
type fakeNonReplayableDef struct {
	fakeDef
	reason string
}

func (f *fakeNonReplayableDef) NonReplayableReason() string { return f.reason }

// fakeBothDef implements both interfaces -- the disallowed, ambiguous
// case Replayability refuses to resolve one way or the other.
type fakeBothDef struct{ fakeDef }

func (f *fakeBothDef) Replay(corpus Corpus, candidate Params) (Result, error) { return Result{}, nil }
func (f *fakeBothDef) NonReplayableReason() string                            { return "ambiguous" }

func TestReplayabilityClassifiesReplayableDefinition(t *testing.T) {
	d := &fakeReplayableOnlyDef{fakeDef: fakeDef{id: "r1", kind: "fake"}}
	capable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability: ok = false for a definition implementing Replayable, want true")
	}
	if !capable {
		t.Error("Replayability: receiptCapable = false, want true")
	}
	if reason != "" {
		t.Errorf("Replayability: reason = %q, want empty for a Replayable definition", reason)
	}
}

func TestReplayabilityClassifiesNonReplayableDeclaration(t *testing.T) {
	d := &fakeNonReplayableDef{fakeDef: fakeDef{id: "nr1", kind: "fake"}, reason: "reputation lookups are replay-time, not event-time, evidence"}
	capable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability: ok = false for a definition implementing NonReplayable, want true")
	}
	if capable {
		t.Error("Replayability: receiptCapable = true, want false")
	}
	if reason != d.reason {
		t.Errorf("Replayability: reason = %q, want %q", reason, d.reason)
	}
}

func TestReplayabilityReportsNotOkForNeitherInterface(t *testing.T) {
	d := &fakeDef{id: "plain", kind: "fake"}
	var e Evaluated = d
	_, _, ok := Replayability(e)
	if ok {
		t.Fatal("Replayability: ok = true for a definition implementing neither Replayable nor NonReplayable, want false")
	}
}

func TestReplayabilityReportsNotOkForBothInterfaces(t *testing.T) {
	d := &fakeBothDef{fakeDef: fakeDef{id: "both", kind: "fake"}}
	_, _, ok := Replayability(d)
	if ok {
		t.Fatal("Replayability: ok = true for a definition implementing both Replayable and NonReplayable, want false (ambiguous, refused)")
	}
}

// TestDeclarativeDefinitionImplementsReplayable pins issue #403's
// "declarative definitions ARE replayable" decision structurally:
// DeclarativeDefinition must satisfy Replayable, and Replayability must
// classify it as receipt-capable with no NonReplayable reason.
func TestDeclarativeDefinitionImplementsReplayable(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	dd, err := NewDeclarativeDefinition(def, conds, KeyPerSource, time.Minute, 5, CountingTotal, "", "{PortCount} hits", nil)
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}

	var e Evaluated = dd
	capable, reason, ok := Replayability(e)
	if !ok || !capable {
		t.Fatalf("Replayability(DeclarativeDefinition) = (%v, %q, %v), want (true, \"\", true)", capable, reason, ok)
	}
}
