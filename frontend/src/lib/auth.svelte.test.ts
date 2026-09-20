// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthSession } from './types'

// auth.svelte.ts talks to the backend exclusively through these
// lib/api.ts functions -- mock the whole module so tests exercise only
// AuthState's own state-transition logic, never a real fetch().
vi.mock('./api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  setNewPasswordAfterReset: vi.fn(),
  signOutEverywhere: vi.fn(),
  fetchPersistence: vi.fn(),
}))

import {
  fetchAuthSession,
  login,
  logout,
  register,
  setNewPasswordAfterReset,
  signOutEverywhere,
  fetchPersistence,
} from './api'
import { authState, pageReload } from './auth.svelte'
import { appState } from './state.svelte'
import { flagsState } from './flags.svelte'
import { watchlistState } from './watchlist.svelte'
import { wizardState } from './wizard.svelte'
import { logEveryRuleWorkState } from './logEveryRuleWork.svelte'
import { tokensState } from './tokens.svelte'
import { usersState } from './users.svelte'
import { auditState } from './audit.svelte'
import { persistenceState } from './persistence.svelte'
import { configProblemsState } from './configProblems.svelte'
import { configUpgradeState } from './configUpgrade.svelte'
import { emptyFilters, type ApiToken, type AuditEntry, type Device, type Flag, type RouterBackupsResponse, type Stats, type UserSummary, type WatchlistEntry } from './types'

function session(overrides: Partial<AuthSession> = {}): AuthSession {
  return {
    setupRequired: false,
    authenticated: false,
    ssoAvailable: false,
    ...overrides,
  }
}

// authState is a module-level singleton (see auth.svelte.ts), so every
// test shares the same instance -- reset it by hand between tests rather
// than re-importing the module, since there's no exported reset() and
// the audit's ask is to test the real, actually-used object.
beforeEach(() => {
  vi.resetAllMocks()
  authState.state = 'loading'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  authState.ssoError = null
  authState.justSignedOut = false
  authState.signedInSince = ''
  authState.mustChangePassword = false
  window.history.replaceState(null, '', '/')
  // jsdom cannot navigate; the reload tests below assert on this spy.
  sessionStorage.clear()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
})

describe('AuthState.check', () => {
  it('applies an authenticated session', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'admin', ssoAvailable: true }),
    )

    await authState.check()

    expect(authState.state).toBe('authenticated')
    expect(authState.username).toBe('tom')
    expect(authState.role).toBe('admin')
    expect(authState.ssoAvailable).toBe(true)
  })

  // #677's sessions row ("this device ... signed in 4 d") reads this.
  it('carries signedInSince through from the session, and clears it once signed out', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'admin', signedInSince: '2026-08-27T00:00:00Z' }),
    )
    await authState.check()
    expect(authState.signedInSince).toBe('2026-08-27T00:00:00Z')

    vi.mocked(fetchAuthSession).mockResolvedValue(session())
    await authState.check()
    expect(authState.signedInSince).toBe('')
  })


  it('applies a viewer session (#653s third role)', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'kai', role: 'viewer', ssoAvailable: false }),
    )

    await authState.check()

    expect(authState.state).toBe('authenticated')
    expect(authState.role).toBe('viewer')
  })

  it('reports setup-required when no accounts exist yet', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(session({ setupRequired: true }))

    await authState.check()

    expect(authState.state).toBe('setup-required')
  })

  it('falls back to unauthenticated and clears identity when the session is anonymous', async () => {
    authState.username = 'stale'
    authState.role = 'admin'
    vi.mocked(fetchAuthSession).mockResolvedValue(session())

    await authState.check()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.username).toBe('')
    expect(authState.role).toBe('')
  })

  it('treats an unreachable API as unauthenticated rather than stalling on loading', async () => {
    vi.mocked(fetchAuthSession).mockRejectedValue(new Error('network down'))

    await authState.check()

    expect(authState.state).toBe('unauthenticated')
  })
})

describe('AuthState.login', () => {
  it('re-checks the session and returns null on success', async () => {
    vi.mocked(login).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'tom', role: 'user' }))

    const result = await authState.login('tom', 'hunter2')

    expect(result).toBeNull()
    expect(login).toHaveBeenCalledWith('tom', 'hunter2')
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
    expect(authState.username).toBe('tom')
  })

  it('returns the error and leaves state untouched on failure, without re-checking', async () => {
    vi.mocked(login).mockResolvedValue('invalid username or password')

    const result = await authState.login('tom', 'wrong')

    expect(result).toBe('invalid username or password')
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('loading')
  })
})

describe('AuthState.register', () => {
  it('re-checks the session and returns null on success', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'admin', role: 'admin' }))

    const result = await authState.register('admin', 'hunter2')

    expect(result).toBeNull()
    expect(register).toHaveBeenCalledWith('admin', 'hunter2')
    expect(authState.state).toBe('authenticated')
  })

  it('returns the error without re-checking on failure', async () => {
    vi.mocked(register).mockResolvedValue('username already taken')

    const result = await authState.register('admin', 'hunter2')

    expect(result).toBe('username already taken')
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })
})


describe('AuthState.logout', () => {
  it('clears identity and returns to unauthenticated without re-checking', async () => {
    authState.state = 'authenticated'
    authState.username = 'tom'
    authState.role = 'admin'
    vi.mocked(logout).mockResolvedValue(null)

    await authState.logout()

    expect(logout).toHaveBeenCalled()
    expect(authState.state).toBe('unauthenticated')
    expect(authState.username).toBe('')
    expect(authState.role).toBe('')
    expect(fetchAuthSession).not.toHaveBeenCalled()
    // Set so AuthLogin's next mount plays the door's way-out beat
    // (#645) -- consumeJustSignedOut() below is how it reads this.
    expect(authState.justSignedOut).toBe(true)
  })
})

describe('AuthState.signOutEverywhere', () => {
  it('calls the endpoint and re-checks the session, unlike logout it does not drop to unauthenticated', async () => {
    authState.state = 'authenticated'
    authState.username = 'tom'
    authState.role = 'admin'
    vi.mocked(signOutEverywhere).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'admin', signedInSince: '2026-08-31T00:00:00Z' }),
    )

    const err = await authState.signOutEverywhere()

    expect(signOutEverywhere).toHaveBeenCalled()
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(err).toBeNull()
    expect(authState.state).toBe('authenticated')
    expect(authState.signedInSince).toBe('2026-08-31T00:00:00Z')
  })

  it('returns the error and skips the re-check on failure', async () => {
    vi.mocked(signOutEverywhere).mockResolvedValue('signOutEverywhere: 500')

    const err = await authState.signOutEverywhere()

    expect(err).toBe('signOutEverywhere: 500')
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })
})

describe('AuthState reloads the page so nothing survives the account (#1083, 2a)', () => {
  it('logout reloads once the server has ended the session', async () => {
    authState.state = 'authenticated'
    vi.mocked(logout).mockResolvedValue(null)

    await authState.logout()

    expect(pageReload.now).toHaveBeenCalledTimes(1)
  })

  it('logout does not reload when the server call failed -- a reload would sign the cookie back in', async () => {
    authState.state = 'authenticated'
    vi.mocked(logout).mockResolvedValue('network down')

    await authState.logout()

    expect(pageReload.now).not.toHaveBeenCalled()
    expect(authState.state).toBe('unauthenticated')
  })

  it('a 401 bounce reloads', () => {
    authState.state = 'authenticated'
    authState.handleUnauthorized()
    expect(pageReload.now).toHaveBeenCalledTimes(1)
  })

  it('a 401 while already signed out does not reload (no loop)', () => {
    authState.state = 'unauthenticated'
    authState.handleUnauthorized()
    expect(pageReload.now).not.toHaveBeenCalled()
  })

  it('the way-out beat survives the reload via sessionStorage, once', async () => {
    authState.state = 'authenticated'
    vi.mocked(logout).mockResolvedValue(null)
    await authState.logout()
    // The old page's login screen mounts before the reload lands and
    // takes the in-memory flag only -- the storage key must outlive it.
    expect(authState.consumeJustSignedOut()).toBe(true)
    expect(sessionStorage.getItem('mikroview.justSignedOut')).toBe('1')

    // The fresh page: in-memory flag gone, storage still set, read once.
    expect(authState.consumeJustSignedOut()).toBe(true)
    expect(authState.consumeJustSignedOut()).toBe(false)
  })
})

describe('AuthState.consumeJustSignedOut', () => {
  it('reads and clears the flag logout() sets, once per page', async () => {
    vi.mocked(logout).mockResolvedValue(null)
    await authState.logout()

    // This page: the in-memory flag.
    expect(authState.consumeJustSignedOut()).toBe(true)
    expect(authState.justSignedOut).toBe(false)
    // The reloaded page: the storage key, then nothing.
    expect(authState.consumeJustSignedOut()).toBe(true)
    expect(authState.consumeJustSignedOut()).toBe(false)
  })

  it('is false when nobody signed out (a plain page load)', () => {
    expect(authState.consumeJustSignedOut()).toBe(false)
  })
})

describe('AuthState.handleUnauthorized', () => {
  it('bounces an authenticated session to unauthenticated and clears identity', () => {
    authState.state = 'authenticated'
    authState.username = 'tom'
    authState.role = 'admin'

    authState.handleUnauthorized()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.username).toBe('')
    expect(authState.role).toBe('')
  })

  it('leaves non-authenticated states alone', () => {
    authState.state = 'setup-required'

    authState.handleUnauthorized()

    expect(authState.state).toBe('setup-required')
  })
})

// #1083: signing out (or a 401 bounce) must not leave the previous
// account's app/flags/watchlist state visible to the next person who
// signs in on this tab. Populates each store, signs out, and asserts
// every field named in the issue -- events, filters, devices, stats,
// the flags list/pins and the watchlist -- is back to empty.
function fixtureDevice(): Device {
  return {
    id: 'core',
    name: 'core',
    sourceIp: '10.0.0.1',
    configured: true,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    eventCount: 1,
    status: 'live',
  }
}

function fixtureStats(): Stats {
  return {
    total: 1,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 0,
    capacity: 100,
    count: 1,
    windowSeconds: 60,
    oldestHeld: null,
    connectedClients: 1,
  }
}

function fixtureFlag(): Flag {
  return {
    id: 'f1',
    type: 'port_scan',
    target: '10.0.0.5',
    detail: '',
    count: 1,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    cleared: false,
  }
}

function fixtureWatchlistEntry(): WatchlistEntry {
  return { id: 'w1', name: 'watch w1', enabled: true, createdAt: '2026-01-01T00:00:00Z' }
}

function fixtureApiToken(): ApiToken {
  return {
    id: 't1',
    name: 'router-a',
    kind: 'ingest',
    device: 'core',
    createdAt: '2026-01-01T00:00:00Z',
    value: 'ingest-token-live-value-the-next-admin-must-not-see',
  }
}

function fixtureUser(): UserSummary {
  return {
    id: 'u1',
    username: 'carol',
    role: 'user',
    createdAt: '2026-01-01T00:00:00Z',
    hasLocalPassword: true,
    sso: false,
  }
}

function fixtureAuditEntry(): AuditEntry {
  return {
    id: 1,
    timestamp: '2026-01-01T00:00:00Z',
    actor: 'tom',
    action: 'user.create',
    target: 'carol',
  }
}

describe('AuthState.logout clears the previous session state (#1083)', () => {
  beforeEach(() => {
    vi.mocked(logout).mockResolvedValue(null)
    authState.state = 'authenticated'

    appState.events = [
      { id: 1, time: '', deviceId: 'core', sourceIp: '10.0.0.1', action: 'accept', ruleLabel: 'r', chain: 'forward', raw: 'raw', receivedAt: 0 },
    ]
    appState.filters = { ...emptyFilters(), rule: 'stale-user-query' }
    appState.devices = [fixtureDevice()]
    appState.stats = fixtureStats()
    appState.initialLoadDone = true
    appState.fetchFailed = true
    appState.paused = true
    appState.pausedAt = 111
    appState.wipedAt = 222
    appState.autoscroll = false
    appState.pendingCount = 3

    flagsState.list = [fixtureFlag()]
    flagsState.timeSeries = [{ time: '2026-01-01T00:00:00Z', byType: {} }]
    flagsState.loaded = true
    flagsState.baselinesWarming = true
    flagsState.pin('f1')

    watchlistState.entries = [fixtureWatchlistEntry()]
    watchlistState.coverage = { w1: 'no-logging' }
    watchlistState.loaded = true
  })

  it('resets appState to its initial values', async () => {
    await authState.logout()

    expect(appState.events).toEqual([])
    expect(appState.filters).toEqual(emptyFilters())
    expect(appState.devices).toEqual([])
    expect(appState.stats).toBeNull()
    expect(appState.initialLoadDone).toBe(false)
    expect(appState.fetchFailed).toBe(false)
    expect(appState.paused).toBe(false)
    expect(appState.pausedAt).toBeNull()
    expect(appState.wipedAt).toBeNull()
    expect(appState.autoscroll).toBe(true)
    expect(appState.pendingCount).toBe(0)
  })

  it('clears flagsState via its existing public API (no reset() of its own)', async () => {
    await authState.logout()

    expect(flagsState.list).toEqual([])
    expect(flagsState.timeSeries).toEqual([])
    expect(flagsState.loaded).toBe(false)
    expect(flagsState.baselinesWarming).toBeUndefined()
    expect(flagsState.pinnedIds).toEqual([])
  })

  // The Security stage of the v0.6.0 pre-release audit: wizardState was
  // never added to #1083's batch, though it is a module-level singleton
  // exactly like the three above. It carries more than a view position
  // -- `token` is a router ingest token, minted for the previous
  // operator and handed straight to whoever signs in next on this tab.
  it('resets wizardState, which carries a router ingest token', async () => {
    wizardState.open = true
    wizardState.pane = 4
    wizardState.token = 'ingest-token-the-next-operator-must-not-be-handed'
    wizardState.tokenDevice = 'core'
    wizardState.devices = [fixtureDevice()]
    wizardState.address = '10.0.0.9'
    wizardState.backups = {
      enabled: true,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: { state: 'open' },
    } as unknown as RouterBackupsResponse

    await authState.logout()

    expect(wizardState.token).toBe('')
    expect(wizardState.tokenDevice).toBe('')
    expect(wizardState.backups).toBeNull()
    expect(wizardState.devices).toEqual([])
    expect(wizardState.address).toBe('')
    expect(wizardState.open).toBe(false)
    expect(wizardState.pane).toBe(1)
  })

  // The ingest token is the sharp one. This release moved it from
  // component-local state, which died with the component at logout, to
  // the wizardState singleton so a step revisit kept it -- so the next
  // admin to sign in on this tab was shown, and could copy, a live
  // token minted for someone else, with the mint gate skipped because
  // a token was already "held".
  it('clears the wizard ingest token, so the next admin is not handed it', async () => {
    wizardState.token = 'ingest-token-minted-for-the-previous-admin'
    wizardState.tokenDevice = 'core'

    await authState.logout()

    expect(wizardState.token).toBe('')
    expect(wizardState.tokenDevice).toBe('')
  })

  // A pasted `/export hide-sensitive` is the operator's whole firewall
  // configuration. Module-lifetime by design, so it survives a deck
  // scroll -- and, until now, a logout.
  it('clears a pasted router export from Log every rule', async () => {
    logEveryRuleWorkState.exportText = '/ip firewall filter add chain=forward comment="the previous operator rules"'
    logEveryRuleWorkState.exportDevice = 'core'

    await authState.logout()

    expect(logEveryRuleWorkState.exportText).toBe('')
  })

  // The history key decrypts this instance's stored events, flags,
  // definitions, watchlist and entities. The wizard clears it once its
  // step is past asking, but that only runs while the wizard is open.
  it('clears the minted history key, which the wizard alone would not', async () => {
    sessionStorage.setItem('mikroview-wizard-history-key', 'a-key-that-decrypts-the-stored-history')

    await authState.logout()

    expect(sessionStorage.getItem('mikroview-wizard-history-key')).toBeNull()
  })

  it('resets watchlistState to its initial values', async () => {
    await authState.logout()

    expect(watchlistState.entries).toEqual([])
    expect(watchlistState.coverage).toEqual({})
    expect(watchlistState.loaded).toBe(false)
  })

  // Security stage, same batch as wizardState/logEveryRuleWorkState
  // above: tokensState.justCreated is a raw, live API/ingest bearer
  // token -- EngineRoom's copy-once banner keeps rendering it after
  // logout, handing it to whoever signs in next on this tab.
  it('resets tokensState, so a raw bearer token is not handed to the next session', async () => {
    tokensState.list = [fixtureApiToken()]
    tokensState.justCreated = fixtureApiToken()

    await authState.logout()

    expect(tokensState.list).toEqual([])
    expect(tokensState.justCreated).toBeNull()
  })

  // usersState.list is the admin account list, rendered by EngineRoom
  // with no role guard of its own -- a non-admin signing in next on
  // this tab must not still see it.
  it('resets usersState, the admin-only account list', async () => {
    usersState.list = [fixtureUser()]

    await authState.logout()

    expect(usersState.list).toEqual([])
  })

  // auditState.loaded never reset on its own, so AuditLog.svelte kept
  // rendering the previous account's action log for whoever signed in
  // next.
  it('resets auditState, the admin-only action log', async () => {
    auditState.list = [fixtureAuditEntry()]
    auditState.hasMore = true
    auditState.loaded = true
    auditState.error = 'stale error from the previous session'

    await authState.logout()

    expect(auditState.list).toEqual([])
    expect(auditState.hasMore).toBe(false)
    expect(auditState.loaded).toBe(false)
    expect(auditState.error).toBeNull()
  })

  // persistenceState.loaded is private and never reset on its own, so
  // ensureLoaded() never fetched again once an admin had opened
  // DiskControl -- a non-admin signing in next on the same tab kept
  // seeing this admin-only info for the rest of the tab's life. loaded
  // is private, so this proves the guard cleared by calling
  // ensureLoaded() again and checking it actually fetches rather than
  // short-circuiting.
  it('resets persistenceState and re-fetches on the next ensureLoaded()', async () => {
    vi.mocked(fetchPersistence).mockResolvedValue({ backend: 'file', dir: '/data' })
    await persistenceState.ensureLoaded()
    expect(persistenceState.info).toEqual({ backend: 'file', dir: '/data' })
    expect(fetchPersistence).toHaveBeenCalledTimes(1)

    await authState.logout()

    expect(persistenceState.info).toBeNull()

    vi.mocked(fetchPersistence).mockResolvedValue({ backend: 'memory' })
    await persistenceState.ensureLoaded()
    expect(fetchPersistence).toHaveBeenCalledTimes(2)
    expect(persistenceState.info).toEqual({ backend: 'memory' })
  })

  // configProblemsState.loaded is private and never reset on its own,
  // so ConfigProblemBanner -- which has no role check of its own --
  // kept showing the previous admin's config diagnostics. Same
  // "prove the guard cleared" shape as persistenceState above, but
  // through the raw fetch() this store calls directly rather than
  // through lib/api.ts.
  it('resets configProblemsState and re-fetches on the next ensureLoaded()', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ problems: [{ code: 'clamped', key: 'x', message: 'y' }] }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    await configProblemsState.ensureLoaded()
    configProblemsState.dismissed = true
    expect(configProblemsState.problems).toHaveLength(1)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await authState.logout()

    expect(configProblemsState.problems).toEqual([])
    expect(configProblemsState.dismissed).toBe(false)

    await configProblemsState.ensureLoaded()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    vi.unstubAllGlobals()
  })

  // Quality stage of the same audit, a round later: configUpgradeState
  // is the sixth admin-only singleton of this shape and the one still
  // missing from the list above. It holds which settings the previous
  // admin's config.yaml does not set, and `loaded` true would let the
  // panel show that list before its own refresh answered.
  it('resets configUpgradeState', async () => {
    configUpgradeState.settings = [{ key: 'flags.new_detector', description: 'd', defaultValue: '1', versionAdded: 'v0.6.0' } as never]
    configUpgradeState.version = 'v0.6.0'
    configUpgradeState.loaded = true
    configUpgradeState.error = 'stale'

    await authState.logout()

    expect(configUpgradeState.settings).toEqual([])
    expect(configUpgradeState.version).toBe('')
    expect(configUpgradeState.loaded).toBe(false)
    expect(configUpgradeState.error).toBeNull()
  })
})

describe('AuthState.handleUnauthorized clears the previous session state (#1083)', () => {
  it('resets every store when it bounces an authenticated session', () => {
    authState.state = 'authenticated'
    appState.devices = [fixtureDevice()]
    appState.stats = fixtureStats()
    flagsState.list = [fixtureFlag()]
    flagsState.loaded = true
    watchlistState.entries = [fixtureWatchlistEntry()]
    watchlistState.loaded = true
    tokensState.list = [fixtureApiToken()]
    tokensState.justCreated = fixtureApiToken()

    authState.handleUnauthorized()

    expect(appState.devices).toEqual([])
    expect(appState.stats).toBeNull()
    expect(flagsState.list).toEqual([])
    expect(flagsState.loaded).toBe(false)
    expect(watchlistState.entries).toEqual([])
    expect(watchlistState.loaded).toBe(false)
    expect(tokensState.list).toEqual([])
    expect(tokensState.justCreated).toBeNull()
  })

  it('leaves every store untouched when the session was not authenticated', () => {
    authState.state = 'setup-required'
    appState.devices = [fixtureDevice()]
    flagsState.list = [fixtureFlag()]
    watchlistState.entries = [fixtureWatchlistEntry()]
    tokensState.list = [fixtureApiToken()]

    authState.handleUnauthorized()

    expect(appState.devices).toEqual([fixtureDevice()])
    expect(flagsState.list).toEqual([fixtureFlag()])
    expect(watchlistState.entries).toEqual([fixtureWatchlistEntry()])
    expect(tokensState.list).toEqual([fixtureApiToken()])
  })
})

describe('AuthState.consumeSSOErrorFromURL', () => {
  it('sets a generic error message and strips ssoError from the URL', () => {
    window.history.replaceState(null, '', '/?ssoError=provider_denied&foo=bar')

    authState.consumeSSOErrorFromURL()

    expect(authState.ssoError).toBe('SSO sign-in failed -- try again, or sign in with your password below.')
    expect(location.search).toBe('?foo=bar')
  })

  it('does nothing when there is no ssoError param', () => {
    window.history.replaceState(null, '', '/?foo=bar')

    authState.consumeSSOErrorFromURL()

    expect(authState.ssoError).toBeNull()
    expect(location.search).toBe('?foo=bar')
  })
})

// #653's three tiers (admin ⊇ user ⊇ viewer): isAdmin/canEdit are the
// two derived checks every control-gating call site reads instead of
// comparing authState.role directly.
describe('AuthState.isAdmin / canEdit', () => {
  it('admin is both', () => {
    authState.role = 'admin'
    expect(authState.isAdmin).toBe(true)
    expect(authState.canEdit).toBe(true)
  })

  it('user can edit but is not admin', () => {
    authState.role = 'user'
    expect(authState.isAdmin).toBe(false)
    expect(authState.canEdit).toBe(true)
  })

  it('viewer is neither', () => {
    authState.role = 'viewer'
    expect(authState.isAdmin).toBe(false)
    expect(authState.canEdit).toBe(false)
  })

  it('an empty (unknown/signed-out) role is treated as the lowest tier', () => {
    authState.role = ''
    expect(authState.isAdmin).toBe(false)
    expect(authState.canEdit).toBe(false)
  })
})


// #1251: an account an admin has reset signs in with a one-time code.
// The session is real but the app is not open -- the server 403s
// everything but the change-password route -- so AuthState has to land
// in its own view state rather than 'authenticated', or App.svelte would
// draw an app of failed requests.
describe('AuthState and a forced password change (#1251)', () => {
  const cases: {
    name: string
    mustChangePassword: boolean | undefined
    want: string
  }[] = [
    { name: 'a reset is outstanding', mustChangePassword: true, want: 'must-change-password' },
    { name: 'no reset is outstanding', mustChangePassword: false, want: 'authenticated' },
    {
      name: 'an older server that does not report the flag at all',
      mustChangePassword: undefined,
      want: 'authenticated',
    },
  ]

  for (const c of cases) {
    it(`lands in ${c.want} when ${c.name}`, async () => {
      vi.mocked(fetchAuthSession).mockResolvedValue(
        session({
          authenticated: true,
          username: 'bilbo',
          role: 'user',
          mustChangePassword: c.mustChangePassword,
        }),
      )

      await authState.check()

      expect(authState.state).toBe(c.want)
      expect(authState.mustChangePassword).toBe(c.mustChangePassword ?? false)
    })
  }

  it('opens the app once the new password is set', async () => {
    vi.mocked(setNewPasswordAfterReset).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'bilbo', role: 'user', mustChangePassword: false }),
    )

    const err = await authState.setNewPassword('new-password-placeholder')

    expect(err).toBeNull()
    expect(setNewPasswordAfterReset).toHaveBeenCalledWith('new-password-placeholder')
    expect(authState.state).toBe('authenticated')
    expect(authState.mustChangePassword).toBe(false)
  })

  it('surfaces a refusal and stays on the set-a-password screen', async () => {
    authState.state = 'must-change-password'
    authState.mustChangePassword = true
    vi.mocked(setNewPasswordAfterReset).mockResolvedValue('password must be at least 8 characters')

    const err = await authState.setNewPassword('short')

    expect(err).toBe('password must be at least 8 characters')
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('must-change-password')
  })

  it('drops the flag when a mid-flight request comes back 401', () => {
    authState.state = 'must-change-password'
    authState.mustChangePassword = true

    authState.handleUnauthorized()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.mustChangePassword).toBe(false)
  })
})
