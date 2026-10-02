// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

// Only the network boundary is faked: the page's catalogue, the router's
// hold and every RouterOS line come from the two builder routes and the
// token mint.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchBlocklistBuilder: vi.fn(),
  fetchBlocklistCommands: vi.fn(),
  createToken: vi.fn(),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
}))

import {
  createToken,
  fetchBlocklistBuilder,
  fetchBlocklistCommands,
  fetchDevices,
  fetchSetupStatus,
  type BlocklistBuilder,
  type BlocklistCatalogueEntry,
  type BlocklistChoice,
  type BlocklistPart,
} from '../../lib/api'
import { blocklistState } from '../../lib/blocklist.svelte'
import Builder from './Builder.svelte'

function entry(key: string, name: string, over: Partial<BlocklistCatalogueEntry> = {}): BlocklistCatalogueEntry {
  return {
    key,
    name,
    short: name,
    url: `https://example.invalid/${key}`,
    terms: 'terms',
    default: false,
    defaultDirection: 'from',
    ipv6: false,
    refresh: [{ value: 'daily' }, { value: 'weekly' }],
    refreshDefault: 'daily',
    facts: `**1** hosts · ${key}`,
    guide: `About ${name}.`,
    flaggedByMikroView: false,
    startTime: '04:17',
    ...over,
  }
}

const ET_REFRESH_724 = [{ value: 'daily' as const }, { value: 'weekdays' as const, since: '7.24' }, { value: 'weekly' as const }]

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
      entry('spamhaus', 'Spamhaus DROP', { default: true, ipv6: true, flaggedByMikroView: true, facts: '**1,692** ranges · credited', guide: 'Spamhaus says **no legitimate traffic**.' }),
      entry('et', 'Emerging Threats compromised IPs', { default: true, defaultDirection: 'both', refresh: ET_REFRESH_724, refreshDefault: 'weekdays', flaggedByMikroView: true }),
      entry('cins', 'CINS Army'),
      entry('blde', 'blocklist.de strongips', { caveat: 'no licence stated', facts: '**385** hosts · no licence stated' }),
      entry('greensnow', 'GreenSnow', { caveat: 'no licence stated', facts: '**4,935** hosts · no licence stated' }),
      entry('dshield', 'DShield top 20', { caveat: 'not for business use', facts: '**20** /24 networks · not for business use' }),
      entry('bindef', 'Binary Defense banlist', { caveat: 'not for business use', facts: '**1,514** hosts · not for business use' }),
    ],
    leftOut: [{ name: 'FireHOL level 1', why: 'would drop your LAN' }],
    lists: [
      { key: 'spamhaus', state: 'held', count: 1692, count6: 91, loadedAt: '2026-10-01 04:17:02', firedToday: 412, flags24h: 0, undo: '/ip firewall raw remove [find comment="mikroview blocklist: spamhaus (from)"]' },
      ...['et', 'cins', 'blde', 'greensnow', 'dshield', 'bindef'].map((key) => ({ key, state: 'off' as const, count: 0, count6: 0, firedToday: null, flags24h: null, undo: `undo ${key}` })),
    ],
    ownDroplist: { held: 3, total: 3 },
    undoAll: '/ip firewall raw remove [find comment~"^mikroview blocklist: "]',
    disableAll: '/ip firewall raw disable [find comment~"^mikroview blocklist"]',
    ...over,
  }
}

// commandsFor answers the commands route the way the server numbers its
// parts: the push, three per list, then run now.
function commandsFor(lists: BlocklistChoice[]) {
  const parts: BlocklistPart[] = [{ ordinal: 1, ink: 'push', title: 'The push', note: [{ text: 'updated once for every list' }], shown: [[{ text: '/system script …' }]], commands: 'PUSH' }]
  for (const l of lists) {
    for (const what of ['the fetch-and-load script', 'the schedule: daily, 04:17', 'the drop rule']) {
      parts.push({ ordinal: parts.length + 1, ink: l.key, title: l.key, note: [{ text: what }], shown: [[{ text: `${l.key} ${what}` }]], commands: `${l.key}:${what}` })
    }
  }
  if (lists.length) parts.push({ ordinal: parts.length + 1, ink: '', title: '', note: [{ text: 'Run them now' }], shown: [[{ text: 'run' }]], commands: 'RUN' })
  return { parts, copyText: parts.map((p) => p.commands).join('\n') }
}

beforeEach(() => {
  blocklistState.reset()
  vi.mocked(createToken).mockResolvedValue({ id: 't1', name: 'blocklist-rb5009', kind: 'ingest', value: 'tok-1' } as never)
  vi.mocked(fetchBlocklistCommands).mockImplementation(async (req) => commandsFor(req.lists))
  vi.mocked(fetchSetupStatus).mockRejectedValue(new Error('not in this test'))
  vi.mocked(fetchDevices).mockResolvedValue([])
})

afterEach(() => {
  vi.clearAllMocks()
})

async function openOn(b: BlocklistBuilder) {
  vi.mocked(fetchBlocklistBuilder).mockResolvedValue(b)
  render(Builder)
  await blocklistState.openFor('rb5009')
}

function card(name: string): HTMLElement {
  return screen.getByRole('region', { name })
}

describe('Builder', () => {
  it('opens with the two default lists on, and the four labelled lists carrying their caveat as a fact', async () => {
    await openOn(builder())
    expect(card('Spamhaus DROP').classList.contains('on')).toBe(true)
    expect(card('Emerging Threats compromised IPs').classList.contains('on')).toBe(true)
    expect(card('CINS Army').classList.contains('off')).toBe(true)
    for (const [name, caveat] of [
      ['blocklist.de strongips', 'no licence stated'],
      ['GreenSnow', 'no licence stated'],
      ['DShield top 20', 'not for business use'],
      ['Binary Defense banlist', 'not for business use'],
    ]) {
      expect(card(name).querySelector('.factsrow')?.textContent).toContain(caveat)
    }
    // The drawn bold survives the catalogue's markers.
    expect(card('Spamhaus DROP').querySelector('.guide b')?.textContent).toBe('no legitimate traffic')
    expect(card('Spamhaus DROP').querySelector('.factsrow b')?.textContent).toBe('1,692')
  })

  it('mints one ingest token for this router and sends it with the block request', async () => {
    await openOn(builder())
    expect(createToken).toHaveBeenCalledTimes(1)
    expect(createToken).toHaveBeenCalledWith('blocklist-rb5009', 'ingest', 'rb5009')
    expect(vi.mocked(fetchBlocklistCommands).mock.calls[0][0]).toMatchObject({ device: 'rb5009', token: 'tok-1' })
    await blocklistState.load('rb5009', false)
    expect(createToken).toHaveBeenCalledTimes(1)
  })

  it('drops a list’s parts when it is set to Not now, and renumbers the rest', async () => {
    await openOn(builder())
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Copy — 2 lists · 8 parts' }).length).toBe(2))
    await fireEvent.click(card('Emerging Threats compromised IPs').querySelector('.top .seg button:last-child') as HTMLElement)
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Copy — 1 list · 5 parts' }).length).toBe(2))
    const pre = screen.getByLabelText('RouterOS block to paste')
    expect(pre.textContent).not.toContain('et the fetch-and-load script')
    expect(pre.textContent).toContain('# 5 · Run them now')
  })

  it('offers weekdays, tagged 7.24, only where the router has scheduler days', async () => {
    await openOn(builder())
    const etRefresh = () => [...card('Emerging Threats compromised IPs').querySelectorAll('.choices .ch')].find((c) => c.textContent?.startsWith('refresh')) as HTMLElement
    expect(etRefresh().textContent).toContain('weekdays7.24')
    blocklistState.reset()
    cleanup()
    const old = builder({ routerosVersion: '7.19.4' })
    old.catalogue[1] = { ...old.catalogue[1], refresh: [{ value: 'daily' }, { value: 'weekly' }], refreshDefault: 'daily' }
    await openOn(old)
    expect(etRefresh().textContent).not.toContain('weekdays')
    expect(etRefresh().textContent).not.toContain('7.24')
    expect(document.querySelector('.vline b')?.textContent).toBe('RouterOS 7.19.4')
  })

  it('shows no card below the floor, only the upgrade the router needs', async () => {
    await openOn(builder({ standing: 'below-floor', routerosVersion: '7.12.1', upgrade: '/system package update check-for-updates\n/system package update install' }))
    expect(screen.queryAllByRole('region').length).toBe(0)
    expect(screen.getByText(/rb5009 runs RouterOS 7\.12\.1\./)).toBeTruthy()
    expect(document.querySelector('.warnbox pre')?.textContent).toBe('/system package update check-for-updates\n/system package update install')
    expect(screen.getByText('RouterOS below 7.18 — unsupported')).toBeTruthy()
    expect(fetchBlocklistCommands).not.toHaveBeenCalled()
  })

  it('says a router that has not pushed is waiting for its push', async () => {
    await openOn(builder({ standing: 'no-push', routerosVersion: '', reportedAt: undefined }))
    expect(screen.queryAllByRole('region').length).toBe(0)
    const paras = [...document.querySelectorAll('.warnbox p')].map((p) => p.textContent)
    expect(paras).toEqual([
      'rb5009 has not pushed yet. This page writes its block for the RouterOS version the push reports, and nothing has arrived from this router.',
      'Run setup first (Admin ▸ Run setup…): its one paste installs the push.',
      'The first push tells this page the version, and the lists appear here.',
    ])
    expect(document.querySelector('.warnbox pre')).toBeNull()
    expect(screen.getByText('no push yet — version unknown').classList.contains('warn')).toBe(true)
    expect(document.querySelector('.att.push')).toBeNull()
    const receipts = [...document.querySelectorAll('.rail .step-row .step-receipt')].slice(1).map((r) => r.textContent)
    expect(receipts.every((r) => r === 'after the first push')).toBe(true)
    expect(document.querySelector('.foot .fhint')?.textContent).toBe('MikroView never connects to the router — it writes for the version the router’s push reports')
    expect(screen.getByText('Not yet on this router.')).toBeTruthy()
  })

  it('reads held, in the block and not now from the router’s own push', async () => {
    await openOn(builder())
    const rows = [...document.querySelectorAll('.ledger .row')]
    expect(rows[0].textContent).toContain('1,692 held (+ 91 IPv6) · refreshed 04:17 · rule fired 412 times today · flags from this list: 0 in 24 h (dropped first)')
    expect(rows[1].textContent).toContain('in the block · not on the router yet')
    expect(rows[2].querySelector('s')?.textContent).toBe('CINS Army')
    expect(rows[2].textContent).toContain('not now')
  })

  it('reveals a list’s undo lines and hides them again', async () => {
    await openOn(builder())
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(screen.getByText('/ip firewall raw remove [find comment="mikroview blocklist: spamhaus (from)"]')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Hide' }))
    expect(screen.queryByText('/ip firewall raw remove [find comment="mikroview blocklist: spamhaus (from)"]')).toBeNull()
  })

  it('keeps Undo everything in the foot', async () => {
    await openOn(builder())
    const foot = document.querySelector('.foot') as HTMLElement
    const undoAll = [...foot.querySelectorAll('button')].find((b) => b.textContent === 'Undo everything on rb5009') as HTMLElement
    expect(undoAll).toBeTruthy()
    await fireEvent.click(undoAll)
    expect(screen.getByLabelText('Undo everything').textContent).toBe('/ip firewall raw remove [find comment~"^mikroview blocklist: "]')
  })

  it('dresses each row and card in its list’s ink through the custom property', async () => {
    await openOn(builder())
    expect(card('Spamhaus DROP').style.getPropertyValue('--ink')).toBe('var(--ink-spamhaus)')
    const rail = [...document.querySelectorAll('.rail .step-row')] as HTMLElement[]
    expect(rail[1].style.getPropertyValue('--ink')).toBe('var(--ink-spamhaus)')
    expect(rail[2].style.getPropertyValue('--ink')).toBe('var(--ink-et)')
    const ledger = [...document.querySelectorAll('.ledger .row')] as HTMLElement[]
    expect(ledger[1].style.getPropertyValue('--ink')).toBe('var(--ink-et)')
  })

  it('closes a row’s undo when Undo everything opens, and Undo everything when a row’s opens', async () => {
    await openOn(builder())
    const foot = document.querySelector('.foot') as HTMLElement
    const undoAll = () => [...foot.querySelectorAll('button')].find((b) => /^(Undo everything|Hide the undo lines)/.test(b.textContent ?? '')) as HTMLElement
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await fireEvent.click(undoAll())
    expect(blocklistState.undoOpen).toBe('')
    const wrap = screen.getByLabelText('Undo everything').closest('.undo') as HTMLElement
    expect(wrap).toBeTruthy()
    expect(wrap.querySelector('.note')?.textContent).toBe(
      'Paste on the router. Every list goes — rules, scripts, schedulers and the lists themselves; the push script stays, so MikroView sees them leave at the next push and every row goes back to not now.',
    )
    expect(undoAll().textContent).toBe('Hide the undo lines')
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(blocklistState.showUndoAll).toBe(false)
    expect(screen.queryByLabelText('Undo everything')).toBeNull()
  })

  it('wears one shared ink on the four labelled lists', async () => {
    await openOn(builder())
    for (const name of ['blocklist.de strongips', 'GreenSnow', 'DShield top 20', 'Binary Defense banlist']) {
      expect(card(name).style.getPropertyValue('--ink')).toBe('var(--ink-labelled)')
    }
  })

  it('says nothing is waiting when the router holds every list in the block', async () => {
    const b = builder()
    b.lists[1] = { ...b.lists[1], state: 'held', count: 633, loadedAt: '2026-10-01 04:31:00', firedToday: 3, flags24h: 0 }
    await openOn(b)
    const obs = document.querySelector('.obs') as HTMLElement
    expect(obs.classList.contains('quiet')).toBe(true)
    expect(obs.textContent).toBe('Nothing waiting. rb5009 holds every list in the block; a paste now only applies a changed choice — it sets, never adds.')
  })

  it('offers nothing to copy with no list on', async () => {
    const b = builder({ lists: builder().lists.map((l) => ({ ...l, state: 'off' as const })) })
    await openOn(b)
    for (const name of ['Spamhaus DROP', 'Emerging Threats compromised IPs']) {
      await fireEvent.click(card(name).querySelector('.top .seg button:last-child') as HTMLElement)
    }
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Copy — no lists chosen' }).length).toBe(2))
    for (const btn of screen.getAllByRole('button', { name: 'Copy — no lists chosen' })) expect((btn as HTMLButtonElement).disabled).toBe(true)
    expect(document.querySelector('.copyrow .note')?.textContent).toBe('Turn a list on above; the block is empty.')
    const pre = screen.getByLabelText('RouterOS block to paste')
    expect(pre.querySelectorAll('.sec').length).toBe(0)
    expect(pre.querySelector('.fold')?.textContent).toBe('# nothing to paste — every list is Not now')
    expect(document.querySelector('.obs')).toBeNull()
    expect(document.querySelector('.blockhead .n')?.textContent).toBe('0 parts')
  })

  it('reads the own drop list from its fetch: set up and empty, or never fetched', async () => {
    await openOn(builder({ ownDroplist: { held: 0, total: 0, fetchedAt: '2026-10-01T14:35:00Z' } }))
    let own = document.querySelector('.rail .step-row') as HTMLElement
    expect(own.classList.contains('done')).toBe(true)
    expect(own.querySelector('.step-n')?.textContent).toBe('✓')
    expect(own.querySelector('.step-receipt')?.textContent).toMatch(/^0 addresses · fetched \d\d:\d\d · from Settings$/)
    blocklistState.reset()
    cleanup()
    await openOn(builder({ ownDroplist: { held: 0, total: 3 } }))
    own = document.querySelector('.rail .step-row') as HTMLElement
    expect(own.classList.contains('off')).toBe(true)
    expect(own.querySelector('.step-n')?.textContent).toBe('–')
    expect(own.querySelector('.step-receipt')?.textContent).toBe('not on this router · Settings ▸ drop list')
  })

  it('shows the live pill at zero lines, as the wizard’s bar does', async () => {
    await openOn(builder())
    const right = document.querySelector('.bar .right') as HTMLElement
    expect(right.textContent).toContain('live · 0/s')
    expect(right.textContent).toContain('0 lines')
  })

  it('counts the lists left out in words', async () => {
    await openOn(builder({ leftOut: Array.from({ length: 10 }, (_, i) => ({ name: `list ${i}`, why: 'why' })) }))
    expect(document.querySelector('details.more summary')?.textContent).toContain('Considered and left out — ten lists')
  })

  it('goes back to the drop list from the foot', async () => {
    await openOn(builder())
    await fireEvent.click(screen.getByRole('button', { name: 'Back to the drop list' }))
    expect(blocklistState.open).toBe(false)
  })
})
