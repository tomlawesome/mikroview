// SPDX-License-Identifier: AGPL-3.0-only
//
// Which unplanned pair the city escalates to bollards, a red mark and a
// callout (#865). The choice itself is reality.ts's worstUnplannedOf,
// shared with the topography's own escalated card (#715 item 4): the two
// views must name the same pair from the same data, and one function is
// the only way to keep that true. This file is the city's spelling of
// it, nothing more.
import { worstUnplannedOf } from '../reality'
import type { CityEdge } from './input'

export function worstUnplanned(edges: CityEdge[]): CityEdge | null {
  return worstUnplannedOf(edges, (e) => ({ verdict: e.verdict, events: e.events, drops: e.drops ?? 0, key: e.key }))
}
