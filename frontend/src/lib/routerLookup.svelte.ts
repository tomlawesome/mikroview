// SPDX-License-Identifier: AGPL-3.0-only

import {
  fetchRouterRules,
  fetchRouterNat,
  type RouterFilterRule,
  type RouterNatRule,
  type RouterTable,
} from './api'

interface Anchor {
  x: number
  y: number
}

export type RouterLookupMode = 'rule' | 'nat'

// Drives the single RouterLookupPopover instance mounted at the app root
// (see App.svelte) -- same singleton-plus-trigger-button shape as
// lib/ipLookup.svelte.ts, async like it, reading only mikroview's own
// pushed-state store. Nothing here ever contacts the router: the data was
// bulk-pushed by the router's own scheduled script (issue #186), which is
// the whole point -- a lookup that reached out live would reintroduce
// exactly the pull design #110 dropped.
//
// Two modes, per #186 step 4c's split:
//  - 'rule': event-to-rule resolution via the operator's log-prefix. The
//    event's rule label IS the log-prefix, and when several rules share
//    one the honest answer is all of them, so `rules` can hold more than
//    one match.
//  - 'nat': the whole NAT table, faithfully numbered -- a log line
//    carries a translation result, never which rule performed it, so
//    there is nothing to resolve, just the table to show.
class RouterLookupState {
  anchor = $state<Anchor | null>(null)
  mode = $state<RouterLookupMode>('rule')
  device = $state('')
  ruleLabel = $state('')
  loading = $state(false)
  error = $state<string | null>(null)
  // available=false (with no error) means the device has never pushed
  // this table -- rendered as "no data pushed yet", never as an empty
  // table pretending to be a real one.
  available = $state(false)
  rules = $state<RouterFilterRule[]>([])
  natRules = $state<RouterNatRule[]>([])
  // tableSize is the full pushed table's length in 'rule' mode, so the
  // popover can say "no rule carries this prefix" while making clear a
  // table WAS pushed -- absence of a match is not absence of the table.
  tableSize = $state(0)

  private requestId = 0

  openRule(device: string, ruleLabel: string, rect: DOMRect) {
    this.mode = 'rule'
    this.device = device
    this.ruleLabel = ruleLabel
    this.open(rect, async () => {
      const table: RouterTable<RouterFilterRule> = await fetchRouterRules(device)
      this.available = table.available
      this.tableSize = table.rules.length
      this.rules = table.rules.filter((r) => prefixMatchesLabel(r.logPrefix, ruleLabel))
    })
  }

  openNat(device: string, rect: DOMRect) {
    this.mode = 'nat'
    this.device = device
    this.ruleLabel = ''
    this.open(rect, async () => {
      const table: RouterTable<RouterNatRule> = await fetchRouterNat(device)
      this.available = table.available
      this.tableSize = table.rules.length
      this.natRules = table.rules
    })
  }

  private open(rect: DOMRect, load: () => Promise<void>) {
    this.anchor = { x: rect.left, y: rect.bottom }
    this.loading = true
    this.error = null
    this.available = false
    this.rules = []
    this.natRules = []
    this.tableSize = 0

    const id = ++this.requestId
    load().then(
      () => {
        if (id !== this.requestId) return
        this.loading = false
      },
      () => {
        if (id !== this.requestId) return
        this.error = 'Lookup failed'
        this.loading = false
      },
    )
  }

  close() {
    this.anchor = null
  }
}

export const routerLookupState = new RouterLookupState()

// prefixMatchesLabel joins an event's rule label back to a pushed rule's
// verbatim log-prefix. An event only carries a rule label at all when
// its prefix followed mikroview's own "<ACTION>|<slug>|" convention
// (internal/routeros/prefix.go strips it, so ruleLabel is the inner
// slug while the rule's configured log-prefix is the full "D|slug|"),
// so that decode is the case that matters -- the verbatim comparison is
// kept for completeness, not because the parser can currently produce
// it.
export function prefixMatchesLabel(logPrefix: string, label: string): boolean {
  if (label === '') return false
  if (logPrefix === label) return true
  return (
    logPrefix.length === label.length + 3 &&
    'ADRL'.includes(logPrefix[0]) &&
    logPrefix[1] === '|' &&
    logPrefix.endsWith('|') &&
    logPrefix.slice(2, -1) === label
  )
}
