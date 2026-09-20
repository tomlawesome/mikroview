import { describe, expect, it } from 'vitest'
import { addressName, endName, portWords, roadEnds, rollUpRoads, verdictWords, zoneIndex, zoneOfAddress } from './baselineRoads'
import type { OffBaseline, OffBaselineLine } from '../baseline'
import type { Building, District, Ground, Road } from './types'

const host = (id: string, name: string, ip: string, districtId: string | null): Building => ({
  id,
  name,
  ip,
  kind: 'host',
  u: 0,
  v: 0,
  R: 2,
  h: 1,
  districtId,
  routerId: 'rb',
  index: 0,
})

const plate = (id: string, u: number, v: number, cidr: string | null, buildings: Building[]): District => ({
  id,
  name: id.toUpperCase(),
  cidr,
  u,
  v,
  r: 10,
  ink: 0,
  routerId: 'rb',
  coverage: 'logged',
  dark: false,
  plateDark: false,
  buildings,
  more: 0,
  gates: [],
  rulesPushed: true,
})

/** A road drawn from `a`'s wall to `b`'s wall, keyed the way layout.ts
 *  keys it: the sorted pair. */
const road = (a: string, b: string, from: [number, number], to: [number, number]): Road => ({
  id: [a, b].sort().join('|'),
  pts: [from, [(from[0] + to[0]) / 2, (from[1] + to[1]) / 2], to],
  w: 2,
  k: 'a',
  from: null,
  to: null,
  label: `${a} to ${b}`,
})

// lan sits at the origin, srv well to the east; the road between them
// runs lan-wall -> srv-wall, so pts[0] is lan's end.
const lanDesk = host('b-desk', 'tom-desktop', '192.168.1.50', 'lan')
const srvNas = host('b-nas', 'nas', '10.0.0.9', 'srv')

function ground(overrides: Partial<Ground> = {}): Ground {
  const lan = plate('lan', 0, 0, '192.168.1.0/24', [lanDesk])
  const srv = plate('srv', 100, 0, '10.0.0.0/24', [srvNas])
  return {
    districts: [lan, srv],
    nodes: [host('n-ether1', 'ether1', 'wan bridge', null)],
    boroughs: [],
    roads: [road('lan', 'srv', [10, 0], [90, 0]), road('lan', 'wan', [-10, 0], [-60, -30])],
    river: null,
    bridges: [],
    bounds: { u0: -60, u1: 100, v0: -30, v1: 10 },
    townBounds: { u0: -60, u1: 100, v0: -30, v1: 10 },
    ...overrides,
  }
}

const line = (o: Partial<OffBaselineLine> = {}): OffBaselineLine => ({
  key: 'k',
  srcIp: '192.168.1.50',
  dstIp: '10.0.0.9',
  port: 5001,
  proto: 'tcp',
  count: 40,
  firstSeenToday: 1_700_000_000_000,
  outcome: 'accept',
  ...o,
})

const doc = (lines: OffBaselineLine[]): OffBaseline => ({
  config: { days: 3, of: 14 },
  generatedAt: 1,
  count: lines.length,
  lines,
})

describe('zoneOfAddress', () => {
  const ix = () => zoneIndex(ground(), 'wan')

  it('resolves a drawn building by its exact address', () => {
    expect(zoneOfAddress(ix(), '192.168.1.50')).toBe('lan')
  })

  it('resolves a host the bounded plate never drew, by its district CIDR', () => {
    // District.more says such hosts exist; they still belong to lan.
    expect(zoneOfAddress(ix(), '192.168.1.77')).toBe('lan')
  })

  it('treats an address in no district as the WAN', () => {
    expect(zoneOfAddress(ix(), '203.0.113.9')).toBe('wan')
  })

  it('says nothing at all when there is no WAN to fall back on', () => {
    expect(zoneOfAddress(zoneIndex(ground(), null), '203.0.113.9')).toBeNull()
  })

  it('prefers the more specific district when two CIDRs overlap', () => {
    const g = ground({
      districts: [plate('wide', 0, 0, '10.0.0.0/8', []), plate('narrow', 50, 0, '10.0.0.0/24', [])],
    })
    expect(zoneOfAddress(zoneIndex(g, 'wan'), '10.0.0.5')).toBe('narrow')
    expect(zoneOfAddress(zoneIndex(g, 'wan'), '10.9.9.9')).toBe('wide')
  })
})

describe('roadEnds', () => {
  it('orients a district-to-district road by which end each plate is nearer', () => {
    const g = ground()
    expect(roadEnds(g.roads[0], g.districts)).toEqual({ start: 'lan', end: 'srv' })
  })

  it('orients a road whose far end is not a district at all', () => {
    const g = ground()
    expect(roadEnds(g.roads[1], g.districts)).toEqual({ start: 'lan', end: 'wan' })
  })

  it('declines a lane, which is not a pair key', () => {
    const lane: Road = { ...road('lan', 'srv', [0, 0], [5, 0]), id: 'lane:b-desk', lane: true }
    expect(roadEnds(lane, ground().districts)).toBeNull()
  })
})

describe('rollUpRoads: the roll-up', () => {
  it('lights the one road an off-baseline line ran along', () => {
    const g = ground()
    const rb = rollUpRoads(g, doc([line()]), 'wan')
    expect([...rb.keys()]).toEqual(['lan|srv'])
    expect(rb.get('lan|srv')?.lines).toHaveLength(1)
  })

  it('draws a road bright on one off-baseline line among a thousand established ones', () => {
    // The register only ever carries today's off-baseline lines, so the
    // thousand established ones are represented by their absence -- which
    // is exactly the claim: the road is lit by the one that is present.
    const g = ground()
    const rb = rollUpRoads(g, doc([line({ key: 'the-one' })]), 'wan')
    expect(rb.has('lan|srv')).toBe(true)
    // And the road that carried none of them stays dark.
    expect(rb.has('lan|wan')).toBe(false)
  })

  it('does not grow the number of lit roads with traffic', () => {
    // Two hundred distinct lines across the same district pair light
    // exactly one road: one road per district pair, never one per flow.
    const g = ground()
    const many = Array.from({ length: 200 }, (_, i) =>
      line({ key: `k${i}`, port: 5000 + i, srcIp: `192.168.1.${i % 200}`, dstIp: '10.0.0.9' }),
    )
    const rb = rollUpRoads(g, doc(many), 'wan')
    expect(rb.size).toBe(1)
    expect(rb.get('lan|srv')?.lines).toHaveLength(200)
  })

  it('rings the end the traffic arrived at, not the end it left', () => {
    const g = ground()
    const rb = rollUpRoads(g, doc([line()]), 'wan')
    // lan -> srv, and srv is the far end of the drawn road.
    expect(rb.get('lan|srv')?.ring).toEqual({ start: false, end: true })
  })

  it('rings the near end when the traffic ran the other way', () => {
    const g = ground()
    const rb = rollUpRoads(g, doc([line({ srcIp: '10.0.0.9', dstIp: '192.168.1.50' })]), 'wan')
    expect(rb.get('lan|srv')?.ring).toEqual({ start: true, end: false })
  })

  it('rings both ends when one folded road carried a new line each way', () => {
    const g = ground()
    const rb = rollUpRoads(
      g,
      doc([line({ key: 'out' }), line({ key: 'back', srcIp: '10.0.0.9', dstIp: '192.168.1.50' })]),
      'wan',
    )
    expect(rb.get('lan|srv')?.ring).toEqual({ start: true, end: true })
  })

  it('lights the WAN road for traffic that arrived from outside', () => {
    const g = ground()
    const rb = rollUpRoads(g, doc([line({ srcIp: '203.0.113.9', dstIp: '192.168.1.50' })]), 'wan')
    expect(rb.get('lan|wan')?.ring).toEqual({ start: true, end: false })
  })

  it('ignores a line that never crossed a boundary', () => {
    const g = ground()
    expect(rollUpRoads(g, doc([line({ dstIp: '192.168.1.51' })]), 'wan').size).toBe(0)
  })

  it('lights nothing where the ground drew no road', () => {
    // A boundary nothing logs draws no road; the line cannot be shown on
    // a road that is not there, and nothing is invented.
    const g = ground({ roads: [] })
    expect(rollUpRoads(g, doc([line()]), 'wan').size).toBe(0)
  })

  it('never lights a lane', () => {
    const lane: Road = { ...road('lan', 'srv', [0, 0], [5, 0]), id: 'lan|srv', lane: true }
    const g = ground({ roads: [lane] })
    expect(rollUpRoads(g, doc([line()]), 'wan').size).toBe(0)
  })

  it('is empty when nothing is off the baseline', () => {
    expect(rollUpRoads(ground(), doc([]), 'wan').size).toBe(0)
  })

  it('orders a road’s lines busiest first', () => {
    const g = ground()
    const rb = rollUpRoads(g, doc([line({ key: 'a', count: 2 }), line({ key: 'b', count: 90 })]), 'wan')
    expect(rb.get('lan|srv')?.lines.map((l) => l.key)).toEqual(['b', 'a'])
  })
})

describe('the card’s wording', () => {
  it('says an accepted line in plain words, and never names a rule it was not given', () => {
    expect(verdictWords('accept')).toBe('the router accepted it; nothing decided it was wanted')
    expect(verdictWords('accept')).not.toContain('#')
  })

  it('says a dropped line in plain words', () => {
    expect(verdictWords('drop')).toContain('dropped it')
  })

  it('names a line’s ends by the buildings drawn at them', () => {
    expect(addressName(ground(), '192.168.1.50')).toBe('tom-desktop')
    expect(addressName(ground(), '10.0.0.9')).toBe('nas')
  })

  it('falls back to the address when nothing is drawn there', () => {
    expect(addressName(ground(), '203.0.113.9')).toBe('203.0.113.9')
  })

  it('names a road’s ends by their districts, and a node end by the node', () => {
    expect(endName(ground(), 'lan')).toBe('LAN')
    expect(endName(ground(), 'ether1')).toBe('ether1')
  })

  it('writes the port and protocol together, and the protocol alone when there was no port', () => {
    expect(portWords(line())).toBe('5001/tcp')
    expect(portWords(line({ port: null, proto: 'icmp' }))).toBe('icmp')
  })
})
