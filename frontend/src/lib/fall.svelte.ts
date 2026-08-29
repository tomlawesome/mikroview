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
// Amended by Fable's 2026-08-29 review (#616 comment) after the base
// version above shipped:
//
//   (a) A pushed rule's srcAddressList (e.g. "lan") is real,
//       operator-assigned evidence of a named group -- unlike the WAN/
//       guest/iot taxonomy, which nothing in mikroview names, this one
//       sometimes IS pushed. Where present, it replaces the interface
//       name on that side of the label. There is no dstAddressList in
//       RouterOS's own schema (ingest.FilterRule only carries a
//       src-side list name; the destination side is a raw address, not
//       a named list) -- the review comment's "srcAddressList/
//       dstAddressList" is read as "whichever address-list evidence
//       exists", which today is only ever the source side.
//   (b) Ordering is semantic, not alphabetical: input-chain/WAN-facing
//       bands first, observed forward bands next, dark (and
//       unknown-coverage) bands last, alphabetical within each class.
//       "WAN-facing" is realised as RouterOS's own `chain === 'input'`
//       (traffic addressed to the router itself) rather than guessing
//       which interface is "the" WAN -- a real distinction already in
//       the data, not an invented one.
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
  // The evidence this boundary was named from, if any -- '' when no
  // pushed rule for this boundary carried a src-address-list. Kept
  // alongside inInterface/outInterface (not replacing them) so the UI
  // can still say "ether1" in the aria-label/click-through even when the
  // header shows "lan".
  srcAddressList: string
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

function boundaryLabel(chain: string, inIf: string, outIf: string, srcAddressList: string): string {
  const inSide = srcAddressList || inIf
  if (inSide && outIf) return `${inSide} → ${outIf}`
  if (inSide) return `${inSide} · ${chain}`
  if (outIf) return `${chain} · ${outIf}`
  return chain
}

// bandClass sorts input-chain/WAN-facing bands first, observed forward
// (or output) bands next, and dark/unknown-coverage bands last -- the
// review's ordering amendment (b). RouterOS's `input` chain is traffic
// addressed to the router itself, a real distinction already in the
// data; nothing here guesses which interface is "the" WAN.
function bandClass(chain: string, coverage: BoundaryCoverage): 0 | 1 | 2 {
  if (chain === 'input') return 0
  if (coverage === 'observed') return 1
  return 2
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
    { chain: string; inInterface: string; outInterface: string; sawLog: boolean; srcAddressList: string }
  >()
  for (const r of rules) {
    const inIf = r.inInterface ?? ''
    const outIf = r.outInterface ?? ''
    const key = boundaryKeyOf(r.chain, inIf, outIf)
    let entry = byKey.get(key)
    if (!entry) {
      entry = { chain: r.chain, inInterface: inIf, outInterface: outIf, sawLog: false, srcAddressList: '' }
      byKey.set(key, entry)
    }
    if (r.log) entry.sawLog = true
    // First non-empty address-list name wins and sticks -- rules on the
    // same boundary naming the same list is the expected case, and a
    // real disagreement is rarer than a rule further down the table
    // simply not bothering to repeat it.
    if (!entry.srcAddressList && r.srcAddressList) entry.srcAddressList = r.srcAddressList
  }
  const list: FallBoundary[] = []
  for (const [key, e] of byKey) {
    const coverage: BoundaryCoverage = anyRulesPushed ? (e.sawLog ? 'observed' : 'dark') : 'unknown'
    list.push({
      key,
      chain: e.chain,
      inInterface: e.inInterface,
      outInterface: e.outInterface,
      srcAddressList: e.srcAddressList,
      label: boundaryLabel(e.chain, e.inInterface, e.outInterface, e.srcAddressList),
      coverage,
    })
  }
  list.sort((a, b) => {
    const ca = bandClass(a.chain, a.coverage)
    const cb = bandClass(b.chain, b.coverage)
    if (ca !== cb) return ca - cb
    return a.label.localeCompare(b.label)
  })
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
