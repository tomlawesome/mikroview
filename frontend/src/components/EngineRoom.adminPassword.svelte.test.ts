// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U8: on the People screen, removing someone, resetting their
// password, clearing their authenticator app and clearing their passkeys each
// ask the calling admin for their own password (gauntlet re-checks it, rate
// limited) and send it. A wrong password keeps the dialog open with an error
// and an emptied field; nothing is sent without a password. Written before
// the fix.
//
// Assumptions the build may change (named in the commit message): the action
// button still opens the prompt; the prompt has a password input labelled
// "your password"; its submit button is named starting "confirm".

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
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
    { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T12:07:00Z', hasLocalPassword: true, sso: false, hasTOTP: true, passkeyCount: 1 },
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

const ISSUED = { username: 'kai', code: 'ABCD-EFGH-JKLM-NPQR', expiresAt: '2026-10-11T00:00:00Z' }

beforeEach(async () => {
  vi.clearAllMocks()
  const api = await import('../lib/api')
  vi.mocked(api.deleteUser).mockReset()
  vi.mocked(api.resetUserPassword).mockReset()
  vi.mocked(api.clearUserTOTP).mockReset()
  vi.mocked(api.clearUserPasskeys).mockReset()
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
  persistenceState.info = null
  deckOrderState.set(['fall', 'metrics', 'live', 'docket', 'entities', 'engineroom'])
  wizardState.address = ''
})

// The four actions, each against kai (u2). `ok` is what the api answers on
// success; `call` is the mocked api function the screen must reach.
type Action = {
  name: string
  button: string
  ok: unknown
  call: () => Promise<{ mock: ReturnType<typeof vi.fn> }>
}

async function apiMock(key: 'deleteUser' | 'resetUserPassword' | 'clearUserTOTP' | 'clearUserPasskeys') {
  const api = await import('../lib/api')
  return { mock: vi.mocked(api[key]) as unknown as ReturnType<typeof vi.fn> }
}

const actions: Action[] = [
  { name: 'remove', button: 'remove', ok: null, call: () => apiMock('deleteUser') },
  { name: 'reset password', button: 'reset password', ok: ISSUED, call: () => apiMock('resetUserPassword') },
  { name: 'clear authenticator app', button: 'clear authenticator app', ok: null, call: () => apiMock('clearUserTOTP') },
  { name: 'clear passkeys', button: 'clear passkeys', ok: null, call: () => apiMock('clearUserPasskeys') },
]

async function openPeople() {
  authState.state = 'authenticated'
  authState.role = 'admin'
  render(EngineRoom)
  await settle()
}

const field = () => screen.getByLabelText(/your password/i) as HTMLInputElement

async function begin(button: string) {
  await fireEvent.click(screen.getByRole('button', { name: button }))
  await settle()
}

async function typeAndConfirm(password: string | null) {
  if (password !== null) {
    await fireEvent.input(field(), { target: { value: password } })
  }
  await fireEvent.click(screen.getByRole('button', { name: /^confirm/ }))
  await settle()
}

describe.each(actions)('$name asks the admin for their own password (#1202 review U8)', (action) => {
  it('opens a password prompt and has called nothing yet', async () => {
    const { mock } = await action.call()
    await openPeople()
    // Pair: before the action is chosen there is no prompt.
    expect(screen.queryByLabelText(/your password/i)).toBeNull()

    await begin(action.button)

    expect(field().type).toBe('password')
    expect(mock).not.toHaveBeenCalled()
  })

  it('sends the typed password with the account id', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce(action.ok)
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('hunter2')

    expect(mock).toHaveBeenCalledTimes(1)
    expect(mock).toHaveBeenCalledWith('u2', 'hunter2')
  })

  it('sends nothing when confirmed with no password typed', async () => {
    const { mock } = await action.call()
    await openPeople()

    await begin(action.button)
    await typeAndConfirm(null)

    expect(mock).not.toHaveBeenCalled()
    // Still waiting for the password.
    expect(field().value).toBe('')
  })

  it('a wrong password keeps the prompt open, shows the error, and empties the field', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce('incorrect password')
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('wrong-one')

    expect(mock).toHaveBeenCalledWith('u2', 'wrong-one')
    expect(screen.getByText(/incorrect password/i)).toBeTruthy()
    expect(field().value).toBe('')
    // Nothing was done to the account: no code shown, row still listed.
    expect(screen.queryByTestId('reset-code')).toBeNull()
    expect(screen.getByText('kai')).toBeTruthy()
  })

  // Pair for the wrong-password case: the person can type again and the
  // second attempt carries the new password.
  it('after a wrong password, a second try sends the new password and succeeds', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce('incorrect password').mockResolvedValueOnce(action.ok)
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('wrong-one')
    await typeAndConfirm('right-one')

    expect(mock).toHaveBeenCalledTimes(2)
    expect(mock).toHaveBeenLastCalledWith('u2', 'right-one')
    expect(screen.queryByLabelText(/your password/i)).toBeNull()
  })

  it('a rate-limit answer says too many attempts, keeps the prompt open and empties the field', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce('too many attempts, try again later')
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('hunter2')

    expect(screen.getByText(/too many attempts/i)).toBeTruthy()
    expect(field().value).toBe('')
  })

  it("a 409 shows the server's reason in the prompt", async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce('the last admin cannot be changed')
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('hunter2')

    expect(screen.getByText('the last admin cannot be changed')).toBeTruthy()
  })

  it('on success the prompt is gone and the password is not left on the page or in the store', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce(action.ok)
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('sw0rdfish-secret')

    expect(screen.queryByLabelText(/your password/i)).toBeNull()
    expect(document.body.innerHTML).not.toContain('sw0rdfish-secret')
    expect(JSON.stringify(usersState)).not.toContain('sw0rdfish-secret')
  })

  it('does not keep the password in the store after a wrong password either', async () => {
    const { mock } = await action.call()
    mock.mockResolvedValueOnce('incorrect password')
    await openPeople()

    await begin(action.button)
    await typeAndConfirm('sw0rdfish-secret')

    expect(JSON.stringify(usersState)).not.toContain('sw0rdfish-secret')
  })
})

// Positive outcomes specific to each action, so "the prompt works" cannot
// pass while the action itself is broken.
describe('what each action does once the password is accepted (#1202 review U8)', () => {
  it('reset password shows the one-time code', async () => {
    const { mock } = await apiMock('resetUserPassword')
    mock.mockResolvedValueOnce(ISSUED)
    await openPeople()

    await begin('reset password')
    await typeAndConfirm('hunter2')

    expect(screen.getByTestId('reset-code').textContent).toBe('ABCD-EFGH-JKLM-NPQR')
  })

  it('remove takes the row out of the list', async () => {
    const api = await import('../lib/api')
    vi.mocked(api.deleteUser).mockResolvedValueOnce(null)
    await openPeople()

    await begin('remove')
    await typeAndConfirm('hunter2')

    expect(screen.queryByText('kai')).toBeNull()
  })
})
