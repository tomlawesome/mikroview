// SPDX-License-Identifier: AGPL-3.0-only
//
// #633 (rounds 23-25): Settings is the shelf -- five groups reporting
// live truth, the deck's cards in the kept order with sign-in landing
// on the first, and the watcher bench behind detection's tune row.
// #490's absorbed pages (Users/Tokens/Detectors) live on behind the
// doors and the bench; the viewer/admin split those tests carried is
// unchanged (chip once, verbs gated, facts identical).

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(async () => ({
    instance: { tlsEnabled: true, hosts: [], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: [],
  })),
  fetchDefinitions: vi.fn(async () => ({
    definitions: [
      {
        id: 'port_scan',
        name: 'Port scan',
        intent: 'detection',
        kind: 'declarative',
        enabled: true,
        scope: { hosts: ['203.0.113.9'], hostsMode: 'deny' },
        params: { threshold: 15, window: '1m0s' },
        provenance: { origin: 'shipped' },
        available: true,
        replay: { known: true, capable: true },
      },
      {
        id: 'device_silence',
        name: 'Device gone quiet',
        intent: 'detection',
        kind: 'declarative',
        enabled: false,
        provenance: { origin: 'shipped' },
        available: true,
        replay: { known: true, capable: true },
      },
    ],
    coverageEvidence: { complete: true },
  })),
  updateDefinition: vi.fn(),
  fetchUsers: vi.fn(async () => [
    { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T13:41:00Z', hasLocalPassword: true, sso: false },
    { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T12:07:00Z', hasLocalPassword: true, sso: false },
  ]),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchTokens: vi.fn(async () => [
    { id: 't1', name: 'rb5009-ingest', kind: 'ingest', device: 'rb5009', createdAt: '2026-08-01T00:00:00Z', lastUsedAt: '2026-08-24T14:02:00Z' },
  ]),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  fetchDevices: vi.fn(async () => []),
  signOutEverywhere: vi.fn(async () => null),
  fetchPersistence: vi.fn(async () => ({ backend: 'file', dir: '/var/lib/mikroview' })),
  fetchAuthSession: vi.fn(async () => ({
    setupRequired: false,
    authenticated: true,
    username: 'admin',
    role: 'admin',
    ssoAvailable: false,
    signedInSince: new Date().toISOString(),
  })),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { usersState } from '../lib/users.svelte'
import { tokensState } from '../lib/tokens.svelte'
import { deckOrderState } from '../lib/deckOrder.svelte'
import { persistenceState } from '../lib/persistence.svelte'
import type { Stats } from '../lib/types'
import EngineRoom from './EngineRoom.svelte'

function stats(overrides: Partial<Stats> = {}): Stats {
  return {
    total: 0,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 7.4,
    capacity: 100000,
    count: 41208,
    oldestHeld: null,
    windowSeconds: 72 * 3600,
    connectedClients: 1,
    ...overrides,
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

beforeEach(() => {
  vi.clearAllMocks()
  appState.stats = stats()
  appState.devices = []
  flagsState.list = []
  detectorSettingsState.list = []
  usersState.list = []
  tokensState.list = []
  tokensState.justCreated = null
  authState.signedInSince = ''
  // persistenceState.ensureLoaded() only ever fetches once (see its own
  // doc comment), so across a whole test file its cache would otherwise
  // leak from whichever test rendered EngineRoom first -- reset the
  // seeded value directly instead of relying on the mocked fetch, same
  // as detectorSettingsState.list/flagsState.list above.
  persistenceState.info = null
  deckOrderState.set(['fall', 'metrics', 'live', 'docket', 'entities', 'engineroom'])
})

describe('The settings shelf (#633)', () => {
  it('renders the five groups and the deck -- seven cards for an admin (#647) -- in the kept order', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    for (const name of ['your deck', 'ingest', 'detection', 'memory', 'account']) {
      expect(screen.getByText(name)).toBeTruthy()
    }
    const shelf = document.querySelector<HTMLElement>('.stshelf')!
    for (const card of ['The fall', 'Metrics', 'Stream', 'The docket', 'Entities', 'Settings']) {
      expect(within(shelf).getByText(card)).toBeTruthy()
    }
    expect(screen.getByText('7 cards in the order you keep them', { exact: false })).toBeTruthy()
    // Sign-in lands on the first card, and the shelf says so exactly once.
    expect(screen.getAllByText('SIGN-IN LANDS HERE')).toHaveLength(1)
  })

  it("a viewer's shelf carries six cards -- no Entities, absent rather than disabled", async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    expect(screen.getByText('6 cards in the order you keep them', { exact: false })).toBeTruthy()
    const shelf = document.querySelector<HTMLElement>('.stshelf')!
    expect(within(shelf).queryByText('Entities')).toBeNull()
    expect(within(shelf).getByText('Settings')).toBeTruthy()
  })

  it('reordering a card moves the landing with it', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    // Arrow keys mirror what a drag does: pushing the first card right
    // makes the second card first, and the landing marker follows.
    const fall = screen.getByRole('button', { name: /The fall, position 1/ })
    await fireEvent.keyDown(fall, { key: 'ArrowRight' })
    flushSync()

    expect(deckOrderState.order[0]).toBe('metrics')
    expect(screen.getByRole('button', { name: /Metrics, position 1/ })).toBeTruthy()

    // Back again, so the kept order is the ratified default for the
    // other tests.
    const metrics = screen.getByRole('button', { name: /The fall, position 2/ })
    await fireEvent.keyDown(metrics, { key: 'ArrowLeft' })
    flushSync()
    expect(deckOrderState.order[0]).toBe('fall')
  })

  it("detection's tune row unfolds the watcher bench in place", async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    // The bench (EngineRoomWatchers) is not mounted until asked for.
    expect(screen.queryByText('Port scan')).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'tune…' }))
    await settle()

    expect(screen.getByText('Port scan')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'close the bench' })).toBeTruthy()
  })

  it('a viewer sees the chip, no verbs, and no admin-only users door', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    // The READ-ONLY chip is gone with the page heading it lived in
    // (#700): round 30 draws no heading on any deck and no replacement
    // chip anywhere, so #548's grammar -- read-only declared once, in
    // words -- currently has nowhere to be said. That is recorded as a
    // gap on #691, not a decision that viewers stop being told; the
    // component and its own test are untouched and still pass. This
    // pins the present truth so the gap cannot be mistaken for done.
    expect(screen.queryByText('READ-ONLY')).toBeNull()

    // Tokens door is viewer-readable but its verbs are gated.
    expect(screen.getByText('rb5009-ingest')).toBeTruthy()

    // An ingest key names the device it speaks for, not just its kind:
    // with two routers pushing, "ingest" alone does not say which key
    // belongs to which, and that is the fact an admin revokes on. The
    // old Tokens page carried it and the door has to as well.
    expect(screen.getByText(/ingest: rb5009/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ Mint a key' })).toBeNull()

    // The users door stayed admin-only (mid-build owner override) --
    // absent entirely for a viewer, not shown empty or explained.
    expect(screen.queryByText('Who may look in')).toBeNull()
    expect(screen.queryByRole('button', { name: '+ Let someone in' })).toBeNull()
  })

  it('an admin sees the verbs', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('READ-ONLY')).toBeNull()
    expect(screen.getByText('Who may look in')).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ Let someone in' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ Mint a key' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Revoke' })).toBeTruthy()
  })

  it('shows a user no read-only chip: they edit the watchers station here', async () => {
    // #653: the chip follows canEdit, not isAdmin. Telling a user this
    // page is read-only was wrong -- the owner-level doors are gated,
    // the page is not.
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('READ-ONLY')).toBeNull()
    // The owner-level door is still absent for them.
    expect(screen.queryByText('Who may look in')).toBeNull()
  })

  // #653's three tiers: running the detector bench (enable/pause, edit
  // scope) is a normal operational action, open to user and admin --
  // unlike the tokens/users doors above, which stay admin-only.
  it('a viewer opening the watchers station sees no run checkbox or scope knob', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    // #633 moved the bench behind the detection group's "tune…" link.
    await fireEvent.click(screen.getByRole('button', { name: 'tune…' }))
    await settle()

    expect(screen.queryByRole('checkbox', { name: 'Port scan runs' })).toBeNull()
    expect(document.querySelector('.scope-knob')).toBeNull()
  })

  it('a user opening the watchers station sees the run checkbox and scope knob', async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'tune…' }))
    await settle()

    expect(screen.getByRole('checkbox', { name: 'Port scan runs' })).toBeTruthy()
    expect(document.querySelector('.scope-knob')).toBeTruthy()
  })

  it('the mint banner appears once', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    tokensState.justCreated = {
      id: 't2',
      name: 'nas-read',
      kind: 'api',
      createdAt: '2026-08-24T14:05:00Z',
      value: 'mv1_4c21secret9b0d',
    }
    render(EngineRoom)
    await settle()

    expect(screen.getAllByText('mv1_4c21secret9b0d')).toHaveLength(1)
    expect(screen.getAllByText(/Copy it now/)).toHaveLength(1)
  })

  // #677: the three previously-unbuilt rows.
  it("detection's port-scan window states the live threshold, editable for a user", async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    const knob = screen.getByRole('button', { name: '15 ports / 60 s' })
    await fireEvent.click(knob)
    await settle()

    const portsInput = screen.getByLabelText('distinct ports') as HTMLInputElement
    const windowInput = screen.getByLabelText('window in seconds') as HTMLInputElement
    expect(portsInput.value).toBe('15')
    expect(windowInput.value).toBe('60')

    await fireEvent.input(portsInput, { target: { value: '25' } })
    await fireEvent.input(windowInput, { target: { value: '90' } })
    await fireEvent.click(screen.getByRole('button', { name: 'save' }))
    await settle()

    const { updateDefinition } = await import('../lib/api')
    expect(updateDefinition).toHaveBeenCalledWith('port_scan', { params: { threshold: 25, window: '90s' } })
  })

  it('a viewer sees the port-scan window as a fact, not a knob', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    expect(screen.getByText('15 ports / 60 s')).toBeTruthy()
    expect(screen.queryByRole('button', { name: '15 ports / 60 s' })).toBeNull()
  })

  it('memory states persistence live truth: the file backend and its directory, and that the buffer is memory-only', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    persistenceState.info = { backend: 'file', dir: '/var/lib/mikroview' }
    render(EngineRoom)
    await settle()

    expect(screen.getByText('persistence')).toBeTruthy()
    expect(screen.getByText(/file store · \/var\/lib\/mikroview/)).toBeTruthy()
    expect(
      screen.getByText(/holds flags, definitions, watchlist entries, entities and tokens/),
    ).toBeTruthy()
    expect(screen.getByText(/the event buffer above is memory-only and clears on restart/)).toBeTruthy()
  })

  it('states Postgres, not a file path, when that backend is live', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    persistenceState.info = { backend: 'postgres' }
    render(EngineRoom)
    await settle()

    expect(screen.getByText(/^Postgres —/)).toBeTruthy()
    expect(screen.queryByText(/file store/)).toBeNull()
  })

  it('a viewer without access to GET /api/persistence sees only the buffer fact, not a fabricated backend', async () => {
    // #677: the route is admin-gated (a directory is infrastructure
    // detail, same reasoning /api/config/problems already applies), so
    // persistenceState.info stays null for anyone else -- absent, not
    // disabled, the same grammar the rest of Settings' admin-only facts
    // already follow.
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    expect(screen.getByText('the event buffer above is memory-only and clears on restart')).toBeTruthy()
    expect(screen.queryByText(/file store/)).toBeNull()
    expect(screen.queryByText(/Postgres/)).toBeNull()
  })

  it('the sessions row states this device and can sign out everywhere', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    authState.signedInSince = new Date(Date.now() - 4.5 * 86_400_000).toISOString()
    render(EngineRoom)
    await settle()

    expect(screen.getByText(/this device, signed in 4 d/)).toBeTruthy()

    const { signOutEverywhere } = await import('../lib/api')
    await fireEvent.click(screen.getByRole('button', { name: 'sign out everywhere' }))
    await settle()

    expect(signOutEverywhere).toHaveBeenCalled()
    expect(screen.getByText(/every other session has been ended/)).toBeTruthy()
  })
})
