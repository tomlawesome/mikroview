// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U10 (note_34147): POST /api/auth/oidc/link requires the
// caller's own password (PasswordRequest), checked before the flow starts. The
// overlay asks for it before starting, sends it, and handles 401 (a wrong
// password keeps the overlay open with an error and an emptied field) and 429.
// Written before the fix.
//
// Assumptions the build may change (named in the commit message): the overlay
// itself carries the password input, labelled "your password", beside the
// existing confirm button (no second prompt screen); startSSOLink(password)
// answers INCORRECT_PASSWORD's text for a wrong password.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
  startSSOLink: vi.fn(),
}))

import { startSSOLink } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import SSOLinkOverlay from './SSOLinkOverlay.svelte'

const USER_CONFIRM = /delete my password and second step, and connect sso/i
const ADMIN_CONFIRM = /connect sso and keep my password/i

beforeEach(() => {
  vi.resetAllMocks()
  authState.showSSOLink = true
  authState.role = 'user'
})

const field = () => screen.getByLabelText(/your password/i) as HTMLInputElement

async function typeAndConfirm(confirm: RegExp, password: string | null) {
  if (password !== null) {
    await fireEvent.input(field(), { target: { value: password } })
  }
  await fireEvent.click(screen.getByRole('button', { name: confirm }))
}

describe.each([
  ['an ordinary user', 'user', USER_CONFIRM],
  ['an admin', 'admin', ADMIN_CONFIRM],
] as const)('%s linking SSO is asked for their own password (#1202 review U10)', (_who, role, confirm) => {
  beforeEach(() => {
    authState.role = role
  })

  // #1202 review U10
  it('shows a password field before anything is started', () => {
    render(SSOLinkOverlay)

    expect(field().type).toBe('password')
    expect(startSSOLink).not.toHaveBeenCalled()
  })

  // #1202 review U10
  it('sends the typed password', async () => {
    vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'hunter2')

    expect(startSSOLink).toHaveBeenCalledTimes(1)
    expect(startSSOLink).toHaveBeenCalledWith('hunter2')
  })

  // #1202 review U10: pair -- nothing typed, nothing sent.
  it('starts nothing when confirmed with no password typed', async () => {
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, null)

    expect(startSSOLink).not.toHaveBeenCalled()
    expect(authState.showSSOLink).toBe(true)
  })

  // #1202 review U10
  it('a wrong password keeps the overlay open, shows the error and empties the field', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('incorrect password')
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'wrong-one')

    expect(await screen.findByText(/incorrect password/i)).toBeTruthy()
    expect(startSSOLink).toHaveBeenCalledWith('wrong-one')
    expect(field().value).toBe('')
    expect(authState.showSSOLink).toBe(true)
    // Offered again, not left on "Redirecting…".
    expect((screen.getByRole('button', { name: confirm }) as HTMLButtonElement).disabled).toBe(false)
  })

  // #1202 review U10: pair -- the second try carries the new password and goes through.
  it('after a wrong password, a second try sends the new password', async () => {
    vi.mocked(startSSOLink)
      .mockResolvedValueOnce('incorrect password')
      .mockResolvedValueOnce({ url: 'https://idp.example/authorize' })
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'wrong-one')
    await screen.findByText(/incorrect password/i)
    await typeAndConfirm(confirm, 'right-one')

    expect(startSSOLink).toHaveBeenCalledTimes(2)
    expect(startSSOLink).toHaveBeenLastCalledWith('right-one')
  })

  // #1202 review U10
  it('a rate-limit answer says too many attempts, keeps the overlay open and empties the field', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('too many attempts, try again later')
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'hunter2')

    expect(await screen.findByText(/too many attempts/i)).toBeTruthy()
    expect(field().value).toBe('')
    expect(authState.showSSOLink).toBe(true)
  })

  // #1202 review U10: the two 409s keep their handling -- the server's words, overlay still open.
  it('a 409 refusal is shown as it is, with the overlay still open', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('this account already signs in through your identity provider')
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'hunter2')

    expect(await screen.findByText(/already signs in through your identity provider/i)).toBeTruthy()
    expect(authState.showSSOLink).toBe(true)
  })

  // #1202 review U10
  it('a dropped connection is shown, the field is usable again, and the password is not left on the page', async () => {
    vi.mocked(startSSOLink).mockRejectedValue(new Error('Failed to fetch'))
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'sw0rdfish-secret')

    expect(await screen.findByText(/failed to fetch/i)).toBeTruthy()
    // Offered again, not stuck on "Redirecting…" with a disabled field.
    expect(field().disabled).toBe(false)
    expect((screen.getByRole('button', { name: confirm }) as HTMLButtonElement).disabled).toBe(false)
    expect(new XMLSerializer().serializeToString(document.body)).not.toContain('sw0rdfish-secret')
  })

  // #1202 review U10
  it('does not leave the password on the page after a wrong one', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('incorrect password')
    render(SSOLinkOverlay)

    await typeAndConfirm(confirm, 'sw0rdfish-secret')
    await screen.findByText(/incorrect password/i)

    expect(new XMLSerializer().serializeToString(document.body)).not.toContain('sw0rdfish-secret')
  })
})

describe('cancelling never reaches the server (#1202 review U10)', () => {
  // #1202 review U10: pair.
  it('cancel with a password typed starts nothing and closes the overlay', async () => {
    render(SSOLinkOverlay)
    await fireEvent.input(field(), { target: { value: 'hunter2' } })

    await fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(startSSOLink).not.toHaveBeenCalled()
    expect(authState.showSSOLink).toBe(false)
  })
})
