// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { applyFilters, isSafeRulePattern } from './state.svelte'
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

  it('matches ruleLabel or raw via a real regex pattern', () => {
    const events = [
      evt({ ruleLabel: 'wan-block-scan' }),
      evt({ ruleLabel: 'other', raw: 'contains-block-in-raw-only' }),
      evt({ ruleLabel: 'lan-internal', raw: 'nothing relevant' }),
    ]
    const got = applyFilters(events, { ...emptyFilters(), rule: '^wan-.*|block-in-raw', ruleRegex: true })
    expect(got.map((e) => e.ruleLabel)).toEqual(['wan-block-scan', 'other'])
  })

  it('is case-insensitive in regex mode', () => {
    const events = [evt({ ruleLabel: 'WAN-Block-Scan' })]
    const got = applyFilters(events, { ...emptyFilters(), rule: 'wan-block', ruleRegex: true })
    expect(got).toHaveLength(1)
  })

  it('treats an invalid pattern as unfiltered rather than throwing or hiding everything', () => {
    const events = [evt({ ruleLabel: 'a' }), evt({ ruleLabel: 'b' }), evt({ ruleLabel: 'c' })]
    expect(() =>
      applyFilters(events, { ...emptyFilters(), rule: '(unterminated', ruleRegex: true }),
    ).not.toThrow()
    const got = applyFilters(events, { ...emptyFilters(), rule: '(unterminated', ruleRegex: true })
    expect(got).toHaveLength(3)
  })

  it('applies the same regex consistently across every event, not just the first', () => {
    // A regression where the regex were somehow rebuilt per-event with
    // inconsistent state would show up as an event-order-dependent
    // result -- this checks a larger, mixed set all at once.
    const events = [
      evt({ id: 1, ruleLabel: 'match-1' }),
      evt({ id: 2, ruleLabel: 'skip' }),
      evt({ id: 3, ruleLabel: 'match-2' }),
      evt({ id: 4, ruleLabel: 'skip' }),
      evt({ id: 5, ruleLabel: 'match-3' }),
    ]
    const got = applyFilters(events, { ...emptyFilters(), rule: '^match-', ruleRegex: true })
    expect(got.map((e) => e.id)).toEqual([1, 3, 5])
  })
})

describe('isSafeRulePattern (client-side ReDoS screen)', () => {
  // Every one of these is a working catastrophic-backtracking payload.
  // A single test() of (a+)+$ against a 30-character non-matching
  // string was measured not to finish inside 60 seconds; applyFilters
  // runs the pattern over up to 5,000 events.
  it.each([
    '(a+)+$',
    '(a*)*$',
    '(\\d+)*$',
    '((ab)+){2,}',
    '(a|a)*$',
    '(a|ab)+$',
    '(x+x+)+y',
    '(([a-z])+)+$',
    '(a?){20}a{20}',
  ])('rejects %s', (pattern) => {
    expect(isSafeRulePattern(pattern)).toBe(false)
  })

  it('rejects an over-long pattern', () => {
    expect(isSafeRulePattern('a'.repeat(201))).toBe(false)
  })

  // False positives here cost the operator a working filter, so the
  // shapes people actually type have to survive.
  it.each([
    'drop',
    '^fw-',
    'block|deny',
    'rule[0-9]+',
    '.*ssh.*',
    'in:(ether1|ether2)',
    '^(DROP|REJECT)$',
    'a{2,4}',
    '\\(literal\\)+',
    '[(){}|*+]+',
    '^(input|forward)$',
    'fw-(drop|reject)-[0-9]{1,3}',
    '(?:abc)+',
  ])('allows %s', (pattern) => {
    expect(isSafeRulePattern(pattern)).toBe(true)
  })

  it('ignores the rule filter entirely when the pattern is rejected', () => {
    const events = [
      { id: 1, ruleLabel: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaX', raw: '' },
      { id: 2, ruleLabel: 'something-else', raw: '' },
    ] as unknown as FirewallEvent[]
    const out = applyFilters(events, { rule: '(a+)+$', ruleRegex: true } as Filters)
    expect(out).toHaveLength(2)
  })
})
