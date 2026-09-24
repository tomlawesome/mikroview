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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent, within } from '@testing-library/svelte'
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
  resetUserPassword: vi.fn(),
  clearUserTOTP: vi.fn(),
  clearUserPasskeys: vi.fn(),
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
  fetchHistorySettings: vi.fn(async () => ({
    keyed: true,
    enabled: true,
    days: 30,
    maxBytes: 1024 * 1024 * 1024,
    held: { days: 27, oldest: '2026-08-07', newest: '2026-09-02', bytes: 812 * 1024 * 1024 },
    capped: false,
    bytesPerDay: 30 * 1024 * 1024,
  })),
  setHistorySettings: vi.fn(),
  fetchRouterBackups: vi.fn(async () => ({
    enabled: false,
    routers: [],
    totalGenerations: 0,
    totalRouters: 0,
    totalBytes: 0,
  })),
  routerBackupDownloadUrl: vi.fn((device: string, generation: string, kind: string) => `/api/router-backups/${device}/${generation}/${kind}`),
  fetchDroplist: vi.fn(async () => ({
    listName: 'mikroview-drops',
    entries: [],
    key: { present: false },
    ownRangesKnown: false,
    setup: { scheduler: '', rule: '', disableRule: '', emptyList: '' },
  })),
  fetchConfigUpgrade: vi.fn(async () => ({ version: 'v1.2.3', settings: [] })),
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
import { watchlistState } from '../lib/watchlist.svelte'
import { geoipState, GEOIP_DOCS_URL } from '../lib/geoip.svelte'
import { wizardState } from '../lib/wizard.svelte'
import {
  fetchHistorySettings as fetchHistorySettingsReal,
  fetchRouterBackups as fetchRouterBackupsReal,
  fetchDroplist as fetchDroplistReal,
  fetchConfigUpgrade as fetchConfigUpgradeReal,
} from '../lib/api'
import type { RouterBackupsResponse, Stats } from '../lib/types'
import EngineRoom from './EngineRoom.svelte'

const fetchHistorySettings = vi.mocked(fetchHistorySettingsReal)
const fetchRouterBackups = vi.mocked(fetchRouterBackupsReal)
const fetchDroplist = vi.mocked(fetchDroplistReal)
const fetchConfigUpgrade = vi.mocked(fetchConfigUpgradeReal)

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

// One router-backups answer naming a single router -- the whole of what
// the out-of-order poll test below needs to tell two answers apart.
function backupsWith(device: string): RouterBackupsResponse {
  return {
    enabled: true,
    keyUnreadable: false,
    routers: [
      { device, generations: [{ id: 'g1', backupArrivedAt: '2026-09-01T00:00:00Z', backupBytes: 1024 }], intervalKnown: false, missed: 0 },
    ],
    totalGenerations: 1,
    totalRouters: 1,
    totalBytes: 1024,
    lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
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
  watchlistState.entries = []
  watchlistState.coverage = {}
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
  // wizardState is a module-level singleton, not a fixture scoped to one
  // test, so the address the drop-list test below sets on it would
  // otherwise leak into whatever test renders EngineRoom next.
  wizardState.address = ''
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
    for (const card of ['The fall', 'Metrics', 'Stream', 'The docket', 'Entities', 'Settings', 'Log every rule']) {
      expect(within(shelf).getByText(card)).toBeTruthy()
    }
    // #735: the "seven cards, in the order you keep them" caption is
    // gone -- its purpose (the owner: "obvious") was redundant with the
    // cards' own drag handle and position aria-label. The count is
    // checked directly instead -- eight since #1134 put Log every rule
    // on the deck.
    expect(within(shelf).getAllByRole('button')).toHaveLength(8)
    // Sign-in lands on the first card, and the shelf says so exactly once.
    expect(screen.getAllByText('SIGN-IN LANDS HERE')).toHaveLength(1)
  })

  // #657: Entities carries `edit: true` (#653's widening to the user
  // tier), and this page is itself gated to the same tier -- so a
  // `user` who reaches Settings at all sees the same eight cards an
  // admin does. Named for the role it actually renders, unlike the
  // pre-#657 version of this test, which called that tier "viewer"
  // when only `user` and `admin` can ever reach this page.
  it("a user's shelf carries all eight cards, same as an admin's", async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    const shelf = document.querySelector<HTMLElement>('.stshelf')!
    expect(within(shelf).getAllByRole('button')).toHaveLength(8)
    expect(within(shelf).getByText('Entities')).toBeTruthy()
    expect(within(shelf).getByText('Settings')).toBeTruthy()
    expect(within(shelf).getByText('Log every rule')).toBeTruthy()
  })

  it('the docket card reads the same watcher count the scene bar does, and leaves the flag count to it (#1156)', async () => {
    // The operator had the bar's eye saying 5 while the card beside it
    // said 6, and "67" flags printed twice on one screen. The card used
    // entries.length -- every watch, including the ring-broken and the
    // switched-off -- where the bar reads heldCount.
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [
      { id: 'f1', type: 'port_scan', cleared: false, provisional: false },
      { id: 'f2', type: 'port_scan', cleared: false, provisional: false },
    ] as never
    watchlistState.entries = [
      { id: 'w1', enabled: true },
      { id: 'w2', enabled: true },
      { id: 'w3', enabled: true },
      { id: 'w4', enabled: true },
      // broken: enabled, but no pushed rule can produce a matching event
      { id: 'w5', enabled: true },
      // switched off: never counted by either marker
      { id: 'w6', enabled: false },
    ] as never
    watchlistState.coverage = { w5: 'no-logging' }
    render(EngineRoom)
    await settle()

    const docket = screen.getByRole('button', { name: /The docket, position/ })
    expect(within(docket).getByText('◉ 4')).toBeTruthy()
    expect(within(docket).getByText('○1')).toBeTruthy()
    expect(docket.textContent).not.toContain('⚑')
    // and the bar's own reading is the one the card now matches
    expect(watchlistState.heldCount).toBe(4)
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
    // #1171: every tier's row says what that account may do. kai is the
    // user tier, which used to be the only one with no pill at all.
    expect(screen.getByText('admin')).toBeTruthy()
    expect(screen.getByText('can change things')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'remove' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ let someone in' })).toBeTruthy()
  })

  // #1194: the wizard can leave two keys with the same name, both
  // "never spoke — yet", and each with its own revoke -- the mint time
  // is what tells the operator which of the two is the older one.
  it('says when each key was minted, so two of the same name can be told apart', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    appState.now = Date.parse('2026-08-24T14:10:00Z')
    const { fetchTokens } = await import('../lib/api')
    vi.mocked(fetchTokens).mockResolvedValueOnce([
      { id: 't8', name: 'setup-172.23.0.1', kind: 'ingest', device: 'rb5009', createdAt: '2026-08-24T11:10:00Z' },
      { id: 't9', name: 'setup-172.23.0.1', kind: 'ingest', device: 'rb5009', createdAt: '2026-08-24T14:05:00Z' },
    ])
    render(EngineRoom)
    await settle()
    expect(screen.getByText('minted 3h ago')).toBeTruthy()
    expect(screen.getByText('minted 5m ago')).toBeTruthy()
    // Both still say nothing has used them -- the mint time is the only
    // thing separating the two rows.
    expect(screen.getAllByText(/never spoke — yet/)).toHaveLength(2)
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
    expect(screen.getByText(/shown once — MikroView keeps only its fingerprint/)).toBeTruthy()
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

  // v0.6.0 audit Lows, R9: a refused POST /api/setup/commands here used
  // to leave "copy for RouterOS" doing nothing and saying nothing --
  // indistinguishable from the click never registering. keyError is the
  // same slot the panel already shows a refused mint or revoke through.
  it('says so when copying the RouterOS lines fails, the same way the keys panel already shows a refusal', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchDevices, fetchSetupCommands } = await import('../lib/api')
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

    vi.mocked(fetchSetupCommands).mockResolvedValueOnce('setup is locked while a restore is in progress')
    await fireEvent.click(screen.getByRole('button', { name: 'copy for RouterOS' }))
    await settle()

    expect(screen.getByText('setup is locked while a restore is in progress')).toBeTruthy()
    expect(screen.queryByText('copied for RouterOS')).toBeNull()
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

  // #1251. The verb kills somebody's password outright, so it arms
  // before it acts like remove and revoke beside it, and what it mints
  // is shown exactly once.
  it('reset password arms before it acts, then shows the code once', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { resetUserPassword } = await import('../lib/api')
    vi.mocked(resetUserPassword).mockResolvedValue({
      username: 'kai',
      code: 'ABCD-EFGH-JKLM-NPQR',
      expiresAt: '2026-09-19T00:00:00Z',
    })
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'reset password' }))
    await settle()
    expect(resetUserPassword).not.toHaveBeenCalled()

    await fireEvent.click(
      screen.getByRole('button', { name: 'confirm — their password stops working now' }),
    )
    await settle()

    expect(resetUserPassword).toHaveBeenCalledWith('u2')
    expect(screen.getByTestId('reset-code').textContent).toBe('ABCD-EFGH-JKLM-NPQR')

    // Closing it is the end of the code: nothing holds it afterwards.
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await settle()
    expect(screen.queryByTestId('reset-code')).toBeNull()
  })

  // An SSO account's password belongs to its provider and the server
  // refuses (409), so the verb is absent rather than offered and denied.
  it('offers no reset for an SSO account', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: false, sso: true },
    ])
    render(EngineRoom)
    await settle()

    expect(screen.queryByRole('button', { name: 'reset password' })).toBeNull()
    expect(screen.getByRole('button', { name: 'remove' })).toBeTruthy()
  })

  // #1249: the pill is shown only when true, the same convention the sso
  // pill just above it already uses -- an SSO account never carries this
  // one either way, since it is never offered a factor.
  it('shows a pill for a person with a factor, and none for a person without', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: false },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: true },
    ])
    render(EngineRoom)
    await settle()

    expect(screen.getByText('authenticator app')).toBeTruthy()
    // tom's row carries no pill -- only one person has a factor here.
    expect(screen.getAllByText('authenticator app')).toHaveLength(1)
  })

  // #1249's lost-phone path: arm-then-confirm like reset password and
  // remove beside it, and only offered when there is a factor to clear.
  it('clear authenticator app arms before it acts, and only appears when there is one to clear', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers, clearUserTOTP } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: false },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: true },
    ])
    vi.mocked(clearUserTOTP).mockResolvedValue(null)
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'clear authenticator app' }))
    await settle()
    expect(clearUserTOTP).not.toHaveBeenCalled()

    await fireEvent.click(
      screen.getByRole('button', { name: 'confirm — turns their authenticator app off' }),
    )
    await settle()
    expect(clearUserTOTP).toHaveBeenCalledWith('u2')
  })

  // usersState.clearFactor() refreshes the list on success -- if that
  // refresh (fetchUsers) itself throws, the button must not be left
  // reading "clearing…" for the rest of the session.
  it('clears the "clearing…" state even when the post-clear refresh fails', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers, clearUserTOTP } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValueOnce([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: false },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: true },
    ])
    vi.mocked(clearUserTOTP).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockRejectedValueOnce(new Error('network error'))
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'clear authenticator app' }))
    await settle()
    await fireEvent.click(
      screen.getByRole('button', { name: 'confirm — turns their authenticator app off' }),
    )
    await settle()

    expect(clearUserTOTP).toHaveBeenCalledWith('u2')
    expect(screen.queryByText('clearing…')).toBeNull()
    expect(screen.getByRole('button', { name: 'clear authenticator app' })).toBeTruthy()
    expect(screen.getByText(/could not refresh the list/i)).toBeTruthy()
  })

  // No admin row ever offers this: the console-only branch replaces
  // every per-row verb for role === 'admin', same as reset password and
  // remove beside it (there is only ever one admin).
  it('offers no clear-factor verb on the admin row, even when the admin has a factor', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers } = await import('../lib/api')
    // kai stays in the list (unrelated to what this test checks) so a
    // later test relying on the default two-person list is not starved
    // of a non-admin row by this mock's leftover mockResolvedValue --
    // vi.clearAllMocks() (this file's beforeEach) clears call history,
    // not a previously-set resolved value.
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: true },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, hasTOTP: false },
    ])
    render(EngineRoom)
    await settle()

    expect(screen.queryByRole('button', { name: 'clear authenticator app' })).toBeNull()
    expect(screen.getByText('console-only')).toBeTruthy()
  })

  // #1250's own pill and clear button, mirroring the authenticator app
  // pair immediately above -- same convention (shown only when nonzero),
  // its own admin verb rather than folded into clearFactor above (an
  // account can hold either factor, or both, and EngineRoom offers each
  // its own button).
  it('shows a passkeys pill with the count for a person who has any, none for a person without', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 0 },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 2 },
    ])
    render(EngineRoom)
    await settle()

    expect(screen.getByText('passkeys · 2')).toBeTruthy()
  })

  it('clear passkeys arms before it acts, and only appears when there is one to clear', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers, clearUserPasskeys } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 0 },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 1 },
    ])
    vi.mocked(clearUserPasskeys).mockResolvedValue(null)
    render(EngineRoom)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'clear passkeys' }))
    await settle()
    expect(clearUserPasskeys).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'confirm — removes their passkeys' }))
    await settle()
    expect(clearUserPasskeys).toHaveBeenCalledWith('u2')
  })

  it('offers no clear-passkeys verb on the admin row, even when the admin has passkeys', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    const { fetchUsers } = await import('../lib/api')
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 3 },
      { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false, passkeyCount: 0 },
    ])
    render(EngineRoom)
    await settle()

    expect(screen.queryByRole('button', { name: 'clear passkeys' })).toBeNull()
    expect(screen.getByText('console-only')).toBeTruthy()
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
    expect(screen.getByText(/shown once — MikroView keeps only its fingerprint/)).toBeTruthy()
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

  it('memory states what outlives a restart, and the disk group carries the state store (#921)', async () => {
    // Round 43: the buffer always clears; with history on, the days on
    // disk stay and a watcher's try reads them. The state store is the
    // disk group's `state` row now, not memory's.
    authState.state = 'authenticated'
    authState.role = 'admin'
    persistenceState.info = { backend: 'file', dir: '/var/lib/mikroview' }
    render(EngineRoom)
    await settle()
    await settle()

    expect(screen.getByText('on restart')).toBeTruthy()
    expect(screen.getByText('the buffer clears — the 27 days on disk stay; trying a watcher reads them')).toBeTruthy()
    expect(screen.queryByText('persistence')).toBeNull()
    expect(screen.queryByText(/memory-only/)).toBeNull()

    const disk = document.getElementById('diskg') as HTMLElement
    expect(within(disk).getByText('state')).toBeTruthy()
    expect(
      within(disk).getByText(
        'encrypted file store · /var/lib/mikroview — flags, definitions, watchlist, entities, tokens',
      ),
    ).toBeTruthy()
    // beside the key: the row after it
    const labels = [...disk.querySelectorAll('.orow > span:first-child')].map((el) => el.textContent)
    expect(labels).toEqual(['on disk', 'allowed', 'key', 'state'])
  })

  it('on restart reads per the disk state: off with a key, and no key (#921)', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchHistorySettings.mockResolvedValueOnce({
      keyed: true,
      enabled: false,
      days: 30,
      maxBytes: 1024 * 1024 * 1024,
      held: null,
      capped: false,
      bytesPerDay: 0,
    })
    render(EngineRoom)
    await settle()
    await settle()
    expect(screen.getByText('the buffer clears — nothing outlives it; days can be kept on disk below')).toBeTruthy()
    cleanup()

    fetchHistorySettings.mockResolvedValueOnce({
      keyed: false,
      enabled: false,
      days: 30,
      maxBytes: 1024 * 1024 * 1024,
      held: null,
      capped: false,
      bytesPerDay: 0,
    })
    render(EngineRoom)
    await settle()
    await settle()
    expect(screen.getByText('the buffer clears — nothing outlives it')).toBeTruthy()
  })

  it('when the history GET fails for a reason other than role, the disk group stays and asks again (#921)', async () => {
    // Round 42's gap 9, round 43's `dfail`: an older server or an error
    // leaves one row saying so, with a link that asks again -- not an
    // absent group and not a switch nothing stands behind.
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchHistorySettings.mockRejectedValueOnce(Object.assign(new Error('503'), { status: 503 }))
    render(EngineRoom)
    await settle()
    await settle()

    const disk = document.getElementById('diskg') as HTMLElement
    expect(disk).toBeTruthy()
    expect(disk.classList.contains('dfail')).toBe(true)
    expect(within(disk).getByText(/unknown — the server did not answer/)).toBeTruthy()
    expect(within(disk).queryByRole('slider')).toBeNull()
    // memory claims the least meanwhile
    expect(screen.getByText('the buffer clears')).toBeTruthy()

    await fireEvent.click(within(disk).getByRole('button', { name: 'ask again' }))
    await settle()
    await settle()
    expect(fetchHistorySettings).toHaveBeenCalledTimes(2)
    expect(document.getElementById('diskg')?.classList.contains('dfail')).toBe(false)
    expect(screen.getByRole('slider', { name: 'Days kept on disk' })).toBeTruthy()
  })

  it('router backups stacks its rows until there is a strip to draw beside them (#1153)', async () => {
    // The left column holds the generation strips; with none drawn the
    // rows used to sit alone in column two, starting 600px in with the
    // whole left half blank. No diagram means the group stacks, the
    // same answer `dnokey`/`dfail` already give the disk group.
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()
    await settle()

    // The mocked default: backups off, no router has pushed a pair.
    expect(document.getElementById('bakg')?.classList.contains('dnodiagram')).toBe(true)
    cleanup()

    fetchRouterBackups.mockResolvedValueOnce({
      enabled: true,
      keyUnreadable: false,
      port: ':2222',
      routers: [
        {
          device: 'rb5009',
          generations: [{ id: 'g1', backupArrivedAt: '2026-09-01T00:00:00Z', backupBytes: 1024 }],
          intervalKnown: false,
          missed: 0,
        },
      ],
      totalGenerations: 1,
      totalRouters: 1,
      totalBytes: 1024,
      lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
    })
    render(EngineRoom)
    await settle()
    await settle()

    expect(document.getElementById('bakg')?.classList.contains('dnodiagram')).toBe(false)
  })

  it('the disk group sits directly after memory, with its statements (#910)', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()
    await settle()

    const disk = document.getElementById('diskg')
    expect(disk).toBeTruthy()
    expect(disk?.previousElementSibling?.id).toBe('memg')
    expect(disk?.querySelector('h3')?.textContent).toBe('disk')
    expect(screen.getByRole('slider', { name: 'Days kept on disk' })).toBeTruthy()
    expect(screen.getByText(/^27 days · since .* · 812 MiB — filling$/)).toBeTruthy()
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

  it('a viewer sees no disk group and no state store, and on restart claims only that the buffer clears', async () => {
    // Both GETs are admin-gated (a directory is infrastructure detail,
    // the reasoning /api/config/problems already applies), so a viewer
    // gets absent-not-disabled: no group, no backend, and the memory
    // row's least claim.
    authState.state = 'authenticated'
    authState.role = 'viewer'
    fetchHistorySettings.mockRejectedValueOnce(Object.assign(new Error('403'), { status: 403 }))
    render(EngineRoom)
    await settle()
    await settle()

    expect(screen.getByText('the buffer clears')).toBeTruthy()
    expect(document.getElementById('diskg')).toBeNull()
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

  // #1142: the door labels were clipped mid-word ("6514 · TLS · listenir")
  // because they start at x=396 and ran past a 520-wide viewBox. jsdom does
  // not lay text out, so the check is arithmetic: a monospace glyph at the
  // labels' font size is ~0.6em wide, so the label must fit in what is left
  // of the viewBox.
  it('the ingest diagram is wide enough for its two door labels', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    const svg = document.querySelector<SVGSVGElement>('#engineroom-ingest .stpath')!
    const boxWidth = Number(svg.getAttribute('viewBox')!.split(' ')[2])
    const labels = [...svg.querySelectorAll<SVGTextElement>('text.sp-k, text.sp-n')].filter(
      (t) => Number(t.getAttribute('x')) > 300,
    )
    expect(labels.length).toBe(2)
    for (const label of labels) {
      const fontSize = label.classList.contains('sp-k') ? 10 : 9.5
      const width = (label.textContent ?? '').trim().length * fontSize * 0.6
      expect(Number(label.getAttribute('x')) + width).toBeLessThanOrEqual(boxWidth)
    }
  })

  // #1205: an upgraded install's router can be missing
  // remote-log-format=syslog with nothing on screen to say so. The
  // setup-drift line only appears once the server has flagged a
  // sustained run from a *declared* device (internal/syslog's
  // oversizedIsSetupDrift) -- it must name that router and point at the
  // docs, and it must stay silent for every other shape of the same
  // counters.
  it('names the router and links the docs once the server flags a setup-drift run', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    appState.stats = stats({
      syslog: {
        inUse: 1,
        capacity: 256,
        reservedForConfigured: 8,
        rejected: 0,
        rejectedConfigured: 0,
        dropped: 0,
        oversized: 42,
        rejectedConfiguredHosts: [],
        oversizedHost: '192.168.254.1',
        loss: {
          dropped: { recent: 0, lastAt: null, active: false },
          rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
          rejected: { recent: 0, lastAt: null, active: false },
          oversized: {
            recent: 42,
            lastAt: new Date().toISOString(),
            active: true,
            host: '192.168.254.1',
            declared: true,
            runs: 6,
            setupDrift: true,
          },
        },
      },
    })
    render(EngineRoom)
    await settle()

    expect(screen.getByText('router setup out of date')).toBeTruthy()
    expect(screen.getByText(/192\.168\.254\.1 is likely missing/)).toBeTruthy()
    const link = screen.getByRole('link', { name: 'RouterOS setup' })
    expect(link.getAttribute('href')).toBe(
      'https://github.com/tomlawesome/mikroview/blob/main/docs/routeros-setup.md',
    )
  })

  it('says nothing about setup drift for an ordinary oversized run', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    appState.stats = stats({
      syslog: {
        inUse: 1,
        capacity: 256,
        reservedForConfigured: 0,
        rejected: 0,
        rejectedConfigured: 0,
        dropped: 0,
        oversized: 3,
        rejectedConfiguredHosts: [],
        oversizedHost: '203.0.113.9',
        loss: {
          dropped: { recent: 0, lastAt: null, active: false },
          rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
          rejected: { recent: 0, lastAt: null, active: false },
          oversized: {
            recent: 3,
            lastAt: new Date().toISOString(),
            active: true,
            host: '203.0.113.9',
            declared: false,
            runs: 3,
            setupDrift: false,
          },
        },
      },
    })
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('router setup out of date')).toBeNull()
  })

  // #1198: the ingest card's own no-GeoIP line -- same fact, same
  // wording as the country filter's disabled row (FilterBar.svelte.test.ts),
  // so a reader who has seen one recognises the other. Set directly
  // rather than mocking fetchHealthz, same reasoning as that file's own
  // geoip describe block: ensureLoaded()'s fetch is a same-value no-op
  // once anything in this module has mounted before.
  describe('the no-GeoIP row (#1198)', () => {
    afterEach(() => {
      geoipState.enabled = null
    })

    it('says no GeoIP database once the server reports none configured', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      geoipState.enabled = false
      render(EngineRoom)
      await settle()

      expect(screen.getByText('geoip')).toBeTruthy()
      const link = screen.getByRole('link', { name: 'see docs ▸' })
      expect(link.getAttribute('href')).toBe(GEOIP_DOCS_URL)
    })

    it('says nothing once a database is configured', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      geoipState.enabled = true
      render(EngineRoom)
      await settle()

      expect(screen.queryByText('geoip')).toBeNull()
    })
  })

  // #1234: a router whose mikroview logging block was pasted more than
  // once floods every downstream count without anything downstream
  // being able to tell -- this row names the source, the apparent copy
  // count and the fix, mirroring the setup-drift row's own
  // active-vs-not shape just above.
  describe('the duplicate logging rules row (#1234)', () => {
    function syslogWithLoss(duplicate?: { host: string; copyCount: number }) {
      return stats({
        syslog: {
          inUse: 1,
          capacity: 256,
          reservedForConfigured: 0,
          rejected: 0,
          rejectedConfigured: 0,
          dropped: 0,
          oversized: 0,
          rejectedConfiguredHosts: [],
          oversizedHost: '',
          loss: {
            dropped: { recent: 0, lastAt: null, active: false },
            rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
            rejected: { recent: 0, lastAt: null, active: false },
            oversized: { recent: 0, lastAt: null, active: false, declared: false, runs: 0 },
            ...(duplicate
              ? {
                  duplicate: {
                    recent: 12,
                    lastAt: new Date().toISOString(),
                    active: true,
                    host: duplicate.host,
                    declared: true,
                    runs: 0,
                    copyCount: duplicate.copyCount,
                  },
                }
              : {}),
          },
        },
      })
    }

    it('names the source, the copy count and the cleanup command while a duplicate is active', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = syslogWithLoss({ host: '203.0.113.9', copyCount: 2 })
      render(EngineRoom)
      await settle()

      expect(screen.getByText('duplicate logging rules')).toBeTruthy()
      expect(screen.getByText(/203\.0\.113\.9 appears to be sending every line twice over/)).toBeTruthy()
      expect(screen.getByText('/system logging remove [find action=mikroview]')).toBeTruthy()
    })

    it('says nothing while no duplicate is active', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = syslogWithLoss()
      render(EngineRoom)
      await settle()

      expect(screen.queryByText('duplicate logging rules')).toBeNull()
    })
  })

  // #1109: checking reads events straight out of the buffer by cursor, so
  // the ingest group has to say which of two very different things is
  // true -- running late on a backlog it will work through, or events
  // that left the buffer before it ever reached them. Read off the whole
  // row rather than one span, because the copy is the row.
  describe('checking (#1109)', () => {
    function ingestRow(label: string): string | null {
      const section = document.querySelector('#engineroom-ingest')!
      for (const row of section.querySelectorAll('.orow')) {
        const first = row.querySelector('span')
        if (first?.textContent?.trim() === label) {
          return (row.textContent ?? '').replace(/\s+/g, ' ').trim()
        }
      }
      return null
    }

    it('says checking is caught up when the cursor is on the newest event', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = stats({ engine: { behind: 0, behindSeconds: 0, outrun: 0 } })
      render(EngineRoom)
      await settle()

      expect(ingestRow('Checking:')).toBe('Checking: caught up')
    })

    it('says how far behind and how late it is while it catches up', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = stats({ engine: { behind: 4200, behindSeconds: 3.4, outrun: 0 } })
      render(EngineRoom)
      await settle()

      expect(ingestRow('Checking:')).toBe('Checking: 4,200 events behind (3 s)')
    })

    it('says "event" for one, not "events"', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = stats({ engine: { behind: 1, behindSeconds: 0.2, outrun: 0 } })
      render(EngineRoom)
      await settle()

      expect(ingestRow('Checking:')).toBe('Checking: 1 event behind (0 s)')
    })

    it('shows the outrun count only once something really went unchecked', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = stats({ engine: { behind: 0, behindSeconds: 0, outrun: 0 } })
      render(EngineRoom)
      await settle()
      expect(ingestRow('Outrun:')).toBeNull()

      cleanup()
      appState.stats = stats({ engine: { behind: 12, behindSeconds: 1, outrun: 12000 } })
      render(EngineRoom)
      await settle()
      expect(ingestRow('Outrun:')).toBe('Outrun: 12,000')
    })

    it('says nothing at all against a server that reports no engine', async () => {
      authState.state = 'authenticated'
      authState.role = 'admin'
      appState.stats = stats()
      render(EngineRoom)
      await settle()

      expect(ingestRow('Checking:')).toBeNull()
      expect(ingestRow('Outrun:')).toBeNull()
    })
  })
})

// #1218 audit finding 15: EngineRoom's three newest groups -- new
// settings (#1218), router backups (#394 round 44) and drop list
// (#1225/#461), landed in that order across three separate recent
// commits -- shipped with no EngineRoom-level test at all (router
// backups had one narrow #1153 regression test, the other two none).
// Each delegates to a child component with its own full internal test
// file (ConfigUpgrade.svelte.test.ts, RouterBackups.svelte.test.ts,
// Droplist.svelte.test.ts), so what belongs here is the integration
// layer those don't cover: the group is mounted under its own heading,
// in the right position, admin-gated, wired to the right props, and
// answers the same dfail "the server did not answer" shape the older
// groups already do.
describe("EngineRoom's three newest settings groups (#1218 finding 15)", () => {
  it('mounts "new settings" right after ingest, before keys, admin-only', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()
    await settle()

    const group = document.getElementById('engineroom-new-settings')
    expect(group).toBeTruthy()
    expect(group?.querySelector('h3')?.textContent).toBe('new settings')
    // ConfigUpgrade really is the thing mounted here, not an empty
    // shell: its own "nothing missing" reading, off the mocked default.
    expect(within(group as HTMLElement).getByText(/Nothing new/)).toBeTruthy()

    // Document order: ingest's own group, then this one, then keys --
    // "straight after ingest" per the component's own comment.
    const headings = Array.from(document.querySelectorAll('.stsection h3')).map((h) => h.textContent)
    const ingestIndex = headings.indexOf('ingest')
    const newSettingsIndex = headings.indexOf('new settings')
    const keysIndex = headings.indexOf('keys')
    expect(ingestIndex).toBeLessThan(newSettingsIndex)
    expect(newSettingsIndex).toBeLessThan(keysIndex)
  })

  it('"new settings" actually renders what the server sends, not just its own empty state', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchConfigUpgrade.mockResolvedValueOnce({
      version: 'v1.3.0',
      settings: [{ key: 'geoip', block: '# geoip:\n#   dbPath: /etc/mikroview/GeoLite2-Country.mmdb' }],
    })
    render(EngineRoom)
    await settle()
    await settle()

    const group = document.getElementById('engineroom-new-settings') as HTMLElement
    expect(within(group).getByText(/dbPath: \/etc\/mikroview\/GeoLite2-Country\.mmdb/)).toBeTruthy()
  })

  it('a viewer sees no "new settings" group at all', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    expect(document.getElementById('engineroom-new-settings')).toBeNull()
    expect(screen.queryByText('new settings')).toBeNull()
  })

  it('mounts "router backups" right after disk, with the router the server reports', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchRouterBackups.mockResolvedValueOnce({
      enabled: true,
      keyUnreadable: false,
      port: ':2222',
      routers: [{ device: 'rb5009', generations: [{ id: 'g1', backupArrivedAt: '2026-09-01T00:00:00Z', backupBytes: 1024 }], intervalKnown: false, missed: 0 }],
      totalGenerations: 1,
      totalRouters: 1,
      totalBytes: 1024,
      lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
    })
    render(EngineRoom)
    await settle()
    await settle()

    const disk = document.getElementById('diskg')
    const backups = document.getElementById('bakg')
    expect(backups).toBeTruthy()
    expect(backups?.querySelector('h3')?.textContent).toBe('router backups')
    expect(disk?.nextElementSibling?.id).toBe('bakg')
    // RouterBackups really is the thing mounted here, wired to the
    // fetched resp -- not an empty shell.
    expect(within(backups as HTMLElement).getByText('rb5009')).toBeTruthy()
  })

  // #1275: the 60s tick, `ask again` and the refresh a write asks for
  // can all be in flight together, and nothing makes them answer in the
  // order they were sent. The older answer carries the rows as they
  // were before the newer request was even issued, so applying it puts
  // the operator's just-kept row back to what it was -- and walks
  // fetchedAt backwards, which is the stamp RouterBackups.svelte uses
  // to decide its optimistic copy has been confirmed and can be
  // dropped. The keep then reads on screen as one that did not happen.
  it('ignores a router-backups poll that answers after a newer one (#1275)', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    vi.useFakeTimers()
    try {
      const answer: ((r: RouterBackupsResponse) => void)[] = []
      const held = () => new Promise<RouterBackupsResponse>((resolve) => answer.push(resolve))
      fetchRouterBackups.mockImplementationOnce(held).mockImplementationOnce(held)

      render(EngineRoom)
      await settle()
      // The mount poll is out; the tick a minute later sends a second.
      vi.advanceTimersByTime(60_000)
      await settle()
      expect(answer).toHaveLength(2)

      // The newer request answers first, then the older one comes back.
      answer[1](backupsWith('rb-newer'))
      await settle()
      answer[0](backupsWith('rb-older'))
      await settle()
      await settle()

      const backups = document.getElementById('bakg') as HTMLElement
      expect(within(backups).getByText('rb-newer')).toBeTruthy()
      expect(within(backups).queryByText('rb-older')).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('"router backups" answers unknown, with a working ask again, when the server does not', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchRouterBackups.mockRejectedValueOnce(new Error('network error'))
    render(EngineRoom)
    await settle()
    await settle()

    const backups = document.getElementById('bakg')
    expect(backups?.classList.contains('dfail')).toBe(true)
    expect(within(backups as HTMLElement).getByText(/unknown — the server did not answer/)).toBeTruthy()

    fetchRouterBackups.mockResolvedValueOnce({
      enabled: false,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
    })
    await fireEvent.click(within(backups as HTMLElement).getByRole('button', { name: 'ask again' }))
    await settle()
    await settle()

    expect(document.getElementById('bakg')?.classList.contains('dfail')).toBe(false)
  })

  it('a viewer sees no "router backups" group at all', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    expect(document.getElementById('bakg')).toBeNull()
    expect(screen.queryByText('router backups')).toBeNull()
  })

  it('mounts "drop list" right after router backups, with the entry the server reports', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchDroplist.mockResolvedValueOnce({
      listName: 'mikroview-drops',
      entries: [{ cidr: '203.0.113.0/24', addedBy: 'tom', addedAt: '2026-09-14T00:00:00Z', reason: 'ssh brute force' }],
      key: { present: false },
      ownRangesKnown: true,
      setup: { scheduler: '', rule: '', disableRule: '', emptyList: '' },
    })
    render(EngineRoom)
    await settle()
    await settle()

    const backups = document.getElementById('bakg')
    const droplist = document.getElementById('engineroom-droplist')
    expect(droplist).toBeTruthy()
    expect(droplist?.querySelector('h3')?.textContent).toBe('drop list')
    expect(backups?.nextElementSibling?.id).toBe('engineroom-droplist')
    // Droplist really is the thing mounted here, wired to the fetched
    // resp -- not an empty shell.
    expect(within(droplist as HTMLElement).getByText('203.0.113.0/24')).toBeTruthy()
  })

  // #1260: refreshDroplist used to read GET /api/droplist with
  // window.location.host, this browser tab's own address, rather than
  // wizardState.address (#1213 -- the operator's own saved answer to
  // "what address can your router reach mikroview on?"). The four
  // printed setup commands (scheduler, drop rule, and the two emergency
  // blocks) the server bakes into that response are only right when
  // this call uses the same stored address Droplist.svelte's mintKey
  // already reads (see its own #1260 test).
  it('polls the drop list against the operator saved address, not this tab\'s own host', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    wizardState.address = 'operator-saved.example:8443'
    fetchDroplist.mockResolvedValueOnce({
      listName: 'mikroview-drops',
      entries: [],
      key: { present: false },
      ownRangesKnown: false,
      setup: { scheduler: '', rule: '', disableRule: '', emptyList: '' },
    })
    render(EngineRoom)
    await settle()
    await settle()

    expect(fetchDroplist).toHaveBeenCalledWith('operator-saved.example:8443')
  })

  // FB12 (v0.6.0 audit): nothing exercised the case where this group
  // mounts before the wizard has answered "what address can your router
  // reach mikroview on?" -- wizardState.address only leaves '' once
  // wizardState.refresh() resolves, which is driven by an effect
  // elsewhere (SetupWizard.svelte's), not by this component. Today's
  // behaviour: the fetch goes out with whatever is currently known, same
  // as the saved-address test above -- there is no wait, and no crash.
  // That matches saveAddress's own "an empty value renders its own
  // no-command state server-side" reasoning (wizard.svelte.ts) rather
  // than this component inventing a second way to say "not answered
  // yet", so it is left as is: this test records the behaviour rather
  // than changing it.
  it('polls the drop list even before the wizard address is known, rather than waiting for it', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    expect(wizardState.address).toBe('')
    fetchDroplist.mockResolvedValueOnce({
      listName: 'mikroview-drops',
      entries: [],
      key: { present: false },
      ownRangesKnown: false,
      setup: { scheduler: '', rule: '', disableRule: '', emptyList: '' },
    })
    render(EngineRoom)
    await settle()
    await settle()

    expect(fetchDroplist).toHaveBeenCalledWith('')
    expect(document.getElementById('engineroom-droplist')).toBeTruthy()
  })

  it('"drop list" answers unknown, with a working ask again, when the server does not', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    fetchDroplist.mockRejectedValueOnce(new Error('network error'))
    render(EngineRoom)
    await settle()
    await settle()

    const droplist = document.getElementById('engineroom-droplist')
    expect(droplist?.classList.contains('dfail')).toBe(true)
    expect(within(droplist as HTMLElement).getByText(/unknown — the server did not answer/)).toBeTruthy()

    fetchDroplist.mockResolvedValueOnce({
      listName: 'mikroview-drops',
      entries: [],
      key: { present: false },
      ownRangesKnown: false,
      setup: { scheduler: '', rule: '', disableRule: '', emptyList: '' },
    })
    await fireEvent.click(within(droplist as HTMLElement).getByRole('button', { name: 'ask again' }))
    await settle()
    await settle()

    expect(document.getElementById('engineroom-droplist')?.classList.contains('dfail')).toBe(false)
  })

  it('a viewer sees no "drop list" group at all', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(EngineRoom)
    await settle()

    expect(document.getElementById('engineroom-droplist')).toBeNull()
    expect(screen.queryByText('drop list')).toBeNull()
  })
})
