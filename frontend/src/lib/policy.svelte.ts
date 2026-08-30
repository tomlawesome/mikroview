// SPDX-License-Identifier: AGPL-3.0-only
//
// The topography's intended-policy edges (#485 layer 2, #628): what the
// pushed rule tables say may cross, refused where they say it may not.
// One edge per boundary pair per direction, aggregated, with port-set
// badges -- the shaped surface's own words.
//
// Only the forward chain draws here. A policy edge is a crossing
// between two boundaries; input and output rules terminate at the
// router itself, so they have no pair to draw -- they stay the fall's
// business (boundariesFromRules), not the map's. Declared on #628.
//
// Everything derives from the pushed tables alone: no rule table, no
// edges -- and the scene says which push is missing rather than drawing
// a guess (the same honesty contract zones.svelte.ts documents).
import { appState } from './state.svelte'
import { fetchRouterRules, type RouterFilterRule } from './api'

export interface PolicyEdge {
  /** `${from}|${to}` -- stable across refreshes for keyed each-blocks. */
  key: string
  /** Boundary interface names; '' means the rule named no interface on
   * that side, which RouterOS reads as "any" and the map draws from the
   * waist itself rather than inventing an endpoint. */
  from: string
  to: string
  /** Any accept (or fasttrack) rule on this pair+direction. */
  accepted: boolean
  /** Any drop/reject rule -- drawn dying at the waist, ⊣, in calm ink:
   * an intended refusal is policy, not the alarm (the one saturated
   * colour stays reserved for layer 3's unplanned traffic). */
  refused: boolean
  /** Port badges from the accepting rules, ":443" form, deduplicated in
   * table order. */
  acceptPorts: string[]
  /** Port badges from the refusing rules. */
  refusePorts: string[]
  /** First comment on the pair, table order -- the edge's epithet. */
  comment: string
  ruleCount: number
}

// Actions that mean "may pass" / "may not". Everything else on the
// forward chain (jump, return, log-only, passthrough) routes evaluation
// rather than answering it, so it draws nothing -- an edge must never
// claim an answer a rule did not give.
const ACCEPTS = new Set(['accept', 'fasttrack-connection'])
const REFUSALS = new Set(['drop', 'reject'])

// dstPort arrives as RouterOSPortSpec: a single port is a JSON number,
// a list or range the string RouterOS prints ("80,443", "1000-2000").
function portTokens(spec: number | string | undefined): string[] {
  if (spec === undefined || spec === '') return []
  if (typeof spec === 'number') return [`:${spec}`]
  return spec
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
    .map((t) => `:${t}`)
}

/**
 * policyEdgesFromRules aggregates pushed forward-chain rules into one
 * edge per (from, to) pair -- direction is the pair's order, so A→B and
 * B→A are two edges, matching "one line per group-pair, per direction".
 * Exported pure, like boundariesFromRules, so it unit-tests without a
 * DOM.
 *
 * Rule order (first match wins on the router) is not re-evaluated here:
 * the edge reports what the table names on a pair, not which rule a
 * given packet would meet first. That is the aggregate's honest limit,
 * declared on #628 -- layer 3 answers what actually happened.
 */
export function policyEdgesFromRules(rules: RouterFilterRule[]): PolicyEdge[] {
  const byPair = new Map<string, PolicyEdge>()
  const ordered = [...rules].sort((a, b) => a.ordinal - b.ordinal)
  for (const r of ordered) {
    if (r.chain !== 'forward') continue
    const accepted = ACCEPTS.has(r.action)
    const refused = REFUSALS.has(r.action)
    if (!accepted && !refused) continue
    const from = r.inInterface ?? ''
    const to = r.outInterface ?? ''
    const key = `${from}|${to}`
    let e = byPair.get(key)
    if (!e) {
      e = {
        key,
        from,
        to,
        accepted: false,
        refused: false,
        acceptPorts: [],
        refusePorts: [],
        comment: '',
        ruleCount: 0,
      }
      byPair.set(key, e)
    }
    e.ruleCount++
    if (accepted) e.accepted = true
    if (refused) e.refused = true
    if (!e.comment && r.comment) e.comment = r.comment
    const badges = accepted ? e.acceptPorts : e.refusePorts
    for (const t of portTokens(r.dstPort)) {
      if (!badges.includes(t)) badges.push(t)
    }
  }
  return [...byPair.values()].sort((a, b) => b.ruleCount - a.ruleCount)
}

class PolicyState {
  /** The pushed filter tables, across every device that has pushed one
   * -- same fan-out zonesState.refresh uses for addresses. */
  pushed = $state<RouterFilterRule[]>([])
  /** False until any device has pushed a rule table: the lens's honest
   * empty state ("waiting for the push", never "broken") reads this. */
  anyPushed = $state(false)

  async refresh() {
    const all: RouterFilterRule[] = []
    let any = false
    for (const d of appState.devices) {
      try {
        const res = await fetchRouterRules(d.id)
        if (res.available) {
          any = true
          all.push(...res.rules)
        }
      } catch {
        // Absence is the empty state, already honest on the lens.
      }
    }
    this.pushed = all
    this.anyPushed = any
  }

  edges = $derived.by(() => policyEdgesFromRules(this.pushed))
}

export const policyState = new PolicyState()
