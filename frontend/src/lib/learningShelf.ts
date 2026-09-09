// SPDX-License-Identifier: AGPL-3.0-only

// The learning shelf's warming question (#642): is any enabled
// detection still holding an observed key below its history floor?
// That is exactly the state engine.Snapshot.ProvisionalFire can fire
// in -- primed but not Ready -- so it is the one state in which the
// shelf's "a spike seen now appears here as provisional" claim is
// true.
//
// Since #768 the answer comes from GET /api/flags' own
// `baselinesWarming` field rather than being computed here from the
// definitions catalogue (the owner's decision, 2026-09-02). The shelf
// is a flags surface: it re-renders on the flags poll, so reading its
// warming state from a second endpoint on a different cadence let it
// disagree with the flags beside it. The rules that decide the answer
// -- a disabled detection, a detection with no warm-up concept and "no
// traffic seen yet" all being not-warming (#642's ruling, amendment 2)
// -- are unchanged; they now live in the handler, internal/api/flags.
// go's baselinesWarming.
//
// The field is absent when the server cannot say (no live engine
// wired), and that is not "false": the shelf then shows nothing rather
// than claiming baselines have settled. Flags.svelte degrades by
// absence there, the app's grammar (#653).
export interface WarmingSignal {
  baselinesWarming?: boolean
}

export function anyBaselineWarming(signal: WarmingSignal): boolean {
  return signal.baselinesWarming === true
}
