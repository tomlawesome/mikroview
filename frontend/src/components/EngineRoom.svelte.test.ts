// SPDX-License-Identifier: AGPL-3.0-only
//
// #633 (rounds 23-25): Settings is the shelf -- groups reporting live
// truth, the deck's cards in the kept order with sign-in landing on the
// first, and the watcher bench behind detection's tune row. #490's
// absorbed pages (Users/Tokens/Detectors) live on behind keys/people and
// the bench; the viewer/admin split those tests carried is unchanged
// (chip once, verbs gated, facts identical).
//
// Round 32 (#767): keys (under ingest) and people (under account) are
// mounted directly in the card, in its own row grammar, replacing the
// retired EngineRoomDoors.svelte and its USERS_DOOR_ENABLED/
// TOKENS_DOOR_ENABLED flags outright -- no shim, per AGENTS.md's
// "removals are wholesale". Both groups are gated on isAdmin: GET
// /api/tokens and GET /api/auth/users are both admin-only server-side
// (#657), so a `user` or `viewer` session gets neither group at all, not
// a read-only rendering of one.

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
  fetchSetupCommands: vi.fn(async () => ({
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: { commands: '', note: '' },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
  })),
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

    for (const name of ['your deck', 'ingest', 'keys', 'detection', 'memory', 'account', 'people']) {
      expect(screen.getByText(name)).toBeTruthy()
    }
    const shelf = document.querySelector<HTMLElement>('.stshelf')!
    for (const card of ['The fall', 'Metrics', 'Stream', 'The docket', 'Entities', 'Settings']) {
      expect(within(shelf).getByText(card)).toBeTruthy()
    }
    // #735: the "seven cards, in the order you keep them" caption is
    // gone -- its purpose (the owner: "obvious") was redundant with the
    // cards' own drag handle and position aria-label. Seven cards for
    // an admin is now checked by counting them directly.
    expect(within(shelf).getAllByRole('button')).toHaveLength(7)
    // Sign-in lands on the first card, and the shelf says so exactly once.
    expect(screen.getAllByText('SIGN-IN LANDS HERE')).toHaveLength(1)
  })

  // #657: Entities carries `edit: true` (#653's widening to the user
  // tier), and this page is itself gated to the same tier -- so a
  // `user` who reaches Settings at all sees the same seven cards an
  // admin does. Named for the role it actually renders, unlike the
  // pre-#657 version of this test, which called that tier "viewer"
  // when only `user` and `admin` can ever reach this page.
  it("a user's shelf carries all seven cards, same as an admin's", async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    const shelf = document.querySelector<HTMLElement>('.stshelf')!
    expect(within(shelf).getAllByRole('button')).toHaveLength(7)
    expect(within(shelf).getByText('Entities')).toBeTruthy()
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

  it('a viewer sees the chip, no verbs, and neither keys nor people', async () => {
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

    // Both groups are gated on isAdmin (see EngineRoom.svelte's own doc
    // comment): GET /api/tokens and GET /api/auth/users 403 for a
    // viewer, so neither group renders at all -- absent, not a
    // read-only view of one.
    expect(screen.queryByText('keys')).toBeNull()
    expect(screen.queryByText('people')).toBeNull()
    expect(screen.queryByText('rb5009-ingest')).toBeNull()
    expect(screen.queryByText(/ingest · speaks for rb5009/)).toBeNull()
    expect(screen.queryByRole('button', { name: 'revoke' })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ mint a key' })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ let someone in' })).toBeNull()
  })

  it('an admin sees keys and people, populated from the state modules', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    authState.username = 'tom'
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('READ-ONLY')).toBeNull()
    expect(screen.getByText('keys')).toBeTruthy()
    expect(screen.getByText('people')).toBeTruthy()

    // The seeded ingest token, chipped with the device it speaks for.
    expect(screen.getByText('rb5009-ingest')).toBeTruthy()
    expect(screen.getByText('ingest · speaks for rb5009')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'revoke' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ mint a key' })).toBeTruthy()

    // The seeded accounts: tom is the caller ("this is you"), and the
    // admin row ends console-only rather than a remove verb.
    expect(screen.getByText('tom')).toBeTruthy()
    expect(screen.getByText(/this is you/)).toBeTruthy()
    expect(screen.getByText('console-only')).toBeTruthy()
    expect(screen.getByText('kai')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'remove' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ let someone in' })).toBeTruthy()
  })

  it('minting a key shows the once-only reveal, and done lets the form close', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    const { createToken } = await import('../lib/api')
    vi.mocked(createToken).mockResolvedValueOnce({
      id: 't9',
      name: 'nas-read',
      kind: 'api',
      createdAt: '2026-09-01T00:00:00Z',
      value: 'mv1_4c21secret9b0d',
    })

    await fireEvent.click(screen.getByRole('button', { name: '+ mint a key' }))
    await settle()
    await fireEvent.input(screen.getByLabelText('key name'), { target: { value: 'nas-read' } })
    await fireEvent.click(screen.getByRole('button', { name: 'mint it' }))
    await settle()

    expect(createToken).toHaveBeenCalledWith('nas-read', 'api', undefined)
    expect(screen.getByText('mv1_4c21secret9b0d')).toBeTruthy()
    expect(screen.getByText(/shown once — mikroview keeps only its fingerprint/)).toBeTruthy()
    // A read-only key gets no RouterOS lines.
    expect(screen.queryByText(/copy for RouterOS/)).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'done' }))
    await settle()

    expect(screen.queryByText('mv1_4c21secret9b0d')).toBeNull()
    expect(screen.getByRole('button', { name: '+ mint a key' })).toBeTruthy()
  })

  it('an ingest key reveal offers copy for RouterOS', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchDevices } = await import('../lib/api')
    vi.mocked(fetchDevices).mockResolvedValueOnce([
      {
        id: 'rb5009',
        name: 'rb5009',
        sourceIp: '203.0.113.5',
        configured: true,
        firstSeen: '2026-08-01T00:00:00Z',
        lastSeen: '2026-09-01T00:00:00Z',
        eventCount: 10,
        status: 'live',
        routerosVersion: '',
      },
    ])
    render(EngineRoom)
    await settle()

    const { createToken } = await import('../lib/api')
    vi.mocked(createToken).mockResolvedValueOnce({
      id: 't10',
      name: 'rb5009-b',
      kind: 'ingest',
      device: 'rb5009',
      createdAt: '2026-09-01T00:00:00Z',
      value: 'mv1_ingestsecret',
    })

    await fireEvent.click(screen.getByRole('button', { name: '+ mint a key' }))
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'ingest' }))
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'mint it' }))
    await settle()

    expect(createToken).toHaveBeenCalledWith('', 'ingest', 'rb5009')
    expect(screen.getByText('mv1_ingestsecret')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'copy for RouterOS' })).toBeTruthy()
  })

  it("revoke arms before it acts, and any other click disarms it", async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    const { revokeToken } = await import('../lib/api')
    const revoke = screen.getByRole('button', { name: 'revoke' })

    await fireEvent.click(revoke)
    await settle()
    expect(screen.getByRole('button', { name: 'confirm — it stops speaking now' })).toBeTruthy()
    expect(revokeToken).not.toHaveBeenCalled()

    // Clicking elsewhere disarms it rather than revoking.
    await fireEvent.click(document.body)
    await settle()
    expect(screen.getByRole('button', { name: 'revoke' })).toBeTruthy()
    expect(revokeToken).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'revoke' }))
    await fireEvent.click(screen.getByRole('button', { name: 'confirm — it stops speaking now' }))
    await settle()
    expect(revokeToken).toHaveBeenCalledWith('t1')
  })

  it('letting someone in calls the create endpoint with the picked role', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    const { createUser } = await import('../lib/api')
    await fireEvent.click(screen.getByRole('button', { name: '+ let someone in' }))
    await settle()
    await fireEvent.input(screen.getByLabelText('username'), { target: { value: 'mia' } })
    await fireEvent.input(screen.getByLabelText('password'), { target: { value: 'first-password' } })
    await fireEvent.click(screen.getByRole('button', { name: 'can only look' }))
    await fireEvent.click(screen.getByRole('button', { name: 'let them in' }))
    await settle()

    expect(createUser).toHaveBeenCalledWith('mia', 'first-password', 'viewer')
  })

  it("remove arms before it acts, same gesture as revoke", async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    const { deleteUser } = await import('../lib/api')
    const remove = screen.getByRole('button', { name: 'remove' })

    await fireEvent.click(remove)
    await settle()
    expect(screen.getByRole('button', { name: 'confirm — signs them out, revokes their keys' })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'confirm — signs them out, revokes their keys' }))
    await settle()
    expect(deleteUser).toHaveBeenCalledWith('u2')
  })

  it('only one verb is armed at a time: arming remove disarms an armed revoke', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'revoke' }))
    await settle()
    expect(screen.getByRole('button', { name: 'confirm — it stops speaking now' })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'remove' }))
    await settle()
    expect(screen.getByRole('button', { name: 'confirm — signs them out, revokes their keys' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'confirm — it stops speaking now' })).toBeNull()
    expect(screen.getByRole('button', { name: 'revoke' })).toBeTruthy()
  })

  it('shows a user no read-only chip, and neither keys nor people: they edit the watchers station here', async () => {
    // #653: the chip follows canEdit, not isAdmin. Telling a user this
    // page is read-only was wrong -- keys and people are gated, the
    // page is not.
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('READ-ONLY')).toBeNull()
    expect(screen.queryByText('keys')).toBeNull()
    expect(screen.queryByText('people')).toBeNull()
  })

  // #653's three tiers: running the detector bench (enable/pause, edit
  // scope) is a normal operational action, open to user and admin --
  // unlike the tokens/users doors above, which stay admin-only.
  it('a viewer opening the watchers station sees no run checkbox and no row expander', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    // #633 moved the bench behind the detection group's "tune…" link.
    await fireEvent.click(screen.getByRole('button', { name: 'tune…' }))
    await settle()

    expect(screen.queryByRole('checkbox', { name: 'Port scan runs' })).toBeNull()
    expect(document.querySelector('.row-knob')).toBeNull()
  })

  it('a user opening the watchers station sees the run checkbox and the row expander', async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'tune…' }))
    await settle()

    expect(screen.getByRole('checkbox', { name: 'Port scan runs' })).toBeTruthy()
    expect(document.querySelector('.row-knob')).toBeTruthy()
  })

  it('a reveal already in state (e.g. a remount mid-session) still renders, not just a freshly-minted one', async () => {
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

    expect(screen.getByText('mv1_4c21secret9b0d')).toBeTruthy()
    expect(screen.getByText(/shown once — mikroview keeps only its fingerprint/)).toBeTruthy()
    // The revealed token does not also render as an ordinary row.
    expect(screen.queryAllByText('nas-read')).toHaveLength(1)
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
