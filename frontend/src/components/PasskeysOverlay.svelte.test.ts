// SPDX-License-Identifier: AGPL-3.0-only
//
// AccountMenu's "Passkeys" overlay (#1250), cut to AuthenticatorOverlay's
// pattern: list -> adding (name, then the browser prompt) -> codes (or
// added-done when a factor already minted them) -> removing (password
// confirm), plus the four unavailable copy blocks the design gives
// verbatim.

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
    fetchPasskeys: vi.fn(),
    renamePasskey: vi.fn(),
    disablePasskey: vi.fn(),
  }
})

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

import { ApiError, disablePasskey, fetchPasskeys, renamePasskey } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
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
  // handleUnauthorized() reloads the page (jsdom has no real navigation).
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  vi.mocked(fetchPasskeys).mockResolvedValue([])
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
})

// A 401 from registerPasskey means the session behind this overlay's own
// header X/Escape/backdrop is already gone (see beginPasskeyRegistration's
// comment in lib/api.ts) -- routed the same way AuthEnrolFactor's forced-
// enrolment door routes it, rather than shown as plain error text.
describe('a session that died mid-request (401) is routed to sign-in', () => {
  it('from adding a passkey', async () => {
    authState.state = 'authenticated'
    vi.mocked(registerPasskey).mockRejectedValue(new ApiError('sign in first', 401))
    render(PasskeysOverlay, { open: true })
    await screen.findByText(/add a passkey/i)
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })

    await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
  })
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

// Closing mid-ceremony doesn't cancel it, it only unmounts the view -- a
// registerPasskey() that turns out to mint fresh recovery codes would
// then have nowhere left open to show them, lost for good on reload.
describe('closing is blocked while a request is in flight', () => {
  async function openAddingWithPendingRegister() {
    let resolveRegister: (v: { passkey: PasskeySummary; recoveryCodes: string[] | null }) => void
    vi.mocked(registerPasskey).mockReturnValue(
      new Promise((resolve) => {
        resolveRegister = resolve
      }),
    )
    render(PasskeysOverlay, { open: true })
    await screen.findByText(/add a passkey/i)
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
    await screen.findByText(/waiting for your browser/i)
    return (v: { passkey: PasskeySummary; recoveryCodes: string[] | null }) => resolveRegister(v)
  }

  it('Escape does not close the dialog while registerPasskey() is pending', async () => {
    await openAddingWithPendingRegister()

    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.getByRole('dialog', { name: /passkeys/i })).toBeTruthy()
  })

  it('the backdrop click does not close the dialog while registerPasskey() is pending', async () => {
    await openAddingWithPendingRegister()

    await fireEvent.click(document.querySelector('.backdrop')!)

    expect(screen.getByRole('dialog', { name: /passkeys/i })).toBeTruthy()
  })

  it('the header close button is disabled while registerPasskey() is pending', async () => {
    await openAddingWithPendingRegister()

    expect(screen.getByRole('button', { name: /^close$/i })).toHaveProperty('disabled', true)
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

  it('removes the row and drops the count on the right password, when the authenticator app still stands', async () => {
    authState.passkeyCount = 1
    authState.hasTOTP = true
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    vi.mocked(disablePasskey).mockResolvedValue({ signedOut: false })
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /remove passkey/i }))

    expect(await screen.findByText(/add a passkey/i)).toBeTruthy()
    expect(authState.passkeyCount).toBe(0)
  })

  // #1253's own ruling: an account with no other factor may still remove
  // its last passkey -- that just costs it the account's last second
  // factor, which the server answers by signing the caller out
  // everywhere rather than leaving a half-protected session up.
  it('warns before removing the account\'s only second factor, not "you\'ll need another way in"', async () => {
    authState.passkeyCount = 1
    authState.hasTOTP = false
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))

    expect(screen.getByText(/signs you out now/i)).toBeTruthy()
  })

  it('does not warn of signing out when the authenticator app will still stand', async () => {
    authState.passkeyCount = 1
    authState.hasTOTP = true
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))

    expect(screen.getByText(/your authenticator app will still be asked for/i)).toBeTruthy()
    expect(screen.queryByText(/signs you out now/i)).toBeNull()
  })

  it('does not warn of signing out when another passkey will still stand', async () => {
    authState.passkeyCount = 2
    authState.hasTOTP = false
    vi.mocked(fetchPasskeys).mockResolvedValue([row(), row({ id: 'cred-2', name: 'phone' })])
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getAllByRole('button', { name: /remove/i })[0])

    expect(screen.getByText(/your other passkeys will still be asked for/i)).toBeTruthy()
    expect(screen.queryByText(/signs you out now/i)).toBeNull()
  })

  it('signs out through authState the way logout does, rather than returning to the list, when this was the last factor', async () => {
    authState.passkeyCount = 1
    authState.hasTOTP = false
    vi.mocked(fetchPasskeys).mockResolvedValue([row()])
    vi.mocked(disablePasskey).mockResolvedValue({ signedOut: true })
    render(PasskeysOverlay, { open: true })
    await screen.findByText('this laptop')

    await fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'correct-horse' } })
    await fireEvent.click(screen.getByRole('button', { name: /remove passkey/i }))

    await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
    expect(authState.state).toBe('unauthenticated')
  })
})
