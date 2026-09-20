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
  // An ordinary user by default: linking is destructive for every role
  // but admin, so the base case is the plain warning.
  authState.role = 'user'
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

  // The other way the call can end: not a refusal string but a thrown
  // fetch -- the connection dropped. AuthSetup.svelte already guarded
  // its own call to startSSOLink for this; this caller did not, so the
  // throw escaped confirm() with submitting still true and left the
  // button on "Redirecting…" for good.
  it('surfaces a dropped connection instead of sticking on "Redirecting…"', async () => {
    vi.mocked(startSSOLink).mockRejectedValue(new Error('Failed to fetch'))
    render(SSOLinkOverlay)

    await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))

    expect(await screen.findByText(/failed to fetch/i)).toBeTruthy()
    // And offered again, rather than left disabled mid-flight.
    expect(screen.getByRole('button', { name: /delete my password and connect sso/i })).toBeTruthy()
    expect(authState.showSSOLink).toBe(true)
  })

  it('surfaces a refusal instead of navigating', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('this account already signs in through your identity provider')
    render(SSOLinkOverlay)

    await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
    expect(await screen.findByText(/already signs in through your identity provider/i)).toBeTruthy()
    // Still open, so the person can read what happened.
    expect(authState.showSSOLink).toBe(true)
  })

  // #1252, owner's ruling: the admin keeps its password through a link
  // (auth.Store.LinkOIDCIdentity), so the admin must not be shown the
  // deletion warning above -- it would be a warning about something
  // that does not happen, and those are the ones that teach people to
  // click past the real ones.
  describe('the admin keeps a password (#1252)', () => {
    it('tells the admin the password survives, and never warns of a deletion', () => {
      authState.role = 'admin'
      render(SSOLinkOverlay)

      expect(screen.getByText(/your MikroView password stays/i)).toBeTruthy()
      expect(screen.getByText(/extra way in rather than a replacement/i)).toBeTruthy()
      expect(screen.queryByText(/password will be deleted/i)).toBeNull()
    })

    it('labels the admin confirm with what actually happens', () => {
      authState.role = 'admin'
      render(SSOLinkOverlay)

      const confirm = screen.getByRole('button', { name: /connect sso and keep my password/i })
      // Nothing to acknowledge any more: the link costs the admin
      // nothing, so the button is live on arrival.
      expect((confirm as HTMLButtonElement).disabled).toBe(false)
      expect(screen.queryByRole('checkbox')).toBeNull()
    })

    it('keeps the deletion warning for every other role', () => {
      render(SSOLinkOverlay)
      expect(screen.getByText(/password will be deleted/i)).toBeTruthy()
      expect(screen.queryByText(/your MikroView password stays/i)).toBeNull()
    })

    it('asks the server for the link with nothing but the session', async () => {
      vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
      authState.role = 'admin'
      render(SSOLinkOverlay)

      await fireEvent.click(screen.getByRole('button', { name: /connect sso and keep my password/i }))
      expect(startSSOLink).toHaveBeenCalledWith()
    })
  })

  it('renders nothing when not open', () => {
    authState.showSSOLink = false
    const { container } = render(SSOLinkOverlay)
    expect(container.textContent).toBe('')
  })
})
