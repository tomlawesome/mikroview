// SPDX-License-Identifier: AGPL-3.0-only
//
// realityEdges / unexercisedIntents (#629): how observed traffic is
// judged against intent -- pure, no DOM, like policy.test.ts.
import { describe, expect, it } from 'vitest'
import type { FirewallEvent } from './types'
import type { PolicyEdge } from './policy.svelte'
import { realityEdges, unexercisedIntents } from './reality'

let id = 0
function ev(over: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: ++id,
    timestamp: '2026-08-30T12:00:00Z',
    deviceId: 'r1',
    action: 'accept',
    chain: 'forward',
    srcIp: '192.168.1.10',
    dstIp: '203.0.113.5',
    inInterface: 'bridge1',
    outInterface: 'ether1',
    raw: '',
    ...over,
  } as FirewallEvent
}

function intent(over: Partial<PolicyEdge> = {}): PolicyEdge {
  return {
    key: 'bridge1|ether1',
    from: 'bridge1',
    to: 'ether1',
    accepted: true,
    refused: false,
    acceptPorts: [],
    refusePorts: [],
    comment: '',
    ruleCount: 1,
    logged: false,
    ...over,
  }
}

describe('realityEdges', () => {
  it('an anticipated flow is planned, with its ports ranked by volume', () => {
    const [r] = realityEdges(
      [ev({ dstPort: 443 }), ev({ dstPort: 443 }), ev({ dstPort: 53 })],
      [intent()],
      true,
    )
    expect(r.verdict).toBe('planned')
    expect(r.topPorts).toEqual([':443', ':53'])
    expect(r.events).toBe(3)
  })

  it('drops on a refusing pair mean the policy is holding', () => {
    const [r] = realityEdges(
      [ev({ action: 'drop', inInterface: 'ether1', outInterface: 'bridge1' })],
      [intent({ key: 'ether1|bridge1', from: 'ether1', to: 'bridge1', accepted: false, refused: true })],
      true,
    )
    expect(r.verdict).toBe('holding')
  })

  it('accepted traffic where the table only refuses is unplanned', () => {
    const [r] = realityEdges(
      [ev({ inInterface: 'ether1', outInterface: 'bridge1' })],
      [intent({ key: 'ether1|bridge1', from: 'ether1', to: 'bridge1', accepted: false, refused: true })],
      true,
    )
    expect(r.verdict).toBe('unplanned')
  })

  it('a pair no rule anticipated is unplanned', () => {
    const [r] = realityEdges([ev({ inInterface: 'bridge1', outInterface: 'wg0' })], [intent()], true)
    expect(r.verdict).toBe('unplanned')
  })

  it('with no rules pushed nothing is judged, in either direction', () => {
    const [r] = realityEdges([ev()], [], false)
    expect(r.verdict).toBe('unjudged')
  })

  it('events without both boundaries draw nothing', () => {
    expect(realityEdges([ev({ outInterface: undefined })], [], true)).toEqual([])
  })

  it('busiest pair sorts first, drops counted apart from accepts', () => {
    const rs = realityEdges(
      [
        ev({ inInterface: 'a', outInterface: 'b' }),
        ev({ inInterface: 'c', outInterface: 'd', action: 'drop' }),
        ev({ inInterface: 'c', outInterface: 'd', action: 'drop' }),
      ],
      [],
      true,
    )
    expect(rs[0].key).toBe('c|d')
    expect(rs[0].drops).toBe(2)
    expect(rs[0].accepts).toBe(0)
  })
})

describe('unexercisedIntents', () => {
  it('an accepting rule nothing exercised is the delta; a silent refusal is not', () => {
    const observed = realityEdges([ev()], [intent()], true)
    const intents = [
      intent(),
      intent({ key: 'wg0|bridge1', from: 'wg0', to: 'bridge1' }),
      intent({ key: 'bridge1|wg0', from: 'bridge1', to: 'wg0', accepted: false, refused: true }),
    ]
    expect(unexercisedIntents(observed, intents).map((i) => i.key)).toEqual(['wg0|bridge1'])
  })
})
