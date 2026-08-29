// SPDX-License-Identifier: AGPL-3.0-only
//
// The fall's own boundaries (#616): a band per boundary, where a
// boundary is a (chain, inInterface, outInterface) triple actually
// referenced by pushed firewall rules.
//
// This is a deliberately narrower reading of the record's "boundary
// (group-pair + direction), the same boundaries the coverage model
// (#392) and topography use" than the ratified design calls for. #392 is
// explicitly undesigned ("design input, not a work package") and its
// build is deferred to #485 (the topography), which #616 marks out of
// scope -- so there is no named-network-group model (LAN/SRV/GUEST/IOT,
// or a WAN interface) anywhere in mikroview to derive bands from. Rather
// than invent one (a product/config decision squarely for design, not
// this delegation), this reads the one boundary identity mikroview
// already has honestly: the interface pair a rule or event actually
// carries. See the PR description for the deviation this records.
//
// Coverage reuses internal/engine/coverage.go's own rule (mirrored here
// client-side rather than added as a new endpoint): only ever claim a
// definite answer. A boundary is 'dark' only when rules were pushed,
// they do reference this exact (chain, inInterface, outInterface), and
// none of them log -- otherwise 'unknown', which the UI renders as
// silence rather than a guess.

import { fetchDevices, fetchRouterRules, type RouterFilterRule } from './api'

export type BoundaryCoverage = 'unknown' | 'dark' | 'observed'

export interface FallBoundary {
  key: string
  chain: string
  inInterface: string
  outInterface: string
  label: string
  coverage: BoundaryCoverage
}

/**
 * boundaryKeyOf is the one key both a pushed FilterRule and a live
 * FirewallEvent are grouped by, so Fall.svelte can bucket real traffic
 * into the same boundaries boundariesFromRules computed from the rule
 * tables. `''` reads as "any"/unset on either side, matching how
 * RouterOS itself leaves an interface unscoped on many rules.
 */
export function boundaryKeyOf(chain: string, inInterface: string | undefined, outInterface: string | undefined): string {
  return `${chain}|${inInterface ?? ''}|${outInterface ?? ''}`
}

function boundaryLabel(chain: string, inIf: string, outIf: string): string {
  if (inIf && outIf) return `${inIf} → ${outIf}`
  if (inIf) return `${inIf} · ${chain}`
  if (outIf) return `${chain} · ${outIf}`
  return chain
}

/**
 * boundariesFromRules groups pushed filter rules by the (chain,
 * inInterface, outInterface) they actually carry, and answers coverage
 * per group with the same "only a definite answer" rule
 * internal/engine/coverage.go uses for a watchlist entry. Exported (not
 * just used internally) so it is unit-testable without a DOM.
 */
export function boundariesFromRules(rules: RouterFilterRule[], anyRulesPushed: boolean): FallBoundary[] {
  const byKey = new Map<
    string,
    { chain: string; inInterface: string; outInterface: string; sawLog: boolean }
  >()
  for (const r of rules) {
    const inIf = r.inInterface ?? ''
    const outIf = r.outInterface ?? ''
    const key = boundaryKeyOf(r.chain, inIf, outIf)
    let entry = byKey.get(key)
    if (!entry) {
      entry = { chain: r.chain, inInterface: inIf, outInterface: outIf, sawLog: false }
      byKey.set(key, entry)
    }
    if (r.log) entry.sawLog = true
  }
  const list: FallBoundary[] = []
  for (const [key, e] of byKey) {
    const coverage: BoundaryCoverage = anyRulesPushed ? (e.sawLog ? 'observed' : 'dark') : 'unknown'
    list.push({
      key,
      chain: e.chain,
      inInterface: e.inInterface,
      outInterface: e.outInterface,
      label: boundaryLabel(e.chain, e.inInterface, e.outInterface),
      coverage,
    })
  }
  // Alphabetical: deterministic, so it is stable across sessions and
  // renders even though it is not the record's semantic "WAN inbound
  // first, guarded/dark last" order -- that needs the network-role model
  // #392 leaves undesigned (see this file's header comment).
  list.sort((a, b) => a.label.localeCompare(b.label))
  return list
}

class FallState {
  boundaries = $state<FallBoundary[]>([])
  loading = $state(true)
  error = $state<string | null>(null)

  async refresh() {
    try {
      const devices = await fetchDevices()
      const tables = await Promise.all(
        devices.map((d) =>
          fetchRouterRules(d.id).catch(
            () => ({ available: false, rules: [] as RouterFilterRule[] }) as const,
          ),
        ),
      )
      const rules: RouterFilterRule[] = []
      let anyAvailable = false
      for (const table of tables) {
        if (table.available) anyAvailable = true
        rules.push(...table.rules)
      }
      this.boundaries = boundariesFromRules(rules, anyAvailable)
      this.error = null
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
    } finally {
      this.loading = false
    }
  }
}

export const fallState = new FallState()
