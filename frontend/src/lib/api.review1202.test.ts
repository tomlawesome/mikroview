// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U3 (note_34034): `confirmTOTP` and `finishPasskeyRegistration`
// treat a 401 `step-expired` as "start again", not as a dead session.
// Gauntlet's errors.md: step-expired is a multi-step process whose step ran
// out (the scanned secret older than ten minutes; the passkey ceremony cookie
// gone) and "a frontend restarts the flow from its first step". A 401
// `sign-in-required` is the session being gone, and still throws. Written
// from the fix list alone, before the fix.
//
// Name the fix must provide: `STEP_EXPIRED`, exported from ./api -- the
// string these calls (and confirmRecoveryCodes) answer for a step-expired
// refusal, as PENDING_LOGIN_EXPIRED does for the login factor. Its text
// contains "start again".

import { afterEach, describe, expect, it, vi } from 'vitest'
import { confirmRecoveryCodes, confirmTOTP, finishPasskeyRegistration, STEP_EXPIRED } from './api'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'

function problem(status: number, cls: string, detail: string): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

const CREDENTIAL = { id: 'c1', response: {} }

describe('a step-expired refusal is "start again", not a dead session (#1202 review U3)', () => {
  // #1202 review U3
  it('STEP_EXPIRED is exported and tells the person to start again', () => {
    expect(typeof STEP_EXPIRED).toBe('string')
    expect(STEP_EXPIRED).toMatch(/start again/i)
  })

  // #1202 review U3
  it('confirmTOTP: a 401 #step-expired answers STEP_EXPIRED and does not throw, whatever the detail says', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'step-expired', 'the secret was set more than ten minutes ago')))
    expect(await confirmTOTP('123456')).toBe(STEP_EXPIRED)
  })

  // #1202 review U3: pair -- a 401 #sign-in-required IS the session being gone and still throws.
  it('confirmTOTP: a 401 #sign-in-required still throws as the session being gone', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'sign-in-required', 'sign in first')))
    await expect(confirmTOTP('123456')).rejects.toMatchObject({ status: 401 })
  })

  // #1202 review U3: pair -- a wrong code (400 invalid-request) is shown as its detail, not "start again".
  it('confirmTOTP: a wrong code is shown as the server detail and is not read as an expired step', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(400, 'invalid-request', 'invalid code')))
    const result = await confirmTOTP('000000')
    expect(result).toBe('invalid code')
    expect(result).not.toBe(STEP_EXPIRED)
  })

  // #1202 review U3
  it('finishPasskeyRegistration: a 401 #step-expired answers STEP_EXPIRED and does not throw', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'step-expired', 'the ceremony ran out')))
    expect(await finishPasskeyRegistration(CREDENTIAL, 'my key')).toBe(STEP_EXPIRED)
  })

  // #1202 review U3: pair.
  it('finishPasskeyRegistration: a 401 #sign-in-required still throws as the session being gone', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'sign-in-required', 'sign in first')))
    await expect(finishPasskeyRegistration(CREDENTIAL, 'my key')).rejects.toMatchObject({ status: 401 })
  })

  // #1202 review U1/U3: the recovery-codes confirm has the same step-expired
  // refusal (the hold ran out after ten minutes and the factor was deleted).
  it('confirmRecoveryCodes: a 401 #step-expired answers STEP_EXPIRED', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'step-expired', 'the hold ran out')))
    expect(await confirmRecoveryCodes()).toBe(STEP_EXPIRED)
  })

  // #1202 review U1: pair -- 409 (nothing on hold) is a different refusal; the
  // detail comes back as text and is not "start again" by this sentinel.
  it('confirmRecoveryCodes: a 409 answers its detail as text, not STEP_EXPIRED', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(409, 'conflict', 'no second factor is waiting to be confirmed')))
    const result = await confirmRecoveryCodes()
    expect(result).toBe('no second factor is waiting to be confirmed')
    expect(result).not.toBe(STEP_EXPIRED)
  })
})
