// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
)

// TestEveryShippedDeclarativeDefinitionIsReplayable is issue #405's
// replayability requirement made executable for the declarative half of
// the port: "every ported definition implements #403's replay method or
// declares itself non-replayable with a stated reason."
//
// Every shipped declarative definition answers the first way, and
// structurally so rather than one at a time: replay is implemented on
// DeclarativeDefinition itself (replay_declarative.go), so a definition
// cannot be built by this package's declarative path and *not* be
// replayable. This test is what keeps that true as builders are added --
// it walks shippedDeclarativeBuilders rather than a hand-maintained list,
// so a new builder is covered the moment it is registered.
//
// Note what "replayable" does and does not promise: a definition whose
// own window is longer than the corpus actually available still declines
// rather than reporting a count (see DeclarativeDefinition.Replay's
// Decline path). Declining on a short corpus is the honest answer to a
// question that was asked; being non-replayable is a permanent property
// of the definition. Only the second needs declaring.
func TestEveryShippedDeclarativeDefinitionIsReplayable(t *testing.T) {
	cfg := DefaultShippedDefaults()
	byName := make(map[string]shippedDetector, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		byName[sd.id] = sd
	}

	for id := range shippedDeclarativeBuilders {
		sd, ok := byName[id]
		if !ok {
			t.Errorf("shipped declarative builder %q has no shippedDetectors entry to seed default params from", id)
			continue
		}
		if sd.kind != KindDeclarative {
			t.Errorf("%q has a declarative builder registered but migrates as kind %q -- the two must agree, or the migrated definition will never reach the builder", id, sd.kind)
		}

		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			t.Errorf("%q: building default params: %v", id, err)
			continue
		}
		def := Definition{
			ID:          id,
			Name:        shippedDetectorDisplayNames[sd.id],
			Intent:      IntentDetection,
			Kind:        KindDeclarative,
			Enabled:     true,
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		}
		dd, err := BuildShippedDeclarativeDefinition(def)
		if err != nil {
			t.Errorf("%q: BuildShippedDeclarativeDefinition: %v", id, err)
			continue
		}

		receiptCapable, reason, ok := Replayability(dd)
		if !ok {
			t.Errorf("%q: Replayability could not classify it -- it implements neither Replayable nor NonReplayable, or both", id)
			continue
		}
		if !receiptCapable {
			t.Errorf("%q declares itself non-replayable (%q), which no declarative definition should: its logic is a plain condition/window/threshold walk over events, exactly what a corpus can be re-walked with", id, reason)
		}
	}
}

// TestEveryDeclarativeShippedDetectorHasABuilder is the other direction
// of the same agreement: a shippedDetectors entry marked KindDeclarative
// with no registered builder would migrate to a definition main.go
// silently skips (see its BuildShippedDeclarativeDefinition error
// branch), leaving that detector evaluated by nothing at all -- a
// coverage hole whose only symptom is a startup warning.
func TestEveryDeclarativeShippedDetectorHasABuilder(t *testing.T) {
	for _, sd := range shippedDetectors {
		if sd.kind != KindDeclarative {
			continue
		}
		if _, ok := shippedDeclarativeBuilders[sd.id]; !ok {
			t.Errorf("%q migrates as kind %q but has no shipped declarative builder registered", sd.id, sd.kind)
		}
	}
}

// TestEveryShippedProgrammaticBuilderMatchesItsCatalogueEntry is the
// programmatic counterpart of the two declarative agreement tests above,
// and it exists because the port broke exactly this way once: off_hours
// was registered under "off_hours" while its shipped definition -- and
// therefore its flag type, since routeToFlag keys on the definition id --
// is "off_hours_activity". Every unit test still passed, because they
// built the definition by hand under the id the builder expected. In
// production main.go would have found no builder for
// "off_hours_activity", logged an info line, and evaluated nothing.
//
// So: every registered programmatic builder must name a shipped detector
// this binary actually migrates, and that entry must be KindProgrammatic.
func TestEveryShippedProgrammaticBuilderMatchesItsCatalogueEntry(t *testing.T) {
	byName := make(map[string]shippedDetector, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		byName[sd.id] = sd
	}
	for id := range shippedProgrammaticBuilders {
		sd, ok := byName[id]
		if !ok {
			t.Errorf("shipped programmatic builder %q names no shipped detector -- main.go would never find it, and the definition would evaluate nothing", id)
			continue
		}
		if sd.kind != KindProgrammatic {
			t.Errorf("%q has a programmatic builder registered but migrates as kind %q -- the two must agree", id, sd.kind)
		}
	}
}

// TestEveryShippedDefinitionIsClassifiedForReplay is #405's replayability
// requirement stated once for the whole catalogue rather than per kind:
// every shipped definition this binary can actually build must resolve to
// exactly one of Replayable or NonReplayable. A definition implementing
// neither is a construction bug; one implementing both is an
// unresolvable ambiguity Go cannot catch at compile time.
//
// Definitions with no builder yet are skipped rather than failed -- they
// are still evaluated by internal/detect, and this test grows to cover
// them as each one lands.
func TestEveryShippedDefinitionIsClassifiedForReplay(t *testing.T) {
	cfg := DefaultShippedDefaults()
	for _, sd := range shippedDetectors {
		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			t.Errorf("%s: building default params: %v", sd.id, err)
			continue
		}
		def := Definition{
			ID:          sd.id,
			Name:        shippedDetectorDisplayNames[sd.id],
			Intent:      IntentDetection,
			Kind:        sd.kind,
			Enabled:     true,
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		}

		var built Evaluated
		switch sd.kind {
		case KindDeclarative:
			dd, err := BuildShippedDeclarativeDefinition(def)
			if err != nil {
				t.Errorf("%s: BuildShippedDeclarativeDefinition: %v", sd.id, err)
				continue
			}
			built = dd
		case KindProgrammatic:
			if _, ok := shippedProgrammaticBuilders[sd.id]; !ok {
				continue // not ported yet
			}
			pd, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
			if err != nil {
				t.Errorf("%s: BuildShippedProgrammaticDefinition: %v", sd.id, err)
				continue
			}
			built = pd
		}

		receiptCapable, reason, ok := Replayability(built)
		if !ok {
			t.Errorf("%s: Replayability could not classify it -- it implements neither Replayable nor NonReplayable, or both", sd.id)
			continue
		}
		if !receiptCapable && strings.TrimSpace(reason) == "" {
			t.Errorf("%s declares itself non-replayable with an empty reason -- declaring is the opposite of hiding, so the reason is the whole point", sd.id)
		}
	}
}
