// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/detect"
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
	cfg := detect.DefaultConfig()
	byName := make(map[detect.DetectorName]shippedDetector, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		byName[sd.name] = sd
	}

	for id := range shippedDeclarativeBuilders {
		sd, ok := byName[detect.DetectorName(id)]
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
			Name:        shippedDetectorDisplayNames[sd.name],
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
		if _, ok := shippedDeclarativeBuilders[string(sd.name)]; !ok {
			t.Errorf("%q migrates as kind %q but has no shipped declarative builder registered", sd.name, sd.kind)
		}
	}
}
