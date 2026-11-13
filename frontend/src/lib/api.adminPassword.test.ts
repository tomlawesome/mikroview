// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U8: gauntlet (docs/api/auth.yaml, v0.4.1) takes the calling
// admin's own password, as a PasswordRequest body ({"password": ...}), on
// DELETE /api/auth/users/{id}, POST .../reset-password, DELETE .../totp and
// DELETE .../passkeys. Written before the fix. The api.ts half: each call
// takes the password as its second argument and sends it in the body.
// Responses are real Response objects so the tests do not care how api.ts
// reads a body.

import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearUserPasskeys, clearUserTOTP, deleteUser, INCORRECT_PASSWORD, resetUserPassword } from './api'

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'

function problem(status: number, cls: string, detail: string): Response {
  return new Response(JSON.stringify({ type: `${ERRORS_MD}#${cls}`, title: cls, status, detail }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

const issued = { username: 'kai', code: 'ABCD-EFGH-JKLM-NPQR', expiresAt: '2026-10-11T00:00:00Z' }

type Row = {
  name: string
  call: (password: string) => Promise<unknown>
  method: string
  url: string
  ok: Response
  okResult: unknown
}

const rows: Row[] = [
  {
    name: 'deleteUser',
    call: (p) => deleteUser('u2', p),
    method: 'DELETE',
    url: '/api/auth/users/u2',
    ok: json({ username: 'kai', tokensRevoked: 2 }),
    okResult: null,
  },
  {
    name: 'resetUserPassword',
    call: (p) => resetUserPassword('u2', p),
    method: 'POST',
    url: '/api/auth/users/u2/reset-password',
    ok: json(issued),
    okResult: issued,
  },
  {
    name: 'clearUserTOTP',
    call: (p) => clearUserTOTP('u2', p),
    method: 'DELETE',
    url: '/api/auth/users/u2/totp',
    ok: json({ username: 'kai', cleared: true }),
    okResult: null,
  },
  {
    name: 'clearUserPasskeys',
    call: (p) => clearUserPasskeys('u2', p),
    method: 'DELETE',
    url: '/api/auth/users/u2/passkeys',
    ok: json({ username: 'kai', cleared: true }),
    okResult: null,
  },
]

function stub(res: () => Response) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => res())
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe.each(rows)('$name sends the admin password (#1202 review U8)', (row) => {
  it('sends {"password": ...} as the JSON body, to the right route and method', async () => {
    const fetchMock = stub(() => row.ok.clone())
    await row.call('hunter2')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe(row.url)
    expect(init?.method).toBe(row.method)
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
    expect(new Headers(init?.headers).get('content-type')).toMatch(/application\/json/)
  })

  // Pair: the password rides in the body only, never in the URL, and is
  // carried byte for byte (no trimming, no URL-style encoding).
  it('never puts the password in the URL, and sends an awkward one verbatim', async () => {
    const fetchMock = stub(() => row.ok.clone())
    const awkward = ' p&ss/w?rd #1 %20 "q" '
    await row.call(awkward)

    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).not.toContain('w?rd')
    expect(String(url)).not.toContain(encodeURIComponent(awkward.trim()))
    expect(String(url)).not.toContain('?')
    expect(JSON.parse(init?.body as string)).toEqual({ password: awkward })
  })

  it('answers the success value of a 200', async () => {
    stub(() => row.ok.clone())
    expect(await row.call('hunter2')).toEqual(row.okResult)
  })

  it('a 401 #invalid-credentials answers INCORRECT_PASSWORD, whatever the detail says', async () => {
    stub(() => problem(401, 'invalid-credentials', 'those words are not the old ones'))
    expect(await row.call('wrong')).toBe(INCORRECT_PASSWORD)
  })

  // Pair: a 401 of another class is the session being gone, not a wrong
  // password, even when its detail reads like one.
  it('a 401 #sign-in-required is not read as INCORRECT_PASSWORD', async () => {
    stub(() => problem(401, 'sign-in-required', 'incorrect password'))
    const outcome = await row.call('hunter2').then(
      (v) => v,
      (e: unknown) => e,
    )
    expect(outcome).not.toBe(INCORRECT_PASSWORD)
  })

  it('a 429 #rate-limited answers its detail (too many attempts)', async () => {
    stub(() => problem(429, 'rate-limited', 'too many attempts, try again later'))
    expect(await row.call('hunter2')).toBe('too many attempts, try again later')
  })

  it('a 409 answers the server reason as text (own account, last admin, SSO-only caller)', async () => {
    stub(() => problem(409, 'conflict', 'you cannot do this to the account you are signed in with'))
    expect(await row.call('hunter2')).toBe('you cannot do this to the account you are signed in with')
  })

  it('a 409 #last-admin answers its detail as text', async () => {
    stub(() => problem(409, 'last-admin', 'the last admin cannot be changed'))
    expect(await row.call('hunter2')).toBe('the last admin cannot be changed')
  })
})

// Pair for the two clear calls: "nothing there to remove" is a 200 with
// cleared false, and is not an error.
describe.each([
  ['clearUserTOTP', (p: string) => clearUserTOTP('u2', p)],
  ['clearUserPasskeys', (p: string) => clearUserPasskeys('u2', p)],
])('%s with nothing to clear (#1202 review U8)', (_name, call) => {
  it('a 200 with cleared false is success, not a refusal', async () => {
    stub(() => json({ username: 'kai', cleared: false }))
    expect(await call('hunter2')).toBeNull()
  })
})
