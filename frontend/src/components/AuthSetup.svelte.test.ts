// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
  startSSOLink: vi.fn(),
}))

import { fetchAuthSession, register, startSSOLink } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { journeyState } from '../lib/journey.svelte'
import AuthSetup from './AuthSetup.svelte'

// The SSO route ends in a real top-level navigation, which jsdom cannot
// perform -- stubbed so the tests can read where the browser was sent.
beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('location', { href: '' })
  authState.state = 'setup-required'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  journeyState.phase = 'idle'
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// The three fields and the button, in the order somebody fills them in.
async function createTheAdmin() {
  await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
  await fireEvent.input(screen.getByLabelText('account'), { target: { value: 'tom' } })
  await fireEvent.input(screen.getByLabelText('password'), { target: { value: 'hunter2222' } })
  await fireEvent.input(screen.getByLabelText('confirm password'), { target: { value: 'hunter2222' } })
  await fireEvent.click(screen.getByRole('button', { name: /create account/i }))
}

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

  // #1252, owner's ruling: first run always creates a local admin, so
  // SSO is never an alternative on this door -- not offered, and
  // nothing withheld to explain either. What the person is told is
  // where they go next.
  it('never offers SSO as a way into the first run', async () => {
    authState.ssoAvailable = true

    render(AuthSetup)
    await fireEvent.click(screen.getByRole('button', { name: /enter/i }))

    expect(screen.queryByRole('link', { name: /sign in with sso/i })).toBeNull()
    expect(screen.getByRole('button', { name: /create account/i })).toBeTruthy()
    expect(screen.getByText(/then you sign in with SSO to connect it/i)).toBeTruthy()
  })

  // Route one out of creation: OIDC is configured, so the browser that
  // made the account carries straight on to the provider, and the
  // identity that comes back is linked to it. Session continuity is the
  // proof it is the same person -- nothing compares an email.
  it('hands over to the identity provider when OIDC is configured', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
    authState.ssoAvailable = true

    render(AuthSetup)
    await createTheAdmin()

    await waitFor(() => expect(startSSOLink).toHaveBeenCalledOnce())
    await waitFor(() => expect(location.href).toBe('https://idp.example/authorize'))
  })

  // Route two: no OIDC details, so there is nowhere to forward to. The
  // flow ends on the confirmation rather than a redirect, and says
  // where the provider's details go.
  it('ends on the confirmation, not a redirect, when OIDC is not configured', async () => {
    vi.mocked(register).mockResolvedValue(null)
    authState.ssoAvailable = false

    render(AuthSetup)
    await createTheAdmin()

    expect(await screen.findByText(/admin account created/i)).toBeTruthy()
    expect(screen.getByText(/config file/i)).toBeTruthy()
    expect(startSSOLink).not.toHaveBeenCalled()
    expect(location.href).toBe('')
    // Still the setup view until the person says they are ready: the
    // session is only re-read when they continue.
    expect(fetchAuthSession).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => expect(fetchAuthSession).toHaveBeenCalledOnce())
  })

  // The account exists by the time the hand-off can fail, so the
  // failure belongs on the confirmation -- not on a creation form that
  // would invite creating it again.
  it('still confirms the account when the hand-off to SSO fails', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(startSSOLink).mockResolvedValue('this account already signs in through your identity provider')
    authState.ssoAvailable = true

    render(AuthSetup)
    await createTheAdmin()

    expect(await screen.findByText(/admin account created/i)).toBeTruthy()
    expect(screen.getByText(/already signs in through your identity provider/i)).toBeTruthy()
    expect(location.href).toBe('')
  })

  // #1218 audit finding 7: startSSOLink() can throw outright (a dropped
  // connection, same as any other fetch), not just answer with an error
  // string -- and unlike that answered-string case above, an uncaught
  // throw here used to skip both the redirect and `created = true`,
  // leaving AuthScreen's handleSubmit stuck mid-await and its submit
  // button on "Please wait…" forever. The account already exists by
  // then (register() above succeeded), so there is no form to retry
  // against -- the operator needs the confirmation screen's working
  // Continue button, the same as any other hand-off failure.
  it('still confirms the account, with a working way in, when the hand-off to SSO throws outright', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(startSSOLink).mockRejectedValue(new Error('network unreachable'))
    authState.ssoAvailable = true

    render(AuthSetup)
    await createTheAdmin()

    expect(await screen.findByText(/admin account created/i)).toBeTruthy()
    expect(location.href).toBe('')

    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => expect(fetchAuthSession).toHaveBeenCalledOnce())
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
