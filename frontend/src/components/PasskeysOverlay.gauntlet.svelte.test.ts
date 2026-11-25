// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 34): the account menu's passkeys overlay. A first passkey is
// held, with the ten codes, until POST /api/auth/recovery-codes/confirm; the
// overlay's "I have saved these" must make that call and only then close.
// Without `pendingConfirmation` (a factor that went live at once) it closes as before.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    fetchPasskeys: vi.fn(),
    renamePasskey: vi.fn(),
    disablePasskey: vi.fn(),
    confirmRecoveryCodes: vi.fn(),
    fetchAuthSession: vi.fn(),
  }
})

vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { confirmRecoveryCodes, fetchAuthSession, fetchPasskeys } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
import PasskeysOverlay from './PasskeysOverlay.svelte'

const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

const PASSKEY = {
  id: 'cred-1',
  name: 'this laptop',
  createdAt: '2026-10-10T00:00:00Z',
  transports: ['internal'],
  stale: false,
  rpId: 'mikroview.example.org',
}

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
  vi.mocked(fetchPasskeys).mockResolvedValue([])
  vi.mocked(fetchAuthSession).mockResolvedValue({
    setupRequired: false,
    authenticated: true,
    username: 'alex',
    role: 'admin',
    ssoAvailable: false,
  })
  authState.state = 'authenticated'
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
})

async function reachCodes() {
  render(PasskeysOverlay, { open: true })
  await screen.findByText(/add a passkey/i)
  await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByTestId('recovery-codes')
}

describe('the passkeys overlay and a held first factor (#1202 spec 34)', () => {
  it('"I have saved these" calls recovery-codes/confirm when the registration said pendingConfirmation, and closes only after it answers', async () => {
    const confirmed = deferred<null>()
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: CODES, pendingConfirmation: true })
    vi.mocked(confirmRecoveryCodes).mockReturnValue(confirmed.promise)
    await reachCodes()
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    await new Promise((r) => setTimeout(r, 0))

    expect(confirmRecoveryCodes).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('dialog', { name: /passkeys/i })).toBeTruthy()
    expect(screen.queryByTestId('recovery-codes')).toBeTruthy()

    confirmed.resolve(null)

    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /passkeys/i })).toBeNull())
  })

  // Pair for the above.
  it('"I have saved these" does not call recovery-codes/confirm when the field is absent, and closes', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: CODES })
    await reachCodes()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    expect(screen.queryByRole('dialog', { name: /passkeys/i })).toBeNull()
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
  })
})
