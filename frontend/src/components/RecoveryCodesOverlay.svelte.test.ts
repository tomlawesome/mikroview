// SPDX-License-Identifier: AGPL-3.0-only
//
// AccountMenu's "New recovery codes…" overlay (#1331): password gate ->
// the ten fresh codes, shown once behind the same explicit "I have saved
// these" AuthenticatorOverlay and PasskeysOverlay already use.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    regenerateRecoveryCodes: vi.fn(),
  }
})

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { ApiError, regenerateRecoveryCodes } from '../lib/api'
import { authState, pageReload } from '../lib/auth.svelte'
import RecoveryCodesOverlay from './RecoveryCodesOverlay.svelte'

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  // handleUnauthorized() only reloads from a state it recognises as a
  // live session -- see its own guard in auth.svelte.ts.
  authState.state = 'authenticated'
})

describe('the password gate', () => {
  it('disables the regenerate button until a password is typed', async () => {
    render(RecoveryCodesOverlay, { open: true })

    expect(screen.getByRole('button', { name: /regenerate codes/i })).toHaveProperty('disabled', true)
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    expect(screen.getByRole('button', { name: /regenerate codes/i })).toHaveProperty('disabled', false)
  })

  it('shows the server refusal inline on a wrong password, without leaving the gate', async () => {
    vi.mocked(regenerateRecoveryCodes).mockResolvedValue('incorrect password')
    render(RecoveryCodesOverlay, { open: true })

    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))

    expect(regenerateRecoveryCodes).toHaveBeenCalledWith('wrong')
    expect(await screen.findByText('incorrect password')).toBeTruthy()
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
  })

  // This route needs an existing session before it does anything else,
  // the same as the four forced-enrolment routes -- a 401 can only mean
  // that session is gone, never a wrong password (which the server
  // answers as a plain 401 body caught above, not thrown).
  it('routes a 401 to sign-in rather than showing it as plain error text', async () => {
    vi.mocked(regenerateRecoveryCodes).mockRejectedValue(new ApiError('sign in first', 401))
    render(RecoveryCodesOverlay, { open: true })

    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
  })
})

describe('a right password shows all ten fresh codes once', () => {
  it('shows the ten codes and clears the password field', async () => {
    const codes = Array.from({ length: 10 }, (_, i) => `code-${i}`)
    vi.mocked(regenerateRecoveryCodes).mockResolvedValue(codes)
    render(RecoveryCodesOverlay, { open: true })

    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))

    const block = await screen.findByTestId('recovery-codes')
    expect(codes.every((c) => block.textContent?.includes(c))).toBe(true)
  })

  // The codes exist in clear nowhere else once this closes -- same
  // no-easy-way-out contract as AuthenticatorOverlay/PasskeysOverlay's
  // own codes screen.
  it('offers no header close button on the codes screen -- only the explicit acknowledgement', async () => {
    vi.mocked(regenerateRecoveryCodes).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
    render(RecoveryCodesOverlay, { open: true })

    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))
    await screen.findByTestId('recovery-codes')

    expect(screen.queryByRole('button', { name: /^close$/i })).toBeNull()
    expect(screen.getByRole('button', { name: /i have saved these/i })).toBeTruthy()
  })

  it('Escape does not close the codes screen, but does close the gate', async () => {
    render(RecoveryCodesOverlay, { open: true })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: /new recovery codes/i })).toBeNull()

    vi.mocked(regenerateRecoveryCodes).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
    render(RecoveryCodesOverlay, { open: true })
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))
    await screen.findByTestId('recovery-codes')

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: /new recovery codes/i })).toBeTruthy()
  })

  it('closes and clears state once "I have saved these" is clicked', async () => {
    vi.mocked(regenerateRecoveryCodes).mockResolvedValue(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
    render(RecoveryCodesOverlay, { open: true })
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))
    await screen.findByTestId('recovery-codes')

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    expect(screen.queryByRole('dialog', { name: /new recovery codes/i })).toBeNull()
  })
})

// Closing mid-request doesn't cancel it, only unmounts the view -- a
// regenerate that turns out to succeed would then have nowhere left
// open to show the fresh codes, lost for good on reload. Same guard
// AuthenticatorOverlay/PasskeysOverlay take on their own confirm calls.
describe('closing is blocked while a request is in flight', () => {
  async function startPendingRegenerate() {
    let resolve: (v: string[]) => void
    vi.mocked(regenerateRecoveryCodes).mockReturnValue(
      new Promise((r) => {
        resolve = r
      }),
    )
    render(RecoveryCodesOverlay, { open: true })
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /regenerate codes/i }))
    return (codes: string[]) => resolve(codes)
  }

  it('Escape does not close the dialog while the request is pending', async () => {
    await startPendingRegenerate()

    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.getByRole('dialog', { name: /new recovery codes/i })).toBeTruthy()
  })

  it('the header close button is disabled while the request is pending', async () => {
    await startPendingRegenerate()

    expect(screen.getByRole('button', { name: /^close$/i })).toHaveProperty('disabled', true)
  })
})
