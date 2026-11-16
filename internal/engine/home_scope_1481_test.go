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

// TestSetHomeScopeReachesDefinitionsAndTheDispatchIndex goes through the
// public path: SetHomeScope, then NewDeclarativeDefinition (which compiles
// with the package scope), then the dispatch index. Dropping SetHomeScope
// from the wiring, or passing nil at the compile site, fails it.
func TestSetHomeScopeReachesDefinitionsAndTheDispatchIndex(t *testing.T) {
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	SetHomeScope(sc)
	t.Cleanup(func() { SetHomeScope(nil) })

	internalDef := mkDeclDef(t, "internal-src", Condition{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"internal"}})
	externalDef := mkDeclDef(t, "external-src", Condition{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"external"}})
	idx := BuildDispatchIndex([]*DeclarativeDefinition{internalDef, externalDef})

	for _, c := range []struct {
		src          string
		wantInternal bool
	}{
		{"198.51.100.66", true},
		{"203.0.113.44", false},
		{"10.1.2.3", true},
	} {
		ev := store.Event{SrcIP: c.src}
		if got := internalDef.compiled.match(ev, nil); got != c.wantInternal {
			t.Errorf("%s: internal condition matched = %v, want %v", c.src, got, c.wantInternal)
		}
		if got := externalDef.compiled.match(ev, nil); got == c.wantInternal {
			t.Errorf("%s: external condition matched = %v, want %v", c.src, got, !c.wantInternal)
		}
		// The index must agree with the compiled condition.
		cand := map[string]bool{}
		for _, d := range idx.Candidates(ev) {
			cand[d.ID()] = true
		}
		if cand["internal-src"] != c.wantInternal || cand["external-src"] == c.wantInternal {
			t.Errorf("%s: dispatch candidates %v disagree with the compiled classification (internal=%v)", c.src, cand, c.wantInternal)
		}
	}
}

// classKeyFor refuses a zoned address, as the store filter does.
func TestClassKeyForRefusesAZonedAddress(t *testing.T) {
	if _, ok := classKeyFor(FieldSourceAddress, "2001:db8::1%eth0"); ok {
		t.Error("a zoned address must not get a class key")
	}
}
