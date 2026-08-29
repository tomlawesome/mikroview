// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { boundariesFromRules, boundaryKeyOf } from './fall.svelte'
import type { RouterFilterRule } from './api'

function rule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
  return {
    ordinal: 0,
    comment: '',
    chain: 'forward',
    action: 'drop',
    srcAddressList: '',
    logPrefix: '',
    log: false,
    ...over,
  }
}

describe('boundaryKeyOf', () => {
  it('treats an absent interface as the empty string, not undefined', () => {
    expect(boundaryKeyOf('forward', undefined, 'bridge1')).toBe('forward||bridge1')
    expect(boundaryKeyOf('forward', '', 'bridge1')).toBe('forward||bridge1')
  })
})

describe('boundariesFromRules', () => {
  it('reports unknown, not dark, when no device has pushed anything', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1' })], false)
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('unknown')
  })

  it('is dark only when rules were pushed, cover this boundary, and none of them log', () => {
    const bands = boundariesFromRules(
      [rule({ inInterface: 'ether9', outInterface: 'bridge9', log: false })],
      true,
    )
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('dark')
  })

  it('is observed as soon as one matching rule logs, even if others on the same boundary do not', () => {
    const bands = boundariesFromRules(
      [
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: false, comment: 'silent variant' }),
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true, comment: 'logging variant' }),
      ],
      true,
    )
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('observed')
  })

  it('never claims dark for a boundary no pushed rule actually references', () => {
    // Only ether1<->bridge1 was pushed; nothing says anything about
    // ether9<->bridge9 -- boundariesFromRules should not fabricate an
    // entry for it at all (the honesty rule: only ever a definite
    // answer, and only about a boundary it has actual evidence for).
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true })], true)
    expect(bands.map((b) => b.key)).not.toContain(boundaryKeyOf('forward', 'ether9', 'bridge9'))
  })

  it('labels a boundary with both interfaces as "in -> out"', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1' })], true)
    expect(bands[0].label).toBe('ether1 → bridge1')
  })

  it('sorts alphabetically by label, deterministic across calls', () => {
    const bands = boundariesFromRules(
      [
        rule({ inInterface: 'wan1', outInterface: 'bridge1', log: true }),
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['ether1 → bridge1', 'wan1 → bridge1'])
  })

  it('groups distinct chains for the same interface pair as distinct boundaries', () => {
    const bands = boundariesFromRules(
      [
        rule({ chain: 'forward', inInterface: 'ether1', outInterface: 'bridge1', log: true }),
        rule({ chain: 'input', inInterface: 'ether1', outInterface: '', log: false }),
      ],
      true,
    )
    expect(bands).toHaveLength(2)
  })

  // Amendment (a), Fable's 2026-08-29 review: a pushed rule's own
  // srcAddressList is real evidence of an operator-named group, and
  // replaces the interface name on that side of the label.
  it('names the source side from srcAddressList when a pushed rule carries one', () => {
    const bands = boundariesFromRules(
      [rule({ inInterface: 'ether1', outInterface: 'bridge9', srcAddressList: 'lan', log: true })],
      true,
    )
    expect(bands[0].label).toBe('lan → bridge9')
    expect(bands[0].srcAddressList).toBe('lan')
    // The raw interface is kept alongside, not discarded.
    expect(bands[0].inInterface).toBe('ether1')
  })

  it('falls back to the interface name when no rule on this boundary names an address list', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge9' })], true)
    expect(bands[0].label).toBe('ether1 → bridge9')
    expect(bands[0].srcAddressList).toBe('')
  })

  // Amendment (b): semantic ordering -- input-chain/WAN-facing first,
  // observed forward next, dark/unknown last, alphabetical within class.
  it('orders input-chain bands before observed forward bands, and dark bands last', () => {
    const bands = boundariesFromRules(
      [
        // forward, dark (no log) -- last class.
        rule({ chain: 'forward', inInterface: 'z-dark', outInterface: 'bridge1', log: false }),
        // forward, observed -- middle class.
        rule({ chain: 'forward', inInterface: 'a-observed', outInterface: 'bridge1', log: true }),
        // input chain -- first class, regardless of its own coverage.
        rule({ chain: 'input', inInterface: 'zzz-wan', outInterface: '', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['zzz-wan · input', 'a-observed → bridge1', 'z-dark → bridge1'])
  })

  it('keeps alphabetical order within a class', () => {
    const bands = boundariesFromRules(
      [
        rule({ chain: 'input', inInterface: 'z-wan', outInterface: '', log: true }),
        rule({ chain: 'input', inInterface: 'a-wan', outInterface: '', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['a-wan · input', 'z-wan · input'])
  })
})
