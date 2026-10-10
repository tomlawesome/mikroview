// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U14 (note_34148), the blocklist page's token mint is per
// router. Router A's mint is still running when router B is opened: B's prompt
// does not show "Minting…" and its button is not disabled, B can mint on its
// own, and A's later answer (error or token) never appears on, or is used for,
// B. Written before the fix.
//
// createToken is the real api.ts function answering from a stubbed fetch
// (POST /api/tokens), so A's mint can be held open and B's can be answered.

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
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
}))

import {
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


const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'

function problem(status: number, cls: string, detail: string): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function stubMint(make: () => Response | Promise<Response>) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/tokens') return make()
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function mintCalls(fetchMock: ReturnType<typeof stubMint>): [string, RequestInit | undefined][] {
  return (fetchMock.mock.calls as unknown as [string, RequestInit | undefined][]).filter(([url]) => url === '/api/tokens')
}

const WRONG_PASSWORD = () => problem(401, 'invalid-credentials', 'incorrect password')
const MINTED = () => json({ id: 't1', name: 'blocklist-rb5009', kind: 'ingest', device: 'rb5009', createdAt: '2026-10-10T00:00:00Z', value: 'tok-1' }, 201)
const MINTED_WITHOUT_VALUE = () => json({ id: 't1', name: 'blocklist-rb5009', kind: 'ingest', device: 'rb5009', createdAt: '2026-10-10T00:00:00Z' }, 201)

beforeEach(() => {
  cleanup()
  blocklistState.reset()
  vi.mocked(fetchBlocklistCommands).mockImplementation(async (req) => commandsFor(req.lists))
  vi.mocked(fetchSetupStatus).mockRejectedValue(new Error('not in this test'))
  vi.mocked(fetchDevices).mockResolvedValue([])
})

afterEach(() => {
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

function passwordField(): Promise<HTMLInputElement> {
  return screen.findByLabelText(/password/i) as Promise<HTMLInputElement>
}

// The prompt's submit button: a button within two levels of the password
// field (a <form> around both, or the prompt's own box) whose name says what
// it does with a password -- mint, unlock, continue, submit, confirm, and the
// like. Not just any button: the page has many unrelated ones near the field
// ("Copy", "Undo", the Yes / Not now toggles).
const SUBMITS = /\b(mint|get|make|create|unlock|continue|submit|confirm|go|ok|okay|allow|authori[sz]e|use|send|next|enter|add|set|key)\b/i

function submitButtonFor(field: HTMLElement): HTMLButtonElement {
  let el: HTMLElement | null = field.parentElement
  for (let depth = 0; el && depth < 2; depth++, el = el.parentElement) {
    const found = (Array.from(el.querySelectorAll('button')) as HTMLButtonElement[]).find((b) =>
      SUBMITS.test((b.getAttribute('aria-label') ?? b.textContent ?? '').trim()),
    )
    if (found) return found
  }
  throw new Error('no submit button beside the password field')
}

function liveRegions(): Element[] {
  return Array.from(document.querySelectorAll('[role="status"], [role="alert"], [aria-live]'))
}

function announced(): string {
  return liveRegions()
    .map((el) => el.textContent ?? '')
    .join(' ')
}

const BOTH = [
  { id: 'rb5009', name: 'rb5009' },
  { id: 'hap-ax3', name: 'hap-ax3' },
]

function mintedFor(device: string, value: string) {
  return json({ id: `t-${device}`, name: `blocklist-${device}`, kind: 'ingest', device, createdAt: '2026-10-10T00:00:00Z', value }, 201)
}

// POST /api/tokens answers by the device in the body. A's answer is held until
// released; B's (any other device) answers at once with `other`.
function stubMintByDevice(other: () => Response) {
  let release!: (res: Response) => void
  const held = new Promise<Response>((resolve) => {
    release = resolve
  })
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (url !== '/api/tokens') throw new Error(`unexpected fetch ${url}`)
    const device = (JSON.parse(init?.body as string) as { device: string }).device
    return device === 'rb5009' ? held : other()
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, release }
}

async function startMintOnA() {
  await openOn(builder({ devices: BOTH }))
  const field = await passwordField()
  await fireEvent.input(field, { target: { value: 'hunter2' } })
  await fireEvent.keyDown(field, { key: 'Enter' })
}

async function openB() {
  vi.mocked(fetchBlocklistBuilder).mockResolvedValue(builder({ device: 'hap-ax3', deviceName: 'hap-ax3', devices: BOTH }))
  await blocklistState.openFor('hap-ax3')
}

const MINTING = /minting/i

describe('Builder: a mint still running for router A does not hold router B (#1202 review U14)', () => {
  // #1202 review U14: pair -- on A itself, the pending mint is shown.
  it('while A\'s mint runs, A\'s own prompt shows "Minting…"', async () => {
    const { fetchMock } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))

    expect(document.body.textContent).toMatch(MINTING)
  })

  // #1202 review U14
  it('opening B while A mints: B\'s prompt does not say "Minting…" and B\'s button is enabled', async () => {
    const { fetchMock } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))

    await openB()

    const field = await passwordField()
    await waitFor(() => expect(document.body.textContent).not.toMatch(MINTING))
    expect(submitButtonFor(field).disabled).toBe(false)
    expect(submitButtonFor(field).textContent).not.toMatch(MINTING)
  })

  // #1202 review U14
  it('B can mint while A\'s is still running, and the mint is for B', async () => {
    const { fetchMock } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))
    await openB()

    const field = await passwordField()
    await fireEvent.input(field, { target: { value: 'hunter3' } })
    await fireEvent.click(submitButtonFor(field))

    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(2))
    const [, init] = mintCalls(fetchMock)[1]
    expect(JSON.parse(init?.body as string)).toMatchObject({ kind: 'ingest', device: 'hap-ax3', password: 'hunter3' })
    await waitFor(() =>
      expect(vi.mocked(fetchBlocklistCommands).mock.lastCall?.[0]).toMatchObject({ device: 'hap-ax3', token: 'tok-b' }),
    )
  })

  // #1202 review U14
  it('A\'s later error never appears on B\'s prompt', async () => {
    const { fetchMock, release } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))
    await openB()
    await passwordField()

    release(WRONG_PASSWORD())
    await new Promise((r) => setTimeout(r, 20))

    expect(document.body.textContent).not.toMatch(/incorrect password/i)
    expect(announced()).not.toMatch(/incorrect password/i)
    expect(submitButtonFor(await passwordField()).disabled).toBe(false)
  })

  // #1202 review U14: A's token is for A -- it is not used to build B's commands.
  it('A\'s later token is not used for B', async () => {
    const { fetchMock, release } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))
    await openB()
    await passwordField()

    release(mintedFor('rb5009', 'tok-a'))
    await new Promise((r) => setTimeout(r, 20))

    const forB = vi.mocked(fetchBlocklistCommands).mock.calls.filter(([req]) => (req as { device?: string }).device === 'hap-ax3')
    expect(forB.every(([req]) => (req as { token?: string }).token !== 'tok-a')).toBe(true)
    // And B is still asking for its own password.
    expect(await passwordField()).toBeTruthy()
  })

  // #1202 review U14: pair -- the error still lands on A when A is the router on screen.
  it('A\'s error still shows on A when A stays open', async () => {
    const { fetchMock, release } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))

    release(WRONG_PASSWORD())

    await waitFor(() => expect(announced()).toMatch(/incorrect password/i))
  })

  // #1202 review U14: returning to A after its mint has ended.
  it('going back to A after its mint ended: A is not left on "Minting…" and its button is enabled', async () => {
    const { fetchMock, release } = stubMintByDevice(() => mintedFor('hap-ax3', 'tok-b'))
    await startMintOnA()
    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))
    await openB()
    await passwordField()
    release(WRONG_PASSWORD())
    await new Promise((r) => setTimeout(r, 20))
    expect(document.body.textContent).not.toMatch(/incorrect password/i)

    vi.mocked(fetchBlocklistBuilder).mockResolvedValue(builder({ devices: BOTH }))
    await blocklistState.openFor('rb5009')

    await waitFor(() => expect(document.body.textContent).not.toMatch(MINTING))
    expect(submitButtonFor(await passwordField()).disabled).toBe(false)
  })
})
