// SPDX-License-Identifier: AGPL-3.0-only

import type { FirewallEvent } from './types'

// Column order for the exported file. Deliberately not the same list as
// the live table's columns -- this includes every field worth having in
// an export (NAT detail, raw line) regardless of which columns happen to
// be visible/sized on screen.
// Exported so the neutralising test can assert over every column,
// including ones added after that test was written -- see
// export.test.ts. A new column is exactly how this defect comes back.
export const COLUMNS: (keyof FirewallEvent)[] = [
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
  'rawTruncated',
]

// Characters that make a spreadsheet treat a cell as a formula rather
// than as text. Tab and CR are included because Excel strips leading
// whitespace before deciding, so "\t=1+1" is still a formula.
const FORMULA_LEADERS = ['=', '+', '-', '@', '\t', '\r']

// neutraliseFormula defuses CSV/DDE formula injection (CWE-1236).
//
// Almost every field here originates outside mikroview -- `raw` is the
// syslog line verbatim, `ruleLabel` comes from the router's own
// configuration, and the hostname columns come from naming/DNS. Anyone
// who can influence one of those can put `=cmd|'/C calc'!A0` or
// `=IMPORTXML(...)` in a cell, and the spreadsheet the operator opens
// the export in will execute it -- running a command, or quietly
// exfiltrating the surrounding rows to a URL of their choosing.
//
// CSV quoting does NOT prevent this. The spreadsheet unquotes first and
// decides afterwards, so `"=1+1"` is still a formula. The neutralising
// step has to be a prefix that survives unquoting: a leading apostrophe,
// which Excel, LibreOffice and Sheets all read as "the rest of this cell
// is literal text" and don't display.
//
// This is the same defect as CVE-2025-62417 (Bagisto), CVE-2025-55745
// (UnoPim) and CVE-2026-39424 (MaxKB), all export features that quoted
// correctly and neutralised nothing.
function neutraliseFormula(s: string): string {
  return s.length > 0 && FORMULA_LEADERS.includes(s[0]) ? `'${s}` : s
}

// The \r in this character class is load-bearing and was missing.
//
// A bare carriage return survives all the way into a cell: the syslog
// listener strips \n, but internal/routeros's parser deliberately does
// not clamp or sanitise Raw (it is kept verbatim so an operator can
// compare a row against what the router actually sent), and \r is not a
// character anything else rejects on that path. Unquoted, a spreadsheet
// reading classic-Mac line endings treats it as a record terminator --
// so the text after it becomes the start of a *new row*, and the start
// of a new row never passes through neutraliseFormula, which only ever
// inspects s[0] of the value it was given. A crafted Raw line therefore
// smuggles `=IMPORTXML(...)` into a cell that was never neutralised.
//
// Quoting closes it: inside quotes a \r is data, not a terminator, and
// the smuggled text stays in the cell it belongs to. Note the
// /api/ingest push path is unaffected -- internal/ingest's decoder
// rejects control characters outright -- so this is specific to the
// syslog path, which is the unauthenticated one.
function csvEscape(value: unknown): string {
  const s = neutraliseFormula(value === undefined || value === null ? '' : String(value))
  return /["\n\r,]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
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

// downloadText (#435) is the same blob-a-link-click idiom above, pulled
// out generic: Tune logging's annotated export is plain text, not a
// table of events, and is the second caller of the pattern rather than
// a reason to bend eventsToCsv/downloadEventsCsv around a second shape.
export function downloadText(filename: string, text: string): void {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
