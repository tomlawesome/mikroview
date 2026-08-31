// SPDX-License-Identifier: AGPL-3.0-only
//
// The foot line's three facts (#691). Most of what is worth pinning
// here is absence: each fact has to disappear rather than round down,
// guess a destination it was never keyed on, or print a multiple the
// buffer cannot support.
import { describe, expect, it } from 'vitest'
import { darkBoundaryFact, footLineFacts, repeatingPathwayFact, surgeFact } from './footLine'
import type { FallBoundary } from './fall.svelte'
import type { ClientEvent, Flag, FlagType } from './types'

const NOW = Date.parse('2026-08-31T14:02:00Z')

function flag(type: FlagType, target: string, over: Partial<Flag> = {}): Flag {
  return {
    id: `${type}:${target}`,
    type,
    target,
    detail: 'detail',
    count: 14,
    firstSeen: new Date(NOW - 10 * 60_000).toISOString(),
    lastSeen: new Date(NOW - 60_000).toISOString(),
    cleared: false,
    ...over,
  }
}

function ev(over: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: Math.random(),
    time: new Date(NOW).toISOString(),
    deviceId: 'router1',
    sourceIp: '203.0.113.10',
    action: 'drop',
    ruleLabel: 'iot-to-lan-drop',
    chain: 'forward',
    raw: 'raw',
    receivedAt: NOW,
    ...over,
  }
}

function boundary(over: Partial<FallBoundary> = {}): FallBoundary {
  return {
    key: 'forward|guest|wan',
    chain: 'forward',
    inInterface: 'guest',
    outInterface: 'wan',
    srcAddressList: 'guest',
    label: 'guest → wan',
    coverage: 'dark',
    epithet: '',
    ...over,
  }
}

describe('repeatingPathwayFact', () => {
  const drops = flag('repeated_drops', '10.0.20.11 -> port 445', {
    evidence: { hosts: ['10.0.40.5'] },
  })

  it('names the pathway, the episode, the count and that it is flagged', () => {
    const events = [
      ev({ srcIp: '10.0.20.11', srcHostName: 'cam-porch', dstIp: '10.0.40.5', dstHostName: 'nas', dstPort: 445 }),
    ]
    const fact = repeatingPathwayFact([drops], events, NOW)
    expect(fact?.salient).toBe('cam-porch → nas :445')
    expect(fact?.tail).toContain('14 so far')
    expect(fact?.tail).toContain('flagged')
    expect(fact?.lead).toBe('')
  })

  it('falls back to raw addresses when the buffer has never named them', () => {
    expect(repeatingPathwayFact([drops], [], NOW)?.salient).toBe('10.0.20.11 → 10.0.40.5 :445')
  })

  // The detector keys on (source, port) and deliberately not on the
  // destination -- with several destinations there is no single one this
  // flag could truthfully point an arrow at.
  it('drops the destination when the flag reached more than one', () => {
    const many = flag('repeated_drops', '10.0.20.11 -> port 445', {
      evidence: { hosts: ['10.0.40.5', '10.0.40.6'] },
    })
    expect(repeatingPathwayFact([many], [], NOW)?.salient).toBe('10.0.20.11 :445')
  })

  it('drops the destination when the flag carries no evidence at all', () => {
    const bare = flag('repeated_drops', '10.0.20.11 -> port 445', { evidence: undefined })
    expect(repeatingPathwayFact([bare], [], NOW)?.salient).toBe('10.0.20.11 :445')
  })

  // episodeShape.ts owns the cadence wording; this pins that the flag's
  // own refused events are what reaches it, so the band and the flag
  // drawer phrase one episode one way.
  it('reads the cadence off the flag’s own events in the buffer', () => {
    const times = [0, 45, 90, 135, 180, 225].map((s) => NOW - 400_000 + s * 1000)
    const events = times.map((t) =>
      ev({ receivedAt: t, time: new Date(t).toISOString(), srcIp: '10.0.20.11', dstPort: 445, action: 'drop' }),
    )
    expect(repeatingPathwayFact([drops], events, NOW)?.tail).toContain('every ~45 s since')
  })

  it('is absent with no repeated_drops flag, and when the only one is cleared', () => {
    expect(repeatingPathwayFact([], [], NOW)).toBeNull()
    expect(repeatingPathwayFact([flag('repeated_drops', '10.0.20.11 -> port 445', { cleared: true })], [], NOW)).toBeNull()
  })

  it('is absent when the target is not the source/port composite', () => {
    expect(repeatingPathwayFact([flag('repeated_drops', 'something else')], [], NOW)).toBeNull()
  })
})

describe('surgeFact', () => {
  // Two hours of buffer: one event a minute until the flag's firstSeen,
  // then three a minute after it.
  const since = NOW - 30 * 60_000
  function surgeBuffer(beforePerMin: number, afterPerMin: number): ClientEvent[] {
    const out: ClientEvent[] = []
    for (let t = since - 30 * 60_000; t < since; t += 60_000) {
      for (let i = 0; i < beforePerMin; i++) out.push(ev({ receivedAt: t }))
    }
    for (let t = since; t < NOW; t += 60_000) {
      for (let i = 0; i < afterPerMin; i++) out.push(ev({ receivedAt: t }))
    }
    return out
  }

  const global = flag('global_spike', 'global', { firstSeen: new Date(since).toISOString() })

  it('sizes a network-wide spike against the buffer before it started', () => {
    const fact = surgeFact([global], surgeBuffer(1, 3), NOW)
    expect(fact?.salient).toBe('▲3.0×')
    expect(fact?.tail).toBe('traffic since 14:32')
  })

  it('measures an activity spike over its own source only', () => {
    const spike = flag('activity_spike', '10.0.20.11', { firstSeen: new Date(since).toISOString() })
    const events = [
      ...surgeBuffer(1, 3).map((e) => ({ ...e, srcIp: '10.0.20.11', srcHostName: 'cam-porch' })),
      // A different, flat source: counted by a whole-buffer reading,
      // which would drag the multiple down. It must not be.
      ...surgeBuffer(5, 5).map((e) => ({ ...e, srcIp: '10.0.10.2' })),
    ]
    const fact = surgeFact([spike], events, NOW)
    expect(fact?.salient).toBe('▲3.0×')
    expect(fact?.tail).toBe('from cam-porch since 14:32')
  })

  it('names the rule for a rule spike', () => {
    const spike = flag('rule_spike', 'iot-to-lan-drop', { firstSeen: new Date(since).toISOString() })
    expect(surgeFact([spike], surgeBuffer(1, 3), NOW)?.tail).toBe('on rule iot-to-lan-drop since 14:32')
  })

  // The honesty rule, four ways: with no baseline in the buffer, with
  // too little buffer either side, and with no actual rise, there is no
  // number that can be printed -- so nothing is.
  it('is absent when the buffer holds nothing from before the flag started', () => {
    const afterOnly = surgeBuffer(1, 3).filter((e) => e.receivedAt >= since)
    expect(surgeFact([global], afterOnly, NOW)).toBeNull()
  })

  it('is absent when either side of the comparison is under a minute', () => {
    const justStarted = flag('global_spike', 'global', { firstSeen: new Date(NOW - 10_000).toISOString() })
    expect(surgeFact([justStarted], surgeBuffer(1, 3), NOW)).toBeNull()
    const barelyBuffered = flag('global_spike', 'global', {
      firstSeen: new Date(NOW - 20 * 60_000).toISOString(),
    })
    const thinBefore = [ev({ receivedAt: NOW - 20 * 60_000 - 5_000 }), ev({ receivedAt: NOW - 60_000 })]
    expect(surgeFact([barelyBuffered], thinBefore, NOW)).toBeNull()
  })

  it('is absent when the rate did not actually rise', () => {
    expect(surgeFact([global], surgeBuffer(3, 3), NOW)).toBeNull()
    expect(surgeFact([global], surgeBuffer(3, 1), NOW)).toBeNull()
  })

  it('is absent for flag types that are not about volume', () => {
    const offHours = flag('off_hours_activity', '10.0.20.11', { firstSeen: new Date(since).toISOString() })
    expect(surgeFact([offHours], surgeBuffer(1, 3), NOW)).toBeNull()
  })
})

describe('darkBoundaryFact', () => {
  it('states the dark boundary in the fall’s own words', () => {
    expect(darkBoundaryFact([boundary()])).toEqual({
      key: 'dark',
      lead: 'guest → wan',
      salient: 'dark',
      tail: '— nothing logged, not nothing sent',
    })
  })

  // 'unknown' means "no rule table has been pushed, so we cannot say" --
  // never dark, and never captioned.
  it('is absent for observed and unknown coverage alike', () => {
    expect(darkBoundaryFact([boundary({ coverage: 'observed' })])).toBeNull()
    expect(darkBoundaryFact([boundary({ coverage: 'unknown' })])).toBeNull()
    expect(darkBoundaryFact([])).toBeNull()
  })
})

describe('footLineFacts', () => {
  it('returns nothing at all when no fact has data behind it', () => {
    expect(footLineFacts({ flags: [], events: [], boundaries: [], nowMs: NOW })).toEqual([])
  })

  it('returns only the facts that have data, in the drawing’s order', () => {
    const facts = footLineFacts({
      flags: [flag('repeated_drops', '10.0.20.11 -> port 445')],
      events: [],
      boundaries: [boundary()],
      nowMs: NOW,
    })
    expect(facts.map((f) => f.key)).toEqual(['pathway', 'dark'])
  })
})
