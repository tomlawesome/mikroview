// SPDX-License-Identifier: AGPL-3.0-only
//
// The one coverage rule (#392, #1014), in one place: something on the
// boundary-direction logs -> logged; nothing logs but an admin declared
// it intentionally quiet -> quiet; neither -> dark.
//
// It lived in three copies -- tuneLogging.ts's darkBoundaryKeys,
// Topography.svelte's coverageOf and zoneCaption -- and cityInputFrom
// carried a fourth that read the policy edges alone and never the
// declarations, so a boundary declared quiet still came out dark on the
// city plaque and the 2D zones card (#1014). Every one of them now calls
// in here, so the surfaces cannot disagree about what dark means.
import type { PolicyEdge } from './policy.svelte'

export type Coverage = 'logged' | 'quiet' | 'dark'

/** The rule itself, for one boundary-direction. `quietKeys` is the set
 * of declared keys -- coverageState.byKey's own keys. */
export function edgeCoverage(edge: Pick<PolicyEdge, 'key' | 'logged'>, quietKeys: ReadonlySet<string>): Coverage {
  if (edge.logged) return 'logged'
  return quietKeys.has(edge.key) ? 'quiet' : 'dark'
}

/**
 * boundaryCoverage reads a whole interface rather than one direction:
 * what the district plaque and the zones card say about a lane, which
 * has an edge each way (and sometimes more, to other lanes).
 *
 * Anything logging on it makes the lane logged, matching what the plate
 * has always dimmed on. Otherwise every remaining direction must be
 * declared quiet for the lane to read quiet -- one undeclared dark
 * direction is still a hole, the same reading zoneCaption already gives
 * ("DARK FROM WAN — quiet toward it by choice"). An interface no pushed
 * rule names at all is dark: a table was pushed and nothing on it
 * mentions this lane.
 */
export function boundaryCoverage(iface: string, edges: readonly PolicyEdge[], quietKeys: ReadonlySet<string>): Coverage {
  let named = false
  let allQuiet = true
  for (const e of edges) {
    if (e.from !== iface && e.to !== iface) continue
    named = true
    const st = edgeCoverage(e, quietKeys)
    if (st === 'logged') return 'logged'
    if (st === 'dark') allQuiet = false
  }
  return named && allQuiet ? 'quiet' : 'dark'
}
