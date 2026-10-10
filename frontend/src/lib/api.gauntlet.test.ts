// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202: authentication moves onto gauntlet, and the wire contract the
// frontend speaks changes with it (docs/decisions/gauntlet-adoption.md,
// decision 4). These pin the api.ts half, written from that record and
// the build notes' test specification (B7, items 31-34) before the code
// exists. Responses are real Response objects so the tests do not care
// whether api.ts reads a body with .text() or .json().

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  beginPasskeyRegistration,
  confirmRecoveryCodes,
  confirmTOTP,
  createToken,
  enrolTOTP,
  fetchAuditLog,
  fetchDevices,
  finishPasskeyRegistration,
  INCORRECT_PASSWORD,
  login,
  onForcedAuthGate,
  PENDING_LOGIN_EXPIRED,
  regenerateRecoveryCodes,
  submitLoginFactor,
} from './api'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'

// A gauntlet error: RFC 9457 Problem Details, type anchored into errors.md.
function problem(status: number, cls: string, detail: string, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json', ...headers },
  })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function plain(status: number, body: string): Response {
  return new Response(body, { status, headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

// B7 item 31.
describe('reading a gauntlet error (Problem Details, #1202 spec 31)', () => {
  // 31: type ending #invalid-credentials is the incorrect-password refusal,
  // whatever words detail happens to use (errors.md: key on type, never detail).
  it.each([
    ['enrolTOTP', (p: string) => enrolTOTP(p)],
    ['beginPasskeyRegistration', (p: string) => beginPasskeyRegistration(p)],
    ['regenerateRecoveryCodes', (p: string) => regenerateRecoveryCodes(p)],
  ])('%s: a 401 #invalid-credentials answers INCORRECT_PASSWORD, whatever the detail says', async (_name, call) => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'invalid-credentials', 'those words are not the old ones')))
    expect(await call('wrong')).toBe(INCORRECT_PASSWORD)
  })

  // Pair for the above: the same detail text under a different type is not
  // the incorrect-password refusal. On these routes a 401 that is not
  // invalid-credentials means the session is gone, which still throws.
  it('enrolTOTP: a 401 #sign-in-required is thrown as the session being gone, even when its detail reads "incorrect password"', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'sign-in-required', 'incorrect password')))
    await expect(enrolTOTP('hunter2')).rejects.toMatchObject({ status: 401 })
  })

  // 31: #step-expired is the pending-login-expired refusal.
  it('submitLoginFactor: a 401 #step-expired answers PENDING_LOGIN_EXPIRED, whatever the detail says', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'step-expired', 'this step has run out')))
    expect(await submitLoginFactor('123456')).toBe(PENDING_LOGIN_EXPIRED)
  })

  // Pair: a wrong code is #invalid-credentials, not an expired step; the
  // person is shown the server's detail and stays on the code box.
  it('submitLoginFactor: a 401 #invalid-credentials shows the detail and is not read as an expired step', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'invalid-credentials', 'invalid code')))
    const result = await submitLoginFactor('000000')
    expect(result).toBe('invalid code')
    expect(result).not.toBe(PENDING_LOGIN_EXPIRED)
  })

  // 31: where the text body was shown, detail is shown -- not the raw JSON.
  it('login: shows the detail of a Problem Details body, not the JSON text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(401, 'invalid-credentials', 'invalid username or password')))
    expect(await login('alex', 'wrong')).toBe('invalid username or password')
  })

  it('a thrown request failure carries the detail of a Problem Details body as its message', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(403, 'forbidden', 'insufficient role')))
    const err = await fetchAuditLog().then(
      () => null,
      (e: unknown) => e as { message: string; status: number },
    )
    expect(err?.message).toBe('insufficient role')
    expect(err?.status).toBe(403)
  })

  it('a 429 #rate-limited is shown as its detail', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(429, 'rate-limited', 'too many attempts, try again later')))
    expect(await enrolTOTP('hunter2')).toBe('too many attempts, try again later')
  })

  // 31: a plain-text error body is shown as is (mikroview's own routes keep
  // their plain-text errors, decision 4.1).
  it('login: a plain-text error body is shown as is', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => plain(401, 'invalid username or password')))
    expect(await login('alex', 'wrong')).toBe('invalid username or password')
  })

  it('a thrown request failure with a plain-text body carries that text as its message', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => plain(403, 'user role required')))
    await expect(fetchAuditLog()).rejects.toThrow('user role required')
  })

  // Pair for the plain-text rule: a plain-text body is never mistaken for a
  // typed class, even when it happens to contain the anchor words.
  it('a plain-text body that mentions #step-expired is not treated as an expired step', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => plain(401, 'see errors.md#step-expired')))
    const result = await submitLoginFactor('123456')
    expect(result).toBe('see errors.md#step-expired')
    expect(result).not.toBe(PENDING_LOGIN_EXPIRED)
  })
})

// B7 item 32, the api.ts half: the door header is X-Auth-Gate and gains
// must-enrol-passkey.
describe('the forced-auth-gate 403 under gauntlet (#1202 spec 32)', () => {
  function stubWithGateHeader(gate: string | null) {
    return vi.fn(async (url: string) => {
      if (url === '/api/auth/session') {
        return json({
          authenticated: true,
          username: 'alex',
          role: 'admin',
          ssoAvailable: false,
          mustEnrolPasskey: gate === 'must-enrol-passkey',
          mustEnrolSecondFactor: gate === 'must-enrol-factor',
          mustChangePassword: gate === 'must-change-password',
        })
      }
      const headers: Record<string, string> = gate ? { 'X-Auth-Gate': gate } : {}
      return problem(403, gate ?? 'forbidden', 'held at a door', headers)
    })
  }

  async function run(gate: string | null) {
    vi.stubGlobal('fetch', stubWithGateHeader(gate))
    const handler = vi.fn()
    onForcedAuthGate(handler)
    try {
      await expect(fetchDevices()).rejects.toMatchObject({ status: 403 })
      // The recheck is fired from inside the failed call, not awaited by it.
      await new Promise((r) => setTimeout(r, 0))
    } finally {
      onForcedAuthGate(() => {})
    }
    return handler
  }

  it('calls the handler with a fresh session when X-Auth-Gate says must-enrol-passkey', async () => {
    const handler = await run('must-enrol-passkey')
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith('must-enrol-passkey', expect.objectContaining({ mustEnrolPasskey: true }))
  })

  it('calls the handler for must-enrol-factor read from X-Auth-Gate', async () => {
    const handler = await run('must-enrol-factor')
    expect(handler).toHaveBeenCalledWith('must-enrol-factor', expect.objectContaining({ mustEnrolSecondFactor: true }))
  })

  it('calls the handler for must-change-password read from X-Auth-Gate', async () => {
    const handler = await run('must-change-password')
    expect(handler).toHaveBeenCalledWith('must-change-password', expect.objectContaining({ mustChangePassword: true }))
  })

  it('does not call the handler, or re-read the session, for a 403 with no X-Auth-Gate', async () => {
    const fetchMock = stubWithGateHeader(null)
    vi.stubGlobal('fetch', fetchMock)
    const handler = vi.fn()
    onForcedAuthGate(handler)
    try {
      await expect(fetchDevices()).rejects.toMatchObject({ status: 403 })
      await new Promise((r) => setTimeout(r, 0))
    } finally {
      onForcedAuthGate(() => {})
    }
    expect(handler).not.toHaveBeenCalled()
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/auth/session')).toHaveLength(0)
  })
})

// B7 item 33, the api.ts half: POST /api/tokens carries the admin's password;
// ingest tokens ask never to expire; API tokens leave expiry to the server's
// one-year default (ruling 4). Gauntlet refuses unknown fields, so only the
// documented ones are sent.
describe('createToken under gauntlet (#1202 spec 33)', () => {
  const tokenBack = { id: 't1', name: 'x', kind: 'api', createdAt: '2026-10-10T00:00:00Z', value: 'gnt_0123456789abcdef' }

  function captured() {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => json(tokenBack, 201))
    vi.stubGlobal('fetch', fetchMock)
    return () => {
      const [url, init] = fetchMock.mock.calls[0]
      return { url, init, body: JSON.parse(init?.body as string) as Record<string, unknown> }
    }
  }

  // createToken(name, kind, device, password): the password joins the call
  // after the device (named in the commit message for the build).
  it('an api key sends the password and no expiresAt and no device', async () => {
    const sent = captured()
    const result = await createToken('nas-read', 'api', undefined, 'hunter2')
    const { url, init, body } = sent()
    expect(url).toBe('/api/tokens')
    expect(init?.method).toBe('POST')
    expect(body).toEqual({ name: 'nas-read', kind: 'api', password: 'hunter2' })
    expect(body).not.toHaveProperty('expiresAt')
    expect(result).toMatchObject({ value: 'gnt_0123456789abcdef' })
  })

  it('an ingest key sends the password, the device and expiresAt "never"', async () => {
    const sent = captured()
    await createToken('rb5009-ingest', 'ingest', 'rb5009', 'hunter2')
    expect(sent().body).toEqual({
      name: 'rb5009-ingest',
      kind: 'ingest',
      device: 'rb5009',
      password: 'hunter2',
      expiresAt: 'never',
    })
  })
})

// B7 item 34, the api.ts half.
describe('the recovery-codes confirm call (#1202 spec 34)', () => {
  const codes = Array.from({ length: 10 }, (_, i) => `code-${i}`)

  it('confirmRecoveryCodes posts an empty JSON body to /api/auth/recovery-codes/confirm and answers null on success', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => json({ confirmed: true, factor: 'totp' }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await confirmRecoveryCodes()
    expect(result).toBeNull()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/recovery-codes/confirm')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual({})
  })

  it('confirmRecoveryCodes answers the detail as text when nothing is on hold (409)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => problem(409, 'conflict', 'no second factor is waiting to be confirmed')))
    expect(await confirmRecoveryCodes()).toBe('no second factor is waiting to be confirmed')
  })

  it('confirmTOTP carries pendingConfirmation through when the server says the app is on hold', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ active: false, recoveryCodes: codes, pendingConfirmation: true })))
    const result = await confirmTOTP('123456')
    expect(result).toMatchObject({ recoveryCodes: codes, pendingConfirmation: true })
  })

  it('confirmTOTP reports no pending confirmation when the server does not send the field (gauntlet omits it unless true)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ recoveryCodes: codes })))
    const result = await confirmTOTP('123456')
    expect(result).toMatchObject({ recoveryCodes: codes })
    expect((result as { pendingConfirmation?: boolean }).pendingConfirmation).toBeFalsy()
  })

  it('finishPasskeyRegistration carries pendingConfirmation through when the passkey is on hold', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({ passkey: { id: 'c1' }, recoveryCodes: codes, pendingConfirmation: true })),
    )
    const result = await finishPasskeyRegistration({ id: 'c1', response: {} }, 'my key')
    expect(result).toMatchObject({ recoveryCodes: codes, pendingConfirmation: true })
  })

  it('finishPasskeyRegistration reports no pending confirmation when the server does not send the field', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ passkey: { id: 'c1' }, recoveryCodes: null })))
    const result = await finishPasskeyRegistration({ id: 'c1', response: {} }, 'my key')
    expect((result as { pendingConfirmation?: boolean }).pendingConfirmation).toBeFalsy()
  })
})
