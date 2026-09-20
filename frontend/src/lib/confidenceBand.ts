// SPDX-License-Identifier: AGPL-3.0-only
//
// The band a detector's confidence falls in (#1231, round 58's ratified
// "three-bands"): the 0-100 number left the flag row -- where it read as
// an event count beside the type -- and became a rating in the drawer,
// under the sparkline. This is the word that travels with it.
//
// The thresholds are the drawing's, not the engine's: emaConfidence
// (internal/engine/baseline.go) hands back 0-100 with no bands of its
// own, so nothing server-side has an opinion this could contradict. The
// band's colour lives with the markup that wears it (Flags.svelte's
// `.conf.c-*`), and the number and the word are always drawn alongside
// it, so the rating never rests on hue alone.
export type ConfidenceBand = 'low' | 'moderate' | 'high'

export function confidenceBand(confidence: number): ConfidenceBand {
  if (confidence >= 70) return 'high'
  if (confidence >= 40) return 'moderate'
  return 'low'
}
