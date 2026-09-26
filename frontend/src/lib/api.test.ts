// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  beginPasskeyLogin,
  beginPasskeyRegistration,
  buildQuery,
  clearAllFlags,
  clearUserPasskeys,
  clearUserTOTP,
  confirmTOTP,
  deleteDevice,
  deleteDroplistEntry,
  disablePasskey,
  disableTOTP,
  enrolTOTP,
  fetchAuditLog,
  fetchEventsWindow,
  fetchPasskeys,
  fetchSetupCommands,
  finishPasskeyRegistration,
  login,
  mintDroplistKey,
  regenerateRecoveryCodes,
  renamePasskey,
  replayDefinition,
  revokeDroplistKey,
  saveSetupBackupTransport,
  setFlagVerdict,
  setRouterBackupComment,
  submitLoginFactor,
  submitPasskeyLoginAssertion,
} from './api'
import { emptyFilters } from './types'

// buildQuery's `ip` forwarding is refetchWithFilters()'s only path back to
// internal/store/query.go's server-side narrowing (see the function's own
// doc comment) -- a regression here silently degrades the "actually
// complete" layer state.svelte.ts describes into "the 500 most recent
// events, unfiltered by address", starving out a selective address that
// only appears further back in the retained buffer than that. Pinned here
// because it slipped through review once already (see #438's PR history)
// without a test that would have caught it immediately.
describe('buildQuery: ip forwarding for srcQuery/dstQuery (#438)', () => {
  it('forwards a bare source IP as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  it('forwards a bare destination IP as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '203.0.113.5' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  it('forwards a source CIDR as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.0/24' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.0/24')
  })

  it('forwards an IPv6 address as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '2001:db8::1' })
    expect(new URLSearchParams(qs).get('ip')).toBe('2001:db8::1')
  })

  it('when both boxes hold an address, forwards srcQuery (still a valid superset for either side)', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5', dstQuery: '198.51.100.9' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  // A pasted address routinely carries leading/trailing whitespace. The
  // forwarded value must be trimmed: internal/store/query.go's
  // net.ParseIP/net.ParseCIDR both fail on padding, which drops
  // matchesFilters to its exact-string-equal fallback -- matching no
  // event at all -- while the client-side matcher (which does trim)
  // leaves the already-buffered rows looking fine. Silently wrong, not
  // visibly broken, which is exactly why this is pinned rather than left
  // to be caught by eye.
  it('trims whitespace from the forwarded ip, not just from the srcQuery param', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '  203.0.113.5  ' })
    const params = new URLSearchParams(qs)
    expect(params.get('ip')).toBe('203.0.113.5')
    expect(params.get('ip')).not.toMatch(/\s/)
  })

  it('trims whitespace from a padded CIDR too', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '\t198.51.100.0/24\n' })
    expect(new URLSearchParams(qs).get('ip')).toBe('198.51.100.0/24')
  })

  it('does not forward a label/name fragment as ip -- no server-side equivalent exists', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: 'nas-basement' })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
    expect(new URLSearchParams(qs).get('srcQuery')).toBe('nas-basement')
  })

  it('does not forward a malformed CIDR as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5/99' })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
  })

  it('does not set ip when neither box holds an address', () => {
    const qs = buildQuery({ ...emptyFilters() })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
  })

  it('only forwards a numeric port, never a text service-name search', () => {
    const numeric = buildQuery({ ...emptyFilters(), port: '443' })
    expect(new URLSearchParams(numeric).get('port')).toBe('443')

    const text = buildQuery({ ...emptyFilters(), port: 'https' })
    expect(new URLSearchParams(text).get('port')).toBeNull()
  })
})

// The decline is the whole point of these (#786). POST
// /api/definitions/{id}/replay answers 200 for both a receipt and a
// decline, because "the corpus is shorter than this definition's window"
// is an honest answer about the traffic held, not a failed request --
// so a wrapper that threw on it, or that flattened it into a receipt of
// zero, would destroy the distinction engine.Result is shaped to keep
// (see internal/engine/replay.go's Decline doc comment). These pin that
// both shapes come back as values, and that a genuine refusal is still
// the error string every other definitions wrapper returns.
describe('replayDefinition (#786)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubFetch(status: number, body: unknown) {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => (typeof body === 'string' ? body : JSON.stringify(body)),
    }))
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  const RECEIPT = {
    receipt: {
      window: {
        start: '2026-09-02T08:00:00Z',
        end: '2026-09-02T12:12:00Z',
        duration: '4h12m0s',
        eventCount: 8421,
      },
      emissionCount: 3,
      sample: [
        {
          at: '2026-09-02T09:04:00Z',
          target: '203.0.113.9',
          detail: '18 ports in 1m0s',
          ports: [22, 23, 25],
          provisional: false,
        },
      ],
      sampleTruncated: false,
      corpusTruncated: false,
      anyProvisional: false,
    },
  }

  it('returns a receipt as a value, with its window and sample intact', async () => {
    const fetchMock = stubFetch(200, RECEIPT)
    const result = await replayDefinition('port_scan', { threshold: 9 })

    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    expect(result.decline).toBeUndefined()
    expect(result.receipt?.emissionCount).toBe(3)
    expect(result.receipt?.window.duration).toBe('4h12m0s')
    expect(result.receipt?.sample[0].target).toBe('203.0.113.9')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/definitions/port_scan/replay')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ params: { threshold: 9 } })
  })

  // The candidate's count is only worth reading against the count the
  // definition makes as it stands, so the server answers a candidate with
  // both (#786). This wrapper must carry the second one through intact:
  // it is receipt-or-decline like the answer around it.
  it('carries the live-params count back beside the candidate’s', async () => {
    stubFetch(200, {
      ...RECEIPT,
      current: {
        receipt: { ...RECEIPT.receipt, emissionCount: 41, sample: [] },
      },
    })
    const result = await replayDefinition('port_scan', { threshold: 9 })
    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    expect(result.receipt?.emissionCount).toBe(3)
    expect(result.current?.receipt?.emissionCount).toBe(41)
  })

  it('carries a declining live-params answer back as a value too', async () => {
    stubFetch(200, {
      ...RECEIPT,
      current: {
        decline: {
          reason: 'corpus covers 4h12m0s (8421 event(s)), shorter than this definition’s window',
          corpusSpan: '4h12m0s',
          definitionWindow: '24h0m0s',
        },
      },
    })
    const result = await replayDefinition('port_scan', { window: '60s' })
    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    // A candidate short enough to judge over a corpus the live window is
    // too long for: the receipt stands, and what cannot be said is the
    // comparison, not the answer.
    expect(result.receipt?.emissionCount).toBe(3)
    expect(result.current?.receipt).toBeUndefined()
    expect(result.current?.decline?.definitionWindow).toBe('24h0m0s')
  })

  it('leaves current absent when the server sent none', async () => {
    stubFetch(200, RECEIPT)
    const result = await replayDefinition('port_scan', {})
    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    expect(result.current).toBeUndefined()
  })

  it('percent-encodes an id that would otherwise change the path', async () => {
    const fetchMock = stubFetch(200, RECEIPT)
    await replayDefinition('custom/one', {})
    expect(fetchMock.mock.calls[0][0]).toBe('/api/definitions/custom%2Fone/replay')
  })

  it('returns a decline as a value, not as a thrown error', async () => {
    stubFetch(200, {
      decline: {
        reason:
          'corpus covers 4h12m0s (8421 event(s)), shorter than this definition\'s 24h0m0s window -- declining rather than reporting a potentially misleading count',
        corpusSpan: '4h12m0s',
        definitionWindow: '24h0m0s',
      },
    })

    const result = await replayDefinition('low_slow_scan', {})
    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    // Not a receipt at all, and specifically not a receipt of zero: the
    // caller has to be able to tell "would not have fired" from "cannot
    // be asked of this corpus yet".
    expect(result.receipt).toBeUndefined()
    expect(result.decline?.corpusSpan).toBe('4h12m0s')
    expect(result.decline?.definitionWindow).toBe('24h0m0s')
  })

  it('returns the server refusal as a string when the replay is refused outright', async () => {
    stubFetch(503, 'no event corpus is available to replay against')
    const result = await replayDefinition('port_scan', {})
    expect(result).toBe('no event corpus is available to replay against')
  })

  it('falls back to a status-bearing message when the refusal carries no body', async () => {
    stubFetch(403, '')
    expect(await replayDefinition('port_scan', {})).toBe('replayDefinition: 403')
  })
})

// fetchSetupCommands (#436): POST /api/setup/commands, the wizard's
// version-aware RouterOS command blocks. `version` is the one field that
// is sometimes present and sometimes not, depending on whether the
// operator has picked one from the wizard's list -- these pin that
// JSON.stringify's own handling of an `undefined` property (dropping it
// rather than sending `"version":null`) is actually what goes on the
// wire, since a server that treats a present-but-null field differently
// from an absent one would otherwise disagree with this client silently.
describe('fetchSetupCommands (#436)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubFetch(status: number, body: unknown) {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => (typeof body === 'string' ? body : JSON.stringify(body)),
    }))
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  const RESPONSE = {
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: { commands: '', note: '' },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
  }

  it('posts to /api/setup/commands and sends version when the operator picked one', async () => {
    const fetchMock = stubFetch(200, RESPONSE)
    await fetchSetupCommands({
      address: 'mv.example.net:8443',
      syslogPort: ':6514',
      token: 'one-time-token',
      kinds: ['filter', 'nat'],
      version: '7.24.1',
    })

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/setup/commands')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({
      address: 'mv.example.net:8443',
      syslogPort: ':6514',
      token: 'one-time-token',
      kinds: ['filter', 'nat'],
      version: '7.24.1',
    })
  })

  it('omits version entirely rather than sending it empty when nothing was picked', async () => {
    const fetchMock = stubFetch(200, RESPONSE)
    await fetchSetupCommands({ address: 'mv.example.net:8443' })

    const [, init] = fetchMock.mock.calls[0]
    const sent = JSON.parse(String(init?.body))
    expect(sent).toEqual({ address: 'mv.example.net:8443' })
    expect('version' in sent).toBe(false)
    expect('token' in sent).toBe(false)
    expect('kinds' in sent).toBe(false)
  })

  it('returns the response as a value', async () => {
    stubFetch(200, RESPONSE)
    const result = await fetchSetupCommands({ address: 'mv.example.net:8443' })
    expect(typeof result).not.toBe('string')
    if (typeof result === 'string') return
    expect(result.routeros.minimum).toBe('7.18')
  })

  it('returns the server refusal as a string', async () => {
    stubFetch(403, 'not signed in')
    expect(await fetchSetupCommands({ address: 'mv.example.net:8443' })).toBe('not signed in')
  })

  it('falls back to a status-bearing message when the refusal carries no body', async () => {
    stubFetch(500, '')
    expect(await fetchSetupCommands({ address: 'mv.example.net:8443' })).toBe('fetchSetupCommands: 500')
  })
})

// #1162: a failed request used to read as the function that made it and
// a number -- "mark as expected: setFlagVerdict: 403", "Could not clear
// all flags: clearAllFlags: 500" -- while the server's own words for it
// ("user role required", "flag not found") sat unread in the body.
describe('the message a failed request carries (#1162)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubFetch(status: number, body: string) {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: status >= 200 && status < 300,
        status,
        json: async () => ({}),
        text: async () => body,
      })),
    )
  }

  it('forwards what the server said', async () => {
    stubFetch(403, 'user role required')
    await expect(setFlagVerdict('f1', 'expected')).rejects.toThrow('user role required')
  })

  it('keeps the status on the error, which is what a 401 bounce reads', async () => {
    stubFetch(401, '')
    await expect(fetchEventsWindow({})).rejects.toMatchObject({ status: 401 })
  })

  it('answers in words, not a bare number, when the refusal carries no body', async () => {
    stubFetch(403, '')
    await expect(clearAllFlags()).rejects.toThrow('you are not allowed to do that')

    stubFetch(500, '   ')
    await expect(clearAllFlags()).rejects.toThrow('the server could not do that (500)')
  })

  // Anything in front of mikroview answers in HTML; mikroview's own
  // handlers answer with one short line (internal/api's httpError).
  it('does not paste a proxy’s HTML error page into the message', async () => {
    stubFetch(502, '<!doctype html><html><body>502 Bad Gateway</body></html>')
    await expect(fetchAuditLog()).rejects.toThrow('the server could not do that (502)')
  })
})

// The server registers DELETE /api/droplist/{cidr...} (internal/api) --
// the CIDR belongs in the path, not a JSON body. Droplist.svelte.test.ts
// mocks this whole module, so it only proves the call happened, never
// that it hit the right URL; this is what actually exercises the request.
describe('deleteDroplistEntry (#1225)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the CIDR in the path, not a JSON body', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)

    await deleteDroplistEntry('203.0.113.0/24')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/droplist/203.0.113.0%2F24')
    expect(init?.method).toBe('DELETE')
    expect(init?.body).toBeUndefined()
  })
})

// The server registers DELETE /api/devices/{id} (internal/api/devices.go,
// #1369). Same null-on-success, message-on-failure shape as burnEnrolment.
describe('deleteDevice (#1369)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('DELETEs the device by id and resolves null on success', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 204, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await deleteDevice('rb5009')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/devices/rb5009')
    expect(init?.method).toBe('DELETE')
    expect(result).toBeNull()
  })

  it('resolves the server’s message on failure, e.g. a config.yaml-declared device', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 400,
      text: async () => 'device: this device is declared in config.yaml; remove it there instead',
    }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await deleteDevice('rb5009')

    expect(result).toBe('device: this device is declared in config.yaml; remove it there instead')
  })
})

// A dropped connection used to escape every mutating call as a thrown
// TypeError, past the caller's busy flag: "minting…" stuck until a
// reload, with no error shown. The four mutating helpers now answer it
// as a refusal instead, so each caller's existing error path shows it
// (v0.6.0 audit, Robustness stage; the class #1218's finding 7 guarded
// at three call sites by hand).
describe('a dropped connection is a refusal, not a throw', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubDroppedConnection() {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )
  }

  it('POST: mintDroplistKey returns the reason as a string', async () => {
    stubDroppedConnection()
    const result = await mintDroplistKey('203.0.113.9')
    expect(typeof result).toBe('string')
    expect(result).toContain('connection dropped')
    expect(result).toContain('Failed to fetch')
  })

  it('DELETE: revokeDroplistKey returns the reason as a string', async () => {
    stubDroppedConnection()
    const result = await revokeDroplistKey()
    expect(typeof result).toBe('string')
    expect(result).toContain('connection dropped')
  })

  it('PUT: saveSetupBackupTransport returns the reason as a string', async () => {
    stubDroppedConnection()
    const result = await saveSetupBackupTransport('sftp')
    expect(typeof result).toBe('string')
    expect(result).toContain('connection dropped')
  })

  it('PATCH: setRouterBackupComment returns the reason as a string', async () => {
    stubDroppedConnection()
    const result = await setRouterBackupComment('core', 'g1', 'note')
    expect(typeof result).toBe('string')
    expect(result).toContain('connection dropped')
  })
})

// #1249: a correct password on an account holding a factor answers 200
// with { secondFactor } and no session, not a 4xx -- login() has to read
// the body on success to tell that apart from an ordinary sign-in, which
// AuthState.login() (auth.svelte.test.ts) is mocked past and so never
// actually exercises this parsing.
describe('login: telling a pending second factor apart from success (#1249)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns null for an ordinary sign-in with no body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, text: async () => '', json: async () => { throw new Error('no body') } })),
    )
    const result = await login('tom', 'hunter2')
    expect(result).toBeNull()
  })

  it('returns the pending-factor shape when the server names one', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, text: async () => '', json: async () => ({ secondFactor: ['totp'] }) })),
    )
    const result = await login('tom', 'hunter2')
    expect(result).toEqual({ secondFactor: ['totp'] })
  })

  it('still returns the server text as an error string on a real failure', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'invalid username or password' })))
    const result = await login('tom', 'wrong')
    expect(result).toBe('invalid username or password')
  })

  // #1250: an account whose only factor is a stale passkey answers with
  // an *empty* secondFactor array, not an absent field -- the password
  // still must not be read as a completed sign-in. A length check here
  // used to treat this the same as the no-body case above.
  it('is still pending, not a success, when the server lists no usable factor at all', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, text: async () => '', json: async () => ({ secondFactor: [] }) })),
    )
    const result = await login('tom', 'hunter2')
    expect(result).toEqual({ secondFactor: [], passkeyOrigin: undefined })
  })

  it('carries passkeyOrigin through when the server sends one', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: true,
        status: 200,
        text: async () => '',
        json: async () => ({ secondFactor: ['passkey', 'totp'], passkeyOrigin: 'https://mikroview.example.org' }),
      })),
    )
    const result = await login('tom', 'hunter2')
    expect(result).toEqual({ secondFactor: ['passkey', 'totp'], passkeyOrigin: 'https://mikroview.example.org' })
  })
})

describe('the TOTP enrol/confirm/disable calls (#1249)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('enrolTOTP returns the otpauth URI on success', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ uri: 'otpauth://totp/MikroView:tom?secret=ABC&issuer=MikroView' }) })))
    const result = await enrolTOTP()
    expect(result).toEqual({ uri: 'otpauth://totp/MikroView:tom?secret=ABC&issuer=MikroView' })
  })

  it('confirmTOTP returns the ten recovery codes on success', async () => {
    const codes = Array.from({ length: 10 }, (_, i) => `code-${i}`)
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ recoveryCodes: codes }) })))
    const result = await confirmTOTP('123456')
    expect(result).toEqual({ recoveryCodes: codes, alreadyIssued: false })
  })

  // #1250: recovery codes are shared with passkeys and minted once --
  // TOTP confirm no longer always returns a fresh set.
  it('confirmTOTP reports alreadyIssued with no codes when a passkey already minted them', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ recoveryCodes: null, alreadyIssued: true }) })),
    )
    const result = await confirmTOTP('123456')
    expect(result).toEqual({ recoveryCodes: null, alreadyIssued: true })
  })

  // The server answers a wrong code with 400 (internal/api's
  // handleTOTPConfirm), never 401 -- 401 there is reserved for "this
  // session no longer exists", checked separately below.
  it('confirmTOTP returns the server refusal as a string on a wrong code', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 400, text: async () => 'wrong code' })))
    const result = await confirmTOTP('000000')
    expect(result).toBe('wrong code')
  })

  // Every one of the four forced-enrolment routes (enrolTOTP,
  // confirmTOTP, beginPasskeyRegistration, finishPasskeyRegistration)
  // needs an existing session before it does anything else, so a 401
  // from any of them can only mean that session died -- thrown, so the
  // forced-enrolment door (which has no cancel/skip/sign-out of its own)
  // can send it through authState.handleUnauthorized() instead of
  // showing it as plain text with nothing else on screen.
  it('confirmTOTP throws on a 401, rather than returning it as text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'sign in first' })))
    await expect(confirmTOTP('123456')).rejects.toMatchObject({ status: 401, message: 'sign in first' })
  })

  it('enrolTOTP throws on a 401, rather than returning it as text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'sign in first' })))
    await expect(enrolTOTP()).rejects.toMatchObject({ status: 401, message: 'sign in first' })
  })

  it('disableTOTP posts the password and reads signedOut off the body on success', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      json: async () => ({ disabled: true, signedOut: false }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await disableTOTP('hunter2')
    expect(result).toEqual({ signedOut: false })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/totp')
    expect(init?.method).toBe('DELETE')
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
  })

  // #1253: turning off the account's last second factor signs the
  // caller out everywhere -- the server says so back rather than
  // answering exactly as it would for the ordinary case.
  it('disableTOTP reports signedOut: true when it was the account\'s last factor', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ disabled: true, signedOut: true }) })),
    )
    const result = await disableTOTP('hunter2')
    expect(result).toEqual({ signedOut: true })
  })

  // An older server (or a body-less 200, matching this route's shape
  // before #1253) has no signedOut field at all -- read as false, the
  // safe direction to be wrong in.
  it('disableTOTP reads a body with no signedOut field as signedOut: false', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ disabled: true }) })))
    const result = await disableTOTP('hunter2')
    expect(result).toEqual({ signedOut: false })
  })
})

// #1331: regenerating recovery codes without touching either factor.
describe('regenerateRecoveryCodes (#1331)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts the password and returns the fresh ten on success', async () => {
    const codes = Array.from({ length: 10 }, (_, i) => `code-${i}`)
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      json: async () => ({ recoveryCodes: codes }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await regenerateRecoveryCodes('hunter2')

    expect(result).toEqual(codes)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/recovery-codes')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
  })

  it('returns the server refusal as a string on a wrong password', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'incorrect password' })))
    const result = await regenerateRecoveryCodes('wrong')
    expect(result).toBe('incorrect password')
  })

  it('refuses with the account has no second factor message when none exists', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 409, text: async () => 'this account has no second factor yet' })),
    )
    const result = await regenerateRecoveryCodes('hunter2')
    expect(result).toBe('this account has no second factor yet')
  })
})

describe('submitLoginFactor and clearUserTOTP (#1249)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('submitLoginFactor posts the code and returns null on success', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await submitLoginFactor('123456')
    expect(result).toBeNull()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/login/factor')
    expect(JSON.parse(init?.body as string)).toEqual({ code: '123456' })
  })

  it('submitLoginFactor accepts a recovery code in the same box, unchanged shape', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)
    await submitLoginFactor('a1b2-c3d4-e5f6-g7h8')
    const [, init] = fetchMock.mock.calls[0]
    expect(JSON.parse(init?.body as string)).toEqual({ code: 'a1b2-c3d4-e5f6-g7h8' })
  })

  it('clearUserTOTP sends no body, identifying the target by URL', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await clearUserTOTP('u-42')
    expect(result).toBeNull()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/users/u-42/totp')
    expect(init?.method).toBe('DELETE')
    expect(init?.body).toBeUndefined()
  })
})

describe('the passkey calls (#1250)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetchPasskeys GETs the list', async () => {
    const rows = [{ id: 'c1', name: 'laptop', createdAt: '2026-01-01T00:00:00Z', transports: [], stale: false, rpId: 'mikroview.example.org' }]
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => rows })))
    const result = await fetchPasskeys()
    expect(result).toEqual(rows)
  })

  it('beginPasskeyRegistration posts an empty body and returns the library\'s own creation options', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, json: async () => ({ publicKey: { challenge: 'c' } }) }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await beginPasskeyRegistration()
    expect(result).toEqual({ publicKey: { challenge: 'c' } })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/passkeys/register/begin')
    expect(init?.method).toBe('POST')
  })

  it('finishPasskeyRegistration posts the credential JSON and the name together', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      json: async () => ({ passkey: { id: 'c1' }, recoveryCodes: null }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await finishPasskeyRegistration({ id: 'c1', response: {} }, 'my key')
    expect(result).toEqual({ passkey: { id: 'c1' }, recoveryCodes: null })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/passkeys/register/finish')
    expect(JSON.parse(init?.body as string)).toEqual({ credential: { id: 'c1', response: {} }, name: 'my key' })
  })

  // Same reasoning as confirmTOTP/enrolTOTP above -- these are two of
  // the same four forced-enrolment routes.
  it('beginPasskeyRegistration throws on a 401, rather than returning it as text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'sign in first' })))
    await expect(beginPasskeyRegistration()).rejects.toMatchObject({ status: 401, message: 'sign in first' })
  })

  it('finishPasskeyRegistration throws on a 401, rather than returning it as text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401, text: async () => 'sign in first' })))
    await expect(finishPasskeyRegistration({ id: 'c1', response: {} }, 'my key')).rejects.toMatchObject({
      status: 401,
      message: 'sign in first',
    })
  })

  it('renamePasskey PATCHes the name to the credential\'s own URL', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await renamePasskey('c1', 'work laptop')
    expect(result).toBeNull()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/passkeys/c1')
    expect(init?.method).toBe('PATCH')
    expect(JSON.parse(init?.body as string)).toEqual({ name: 'work laptop' })
  })

  it('disablePasskey DELETEs with the password in the body and reads signedOut off the response', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      json: async () => ({ removed: true, signedOut: false }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await disablePasskey('c1', 'hunter2')
    expect(result).toEqual({ signedOut: false })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/passkeys/c1')
    expect(init?.method).toBe('DELETE')
    expect(JSON.parse(init?.body as string)).toEqual({ password: 'hunter2' })
  })

  // Same #1253 contract as disableTOTP's own tests.
  it('disablePasskey reports signedOut: true when it was the account\'s last factor', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ removed: true, signedOut: true }) })),
    )
    const result = await disablePasskey('c1', 'hunter2')
    expect(result).toEqual({ signedOut: true })
  })

  it('beginPasskeyLogin/submitPasskeyLoginAssertion share the login/factor route shapes with submitLoginFactor', async () => {
    const beginMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, json: async () => ({ publicKey: { challenge: 'c' } }) }))
    vi.stubGlobal('fetch', beginMock)
    const begin = await beginPasskeyLogin()
    expect(begin).toEqual({ publicKey: { challenge: 'c' } })
    expect(beginMock.mock.calls[0][0]).toBe('/api/auth/login/factor/begin')

    const assertMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', assertMock)
    const result = await submitPasskeyLoginAssertion({ id: 'c1' })
    expect(result).toBeNull()
    const [url, init] = assertMock.mock.calls[0]
    expect(url).toBe('/api/auth/login/factor')
    expect(JSON.parse(init?.body as string)).toEqual({ assertion: { id: 'c1' } })
  })

  it('clearUserPasskeys sends no body, identifying the target by URL', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => ({ ok: true, status: 200, text: async () => '' }))
    vi.stubGlobal('fetch', fetchMock)
    const result = await clearUserPasskeys('u-42')
    expect(result).toBeNull()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/auth/users/u-42/passkeys')
    expect(init?.method).toBe('DELETE')
    expect(init?.body).toBeUndefined()
  })
})
