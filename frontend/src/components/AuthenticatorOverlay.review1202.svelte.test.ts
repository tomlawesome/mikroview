// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U1, U3, U6 (note_34034), the account menu's authenticator-app
// overlay. Written from the fix list alone, before the fix.
//
//  - U1: a refused recovery-codes confirm must not strand the person: 401
//    step-expired and 409 go back to the start of the overlay (or close it)
//    with a "start again" message; 401 sign-in-required signs out; a network
//    error shows an error and leaves "I have saved these" usable.
//  - U3: a 401 step-expired from the code confirm is "start again", not a
//    dead session.
//  - U6: no "every other session ... ended" / "turns this on" wording before
//    the factor is confirmed.
//
// confirmTOTP and confirmRecoveryCodes are the real api.ts functions
// answering Problem Details responses from a stubbed fetch.
//
// Untestable here: if the fix reports "start again" through a toast that
// outlives a closed overlay, that toast is outside this component's DOM. The
// message is therefore only required in the overlay while it stays open.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    disableTOTP: vi.fn(),
    enrolTOTP: vi.fn(),
    fetchAuthSession: vi.fn(),
    logout: vi.fn(),
  }
})

vi.mock('../lib/qrcode', () => ({
  qrCode: () => ({ update: () => {}, destroy: () => {} }),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { enrolTOTP, fetchAuthSession, logout } from '../lib/api'
import { authState, pageReload } from '../lib/auth.svelte'
import AuthenticatorOverlay from './AuthenticatorOverlay.svelte'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'
const CONFIRM_URL = '/api/auth/recovery-codes/confirm'
const TOTP_CONFIRM_URL = '/api/auth/totp/confirm'
const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

const SESSION = {
  setupRequired: false,
  authenticated: true,
  username: 'alex',
  role: 'admin' as const,
  ssoAvailable: false,
}

function problem(status: number, cls: string, detail: string): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function stubNetwork(routes: Record<string, () => Response | Promise<Response>>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const route = routes[url]
      if (!route) throw new Error(`unexpected fetch ${url}`)
      return route()
    }),
  )
}

async function flush() {
  await new Promise((r) => setTimeout(r, 0))
}

beforeEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.state = 'authenticated'
  authState.hasTOTP = false
  vi.mocked(fetchAuthSession).mockResolvedValue(SESSION)
  vi.mocked(logout).mockResolvedValue(null)
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:alex?secret=JBSWY3DPEHPK3PXP&issuer=MikroView',
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function reachProveIt() {
  render(AuthenticatorOverlay, { open: true })
  await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByTestId('totp-secret')
}

async function submitCode() {
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
}

async function reachHeldCodes(routes: Record<string, () => Response | Promise<Response>>) {
  stubNetwork({
    [TOTP_CONFIRM_URL]: () => ok({ enabled: false, recoveryCodes: CODES, pendingConfirmation: true }),
    ...routes,
  })
  await reachProveIt()
  await submitCode()
  await screen.findByTestId('recovery-codes')
}

async function saveTheCodes() {
  await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
  await flush()
}

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

// The codes are gone; the overlay is either closed or open at a place with a
// way forward and the "start again" message.
async function expectSentBackToStart() {
  await vi.waitFor(() => expect(screen.queryByTestId('recovery-codes')).toBeNull())
  const dialog = screen.queryByRole('dialog', { name: /authenticator app/i })
  if (dialog) {
    expect(within(dialog).getAllByText(/start again/i).length).toBeGreaterThan(0)
    expect(within(dialog).getAllByRole('button', { name: /set up authenticator app|close/i }).length).toBeGreaterThan(0)
  }
  expect(authState.state).toBe('authenticated')
}

describe('a refused recovery-codes confirm does not strand the person (#1202 review U1)', () => {
  // #1202 review U1
  it('401 step-expired: goes back to the start (or closes) with a "start again" message', async () => {
    await reachHeldCodes({ [CONFIRM_URL]: () => problem(401, 'step-expired', 'the hold ran out') })
    await saveTheCodes()
    await expectSentBackToStart()
  })

  // #1202 review U1
  it('409 (nothing is held): goes back to the start (or closes) with a "start again" message', async () => {
    await reachHeldCodes({
      [CONFIRM_URL]: () => problem(409, 'conflict', 'no second factor is waiting to be confirmed'),
    })
    await saveTheCodes()
    await expectSentBackToStart()
  })

  // #1202 review U1
  it('401 sign-in-required: signs out', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue({ setupRequired: false, authenticated: false, ssoAvailable: false })
    await reachHeldCodes({ [CONFIRM_URL]: () => problem(401, 'sign-in-required', 'sign in first') })
    await saveTheCodes()
    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
  })

  // #1202 review U1: pair -- an accepted confirm closes the overlay, signed in, no "start again".
  it('a confirm that succeeds closes the overlay and does not say "start again"', async () => {
    await reachHeldCodes({ [CONFIRM_URL]: () => ok({ confirmed: true, factor: 'totp' }) })
    await saveTheCodes()
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull())
    expect(screen.queryByText(/start again/i)).toBeNull()
    expect(authState.state).toBe('authenticated')
  })

  // #1202 review U1
  it('a network error shows an error, keeps the codes on screen and leaves "I have saved these" usable', async () => {
    let attempt = 0
    await reachHeldCodes({
      [CONFIRM_URL]: () => {
        attempt++
        if (attempt === 1) throw new TypeError('Failed to fetch')
        return ok({ confirmed: true, factor: 'totp' })
      },
    })

    // Nothing says it failed until it has.
    expect(somethingSaysItFailed()).toBe(false)

    await saveTheCodes()

    await vi.waitFor(() => expect(somethingSaysItFailed()).toBe(true))
    expect(screen.getByRole('dialog', { name: /authenticator app/i })).toBeTruthy()
    expect(screen.getByTestId('recovery-codes')).toBeTruthy()
    const button = screen.getByRole('button', { name: /i have saved these/i }) as HTMLButtonElement
    expect(button.disabled).toBe(false)
    expect(button.getAttribute('aria-disabled')).not.toBe('true')

    // Usable means a second press goes through.
    await fireEvent.click(button)
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /authenticator app/i })).toBeNull())
  })
})

describe('a step-expired code confirm is "start again" (#1202 review U3)', () => {
  // #1202 review U3
  it('401 step-expired on the code: told to start again, not signed out', async () => {
    stubNetwork({ [TOTP_CONFIRM_URL]: () => problem(401, 'step-expired', 'the secret was set more than ten minutes ago') })
    await reachProveIt()
    await submitCode()

    await vi.waitFor(() => expect(screen.getAllByText(/start again/i).length).toBeGreaterThan(0))
    expect(authState.state).toBe('authenticated')
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
  })

  // #1202 review U3: pair.
  it('401 sign-in-required on the code: signs out, and does not say "start again"', async () => {
    vi.mocked(fetchAuthSession).mockResolvedValue({ setupRequired: false, authenticated: false, ssoAvailable: false })
    stubNetwork({ [TOTP_CONFIRM_URL]: () => problem(401, 'sign-in-required', 'sign in first') })
    await reachProveIt()
    await submitCode()

    await vi.waitFor(() => expect(authState.state).toBe('unauthenticated'))
    expect(screen.queryByText(/start again/i)).toBeNull()
  })
})

describe('no "every other session ended" wording before the factor is confirmed (#1202 review U6)', () => {
  const PREMATURE = /every other session|other sessions|turns this on|turns it on/i

  // #1202 review U6
  it('the password screen says nothing about ending other sessions or turning anything on', async () => {
    render(AuthenticatorOverlay, { open: true })
    await fireEvent.click(screen.getByRole('button', { name: /set up authenticator app/i }))
    await screen.findByLabelText('Password')
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6
  it('the prove-it screen says nothing about ending other sessions or turning anything on', async () => {
    await reachProveIt()
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6
  it('the held codes screen says nothing about ending other sessions or turning anything on', async () => {
    await reachHeldCodes({})
    expect(document.body.textContent).not.toMatch(PREMATURE)
  })

  // #1202 review U6: pair -- the screen still carries the way to confirm.
  it('the held codes screen still offers "I have saved these"', async () => {
    await reachHeldCodes({})
    expect(screen.getByRole('button', { name: /i have saved these/i })).toBeTruthy()
  })
})
