// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  EMPTY_OFF_BASELINE,
  isOffBaseline,
  offBaselineMatching,
  type OffBaseline,
  type OffBaselineLine,
} from './baseline'

function line(over: Partial<OffBaselineLine> = {}): OffBaselineLine {
  return {
    key: '10.0.10.5|10.0.0.1|443|tcp',
    srcIp: '10.0.10.5',
    dstIp: '10.0.0.1',
    port: 443,
    proto: 'tcp',
    count: 1,
    firstSeenToday: 1_757_000_000_000,
    outcome: 'accept',
    ...over,
  }
}

function doc(lines: OffBaselineLine[]): OffBaseline {
  return { config: { days: 3, of: 14 }, generatedAt: 1_757_000_000_000, count: lines.length, lines }
}

describe('offBaselineMatching', () => {
  it('returns only the lines the predicate accepts', () => {
    const wanted = line({ key: 'a', dstIp: '203.0.113.9' })
    const off = doc([line({ key: 'b' }), wanted, line({ key: 'c' })])

    expect(offBaselineMatching(off, (l) => l.dstIp === '203.0.113.9')).toEqual([wanted])
  })

  it('answers with an empty array rather than null when nothing matches', () => {
    // The caller renders a list from this, so "nothing" has to be
    // iterable rather than something it must null-check first.
    expect(offBaselineMatching(doc([line()]), () => false)).toEqual([])
  })

  it('does not mutate the document it was given', () => {
    const off = doc([line({ key: 'a' }), line({ key: 'b' })])
    const before = [...off.lines]

    offBaselineMatching(off, (l) => l.key === 'a')

    expect(off.lines).toEqual(before)
  })

  it('can roll a host dot up by either end of the line', () => {
    // The predicate is the caller's because only the caller knows what
    // its element is -- a lane belongs to the host at one end, and which
    // end depends on the direction being drawn.
    const off = doc([
      line({ key: 'out', srcIp: '10.0.10.5', dstIp: '203.0.113.9' }),
      line({ key: 'in', srcIp: '10.0.10.9', dstIp: '10.0.10.5' }),
    ])

    const touching = offBaselineMatching(
      off,
      (l) => l.srcIp === '10.0.10.5' || l.dstIp === '10.0.10.5',
    )

    expect(touching.map((l) => l.key)).toEqual(['out', 'in'])
  })

  it('carries a null port through rather than turning it into a number', () => {
    // 0 is not a port, and a caller formatting `:0` into a label would be
    // stating something untrue.
    const icmp = line({ key: 'icmp', port: null, proto: 'icmp' })

    expect(offBaselineMatching(doc([icmp]), (l) => l.port === null)).toEqual([icmp])
  })
})

describe('isOffBaseline', () => {
  it('is true when any one line matches', () => {
    // "One off-baseline line among a thousand lights the one place it
    // happened" -- the roll-up is an any, not an every.
    const off = doc([line({ key: 'a' }), line({ key: 'b', dstIp: '203.0.113.9' })])

    expect(isOffBaseline(off, (l) => l.dstIp === '203.0.113.9')).toBe(true)
  })

  it('is false when no line matches', () => {
    expect(isOffBaseline(doc([line()]), (l) => l.dstIp === '198.51.100.1')).toBe(false)
  })

  it('is false on an empty document', () => {
    // The state before the first fetch lands, and after a failed one:
    // nothing off-baseline reads as nothing unusual.
    expect(isOffBaseline(EMPTY_OFF_BASELINE, () => true)).toBe(false)
  })

  it('agrees with offBaselineMatching', () => {
    const off = doc([line({ key: 'a', outcome: 'drop' }), line({ key: 'b' })])
    const pred = (l: OffBaselineLine) => l.outcome === 'drop'

    expect(isOffBaseline(off, pred)).toBe(offBaselineMatching(off, pred).length > 0)
  })
})

describe('EMPTY_OFF_BASELINE', () => {
  it('carries the ratified 3-of-14 threshold and no lines', () => {
    expect(EMPTY_OFF_BASELINE.config).toEqual({ days: 3, of: 14 })
    expect(EMPTY_OFF_BASELINE.count).toBe(0)
    expect(EMPTY_OFF_BASELINE.lines).toEqual([])
  })
})
