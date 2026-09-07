// SPDX-License-Identifier: AGPL-3.0-only
//
// A gate (#865): an accept rule crossing a district boundary, keyed by
// chain plus in/out interface -- boundaryKeyOf's own shape
// (fall.svelte.ts), reused rather than invented afresh so a gate and the
// fall's boundary band agree on what "a boundary" means. This answers a
// narrower question than the fall's boundariesFromRules (does anything
// log here, for coverage): does an accept rule stand on this exact
// boundary at all, and does IT log -- the gate's own lamp.
//
// Honesty rule (#865, non-negotiable): a gate is only ever built from
// rules a router actually pushed. Called with nothing pushed at all
// (anyPushed false, upstream), the caller passes [] rather than this
// function guessing -- see cityInputFrom's gates field.
import { boundaryKeyOf } from '../fall.svelte'
import type { RouterFilterRule } from '../api'
import { edgeCoverage, type Coverage } from '../coverageRule'
import type { PolicyEdge } from '../policy.svelte'

// The same accept vocabulary policy.svelte.ts's ACCEPTS set reads --
// kept local so this module depends on nothing but the shared key
// function; a gate and a policy edge answer different questions and
// have no reason to share more than that.
const GATE_ACCEPTS = new Set(['accept', 'fasttrack-connection'])

export interface CityGate {
  key: string
  chain: string
  inInterface: string
  outInterface: string
  /** An accept rule on this exact boundary logs: the gate's lamp. */
  logged: boolean
  ruleCount: number
  comment: string
  /** The declaration/policy-edge key for this direction, `from|to` --
   * a different namespace from `key` above (chain|in|out), and the one
   * the declare API (#392) and coverageRule.ts both speak. */
  edgeKey: string
  /** The same boundary the other way round. A wall has no direction, so
   * both go on its card (round 49, #1016). */
  reverseEdgeKey: string
  /** How each direction reads under the one rule (lib/coverageRule.ts).
   * Round 49 makes this the gate's material: accent posts and a lamp
   * when logged, grey posts and no lamp otherwise. */
  coverage: Coverage
  reverseCoverage: Coverage
}

/** Dark is worse than quiet is worse than logged. A wall edge takes the
 * worse of its gates' directions, because a wall has no direction. */
const COVERAGE_RANK: Record<Coverage, number> = { logged: 0, quiet: 1, dark: 2 }
export const worseCoverage = (a: Coverage, b: Coverage): Coverage => (COVERAGE_RANK[a] >= COVERAGE_RANK[b] ? a : b)
/** The kinder of two readings: a boundary is only dark when every one of
 * its directions is. */
export const betterCoverage = (a: Coverage, b: Coverage): Coverage => (COVERAGE_RANK[a] <= COVERAGE_RANK[b] ? a : b)

/**
 * gatesFromRules finds every boundary an accept (or fasttrack) rule
 * crosses -- a gate in the wall. Only the forward chain: input/output
 * rules terminate at the router itself, the same reasoning
 * policy.svelte.ts's policyEdgesFromRules already documents for the
 * topography's policy edges.
 */
export function gatesFromRules(
  rules: RouterFilterRule[],
  /** Every pushed boundary-direction, for the coverage reading. A
   * direction no pushed rule names at all logs nothing, which is what
   * the empty default reads as -- never a guessed lamp. */
  policyEdges: readonly PolicyEdge[] = [],
  /** The declared-quiet keys (#392 -- coverageState.byKey's own). */
  quietKeys: ReadonlySet<string> = new Set(),
): CityGate[] {
  const byKey = new Map<string, CityGate>()
  for (const r of rules) {
    if (r.chain !== 'forward') continue
    if (!GATE_ACCEPTS.has(r.action)) continue
    const inIf = r.inInterface ?? ''
    const outIf = r.outInterface ?? ''
    const key = boundaryKeyOf(r.chain, inIf, outIf)
    let g = byKey.get(key)
    if (!g) {
      g = {
        key,
        chain: r.chain,
        inInterface: inIf,
        outInterface: outIf,
        logged: false,
        ruleCount: 0,
        comment: '',
        edgeKey: `${inIf}|${outIf}`,
        reverseEdgeKey: `${outIf}|${inIf}`,
        coverage: 'dark',
        reverseCoverage: 'dark',
      }
      byKey.set(key, g)
    }
    g.ruleCount++
    if (r.log) g.logged = true
    if (!g.comment && r.comment) g.comment = r.comment
  }
  // The coverage reading comes from the pushed boundary-direction where
  // there is one -- anything on it that logs, not only an accept rule --
  // and falls back to the gate's own accept rules when no policy edge
  // names the direction at all. One rule, one place: coverageRule.ts.
  const byEdgeKey = new Map<string, PolicyEdge>()
  for (const e of policyEdges) byEdgeKey.set(e.key, e)
  const readingFor = (edgeKey: string, logged: boolean): Coverage => {
    const e = byEdgeKey.get(edgeKey)
    return edgeCoverage(e ?? { key: edgeKey, logged }, quietKeys)
  }
  for (const g of byKey.values()) {
    g.coverage = readingFor(g.edgeKey, g.logged)
    // Nothing on this side of the boundary is claimed from the forward
    // gate's own rules: the reverse direction has its own accept rules
    // (its own gate) or none at all.
    g.reverseCoverage = readingFor(g.reverseEdgeKey, false)
  }
  return [...byKey.values()]
}
