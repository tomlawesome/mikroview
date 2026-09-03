// SPDX-License-Identifier: AGPL-3.0-only
//
// Which unplanned pair the city escalates to bollards, a red mark and a
// callout (#865) -- the same choice Topography.svelte's own worst-
// unplanned card makes: worst means busiest (realityEdges already sorts
// by events, so this adds no third ranking to the app), ties break on
// drops, then the pair's own key, so the same data always escalates the
// same pair in both views. One pair only, however close the runners-up
// -- "worst" is a superlative, and two callouts would un-say it. Pure,
// so the two surfaces can be proven to agree without mounting either.
import type { CityEdge } from './input'

export function worstUnplanned(edges: CityEdge[]): CityEdge | null {
  let best: CityEdge | null = null
  for (const e of edges) {
    if (e.verdict !== 'unplanned') continue
    if (!best) {
      best = e
      continue
    }
    if (e.events !== best.events) {
      if (e.events > best.events) best = e
      continue
    }
    const ed = e.drops ?? 0
    const bd = best.drops ?? 0
    if (ed !== bd) {
      if (ed > bd) best = e
      continue
    }
    if (e.key < best.key) best = e
  }
  return best
}
