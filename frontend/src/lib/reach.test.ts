// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import { RECENCY_HALF_LIFE_MS, portsLine, reachFor, reachLineSummary } from './reach'
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

describe('refusedBy names the latest rule, not the first (#967)', () => {
  it('a rule table edit does not leave a stale rule name behind', () => {
    // "old-drop" refused this pair once; a later table push replaced it
    // with "new-drop". Events arrive oldest first (appState.events
    // appends), so the strand must carry the most recent table's name,
    // never the first one it happened to see.
    const events = [
      event({ id: 1, action: 'drop', ruleLabel: 'old-drop', outInterface: 'ether1' }),
      event({ id: 2, action: 'drop', ruleLabel: 'new-drop', outInterface: 'ether1' }),
    ]

    const { strands } = reachFor(HOST, null, events, NOW)
    expect(strands[0].refusedBy).toBe('new-drop')
  })

  it('reverts to unnamed when the newest drop carries no label', () => {
    const events = [
      event({ id: 1, action: 'drop', ruleLabel: 'old-drop', outInterface: 'ether1' }),
      event({ id: 2, action: 'drop', ruleLabel: '', outInterface: 'ether1' }),
    ]

    const { strands } = reachFor(HOST, null, events, NOW)
    expect(strands[0].refusedBy).toBeUndefined()
  })
})

describe('reachLineSummary: one hovered line, not one strand (#1016)', () => {
  /** The strands a line is drawn from, for a host talking over ether1. */
  const line = (events: ClientEvent[]) => reachLineSummary(reachFor(HOST, null, events, NOW).strands, 'ether1')

  it('merges the accepted and dropped strands of a line into one port list', () => {
    // A drawn line is one counterpart pair, but reachFor splits it by
    // outcome. The card asks about the line, so :443 is one row that was
    // both let through and refused -- not two rows that look unrelated.
    const events = [
      ...burst(3, 0, { protocol: 'tcp', dstPort: 443 }),
      ...burst(2, 0, { action: 'drop', protocol: 'tcp', dstPort: 443 }),
      ...burst(1, 0, { action: 'drop', protocol: 'tcp', dstPort: 22 }),
    ]

    const s = line(events)
    expect(s.ports).toEqual([
      { port: 443, proto: 'tcp', accepted: 3, dropped: 2 },
      { port: 22, proto: 'tcp', accepted: 0, dropped: 1 },
    ])
    expect([s.accepted, s.dropped]).toEqual([3, 3])
  })

  it('splits the line by protocol, counting portless traffic too', () => {
    // "TCP versus UDP on this line" has to include the ICMP that carries
    // no destination port, or a pinged line reads as no traffic at all.
    const events = [
      ...burst(2, 0, { protocol: 'tcp', dstPort: 443 }),
      ...burst(3, 0, { protocol: 'udp', dstPort: 53 }),
      ...burst(1, 0, { action: 'drop', protocol: 'icmp' }),
    ]

    const s = line(events)
    expect([s.tcp, s.udp, s.other]).toEqual([2, 3, 1])
    // Every event on the line lands in exactly one protocol bucket and
    // exactly one outcome, so the two splits agree.
    expect(s.tcp + s.udp + s.other).toBe(s.accepted + s.dropped)
    // Portless traffic still cannot appear as a port.
    expect(s.ports.map((p) => p.port)).toEqual([53, 443])
  })

  it('keeps the same port on separate rows per protocol', () => {
    const events = [...burst(4, 0, { protocol: 'udp', dstPort: 53 }), ...burst(1, 0, { protocol: 'tcp', dstPort: 53 })]

    expect(line(events).ports).toEqual([
      { port: 53, proto: 'udp', accepted: 4, dropped: 0 },
      { port: 53, proto: 'tcp', accepted: 1, dropped: 0 },
    ])
  })

  it('ranks ports busiest first, breaking ties by port number', () => {
    const events = [
      ...burst(2, 0, { protocol: 'tcp', dstPort: 443 }),
      ...burst(2, 0, { protocol: 'tcp', dstPort: 80 }),
      ...burst(5, 0, { action: 'drop', protocol: 'tcp', dstPort: 22 }),
    ]

    expect(line(events).ports.map((p) => p.port)).toEqual([22, 80, 443])
  })

  it('merges both directions of the same counterpart', () => {
    // The line is drawn once for the pair. Which way the packets went is
    // the arrow's job, not the port list's.
    const events = [
      ...burst(2, 0, { protocol: 'tcp', dstPort: 443 }),
      ...burst(3, 0, { srcIp: '10.0.9.9', dstIp: HOST, outInterface: undefined, inInterface: 'ether1', protocol: 'tcp', dstPort: 443 }),
    ]

    const s = line(events)
    expect(s.ports).toEqual([{ port: 443, proto: 'tcp', accepted: 5, dropped: 0 }])
    expect(s.accepted).toBe(5)
  })

  it('carries the refusing rule from the line\'s dropped strand', () => {
    const events = [
      ...burst(3, 0, { protocol: 'tcp', dstPort: 443 }),
      ...burst(1, 0, { action: 'drop', ruleLabel: 'block-scan', protocol: 'tcp', dstPort: 22 }),
    ]

    expect(line(events).refusedBy).toBe('block-scan')
  })

  it('names no rule for a line that was never refused', () => {
    expect(line(burst(3, 0, { protocol: 'tcp', dstPort: 443 })).refusedBy).toBeUndefined()
  })

  it('reports zeros for a counterpart the buffer never saw', () => {
    // Same standing rule as `busiest`: an absence of ours is not
    // reported as a fact about the network, it is just empty.
    const { strands } = reachFor(HOST, null, burst(3, 0, { protocol: 'tcp', dstPort: 443 }), NOW)

    expect(reachLineSummary(strands, 'bridge9')).toEqual({
      counterpart: 'bridge9',
      ports: [],
      tcp: 0,
      udp: 0,
      other: 0,
      accepted: 0,
      dropped: 0,
      refusedBy: undefined,
    })
  })

  it('does not report an unnamed protocol as TCP', () => {
    // portHits falls back to 'tcp' for display. Doing that here would
    // invent the very fact the tcp/udp split is asked for.
    const s = line(burst(2, 0, { dstPort: 443 }))

    expect([s.tcp, s.udp, s.other]).toEqual([0, 0, 2])
    expect(s.ports).toEqual([{ port: 443, proto: '', accepted: 2, dropped: 0 }])
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
