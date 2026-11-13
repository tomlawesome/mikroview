// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review (note_34147), the api.ts half of U9, U10, U11 and U12,
// checked against gauntlet v0.4.1 docs/api/auth.yaml. Written before the fix.
//
//  - U9:  POST /api/auth/logout-all takes the caller's own `password` in the
//         body when the account has one (the body is optional for an SSO-only
//         account).
//  - U10: POST /api/auth/oidc/link requires `password` (PasswordRequest).
//  - U11: on login/factor, a 401 step-expired answering a PASSKEY assertion
//         means only the passkey ceremony lapsed (call login/factor/begin
//         again); on login/factor/begin the same refusal means the pending
//         login itself is gone (back to the password form).
//  - U12: DELETE /api/auth/users/{id} answering 500 #partially-completed means
//         the account is gone but its tokens were not revoked.
//
// Names the fix must provide, exported from ./api:
//   PASSKEY_STEP_EXPIRED       string answered by submitPasskeyLoginAssertion
//                              for a step-expired refusal (not
//                              PENDING_LOGIN_EXPIRED).
//   USER_DELETED_TOKENS_REMAIN string answered by deleteUser for a 500
//                              #partially-completed; it is itself the message
//                              for the admin, so it tells them to revoke the
//                              account's tokens.
// signOutEverywhere(password?) and startSSOLink(password) take the password as
// an argument.
//
// Responses are real Response objects so the tests do not care how api.ts
// reads a body.

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  beginPasskeyLogin,
  deleteUser,
  INCORRECT_PASSWORD,
  PASSKEY_STEP_EXPIRED,
  PENDING_LOGIN_EXPIRED,
  signOutEverywhere,
  startSSOLink,
  submitLoginFactor,
  submitPasskeyLoginAssertion,
  USER_DELETED_TOKENS_REMAIN,
} from './api'

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

function stub(res: () => Response) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => res())
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('signOutEverywhere sends the caller\'s own password (#1202 review U9)', () => {
  const ok = () => json({ signedOutEverywhere: true })

  // #1202 review U9
  it('sends {"password": ...} as the JSON body to POST /api/auth/logout-all', async () => {
    const fetchMock = stub(ok)
    expect(await signOutEverywhere('hunter2')).toBeNull()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/logout-all')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
    expect(new Headers(init?.headers).get('content-type')).toMatch(/application\/json/)
  })

  // #1202 review U9: pair -- an awkward password rides in the body only, verbatim.
  it('carries an awkward password byte for byte and never in the URL', async () => {
    const fetchMock = stub(ok)
    const awkward = ' p&ss/w?rd #1 %20 "q" '
    await signOutEverywhere(awkward)

    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe('/api/auth/logout-all')
    expect(JSON.parse(init?.body as string)).toEqual({ password: awkward })
  })

  // #1202 review U9: pair -- an SSO-only account has no password to send.
  it('with no password (an SSO-only account) sends no password at all', async () => {
    const fetchMock = stub(ok)
    expect(await signOutEverywhere()).toBeNull()

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/logout-all')
    const body = init?.body
    if (body !== undefined && body !== null && body !== '') {
      expect(JSON.parse(body as string)).toEqual({})
    }
  })

  // #1202 review U9
  it('a 401 #invalid-credentials answers INCORRECT_PASSWORD, whatever the detail says', async () => {
    stub(() => problem(401, 'invalid-credentials', 'those words are not the old ones'))
    expect(await signOutEverywhere('wrong')).toBe(INCORRECT_PASSWORD)
  })

  // #1202 review U9: pair -- a 401 of another class is the session being gone.
  it('a 401 #sign-in-required is not read as INCORRECT_PASSWORD', async () => {
    stub(() => problem(401, 'sign-in-required', 'incorrect password'))
    const outcome = await signOutEverywhere('hunter2').then(
      (v) => v,
      (e: unknown) => e,
    )
    expect(outcome).not.toBe(INCORRECT_PASSWORD)
  })

  // #1202 review U9
  it('a 429 #rate-limited answers its detail', async () => {
    stub(() => problem(429, 'rate-limited', 'too many attempts, try again later'))
    expect(await signOutEverywhere('hunter2')).toBe('too many attempts, try again later')
  })
})

describe('startSSOLink sends the caller\'s own password (#1202 review U10)', () => {
  const ok = () => json({ url: 'https://idp.example/authorize' })

  // #1202 review U10
  it('sends {"password": ...} as the JSON body to POST /api/auth/oidc/link and answers the url', async () => {
    const fetchMock = stub(ok)
    expect(await startSSOLink('hunter2')).toEqual({ url: 'https://idp.example/authorize' })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/oidc/link')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
    expect(new Headers(init?.headers).get('content-type')).toMatch(/application\/json/)
  })

  // #1202 review U10: pair.
  it('never puts the password in the URL, and sends an awkward one verbatim', async () => {
    const fetchMock = stub(ok)
    const awkward = ' p&ss/w?rd #1 %20 "q" '
    await startSSOLink(awkward)

    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe('/api/auth/oidc/link')
    expect(JSON.parse(init?.body as string)).toEqual({ password: awkward })
  })

  // #1202 review U10
  it('a 401 #invalid-credentials answers INCORRECT_PASSWORD, whatever the detail says', async () => {
    stub(() => problem(401, 'invalid-credentials', 'those words are not the old ones'))
    expect(await startSSOLink('wrong')).toBe(INCORRECT_PASSWORD)
  })

  // #1202 review U10: pair.
  it('a 401 #sign-in-required is not read as INCORRECT_PASSWORD', async () => {
    stub(() => problem(401, 'sign-in-required', 'incorrect password'))
    const outcome = await startSSOLink('hunter2').then(
      (v) => v,
      (e: unknown) => e,
    )
    expect(outcome).not.toBe(INCORRECT_PASSWORD)
  })

  // #1202 review U10
  it('a 429 #rate-limited answers its detail', async () => {
    stub(() => problem(429, 'rate-limited', 'too many attempts, try again later'))
    expect(await startSSOLink('hunter2')).toBe('too many attempts, try again later')
  })

  // #1202 review U10: the two 409s keep their handling -- a refusal as text,
  // never read as a wrong password and never as a url.
  it.each([
    ['already SSO-only', 'this account already signs in through your identity provider'],
    ['already linked', 'this account is already linked'],
  ])('a 409 (%s) is a text refusal, not a wrong password and not a url', async (_name, detail) => {
    stub(() => problem(409, 'conflict', detail))
    const result = await startSSOLink('hunter2')
    expect(typeof result).toBe('string')
    expect(result).not.toBe(INCORRECT_PASSWORD)
  })
})

describe('a passkey assertion\'s step-expired restarts the passkey step (#1202 review U11)', () => {
  // #1202 review U11
  it('PASSKEY_STEP_EXPIRED is exported, is not the pending-login sentinel, and says to try the passkey again', () => {
    expect(typeof PASSKEY_STEP_EXPIRED).toBe('string')
    expect(PASSKEY_STEP_EXPIRED).not.toBe('')
    expect(PASSKEY_STEP_EXPIRED).not.toBe(PENDING_LOGIN_EXPIRED)
    expect(PASSKEY_STEP_EXPIRED).toMatch(/passkey/i)
  })

  // #1202 review U11
  it('submitPasskeyLoginAssertion: a 401 #step-expired answers PASSKEY_STEP_EXPIRED, not PENDING_LOGIN_EXPIRED, whatever the detail says', async () => {
    const fetchMock = stub(() => problem(401, 'step-expired', 'the passkey ceremony ran out'))
    const result = await submitPasskeyLoginAssertion({ id: 'c1' })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/auth/login/factor')
    expect(result).toBe(PASSKEY_STEP_EXPIRED)
    expect(result).not.toBe(PENDING_LOGIN_EXPIRED)
  })

  // #1202 review U11: pair -- a wrong assertion is a plain refusal, shown as its detail.
  it('submitPasskeyLoginAssertion: a 401 #invalid-credentials is shown as its detail and does not restart', async () => {
    stub(() => problem(401, 'invalid-credentials', 'that passkey could not be verified'))
    const result = await submitPasskeyLoginAssertion({ id: 'c1' })
    expect(result).toBe('that passkey could not be verified')
    expect(result).not.toBe(PASSKEY_STEP_EXPIRED)
    expect(result).not.toBe(PENDING_LOGIN_EXPIRED)
  })

  // #1202 review U11: pair -- a code at the same route is unchanged: step-expired there is the pending login.
  it('submitLoginFactor (a code): a 401 #step-expired still answers PENDING_LOGIN_EXPIRED', async () => {
    stub(() => problem(401, 'step-expired', 'this step has run out'))
    const result = await submitLoginFactor('123456')
    expect(result).toBe(PENDING_LOGIN_EXPIRED)
    expect(result).not.toBe(PASSKEY_STEP_EXPIRED)
  })

  // #1202 review U11: the restart's own begin is where a lapsed pending login shows.
  it('beginPasskeyLogin: a 401 #step-expired answers PENDING_LOGIN_EXPIRED (the pending login is gone)', async () => {
    const fetchMock = stub(() => problem(401, 'step-expired', 'sign in again from the start'))
    const result = await beginPasskeyLogin()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/auth/login/factor/begin')
    expect(result).toBe(PENDING_LOGIN_EXPIRED)
  })

  // #1202 review U11: pair.
  it('beginPasskeyLogin: a 200 answers the request options', async () => {
    stub(() => json({ publicKey: { challenge: 'c' } }))
    expect(await beginPasskeyLogin()).toEqual({ publicKey: { challenge: 'c' } })
  })
})

describe('deleteUser and a 500 #partially-completed (#1202 review U12)', () => {
  // #1202 review U12
  it('USER_DELETED_TOKENS_REMAIN is exported and tells the admin to revoke the account\'s tokens', () => {
    expect(typeof USER_DELETED_TOKENS_REMAIN).toBe('string')
    expect(USER_DELETED_TOKENS_REMAIN).toMatch(/revoke/i)
    expect(USER_DELETED_TOKENS_REMAIN).toMatch(/tokens/i)
  })

  // #1202 review U12
  it('answers USER_DELETED_TOKENS_REMAIN for a 500 #partially-completed, whatever the detail says', async () => {
    const fetchMock = stub(() =>
      new Response(
        JSON.stringify({
          type: `${ERRORS_MD}#partially-completed`,
          title: 'partially-completed',
          status: 500,
          detail: 'something unrelated',
          username: 'kai',
        }),
        { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
      ),
    )
    const result = await deleteUser('u2', 'hunter2')

    expect(fetchMock.mock.calls[0][0]).toBe('/api/auth/users/u2')
    expect(result).toBe(USER_DELETED_TOKENS_REMAIN)
  })

  // #1202 review U12: pair -- a 500 #server-error means nothing was deleted.
  it('a 500 #server-error is a text refusal, not USER_DELETED_TOKENS_REMAIN', async () => {
    stub(() => problem(500, 'server-error', 'the account could not be deleted'))
    const result = await deleteUser('u2', 'hunter2')
    expect(typeof result).toBe('string')
    expect(result).not.toBe(USER_DELETED_TOKENS_REMAIN)
  })

  // #1202 review U12: pair -- a plain delete is still success.
  it('a 200 is still success (null)', async () => {
    stub(() => json({ username: 'kai', tokensRevoked: 2 }))
    expect(await deleteUser('u2', 'hunter2')).toBeNull()
  })
})
