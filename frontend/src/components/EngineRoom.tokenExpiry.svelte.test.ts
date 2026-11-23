// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U4 (note_34034): an expired token's row says "expired".
// Gauntlet tokens now carry an expiry (an api key's default is one year; an
// ingest key's is "never"), so the Keys list must tell the person which row
// has run out. Rows are found by their key name, then widened to the largest
// element that holds no other key's name, so the test does not depend on the
// row's markup.
//
// Assumption the fix may change (named in the commit message): the list
// token carries `expiresAt` (an RFC 3339 string), absent for a key that never
// expires.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
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
  fetchTokens: vi.fn(async () => []),
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



function rowOf(name: string, otherName: string): HTMLElement {
  let el: HTMLElement = screen.getByText(name)
  while (el.parentElement && !el.parentElement.textContent?.includes(otherName)) {
    el = el.parentElement
  }
  return el
}

async function openWith(tokens: unknown[]) {
  const { fetchTokens } = await import('../lib/api')
  vi.mocked(fetchTokens).mockResolvedValue(tokens as never)
  authState.state = 'authenticated'
  authState.role = 'admin'
  render(EngineRoom)
  await settle()
  await settle()
}

const EXPIRED = { id: 't1', name: 'old-nas-key', kind: 'api', createdAt: '2020-01-01T00:00:00Z', expiresAt: '2021-01-01T00:00:00Z' }
const LIVE = { id: 't2', name: 'fresh-nas-key', kind: 'api', createdAt: '2026-10-01T00:00:00Z', expiresAt: '2099-01-01T00:00:00Z' }
const NEVER = { id: 't3', name: 'rb5009-ingest', kind: 'ingest', device: 'rb5009', createdAt: '2026-08-01T00:00:00Z' }

describe('a token past its expiry is marked in the Keys list (#1202 review U4)', () => {
  // #1202 review U4
  it('the row of a key whose expiry has passed says "expired"', async () => {
    await openWith([EXPIRED, LIVE])
    expect(rowOf('old-nas-key', 'fresh-nas-key').textContent).toMatch(/expired/i)
  })

  // #1202 review U4: pair -- the same list, a key still in date.
  it('the row of a key still in date does not say "expired"', async () => {
    await openWith([EXPIRED, LIVE])
    expect(rowOf('fresh-nas-key', 'old-nas-key').textContent).not.toMatch(/expired/i)
  })

  // #1202 review U4: pair -- no expiry at all is "never expires", not expired.
  it('the row of a key with no expiry does not say "expired"', async () => {
    await openWith([EXPIRED, NEVER])
    expect(rowOf('rb5009-ingest', 'old-nas-key').textContent).not.toMatch(/expired/i)
  })
})
