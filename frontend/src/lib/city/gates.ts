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
}

/**
 * gatesFromRules finds every boundary an accept (or fasttrack) rule
 * crosses -- a gate in the wall. Only the forward chain: input/output
 * rules terminate at the router itself, the same reasoning
 * policy.svelte.ts's policyEdgesFromRules already documents for the
 * topography's policy edges.
 */
export function gatesFromRules(rules: RouterFilterRule[]): CityGate[] {
  const byKey = new Map<string, CityGate>()
  for (const r of rules) {
    if (r.chain !== 'forward') continue
    if (!GATE_ACCEPTS.has(r.action)) continue
    const inIf = r.inInterface ?? ''
    const outIf = r.outInterface ?? ''
    const key = boundaryKeyOf(r.chain, inIf, outIf)
    let g = byKey.get(key)
    if (!g) {
      g = { key, chain: r.chain, inInterface: inIf, outInterface: outIf, logged: false, ruleCount: 0, comment: '' }
      byKey.set(key, g)
    }
    g.ruleCount++
    if (r.log) g.logged = true
    if (!g.comment && r.comment) g.comment = r.comment
  }
  return [...byKey.values()]
}
