// SPDX-License-Identifier: AGPL-3.0-only

import { flushSync } from 'svelte'
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
import { MAX_CLIENT_EVENTS } from './constants'
import { matchingIds } from './ruleMatcher'
import { emptyFilters, type ClientEvent, type FirewallEvent, type Filters, type Stats } from './types'

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

// #549's Loading chrome state (LiveTable/Fleet's ghost-rows branch) reads
// initialLoadDone to tell "the first fetch hasn't come back" apart from
// "it came back and there is genuinely nothing" -- both look like an
// empty buffer. These prove the flag itself settles correctly on both
// outcomes, independent of any component reading it.
describe('AppState.initialLoadDone (#549)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    appState.initialLoadDone = false
  })

  it('is set once loadInitial succeeds', async () => {
    vi.mocked(fetchEvents).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    vi.mocked(fetchDevices).mockResolvedValue([])
    vi.mocked(fetchStats).mockResolvedValue({
      total: 0,
      byAction: {},
      topRules: [],
      timeline: [],
    } as unknown as Awaited<ReturnType<typeof fetchStats>>)

    expect(appState.initialLoadDone).toBe(false)
    await appState.loadInitial()
    expect(appState.initialLoadDone).toBe(true)
  })

  it('is also set when loadInitial fails -- a failure still ends the "still loading" window', async () => {
    vi.mocked(fetchEvents).mockRejectedValue(new Error('network error'))
    vi.mocked(fetchDevices).mockResolvedValue([])
    vi.mocked(fetchStats).mockResolvedValue({
      total: 0,
      byAction: {},
      topRules: [],
      timeline: [],
    } as unknown as Awaited<ReturnType<typeof fetchStats>>)

    await expect(appState.loadInitial()).rejects.toThrow()
    expect(appState.initialLoadDone).toBe(true)
  })
})


// Hold-while-open (#445, sharing #413's decision). The interesting case
// is not the arithmetic -- it is that holders take the hold from inside
// an $effect, because that is what makes the release survive an unmount.
// An earlier version counted in a $state field, so `count++` read the
// signal it was about to write and the effect re-triggered itself
// forever. Svelte aborts that with effect_update_depth_exceeded and
// stops re-rendering the whole app: popovers stuck on "Loading…" and Esc
// did nothing. Nothing in the type system or the rest of this suite
// objected; it took running the app. So the test is written the way the
// real caller behaves, from inside an effect, rather than by calling the
// methods directly.
describe('AppState stream hold (#445)', () => {
  it('settles when taken and released from inside an effect', () => {
    let open = $state(false)
    let runs = 0

    const stop = $effect.root(() => {
      $effect(() => {
        runs++
        if (!open) return
        appState.holdStream()
        return () => appState.releaseStream()
      })
    })

    flushSync()
    const runsAfterMount = runs
    expect(appState.streamHeld).toBe(false)

    open = true
    flushSync()
    expect(appState.streamHeld).toBe(true)
    // One further run for the change itself. Anything more means the
    // effect is feeding itself.
    expect(runs).toBe(runsAfterMount + 1)

    open = false
    flushSync()
    expect(appState.streamHeld).toBe(false)

    stop()
  })

  it('holds until the last holder releases', () => {
    appState.holdStream()
    appState.holdStream()
    appState.releaseStream()
    expect(appState.streamHeld).toBe(true)
    appState.releaseStream()
    expect(appState.streamHeld).toBe(false)
    // Never goes negative, so a stray release cannot bank credit that
    // silently swallows the next real hold.
    appState.releaseStream()
    appState.holdStream()
    expect(appState.streamHeld).toBe(true)
    appState.releaseStream()
    expect(appState.streamHeld).toBe(false)
  })
})

// #993: a rename racing the debounced server refetch. refetchWithFilters
// replaces `events` wholesale with the server's snapshot, and the server
// stamps names at ingest -- so a snapshot taken before the rename carries
// the old name even when its response lands *after* relabel() has
// rewritten the buffer (~9 ms after, in the #611 trace), silently undoing
// the rename on every visible row. The guard records relabels taken while
// a fetch is in flight and applies them, one-shot, to that fetch's
// response; fetches issued after the save are the server's problem (its
// buffer is re-stamped on entity upsert, same one-shot philosophy).
describe('relabel racing an in-flight refetch (#993)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    appState.events = []
    appState.filters = emptyFilters()
    appState.fetchFailed = false
  })

  function deferredFetch() {
    let land!: (r: Awaited<ReturnType<typeof fetchEvents>>) => void
    vi.mocked(fetchEvents).mockImplementationOnce(
      () => new Promise((resolve) => { land = resolve }),
    )
    return (events: FirewallEvent[]) =>
      land({ events, hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })
  }

  it('a refetch snapshotted before a rename cannot resurface the old name by landing after relabel', async () => {
    appState.setInitialEvents([evt({ id: 1, srcIp: '10.0.0.9' })])

    const land = deferredFetch()
    const refetch = appState.refetchWithFilters()

    // The save lands while that fetch is in flight...
    appState.relabel('host', '10.0.0.9', 'jellyfish')
    expect(appState.events[0]?.srcHostName).toBe('jellyfish')

    // ...and the stale pre-rename snapshot arrives afterwards.
    land([evt({ id: 1, srcIp: '10.0.0.9' })])
    await refetch

    expect(appState.events[0]?.srcHostName).toBe('jellyfish')
  })

  it('the rewrite dies with the flight it raced -- a later response is applied exactly as the server sent it', async () => {
    appState.setInitialEvents([evt({ id: 1, srcIp: '10.0.0.9' })])

    const land = deferredFetch()
    const refetch = appState.refetchWithFilters()
    appState.relabel('host', '10.0.0.9', 'jellyfish')
    land([evt({ id: 1, srcIp: '10.0.0.9' })])
    await refetch

    // No fetch in flight now. A later response carrying a different name
    // (say, RouterOS started supplying one) must win untouched: replaying
    // old relabels onto it would be the standing overlay relabel()'s own
    // comment records as rejected.
    vi.mocked(fetchEvents).mockResolvedValue({
      events: [evt({ id: 1, srcIp: '10.0.0.9', srcHostName: 'android-dhcp' })],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    await appState.refetchWithFilters()

    expect(appState.events[0]?.srcHostName).toBe('android-dhcp')
  })
})

// #1218 audit finding 10: outrunEpisode's two defects. outrun is a
// server-side lifetime counter with no freshness of its own -- setStats
// is what turns it into "is this still happening" -- and both bugs were
// in that translation.
describe('AppState.setStats and the outrun episode (#1109, #1218 finding 10)', () => {
  function stats(overrides: Partial<Stats> = {}): Stats {
    return {
      total: 0,
      byAction: {},
      topRules: [],
      timeSeries: [],
      eventsPerSecond: 0,
      capacity: 1000,
      count: 0,
      windowSeconds: 900,
      oldestHeld: null,
      connectedClients: 0,
      ...overrides,
    }
  }

  beforeEach(() => {
    appState.reset()
  })

  // A lifetime counter that is already nonzero on the very first poll of
  // a session accumulated over however long the server has been running,
  // not in the last 60 seconds this tab has been open -- folding it
  // straight into a brand-new episode used to raise a fresh outrun
  // warning on every page load, however old the actual events were.
  it('does not raise a fresh episode from an already-nonzero counter on the first poll', () => {
    appState.setStats(stats({ engine: { behind: 0, behindSeconds: 0, outrun: 5000 } }))

    expect(appState.outrunEpisode.recent).toBe(0)
    expect(appState.outrunActive).toBe(false)
    // The baseline is still recorded -- a *second* poll's growth past it
    // is what should read as recent, which the next test covers.
    expect(appState.outrunEpisode.total).toBe(5000)
  })

  it('reads a later increase past that seeded baseline as recent activity', () => {
    appState.setStats(stats({ engine: { behind: 0, behindSeconds: 0, outrun: 5000 } }))
    appState.setStats(stats({ engine: { behind: 0, behindSeconds: 0, outrun: 5010 } }))

    expect(appState.outrunEpisode.recent).toBe(10)
    expect(appState.outrunActive).toBe(true)
  })

  // tick()'s own comment: `now` is deliberately frozen while paused, for
  // filteredEvents' display-duration cutoff. outrunActive used to be
  // computed against that same frozen clock, so pausing the live view
  // froze whether a flood read as "still happening" too -- a flood that
  // had genuinely stopped stayed reported as active for as long as the
  // tab stayed paused, however long that was.
  it('keeps aging the outrun episode while paused', () => {
    appState.setStats(stats({ engine: { behind: 0, behindSeconds: 0, outrun: 100 } }))
    appState.setStats(stats({ engine: { behind: 0, behindSeconds: 0, outrun: 110 } }))
    expect(appState.outrunActive).toBe(true)

    appState.paused = true
    const start = appState.wallNow
    vi.spyOn(Date, 'now').mockReturnValue(start + 61_000)
    appState.tick()

    expect(appState.paused).toBe(true)
    expect(appState.outrunActive).toBe(false)

    vi.restoreAllMocks()
  })
})

// #1304 E2: chainOptions/srcCountryOptions/dstCountryOptions used to be
// $derived.by scans of the whole `events` buffer, so every flush re-walked
// up to MAX_CLIENT_EVENTS items just to notice a value already counted.
// They are now kept by running counts, updated only by the events that
// actually joined or left the buffer.
describe('#1304 E2: chain/country option lists stay correct incrementally', () => {
  beforeEach(() => {
    appState.reset()
  })

  it('picks up a newly observed chain and source country, then drops them once evicted', async () => {
    appState.appendLive([evt({ id: 1, chain: 'mangle', srcIp: '203.0.113.9', srcCountry: 'FR' })])
    await new Promise((r) => setTimeout(r, 220))

    expect(appState.chainOptions).toContain('mangle')
    expect(appState.srcCountryOptions.map((o) => o.value)).toContain('FR')

    // Exactly enough unrelated events to push event id 1 out of the
    // MAX_CLIENT_EVENTS window -- the only thing that should evict it.
    const filler = Array.from({ length: MAX_CLIENT_EVENTS }, (_, i) =>
      evt({ id: i + 100, chain: 'forward', srcIp: '198.51.100.1', srcCountry: 'US' }),
    )
    appState.appendLive(filler)
    await new Promise((r) => setTimeout(r, 220))

    expect(appState.chainOptions).not.toContain('mangle')
    expect(appState.srcCountryOptions.map((o) => o.value)).not.toContain('FR')
    // Builtins and the now-dominant country survive the eviction untouched.
    expect(appState.chainOptions).toEqual(['input', 'forward', 'output', 'srcnat', 'dstnat'])
    expect(appState.srcCountryOptions.map((o) => o.value)).toContain('US')
  }, 10000)

  // The correctness test above passes even against the old full-rescan
  // implementation -- both produce the same answer, just at different
  // cost. This is the regression test for the actual audit finding: the
  // filter bar reads all three option lists on every render (see
  // FilterBar.svelte), so a rescan-on-every-change implementation makes
  // that read cost scale with the whole buffer. Asserted as a cost
  // *ratio* between a small and a huge buffer, not a wall-clock ceiling,
  // for the same contended-shared-runner reason as the LiveTable #728
  // test above it in this suite -- appending the same 5 events should
  // cost about the same whether the buffer already holds 200 or 20,000.
  it('costs about the same to append a handful of events whether the buffer holds hundreds or tens of thousands', () => {
    function clientEvt(overrides: Partial<FirewallEvent> = {}): ClientEvent {
      return { ...evt(overrides), receivedAt: Date.now() }
    }
    // Measured through applyOptionCountDelta directly, not appendLive or
    // even appendUnseen -- both of those also pay an unrelated O(buffer)
    // cost building the id-dedup Set, which would swamp this measurement
    // without narrowing what it says about E2's specific fix. This is the
    // exact method the option lists are now maintained through.
    function callApplyDelta(added: readonly ClientEvent[], removed: readonly ClientEvent[] = []) {
      ;(
        appState as unknown as {
          applyOptionCountDelta(a: readonly ClientEvent[], r: readonly ClientEvent[]): void
        }
      ).applyOptionCountDelta(added, removed)
    }

    // Summed over many reps (each starting from a fresh buffer of the
    // given size, built outside the timed section) so the signal clears
    // timer-resolution noise -- a single sub-millisecond call is not
    // reliably measurable on its own.
    function totalCostOfFiveArrivals(bufferSize: number, reps: number): number {
      let total = 0
      for (let rep = 0; rep < reps; rep++) {
        const base = rep * 100_000
        appState.setInitialEvents(
          Array.from({ length: bufferSize }, (_, i) =>
            evt({ id: base + i + 1, chain: 'forward', srcIp: '198.51.100.1', srcCountry: 'US' }),
          ),
        )
        const fresh = Array.from({ length: 5 }, (_, i) =>
          clientEvt({ id: base + bufferSize + i + 1, chain: 'forward', srcIp: '198.51.100.1', srcCountry: 'US' }),
        )
        const t0 = performance.now()
        callApplyDelta(fresh)
        total += performance.now() - t0
      }
      return total
    }

    // Unmeasured warm-up: module/JIT costs would otherwise swamp the
    // small-N sample below.
    totalCostOfFiveArrivals(50, 5)

    const small = 200
    const large = MAX_CLIENT_EVENTS
    const reps = 40
    const smallCost = totalCostOfFiveArrivals(small, reps)
    const largeCost = totalCostOfFiveArrivals(large, reps)

    const bufferRatio = large / small // 100
    const costRatio = largeCost / Math.max(smallCost, 0.5)
    // A per-arrival rescan of the whole buffer would make this scale with
    // buffer size (~100x here); incremental maintenance keeps it flat.
    expect(costRatio).toBeLessThan(bufferRatio / 4)
  }, 20000)
})
