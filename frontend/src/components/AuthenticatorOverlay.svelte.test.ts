// SPDX-License-Identifier: AGPL-3.0-only
//
// AccountMenu's "Authenticator app" overlay (#1249): enrol (QR + the
// secret as text, always) -> confirm -> the ten recovery codes, shown
// once behind an explicit "I have saved these" rather than the header
// X/Escape/backdrop every other overlay here answers to -- and the
// separate turn-off path, password-gated like changePassword.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

// Spreads the real module (importOriginal) rather than a bare object
// literal so ApiError stays the real class -- this overlay catches it
// with `instanceof ApiError` to route a 401 through
// authState.handleUnauthorized().
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    confirmTOTP: vi.fn(),
    disableTOTP: vi.fn(),
    enrolTOTP: vi.fn(),
  }
})

// jsdom has no real canvas backend -- QRCode.toCanvas would either throw
// or silently draw nothing. This overlay's own contract is "the secret
// is always shown as text beside it", so the canvas drawing itself is
// qrcode's problem, not this component's; stubbed here the same way
// AccountMenu.svelte.test.ts stubs matchMedia for a jsdom gap.
vi.mock('../lib/qrcode', () => ({
  qrCode: () => ({ update: () => {}, destroy: () => {} }),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { ApiError, confirmTOTP, disableTOTP, enrolTOTP } from '../lib/api'
import { authState, pageReload } from '../lib/auth.svelte'
import AuthenticatorOverlay from './AuthenticatorOverlay.svelte'

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  // handleUnauthorized() reloads the page (jsdom has no real navigation).
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.hasTOTP = false
})

// A 401 from either of this overlay's own calls means the session behind
// its header X/Escape/backdrop is already gone (see enrolTOTP's own
// comment in lib/api.ts) -- routed the same way AuthEnrolFactor's forced-
// enrolment door routes it, rather than shown as plain error text.
describe('a session that died mid-request (401) is routed to sign-in', () => {
  it('from starting enrolment', async () => {
    authState.state = 'authenticated'
    vi.mocked(enrolTOTP).mockRejectedValue(new ApiError('sign in first', 401))
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
  })

  it('from confirming the code', async () => {
    authState.state = 'authenticated'
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:tom?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockRejectedValue(new ApiError('sign in first', 401))
    render(AuthenticatorOverlay, { open: true })
    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
    await screen.findByTestId('totp-secret')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })

    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
  })
})

describe('the status screen', () => {
  it('offers to set one up when the account has none', async () => {
    render(AuthenticatorOverlay, { open: true })

    expect(screen.getByRole('button', { name: /set up authenticator app/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /turn off/i })).toBeNull()
  })

  it('offers to turn it off when the account already has one', async () => {
    authState.hasTOTP = true
    render(AuthenticatorOverlay, { open: true })

    expect(screen.getByRole('button', { name: /turn off/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /set up authenticator app/i })).toBeNull()
  })
})

describe('enrol shows the QR and the secret as text beside it, always (#1249)', () => {
  it('extracts and shows the secret from the returned otpauth URI', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:tom?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
    })
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))

    expect(await screen.findByTestId('totp-secret')).toHaveProperty('textContent', 'JBSWY3DPEHPK3PXP')
    // The canvas is present too -- the QR is not a substitute for the
    // text, both are drawn at once.
    expect(document.querySelector('canvas')).toBeTruthy()
  })

  it('shows the enrol error and stays on the status screen when it fails', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue('the server could not do that (500)')
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))

    expect(await screen.findByText('the server could not do that (500)')).toBeTruthy()
    expect(screen.queryByTestId('totp-secret')).toBeNull()
  })
})

describe('confirm activates it and shows the ten recovery codes once', () => {
  async function reachEnrolling() {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:tom?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
    })
    render(AuthenticatorOverlay, { open: true })
    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
    await screen.findByTestId('totp-secret')
  }

  it('shows all ten codes and flips authState.hasTOTP on a right code', async () => {
    const codes = Array.from({ length: 10 }, (_, i) => `code-${i}`)
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: codes, alreadyIssued: false })
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(confirmTOTP).toHaveBeenCalledWith('123456')
    const block = await screen.findByTestId('recovery-codes')
    expect(codes.every((c) => block.textContent?.includes(c))).toBe(true)
    expect(authState.hasTOTP).toBe(true)
  })

  // #1250: recovery codes are shared with passkeys and minted once, by
  // whichever factor activates first -- a passkey already on this
  // account means confirming TOTP has nothing new to show.
  it('skips the codes screen and notes the existing codes still stand when a passkey already minted them', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: null, alreadyIssued: true })
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(authState.hasTOTP).toBe(true)
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
    expect(await screen.findByText(/already issued/i)).toBeTruthy()
    // Nothing sensitive on this screen (there are no fresh codes to
    // lose) -- both the header X and the footer's explicit Close are
    // offered, unlike the real codes screen just above.
    expect(screen.getAllByRole('button', { name: /close/i }).length).toBe(2)
  })

  it('shows the refusal and stays on the enrol screen for a wrong code', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue('invalid code')
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '000000' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(await screen.findByText('invalid code')).toBeTruthy()
    expect(authState.hasTOTP).toBe(false)
  })

  // The codes exist in clear nowhere else once this closes -- the header
  // X and Escape are the two easy ways to lose them by accident, so
  // neither is wired on this screen (see the backdrop/Escape guard too).
  it('offers no header close button on the codes screen -- only the explicit acknowledgement', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'], alreadyIssued: false })
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    expect(screen.queryByRole('button', { name: /close/i })).toBeNull()
    expect(screen.getByRole('button', { name: /i have saved these/i })).toBeTruthy()
  })

  it('Escape does not close the codes screen, but does close every other screen', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'], alreadyIssued: false })
    await reachEnrolling()
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: /authenticator app/i })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull()
  })

  it('closes and clears state once "I have saved these" is clicked', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'], alreadyIssued: false })
    await reachEnrolling()
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull()
  })
})

// Closing mid-confirm doesn't cancel the request, it only unmounts the
// view -- a confirm that turns out to mint fresh recovery codes would
// then have nowhere left open to show them, lost for good on reload.
// The 'codes' screen itself already has no way out; this is the window
// just before it, while confirm() is still in flight.
describe('closing is blocked while a request is in flight', () => {
  async function reachEnrollingWithPendingConfirm() {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:tom?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
    })
    let resolveConfirm: (v: { recoveryCodes: string[]; alreadyIssued: boolean }) => void
    vi.mocked(confirmTOTP).mockReturnValue(
      new Promise((resolve) => {
        resolveConfirm = resolve
      }),
    )
    render(AuthenticatorOverlay, { open: true })
    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
    await screen.findByTestId('totp-secret')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    return (result: { recoveryCodes: string[]; alreadyIssued: boolean }) => resolveConfirm(result)
  }

  it('Escape does not close the dialog while confirm() is pending', async () => {
    await reachEnrollingWithPendingConfirm()

    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.getByRole('dialog', { name: /authenticator app/i })).toBeTruthy()
  })

  it('the backdrop click does not close the dialog while confirm() is pending', async () => {
    await reachEnrollingWithPendingConfirm()

    await fireEvent.click(document.querySelector('.backdrop')!)

    expect(screen.getByRole('dialog', { name: /authenticator app/i })).toBeTruthy()
  })

  it('the header close button is disabled while confirm() is pending', async () => {
    await reachEnrollingWithPendingConfirm()

    expect(screen.getByRole('button', { name: /^close$/i })).toHaveProperty('disabled', true)
  })
})

describe('turning it off needs the password', () => {
  it('is refused server-side and shown inline, leaving hasTOTP untouched', async () => {
    vi.mocked(disableTOTP).mockResolvedValue('wrong password')
    authState.hasTOTP = true
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: /turn off authenticator app/i }))

    expect(disableTOTP).toHaveBeenCalledWith('wrong')
    expect(await screen.findByText('wrong password')).toBeTruthy()
    expect(authState.hasTOTP).toBe(true)
  })

  it('turns it off and flips authState.hasTOTP on the right password, when a passkey still stands', async () => {
    vi.mocked(disableTOTP).mockResolvedValue({ signedOut: false })
    authState.hasTOTP = true
    authState.passkeyCount = 1
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /turn off authenticator app/i }))

    expect(disableTOTP).toHaveBeenCalledWith('correct-horse')
    expect(authState.hasTOTP).toBe(false)
    expect(await screen.findByText(/turned off/i)).toBeTruthy()
    expect(screen.getByText(/your passkey still protects sign-in/i)).toBeTruthy()
  })

  // #1253's own ruling: a passkey-less account may still turn off its
  // authenticator app -- that just costs it the account's last second
  // factor, which the server answers by signing the caller out
  // everywhere rather than leaving a half-protected session up.
  it('warns before removing the account\'s only second factor, not "password alone"', async () => {
    authState.hasTOTP = true
    authState.passkeyCount = 0
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))

    expect(screen.getByText(/signs you out now/i)).toBeTruthy()
    expect(screen.queryByText(/password alone/i)).toBeNull()
  })

  it('does not claim "password alone" when a passkey will still stand', async () => {
    authState.hasTOTP = true
    authState.passkeyCount = 1
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))

    expect(screen.getByText(/your passkey will still be asked for/i)).toBeTruthy()
    expect(screen.queryByText(/password alone/i)).toBeNull()
  })

  it('signs out through authState the way logout does, rather than showing "turned off", when this was the last factor', async () => {
    vi.mocked(disableTOTP).mockResolvedValue({ signedOut: true })
    authState.hasTOTP = true
    authState.passkeyCount = 0
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /turn off authenticator app/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
    expect(authState.state).toBe('unauthenticated')
    expect(screen.queryByText(/turned off\.$/i)).toBeNull()
  })
})
