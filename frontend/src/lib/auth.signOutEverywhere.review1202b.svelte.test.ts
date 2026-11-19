// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U9 (note_34147), the store half: authState
// .signOutEverywhere(password?) hands the caller's own password to api.ts and
// keeps no copy. A refusal (wrong password, rate limit) comes back as text and
// the session is not re-read. Written before the fix.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthSession } from './types'

vi.mock('./api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
  PENDING_LOGIN_EXPIRED: 'sign in again',
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  onForcedAuthGate: vi.fn(),
  register: vi.fn(),
  setNewPasswordAfterReset: vi.fn(),
  signOutEverywhere: vi.fn(),
  submitLoginFactor: vi.fn(),
  fetchPersistence: vi.fn(),
  fetchMyPreferences: vi.fn(),
  saveMyPreferences: vi.fn(),
}))

vi.mock('./passkeys.svelte', () => ({
  loginWithPasskey: vi.fn(),
}))

import { fetchAuthSession, fetchMyPreferences, saveMyPreferences, signOutEverywhere } from './api'
import { authState } from './auth.svelte'
import { preferencesState } from './preferences.svelte'

function session(overrides: Partial<AuthSession> = {}): AuthSession {
  return { setupRequired: false, authenticated: true, username: 'alex', role: 'admin', ssoAvailable: false, ...overrides }
}

beforeEach(() => {
  vi.resetAllMocks()
  authState.state = 'authenticated'
  authState.username = 'alex'
  authState.role = 'admin'
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
  vi.mocked(fetchAuthSession).mockResolvedValue(session({ signedInSince: '2026-10-10T00:00:00Z' }))
})

// A $state class keeps its fields behind private #fields, so
// JSON.stringify(instance) is always '{}'. Read every own and prototype
// property (getters included) so a retained value would show up.
function visibleState(obj: object): string {
  const out: Record<string, unknown> = {}
  for (let o: object | null = obj; o && o !== Object.prototype; o = Object.getPrototypeOf(o)) {
    for (const name of Object.getOwnPropertyNames(o)) {
      if (name === 'constructor' || name in out) continue
      try {
        const v = (obj as Record<string, unknown>)[name]
        if (typeof v !== 'function') out[name] = v
      } catch {
        /* a getter that throws holds nothing readable */
      }
    }
  }
  return JSON.stringify(out)
}

describe('authState.signOutEverywhere passes the password on (#1202 review U9)', () => {
  // #1202 review U9
  it('signOutEverywhere(password) calls the api with that password, then re-reads the session', async () => {
    vi.mocked(signOutEverywhere).mockResolvedValue(null)

    expect(await authState.signOutEverywhere('hunter2')).toBeNull()

    expect(signOutEverywhere).toHaveBeenCalledTimes(1)
    expect(signOutEverywhere).toHaveBeenCalledWith('hunter2')
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
  })

  // #1202 review U9: pair -- an SSO-only account sends no password.
  it('signOutEverywhere() with no password calls the api with none', async () => {
    vi.mocked(signOutEverywhere).mockResolvedValue(null)

    expect(await authState.signOutEverywhere()).toBeNull()

    expect(vi.mocked(signOutEverywhere).mock.calls[0][0]).toBeFalsy()
  })

  // #1202 review U9
  it.each(['incorrect password', 'too many attempts, try again later'])(
    'a refusal "%s" is returned as text, the session is not re-read and the person stays signed in',
    async (reason) => {
      vi.mocked(signOutEverywhere).mockResolvedValue(reason)

      expect(await authState.signOutEverywhere('wrong')).toBe(reason)

      expect(signOutEverywhere).toHaveBeenCalledWith('wrong')
      expect(fetchAuthSession).not.toHaveBeenCalled()
      expect(authState.state).toBe('authenticated')
    },
  )

  // #1202 review U9
  it('keeps no copy of the password on the store', async () => {
    vi.mocked(signOutEverywhere).mockResolvedValue(null)
    await authState.signOutEverywhere('sw0rdfish-secret')
    expect(visibleState(authState)).not.toContain('sw0rdfish-secret')
    // The check must be able to see a field: the store has a 'state' field.
    expect(visibleState(authState)).toContain('state')

    vi.mocked(signOutEverywhere).mockResolvedValue('incorrect password')
    await authState.signOutEverywhere('sw0rdfish-secret')
    expect(visibleState(authState)).not.toContain('sw0rdfish-secret')
  })
})
