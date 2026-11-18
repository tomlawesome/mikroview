// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the recovery-codes "Copy all" control flips its own label
// to "copied" / "copy failed" (lowercase, like every other copy control).
// Harness as AuthenticatorOverlay.review1202b.svelte.test.ts.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, disableTOTP: vi.fn(), enrolTOTP: vi.fn(), fetchAuthSession: vi.fn(), logout: vi.fn() }
})
vi.mock('../lib/qrcode', () => ({ qrCode: () => ({ update: () => {}, destroy: () => {} }) }))
vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { enrolTOTP, fetchAuthSession, logout } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { authState, pageReload } from '../lib/auth.svelte'
import AuthenticatorOverlay from './AuthenticatorOverlay.svelte'

const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

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
    role: 'admin' as const,
    ssoAvailable: false,
  })
  vi.mocked(logout).mockResolvedValue(null)
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:alex?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
  })
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/auth/totp/confirm') {
        return new Response(JSON.stringify({ enabled: false, recoveryCodes: CODES, pendingConfirmation: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      throw new Error(`unexpected fetch ${url}`)
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function copyAll(ok: boolean): Promise<HTMLElement> {
  vi.mocked(copyToClipboard).mockResolvedValue(ok)
  render(AuthenticatorOverlay, { open: true })
  await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByTestId('totp-secret')
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
  await screen.findByTestId('recovery-codes')
  const btn = screen.getByRole('button', { name: /^copy all$/i })
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('AuthenticatorOverlay copy label flip (#1542 C12)', () => {
  it('flips to lowercase "copied" after a successful copy', async () => {
    const btn = await copyAll(true)
    expect(btn.textContent?.trim()).toBe('copied')
  })

  it('flips to "copy failed" after a failed copy (today it reverts to "Copy all")', async () => {
    const btn = await copyAll(false)
    expect(btn.textContent?.trim()).toBe('copy failed')
  })
})
