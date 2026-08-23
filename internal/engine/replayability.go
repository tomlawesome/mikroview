// SPDX-License-Identifier: AGPL-3.0-only

package engine

// Replayable is implemented by a definition whose own evaluation logic
// can honestly be re-run over a stored corpus with candidate params --
// docs/decisions/evaluation-engine.md section 4's "every definition
// exposes, uniformly: enabled, scope, its typed params, and replay",
// grown onto the Evaluated shape (engine.go) as its own, separate,
// optional interface rather than a new required method on Evaluated
// itself: not every definition kind can honestly answer a replay
// question at all (see NonReplayable), so requiring every Evaluated to
// implement Replay would force exactly the silent/confident-zero
// dishonesty this contract exists to rule out. See
// DeclarativeDefinition.Replay (declarative.go) for the proof-of-
// contract implementation issue #403 asks for.
type Replayable interface {
	// Replay re-runs this definition's logic over every event corpus
	// currently has available, with candidate overriding whichever of
	// this definition's own tunable params it names (absent keys keep
	// the definition's own current value; an unknown key is a hard
	// error, same convention ValidateParams already uses). Must never
	// mutate this definition's own live evaluation state -- see the
	// implementing type's own doc comment for its isolation guarantee,
	// and must never be affected by, or race with, concurrent live
	// Evaluate calls.
	Replay(corpus Corpus, candidate Params) (Result, error)
}

// NonReplayable is implemented by a definition that never implements
// Replayable, declaring why. Issue #403's own requirement: "A
// definition may declare itself NON-REPLAYABLE with a stated reason,
// surfaced through the contract rather than a silent/confident zero."
//
// The issue names the honest candidates for this once the programmatic
// port (#405/#406) lands:
//
//   - reputation: a replay-time lookup against today's reputation data
//     is not evidence about what would have been true at the time each
//     corpus event actually occurred -- replaying it would silently
//     mix "what this IP's reputation is now" into a judgement about
//     "what would this definition have said back then."
//   - absence-of-events definitions (device_silence, stale_rule kind):
//     "nothing arrived for five minutes" is not a predicate any
//     per-event corpus walk can evaluate -- there is no event for a
//     condition to match against, the same reasoning Kind's own doc
//     comment (definition.go) gives for why these are programmatic at
//     all, not declarative.
//   - floor-exceeds-corpus: a definition whose own history floor (see
//     BaselineFloor, baseline.go) structurally exceeds any corpus this
//     process could ever hold in memory -- e.g. a 14-distinct-day floor
//     against an in-memory window measured in minutes (see Corpus's own
//     doc comment). Unlike the dynamic, per-call "this particular
//     corpus is currently too short" case (Decline, replay.go), this is
//     a permanent property of the definition: no amount of waiting
//     produces a corpus long enough while replay's corpus stays the
//     in-memory ring, so the definition declares it once rather than
//     declining on every call.
//
// None of those three concrete cases exists in this codebase yet
// (#405/#406's job, deliberately out of this issue's scope -- see
// declarative.go's own note that DeclarativeDefinition is this issue's
// only concrete Replayable implementation); this interface is what a
// definition implements to model the declaration once one does. See
// TestReplayabilityClassifiesNonReplayableDeclaration for the mechanism
// exercised against a minimal test-only definition.
type NonReplayable interface {
	// NonReplayableReason states, in one sentence suitable for
	// surfacing to an operator, why this definition never answers a
	// replay question.
	NonReplayableReason() string
}

// Replayability classifies d into exactly the two structural
// possibilities issue #403 requires every definition to resolve to: it
// implements Replayable (receiptCapable=true), or it implements
// NonReplayable (receiptCapable=false, reason set) -- "every definition
// implements or declares non-replayable with reason." A definition set
// (#404's DefinitionsStore, or whatever iterates registered definitions
// once #407's API exists) calls this once per definition rather than
// re-deriving the same type-switch at every call site -- the "the
// definition set knowing the difference" issue #403 asks for.
//
// ok is false in the two cases that should not occur for any definition
// kind this codebase ships: implementing neither interface (a
// definition kind that has not yet declared its replayability at all --
// a construction-time bug in whatever registered it, not a state
// issue #403's own definitions can reach; see
// TestDeclarativeDefinitionImplementsReplayable) or implementing both
// (an unresolvable ambiguity about which contract governs, which Go's
// type system cannot prevent at compile time, so this function refuses
// to silently pick one).
func Replayability(d Evaluated) (receiptCapable bool, reason string, ok bool) {
	_, isReplayable := d.(Replayable)
	nr, isNonReplayable := d.(NonReplayable)
	switch {
	case isReplayable && !isNonReplayable:
		return true, "", true
	case isNonReplayable && !isReplayable:
		return false, nr.NonReplayableReason(), true
	default:
		return false, "", false
	}
}
