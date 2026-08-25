// SPDX-License-Identifier: AGPL-3.0-only

import {
  fetchRouterRules,
  fetchRouterNat,
  type RouterFilterRule,
  type RouterNatRule,
  type RouterTable,
} from './api'
import { partitionNatTable, type NatEventFacts, type NatPartition } from './natMatch'
import { appState } from './state.svelte'

interface Anchor {
  x: number
  y: number
}

export type RouterLookupMode = 'rule' | 'nat'

// Which of the NAT popup's two modes is showing. They are two different
// claims and #445 requires that they never share a rendering, so this is
// announced in the popover's header and in a text chip rather than being
// left for the reader to infer from the body:
//
//  - 'logged': the operator put a log-prefix on the NAT rule, the event
//    carries it, so the rule is named. A fact.
//  - 'not-logged': nothing names a rule, so the table is partitioned
//    into what the event could have come from and what it rules out.
//    Never an answer.
export type NatMode = 'logged' | 'not-logged'

// What the popup was evaluated against. In Group mode a group's key does
// not include the interfaces (lib/grouping.ts), so members of one group
// can differ in exactly the fields the partition subtracts on -- the
// popup names its evidence rather than letting a head row's answer read
// as the whole group's.
export type NatEvidence = 'row' | 'group-head'

export interface NatLookupContext {
  // ruleLabel is the event's log-prefix slug, empty when the rule was
  // not tagged. It alone decides which of the two modes this is.
  ruleLabel: string
  facts: NatEventFacts
  evidence: NatEvidence
}

// Drives the single RouterLookupPopover instance mounted at the app root
// (see App.svelte) -- same singleton-plus-trigger-button shape as
// lib/ipLookup.svelte.ts, async like it, reading only mikroview's own
// pushed-state store. Nothing here ever contacts the router: the data was
// bulk-pushed by the router's own scheduled script (issue #186), which is
// the whole point -- a lookup that reached out live would reintroduce
// exactly the pull design #110 dropped. That extends to the NAT
// partition below, which is subtraction over what already arrived, never
// anything elicited.
//
// Two modes, per #186 step 4c's split as #445 revised it:
//  - 'rule': event-to-rule resolution via the operator's log-prefix on a
//    *filter* rule. The event's rule label IS the log-prefix, and when
//    several rules share one the honest answer is all of them, so
//    `rules` can hold more than one match.
//  - 'nat': the same resolution pointed at the NAT table when the event
//    carries a prefix, and otherwise the could-have/ruled-out partition.
//    #186 read "a NAT log line never names its rule" as meaning NAT
//    could only be a display table; #445 kept the observation and noted
//    that a filter line does not name its rule either -- the operator's
//    log-prefix does, and it works identically on a NAT rule.
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

  natMode = $state<NatMode>('not-logged')
  natEvidence = $state<NatEvidence>('row')
  // Populated in 'logged' mode only: the NAT rules carrying the event's
  // prefix. Empty with natMode 'logged' is its own honest state -- the
  // event was logged, but no pushed rule carries that prefix.
  natMatches = $state<RouterNatRule[]>([])
  // Populated in 'not-logged' mode only.
  natPartition = $state<NatPartition | null>(null)
  // The sheet surface's own visibility, since a sheet section has no
  // anchor coordinates to be present or absent.
  sheetOpen = $state(false)

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

  openNat(device: string, rect: DOMRect, ctx: NatLookupContext) {
    this.prepareNat(device, ctx)
    this.open(rect, this.loadNat(device, ctx))
  }

  // The mobile sheet shows the same lookup as one of its own sections
  // rather than as an anchored popover: there is no row to anchor to
  // behind an open sheet, and no hover to reveal a trigger. Same store,
  // same two modes, same wording -- only the surface differs.
  openNatInSheet(device: string, ctx: NatLookupContext) {
    this.prepareNat(device, ctx)
    this.anchor = null
    this.sheetOpen = true
    this.begin(this.loadNat(device, ctx))
  }

  private prepareNat(device: string, ctx: NatLookupContext) {
    this.mode = 'nat'
    this.device = device
    this.ruleLabel = ctx.ruleLabel
    this.natEvidence = ctx.evidence
    // The mode is decided by the event, before the table is even
    // fetched, and never revised by what the fetch finds. A logged event
    // whose prefix matches nothing stays "logged" and says the table
    // carries no such prefix; quietly falling through to the partition
    // would be the popup answering a question the operator did not ask,
    // in the rendering reserved for the other mode.
    this.natMode = ctx.ruleLabel ? 'logged' : 'not-logged'
  }

  private loadNat(device: string, ctx: NatLookupContext): () => Promise<void> {
    return async () => {
      const table: RouterTable<RouterNatRule> = await fetchRouterNat(device)
      this.available = table.available
      this.tableSize = table.rules.length
      this.natRules = table.rules
      if (ctx.ruleLabel) {
        this.natMatches = table.rules.filter((r) =>
          prefixMatchesLabel(r.logPrefix ?? '', ctx.ruleLabel),
        )
      } else {
        this.natPartition = partitionNatTable(table.rules, ctx.facts)
      }
    }
  }

  private open(rect: DOMRect, load: () => Promise<void>) {
    this.sheetOpen = false
  // Hold the stream while this is open (#413's "the stream holds while
  // you edit", stated once for every row-anchored surface). Newest-at-top
  // pushes rows down as events arrive, and a popover anchored to a row
  // that keeps moving is hostile. Guarded on anchor so re-opening for a
  // different token does not take a second hold it will never release.
    if (this.anchor === null) appState.holdStream()
    this.anchor = { x: rect.left, y: rect.bottom }
    this.begin(load)
  }

  private begin(load: () => Promise<void>) {
    this.loading = true
    this.error = null
    this.available = false
    this.rules = []
    this.natRules = []
    this.natMatches = []
    this.natPartition = null
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
    if (this.anchor === null) return
    this.anchor = null
    this.sheetOpen = false
    appState.releaseStream()
  }
}

export const routerLookupState = new RouterLookupState()

// The mode announcement, in one place so the popover and the mobile
// sheet cannot drift into telling the operator two different things
// about the same lookup.
export function natTitle(device: string, natMode: NatMode): string {
  return natMode === 'logged' ? 'NAT rule — logged' : `NAT table — ${device}`
}

export function natChip(natMode: NatMode): string {
  return natMode === 'logged' ? 'logged' : 'not logged'
}

// prefixMatchesLabel joins an event's rule label back to a pushed rule's
// verbatim log-prefix. An event only carries a rule label at all when
// its prefix followed mikroview's own "<ACTION>|<slug>|" convention
// (internal/routeros/prefix.go strips it, so ruleLabel is the inner
// slug while the rule's configured log-prefix is the full "D|slug|"),
// so that decode is the case that matters -- the verbatim comparison is
// kept for completeness, not because the parser can currently produce
// it.
//
// The code letters are the full set internal/routeros/prefix.go's
// actionFromCode accepts, not just the four filter verdicts: M (mangle)
// and N (NAT) became real codes in #437, and a NAT rule tagged
// "N|port-fwd|" is exactly what #445's logged mode resolves through. A
// list that stopped at "ADRL" would have made every logged NAT
// translation report itself as unresolvable.
export function prefixMatchesLabel(logPrefix: string, label: string): boolean {
  if (label === '') return false
  if (logPrefix === label) return true
  return (
    logPrefix.length === label.length + 3 &&
    'ADRLMN'.includes(logPrefix[0]) &&
    logPrefix[1] === '|' &&
    logPrefix.endsWith('|') &&
    logPrefix.slice(2, -1) === label
  )
}
