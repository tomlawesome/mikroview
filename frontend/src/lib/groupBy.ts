// SPDX-License-Identifier: AGPL-3.0-only

import type { FirewallEvent } from './types'

// Dimensions a custom top-talkers widget can rank by (see
// lib/topTalkers.svelte.ts). Deliberately excludes 'device' -- that's
// already its own fixed ledger column on metrics (MetricsTotals.svelte's
// "By device"), and mapping a device id to its display name
// here would need appState.devices threaded through just for that one
// case.
export type GroupByField =
  | 'srcIp'
  | 'dstIp'
  | 'srcPort'
  | 'dstPort'
  | 'rule'
  | 'protocol'
  | 'action'
  | 'chain'
  | 'interface'

export const GROUP_BY_LABELS: Record<GroupByField, string> = {
  srcIp: 'Source IP',
  dstIp: 'Destination IP',
  srcPort: 'Source port',
  dstPort: 'Destination port',
  rule: 'Rule',
  protocol: 'Protocol',
  action: 'Action',
  chain: 'Chain',
  interface: 'Interface',
}

export const GROUP_BY_FIELDS = Object.keys(GROUP_BY_LABELS) as GroupByField[]

export function groupByKey(field: GroupByField, e: FirewallEvent): string | undefined {
  switch (field) {
    case 'srcIp':
      return e.srcIp || undefined
    case 'dstIp':
      return e.dstIp || undefined
    case 'srcPort':
      return e.srcPort ? String(e.srcPort) : undefined
    case 'dstPort':
      return e.dstPort ? String(e.dstPort) : undefined
    case 'rule':
      return e.ruleLabel || undefined
    case 'protocol':
      return e.protocol ? e.protocol.toUpperCase() : undefined
    case 'action':
      return e.action
    case 'chain':
      return e.chain || undefined
    case 'interface':
      // In-interface wins when both are set, which is every forward-chain
      // event -- so a forward event never appears under its out-interface
      // here, while the interface *filter* matches either side.
      //
      // Not an inconsistency to fix (#267, Uncertain): a breakdown gives
      // each event exactly one bucket, and the alternative is counting
      // the same event twice so the totals stop summing to the event
      // count. Filtering answers a different question -- "show me
      // anything touching this interface" -- where matching either side
      // is right. Same preference as grouping by srcIp rather than by
      // "either address".
      return e.inInterface || e.outInterface || undefined
  }
}
