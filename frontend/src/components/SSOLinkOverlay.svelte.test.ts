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
  // An ordinary user by default: the deployment keeps its local admin
  // whatever this person does, so the base case is the plain warning.
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

  it('surfaces a refusal instead of navigating', async () => {
    vi.mocked(startSSOLink).mockResolvedValue('this account already signs in through your identity provider')
    render(SSOLinkOverlay)

    await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
    expect(await screen.findByText(/already signs in through your identity provider/i)).toBeTruthy()
    // Still open, so the person can read what happened.
    expect(authState.showSSOLink).toBe(true)
  })

  // #1252: mikroview holds exactly one admin, so the admin linking
  // theirs is the link that costs the deployment its only way in that
  // does not need the identity provider. That needs saying, and it
  // needs its own confirm -- the first warning has already been read
  // past by the time this one matters.
  describe('the last local admin (#1252)', () => {
    it('says a user link costs the deployment nothing extra', () => {
      render(SSOLinkOverlay)
      expect(screen.queryByText(/only account that can sign in without SSO/i)).toBeNull()
      expect(screen.queryByRole('checkbox')).toBeNull()
    })

    it('warns the admin that nobody would be left who can sign in without SSO', () => {
      authState.role = 'admin'
      render(SSOLinkOverlay)

      expect(screen.getByText(/only account that can sign in without SSO/i)).toBeTruthy()
      expect(screen.getByText(/nobody can sign in to MikroView at all/i)).toBeTruthy()
      expect(screen.getByText(/-transfer-admin/)).toBeTruthy()
    })

    it('holds the confirm shut until the admin acknowledges it', async () => {
      authState.role = 'admin'
      render(SSOLinkOverlay)

      const confirm = screen.getByRole('button', { name: /delete my password and connect sso/i })
      expect((confirm as HTMLButtonElement).disabled).toBe(true)

      await fireEvent.click(screen.getByRole('checkbox'))
      expect((confirm as HTMLButtonElement).disabled).toBe(false)
    })

    it('sends the acknowledgement, so the server can tell a confirm from a click', async () => {
      vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
      authState.role = 'admin'
      render(SSOLinkOverlay)

      await fireEvent.click(screen.getByRole('checkbox'))
      await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
      expect(startSSOLink).toHaveBeenCalledWith(true)
    })

    it('does not claim an acknowledgement a user never gave', async () => {
      vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
      render(SSOLinkOverlay)

      await fireEvent.click(screen.getByRole('button', { name: /delete my password and connect sso/i }))
      expect(startSSOLink).toHaveBeenCalledWith(false)
    })
  })

  it('renders nothing when not open', () => {
    authState.showSSOLink = false
    const { container } = render(SSOLinkOverlay)
    expect(container.textContent).toBe('')
  })
})
