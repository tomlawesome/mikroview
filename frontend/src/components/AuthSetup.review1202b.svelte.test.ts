// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U10 (note_34147), the first-run door: POST
// /api/auth/oidc/link requires the caller's own password, and the person has
// just chosen one for the admin account. The hand-off to the identity provider
// sends that password; it asks for nothing twice. Written before the fix.
//
// Assumption the build may change (named in the commit message): the password
// chosen on the creation form is the one handed to startSSOLink; there is no
// second prompt. If the fix prompts instead, these two tests change with it.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
  createUser: vi.fn(),
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  startSSOLink: vi.fn(),
}))

import { fetchAuthSession, register, startSSOLink } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { wizardJourney } from '../lib/wizardJourney.svelte'
import AuthSetup from './AuthSetup.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('location', { href: '' })
  authState.state = 'setup-required'
  authState.username = ''
  authState.role = ''
  authState.ssoAvailable = false
  wizardJourney.end()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const CODE = 'abcd-efgh-jkmn-pqrs'

async function createTheAdmin(password: string) {
  await fireEvent.click(screen.getByRole('button', { name: /enter/i }))
  await fireEvent.input(screen.getByLabelText('setup code'), { target: { value: CODE } })
  await fireEvent.input(screen.getByLabelText('account'), { target: { value: 'alex' } })
  await fireEvent.input(screen.getByLabelText('password'), { target: { value: password } })
  await fireEvent.input(screen.getByLabelText('confirm password'), { target: { value: password } })
  await fireEvent.click(screen.getByRole('button', { name: /create account/i }))
}

describe('the first-run hand-off to SSO sends the password just chosen (#1202 review U10)', () => {
  // #1202 review U10
  it('hands startSSOLink the password the admin account was created with', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(startSSOLink).mockResolvedValue({ url: 'https://idp.example/authorize' })
    authState.ssoAvailable = true

    render(AuthSetup)
    await createTheAdmin('hunter2222')

    await waitFor(() => expect(startSSOLink).toHaveBeenCalledOnce())
    expect(startSSOLink).toHaveBeenCalledWith('hunter2222')
    await waitFor(() => expect(location.href).toBe('https://idp.example/authorize'))
  })

  // #1202 review U10: pair -- no OIDC, no hand-off, so no password goes anywhere.
  it('without OIDC configured, startSSOLink is never called', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue({ setupRequired: false, authenticated: true, username: 'alex', role: 'admin', ssoAvailable: false })

    render(AuthSetup)
    await createTheAdmin('hunter2222')

    expect(await screen.findByText(/admin account created/i)).toBeTruthy()
    expect(startSSOLink).not.toHaveBeenCalled()
  })

  // #1202 review U10: a refusal of the hand-off (a wrong password cannot happen here, but a 429 can)
  // lands on the confirmation, which still says the account was created, and leaves no password on the page.
  it('a refused hand-off is shown on the confirmation, which still says the account was created, with no password left on the page', async () => {
    vi.mocked(register).mockResolvedValue(null)
    vi.mocked(startSSOLink).mockResolvedValue('too many attempts, try again later')
    authState.ssoAvailable = true

    render(AuthSetup)
    await createTheAdmin('sw0rdfish-secret')

    expect(await screen.findByText(/admin account created/i)).toBeTruthy()
    expect(screen.getByText(/too many attempts/i)).toBeTruthy()
    expect(location.href).toBe('')
    expect(new XMLSerializer().serializeToString(document.body)).not.toContain('sw0rdfish-secret')
  })
})
