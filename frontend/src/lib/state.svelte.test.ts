// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { applyFilters } from './state.svelte'
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

  // Number("abc") is NaN, and every `!==` comparison against NaN is
  // true -- an unguarded filter would hide every event, including ones
  // with no port at all, reading as "no traffic" while the operator is
  // still mid-typing a port number.
  it('treats a non-numeric value as unfiltered rather than hiding everything', () => {
    const events = [evt({ id: 1, srcPort: 443 }), evt({ id: 2, dstPort: 22 })]
    const got = applyFilters(events, { ...emptyFilters(), port: 'abc' })
    expect(got).toHaveLength(2)
  })
})

