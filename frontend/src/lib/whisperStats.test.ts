// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  bucketAt,
  bucketTotal,
  dropShare,
  eventsBetween,
  recentBuckets,
  topPort,
  topTalker,
} from './whisperStats'
import type { ClientEvent, TimeBucket } from './types'

function bucket(time: string, byAction: TimeBucket['byAction']): TimeBucket {
  return { time, byAction }
}

function evt(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: Math.random(),
    time: '2026-01-01T00:00:00Z',
    deviceId: 'router1',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'r',
    chain: 'forward',
    raw: '',
    receivedAt: 0,
    ...overrides,
  }
}

describe('recentBuckets', () => {
  it('takes the last n, oldest first', () => {
    const series = Array.from({ length: 60 }, (_, i) => bucket(`m${i}`, {}))
    const got = recentBuckets(series, 15)
    expect(got).toHaveLength(15)
    expect(got[0].time).toBe('m45')
    expect(got[14].time).toBe('m59')
  })

  it('never asks for more than the series has', () => {
    const series = [bucket('m0', {}), bucket('m1', {})]
    expect(recentBuckets(series, 15)).toHaveLength(2)
  })
})

describe('bucketTotal', () => {
  it('sums every action slot', () => {
    expect(bucketTotal(bucket('t', { accept: 3, drop: 2, natted: 1 }))).toBe(6)
  })

  it('is 0 for an empty bucket', () => {
    expect(bucketTotal(bucket('t', {}))).toBe(0)
  })
})

describe('dropShare', () => {
  it('counts drop and reject together as refused', () => {
    const buckets = [bucket('t', { accept: 6, drop: 2, reject: 2 })]
    expect(dropShare(buckets)).toBeCloseTo(0.4)
  })

  it('is null, not 0, over a window with no traffic -- absence, not a clean reading', () => {
    const buckets = [bucket('t1', {}), bucket('t2', {})]
    expect(dropShare(buckets)).toBeNull()
  })

  it('sums across every bucket in the window', () => {
    const buckets = [bucket('t1', { accept: 10 }), bucket('t2', { drop: 10 })]
    expect(dropShare(buckets)).toBeCloseTo(0.5)
  })
})

describe('bucketAt', () => {
  const buckets = [bucket('2026-01-01T00:00:00Z', {}), bucket('2026-01-01T00:01:00Z', {})]

  it('finds the bucket whose minute contains the timestamp', () => {
    const ms = new Date('2026-01-01T00:01:30Z').getTime()
    expect(bucketAt(buckets, ms)?.time).toBe('2026-01-01T00:01:00Z')
  })

  it('returns undefined outside the series', () => {
    const ms = new Date('2026-01-01T00:05:00Z').getTime()
    expect(bucketAt(buckets, ms)).toBeUndefined()
  })
})

describe('eventsBetween', () => {
  it('is start-inclusive, end-exclusive on receivedAt', () => {
    const events = [evt({ id: 1, receivedAt: 100 }), evt({ id: 2, receivedAt: 200 }), evt({ id: 3, receivedAt: 300 })]
    const got = eventsBetween(events, 100, 300)
    expect(got.map((e) => e.id)).toEqual([1, 2])
  })
})

describe('topTalker', () => {
  it('prefers a resolved host name over the bare IP', () => {
    const events = [
      evt({ srcIp: '10.0.0.1', srcHostName: 'nas' }),
      evt({ srcIp: '10.0.0.1', srcHostName: 'nas' }),
      evt({ srcIp: '10.0.0.2' }),
    ]
    expect(topTalker(events)).toBe('nas')
  })

  it('is undefined over an empty window -- absence, not a fake answer', () => {
    expect(topTalker([])).toBeUndefined()
  })
})

describe('topPort', () => {
  it('counts the destination port when present', () => {
    const events = [evt({ dstPort: 445 }), evt({ dstPort: 445 }), evt({ dstPort: 22 })]
    expect(topPort(events)).toBe('445')
  })

  it('falls back to the source port when there is no destination port', () => {
    const events = [evt({ srcPort: 5353 }), evt({ srcPort: 5353 })]
    expect(topPort(events)).toBe('5353')
  })

  it('is undefined when nothing in the window carries a port', () => {
    expect(topPort([evt({})])).toBeUndefined()
  })
})
