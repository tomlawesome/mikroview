// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 34): the account menu's authenticator-app overlay. When the
// app is the account's first second factor the server holds it, with its ten
// codes, until POST /api/auth/recovery-codes/confirm; the overlay's
// "I have saved these" must make that call and only then close. Without
// `pendingConfirmation` (a factor that went live at once) it closes as before.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    confirmTOTP: vi.fn(),
    confirmRecoveryCodes: vi.fn(),
    disableTOTP: vi.fn(),
    enrolTOTP: vi.fn(),
    fetchAuthSession: vi.fn(),
  }
})

vi.mock('../lib/qrcode', () => ({
  qrCode: () => ({ update: () => {}, destroy: () => {} }),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { confirmRecoveryCodes, confirmTOTP, enrolTOTP, fetchAuthSession } from '../lib/api'
import { authState, pageReload } from '../lib/auth.svelte'
import AuthenticatorOverlay from './AuthenticatorOverlay.svelte'

const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.state = 'authenticated'
  authState.hasTOTP = false
  vi.mocked(fetchAuthSession).mockResolvedValue({
    setupRequired: false,
    authenticated: true,
    username: 'alex',
    role: 'admin',
    ssoAvailable: false,
  })
})

async function reachCodes() {
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:alex?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
  })
  render(AuthenticatorOverlay, { open: true })
  await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByTestId('totp-secret')
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
  await screen.findByTestId('recovery-codes')
}

describe('the authenticator overlay and a held first factor (#1202 spec 34)', () => {
  it('"I have saved these" calls recovery-codes/confirm when the confirm said pendingConfirmation, and closes only after it answers', async () => {
    const confirmed = deferred<null>()
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: CODES, alreadyIssued: false, pendingConfirmation: true })
    vi.mocked(confirmRecoveryCodes).mockReturnValue(confirmed.promise)
    await reachCodes()
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    await new Promise((r) => setTimeout(r, 0))

    expect(confirmRecoveryCodes).toHaveBeenCalledTimes(1)
    // Not answered yet: the codes are still on screen.
    expect(screen.getByRole('dialog', { name: /authenticator app/i })).toBeTruthy()
    expect(screen.queryByTestId('recovery-codes')).toBeTruthy()

    confirmed.resolve(null)

    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull())
  })

  // Pair for the above.
  it('"I have saved these" does not call recovery-codes/confirm when the field is absent, and closes', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: CODES, alreadyIssued: false })
    await reachCodes()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull()
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
  })
})
