// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U13 (note_34148), the forced-enrolment door's
// recovery-codes screen: the "start again", 409 and network-error messages are
// announced (a live region exists before the message arrives, and the message
// goes into it), and focus lands on a control on the screen shown, never the
// page body. Written before the fix.
//
// confirmRecoveryCodes and confirmTOTP are the REAL api.ts functions here,
// answering from a stubbed fetch, as in AuthEnrolFactor.review1202.svelte.test.ts.

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
}))

vi.mock('../lib/qrcode', () => ({
  qrCode: () => {},
}))

import { enrolTOTP, fetchAuthSession, fetchMyPreferences, logout, saveMyPreferences } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
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

const CONFIRMED = { confirmed: true, factor: 'totp' }

function reach(routes: Record<string, () => Response | Promise<Response>>) {
  return reachHeldCodesViaApp(routes)
}

// The whole door is the screen; focus must be a real control within it.
function inScope(el: Element): boolean {
  return el !== document.body && document.body.contains(el)
}

// Text of the live regions (role="status", role="alert" or aria-live) now.
function liveRegions(): Element[] {
  return Array.from(document.querySelectorAll('[role="status"], [role="alert"], [aria-live]'))
}

const START_AGAIN = /start again/i
const FAILED = /network|failed|couldn|could not|error|try again|unreachable|offline|not reach/i

type Refusal = { name: string; routes: () => Record<string, () => Response | Promise<Response>>; message: RegExp }

const refusals: Refusal[] = [
  {
    name: '401 step-expired ("start again")',
    routes: () => ({ [CONFIRM_URL]: () => problem(401, 'step-expired', 'the hold ran out') }),
    message: START_AGAIN,
  },
  {
    name: '409 ("start again")',
    routes: () => ({ [CONFIRM_URL]: () => problem(409, 'conflict', 'no second factor is waiting to be confirmed') }),
    message: START_AGAIN,
  },
  {
    name: 'a network error',
    routes: () => ({
      [CONFIRM_URL]: () => {
        throw new TypeError('Failed to fetch')
      },
    }),
    message: FAILED,
  },
]

describe.each(refusals)('the recovery-codes screen announces $name (#1202 review U13)', (refusal) => {
  // #1202 review U13: the live region is in the page before the message is.
  it('a live region exists before the refusal, and the message is put into that same region', async () => {
    await reach(refusal.routes())

    const before = liveRegions()
    expect(before.length).toBeGreaterThan(0)
    expect(before.map((el) => el.textContent ?? '').join(' ')).not.toMatch(refusal.message)

    await saveTheCodes()

    await vi.waitFor(() => expect(liveRegions().map((el) => el.textContent ?? '').join(' ')).toMatch(refusal.message))
    // The node that stood there before the refusal, not one created along with the message.
    const receivers = before.filter((el) => el.isConnected && refusal.message.test(el.textContent ?? ''))
    expect(receivers.length).toBeGreaterThan(0)
  })

  // #1202 review U13: focus is never dropped on the page body.
  it('focus is on a control on the screen shown, never the page body', async () => {
    await reach(refusal.routes())
    const save = screen.getByRole('button', { name: /i have saved these/i })
    save.focus()
    expect(document.activeElement).toBe(save)

    await fireEvent.click(save)
    await flush()
    await vi.waitFor(() => expect(document.body.textContent ?? '').toMatch(refusal.message))

    const active = document.activeElement
    expect(active).not.toBeNull()
    expect(active).not.toBe(document.body)
    expect(active?.isConnected).toBe(true)
    expect(inScope(active as Element)).toBe(true)
  })
})

// #1202 review U13: pair -- an accepted confirm announces nothing of the kind.
describe('an accepted confirm announces no refusal (#1202 review U13)', () => {
  it('no live region carries a "start again" or failure message', async () => {
    await reach({ [CONFIRM_URL]: () => ok(CONFIRMED) })
    await saveTheCodes()
    await flush()

    expect(liveRegions().map((el) => el.textContent ?? '').join(' ')).not.toMatch(START_AGAIN)
  })
})
