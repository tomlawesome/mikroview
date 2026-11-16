// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ipscope"
	"github.com/tomlawesome/mikroview/internal/store"
)

// Assumed API (the spec says "compile takes the scope"):
// compileConditions(conds []Condition, scope *ipscope.Scope). A nil scope
// is today's behaviour.
func TestMatchesClassificationTreatsAConfiguredHomeRangeAsInternal(t *testing.T) {
	conds := []Condition{{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"internal"}}}
	ev := store.Event{SrcIP: "198.51.100.66"}

	plain, err := compileConditions(conds, nil)
	if err != nil {
		t.Fatalf("compileConditions(nil scope): %v", err)
	}
	if plain.match(ev, nil) {
		t.Error("with no scope, 198.51.100.66 must not classify as internal")
	}

	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	homed, err := compileConditions(conds, sc)
	if err != nil {
		t.Fatalf("compileConditions(scope): %v", err)
	}
	if !homed.match(ev, nil) {
		t.Error("with home 198.51.100.0/24, 198.51.100.66 must classify as internal")
	}
	if homed.match(store.Event{SrcIP: "203.0.113.44"}, nil) {
		t.Error("203.0.113.44 must stay external with the home scope set")
	}

	ext := []Condition{{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"external"}}}
	extHomed, err := compileConditions(ext, sc)
	if err != nil {
		t.Fatal(err)
	}
	if extHomed.match(ev, nil) {
		t.Error("with home 198.51.100.0/24, 198.51.100.66 must not classify as external")
	}
}
