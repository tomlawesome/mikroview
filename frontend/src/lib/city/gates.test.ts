// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { boundaryKeyOf } from '../fall.svelte'
import { gatesFromRules } from './gates'
import type { RouterFilterRule } from '../api'

let ord = 0
function rule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
  return {
    ordinal: ord++,
    comment: '',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    log: false,
    ...over,
  }
}

describe('gatesFromRules', () => {
  it('opens a gate for an accept rule, keyed like the fall boundary', () => {
    const [g] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv' })])
    expect(g.key).toBe(boundaryKeyOf('forward', 'lan', 'srv'))
    expect(g.inInterface).toBe('lan')
    expect(g.outInterface).toBe('srv')
    expect(g.ruleCount).toBe(1)
    expect(g.logged).toBe(false)
  })

  it('a fasttrack rule opens a gate too', () => {
    expect(gatesFromRules([rule({ action: 'fasttrack-connection', inInterface: 'lan', outInterface: 'srv' })])).toHaveLength(1)
  })

  it('a drop or reject rule opens no gate: a gate is an accept crossing, never a refusal', () => {
    expect(gatesFromRules([rule({ action: 'drop', inInterface: 'lan', outInterface: 'guest' })])).toHaveLength(0)
    expect(gatesFromRules([rule({ action: 'reject', inInterface: 'lan', outInterface: 'guest' })])).toHaveLength(0)
  })

  it('input and output chains never open a gate: a district boundary is the forward chain only', () => {
    expect(gatesFromRules([rule({ chain: 'input', inInterface: 'ether1' })])).toHaveLength(0)
    expect(gatesFromRules([rule({ chain: 'output', outInterface: 'ether1' })])).toHaveLength(0)
  })

  it('lamps the gate only when an accept rule on that exact boundary logs', () => {
    const [dark] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv', log: false })])
    expect(dark.logged).toBe(false)
    const [lit] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv', log: true })])
    expect(lit.logged).toBe(true)
  })

  it('the two directions of a boundary are different gates', () => {
    const gates = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv', log: true }), rule({ inInterface: 'srv', outInterface: 'lan', log: false })])
    expect(gates).toHaveLength(2)
    expect(gates.find((g) => g.inInterface === 'lan')?.logged).toBe(true)
    expect(gates.find((g) => g.inInterface === 'srv')?.logged).toBe(false)
  })

  it('several accept rules on one boundary count and keep the first comment', () => {
    const [g] = gatesFromRules([
      rule({ inInterface: 'lan', outInterface: 'srv', comment: 'nas access' }),
      rule({ inInterface: 'lan', outInterface: 'srv', comment: 'second' }),
    ])
    expect(g.ruleCount).toBe(2)
    expect(g.comment).toBe('nas access')
  })

  it('no rules at all opens no gates', () => {
    expect(gatesFromRules([])).toEqual([])
  })
})
