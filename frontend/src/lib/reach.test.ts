// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import { RECENCY_HALF_LIFE_MS, portsLine, reachFor } from './reach'
import type { ClientEvent } from './types'

const NOW = 1_780_000_000_000
const HOST = '192.168.1.50'

function event(over: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-09-03T12:00:00Z',
    receivedAt: NOW,
    deviceId: 'router1',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'r',
    chain: 'forward',
    raw: '',
    srcIp: HOST,
    dstIp: '10.0.9.9',
    outInterface: 'ether1',
    ...over,
  }
}

/** n events on one pathway, all `agoMs` old. */
function burst(n: number, agoMs: number, over: Partial<ClientEvent> = {}): ClientEvent[] {
  return Array.from({ length: n }, (_, i) => event({ id: i + 1, receivedAt: NOW - agoMs, ...over }))
}

describe('the busiest pathway is weighted toward now (#701)', () => {
  it('prefers a small burst happening now over a bigger one that has gone quiet', () => {
    // The reading the owner chose this weighting for: "busiest right
    // now" should not keep naming a pathway that stopped an hour ago
    // merely because it was noisy while it lasted.
    const events = [
      ...burst(50, 60 * 60 * 1000, { outInterface: 'ether1' }),
      ...burst(5, 0, { outInterface: 'bridge2' }),
    ]

    const { busiest } = reachFor(HOST, null, events, NOW)
    expect(busiest?.counterpart).toBe('bridge2')
  })

  it('still prefers the bigger burst when both are equally recent', () => {
    const events = [...burst(50, 0, { outInterface: 'ether1' }), ...burst(5, 0, { outInterface: 'bridge2' })]

    const { busiest } = reachFor(HOST, null, events, NOW)
    expect(busiest?.counterpart).toBe('ether1')
  })

  it('halves an event\'s contribution every half-life', () => {
    const now = reachFor(HOST, null, burst(1, 0), NOW).strands[0]
    const oneHalfLife = reachFor(HOST, null, burst(1, RECENCY_HALF_LIFE_MS), NOW).strands[0]
    const twoHalfLives = reachFor(HOST, null, burst(1, 2 * RECENCY_HALF_LIFE_MS), NOW).strands[0]

    expect(now.weight).toBeCloseTo(1, 6)
    expect(oneHalfLife.weight).toBeCloseTo(0.5, 6)
    expect(twoHalfLives.weight).toBeCloseTo(0.25, 6)
    // The lifetime count is untouched by any of it -- the two answer
    // different questions and both are kept.
    expect([now.count, oneHalfLife.count, twoHalfLives.count]).toEqual([1, 1, 1])
  })

  it('does not let an event stamped in the future outrank everything', () => {
    // A clock that moved, or a replayed buffer. Clamped to full weight
    // rather than allowed to grow without bound.
    const events = [...burst(1, -24 * 60 * 60 * 1000, { outInterface: 'ether1' }), ...burst(3, 0, { outInterface: 'bridge2' })]

    const { busiest, strands } = reachFor(HOST, null, events, NOW)
    expect(strands.find((s) => s.counterpart === 'ether1')?.weight).toBeCloseTo(1, 6)
    expect(busiest?.counterpart).toBe('bridge2')
  })

  it('names no pathway at all when nothing was observed', () => {
    // The standing rule: an absence of our own is never reported as a
    // fact about the network.
    const { busiest, strands } = reachFor(HOST, null, [], NOW)
    expect(strands).toEqual([])
    expect(busiest).toBeNull()
  })

  it('leaves the drawn strand order ranked by lifetime count, not by weight', () => {
    // The strands the map draws, and the crumb's own alarm line, read
    // this order. Re-sorting it for the sentence would have changed both
    // without anything saying so.
    const events = [
      ...burst(50, 60 * 60 * 1000, { outInterface: 'ether1' }),
      ...burst(5, 0, { outInterface: 'bridge2' }),
    ]

    const { strands, busiest } = reachFor(HOST, null, events, NOW)
    expect(strands[0].counterpart).toBe('ether1')
    expect(busiest?.counterpart).toBe('bridge2')
  })
})

describe('portsLine (#868: shared with the city so neither view invents its own wording)', () => {
  it('joins up to three ports with a leading colon', () => {
    expect(portsLine([445, 22, 80, 9999])).toBe(':445 :22 :80')
  })

  it('is empty for no ports', () => {
    expect(portsLine([])).toBe('')
  })
})
