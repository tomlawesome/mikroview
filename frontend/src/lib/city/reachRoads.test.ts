import { describe, expect, it } from 'vitest'
import { EMPTY_REACH_LANES, rollUpLanes, type LaneSubject } from './reachRoads'
import type { OffBaseline, OffBaselineLine } from '../baseline'

const line = (over: Partial<OffBaselineLine> = {}): OffBaselineLine => ({
  key: 'k',
  srcIp: '10.0.10.21',
  dstIp: '10.0.20.10',
  port: 5001,
  proto: 'tcp',
  count: 40,
  firstSeenToday: 1_000,
  outcome: 'accept',
  ...over,
})

const off = (lines: OffBaselineLine[]): OffBaseline => ({
  config: { days: 3, of: 14 },
  generatedAt: 0,
  count: lines.length,
  lines,
})

const lane = (roadId: string, ip: string): LaneSubject => ({ roadId, ip })

const TOM = lane('lane:tom-desktop', '10.0.10.21')
const NAS = lane('lane:nas', '10.0.20.10')

describe('rollUpLanes: brightness for the reach’s own lanes (#1016)', () => {
  it('lands a line on the lane of each host it names, and on no other', () => {
    const got = rollUpLanes(off([line({ key: 'a' })]), [TOM, NAS, lane('lane:pihole', '10.0.10.5')])
    expect([...got.keys()].sort()).toEqual(['lane:nas', 'lane:tom-desktop'])
    expect(got.get('lane:tom-desktop')?.lines.map((l) => l.key)).toEqual(['a'])
  })

  it('rings only the end the traffic arrived at: the destination’s lane, never the source’s', () => {
    const got = rollUpLanes(off([line({ key: 'a' })]), [TOM, NAS])
    // tom-desktop spoke, so nothing arrived at its own end of its lane.
    expect(got.get('lane:tom-desktop')?.ring).toEqual({ start: false, end: false })
    // nas was spoken to, so the mark belongs at nas.
    expect(got.get('lane:nas')?.ring).toEqual({ start: true, end: false })
  })

  it('never rings a lane’s far end: that end is the district gate, where nothing arrives', () => {
    const got = rollUpLanes(off([line({ key: 'a' }), line({ key: 'b', srcIp: '10.0.20.10', dstIp: '10.0.10.21' })]), [TOM, NAS])
    for (const e of got.values()) expect(e.ring.end).toBe(false)
  })

  it('rings a host that is both ends of its own line', () => {
    const got = rollUpLanes(off([line({ key: 'a', srcIp: '10.0.10.21', dstIp: '10.0.10.21' })]), [TOM])
    expect(got.get('lane:tom-desktop')?.ring.start).toBe(true)
    // Counted once, not twice, though the host matched both ends.
    expect(got.get('lane:tom-desktop')?.lines).toHaveLength(1)
  })

  it('takes accept ink when anything on the lane was accepted, drop ink when nothing was', () => {
    const accepted = rollUpLanes(off([line({ key: 'a', outcome: 'drop' }), line({ key: 'b', outcome: 'accept' })]), [TOM])
    expect(accepted.get('lane:tom-desktop')?.kind).toBe('a')
    const refusedOnly = rollUpLanes(off([line({ key: 'a', outcome: 'drop' })]), [TOM])
    expect(refusedOnly.get('lane:tom-desktop')?.kind).toBe('d')
  })

  it('orders a lane’s lines busiest first', () => {
    const got = rollUpLanes(off([line({ key: 'quiet', count: 2 }), line({ key: 'busy', count: 99 })]), [NAS])
    expect(got.get('lane:nas')?.lines.map((l) => l.key)).toEqual(['busy', 'quiet'])
  })

  it('is empty when nothing is off the baseline, and when the reach lights no lane', () => {
    expect(rollUpLanes(off([]), [TOM]).size).toBe(0)
    expect(rollUpLanes(off([line()]), []).size).toBe(0)
    expect(EMPTY_REACH_LANES.size).toBe(0)
  })

  it('leaves a lane absent when its host is named by no off-baseline line -- absence is establishment', () => {
    const got = rollUpLanes(off([line({ srcIp: '10.0.99.1', dstIp: '10.0.99.2' })]), [TOM, NAS])
    expect(got.size).toBe(0)
  })
})
