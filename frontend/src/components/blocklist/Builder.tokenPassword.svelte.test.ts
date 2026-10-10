// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 33): the blocklist page mints one ingest token for the router
// when it opens. Gauntlet's POST /api/tokens takes the calling admin's own
// password, so the page must ask for it before minting and send it:
// createToken(name, kind, device, password).
//
// Assumption the build may change (named in the commit message): the page
// shows a field labelled with the word "password" and Enter in it submits.

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

describe('Builder: the token mint asks for the admin password (#1202 spec 33)', () => {
  it('does not mint on opening, and asks for the password', async () => {
    await openOn(builder())
    expect(await screen.findByLabelText(/password/i)).toBeTruthy()
    expect(createToken).not.toHaveBeenCalled()
  })

  // Pair for the above: once the password is given, the mint happens once, with it.
  it('mints one ingest token with the typed password and sends it with the block request', async () => {
    await openOn(builder())
    const field = await screen.findByLabelText(/password/i)
    await fireEvent.input(field, { target: { value: 'hunter2' } })
    await fireEvent.keyDown(field, { key: 'Enter' })

    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken).toHaveBeenCalledWith('blocklist-rb5009', 'ingest', 'rb5009', 'hunter2')
    // The page builds a token-less preview on open (existing behaviour); the
    // request after the mint is the one that must carry the new token.
    await waitFor(() =>
      expect(vi.mocked(fetchBlocklistCommands).mock.lastCall?.[0]).toMatchObject({ device: 'rb5009', token: 'tok-1' }),
    )

    // Reloading the same router does not mint a second token.
    await blocklistState.load('rb5009', false)
    expect(createToken).toHaveBeenCalledTimes(1)
  })

  it('does nothing on Enter with no password typed', async () => {
    await openOn(builder())
    const field = await screen.findByLabelText(/password/i)
    await fireEvent.keyDown(field, { key: 'Enter' })
    expect(createToken).not.toHaveBeenCalled()
  })
})
