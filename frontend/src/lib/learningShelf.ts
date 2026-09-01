// SPDX-License-Identifier: AGPL-3.0-only

import type { LearningState } from './types'

// The learning shelf's warming question (#642): is any enabled
// detection still holding an observed key below its history floor?
// That is exactly the state engine.Snapshot.ProvisionalFire can fire
// in -- primed but not Ready -- so it is the one state in which the
// shelf's "a spike seen now appears here as provisional" claim is
// true. Deliberately NOT counted as warming (#642's ruling,
// amendment 2):
//   - a detection with no learning at all (no warm-up concept);
//   - a disabled detection, however warm its baseline;
//   - "no traffic seen yet" (keys 0) -- nothing observed can be
//     provisional, so claiming the shelf would catch a spike is false.
// The projection this reads is DetectorSettings (detectorSettings.
// svelte.ts), sourced from GET /api/definitions -- which #653 gates at
// the user tier, so a viewer can never evaluate this; Flags.svelte
// degrades by absence there rather than by a claim it cannot verify.
export interface WarmableDetector {
  enabled: boolean
  learning?: LearningState
}

export function anyBaselineWarming(detectors: readonly WarmableDetector[]): boolean {
  return detectors.some((d) => d.enabled && d.learning !== undefined && d.learning.keys > d.learning.ready)
}
