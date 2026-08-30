// SPDX-License-Identifier: AGPL-3.0-only
//
// The topography's reality overlay (#485 layer 3, #629): observed
// traffic laid onto the intended flows, deltas highlighted. Three
// deltas, #485's own words -- flows no rule anticipated, rules no
// packet ever exercises, and edges where drops concentrate.
//
// The one saturated colour finally earns its reservation here: an
// unplanned flow is the alarm. But "unplanned" is a claim about intent,
// so it is only ever made while a rule table has actually been pushed
// -- with nothing to judge against, observed traffic is 'unjudged',
// calm, and the scene says which push is missing (the same honesty
// contract zones.svelte.ts and policy.svelte.ts document).
import type { FirewallEvent } from './types'
import type { PolicyEdge } from './policy.svelte'

export interface RealityEdge {
  /** Same key grammar as PolicyEdge: `${from}|${to}`. */
  key: string
  from: string
  to: string
  events: number
  accepts: number
  drops: number
  /** Top accepted destination ports, ":443" form, busiest first. */
  topPorts: string[]
  /**
   * How reality relates to intent on this pair:
   * - planned: an accepting rule anticipated it.
   * - holding: a refusing rule anticipated it, and only drops arrive --
   *   the policy is doing its job; drawn dying at the waist, calm.
   * - unplanned: no rule anticipated the pair at all, or traffic passes
   *   where the table only refuses -- the alarm, both times.
   * - unjudged: no rule table pushed; no claim made either way.
   */
  verdict: 'planned' | 'holding' | 'unplanned' | 'unjudged'
}

/**
 * realityEdges groups observed forward crossings (events carrying both
 * boundaries) by pair, and judges each against the intended-policy
 * edges. Pure, like policyEdgesFromRules, for the same DOM-free
 * testability.
 */
export function realityEdges(events: FirewallEvent[], intents: PolicyEdge[], anyRulesPushed: boolean): RealityEdge[] {
  const byPair = new Map<string, { from: string; to: string; events: number; accepts: number; drops: number; ports: Map<string, number> }>()
  for (const e of events) {
    if (!e.inInterface || !e.outInterface) continue
    const key = `${e.inInterface}|${e.outInterface}`
    let r = byPair.get(key)
    if (!r) {
      r = { from: e.inInterface, to: e.outInterface, events: 0, accepts: 0, drops: 0, ports: new Map() }
      byPair.set(key, r)
    }
    r.events++
    if (e.action === 'accept') {
      r.accepts++
      if (e.dstPort !== undefined) {
        const t = `:${e.dstPort}`
        r.ports.set(t, (r.ports.get(t) ?? 0) + 1)
      }
    } else if (e.action === 'drop' || e.action === 'reject') {
      r.drops++
    }
  }
  const intentByKey = new Map(intents.map((i) => [i.key, i]))
  return [...byPair.values()]
    .map((r) => {
      const intent = intentByKey.get(`${r.from}|${r.to}`)
      let verdict: RealityEdge['verdict']
      if (!anyRulesPushed) verdict = 'unjudged'
      else if (intent?.accepted) verdict = 'planned'
      else if (intent?.refused) verdict = r.accepts > 0 ? 'unplanned' : 'holding'
      else verdict = 'unplanned'
      return {
        key: `${r.from}|${r.to}`,
        from: r.from,
        to: r.to,
        events: r.events,
        accepts: r.accepts,
        drops: r.drops,
        topPorts: [...r.ports.entries()].sort((a, b) => b[1] - a[1]).map(([t]) => t),
        verdict,
      }
    })
    .sort((a, b) => b.events - a.events)
}

/**
 * unexercisedIntents: the second delta -- accepting rules no packet has
 * ever exercised, returned as the intended edges reality never touched.
 * Refused pairs with no traffic are not listed: silence on a refusal is
 * the intended outcome, not a gap worth ink.
 */
export function unexercisedIntents(observed: RealityEdge[], intents: PolicyEdge[]): PolicyEdge[] {
  const seen = new Set(observed.map((o) => o.key))
  return intents.filter((i) => i.accepted && !seen.has(i.key))
}
