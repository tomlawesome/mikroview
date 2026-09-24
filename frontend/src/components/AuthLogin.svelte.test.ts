// SPDX-License-Identifier: AGPL-3.0-only

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
  setNewPasswordAfterReset: vi.fn(),
  submitLoginFactor: vi.fn(),
  // #1250: authState.loginWithPasskey() runs the real
  // lib/passkeys.svelte.ts ceremony (its own test file covers that code
  // in isolation) -- only the network boundary underneath it is faked
  // here, same as every other call this file already mocks.
  beginPasskeyLogin: vi.fn(),
  submitPasskeyLoginAssertion: vi.fn(),
}))

import {
  beginPasskeyLogin,
  fetchAuthSession,
  login,
  setNewPasswordAfterReset,
  submitLoginFactor,
  submitPasskeyLoginAssertion,
} from '../lib/api'
import { authState } from '../lib/auth.svelte'
import AuthLogin from './AuthLogin.svelte'

// The browser ceremony's own JSON helpers -- jsdom has neither by
// default (lib/passkeys.svelte.test.ts pins that down). Stubbed here so
// the "capable browser" tests below can reach the real button; the
// "incapable browser" tests instead rely on jsdom's own absence of them.
function stubPasskeyCapableBrowser() {
  vi.stubGlobal(
    'PublicKeyCredential',
    class {
      static parseCreationOptionsFromJSON(o: unknown) {
        return o
      }
      static parseRequestOptionsFromJSON(o: unknown) {
        return o
      }
    },
  )
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
  authState.state = 'loading'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  authState.ssoError = null
  authState.justSignedOut = false
  authState.mustChangePassword = false
  authState.pendingSecondFactor = []
  authState.pendingPasskeyOrigin = undefined
})

async function fillAndSubmit(username: string, password: string) {
  await fireEvent.input(screen.getByLabelText('account'), { target: { value: username } })
  await fireEvent.input(screen.getByLabelText('password'), { target: { value: password } })
  await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
}

describe('AuthLogin', () => {
  it('submits the entered credentials to authState.login', async () => {
    vi.mocked(login).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
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
    expect(screen.getByRole('button', { name: /enter/i })).toBeTruthy()
  })

  // #1187: an empty field used to raise the browser's own "Please fill
  // out this field." bubble -- the engine's words, in the browser's
  // language, over a form that already has its own error line.
  it('answers an empty form in its own error line, with the browser kept out of it', async () => {
    const { container } = render(AuthLogin)
    expect(container.querySelector('form')?.hasAttribute('novalidate')).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
    expect(await screen.findByText('Enter your account name.')).toBeTruthy()
    expect(login).not.toHaveBeenCalled()

    await fireEvent.input(screen.getByLabelText('account'), { target: { value: 'tom' } })
    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
    expect(await screen.findByText('Enter your password.')).toBeTruthy()
    expect(login).not.toHaveBeenCalled()
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

  it('plays the way-out beat when this mount follows a sign-out, and consumes the flag', () => {
    authState.justSignedOut = true

    const { container } = render(AuthLogin)

    expect(container.querySelector('.reverse')).toBeTruthy()
    // One-shot: a second mount (e.g. a plain page refresh) must not
    // replay it.
    expect(authState.justSignedOut).toBe(false)
  })

  // #1214 extracted the door's rain into the shared Fullfall component
  // -- this pins that the extraction left the door still raining, with
  // its own centre mask variant.
  it('still rains the fullfall across the door, masked out of the centre', () => {
    const { container } = render(AuthLogin)

    expect(container.querySelectorAll('.fullfall.door i').length).toBe(40)
  })

  it('does not play the way-out beat on a plain page load', () => {
    const { container } = render(AuthLogin)

    expect(container.querySelector('.reverse')).toBeNull()
  })
})


// #1251: after signing in with a one-time code, the door does not open.
// It asks for a password of your own and shows nothing else -- the
// server 403s everything but that one route, so anything else on screen
// would be a promise the session cannot keep.
describe('AuthLogin after a one-time code sign-in', () => {
  beforeEach(() => {
    authState.state = 'must-change-password'
    authState.mustChangePassword = true
  })

  it('asks only for a new password, with no account field and no SSO way round it', () => {
    authState.ssoAvailable = true

    render(AuthLogin)

    expect(screen.getByText('Set a new password')).toBeTruthy()
    expect(screen.queryByLabelText('account')).toBeNull()
    expect(screen.getByLabelText('new password')).toBeTruthy()
    expect(screen.getByLabelText('confirm password')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /sign in with sso/i })).toBeNull()
  })

  it('refuses two passwords that do not match without calling the server', async () => {
    render(AuthLogin)

    await fireEvent.input(screen.getByLabelText('new password'), {
      target: { value: 'new-password-placeholder' },
    })
    await fireEvent.input(screen.getByLabelText('confirm password'), {
      target: { value: 'a-different-placeholder' },
    })
    await fireEvent.click(screen.getByRole('button', { name: /set password/i }))

    expect(await screen.findByText('Passwords do not match.')).toBeTruthy()
    expect(setNewPasswordAfterReset).not.toHaveBeenCalled()
  })

  it('sets the password and opens the app', async () => {
    vi.mocked(setNewPasswordAfterReset).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'bilbo',
      role: 'user',
      mustChangePassword: false,
      ssoAvailable: false,
    })

    render(AuthLogin)

    await fireEvent.input(screen.getByLabelText('new password'), {
      target: { value: 'new-password-placeholder' },
    })
    await fireEvent.input(screen.getByLabelText('confirm password'), {
      target: { value: 'new-password-placeholder' },
    })
    await fireEvent.click(screen.getByRole('button', { name: /set password/i }))

    expect(setNewPasswordAfterReset).toHaveBeenCalledWith('new-password-placeholder')
    expect(authState.state).toBe('authenticated')
  })
})

// #1249's second step: a right password on an account holding a factor
// lands here (authState.state === 'pending-factor') instead of opening
// the app -- login() itself is exercised in auth.svelte.test.ts; this is
// what AuthLogin actually draws for that state and wires the code box to.
describe('AuthLogin at the pending-factor step', () => {
  beforeEach(() => {
    authState.state = 'pending-factor'
    // These tests exercise the authenticator-app/recovery-code box, the
    // same as before #1250 -- an account whose pending login lists no
    // passkey at all (real logins now always set this, via login()).
    // The passkey-specific screens get their own describe block below.
    authState.pendingSecondFactor = ['totp']
    authState.pendingPasskeyOrigin = undefined
  })

  it('shows the code box, with no account field and no SSO way round it', () => {
    authState.ssoAvailable = true

    render(AuthLogin)

    expect(screen.getByText('Enter your code')).toBeTruthy()
    expect(screen.queryByLabelText('account')).toBeNull()
    expect(screen.getByLabelText('code')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /sign in with sso/i })).toBeNull()
  })

  it('submits the code to authState.submitFactor and opens the app on success', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthLogin)

    await fireEvent.input(screen.getByLabelText('code'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(submitLoginFactor).toHaveBeenCalledWith('123456')
    expect(authState.state).toBe('authenticated')
  })

  it('shows the server refusal on a wrong code without opening the app', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue('invalid code')

    render(AuthLogin)

    await fireEvent.input(screen.getByLabelText('code'), { target: { value: '000000' } })
    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(await screen.findByText('invalid code')).toBeTruthy()
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })

  // The recovery-code toggle only relabels the field -- same box, same
  // call, so this pins that it never changes what field name/shape is
  // submitted.
  it('the "use a recovery code" toggle relabels the field without changing what is submitted', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthLogin)

    expect(screen.queryByLabelText('recovery code')).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /use a recovery code instead/i }))
    expect(screen.getByLabelText('recovery code')).toBeTruthy()

    await fireEvent.input(screen.getByLabelText('recovery code'), { target: { value: 'a1b2-c3d4-e5f6-g7h8' } })
    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(submitLoginFactor).toHaveBeenCalledWith('a1b2-c3d4-e5f6-g7h8')
  })

  it('answers an empty code in its own error line, without calling the server', async () => {
    render(AuthLogin)

    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(await screen.findByText('Enter the code from your app.')).toBeTruthy()
    expect(submitLoginFactor).not.toHaveBeenCalled()
  })
})

// The wording has to follow what this account's pending login actually
// listed -- a passkey-only account has no authenticator app to be told to
// open. Companion to the 'Enter your code' assertion above, which pins the
// app-only wording unchanged.
describe('AuthLogin at the pending-factor step, wording per factor (#1250 follow-up)', () => {
  beforeEach(() => {
    authState.state = 'pending-factor'
  })

  it('says nothing about an authenticator app for a passkey-only account', () => {
    authState.pendingSecondFactor = ['passkey']
    authState.pendingPasskeyOrigin = location.origin

    render(AuthLogin)

    expect(screen.getByText('Use your passkey')).toBeTruthy()
    expect(screen.getByText(/use your passkey to finish signing in/i)).toBeTruthy()
    expect(screen.queryByText(/authenticator app/i)).toBeNull()
  })

  it('offers both wordings when the account holds a passkey and an authenticator app', () => {
    authState.pendingSecondFactor = ['passkey', 'totp']
    authState.pendingPasskeyOrigin = location.origin

    render(AuthLogin)

    expect(screen.getByText(/use your passkey, or enter the current code from your authenticator app/i)).toBeTruthy()
  })
})

// #1250: the passkey half of the same pending-factor step -- driven by
// authState.pendingSecondFactor/pendingPasskeyOrigin, both set by
// login() itself (see auth.svelte.test.ts for that wiring).
describe('AuthLogin at the pending-factor step offering a passkey (#1250)', () => {
  beforeEach(() => {
    authState.state = 'pending-factor'
    authState.pendingSecondFactor = ['passkey', 'totp']
    authState.pendingPasskeyOrigin = location.origin
  })

  it('leads with the passkey button when the browser is capable, with the other two as links below', () => {
    stubPasskeyCapableBrowser()
    render(AuthLogin)

    expect(screen.getByRole('button', { name: /^use your passkey$/i })).toBeTruthy()
    expect(screen.queryByLabelText('code')).toBeNull()
    expect(screen.getByRole('button', { name: /use your authenticator app instead/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /use a recovery code instead/i })).toBeTruthy()
  })

  it('fires the ceremony only on the click, never on mount, and opens the app on success', async () => {
    stubPasskeyCapableBrowser()
    vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: { challenge: 'c' } })
    const get = vi.fn(async () => ({ toJSON: () => ({ id: 'cred-1' }) }))
    vi.stubGlobal('navigator', { credentials: { get } })
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthLogin)
    expect(get).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /^use your passkey$/i }))
    // The real chain runs one hop deeper than a code submit (begin ->
    // browser prompt -> submit assertion -> re-check the session), which
    // needs more than the one microtask flush fireEvent.click already
    // waits for.
    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))

    expect(get).toHaveBeenCalled()
    expect(submitPasskeyLoginAssertion).toHaveBeenCalledWith({ id: 'cred-1' })
  })

  it('switching to "use a recovery code instead" is client-side only -- still the same pending login', async () => {
    stubPasskeyCapableBrowser()
    vi.mocked(submitLoginFactor).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'tom',
      role: 'admin',
      ssoAvailable: false,
    })

    render(AuthLogin)
    await fireEvent.click(screen.getByRole('button', { name: /use a recovery code instead/i }))

    expect(screen.getByLabelText('recovery code')).toBeTruthy()
    await fireEvent.input(screen.getByLabelText('recovery code'), { target: { value: 'a1b2-c3d4-e5f6-g7h8' } })
    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(submitLoginFactor).toHaveBeenCalledWith('a1b2-c3d4-e5f6-g7h8')
    expect(beginPasskeyLogin).not.toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
  })

  it('replaces the button with a link to the right address when this browser cannot use the passkey, but still offers the other ways in', () => {
    // No stubPasskeyCapableBrowser() -- jsdom's own default (no
    // PublicKeyCredential at all).
    render(AuthLogin)

    expect(screen.queryByRole('button', { name: /^use your passkey$/i })).toBeNull()
    expect(screen.getByText(/your passkeys work at/i)).toBeTruthy()
    expect(screen.getByLabelText('code')).toBeTruthy()
    expect(screen.getByRole('button', { name: /use a recovery code instead/i })).toBeTruthy()
  })

  it('shows the recovery-code explanation with nothing else offered when the account has no usable factor at all', () => {
    authState.pendingSecondFactor = []
    authState.pendingPasskeyOrigin = undefined
    render(AuthLogin)

    expect(screen.getByText(/made for a different web address/i)).toBeTruthy()
    expect(screen.getByLabelText('recovery code')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /use your passkey/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /use your authenticator app instead/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /use a recovery code instead/i })).toBeNull()
  })
})
