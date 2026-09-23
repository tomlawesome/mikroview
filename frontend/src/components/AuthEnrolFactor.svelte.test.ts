// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

// Same approach as AuthLogin.svelte.test.ts: mock lib/api.ts (never
// auth.svelte.ts itself), so these tests exercise the real AuthState
// transitions and the door's real markup with only the network faked.
vi.mock('../lib/api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  setNewPasswordAfterReset: vi.fn(),
  signOutEverywhere: vi.fn(),
  submitLoginFactor: vi.fn(),
  enrolTOTP: vi.fn(),
  confirmTOTP: vi.fn(),
  fetchPersistence: vi.fn(),
  fetchMyPreferences: vi.fn(),
  saveMyPreferences: vi.fn(),
}))

// The passkey ceremony is a browser boundary jsdom cannot cross --
// mocked whole, the same way auth.svelte.test.ts already stubs
// loginWithPasskey (registerPasskey's own behaviour has its own file,
// lib/passkeys.svelte.test.ts).
vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
  loginWithPasskey: vi.fn(),
}))

// jsdom has no canvas 2D context for the real action to draw into; the
// markup around the canvas is what this file asserts on.
vi.mock('../lib/qrcode', () => ({
  qrCode: () => {},
}))

import { confirmTOTP, enrolTOTP, fetchAuthSession, fetchMyPreferences, saveMyPreferences } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState } from '../lib/auth.svelte'
import { preferencesState } from '../lib/preferences.svelte'
import AuthEnrolFactor from './AuthEnrolFactor.svelte'

const TEN_CODES = [
  'p2xk-9qfw',
  'j6nm-4rvt',
  'w8cd-1lsz',
  'm3hg-7ypb',
  'q9vr-5kdn',
  't4bj-8wmf',
  'x1sl-6czh',
  'd7pn-3qgy',
  'f5wt-2jrk',
  'b8mz-9xvc',
]

beforeEach(() => {
  vi.resetAllMocks()
  authState.state = 'must-enrol-factor'
  authState.mustEnrolSecondFactor = true
  authState.username = 'meredith'
  authState.role = 'viewer'
  authState.hasTOTP = false
  authState.passkeyCount = 0
  // Both keys live by default; the unusable-deployment tests override.
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
  // apply() fires preferencesState.ensureLoaded() the moment the door
  // opens into 'authenticated' -- give it the same safe default
  // auth.svelte.test.ts does.
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
})

// Choose the authenticator key and arrive on the prove-it stage.
async function enterTotpStage() {
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
  })
  await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
  await screen.findByLabelText('Code from the app')
}

describe('AuthEnrolFactor (the forced-enrolment door, #1336)', () => {
  it('offers both factor kinds, the authenticator app first', () => {
    render(AuthEnrolFactor)

    const keys = screen.getAllByRole('button', { name: /set it up/i })
    expect(keys.length).toBe(2)
    expect(keys[0].textContent).toContain('Authenticator app')
    expect(keys[1].textContent).toContain('Passkey')
  })

  it('has no way past it: no links out, no cancel, no skip, no sign-out -- and says so', () => {
    const { container } = render(AuthEnrolFactor)

    // The whole door carries not one anchor -- nothing navigates away.
    expect(container.querySelector('a')).toBeNull()
    expect(screen.queryByRole('button', { name: /cancel|close|skip|sign out|log out/i })).toBeNull()
    // The no-way-out is said, quietly, in the mono caption voice.
    expect(screen.getByText(/no skipping this one/i)).toBeTruthy()
  })

  it('keeps the passkey key visible but disabled, with the one line why, when the deployment cannot offer one', () => {
    authState.passkeyStatus = 'ip'
    authState.passkeyOrigin = undefined

    const { container } = render(AuthEnrolFactor)

    // Never a hidden row (the ratified rule): the key is still drawn...
    const disabled = container.querySelector('.key[aria-disabled="true"]')
    expect(disabled?.textContent).toContain('Passkey')
    // ...but it is not a control, and it says why in the mockup's line.
    expect(screen.getAllByRole('button', { name: /set it up/i }).length).toBe(1)
    expect(screen.getByText(/passkeys need a web address/i)).toBeTruthy()
  })

  it('walks the authenticator through prove-it to the codes, and "I have saved these" opens the app', async () => {
    render(AuthEnrolFactor)

    await enterTotpStage()

    // The mockup's grouped presentation of the same secret the QR holds.
    expect(screen.getByTestId('totp-secret').textContent).toBe(
      'GQ4T MNZV G5UW K2LN MFRG YZLB OR2W CZ3F',
    )

    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false })
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    expect(confirmTOTP).toHaveBeenCalledWith('123456')
    expect(authState.hasTOTP).toBe(true)
    expect(screen.getByText('Keep the codes')).toBeTruthy()
    expect(screen.getByTestId('recovery-codes').textContent).toContain('p2xk-9qfw')
    // Still no way out on the codes stage: only "Copy all" and the
    // explicit acknowledgement exist.
    expect(screen.queryByRole('button', { name: /cancel|close|skip/i })).toBeNull()

    // The door has not opened yet -- only the acknowledgement opens it.
    expect(authState.state).toBe('must-enrol-factor')

    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: false,
      ssoAvailable: false,
    })
    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
  })

  it('shows the server refusal on a wrong code and stays on the door', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockResolvedValue('invalid code')

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '000000' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(await screen.findByText('invalid code')).toBeTruthy()
    expect(authState.state).toBe('must-enrol-factor')
    expect(fetchAuthSession).not.toHaveBeenCalled()
  })

  it('walks the passkey through naming and the ceremony to the codes', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({
      passkey: {
        id: 'cred-1',
        name: 'this laptop',
        createdAt: '2026-09-23T10:00:00Z',
        transports: [],
        stale: false,
        rpId: 'view.brandt.example',
      },
      recoveryCodes: TEN_CODES,
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
    await screen.findByLabelText('Name')
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

    await screen.findByTestId('recovery-codes')
    expect(registerPasskey).toHaveBeenCalledWith('this laptop')
    expect(authState.passkeyCount).toBe(1)
    expect(screen.getByText('Keep the codes')).toBeTruthy()
  })

  it('each prove stage offers the other key as the quiet link, when that key is live', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    expect(screen.getByRole('button', { name: /use a passkey instead/i })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /use a passkey instead/i }))
    await screen.findByLabelText('Name')
    expect(screen.getByRole('button', { name: /use an authenticator app instead/i })).toBeTruthy()
  })

  it('omits the "use a passkey instead" link when the deployment cannot offer one', async () => {
    authState.passkeyStatus = 'unset'
    authState.passkeyOrigin = undefined
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    expect(screen.queryByRole('button', { name: /use a passkey instead/i })).toBeNull()
  })

  it('opens the app straight from a confirm whose codes were already issued -- there is nothing to show', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: null, alreadyIssued: true })
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: false,
      ssoAvailable: false,
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
  })

  it('rains the ratified fullfall behind the door, masked out of its own wider centre', () => {
    const { container } = render(AuthEnrolFactor)

    expect(container.querySelectorAll('.fullfall.enrol i').length).toBe(40)
  })
})
