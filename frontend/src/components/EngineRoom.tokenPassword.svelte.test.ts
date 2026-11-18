// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 33): minting a key from Settings. Gauntlet's POST /api/tokens
// takes the calling admin's own password (ASVS 7.5.3: a token outlives the
// session that minted it), so the mint form asks for it and sends it:
// createToken(name, kind, device, password). Ingest keys additionally ask
// never to expire; that is api.ts's job and is pinned in api.gauntlet.test.ts.
//
// Assumption the build may change (named in the commit message): the password
// is a field in the mint form, labelled with the word "password", filled
// before "mint it" is pressed.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
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
    { id: 'u1', username: 'alex', role: 'admin', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T13:41:00Z', hasLocalPassword: true, sso: false },
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
  fetchGeoSettings: vi.fn(async () => ({
    source: 'dbip',
    sources: {
      dbip: { loaded: true, fetchedAt: '2026-09-27T00:00:00Z', nextRefresh: null, lastError: null },
      ipinfo: { keySet: false, setAt: null, setBy: null, loaded: false, fetchedAt: null, nextRefresh: null, lastError: null },
      maxmind: { keySet: false, setAt: null, setBy: null, loaded: false, fetchedAt: null, nextRefresh: null, lastError: null },
    },
  })),
  setGeoIpinfoToken: vi.fn(),
  removeGeoIpinfoToken: vi.fn(),
  setGeoMaxmindKey: vi.fn(),
  removeGeoMaxmindKey: vi.fn(),
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
import { wizardState } from '../lib/wizard.svelte'
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


async function openMintForm() {
  authState.state = 'authenticated'
  authState.role = 'admin'
  render(EngineRoom)
  await settle()
  await fireEvent.click(screen.getByRole('button', { name: '+ mint a new key' }))
  await settle()
}

describe('minting a key asks for the admin password and sends it (#1202 spec 33)', () => {
  it('an api key is created with the typed password', async () => {
    const { createToken } = await import('../lib/api')
    vi.mocked(createToken).mockResolvedValueOnce({
      id: 't9',
      name: 'nas-read',
      kind: 'api',
      createdAt: '2026-10-10T00:00:00Z',
      value: 'gnt_4c21secret9b0d',
    })
    await openMintForm()

    await fireEvent.input(screen.getByLabelText('key name'), { target: { value: 'nas-read' } })
    await fireEvent.input(screen.getByLabelText(/password/i), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'mint it' }))
    await settle()

    expect(createToken).toHaveBeenCalledTimes(1)
    expect(createToken).toHaveBeenCalledWith('nas-read', 'api', undefined, 'hunter2')
    expect(screen.getByText('gnt_4c21secret9b0d')).toBeTruthy()
  })

  it('an ingest key is created with the typed password and its device', async () => {
    const { createToken, fetchDevices } = await import('../lib/api')
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
    vi.mocked(createToken).mockResolvedValueOnce({
      id: 't10',
      name: 'rb5009-b',
      kind: 'ingest',
      device: 'rb5009',
      createdAt: '2026-10-10T00:00:00Z',
      value: 'gnt_ingestsecret',
    })
    await openMintForm()

    await fireEvent.click(screen.getByRole('button', { name: 'ingest' }))
    await settle()
    await fireEvent.input(screen.getByLabelText(/password/i), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'mint it' }))
    await settle()

    expect(createToken).toHaveBeenCalledWith('', 'ingest', 'rb5009', 'hunter2')
  })

  // Pair for the two above: with no password typed, nothing is sent.
  it('does not call createToken when no password has been typed', async () => {
    const { createToken } = await import('../lib/api')
    await openMintForm()

    await fireEvent.input(screen.getByLabelText('key name'), { target: { value: 'nas-read' } })
    await fireEvent.click(screen.getByRole('button', { name: 'mint it' }))
    await settle()

    expect(createToken).not.toHaveBeenCalled()
  })
})
