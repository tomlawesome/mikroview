// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "time"

// This file is issue #639's engine-side half: exposing per-definition
// baseline warm-up state to the definitions API, so a fresh install can
// say "no traffic seen yet, needs 14 days" instead of looking identical
// to "nothing wrong." See that issue's architecture record for why this
// has to ask the *live* engine (Engine.Learning) rather than being
// derived from BuildForInspection or persisted StateStore -- both answer
// a different question (what a definition's type is, not what its
// running instance has actually accumulated).

// LearningProgress is one not-yet-ready key's progress toward its
// definition's BaselineFloor, in the floor's own raw dimensions -- never
// a pre-baked percentage, so a caller decides how (or whether) to
// collapse "3 of 14 days" into a bar rather than being handed one
// already-lossy number.
type LearningProgress struct {
	// ObservedFor is how long this key has been tracked, per
	// Baseline.Snapshot's FirstSeen -- meaningful against
	// BaselineFloor.MinDuration.
	ObservedFor time.Duration
	// Samples is how many readings this key's baseline has folded in --
	// meaningful against BaselineFloor.MinSamples.
	Samples int
}

// LearningState is one definition's baseline warm-up state as of one
// moment -- what LearningReporter answers, and what the definitions API
// (#639) renders as the "learning" field. A definition may track many
// keys (one baseline per source, per rule, ...), so this is a census
// across all of them, never a single collapsed "ready" bool: a
// definition-level ready is a lie the moment a definition tracks more
// than one key -- see #639's own superseded scoping note.
type LearningState struct {
	// Floor is the history this definition's baselines require before
	// any of them may fire -- known statically from the definition's own
	// params, so it is present even when Keys is 0 (the fresh-install
	// case this issue exists for): an operator can be told what is
	// needed before anything has been observed to measure against it.
	Floor BaselineFloor
	// Keys is how many distinct keys this definition has ever tracked a
	// baseline for -- "sources seen," not a total census: a restarted
	// process repopulates keys lazily as traffic actually arrives, and an
	// evicted key (Keyed's own bound) simply stops counting. Understated
	// after a restart or under eviction, never overstated -- the
	// direction #639's design record prefers.
	Keys int
	// Ready is how many of those Keys have cleared Floor -- i.e. how many
	// could actually fire today.
	Ready int
	// Nearest is the not-yet-ready key furthest along toward Floor, or
	// nil when every observed key is ready, or when Keys is 0. Exactly
	// one key: an operator asking "how much longer" wants the most
	// encouraging honest answer, not an arbitrary one.
	Nearest *LearningProgress
}

// LearningReporter is the optional interface a definition with a
// baseline-shaped warm-up implements -- one per baseline-backed shipped
// definition (activity_spike, global_spike, rule_spike, off_hours,
// low_slow_scan). ok is false for every definition without a warm-up
// concept at all (most of the catalogue), which is what lets the API
// field be omitted entirely rather than sent as a misleading zero value
// -- see api.definitionView's own Coverage field for the same "omit
// rather than guess" convention this follows.
type LearningReporter interface {
	Learning(now time.Time) (state LearningState, ok bool)
}

// baselineLearning is one key's learning progress as of the moment a
// baselineSet.learning (or, for global_spike's single un-keyed Baseline,
// a direct Baseline.Snapshot) call captured it -- the input
// learningStateFrom reduces into a LearningState. Unexported: this is
// purely the seam between a per-key read and the shared reduction below,
// never part of the engine's own public surface.
type baselineLearning struct {
	ready       bool
	observedFor time.Duration
	samples     int
}

// newBaselineLearning derives one key's baselineLearning from a
// Baseline.Snapshot taken as of now -- observedFor is zero rather than a
// huge nonsense duration for a key whose baseline has never actually
// been primed with a first reading (FirstSeen still the zero time; see
// Baseline.Reading's own doc comment for when that clears).
func newBaselineLearning(now time.Time, snap Snapshot) baselineLearning {
	var observedFor time.Duration
	if !snap.FirstSeen.IsZero() {
		observedFor = now.Sub(snap.FirstSeen)
	}
	return baselineLearning{ready: snap.Ready, observedFor: observedFor, samples: snap.Samples}
}

// learningStateFrom is the one place "how many keys, how many ready,
// which not-ready key is furthest along" is computed -- the aggregate
// logic issue #639 asks to live once, shared by every baseline-backed
// definition's Learning method (directly for the four that track one
// baselineSet, and over a hand-merged map for activity_spike's two --
// see that file's own Learning for why a merge, not two separate
// answers, is the honest one).
//
// "Furthest along" is the not-ready key with the greatest floorProgress
// against floor, ties broken by the lexically smaller key so the result
// is deterministic regardless of Go's randomized map iteration order.
func learningStateFrom(floor BaselineFloor, keys map[string]baselineLearning) LearningState {
	state := LearningState{Floor: floor, Keys: len(keys)}
	var nearestKey string
	var nearest *LearningProgress
	bestFrac := -1.0
	for key, k := range keys {
		if k.ready {
			state.Ready++
			continue
		}
		frac := floorProgress(floor, k.observedFor, k.samples)
		if frac > bestFrac || (frac == bestFrac && key < nearestKey) {
			bestFrac = frac
			nearestKey = key
			progress := LearningProgress{ObservedFor: k.observedFor, Samples: k.samples}
			nearest = &progress
		}
	}
	state.Nearest = nearest
	return state
}

// floorProgress reports how far (observedFor, samples) has gotten toward
// clearing floor, as the minimum of whichever dimensions floor actually
// binds (BaselineFloor.cleared's own conjunction, restated as a fraction
// rather than a bool) -- the smaller of two ratios is the one still
// blocking Ready, exactly as cleared requires *both* non-zero dimensions
// satisfied. 1.0 for a floor with neither dimension set: there is
// nothing to measure progress against, and Baseline.Reading's own
// priming gate is the only thing that could still be standing between
// such a key and Ready.
func floorProgress(floor BaselineFloor, observedFor time.Duration, samples int) float64 {
	frac, set := 1.0, false
	if floor.MinDuration > 0 {
		r := float64(observedFor) / float64(floor.MinDuration)
		if r > 1 {
			r = 1
		}
		frac, set = r, true
	}
	if floor.MinSamples > 0 {
		r := float64(samples) / float64(floor.MinSamples)
		if r > 1 {
			r = 1
		}
		if !set || r < frac {
			frac = r
		}
		set = true
	}
	return frac
}
