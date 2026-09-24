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
  submitLoginFactor: vi.fn(),
  // The one non-function export this module reaches for -- kept as the
  // real literal (rather than importOriginal's whole module) since
  // everything else here is deliberately a bare vi.fn() stub.
  PENDING_LOGIN_EXPIRED: 'sign in again',
  fetchPersistence: vi.fn(),
  // #1283: preferences.svelte.ts (imported transitively through
  // clearSessionState's preferencesState.reset(), and through apply()'s
  // preferencesState.ensureLoaded()) talks to the backend only through
  // these two -- mocked here for the same reason as every other api.ts
  // call this file already stubs.
  fetchMyPreferences: vi.fn(),
  saveMyPreferences: vi.fn(),
}))

// #1250: authState.loginWithPasskey() delegates the actual ceremony to
// lib/passkeys.svelte.ts -- mocked here for the same reason every other
// api.ts-adjacent call this file stubs is: this file exercises AuthState's
// own wiring, never the browser boundary underneath it (that boundary has
// its own test file, lib/passkeys.svelte.test.ts).
vi.mock('./passkeys.svelte', () => ({
  loginWithPasskey: vi.fn(),
}))

import {
  fetchAuthSession,
  login,
  logout,
  register,
  setNewPasswordAfterReset,
  signOutEverywhere,
  submitLoginFactor,
  fetchPersistence,
  fetchMyPreferences,
  saveMyPreferences,
} from './api'
import { loginWithPasskey } from './passkeys.svelte'
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
import { preferencesState } from './preferences.svelte'
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
  authState.signInTimedOut = false
  authState.justSignedOut = false
  authState.signedInSince = ''
  authState.mustChangePassword = false
  authState.mustEnrolSecondFactor = false
  authState.hasTOTP = false
  authState.passkeyCount = 0
  authState.passkeyStatus = 'unset'
  authState.passkeyOrigin = undefined
  authState.pendingSecondFactor = []
  authState.pendingPasskeyOrigin = undefined
  window.history.replaceState(null, '', '/')
  // jsdom cannot navigate; the reload tests below assert on this spy.
  sessionStorage.clear()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  // preferencesState is a module-level singleton too (#1283) -- reset
  // between tests for the same reason appState/flagsState/etc. already
  // are, and give the two calls apply()/logout() make a safe default so
  // a test that doesn't care about preferences at all isn't left with
  // an unhandled rejection from an unmocked resolution.
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
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

  // #1249: absent on an older server, read as false -- there is nothing
  // to be wrong about, since a server that predates the field cannot
  // have activated a factor either.
  it('carries hasTOTP through, defaulting to false when the server omits it', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'admin', hasTOTP: true }),
    )
    await authState.check()
    expect(authState.hasTOTP).toBe(true)

    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'tom', role: 'admin' }))
    await authState.check()
    expect(authState.hasTOTP).toBe(false)
  })

  // #1250: mirrors the session's own passkeys summary, same "absent on
  // an older server reads as none of them" reasoning as hasTOTP above.
  it('carries the passkeys summary through, defaulting to none/unset when the server omits it', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({
        authenticated: true,
        username: 'tom',
        role: 'admin',
        passkeys: { count: 2, status: 'ready', origin: 'https://mikroview.example.org' },
      }),
    )
    await authState.check()
    expect(authState.passkeyCount).toBe(2)
    expect(authState.passkeyStatus).toBe('ready')
    expect(authState.passkeyOrigin).toBe('https://mikroview.example.org')

    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'tom', role: 'admin' }))
    await authState.check()
    expect(authState.passkeyCount).toBe(0)
    expect(authState.passkeyStatus).toBe('unset')
    expect(authState.passkeyOrigin).toBeUndefined()
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

// #1283: preferences load from the server once, on the transition into
// the real 'authenticated' view -- not on every check(), and not for a
// forced password change, which cannot reach the app to use them.
describe('AuthState wires preferencesState to sign-in (#1283)', () => {
  it('loads preferences once the session is authenticated', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'admin' }),
    )
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { colorway: 'pulse' } })

    await authState.check()
    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
    expect(preferencesState.get('colorway')).toBe('pulse')
  })

  it('does not load preferences for a forced password change -- that session cannot reach the app', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'bilbo', role: 'user', mustChangePassword: true }),
    )

    await authState.check()

    expect(authState.state).toBe('must-change-password')
    expect(fetchMyPreferences).not.toHaveBeenCalled()
  })

  it('does not re-fetch on a second check() once already loaded', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'tom', role: 'admin' }))

    await authState.check()
    await preferencesState.ensureLoaded()
    await authState.check()
    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
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

  // #1249: a correct password on an account holding a factor lands here
  // instead of re-checking the session -- there isn't one yet, only the
  // pending cookie the server set.
  it('moves to pending-factor without re-checking when the account holds a factor', async () => {
    vi.mocked(login).mockResolvedValue({ secondFactor: ['totp'] })

    const result = await authState.login('tom', 'hunter2')

    expect(result).toBeNull()
    expect(authState.state).toBe('pending-factor')
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })

  // #1250: carries the pending login's own factor list and passkey
  // origin -- AuthScreen reads these directly to decide which of the
  // three ways in to offer.
  it('carries the pending secondFactor list and passkeyOrigin through', async () => {
    vi.mocked(login).mockResolvedValue({
      secondFactor: ['passkey', 'totp'],
      passkeyOrigin: 'https://mikroview.example.org',
    })

    await authState.login('tom', 'hunter2')

    expect(authState.pendingSecondFactor).toEqual(['passkey', 'totp'])
    expect(authState.pendingPasskeyOrigin).toBe('https://mikroview.example.org')
  })

  // #1250: an empty list is still pending -- an account whose only
  // factor is a stale passkey never signs in on the password alone.
  it('moves to pending-factor on an empty secondFactor list too', async () => {
    vi.mocked(login).mockResolvedValue({ secondFactor: [] })

    const result = await authState.login('tom', 'hunter2')

    expect(result).toBeNull()
    expect(authState.state).toBe('pending-factor')
    expect(authState.pendingSecondFactor).toEqual([])
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })
})

describe('AuthState.loginWithPasskey', () => {
  it('re-checks the session and returns null on success', async () => {
    vi.mocked(loginWithPasskey).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'user' }),
    )

    const result = await authState.loginWithPasskey()

    expect(result).toBeNull()
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
  })

  it('returns the error and does not re-check on a refused ceremony', async () => {
    vi.mocked(loginWithPasskey).mockResolvedValue("that passkey couldn't be verified")

    const result = await authState.loginWithPasskey()

    expect(result).toBe("that passkey couldn't be verified")
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })

  // The 5-minute pending-login cookie can outlive the passkey prompt too
  // -- same fallback as submitFactor's own case below.
  it('falls back to the password form on a pending-login timeout, instead of an inline error', async () => {
    authState.state = 'pending-factor'
    authState.pendingSecondFactor = ['passkey']
    authState.pendingPasskeyOrigin = location.origin
    vi.mocked(loginWithPasskey).mockResolvedValue('sign in again\n')

    const result = await authState.loginWithPasskey()

    expect(result).toBeNull()
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('unauthenticated')
    expect(authState.signInTimedOut).toBe(true)
    expect(authState.pendingSecondFactor).toEqual([])
    expect(authState.pendingPasskeyOrigin).toBeUndefined()
  })
})

describe('AuthState.submitFactor', () => {
  it('re-checks the session and returns null on success', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'tom', role: 'user', hasTOTP: true }),
    )

    const result = await authState.submitFactor('123456')

    expect(result).toBeNull()
    expect(submitLoginFactor).toHaveBeenCalledWith('123456')
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
    expect(authState.hasTOTP).toBe(true)
  })

  it('returns the error and does not re-check on a wrong code', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue('invalid code')

    const result = await authState.submitFactor('000000')

    expect(result).toBe('invalid code')
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })

  // A recovery code goes through the same box and the same call -- the
  // server is what tells the two apart, not this method.
  it('accepts a recovery code through the same call', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(session({ authenticated: true, username: 'tom', role: 'user' }))

    await authState.submitFactor('a1b2-c3d4-e5f6-g7h8')

    expect(submitLoginFactor).toHaveBeenCalledWith('a1b2-c3d4-e5f6-g7h8')
  })

  // #1249's pending-login cookie lasts 5 minutes -- past that, the
  // server answers every one of code/recovery/passkey with the same
  // "sign in again" 401 (handleAuthLoginFactor), which is not an
  // ordinary wrong code for the box to show inline: there is no session
  // left for any code to complete. This falls back to the password form
  // instead of leaving the code box up with nothing that can ever work.
  it('falls back to the password form on a pending-login timeout, instead of an inline error', async () => {
    authState.state = 'pending-factor'
    authState.pendingSecondFactor = ['totp']
    // The body exactly as the server sends it: Go's http.Error ends it
    // with a newline.
    vi.mocked(submitLoginFactor).mockResolvedValue('sign in again\n')

    const result = await authState.submitFactor('123456')

    expect(result).toBeNull()
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('unauthenticated')
    expect(authState.signInTimedOut).toBe(true)
    expect(authState.pendingSecondFactor).toEqual([])
  })

  // "invalid code" is a different 401 from the very same route -- an
  // ordinary wrong guess, not an expired cookie -- and must stay on the
  // code box exactly as it did before.
  it('does not bounce to the password form on an ordinary wrong code', async () => {
    authState.state = 'pending-factor'
    vi.mocked(submitLoginFactor).mockResolvedValue('invalid code')

    const result = await authState.submitFactor('000000')

    expect(result).toBe('invalid code')
    expect(authState.state).toBe('pending-factor')
    expect(authState.signInTimedOut).toBe(false)
  })

  // The timed-out note belongs to the attempt that timed out: signing in
  // again takes it away, rather than leaving it beside a fresh code box.
  it('drops the timed-out note once the next sign-in starts', async () => {
    authState.signInTimedOut = true
    vi.mocked(login).mockResolvedValue({ secondFactor: ['totp'] } as never)

    await authState.login('admin', 'right-password')

    expect(authState.state).toBe('pending-factor')
    expect(authState.signInTimedOut).toBe(false)
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

  // #1283: a debounced write still sitting inside its 500ms window must
  // reach the server before the session it depends on is gone --
  // flush() has to run, and it has to run before the server logout
  // call, not after.
  it('flushes any pending preference write before ending the session', async () => {
    authState.state = 'authenticated'
    vi.mocked(logout).mockResolvedValue(null)
    preferencesState.seedForTest({})
    preferencesState.set('colorway', 'nebula')

    const order: string[] = []
    vi.mocked(saveMyPreferences).mockImplementation(async () => {
      order.push('saveMyPreferences')
      return null
    })
    vi.mocked(logout).mockImplementation(async () => {
      order.push('logout')
      return null
    })

    await authState.logout()

    expect(order).toEqual(['saveMyPreferences', 'logout'])
  })
})

// #1253: AuthenticatorOverlay/PasskeysOverlay call this when
// disableTOTP/disablePasskey answers signedOut -- the server has
// already revoked every session on the account (removing its last
// second factor), so unlike logout() above this makes no server call of
// its own.
describe('AuthState.signOutAfterFactorRemoved', () => {
  it('clears identity and reloads without calling the server logout route', async () => {
    authState.state = 'authenticated'
    authState.username = 'tom'
    authState.role = 'user'
    authState.hasTOTP = true

    await authState.signOutAfterFactorRemoved()

    expect(logout).not.toHaveBeenCalled()
    expect(authState.state).toBe('unauthenticated')
    expect(authState.username).toBe('')
    expect(authState.role).toBe('')
    expect(pageReload.now).toHaveBeenCalled()
    // Plays the door's way-out beat, the same as an ordinary logout()
    // -- this is the caller's own action ending their session, not a
    // forced expiry (handleUnauthorized deliberately leaves this alone).
    expect(authState.justSignedOut).toBe(true)
  })

  it('flushes any pending preference write first, the same as logout()', async () => {
    authState.state = 'authenticated'
    preferencesState.seedForTest({})
    preferencesState.set('colorway', 'nebula')
    vi.mocked(saveMyPreferences).mockResolvedValue(null)

    await authState.signOutAfterFactorRemoved()

    expect(saveMyPreferences).toHaveBeenCalled()
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

  // #1283: preferencesState is the seventh module-lifetime singleton in
  // this batch. logout() flushes it first (its own test, above); this
  // is the "dropped from memory" half -- the next sign-in on this tab
  // must call ensureLoaded() again rather than see the previous
  // account's cached record.
  it('drops preferencesState from memory and re-fetches on the next ensureLoaded()', async () => {
    preferencesState.seedForTest({ colorway: 'nebula' })
    expect(preferencesState.get('colorway')).toBe('nebula')

    await authState.logout()

    expect(preferencesState.get('colorway')).toBeUndefined()

    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { colorway: 'frequency' } })
    await preferencesState.ensureLoaded()
    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
    expect(preferencesState.get('colorway')).toBe('frequency')
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
    preferencesState.seedForTest({ colorway: 'nebula' })

    authState.handleUnauthorized()

    expect(appState.devices).toEqual([])
    expect(appState.stats).toBeNull()
    expect(flagsState.list).toEqual([])
    expect(flagsState.loaded).toBe(false)
    expect(watchlistState.entries).toEqual([])
    expect(watchlistState.loaded).toBe(false)
    expect(tokensState.list).toEqual([])
    expect(tokensState.justCreated).toBeNull()
    expect(preferencesState.get('colorway')).toBeUndefined()
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


// #1336 (#1253's door): a local account with no second factor holds a
// real session that may reach nothing but the four enrolment routes.
// Unlike 'pending-factor', this state comes straight from
// GET /api/auth/session -- the flag is computed from the account, not
// the session -- so a plain check() (app boot, page reload
// mid-enrolment) lands on the door and stays there until a factor is
// proven.
describe('AuthState and the forced-enrolment door (#1336)', () => {
  const cases: {
    name: string
    mustEnrolSecondFactor: boolean | undefined
    want: string
  }[] = [
    { name: 'the account holds no second factor', mustEnrolSecondFactor: true, want: 'must-enrol-factor' },
    { name: 'the account holds a factor', mustEnrolSecondFactor: false, want: 'authenticated' },
    {
      name: 'an older server does not report the flag at all',
      mustEnrolSecondFactor: undefined,
      want: 'authenticated',
    },
  ]

  for (const c of cases) {
    it(`lands in ${c.want} when ${c.name}`, async () => {
      vi.mocked(fetchAuthSession).mockResolvedValue(
        session({
          authenticated: true,
          username: 'meredith',
          role: 'viewer',
          mustEnrolSecondFactor: c.mustEnrolSecondFactor,
        }),
      )

      await authState.check()

      expect(authState.state).toBe(c.want)
      expect(authState.mustEnrolSecondFactor).toBe(c.mustEnrolSecondFactor ?? false)
    })
  }

  it('a page reload mid-enrolment lands back on the door, not the login form', async () => {
    // The state a reload starts from is 'loading' with the flag long
    // gone from memory -- everything the door needs must come from the
    // session response alone.
    authState.state = 'loading'
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'meredith', role: 'viewer', mustEnrolSecondFactor: true }),
    )

    await authState.check()

    expect(authState.state).toBe('must-enrol-factor')
  })

  it('an outstanding password change outranks the door, matching requireAuth\'s own gate order', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({
        authenticated: true,
        username: 'meredith',
        role: 'viewer',
        mustChangePassword: true,
        mustEnrolSecondFactor: true,
      }),
    )

    await authState.check()

    expect(authState.state).toBe('must-change-password')
    // Both flags are still mirrored -- the door is what comes next once
    // the password is set.
    expect(authState.mustEnrolSecondFactor).toBe(true)
  })

  it('opens the app once a re-check reports the factor is held', async () => {
    authState.state = 'must-enrol-factor'
    authState.mustEnrolSecondFactor = true
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'meredith', role: 'viewer', mustEnrolSecondFactor: false }),
    )

    await authState.check()

    expect(authState.state).toBe('authenticated')
    expect(authState.mustEnrolSecondFactor).toBe(false)
  })

  it('drops the flag when a mid-enrolment request comes back 401', () => {
    authState.state = 'must-enrol-factor'
    authState.mustEnrolSecondFactor = true

    authState.handleUnauthorized()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.mustEnrolSecondFactor).toBe(false)
    expect(pageReload.now).toHaveBeenCalled()
  })
})
