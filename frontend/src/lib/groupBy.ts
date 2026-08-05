import type { FirewallEvent } from './types'

// Dimensions a custom top-talkers widget can rank by (see
// lib/topTalkers.svelte.ts). Deliberately excludes 'device' -- that's
// already its own fixed panel on the dashboard (Dashboard.svelte's
// "Event volume by device"), and mapping a device id to its display name
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
      return e.inInterface || e.outInterface || undefined
  }
}
