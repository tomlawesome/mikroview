// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildQuery, fetchSetupCommands, replayDefinition } from './api'
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
