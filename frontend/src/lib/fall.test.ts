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
})
