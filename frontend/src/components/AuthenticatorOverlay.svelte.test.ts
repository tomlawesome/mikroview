// SPDX-License-Identifier: AGPL-3.0-only
//
// AccountMenu's "Authenticator app" overlay (#1249): enrol (QR + the
// secret as text, always) -> confirm -> the ten recovery codes, shown
// once behind an explicit "I have saved these" rather than the header
// X/Escape/backdrop every other overlay here answers to -- and the
// separate turn-off path, password-gated like changePassword.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  confirmTOTP: vi.fn(),
  disableTOTP: vi.fn(),
  enrolTOTP: vi.fn(),
}))

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

import { confirmTOTP, disableTOTP, enrolTOTP } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import AuthenticatorOverlay from './AuthenticatorOverlay.svelte'

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  authState.hasTOTP = false
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
    vi.mocked(confirmTOTP).mockResolvedValue(codes)
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(confirmTOTP).toHaveBeenCalledWith('123456')
    const block = await screen.findByTestId('recovery-codes')
    expect(codes.every((c) => block.textContent?.includes(c))).toBe(true)
    expect(authState.hasTOTP).toBe(true)
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
    vi.mocked(confirmTOTP).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
    await reachEnrolling()

    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    expect(screen.queryByRole('button', { name: /close/i })).toBeNull()
    expect(screen.getByRole('button', { name: /i have saved these/i })).toBeTruthy()
  })

  it('Escape does not close the codes screen, but does close every other screen', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
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
    vi.mocked(confirmTOTP).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
    await reachEnrolling()
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull()
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

  it('turns it off and flips authState.hasTOTP on the right password', async () => {
    vi.mocked(disableTOTP).mockResolvedValue(null)
    authState.hasTOTP = true
    render(AuthenticatorOverlay, { open: true })

    await fireEvent.click(screen.getByRole('button', { name: /turn off/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /turn off authenticator app/i }))

    expect(disableTOTP).toHaveBeenCalledWith('correct-horse')
    expect(authState.hasTOTP).toBe(false)
    expect(await screen.findByText(/turned off/i)).toBeTruthy()
  })
})
