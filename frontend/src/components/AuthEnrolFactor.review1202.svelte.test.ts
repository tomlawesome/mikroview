// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U1, U2, U3, U6 (note_34034), the forced-enrolment door.
// Written from the fix list alone, before the fix.
//
//  - U1: a refused recovery-codes confirm must not strand the person.
//    401 step-expired and 409 go back to choosing a factor with a "start
//    again" message; 401 sign-in-required signs out; a network error shows an
//    error and leaves "I have saved these" usable.
//  - U2: on the passkey-only door, when this address cannot make a passkey,
//    there is a link to the address that can (passkeyOrigin) and a sign-out.
//  - U3: a 401 step-expired from the authenticator-app confirm is "start
//    again", not a dead session.
//  - U6: no "every other session ... ended" or "turns this on" wording until
//    the factor is confirmed.
//
// confirmRecoveryCodes and confirmTOTP are the REAL api.ts functions here,
// answering Problem Details responses from a stubbed fetch, so the tests do
// not depend on how the fix chooses to report each refusal internally.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    fetchAuthSession: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    register: vi.fn(),
    setNewPasswordAfterReset: vi.fn(),
    signOutEverywhere: vi.fn(),
    submitLoginFactor: vi.fn(),
    enrolTOTP: vi.fn(),
    fetchPersistence: vi.fn(),
    fetchMyPreferences: vi.fn(),
    saveMyPreferences: vi.fn(),
  }
})

vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
  loginWithPasskey: vi.fn(),
  passkeysSupported: vi.fn(() => true),
}))

vi.mock('../lib/qrcode', () => ({
  qrCode: () => {},
}))

import { enrolTOTP, fetchAuthSession, fetchMyPreferences, logout, saveMyPreferences } from '../lib/api'
import { passkeysSupported, registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
import { preferencesState } from '../lib/preferences.svelte'
import AuthEnrolFactor from './AuthEnrolFactor.svelte'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'
const CONFIRM_URL = '/api/auth/recovery-codes/confirm'
const TOTP_CONFIRM_URL = '/api/auth/totp/confirm'

const TEN_CODES = [
  'p2xk-9qfw',
  'j6nm-4rvt',
  'w8cd-1lsz',
  'm3hg-7ypb',
  'q9vr-5kdn',
  't4bj-8wmf',
  'x1sl-6czh',
  'd7pn-3qgy',
  'f5wt-2jrk',
  'b8mz-9xvc',
]

const PASSKEY = {
  id: 'cred-1',
  name: 'this laptop',
  createdAt: '2026-10-10T10:00:00Z',
  transports: [],
  stale: false,
  rpId: 'view.brandt.example',
}

const OPEN_SESSION = {
  setupRequired: false,
  authenticated: true,
  username: 'meredith',
  role: 'admin' as const,
  mustEnrolSecondFactor: false,
  mustEnrolPasskey: false,
  ssoAvailable: false,
}

const SIGNED_OUT_SESSION = { setupRequired: false, authenticated: false, ssoAvailable: false }

function problem(status: number, cls: string, detail: string): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

// Routes the two calls the door makes for real; anything else is a test bug.
function stubNetwork(routes: Record<string, () => Response | Promise<Response>>) {
  const fetchMock = vi.fn(async (url: string) => {
    const route = routes[url]
    if (!route) throw new Error(`unexpected fetch ${url}`)
    return route()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

async function flush() {
  await new Promise((r) => setTimeout(r, 0))
}

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
  vi.mocked(logout).mockResolvedValue(null)
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function enterTotpStage() {
  await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByLabelText('Code from the app')
}

async function submitCode() {
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
}

// Reaches the held codes screen through the authenticator app: the app's code
// is accepted and the server says the factor is on hold.
async function reachHeldCodesViaApp(routes: Record<string, () => Response | Promise<Response>>) {
  stubNetwork({
    [TOTP_CONFIRM_URL]: () => ok({ enabled: false, recoveryCodes: TEN_CODES, pendingConfirmation: true }),
    ...routes,
  })
  render(AuthEnrolFactor)
  await enterTotpStage()
  await submitCode()
  await screen.findByTestId('recovery-codes')
}

async function enterPasskeyStage() {
  const keys = screen.queryAllByRole('button', { name: /set it up/i })
  if (keys.length > 0) await fireEvent.click(keys[keys.length - 1])
  await screen.findByLabelText('Name')
}

async function reachHeldCodesViaPasskey(routes: Record<string, () => Response | Promise<Response>>) {
  stubNetwork(routes)
  vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: TEN_CODES, pendingConfirmation: true })
  render(AuthEnrolFactor)
  await enterPasskeyStage()
  await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  await screen.findByTestId('recovery-codes')
}

async function saveTheCodes() {
  await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
  await flush()
}

// Text of every live region and alert in the document.
function announced(): string {
  return Array.from(document.querySelectorAll('[role="alert"], [role="status"], [aria-live]'))
    .map((el) => el.textContent ?? '')
    .join(' ')
}

function somethingSaysItFailed(): boolean {
  return /network|failed|couldn|could not|error|try again|unreachable|offline|not reach/i.test(
    announced() + ' ' + document.body.textContent,
  )
}

// Back at the first choice, with the codes gone and the person told to start again.
async function expectSentBackToChoosing() {
  await vi.waitFor(() => expect(screen.queryByTestId('recovery-codes')).toBeNull())
  expect(screen.getAllByText(/start again/i).length).toBeGreaterThan(0)
  expect(screen.getByRole('button', { name: /authenticator app/i })).toBeTruthy()
  // Not signed out, and the door still holds.
  expect(authState.state).toBe('must-enrol-factor')
}

// U1 -- the codes screen's confirm is refused.
describe('a refused recovery-codes confirm does not strand the person (#1202 review U1)', () => {
  // #1202 review U1
  it('401 step-expired: goes back to choosing a factor with a "start again" message', async () => {
    await reachHeldCodesViaApp({ [CONFIRM_URL]: () => problem(401, 'step-expired', 'the hold ran out') })
    await saveTheCodes()
    await expectSentBackToChoosing()
  })

  // #1202 review U1
  it('409 (nothing is held): goes back to choosing a factor with a "start again" message', async () => {
    await reachHeldCodesViaApp({
      [CONFIRM_URL]: () => problem(409, 'conflict', 'no second factor is waiting to be confirmed'),
    })
    await saveTheCodes()
    await expectSentBackToChoosing()
  })

  // #1202 review U1: the same exit on a passkey that was held.
  it('401 step-expired after a held passkey: goes back to choosing a factor with a "start again" message', async () => {
    await reachHeldCodesViaPasskey({ [CONFIRM_URL]: () => problem(401, 'step-expired', 'the hold ran out') })
    await saveTheCodes()
    await expectSentBackToChoosing()
  })

  // #1202 review U1
  it('401 sign-in-required: signs out', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(SIGNED_OUT_SESSION)
    await reachHeldCodesViaApp({ [CONFIRM_URL]: () => problem(401, 'sign-in-required', 'sign in first') })
    await saveTheCodes()
    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
  })

  // #1202 review U1: pair for the three above -- an accepted confirm opens the
  // door and shows no "start again".
  it('a confirm that succeeds opens the door and does not tell the person to start again', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)
    await reachHeldCodesViaApp({ [CONFIRM_URL]: () => ok({ confirmed: true, factor: 'totp' }) })
    await saveTheCodes()
    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(screen.queryByText(/start again/i)).toBeNull()
  })

  // #1202 review U1
  it('a network error shows an error, keeps the codes on screen and leaves "I have saved these" usable', async () => {
    let attempt = 0
    await reachHeldCodesViaApp({
      [CONFIRM_URL]: () => {
        attempt++
        if (attempt === 1) throw new TypeError('Failed to fetch')
        return ok({ confirmed: true, factor: 'totp' })
      },
    })
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    // Nothing says it failed until it has.
    expect(somethingSaysItFailed()).toBe(false)

    await saveTheCodes()

    await vi.waitFor(() => expect(somethingSaysItFailed()).toBe(true))
    expect(screen.getByTestId('recovery-codes')).toBeTruthy()
    const button = screen.getByRole('button', { name: /i have saved these/i }) as HTMLButtonElement
    expect(button.disabled).toBe(false)
    expect(button.getAttribute('aria-disabled')).not.toBe('true')
    expect(authState.state).toBe('must-enrol-factor')

    // Usable means a second press goes through.
    await fireEvent.click(button)
    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
  })
})

// U3 -- the authenticator-app confirm's step-expired.
describe('a step-expired authenticator-app confirm is "start again" (#1202 review U3)', () => {
  // #1202 review U3
  it('401 step-expired on the code: the person is told to start again and is not signed out', async () => {
    stubNetwork({ [TOTP_CONFIRM_URL]: () => problem(401, 'step-expired', 'the secret was set more than ten minutes ago') })
    render(AuthEnrolFactor)
    await enterTotpStage()
    await submitCode()

    await vi.waitFor(() => expect(screen.getAllByText(/start again/i).length).toBeGreaterThan(0))
    expect(authState.state).toBe('must-enrol-factor')
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
  })

  // #1202 review U3: pair -- a 401 sign-in-required there IS a dead session.
  it('401 sign-in-required on the code: signs out, and does not say "start again"', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue(SIGNED_OUT_SESSION)
    stubNetwork({ [TOTP_CONFIRM_URL]: () => problem(401, 'sign-in-required', 'sign in first') })
    render(AuthEnrolFactor)
    await enterTotpStage()
    await submitCode()

    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
    expect(screen.queryByText(/start again/i)).toBeNull()
  })
})

// U2 -- the passkey-only door at an address that cannot make a passkey.
describe('the passkey-only door at an address that cannot make a passkey (#1202 review U2)', () => {
  beforeEach(() => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolSecondFactor = false
    authState.mustEnrolPasskey = true
    authState.role = 'admin'
  })

  // #1202 review U2
  it('wrong address: links to the address that can (passkeyOrigin) and offers a sign-out', () => {
    authState.passkeyStatus = 'ready'
    authState.passkeyOrigin = 'https://mikroview.elsewhere.example'

    render(AuthEnrolFactor)

    const link = screen.getByRole('link', { name: /mikroview\.elsewhere\.example/i })
    expect(link.getAttribute('href')).toMatch(/^https:\/\/mikroview\.elsewhere\.example/)
    expect(screen.getByRole('button', { name: /sign out|log out/i })).toBeTruthy()
  })

  // #1202 review U2
  it('a bare-IP address with a passkey address known: links to it and offers a sign-out', () => {
    authState.passkeyStatus = 'ip'
    authState.passkeyOrigin = 'https://mikroview.elsewhere.example'

    render(AuthEnrolFactor)

    expect(screen.getByRole('link', { name: /mikroview\.elsewhere\.example/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /sign out|log out/i })).toBeTruthy()
  })

  // #1202 review U2: with no address to offer there is nothing to link to,
  // but the person must still have a way out.
  it('no passkey address at all: offers a sign-out and no link', () => {
    authState.passkeyStatus = 'unset'
    authState.passkeyOrigin = undefined

    const { container } = render(AuthEnrolFactor)

    expect(container.querySelector('a')).toBeNull()
    expect(screen.getByRole('button', { name: /sign out|log out/i })).toBeTruthy()
  })

  // #1202 review U2
  it('the sign-out signs out', async () => {
    authState.passkeyStatus = 'ready'
    authState.passkeyOrigin = 'https://mikroview.elsewhere.example'
    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /sign out|log out/i }))

    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
    expect(logout).toHaveBeenCalled()
  })

  // #1202 review U2: pair -- where a passkey CAN be made here, the door has no
  // link out and no sign-out, as before.
  it('an address that can make a passkey: no link and no sign-out', () => {
    authState.passkeyStatus = 'ready'
    authState.passkeyOrigin = location.origin

    const { container } = render(AuthEnrolFactor)

    expect(container.querySelector('a')).toBeNull()
    expect(screen.queryByRole('button', { name: /sign out|log out/i })).toBeNull()
  })
})

// A1-S1 -- a browser that cannot create a passkey.
describe('the passkey-only door in a browser that cannot make a passkey (v0.7.0 audit A1-S1)', () => {
  beforeEach(() => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolSecondFactor = false
    authState.mustEnrolPasskey = true
    authState.role = 'admin'
    authState.passkeyStatus = 'ready'
    authState.passkeyOrigin = location.origin
  })

  it('unsupported browser at the right address: the Passkey key is off with the reason, and there is a sign-out', () => {
    vi.mocked(passkeysSupported).mockReturnValue(false)

    const { container } = render(AuthEnrolFactor)

    expect(screen.queryByRole('button', { name: /passkey/i })).toBeNull()
    expect(screen.getByText(/this browser can.t create a passkey/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /sign out|log out/i })).toBeTruthy()
    expect(container.querySelector('a')).toBeNull()
    expect(container.querySelector('section.state')?.getAttribute('aria-label')).toBe(
      'Add a passkey: passkeys unavailable in this browser',
    )
  })

  it('the sign-out on that door signs out', async () => {
    vi.mocked(passkeysSupported).mockReturnValue(false)
    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /sign out|log out/i }))

    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
    expect(logout).toHaveBeenCalled()
  })

  // Legitimate path: must not be over-gated.
  it('capable browser at the right address: the Passkey key is live and there is no sign-out', () => {
    vi.mocked(passkeysSupported).mockReturnValue(true)

    const { container } = render(AuthEnrolFactor)

    expect(screen.getByRole('button', { name: /passkey/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /sign out|log out/i })).toBeNull()
    expect(screen.queryByText(/can.t create a passkey/i)).toBeNull()
    expect(container.querySelector('a')).toBeNull()
  })
})

// A1-S1 -- the two-key door.
it('two-key door in an unsupported browser: the Passkey key is off and points at the authenticator app, which stays live', () => {
  vi.mocked(passkeysSupported).mockReturnValue(false)

  const { container } = render(AuthEnrolFactor)

  const app = screen.getByRole('button', { name: /authenticator app/i }) as HTMLButtonElement
  expect(app.disabled).toBe(false)
  expect(screen.queryByRole('button', { name: /^passkey/i })).toBeNull()
  expect(screen.getByText(/try updating it, or use your authenticator app instead/i)).toBeTruthy()
  expect(screen.queryByRole('button', { name: /sign out|log out/i })).toBeNull()
  expect(container.querySelector('section.state')?.getAttribute('aria-label')).toBe(
    'Add a second step: passkeys unavailable in this browser',
  )
})

// U6 -- wording that is only true once the factor is confirmed.
describe('no "every other session ended" wording before the factor is confirmed (#1202 review U6)', () => {
  const PREMATURE = /every other session|other sessions|turns this on|turns it on/i

  // #1202 review U6
  it('the authenticator-app prove-it screen does not say it ends other sessions or turns anything on', async () => {
    render(AuthEnrolFactor)
    await enterTotpStage()
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6
  it('the held codes screen (authenticator app) does not say it ends other sessions or turns anything on', async () => {
    await reachHeldCodesViaApp({})
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6
  it('the held codes screen (passkey) does not say it ends other sessions or turns anything on', async () => {
    await reachHeldCodesViaPasskey({})
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6: pair -- the screen still carries the way to confirm.
  it('the held codes screen still offers "I have saved these"', async () => {
    await reachHeldCodesViaApp({})
    expect(screen.getByRole('button', { name: /i have saved these/i })).toBeTruthy()
  })
})
