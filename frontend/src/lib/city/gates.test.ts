// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { boundaryKeyOf } from '../fall.svelte'
import { betterCoverage, gatesFromRules, worseCoverage } from './gates'
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

  it('several accept rules on one boundary count, and the lowest-numbered one names the gate', () => {
    // The card prints the two together -- `rule 4 · nas access` (owner,
    // 2026-09-08 on #1016) -- so the number and the name must come from
    // the same line of the table, never a name borrowed from another
    // rule. Numbered as RouterOS numbers them.
    const [g] = gatesFromRules([
      rule({ ordinal: 4, inInterface: 'lan', outInterface: 'srv', comment: 'nas access' }),
      rule({ ordinal: 7, inInterface: 'lan', outInterface: 'srv', comment: 'second' }),
    ])
    expect(g.ruleCount).toBe(2)
    expect(g.ordinal).toBe(4)
    expect(g.comment).toBe('nas access')
  })

  it('takes the lowest rule number whatever order the rules arrive in', () => {
    const [g] = gatesFromRules([
      rule({ ordinal: 7, inInterface: 'lan', outInterface: 'srv', comment: 'second' }),
      rule({ ordinal: 4, inInterface: 'lan', outInterface: 'srv', comment: 'nas access' }),
    ])
    expect(g.ordinal).toBe(4)
    expect(g.comment).toBe('nas access')
  })

  it('leaves a gate whose rule carries no comment unnamed rather than inventing one', () => {
    const [g] = gatesFromRules([
      rule({ ordinal: 4, inInterface: 'lan', outInterface: 'srv', comment: '' }),
      rule({ ordinal: 7, inInterface: 'lan', outInterface: 'srv', comment: 'second' }),
    ])
    expect(g.ordinal).toBe(4)
    expect(g.comment).toBe('')
  })

  it('no rules at all opens no gates', () => {
    expect(gatesFromRules([])).toEqual([])
  })
})

// Round 49 (#1016): a gate is drawn in its boundary's own state, and
// the reading comes from the one rule (lib/coverageRule.ts) applied to
// the pushed boundary-direction -- not from the gate's accept rules
// alone, which say nothing about a drop rule that logs.
describe('gatesFromRules: the coverage reading (round 49)', () => {
  const edge = (from: string, to: string, logged: boolean) => ({
    key: `${from}|${to}`,
    from,
    to,
    logged,
    accepted: true,
    refused: false,
    acceptPorts: [],
    refusePorts: [],
    comment: '',
    ruleCount: 1,
  })

  it('reads each direction from its own pushed boundary, and names both keys', () => {
    const [g] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv' })], [edge('lan', 'srv', true), edge('srv', 'lan', false)])
    expect(g.edgeKey).toBe('lan|srv')
    expect(g.reverseEdgeKey).toBe('srv|lan')
    expect(g.coverage).toBe('logged')
    expect(g.reverseCoverage).toBe('dark')
  })

  it('reads a declared direction as quiet on purpose, never dark', () => {
    const [g] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv' })], [edge('lan', 'srv', false), edge('srv', 'lan', false)], new Set(['lan|srv']))
    expect(g.coverage).toBe('quiet')
    expect(g.reverseCoverage).toBe('dark')
  })

  it('falls back to the gate’s own accept rules when no pushed edge names the direction', () => {
    const [g] = gatesFromRules([rule({ inInterface: 'lan', outInterface: 'srv', log: true })])
    expect(g.coverage).toBe('logged')
    // Nothing is claimed about the way back from the forward rules.
    expect(g.reverseCoverage).toBe('dark')
  })
})

describe('worseCoverage / betterCoverage', () => {
  it('ranks dark worse than quiet worse than logged', () => {
    expect(worseCoverage('logged', 'quiet')).toBe('quiet')
    expect(worseCoverage('quiet', 'dark')).toBe('dark')
    expect(worseCoverage('logged', 'logged')).toBe('logged')
    expect(betterCoverage('dark', 'quiet')).toBe('quiet')
    expect(betterCoverage('quiet', 'logged')).toBe('logged')
  })
})
