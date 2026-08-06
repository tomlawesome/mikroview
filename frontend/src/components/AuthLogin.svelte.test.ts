import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

// AuthLogin is a thin wrapper: it renders AuthScreen and wires its
// onsubmit straight to authState.login. Mocking lib/api.ts (rather than
// auth.svelte.ts itself) means this test exercises the real AuthState
// logic and the real AuthScreen form markup -- only the network boundary
// is faked, same approach as auth.svelte.test.ts.
vi.mock('../lib/api', () => ({
  createUser: vi.fn(),
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  skipAuthSetup: vi.fn(),
}))

import { fetchAuthSession, login } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import AuthLogin from './AuthLogin.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  authState.state = 'loading'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  authState.ssoError = null
})

async function fillAndSubmit(username: string, password: string) {
  await fireEvent.input(screen.getByLabelText('Username'), { target: { value: username } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: password } })
  await fireEvent.click(screen.getByRole('button', { name: /sign in/i }))
}

describe('AuthLogin', () => {
  it('submits the entered credentials to authState.login', async () => {
    vi.mocked(login).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      authDisabled: false,
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthLogin)

    await fillAndSubmit('tom', 'hunter2')

    expect(login).toHaveBeenCalledWith('tom', 'hunter2')
    // A successful login re-checks the session, which is how the app
    // actually learns it's authenticated (see auth.svelte.ts's login()).
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
  })

  it('renders the error message returned by a failed login without navigating away', async () => {
    vi.mocked(login).mockResolvedValue('invalid username or password')

    render(AuthLogin)

    await fillAndSubmit('tom', 'wrong-password')

    expect(await screen.findByText('invalid username or password')).toBeTruthy()
    expect(fetchAuthSession).not.toHaveBeenCalled()
    // Still on the login screen -- the form's failure path never flips
    // authState.state, it only surfaces the error string.
    expect(screen.getByRole('button', { name: /sign in/i })).toBeTruthy()
  })

  it('shows the SSO link only when the backend reports SSO is configured', async () => {
    authState.ssoAvailable = true

    render(AuthLogin)

    const ssoLink = screen.getByRole('link', { name: /sign in with sso/i })
    expect(ssoLink.getAttribute('href')).toBe('/api/auth/oidc/login')
  })

  it('omits the SSO link when the backend has no SSO configured', () => {
    authState.ssoAvailable = false

    render(AuthLogin)

    expect(screen.queryByRole('link', { name: /sign in with sso/i })).toBeNull()
  })
})
