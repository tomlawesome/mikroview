// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthSession } from './types'

// #1362: holds whatever auth.svelte.ts registers via api.ts's
// onForcedAuthGate -- vi.hoisted because the vi.mock factory below runs
// before an ordinary module-scope `let` would be initialised.
const { getCapturedForcedAuthGateHandler, setCapturedForcedAuthGateHandler } = vi.hoisted(() => {
  let handler: ((gate: string, session: AuthSession) => void) | null = null
  return {
    getCapturedForcedAuthGateHandler: () => handler,
    setCapturedForcedAuthGateHandler: (h: (gate: string, session: AuthSession) => void) => {
      handler = h
    },
  }
})

// auth.svelte.ts talks to the backend exclusively through these
// lib/api.ts functions -- mock the whole module so tests exercise only
// AuthState's own state-transition logic, never a real fetch().
vi.mock('./api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  // #1362: auth.svelte.ts registers its forced-auth-gate handler at
  // module load -- capturing it here (rather than a bare vi.fn()) is
  // what lets the tests below invoke it directly, the same way api.ts's
  // shared fetch wrapper would once it detects the gate header.
  onForcedAuthGate: vi.fn((handler: (gate: string, session: AuthSession) => void) => {
    setCapturedForcedAuthGateHandler(handler)
  }),
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
import { authState, pageReload, wireForcedAuthGate } from './auth.svelte'
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

// #1362: App.svelte is what calls this in real use -- called once here,
// directly, so the tests further down can drive the handler it
// registers (captured by the mocked onForcedAuthGate above) the same
// way api.ts's shared fetch wrapper would.
wireForcedAuthGate()

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
  authState.mustEnrolPasskey = false
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

// #1202 (spec 32): gauntlet's third door. An admin without a passkey, under
// the admin passkey rule, is held at `must-enrol-passkey` until one is
// registered. GET /api/auth/session says so with `mustEnrolPasskey`, and a 403
// carrying `X-Auth-Gate: must-enrol-passkey` (api.ts) reaches here through the
// onForcedAuthGate handler. gauntlet's errors.md orders the doors: password
// change first, then this one, then the second-factor door.
describe('AuthState and the must-enrol-passkey door (#1202)', () => {
  const cases: {
    name: string
    mustEnrolPasskey: boolean | undefined
    want: string
  }[] = [
    { name: 'the admin holds no passkey under the rule', mustEnrolPasskey: true, want: 'must-enrol-passkey' },
    { name: 'the admin holds a passkey', mustEnrolPasskey: false, want: 'authenticated' },
    { name: 'an older server does not report the field at all', mustEnrolPasskey: undefined, want: 'authenticated' },
  ]

  for (const c of cases) {
    it(`lands in ${c.want} when ${c.name}`, async () => {
      vi.mocked(fetchAuthSession).mockResolvedValue(
        session({ authenticated: true, username: 'alex', role: 'admin', mustEnrolPasskey: c.mustEnrolPasskey }),
      )

      await authState.check()

      expect(authState.state).toBe(c.want)
      expect(authState.mustEnrolPasskey).toBe(c.mustEnrolPasskey ?? false)
    })
  }

  it('a page reload while held lands back on the door, not the login form', async () => {
    authState.state = 'loading'
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'alex', role: 'admin', mustEnrolPasskey: true, adminPasskeyRequired: true }),
    )

    await authState.check()

    expect(authState.state).toBe('must-enrol-passkey')
  })

  it('an admin with no factor at all is sent to the passkey door, not the any-factor door', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({
        authenticated: true,
        username: 'alex',
        role: 'admin',
        mustEnrolPasskey: true,
        mustEnrolSecondFactor: true,
      }),
    )

    await authState.check()

    expect(authState.state).toBe('must-enrol-passkey')
  })

  it('the any-factor door still holds a user with no factor (the passkey door never applies to them)', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({
        authenticated: true,
        username: 'kai',
        role: 'user',
        mustEnrolPasskey: false,
        mustEnrolSecondFactor: true,
      }),
    )

    await authState.check()

    expect(authState.state).toBe('must-enrol-factor')
  })

  it('an outstanding password change outranks the passkey door', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({
        authenticated: true,
        username: 'alex',
        role: 'admin',
        mustChangePassword: true,
        mustEnrolPasskey: true,
      }),
    )

    await authState.check()

    expect(authState.state).toBe('must-change-password')
  })

  it('opens the app once a re-check reports the passkey is held', async () => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolPasskey = true
    vi.mocked(fetchAuthSession).mockResolvedValue(
      session({ authenticated: true, username: 'alex', role: 'admin', mustEnrolPasskey: false }),
    )

    await authState.check()

    expect(authState.state).toBe('authenticated')
    expect(authState.mustEnrolPasskey).toBe(false)
  })

  it('drops the flag when a mid-enrolment request comes back 401', () => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolPasskey = true

    authState.handleUnauthorized()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.mustEnrolPasskey).toBe(false)
    expect(pageReload.now).toHaveBeenCalled()
  })

  it('moves an authenticated session to must-enrol-passkey when the forced-gate handler fires', () => {
    authState.state = 'authenticated'
    authState.username = 'alex'
    authState.role = 'admin'

    const handler = getCapturedForcedAuthGateHandler()
    expect(handler).not.toBeNull()
    handler?.(
      'must-enrol-passkey',
      session({ authenticated: true, username: 'alex', role: 'admin', mustEnrolPasskey: true }),
    )

    expect(authState.state).toBe('must-enrol-passkey')
    expect(authState.mustEnrolPasskey).toBe(true)
  })

  it('trusts the fresh session over the gate name -- a passkey registered from another tab wins', () => {
    authState.state = 'authenticated'
    authState.username = 'alex'
    authState.role = 'admin'

    const handler = getCapturedForcedAuthGateHandler()
    handler?.(
      'must-enrol-passkey',
      session({ authenticated: true, username: 'alex', role: 'admin', mustEnrolPasskey: false }),
    )

    expect(authState.state).toBe('authenticated')
  })
})
