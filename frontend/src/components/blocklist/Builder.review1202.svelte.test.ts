// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U5 and U7 (note_34034), the blocklist page's password prompt
// for minting its ingest token. Written from the fix list alone, before the
// fix.
//
//  - U5: the prompt has a visible submit button and a label. After a failure
//    focus returns to the password field, and the error is announced: a live
//    region (role="status", role="alert" or aria-live) exists BEFORE the
//    message arrives, and the message is put into it.
//  - U7: a mint that answers with no token value shows an error, and opening
//    another router clears the last error.
//
// createToken is the real api.ts function answering from a stubbed fetch
// (POST /api/tokens), so the failures are real Problem Details responses and
// the tests do not depend on how the page learns of them.

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

async function typeAndEnter(field: HTMLElement, password = 'hunter2') {
  await fireEvent.input(field, { target: { value: password } })
  await fireEvent.keyDown(field, { key: 'Enter' })
}

describe('Builder: the password prompt is a form a person can use (#1202 review U5)', () => {
  // #1202 review U5
  it('has a labelled password field and a visible, enabled submit button', async () => {
    await openOn(builder())
    const field = await passwordField()

    expect(screen.getByLabelText(/password/i)).toBe(field)
    const button = submitButtonFor(field)
    expect(button.disabled).toBe(false)
    // Found by role, so it is not hidden from the accessibility tree either.
    expect(screen.getAllByRole('button').includes(button)).toBe(true)
  })

  // #1202 review U5: the button does what Enter does.
  it('pressing the submit button with the password typed mints the token with it', async () => {
    const fetchMock = stubMint(MINTED)
    await openOn(builder())
    const field = await passwordField()

    await fireEvent.input(field, { target: { value: 'hunter2' } })
    await fireEvent.click(submitButtonFor(field))

    await waitFor(() => expect(mintCalls(fetchMock)).toHaveLength(1))
    const [, init] = mintCalls(fetchMock)[0]
    expect(JSON.parse(init?.body as string)).toMatchObject({ kind: 'ingest', device: 'rb5009', password: 'hunter2' })
  })

  // #1202 review U5: pair -- nothing typed, nothing sent.
  it('pressing the submit button with no password typed mints nothing', async () => {
    const fetchMock = stubMint(MINTED)
    await openOn(builder())
    const field = await passwordField()

    await fireEvent.click(submitButtonFor(field))
    await new Promise((r) => setTimeout(r, 0))

    expect(mintCalls(fetchMock)).toHaveLength(0)
  })

  // #1202 review U5
  it('after a wrong password, focus is back in the password field', async () => {
    stubMint(WRONG_PASSWORD)
    await openOn(builder())
    const field = await passwordField()
    await fireEvent.input(field, { target: { value: 'wrong' } })
    // Before the failure focus is away from the field, so the test cannot pass
    // by the field merely having had focus all along.
    field.blur()
    expect(document.activeElement).not.toBe(field)

    await fireEvent.keyDown(field, { key: 'Enter' })

    await waitFor(() => expect(announced()).toMatch(/incorrect password/i))
    expect(document.activeElement).toBe(screen.getByLabelText(/password/i))
  })

  // #1202 review U5: the live region is in the page before the error is.
  it('an element with role="status", role="alert" or aria-live exists before the error arrives, and the error is put into it', async () => {
    stubMint(WRONG_PASSWORD)
    await openOn(builder())
    const field = await passwordField()
    const before = liveRegions()
    expect(before.length).toBeGreaterThan(0)
    expect(before.map((el) => el.textContent).join('')).not.toMatch(/incorrect password/i)

    await typeAndEnter(field, 'wrong')

    await waitFor(() => expect(announced()).toMatch(/incorrect password/i))
    // The same node that stood there before, not one created with the message.
    const receivers = before.filter((el) => /incorrect password/i.test(el.textContent ?? ''))
    expect(receivers.length).toBeGreaterThan(0)
    expect(receivers.every((el) => el.isConnected)).toBe(true)
  })

  // #1202 review U5: pair -- a good password announces no error and does not pull focus back.
  it('a good password announces no error', async () => {
    stubMint(MINTED)
    await openOn(builder())
    const field = await passwordField()

    await typeAndEnter(field)

    await waitFor(() =>
      expect(vi.mocked(fetchBlocklistCommands).mock.lastCall?.[0]).toMatchObject({ device: 'rb5009', token: 'tok-1' }),
    )
    expect(announced()).not.toMatch(/incorrect password|couldn|failed|error/i)
  })
})

describe('Builder: a mint with no token value, and a stale error (#1202 review U7)', () => {
  // #1202 review U7
  it('a mint that returns no token value shows an error, and builds nothing with a token', async () => {
    stubMint(MINTED_WITHOUT_VALUE)
    await openOn(builder())
    const field = await passwordField()
    const baseline = announced()

    await typeAndEnter(field)

    await waitFor(() => expect(announced()).not.toBe(baseline))
    expect(
      vi.mocked(fetchBlocklistCommands).mock.calls.every(([req]) => !(req as { token?: string }).token),
    ).toBe(true)
  })

  // #1202 review U7: pair -- a mint that does return its value shows no error.
  it('a mint that returns its token value shows no error', async () => {
    stubMint(MINTED)
    await openOn(builder())
    const field = await passwordField()
    const baseline = announced()

    await typeAndEnter(field)

    await waitFor(() =>
      expect(vi.mocked(fetchBlocklistCommands).mock.lastCall?.[0]).toMatchObject({ token: 'tok-1' }),
    )
    expect(announced()).toBe(baseline)
  })

  // #1202 review U7
  it('opening another router clears the last error', async () => {
    stubMint(WRONG_PASSWORD)
    const both = [
      { id: 'rb5009', name: 'rb5009' },
      { id: 'hap-ax3', name: 'hap-ax3' },
    ]
    await openOn(builder({ devices: both }))
    const field = await passwordField()
    await typeAndEnter(field, 'wrong')

    // The error is there to be cleared.
    await waitFor(() => expect(document.body.textContent).toMatch(/incorrect password/i))

    vi.mocked(fetchBlocklistBuilder).mockResolvedValue(
      builder({ device: 'hap-ax3', deviceName: 'hap-ax3', devices: both }),
    )
    await blocklistState.openFor('hap-ax3')

    await waitFor(() => expect(document.body.textContent).not.toMatch(/incorrect password/i))
    expect(announced()).not.toMatch(/incorrect password/i)
  })

  // #1202 review U7: pair -- the error does not clear itself while the same router stays open.
  it('the error stays while the same router stays open', async () => {
    stubMint(WRONG_PASSWORD)
    await openOn(builder())
    const field = await passwordField()
    await typeAndEnter(field, 'wrong')
    await waitFor(() => expect(document.body.textContent).toMatch(/incorrect password/i))

    await new Promise((r) => setTimeout(r, 20))

    expect(document.body.textContent).toMatch(/incorrect password/i)
  })
})
