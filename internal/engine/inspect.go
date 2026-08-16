// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"

	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is what the definitions API (issue #407) asks a stored
// definition rather than asking the running engine: is this replayable,
// and if so, what would these candidate params have done?
//
// It builds the definition's live logic from its envelope and throws the
// result away. That is deliberate, and it is the only honest way to
// answer either question: replayability is a property of the concrete
// Go type a definition's Kind/ID resolves to (see Replayability,
// replayability.go), not of the envelope -- the envelope has no field
// that could carry it, and a hand-maintained table of "which ids are
// replayable" would be a second source of truth that drifts the first
// time a definition changes kind. Building the real thing and asking it
// cannot drift.
//
// Nothing built here is ever registered, and nothing built here is ever
// handed a dependency: BuildForInspection passes a zero ShippedDeps,
// whose every field is documented as an optional "not configured" no-op
// (programmatic.go). A definition built this way therefore has no flag
// store to raise into, no blocklist to consult and no sink to emit
// through -- which is exactly right for something whose only job is to
// answer a question about itself.

// BuildForInspection constructs def's live evaluation logic, detached
// from the engine, for a caller that wants to interrogate the definition
// rather than run it. See this file's own doc comment for why this
// builds the real type instead of consulting a table.
//
// The four cases are the four (Intent, Kind) combinations this codebase
// produces, and each is built by the same constructor production uses:
//
//   - expectation + declarative: a non-inverted watchlist entry
//     (BuildExpectationDefinition). Address-list membership is nil, so an
//     entry scoped to a router list resolves nothing -- correct for
//     inspection, where there is no live event to resolve against
//     anyway.
//   - expectation + programmatic: an inverted entry's state machine
//     (NewInvertedExpectations over just this entry).
//   - detection + declarative: a shipped declarative definition
//     (BuildShippedDeclarativeDefinition).
//   - detection + programmatic: a shipped programmatic definition
//     (BuildShippedProgrammaticDefinition) with zero deps.
func BuildForInspection(def Definition) (Evaluated, error) {
	if def.Intent == IntentExpectation {
		if def.Kind == KindDeclarative {
			return BuildExpectationDefinition(def, nil)
		}
		e, err := EntryFromDefinition(def)
		if err != nil {
			return nil, err
		}
		return NewInvertedExpectations([]watchlist.Entry{e}, nil)
	}
	if def.Kind == KindDeclarative {
		return BuildShippedDeclarativeDefinition(def)
	}
	return BuildShippedProgrammaticDefinition(def, ShippedDeps{})
}

// ReplayabilityOf answers, for one stored definition, the question issue
// #403 requires every definition to resolve: does it produce a replay
// receipt, or does it decline permanently with a stated reason?
//
// ok is false when the definition could not be built at all -- an id this
// binary has no logic for (a document written by a newer build), or an
// envelope whose params no longer satisfy its own schema. That is
// reported as its own case rather than folded into "not replayable":
// "this binary cannot build this definition" and "this definition can
// never answer a replay question" are different facts, and a UI that
// showed the second for the first would be stating a property of the
// definition that nobody has established.
func ReplayabilityOf(def Definition) (receiptCapable bool, reason string, ok bool) {
	built, err := BuildForInspection(def)
	if err != nil {
		return false, err.Error(), false
	}
	return Replayability(built)
}

// ReplayDefinition re-runs def's own logic over corpus with candidate
// params overriding whichever of its tunable params they name -- the
// engine side of POST /api/definitions/{id}/replay.
//
// Refuses, rather than returning an empty result, for a definition that
// declares itself non-replayable: a caller must be able to tell "it
// would have fired zero times" from "this question cannot honestly be
// asked of this definition", which is the whole reason Replayable and
// NonReplayable are separate interfaces.
func ReplayDefinition(def Definition, corpus Corpus, candidate Params) (Result, error) {
	built, err := BuildForInspection(def)
	if err != nil {
		return Result{}, err
	}
	replayable, ok := built.(Replayable)
	if !ok {
		capable, reason, classified := Replayability(built)
		switch {
		case classified && !capable:
			return Result{}, fmt.Errorf("engine: definition %q is not replayable: %s", def.ID, reason)
		default:
			return Result{}, fmt.Errorf("engine: definition %q neither implements replay nor declares itself non-replayable", def.ID)
		}
	}
	return replayable.Replay(corpus, candidate)
}
