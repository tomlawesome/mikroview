// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U8: on the People screen, removing someone, resetting their
// password, clearing their authenticator app and clearing their passkeys each
// ask the calling admin for their own password (gauntlet re-checks it, rate
// limited) and send it. A wrong password keeps the dialog open with an error
// and an emptied field; nothing is sent without a password. Written before
// the fix.
//
// #1202 second review (note_34147), the Engine room's People and sessions
// rows. Written before the fix.
//
//  - U9:  "sign out everywhere" asks the calling account for its own password
//         (gauntlet re-checks it, rate limited) and sends it. A wrong password
//         keeps the prompt open with an error and an emptied field. An
//         SSO-only account (session hasLocalPassword: false) is not asked.
//  - U12: removing an account that gauntlet deleted but whose tokens it could
//         not revoke (api answers USER_DELETED_TOKENS_REMAIN) refreshes the
//         list and tells the admin to revoke that account's tokens.
//  - U15: opening the People-screen password prompt puts the cursor in the
//         password field.
//
// Assumptions the build may change (named in the commit message): the sign-out
// prompt is the same kind as the People ones -- a password input labelled "your
// password" and a submit button named starting "confirm"; the screen shows the
// text the api answers for U12 (or its own wording with "revoke" and "tokens").

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
  USER_DELETED_TOKENS_REMAIN: "kai was deleted, but their tokens were not revoked. Revoke that account's tokens by hand.",
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
  vi.mocked(api.signOutEverywhere).mockReset()
  vi.mocked(api.fetchUsers).mockReset()
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
  authState.hasLocalPassword = true
})


async function openEngineRoom() {
  authState.state = 'authenticated'
  authState.role = 'admin'
  authState.signedInSince = new Date(Date.now() - 4.5 * 86_400_000).toISOString()
  render(EngineRoom)
  await settle()
}

const field = () => screen.getByLabelText(/your password/i) as HTMLInputElement

async function clickButton(name: string | RegExp) {
  await fireEvent.click(screen.getByRole('button', { name }))
  await settle()
}

async function typeAndConfirm(password: string | null) {
  if (password !== null) {
    await fireEvent.input(field(), { target: { value: password } })
  }
  await fireEvent.click(screen.getByRole('button', { name: /^confirm/ }))
  await settle()
}

async function signOutMock() {
  const api = await import('../lib/api')
  return vi.mocked(api.signOutEverywhere)
}

describe('sign out everywhere asks the account for its own password (#1202 review U9)', () => {
  // #1202 review U9
  it('opens a password prompt and has called nothing yet', async () => {
    const mock = await signOutMock()
    await openEngineRoom()
    // Pair: before the button is pressed there is no prompt.
    expect(screen.queryByLabelText(/your password/i)).toBeNull()

    await clickButton('sign out everywhere')

    expect(field().type).toBe('password')
    expect(mock).not.toHaveBeenCalled()
    expect(screen.queryByText(/every other session has been ended/)).toBeNull()
  })

  // #1202 review U9
  it('sends the typed password, then says every other session has been ended and closes the prompt', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce(null)
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm('hunter2')

    expect(mock).toHaveBeenCalledTimes(1)
    expect(mock).toHaveBeenCalledWith('hunter2')
    expect(screen.getByText(/every other session has been ended/)).toBeTruthy()
    expect(screen.queryByLabelText(/your password/i)).toBeNull()
  })

  // #1202 review U9
  it('sends nothing when confirmed with no password typed', async () => {
    const mock = await signOutMock()
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm(null)

    expect(mock).not.toHaveBeenCalled()
    expect(field().value).toBe('')
  })

  // #1202 review U9
  it('a wrong password keeps the prompt open, shows the error and empties the field', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce('incorrect password')
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm('wrong-one')

    expect(mock).toHaveBeenCalledWith('wrong-one')
    expect(screen.getByText(/incorrect password/i)).toBeTruthy()
    expect(field().value).toBe('')
    expect(screen.queryByText(/every other session has been ended/)).toBeNull()
  })

  // #1202 review U9: pair -- the person can type again.
  it('after a wrong password, a second try sends the new password and succeeds', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce('incorrect password').mockResolvedValueOnce(null)
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm('wrong-one')
    await typeAndConfirm('right-one')

    expect(mock).toHaveBeenCalledTimes(2)
    expect(mock).toHaveBeenLastCalledWith('right-one')
    expect(screen.getByText(/every other session has been ended/)).toBeTruthy()
  })

  // #1202 review U9
  it('a rate-limit answer says too many attempts, keeps the prompt open and empties the field', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce('too many attempts, try again later')
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm('hunter2')

    expect(screen.getByText(/too many attempts/i)).toBeTruthy()
    expect(field().value).toBe('')
  })

  // #1202 review U9
  it('does not leave the password on the page after success or after a wrong one', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce('incorrect password').mockResolvedValueOnce(null)
    await openEngineRoom()

    await clickButton('sign out everywhere')
    await typeAndConfirm('sw0rdfish-secret')
    expect(document.body.innerHTML).not.toContain('sw0rdfish-secret')
    await typeAndConfirm('sw0rdfish-secret')
    expect(document.body.innerHTML).not.toContain('sw0rdfish-secret')
  })

  // #1202 review U9: an SSO-only account has no password to give, so it is not asked.
  it('an SSO-only account (hasLocalPassword false) is not asked: the call goes straight out with no password', async () => {
    const mock = await signOutMock()
    mock.mockResolvedValueOnce(null)
    authState.hasLocalPassword = false
    await openEngineRoom()

    await clickButton('sign out everywhere')

    expect(screen.queryByLabelText(/your password/i)).toBeNull()
    expect(mock).toHaveBeenCalledTimes(1)
    expect(mock.mock.calls[0][0]).toBeFalsy()
    expect(screen.getByText(/every other session has been ended/)).toBeTruthy()
  })
})

describe('removing an account whose tokens could not be revoked (#1202 review U12)', () => {
  const alex = { id: 'u1', username: 'alex', role: 'admin', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T13:41:00Z', hasLocalPassword: true, sso: false }

  async function removeKai(answer: string | null) {
    const api = await import('../lib/api')
    vi.mocked(api.deleteUser).mockResolvedValueOnce(answer)
    await openEngineRoom()
    await clickButton('remove')
    return api
  }

  // #1202 review U12
  it('refreshes the list so the row goes, and tells the admin to revoke that account\'s tokens', async () => {
    const api = await import('../lib/api')
    await removeKai(api.USER_DELETED_TOKENS_REMAIN)
    const before = vi.mocked(api.fetchUsers).mock.calls.length
    vi.mocked(api.fetchUsers).mockResolvedValue([alex] as never)

    await typeAndConfirm('hunter2')
    await vi.waitFor(() => expect(vi.mocked(api.fetchUsers).mock.calls.length).toBeGreaterThan(before))
    await settle()

    expect(api.deleteUser).toHaveBeenCalledWith('u2', 'hunter2')
    expect(screen.queryByText('kai')).toBeNull()
    expect(screen.queryAllByText(/revoke.*tokens|tokens.*revoke/i).length).toBeGreaterThan(0)
  })

  // #1202 review U12: pair -- an ordinary refusal keeps the row and gives no revoke advice.
  it('an ordinary refusal keeps the row and does not tell the admin to revoke tokens', async () => {
    await removeKai('the account could not be deleted')

    await typeAndConfirm('hunter2')

    expect(screen.getByText('kai')).toBeTruthy()
    expect(screen.getByText('the account could not be deleted')).toBeTruthy()
    expect(screen.queryAllByText(/revoke.*tokens|tokens.*revoke/i)).toHaveLength(0)
  })

  // #1202 review U12: pair -- a clean delete gives no revoke advice either.
  it('a clean delete takes the row out and gives no revoke advice', async () => {
    await removeKai(null)

    await typeAndConfirm('hunter2')

    expect(screen.queryByText('kai')).toBeNull()
    expect(screen.queryAllByText(/revoke.*tokens|tokens.*revoke/i)).toHaveLength(0)
  })
})

describe('the People-screen password prompt takes the cursor (#1202 review U15)', () => {
  const buttons = ['remove', 'reset password', 'clear authenticator app', 'clear passkeys']

  // #1202 review U15
  it.each(buttons)('after "%s" is pressed, focus is in the password field', async (button) => {
    await openEngineRoom()
    // Pair: before the prompt opens, focus is not in any password field.
    expect(document.activeElement?.getAttribute('type')).not.toBe('password')

    await clickButton(button)

    expect(document.activeElement).toBe(field())
  })

  // #1202 review U15: focus is taken by the prompt even when the pressed button is focused first,
  // as a real click or Enter would leave it.
  it('takes focus from the button that was pressed', async () => {
    await openEngineRoom()
    const pressed = screen.getByRole('button', { name: 'reset password' })
    pressed.focus()
    expect(document.activeElement).toBe(pressed)

    await fireEvent.click(pressed)
    await settle()

    expect(document.activeElement).toBe(field())
  })
})
