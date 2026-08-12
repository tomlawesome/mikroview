// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthSession } from './types'

// auth.svelte.ts talks to the backend exclusively through these five
// lib/api.ts functions -- mock the whole module so tests exercise only
// AuthState's own state-transition logic, never a real fetch().
vi.mock('./api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
}))

import { fetchAuthSession, login, logout, register } from './api'
import { authState } from './auth.svelte'

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
  authState.showUsers = false
  authState.ssoAvailable = false
  authState.ssoError = null
  window.history.replaceState(null, '', '/')
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
