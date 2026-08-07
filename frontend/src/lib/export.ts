// SPDX-License-Identifier: AGPL-3.0-only

import type { FirewallEvent } from './types'

// Column order for the exported file. Deliberately not the same list as
// the live table's columns -- this includes every field worth having in
// an export (NAT detail, raw line) regardless of which columns happen to
// be visible/sized on screen.
const COLUMNS: (keyof FirewallEvent)[] = [
  'time',
  'deviceId',
  'sourceIp',
  'action',
  'ruleLabel',
  'ruleName',
  'chain',
  'inInterface',
  'outInterface',
  'connState',
  'protocol',
  'srcMac',
  'srcIp',
  'srcHostName',
  'srcPort',
  'srcPortName',
  'dstIp',
  'dstHostName',
  'dstPort',
  'dstPortName',
  'natIp',
  'natPort',
  'natRaw',
  'length',
  'flags',
  'raw',
]

function csvEscape(value: unknown): string {
  const s = value === undefined || value === null ? '' : String(value)
  return /["\n,]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

export function eventsToCsv(events: FirewallEvent[]): string {
  const header = COLUMNS.join(',')
  const rows = events.map((e) => COLUMNS.map((c) => csvEscape(e[c])).join(','))
  return [header, ...rows].join('\n')
}

// Downloads exactly the events passed in (the caller decides what
// "currently shown" means -- see Toolbar.svelte, which passes
// appState.filteredEvents) as a CSV file, entirely client-side.
export function downloadEventsCsv(events: FirewallEvent[]): void {
  const blob = new Blob([eventsToCsv(events)], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `mikroview-events-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
