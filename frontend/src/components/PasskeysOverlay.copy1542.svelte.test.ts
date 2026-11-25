// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the passkey overlay's "Copy all" flips its own label to
// "Copied" / "Copy failed" (sentence case: a modal overlay, C6 #1541), then
// returns to "Copy all". Harness as PasskeysOverlay.review1202.svelte.test.ts.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, fetchPasskeys: vi.fn(), fetchAuthSession: vi.fn(), logout: vi.fn() }
})
vi.mock('../lib/passkeys.svelte', () => ({ registerPasskey: vi.fn() }))
vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { fetchAuthSession, fetchPasskeys, logout } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
import PasskeysOverlay from './PasskeysOverlay.svelte'

const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

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
  vi.mocked(logout).mockResolvedValue(null)
  vi.mocked(registerPasskey).mockResolvedValue({
    passkey: { id: 'c1', name: 'this laptop', createdAt: '2026-10-10T00:00:00Z', transports: [], stale: false, rpId: 'mikroview.example.org' },
    recoveryCodes: CODES,
    pendingConfirmation: true,
  })
  authState.state = 'authenticated'
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
})

afterEach(() => {
  vi.useRealTimers()
})

async function copyAll(ok: boolean): Promise<HTMLElement> {
  vi.mocked(copyToClipboard).mockResolvedValue(ok)
  render(PasskeysOverlay, { open: true })
  await screen.findByText(/add a passkey/i)
  await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByTestId('recovery-codes')
  const btn = screen.getByRole('button', { name: /^copy all$/i })
  vi.useFakeTimers()
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('PasskeysOverlay copy label flip (#1542 C12)', () => {
  it('flips to "Copied" and returns to "Copy all"', async () => {
    const btn = await copyAll(true)
    expect(btn.textContent?.trim()).toBe('Copied')
    vi.advanceTimersByTime(1600)
    flushSync()
    expect(btn.textContent?.trim()).toBe('Copy all')
  })

  it('flips to "Copy failed" when the copy fails', async () => {
    const btn = await copyAll(false)
    expect(btn.textContent?.trim()).toBe('Copy failed')
  })
})
