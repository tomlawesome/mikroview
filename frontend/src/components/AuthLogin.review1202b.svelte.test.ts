// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U11 (note_34147), the sign-in second step. On POST
// /api/auth/login/factor a 401 step-expired answering a passkey assertion
// means only the passkey ceremony lapsed: the pending login still holds, so
// the passkey step starts again (login/factor/begin is called again) and the
// person stays off the password form. A lapsed pending login (the code box's
// step-expired, or login/factor/begin itself refusing) still goes back to the
// password form with the timeout notice. Written before the fix.
//
// Names the fix must provide: api.ts exports PASSKEY_STEP_EXPIRED, the string
// submitPasskeyLoginAssertion answers for the ceremony having lapsed
// (pinned in lib/api.review1202b.test.ts). Assumption the build may change:
// the restart is automatic -- begin is called again without a second click.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  createUser: vi.fn(),
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  setNewPasswordAfterReset: vi.fn(),
  submitLoginFactor: vi.fn(),
  PENDING_LOGIN_EXPIRED: 'sign in again',
  PASSKEY_STEP_EXPIRED: 'the passkey step ran out, try your passkey again',
  beginPasskeyLogin: vi.fn(),
  submitPasskeyLoginAssertion: vi.fn(),
}))

import {
  beginPasskeyLogin,
  fetchAuthSession,
  PASSKEY_STEP_EXPIRED,
  PENDING_LOGIN_EXPIRED,
  submitLoginFactor,
  submitPasskeyLoginAssertion,
} from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { wizardJourney } from '../lib/wizardJourney.svelte'
import AuthLogin from './AuthLogin.svelte'

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

const SESSION = { setupRequired: false, authenticated: true, username: 'alex', role: 'admin' as const, ssoAvailable: false }

beforeEach(() => {
  vi.resetAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
  authState.state = 'pending-factor'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  authState.ssoError = null
  authState.signInTimedOut = false
  authState.justSignedOut = false
  authState.mustChangePassword = false
  authState.pendingSecondFactor = ['passkey', 'totp']
  authState.pendingPasskeyOrigin = location.origin
  wizardJourney.end()
  stubPasskeyCapableBrowser()
})

const OPTIONS = { publicKey: { challenge: 'c' } }

// Each press of the browser prompt answers the next entry; a function entry
// can hold the prompt open forever.
function promptAnswers(...answers: (object | (() => Promise<never>))[]) {
  let n = 0
  const get = vi.fn(async () => {
    const answer = answers[Math.min(n++, answers.length - 1)]
    if (typeof answer === 'function') return answer()
    return answer
  })
  vi.stubGlobal('navigator', { credentials: { get } })
  return get
}

const credential = (id: string) => ({ toJSON: () => ({ id }) })
const forever = () => new Promise<never>(() => {})

async function pressPasskey() {
  await fireEvent.click(screen.getByRole('button', { name: /^use your passkey$/i }))
}

const onPasswordForm = () => screen.queryByLabelText('account') !== null || screen.queryByLabelText('password') !== null

describe('a lapsed passkey ceremony restarts the passkey step (#1202 review U11)', () => {
  // #1202 review U11
  it('calls login/factor/begin again and stays off the password form while the new prompt is open', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValue(OPTIONS)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValueOnce(PASSKEY_STEP_EXPIRED)
    const get = promptAnswers(credential('cred-1'), forever)

    render(AuthLogin)
    await pressPasskey()

    await vi.waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    expect(submitPasskeyLoginAssertion).toHaveBeenCalledTimes(1)
    expect(authState.state).toBe('pending-factor')
    expect(authState.signInTimedOut).toBe(false)
    expect(onPasswordForm()).toBe(false)
    expect(screen.queryByText(/your sign-in timed out/i)).toBeNull()
  })

  // #1202 review U11
  it('the restarted prompt can finish the sign-in: the second assertion opens the app', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValue(OPTIONS)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValueOnce(PASSKEY_STEP_EXPIRED).mockResolvedValueOnce(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(SESSION)
    promptAnswers(credential('cred-1'), credential('cred-2'))

    render(AuthLogin)
    await pressPasskey()

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
    expect(submitPasskeyLoginAssertion).toHaveBeenNthCalledWith(1, { id: 'cred-1' })
    expect(submitPasskeyLoginAssertion).toHaveBeenNthCalledWith(2, { id: 'cred-2' })
  })

  // #1202 review U11: the person is not sent to the code box or the password form either.
  it('does not show a refusal message or throw the person onto another way in', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValue(OPTIONS)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValueOnce(PASSKEY_STEP_EXPIRED)
    promptAnswers(credential('cred-1'), forever)

    render(AuthLogin)
    await pressPasskey()
    await vi.waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(2))

    expect(screen.queryByLabelText('code')).toBeNull()
    expect(screen.queryByText(PASSKEY_STEP_EXPIRED)).toBeNull()
  })
})

describe('a lapsed pending login still goes back to the password form (#1202 review U11)', () => {
  // #1202 review U11: pair -- the assertion says the pending login is gone.
  it('an assertion refused with PENDING_LOGIN_EXPIRED falls back to the password form with the timeout notice, and does not restart', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValue(OPTIONS)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValue(PENDING_LOGIN_EXPIRED)
    promptAnswers(credential('cred-1'))

    render(AuthLogin)
    await pressPasskey()

    expect(await screen.findByText(/your sign-in timed out/i)).toBeTruthy()
    expect(screen.getByLabelText('account')).toBeTruthy()
    expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
  })

  // #1202 review U11: pair -- the ceremony lapsed, but on the restart the pending login turns out to be gone too.
  it('a restart whose begin answers PENDING_LOGIN_EXPIRED falls back to the password form with the timeout notice', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValueOnce(OPTIONS).mockResolvedValueOnce(PENDING_LOGIN_EXPIRED)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValueOnce(PASSKEY_STEP_EXPIRED)
    promptAnswers(credential('cred-1'))

    render(AuthLogin)
    await pressPasskey()

    expect(await screen.findByText(/your sign-in timed out/i)).toBeTruthy()
    expect(screen.getByLabelText('account')).toBeTruthy()
    expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
    expect(submitPasskeyLoginAssertion).toHaveBeenCalledTimes(1)
  })

  // #1202 review U11: pair -- a code at the same route: step-expired is the pending login.
  it('a code refused with PENDING_LOGIN_EXPIRED still falls back to the password form', async () => {
    vi.mocked(submitLoginFactor).mockResolvedValue(PENDING_LOGIN_EXPIRED)

    render(AuthLogin)
    await fireEvent.click(screen.getByRole('button', { name: /use your authenticator app instead/i }))
    await fireEvent.input(screen.getByLabelText('code'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(await screen.findByText(/your sign-in timed out/i)).toBeTruthy()
    expect(screen.getByLabelText('account')).toBeTruthy()
    expect(beginPasskeyLogin).not.toHaveBeenCalled()
  })

  // #1202 review U11: pair -- an ordinary refusal of an assertion neither restarts nor leaves the step.
  it('a wrong assertion shows its message under the passkey button and does not restart', async () => {
    vi.mocked(beginPasskeyLogin).mockResolvedValue(OPTIONS)
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValue("that passkey couldn't be verified")
    promptAnswers(credential('cred-1'))

    render(AuthLogin)
    await pressPasskey()

    expect(await screen.findByText(/that passkey couldn't be verified/i)).toBeTruthy()
    expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
    expect(authState.state).toBe('pending-factor')
    expect(onPasswordForm()).toBe(false)
  })
})
