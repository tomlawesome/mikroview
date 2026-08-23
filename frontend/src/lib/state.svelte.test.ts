// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'

// Mocked so loadInitial()/refetchWithFilters() below can be made to
// reject on demand, the same way a 503 or a dropped connection would --
// see the "swallowed failure" tests (issue #373).
vi.mock('./api', () => ({
  fetchEvents: vi.fn(),
  fetchDevices: vi.fn(),
  fetchStats: vi.fn(),
}))

import { fetchDevices, fetchEvents, fetchStats } from './api'
import { appState, applyFilters } from './state.svelte'
import { matchingIds } from './ruleMatcher'
import { emptyFilters, type FirewallEvent, type Filters } from './types'

// Covers applyFilters's rule/ruleRegex branch specifically -- a
// performance audit found the regex used to be constructed inside the
// per-event filter callback (recompiling the same pattern for every
// event on every call) instead of once per call. The fix must preserve
// exact matching behavior, including the "invalid pattern degrades to
// unfiltered" case -- these tests would fail under either a matching
// regression or the invalid-pattern case now differing per-event.
function evt(overrides: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00Z',
    deviceId: 'core',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'lan-wan',
    chain: 'forward',
    raw: 'A|lan-wan|forward: ...',
    ...overrides,
  }
}

describe('applyFilters rule/ruleRegex', () => {
  it('matches via substring when ruleRegex is off', () => {
    const events = [evt({ ruleLabel: 'wan-block-scan' }), evt({ ruleLabel: 'lan-internal' })]
    const got = applyFilters(events, { ...emptyFilters(), rule: 'block' })
    expect(got.map((e) => e.ruleLabel)).toEqual(['wan-block-scan'])
  })

  // Regex semantics moved out of applyFilters with #157 -- it no longer
  // compiles or runs a pattern. These two assertions live on matchingIds
  // now (see ruleMatcher.test.ts), which is where the matching happens;
  // what applyFilters owes is honouring the set it is handed, below.
  it('honours a match set that spans both label and raw matches', () => {
    const events = [
      evt({ id: 1, ruleLabel: 'wan-block-scan' }),
      evt({ id: 2, ruleLabel: 'other', raw: 'contains-block-in-raw-only' }),
      evt({ id: 3, ruleLabel: 'lan-internal', raw: 'nothing relevant' }),
    ]
    const got = applyFilters(
      events,
      { ...emptyFilters(), rule: '^wan-.*|block-in-raw', ruleRegex: true },
      new Set(matchingIds('^wan-.*|block-in-raw', events.map((e) => ({ id: e.id, ruleLabel: e.ruleLabel, raw: e.raw })))),
    )
    expect(got.map((e) => e.ruleLabel)).toEqual(['wan-block-scan', 'other'])
  })

  it('treats an invalid pattern as unfiltered rather than throwing or hiding everything', () => {
    const events = [evt({ ruleLabel: 'a' }), evt({ ruleLabel: 'b' }), evt({ ruleLabel: 'c' })]
    expect(() =>
      applyFilters(events, { ...emptyFilters(), rule: '(unterminated', ruleRegex: true }),
    ).not.toThrow()
    const got = applyFilters(events, { ...emptyFilters(), rule: '(unterminated', ruleRegex: true })
    expect(got).toHaveLength(3)
  })

  it('keeps only the events in the precomputed match set', () => {
    const events = [
      evt({ id: 1, ruleLabel: 'match-1' }),
      evt({ id: 2, ruleLabel: 'skip' }),
      evt({ id: 3, ruleLabel: 'match-2' }),
      evt({ id: 4, ruleLabel: 'skip' }),
      evt({ id: 5, ruleLabel: 'match-3' }),
    ]
    const got = applyFilters(
      events,
      { ...emptyFilters(), rule: '^match-', ruleRegex: true },
      new Set([1, 3, 5]),
    )
    expect(got.map((e) => e.id)).toEqual([1, 3, 5])
  })

  // A null set covers three cases that must all behave identically and
  // harmlessly: not evaluated yet, invalid pattern, and a pattern refused
  // for overrunning. Hiding everything would look like "no matches",
  // which is a lie the operator would act on.
  it('leaves events unfiltered when there is no usable match set', () => {
    const events = [evt({ id: 1, ruleLabel: 'a' }), evt({ id: 2, ruleLabel: 'b' })]
    const got = applyFilters(events, { ...emptyFilters(), rule: '(', ruleRegex: true }, null)
    expect(got).toHaveLength(2)
  })

  // applyFilters must not compile or execute a regex itself -- that is
  // the entire point of #157. A pattern that would hang a backtracking
  // engine has to pass straight through.
  it('does not execute the pattern, so a catastrophic one costs nothing', () => {
    const events = [evt({ id: 1, ruleLabel: 'a'.repeat(40) })]
    const start = performance.now()
    applyFilters(events, { ...emptyFilters(), rule: '(a+)+$', ruleRegex: true }, null)
    expect(performance.now() - start).toBeLessThan(50)
  })
})

describe('applyFilters port', () => {
  it('matches an event by source or destination port', () => {
    const events = [
      evt({ id: 1, srcPort: 443 }),
      evt({ id: 2, dstPort: 443 }),
      evt({ id: 3, srcPort: 80, dstPort: 8080 }),
    ]
    const got = applyFilters(events, { ...emptyFilters(), port: '443' })
    expect(got.map((e) => e.id)).toEqual([1, 2])
  })

  // #438: text is no longer silently ignored -- it searches the
  // displayed port label (operator name or well-known service name, see
  // lib/portMatch.ts/portMatch.test.ts for the full precedence table).
  // A label that matches nothing genuinely filters everything out,
  // which is the point of a text search box; it is only a *numeric*
  // value that is still exact-or-nothing on the port number itself.
  it('matches a well-known service name against the displayed port label', () => {
    const events = [evt({ id: 1, srcPort: 443 }), evt({ id: 2, dstPort: 22 })]
    const got = applyFilters(events, { ...emptyFilters(), port: 'https' })
    expect(got.map((e) => e.id)).toEqual([1])
  })

  it('a text value matching no port label filters out events that have no ports at all', () => {
    const events = [evt({ id: 1, srcPort: 443 }), evt({ id: 2, dstPort: 22 })]
    const got = applyFilters(events, { ...emptyFilters(), port: 'nonesuch' })
    expect(got).toHaveLength(0)
  })
})

// #438: srcQuery/dstQuery replaced the single "ip" box. The label/IP/CIDR
// precedence itself is pinned in addressMatch.test.ts's table tests; these
// cover applyFilters's own wiring -- which candidates each side builds,
// including the NAT-parity rule (srcnat/dstnat only, per
// internal/routeros/parser.go's isNATChain).
describe('applyFilters srcQuery/dstQuery', () => {
  it('matches the source and destination independently', () => {
    const events = [
      evt({ id: 1, srcIp: '10.0.0.5', dstIp: '203.0.113.9' }),
      evt({ id: 2, srcIp: '10.0.0.9', dstIp: '203.0.113.5' }),
    ]
    expect(applyFilters(events, { ...emptyFilters(), srcQuery: '10.0.0.5' }).map((e) => e.id)).toEqual([1])
    expect(applyFilters(events, { ...emptyFilters(), dstQuery: '203.0.113.5' }).map((e) => e.id)).toEqual([2])
  })

  it('matches the resolved label, live -- not frozen when the filter was set (#413 integration)', () => {
    const events = [evt({ id: 1, srcIp: '10.0.0.5', srcHostName: 'nas-basement' })]
    expect(applyFilters(events, { ...emptyFilters(), srcQuery: 'nas' })).toHaveLength(1)
  })

  it('includes a srcnat row\'s translated source, but not an unrelated forward row\'s inherited NAT annotation', () => {
    const natted = evt({
      id: 1,
      chain: 'srcnat',
      srcIp: '10.0.0.5',
      natIp: '198.51.100.9',
    })
    const inherited = evt({
      id: 2,
      chain: 'forward',
      srcIp: '10.0.0.6',
      natIp: '198.51.100.9',
    })
    const got = applyFilters([natted, inherited], { ...emptyFilters(), srcQuery: '198.51.100.9' })
    expect(got.map((e) => e.id)).toEqual([1])
  })

  it('includes a dstnat row\'s translated destination', () => {
    const events = [evt({ id: 1, chain: 'dstnat', dstIp: '192.168.1.10', natIp: '198.51.100.9' })]
    expect(applyFilters(events, { ...emptyFilters(), dstQuery: '198.51.100.9' })).toHaveLength(1)
  })

  it('a srcnat row\'s NAT address does not leak into destination matching', () => {
    const events = [evt({ id: 1, chain: 'srcnat', srcIp: '10.0.0.5', natIp: '198.51.100.9' })]
    expect(applyFilters(events, { ...emptyFilters(), dstQuery: '198.51.100.9' })).toHaveLength(0)
  })
})

describe('applyFilters srcCountry/dstCountry', () => {
  it('matches a resolved country code', () => {
    const events = [evt({ id: 1, srcIp: '8.8.8.8', srcCountry: 'US' }), evt({ id: 2, srcIp: '1.1.1.1', srcCountry: 'AU' })]
    expect(applyFilters(events, { ...emptyFilters(), srcCountry: 'US' }).map((e) => e.id)).toEqual([1])
  })

  it('the Unknown sentinel finds addressed rows with no resolved country, and excludes address-less ones', () => {
    const events = [
      evt({ id: 1, srcIp: '10.0.0.5' }), // has an address, no country -- unknown
      evt({ id: 2 }), // no source address at all -- not "unknown", not applicable
      evt({ id: 3, srcIp: '8.8.8.8', srcCountry: 'US' }),
    ]
    const got = applyFilters(events, { ...emptyFilters(), srcCountry: 'unknown' })
    expect(got.map((e) => e.id)).toEqual([1])
  })
})

describe('applyFilters chain (#438: same field, now reachable from the bar)', () => {
  it('matches the exact chain', () => {
    const events = [evt({ id: 1, chain: 'srcnat' }), evt({ id: 2, chain: 'forward' })]
    expect(applyFilters(events, { ...emptyFilters(), chain: 'srcnat' }).map((e) => e.id)).toEqual([1])
  })
})

describe('applyFilters rule (ruleName alias, #438)', () => {
  it('matches the operator-configured alias in addition to ruleLabel and raw', () => {
    const events = [evt({ id: 1, ruleLabel: 'r13', ruleName: 'block-guest-wifi' })]
    expect(applyFilters(events, { ...emptyFilters(), rule: 'guest-wifi' })).toHaveLength(1)
  })
})

// Issue #373: a failed refetch (or initial load) used to be indistinguishable
// from a genuinely empty result -- handleApiError (App.svelte) only acts on
// 401s, so the rejection from fetchEvents/fetchDevices/fetchStats was dropped
// on the floor and appState.events was simply left as whatever it already
// held. LiveTable then read that untouched buffer as a definite "nothing
// matches". These tests simulate the real failure (a rejected fetch, e.g. a
// 503 from the server) and check that appState records the failure rather
// than silently keeping the stale-but-plausible-looking buffer.
describe('AppState surfaces a failed refetch/initial load (issue #373)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    appState.events = []
    appState.filters = emptyFilters()
    appState.fetchFailed = false
  })

  it('flags the failure when refetchWithFilters rejects, and does not silently accept an empty buffer as the answer', async () => {
    // Buffer holds an event from before the filter was narrowed -- exactly
    // the "incomplete client-side buffer" the issue describes.
    appState.events = [evt({ id: 1 })] as unknown as (typeof appState)['events']

    vi.mocked(fetchEvents).mockRejectedValue(new Error('503 Service Unavailable'))

    await expect(appState.refetchWithFilters()).rejects.toThrow()

    // The real defect: without this, appState.events is left untouched and
    // nothing anywhere records that the query that would have proven
    // completeness never ran.
    expect(appState.fetchFailed).toBe(true)
  })

  it('clears the flag once a refetch actually succeeds', async () => {
    appState.fetchFailed = true
    vi.mocked(fetchEvents).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })

    await appState.refetchWithFilters()

    expect(appState.fetchFailed).toBe(false)
  })

  it('flags the failure when the initial load rejects', async () => {
    vi.mocked(fetchEvents).mockRejectedValue(new Error('network error'))
    vi.mocked(fetchDevices).mockResolvedValue([])
    vi.mocked(fetchStats).mockResolvedValue({
      total: 0,
      byAction: {},
      topRules: [],
      timeline: [],
    } as unknown as Awaited<ReturnType<typeof fetchStats>>)

    await expect(appState.loadInitial()).rejects.toThrow()

    expect(appState.fetchFailed).toBe(true)
  })
})

