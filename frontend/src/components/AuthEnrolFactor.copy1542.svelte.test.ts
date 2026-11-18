// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the enrolment door's recovery-codes "Copy all" flips its own
// label to "Copied" / "Copy failed" (sentence case: a modal overlay, C6
// #1541), then returns to "Copy all". Harness as
// AuthEnrolFactor.gauntlet.svelte.test.ts.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    fetchAuthSession: vi.fn(),
    enrolTOTP: vi.fn(),
    confirmTOTP: vi.fn(),
    confirmRecoveryCodes: vi.fn(),
    fetchMyPreferences: vi.fn(),
    saveMyPreferences: vi.fn(),
  }
})
vi.mock('../lib/passkeys.svelte', () => ({ registerPasskey: vi.fn(), loginWithPasskey: vi.fn() }))
vi.mock('../lib/qrcode', () => ({ qrCode: () => {} }))
vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { confirmTOTP, enrolTOTP, fetchMyPreferences, saveMyPreferences } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { authState, pageReload } from '../lib/auth.svelte'
import { preferencesState } from '../lib/preferences.svelte'
import AuthEnrolFactor from './AuthEnrolFactor.svelte'

const CODES = ['p2xk-9qfw', 'j6nm-4rvt', 'w8cd-1lsz', 'm3hg-7ypb', 'q9vr-5kdn', 't4bj-8wmf', 'x1sl-6czh', 'd7pn-3qgy', 'f5wt-2jrk', 'b8mz-9xvc']

beforeEach(() => {
  vi.resetAllMocks()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.state = 'must-enrol-factor'
  authState.mustEnrolSecondFactor = true
  authState.mustEnrolPasskey = false
  authState.username = 'meredith'
  authState.role = 'viewer'
  authState.hasTOTP = false
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
  })
  vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: CODES, alreadyIssued: false, pendingConfirmation: true })
})

afterEach(() => {
  vi.useRealTimers()
})

async function copyAll(ok: boolean): Promise<HTMLElement> {
  vi.mocked(copyToClipboard).mockResolvedValue(ok)
  render(AuthEnrolFactor)
  await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByLabelText('Code from the app')
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
  await screen.findByTestId('recovery-codes')
  const btn = screen.getByRole('button', { name: /^copy all$/i })
  vi.useFakeTimers()
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('AuthEnrolFactor copy label flip (#1542 C12)', () => {
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
