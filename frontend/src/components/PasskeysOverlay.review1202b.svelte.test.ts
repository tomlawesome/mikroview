// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U13 (note_34148), the account menu's passkeys
// overlay, recovery-codes screen: the "start again", 409 and network-error
// messages are announced (a live region exists before the message arrives, and
// the message goes into it), and focus lands inside the dialog, never on the
// page body. Written before the fix.
//
// confirmTOTP and confirmRecoveryCodes are the real api.ts functions answering
// from a stubbed fetch, as in PasskeysOverlay.review1202.svelte.test.ts.

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

const CONFIRMED = { confirmed: true, factor: 'passkey' }

function reach(routes: Record<string, () => Response | Promise<Response>>) {
  return reachHeldCodes(routes)
}

function inScope(el: Element): boolean {
  const dialog = screen.queryByRole('dialog', { name: /passkeys/i })
  return dialog !== null && dialog.contains(el)
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
