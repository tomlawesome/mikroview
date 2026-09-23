// SPDX-License-Identifier: AGPL-3.0-only
//
// AccountMenu's "Passkeys" overlay (#1250), cut to AuthenticatorOverlay's
// pattern: list -> adding (name, then the browser prompt) -> codes (or
// added-done when a factor already minted them) -> removing (password
// confirm), plus the four unavailable copy blocks the design gives
// verbatim.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchPasskeys: vi.fn(),
  renamePasskey: vi.fn(),
  disablePasskey: vi.fn(),
}))

// The browser ceremony itself (navigator.credentials.create) is
// lib/passkeys.svelte.ts's own job, covered by its own test file --
// this component only has to do the right thing with whatever that
// module hands back.
vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { disablePasskey, fetchPasskeys, renamePasskey } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState } from '../lib/auth.svelte'
import type { PasskeySummary } from '../lib/types'
import PasskeysOverlay from './PasskeysOverlay.svelte'

function row(overrides: Partial<PasskeySummary> = {}): PasskeySummary {
  return {
    id: 'cred-1',
    name: 'this laptop',
    createdAt: '2026-09-01T00:00:00Z',
    lastUsedAt: '2026-09-20T00:00:00Z',
    transports: ['internal'],
    stale: false,
    rpId: 'mikroview.example.org',
    ...overrides,
  }
}

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.mocked(fetchPasskeys).mockResolvedValue([])
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
})

describe('the four unavailable copy blocks', () => {
  it('says why for status unset, and never fetches the list', async () => {
    authState.passkeyStatus = 'unset'
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/bare ip address can never hold one/i)).toBeTruthy()
    expect(fetchPasskeys).not.toHaveBeenCalled()
  })

  it('says why for status ip', async () => {
    authState.passkeyStatus = 'ip'
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/browsers refuse to create a passkey for an ip/i)).toBeTruthy()
  })

  it('says why for status insecure', async () => {
    authState.passkeyStatus = 'insecure'
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/browsers only offer passkeys over https/i)).toBeTruthy()
  })

  it('says why for a capable server at the wrong address for this browser', async () => {
    authState.passkeyStatus = 'ready'
    authState.passkeyOrigin = 'https://mikroview.elsewhere.example'
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/passkeys live at/i)).toBeTruthy()
    expect(screen.getByRole('link', { name: /mikroview\.elsewhere\.example/i })).toBeTruthy()
  })

  it('every unavailable state still offers a way to close', async () => {
    authState.passkeyStatus = 'unset'
    render(PasskeysOverlay, { open: true })

    await screen.findByText(/web address/i)
    // Both the header X and the footer's explicit Close are offered here
    // -- nothing sensitive is ever shown on this screen, unlike the
    // codes step.
    expect(screen.getAllByRole('button', { name: /close/i }).length).toBe(2)
  })
})

describe('the list step', () => {
  it('fetches and shows every registered passkey', async () => {
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText('this laptop')).toBeTruthy()
    expect(fetchPasskeys).toHaveBeenCalled()
  })

  it('offers nothing to remove but says so plainly with no passkeys yet', async () => {
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/add a passkey/i)).toBeTruthy()
  })

  it('tags a stale row and offers only remove on it, no rename', async () => {
    vi.mocked(fetchPasskeys).mockResolvedValue([row({ id: 'cred-2', name: 'old phone', stale: true, rpId: 'old.example.org' })])
    render(PasskeysOverlay, { open: true })

    expect(await screen.findByText(/made for old\.example\.org — won't work here/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /rename old phone/i })).toBeNull()
    expect(screen.getByRole('button', { name: /remove/i })).toBeTruthy()
  })

  it('renames a passkey in place', async () => {
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    vi.mocked(renamePasskey).mockResolvedValue(null)
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /rename this laptop/i }))
    const input = screen.getByLabelText(/rename this laptop/i)
    await fireEvent.input(input, { target: { value: 'work phone' } })
    await fireEvent.keyDown(input, { key: 'Enter' })

    expect(renamePasskey).toHaveBeenCalledWith('cred-1', 'work phone')
    expect(await screen.findByText('work phone')).toBeTruthy()
  })
})

describe('adding a passkey', () => {
  async function openAdding() {
    render(PasskeysOverlay, { open: true })
    await screen.findByText(/add a passkey/i)
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  }

  it('names it, runs the ceremony, and shows fresh recovery codes when this is the first factor', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({
      passkey: row(),
      recoveryCodes: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'],
    })
    await openAdding()

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))

    expect(registerPasskey).toHaveBeenCalledWith('this laptop')
    const block = await screen.findByTestId('recovery-codes')
    expect(block.textContent).toContain('a')
    expect(authState.passkeyCount).toBe(1)
  })

  it('says the existing codes still stand, with no blank grid, when a factor already minted them', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: row(), recoveryCodes: null })
    await openAdding()

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))

    expect(screen.queryByTestId('recovery-codes')).toBeNull()
    expect(await screen.findByText(/already issued/i)).toBeTruthy()
  })

  it('shows the ceremony\'s own refusal and stays put to retry', async () => {
    vi.mocked(registerPasskey).mockResolvedValue("That didn't complete -- try again, or use another way in.")
    await openAdding()

    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))

    expect(await screen.findByText(/didn't complete/i)).toBeTruthy()
    expect(authState.passkeyCount).toBe(0)
  })
})

describe('removing a passkey', () => {
  it('is refused server-side and shown inline, leaving the row in place', async () => {
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    vi.mocked(disablePasskey).mockResolvedValue('wrong password')
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: /remove passkey/i }))

    expect(disablePasskey).toHaveBeenCalledWith('cred-1', 'wrong')
    expect(await screen.findByText('wrong password')).toBeTruthy()
    // Still on the removing screen, naming the same row -- nothing was
    // removed.
    expect(screen.getByText(/this removes "this laptop"/i)).toBeTruthy()
  })

  it('removes the row and drops the count on the right password', async () => {
    authState.passkeyCount = 1
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    vi.mocked(disablePasskey).mockResolvedValue(null)
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /remove passkey/i }))

    expect(await screen.findByText(/add a passkey/i)).toBeTruthy()
    expect(authState.passkeyCount).toBe(0)
  })
})
