// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

// Only the network boundary is faked, so this exercises the real
// component markup -- which is the point, since what's being guarded
// here is what the person actually reads before committing.
vi.mock('../lib/api', () => ({
  startSSOLink: vi.fn(),
}))

import { startSSOLink } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import SSOLinkOverlay from './SSOLinkOverlay.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  authState.showSSOLink = true
})

describe('SSOLinkOverlay', () => {
  // Linking permanently destroys the account's password and can't be
  // undone from MikroView. The issue's requirement is that this is said
  // plainly *before* the person confirms -- not afterwards, and not
  // only in the docs.
  it('warns that the password will be deleted, before anything is confirmed', () => {
    render(SSOLinkOverlay)

    expect(screen.getByText(/password will be deleted/i)).toBeTruthy()
    expect(screen.getByText(/can't be undone/i)).toBeTruthy()
    expect(startSSOLink).not.toHaveBeenCalled()
  })

  // A button labelled "OK" or "Confirm" would let someone click through
  // without reading. The label has to name the consequence.
  it('labels the confirm button with what it does', () => {
    render(SSOLinkOverlay)
    expect(screen.getByRole('button', { name: /delete my password and connect sso/i })).toBeTruthy()
  })

  it('does nothing at all when cancelled', async () => {
    render(SSOLinkOverlay)
    await fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(startSSOLink).not.toHaveBeenCalled()
    expect(authState.showSSOLink).toBe(false)
  })

  it('starts the flow only once the confirm is clicked', async () => {
    vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
    render(SSOLinkOverlay)

    await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
    expect(startSSOLink).toHaveBeenCalledOnce()
  })

  it('surfaces a refusal instead of navigating', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('this account already signs in through your identity provider')
    render(SSOLinkOverlay)

    await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
    expect(await screen.findByText(/already signs in through your identity provider/i)).toBeTruthy()
    // Still open, so the person can read what happened.
    expect(authState.showSSOLink).toBe(true)
  })

  it('renders nothing when not open', () => {
    authState.showSSOLink = false
    const { container } = render(SSOLinkOverlay)
    expect(container.textContent).toBe('')
  })
})
