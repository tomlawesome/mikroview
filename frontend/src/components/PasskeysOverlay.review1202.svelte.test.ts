// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U1, U6 (note_34034), the account menu's passkeys overlay.
// Written from the fix list alone, before the fix.
//
//  - U1: a refused recovery-codes confirm must not strand the person: 401
//    step-expired and 409 go back to the start of the overlay (or close it)
//    with a "start again" message; 401 sign-in-required signs out; a network
//    error shows an error and leaves "I have saved these" usable.
//  - U6: no "every other session ... ended" / "turns this on" wording before
//    the first passkey is confirmed.
//
// confirmRecoveryCodes is the real api.ts function answering Problem Details
// responses from a stubbed fetch. (finishPasskeyRegistration's own
// step-expired handling, U3, is pinned in lib/api.review1202.test.ts: the
// passkey ceremony is faked whole here.)
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
    fetchPasskeys: vi.fn(),
    renamePasskey: vi.fn(),
    disablePasskey: vi.fn(),
    fetchAuthSession: vi.fn(),
    logout: vi.fn(),
  }
})

vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
}))

vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { fetchAuthSession, fetchPasskeys, logout } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
import PasskeysOverlay from './PasskeysOverlay.svelte'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'
const CONFIRM_URL = '/api/auth/recovery-codes/confirm'
const CODES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']

const PASSKEY = {
  id: 'cred-1',
  name: 'this laptop',
  createdAt: '2026-10-10T00:00:00Z',
  transports: ['internal'],
  stale: false,
  rpId: 'mikroview.example.org',
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
  vi.mocked(fetchPasskeys).mockResolvedValue([])
  vi.mocked(fetchAuthSession).mockResolvedValue({
    setupRequired: false,
    authenticated: true,
    username: 'alex',
    role: 'admin',
    ssoAvailable: false,
  })
  vi.mocked(logout).mockResolvedValue(null)
  vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: CODES, pendingConfirmation: true })
  authState.state = 'authenticated'
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function reachForm() {
  render(PasskeysOverlay, { open: true })
  await screen.findByText(/add a passkey/i)
  await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
}

async function reachHeldCodes(routes: Record<string, () => Response | Promise<Response>>) {
  stubNetwork(routes)
  await reachForm()
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
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

async function expectSentBackToStart() {
  await vi.waitFor(() => expect(screen.queryByTestId('recovery-codes')).toBeNull())
  const dialog = screen.queryByRole('dialog', { name: /passkeys/i })
  if (dialog) {
    expect(within(dialog).getAllByText(/start again/i).length).toBeGreaterThan(0)
    expect(within(dialog).getAllByRole('button', { name: /add passkey|close/i }).length).toBeGreaterThan(0)
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
    await reachHeldCodes({ [CONFIRM_URL]: () => ok({ confirmed: true, factor: 'passkey' }) })
    await saveTheCodes()
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /passkeys/i })).toBeNull())
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
        return ok({ confirmed: true, factor: 'passkey' })
      },
    })

    // Nothing says it failed until it has.
    expect(somethingSaysItFailed()).toBe(false)

    await saveTheCodes()

    await vi.waitFor(() => expect(somethingSaysItFailed()).toBe(true))
    expect(screen.getByRole('dialog', { name: /passkeys/i })).toBeTruthy()
    expect(screen.getByTestId('recovery-codes')).toBeTruthy()
    const button = screen.getByRole('button', { name: /i have saved these/i }) as HTMLButtonElement
    expect(button.disabled).toBe(false)
    expect(button.getAttribute('aria-disabled')).not.toBe('true')

    // Usable means a second press goes through.
    await fireEvent.click(button)
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /passkeys/i })).toBeNull())
  })
})

describe('no "every other session ended" wording before the passkey is confirmed (#1202 review U6)', () => {
  const PREMATURE = /every other session|other sessions|turns this on|turns it on/i

  // #1202 review U6
  it('the name-and-password screen says nothing about ending other sessions or turning anything on', async () => {
    await reachForm()
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
