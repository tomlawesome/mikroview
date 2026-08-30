// SPDX-License-Identifier: AGPL-3.0-only
//
// policyEdgesFromRules (#628): the aggregation rules the map's Policy
// lens stands on, tested the way boundariesFromRules is -- pure, no DOM.
import { describe, expect, it } from 'vitest'
import type { RouterFilterRule } from './api'
import { policyEdgesFromRules } from './policy.svelte'

function rule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
  return {
    ordinal: 0,
    comment: '',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    log: false,
    ...over,
  }
}

describe('policyEdgesFromRules', () => {
  it('aggregates one edge per pair per direction', () => {
    const edges = policyEdgesFromRules([
      rule({ inInterface: 'bridge1', outInterface: 'ether1' }),
      rule({ ordinal: 1, inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443 }),
      rule({ ordinal: 2, inInterface: 'ether1', outInterface: 'bridge1', action: 'drop' }),
    ])
    expect(edges.map((e) => e.key).sort()).toEqual(['bridge1|ether1', 'ether1|bridge1'])
    const out = edges.find((e) => e.key === 'bridge1|ether1')!
    expect(out.accepted).toBe(true)
    expect(out.refused).toBe(false)
    expect(out.ruleCount).toBe(2)
    const back = edges.find((e) => e.key === 'ether1|bridge1')!
    expect(back.accepted).toBe(false)
    expect(back.refused).toBe(true)
  })

  it('only the forward chain draws -- input/output have no pair', () => {
    const edges = policyEdgesFromRules([
      rule({ chain: 'input', inInterface: 'ether1' }),
      rule({ chain: 'output', outInterface: 'ether1' }),
    ])
    expect(edges).toEqual([])
  })

  it('non-answering actions (jump, log, passthrough) draw nothing', () => {
    const edges = policyEdgesFromRules([
      rule({ action: 'jump', inInterface: 'a', outInterface: 'b' }),
      rule({ action: 'log', inInterface: 'a', outInterface: 'b' }),
      rule({ action: 'passthrough', inInterface: 'a', outInterface: 'b' }),
    ])
    expect(edges).toEqual([])
  })

  it('fasttrack-connection counts as accepting', () => {
    const [e] = policyEdgesFromRules([rule({ action: 'fasttrack-connection', inInterface: 'a', outInterface: 'b' })])
    expect(e.accepted).toBe(true)
  })

  it('port badges keep table order, deduplicate, and split RouterOS lists', () => {
    const [e] = policyEdgesFromRules([
      rule({ inInterface: 'a', outInterface: 'b', dstPort: 443 }),
      rule({ ordinal: 1, inInterface: 'a', outInterface: 'b', dstPort: '80,443' }),
      rule({ ordinal: 2, inInterface: 'a', outInterface: 'b', dstPort: '1000-2000' }),
    ])
    expect(e.acceptPorts).toEqual([':443', ':80', ':1000-2000'])
  })

  it('refused ports are badged separately from accepted ones', () => {
    const [e] = policyEdgesFromRules([
      rule({ inInterface: 'a', outInterface: 'b', dstPort: 443 }),
      rule({ ordinal: 1, inInterface: 'a', outInterface: 'b', action: 'drop', dstPort: 23 }),
    ])
    expect(e.acceptPorts).toEqual([':443'])
    expect(e.refusePorts).toEqual([':23'])
  })

  it('a rule naming no interface keeps "" -- RouterOS any, drawn from the waist', () => {
    const [e] = policyEdgesFromRules([rule({ outInterface: 'ether1', action: 'drop' })])
    expect(e.from).toBe('')
    expect(e.to).toBe('ether1')
  })

  it('the first comment in table order is the epithet, whatever rule order arrived in', () => {
    const [e] = policyEdgesFromRules([
      rule({ ordinal: 5, comment: 'later', inInterface: 'a', outInterface: 'b' }),
      rule({ ordinal: 1, comment: 'first', inInterface: 'a', outInterface: 'b' }),
    ])
    expect(e.comment).toBe('first')
  })

  it('a pair logs if any answering rule logs, or a dedicated log rule names it', () => {
    const edges = policyEdgesFromRules([
      rule({ inInterface: 'a', outInterface: 'b' }),
      rule({ ordinal: 1, inInterface: 'a', outInterface: 'b', action: 'drop', log: true }),
      rule({ ordinal: 2, inInterface: 'c', outInterface: 'd' }),
      rule({ ordinal: 3, inInterface: 'e', outInterface: 'f' }),
      rule({ ordinal: 4, inInterface: 'e', outInterface: 'f', action: 'log' }),
    ])
    expect(edges.find((e) => e.key === 'a|b')!.logged).toBe(true)
    expect(edges.find((e) => e.key === 'c|d')!.logged).toBe(false)
    expect(edges.find((e) => e.key === 'e|f')!.logged).toBe(true)
  })

  it('busiest pair sorts first', () => {
    const edges = policyEdgesFromRules([
      rule({ inInterface: 'a', outInterface: 'b' }),
      rule({ ordinal: 1, inInterface: 'c', outInterface: 'd' }),
      rule({ ordinal: 2, inInterface: 'c', outInterface: 'd', dstPort: 53 }),
    ])
    expect(edges[0].key).toBe('c|d')
  })
})
