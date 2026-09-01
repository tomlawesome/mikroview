// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

// Same approach as AuthLogin.svelte.test.ts: only the network boundary is
// faked, so this exercises the real AuthState logic, the real AuthScreen
// gate/form markup, and the real journeyState transition together.
vi.mock('../lib/api', () => ({
  createUser: vi.fn(),
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
}))

import { fetchAuthSession, register } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { journeyState } from '../lib/journey.svelte'
import AuthSetup from './AuthSetup.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  authState.state = 'setup-required'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  journeyState.phase = 'idle'
})

describe('AuthSetup', () => {
  // #645's own scope: a virgin instance shows the door's chrome with an
  // Enter button standing in for the login button, revealing the
  // unchanged account-creation form only once clicked.
  it('starts on the gate, not the form', () => {
    render(AuthSetup)
    expect(screen.getByRole('button', { name: /enter/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /create account/i })).toBeNull()
  })

  it('reveals the account-creation form after Enter', async () => {
    render(AuthSetup)
    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
    expect(screen.getByRole('button', { name: /create account/i })).toBeTruthy()
  })

  // #646's trigger: a successful register() -- and only that -- starts
  // the journey at its Attach beat.
  it('starts the journey once the admin account is actually created', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthSetup)
    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
    await fireEvent.input(screen.getByLabelText('account'), { target: { value: 'tom' } })
    await fireEvent.input(screen.getByLabelText('password'), { target: { value: 'hunter2222' } })
    await fireEvent.input(screen.getByLabelText('confirm password'), { target: { value: 'hunter2222' } })
    await fireEvent.click(screen.getByRole('button', { name: /create account/i }))

    // Three sequential awaits sit between the click and the journey
    // starting (authState.register -> its own check() -> fetchAuthSession),
    // one more hop than a plain login -- waitFor rather than assuming a
    // single fireEvent tick flushes all of them.
    await waitFor(() => expect(register).toHaveBeenCalledWith('tom', 'hunter2222'))
    await waitFor(() => expect(journeyState.phase).toBe('attach'))
  })

  it('never starts the journey when registration fails', async () => {
    vi.mocked(register).mockResolvedValue('username already taken')

    render(AuthSetup)
    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
    await fireEvent.input(screen.getByLabelText('account'), { target: { value: 'tom' } })
    await fireEvent.input(screen.getByLabelText('password'), { target: { value: 'hunter2222' } })
    await fireEvent.input(screen.getByLabelText('confirm password'), { target: { value: 'hunter2222' } })
    await fireEvent.click(screen.getByRole('button', { name: /create account/i }))

    expect(await screen.findByText('username already taken')).toBeTruthy()
    expect(journeyState.phase).toBe('idle')
  })
})
