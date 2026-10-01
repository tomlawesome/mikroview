// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import type { BlocklistBuilder, BlocklistCatalogueEntry, BlocklistLedgerEntry, BlocklistPart } from './api'
import {
  blockHead,
  defaultChoices,
  flagsFact,
  hintsFor,
  hm,
  inkFor,
  ledgerView,
  marked,
  newPartsNote,
  partsSummary,
  railRows,
  ranges,
  requestLists,
  versionLine,
  waitingLists,
} from './blocklistBuild'

// The catalogue as GET /api/blocklist/builder serves it for rb5009 on
// 7.24.4 (internal/blcatalogue), trimmed to what these tests read.
function entry(key: string, over: Partial<BlocklistCatalogueEntry> = {}): BlocklistCatalogueEntry {
  return {
    key,
    name: key,
    short: key,
    url: `https://example.invalid/${key}.txt`,
    terms: 'terms',
    default: false,
    defaultDirection: 'from',
    ipv6: false,
    refresh: [{ value: 'daily' }],
    refreshDefault: 'daily',
    facts: `**1** hosts · ${key}`,
    guide: `A **bold** guide for ${key}.`,
    flaggedByMikroView: false,
    startTime: '04:17',
    ...over,
  }
}

function row(key: string, over: Partial<BlocklistLedgerEntry> = {}): BlocklistLedgerEntry {
  return { key, state: 'off', count: 0, count6: 0, firedToday: null, flags24h: null, undo: `undo ${key}`, ...over }
}

function builder(over: Partial<BlocklistBuilder> = {}): BlocklistBuilder {
  return {
    device: 'rb5009',
    deviceName: 'rb5009',
    devices: [{ id: 'rb5009', name: 'rb5009' }],
    routerosVersion: '7.24.4',
    reportedAt: '2026-10-01T14:32:00Z',
    reviewedVersion: '7.24.4',
    minimumVersion: '7.18',
    standing: 'ok',
    pushCurrent: true,
    catalogueDate: '2026-10-01',
    catalogue: [
      entry('spamhaus', { name: 'Spamhaus DROP', short: 'Spamhaus DROP', default: true, ipv6: true, flaggedByMikroView: true }),
      entry('et', { name: 'Emerging Threats compromised IPs', short: 'Emerging Threats', default: true, defaultDirection: 'both', refreshDefault: 'weekdays', refresh: [{ value: 'daily' }, { value: 'weekdays', since: '7.24' }, { value: 'weekly' }], flaggedByMikroView: true }),
      entry('cins', { name: 'CINS Army', short: 'CINS Army', refreshDefault: '6h' }),
      entry('dshield', { name: 'DShield top 20', short: 'DShield', caveat: 'not for business use', facts: '**20** /24 networks · not for business use' }),
    ],
    leftOut: [{ name: 'FireHOL level 1', why: 'would drop your LAN' }],
    lists: [
      row('spamhaus', { state: 'held', count: 1692, count6: 91, loadedAt: '2026-10-01 04:17:02', firedToday: 412, flags24h: 0 }),
      row('et'),
      row('cins'),
      row('dshield'),
    ],
    ownDroplist: { held: 3, total: 3, fetchedAt: '2026-10-01T14:35:00Z' },
    undoAll: 'undo all',
    disableAll: 'disable all',
    ...over,
  }
}

function part(ordinal: number, ink: string): BlocklistPart {
  return { ordinal, ink, title: ink, note: [{ text: 'n' }], shown: [], commands: '' }
}

// The drawn data story: Spamhaus held, ET in the block, CINS not now.
const drawnParts = [part(1, 'push'), part(2, 'spamhaus'), part(3, 'spamhaus'), part(4, 'spamhaus'), part(5, 'et'), part(6, 'et'), part(7, 'et'), part(8, '')]

describe('the cards', () => {
  it('opens on the catalogue defaults, Yes where the router already holds a list', () => {
    const b = builder({ lists: [row('spamhaus'), row('et'), row('cins', { state: 'held', count: 15000 }), row('dshield')] })
    const c = defaultChoices(b)
    expect(c.spamhaus.on).toBe(true)
    expect(c.et).toEqual({ on: true, direction: 'both', ipv6: false, log: true, refresh: 'weekdays' })
    expect(c.cins.on).toBe(true)
    expect(c.dshield.on).toBe(false)
  })

  it('sends only the lists switched on, in catalogue order, IPv6 only where the list has it', () => {
    const b = builder()
    const c = defaultChoices(b)
    c.dshield.on = true
    c.dshield.ipv6 = true
    expect(requestLists(b, c).map((l) => [l.key, l.ipv6])).toEqual([
      ['spamhaus', true],
      ['et', false],
      ['dshield', false],
    ])
  })

  it('bolds the marked phrase and keeps the rest as text', () => {
    expect(marked('Whole netblocks carry **no legitimate traffic** — and *to* them.')).toEqual([
      { text: 'Whole netblocks carry ' },
      { text: 'no legitimate traffic', b: true },
      { text: ' — and ' },
      { text: 'to', em: true },
      { text: ' them.' },
    ])
  })

  it('gives every list a hint under every choice it offers, weekdays only with scheduler days', () => {
    for (const key of ['spamhaus', 'et', 'cins', 'blde', 'greensnow', 'dshield', 'bindef']) {
      const h = hintsFor(key, true)
      expect(h.block && h.log && h.refresh).toBeTruthy()
    }
    expect(hintsFor('spamhaus', true).ipv6).toMatch(/DROPv6/)
    expect(hintsFor('et', true).refresh).toMatch(/\*\*weekdays\*\*/)
    expect(hintsFor('et', false).refresh).not.toMatch(/weekdays/)
    expect(hintsFor('dshield', true).block).toMatch(/whole \/24/)
  })

  it('wears each drawn list’s ink, and a neutral one for the lists round 2 did not draw', () => {
    expect(inkFor('spamhaus')).toBe('var(--ink-spamhaus)')
    expect(inkFor('et')).toBe('var(--ink-et)')
    expect(inkFor('cins')).toBe('var(--ink-cins)')
    expect(inkFor('push')).toBe('var(--ink-push)')
    expect(inkFor('greensnow')).toBe('var(--ink-list)')
  })
})

describe('the copy row', () => {
  it('counts lists and parts as drawn', () => {
    expect(partsSummary(2, 8)).toBe('Copy — 2 lists · 8 parts')
    expect(partsSummary(1, 5)).toBe('Copy — 1 list · 5 parts')
  })

  it('says which parts are new: the drawn "Parts 5–7 are new; re-pasting 1–4 changes nothing."', () => {
    expect(newPartsNote(builder(), drawnParts, 'rb5009')).toBe('Parts 5–7 are new; re-pasting 1–4 changes nothing.')
  })

  it('says every part is new on a router that holds nothing and pushes neither new kind', () => {
    const b = builder({ pushCurrent: false, lists: [row('spamhaus'), row('et'), row('cins'), row('dshield')] })
    expect(newPartsNote(b, drawnParts, 'rb5009')).toBe('All 8 parts are new on rb5009.')
  })

  it('writes ordinals as ranges', () => {
    expect(ranges([1, 5, 6, 7])).toBe('1, 5–7')
    expect(ranges([2])).toBe('2')
    expect(ranges([])).toBe('')
  })

  it('names the parts in the block head', () => {
    expect(blockHead(builder(), { parts: drawnParts, copyText: 'x' })).toBe('8 parts, in order — the push, Spamhaus DROP, Emerging Threats, run now')
  })
})

describe('the rail and the ledger', () => {
  it('reads held, in the block and not now from the push and the clicks', () => {
    const b = builder()
    const rows = railRows(b, defaultChoices(b))
    expect(rows.map((r) => [r.title, r.n, r.state, r.receipt])).toEqual([
      ['Your drop list', '✓', 'done', expect.stringMatching(/^3 addresses · fetched \d\d:\d\d · from Settings$/)],
      ['Spamhaus DROP', '✓', 'done', '1,692 held · refreshed 04:17 · fired 412 today'],
      ['Emerging Threats compromised', '2', 'chosen', 'in the block · not on the router yet'],
      ['CINS Army', '–', 'off', 'not now'],
      ['DShield top 20', '–', 'off', 'not now'],
    ])
    expect(rows[1].ink).toBe('var(--ink-spamhaus)')
  })

  it('dims every list after the upgrade on a router below the floor', () => {
    const b = builder({ standing: 'below-floor', routerosVersion: '7.12.1' })
    const rows = railRows(b, defaultChoices(b))
    expect(rows.slice(1).every((r) => r.receipt === 'after the upgrade' && r.state === 'off')).toBe(true)
  })

  it('writes the held row as drawn, with Undo, and the flags fact only where MikroView flags from the list', () => {
    const b = builder()
    const v = ledgerView(b, defaultChoices(b))
    expect(v[0]).toMatchObject({
      state: 'done',
      receipt: '1,692 held (+ 91 IPv6) · refreshed 04:17 · rule fired 412 times today · flags from this list: 0 in 24 h (dropped first)',
      undo: 'undo spamhaus',
    })
    expect(v[1]).toMatchObject({ state: 'wait', n: '2', receipt: 'in the block · not on the router yet', undo: '' })
    expect(v[2]).toMatchObject({ state: 'skip', receipt: 'not now', undo: '' })
    expect(flagsFact(b.catalogue[2], row('cins'))).toBe('MikroView does not flag from it')
    expect(flagsFact(b.catalogue[0], row('spamhaus', { flags24h: null }))).toBe('flags: not tracked per list')
  })

  it('names the lists the router has not loaded yet', () => {
    const b = builder()
    expect(waitingLists(b, defaultChoices(b))).toEqual(['mv-bl-et'])
  })

  it('reads the router’s own clock off creation-time, in either of its spellings', () => {
    expect(hm('2026-10-01 04:17:02')).toBe('04:17')
    expect(hm('oct/01/2026 04:17:02')).toBe('04:17')
    expect(hm('')).toBe('')
  })

  it('writes the version line for the pushed version', () => {
    const v = versionLine(builder())
    expect(v.version).toBe('RouterOS 7.24.4')
    expect(v.rest).toMatch(/^ — what rb5009 reported at \d\d:\d\d · the choices below are the ones this version offers · reviewed on 7\.24\.4$/)
  })
})
