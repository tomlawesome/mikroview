// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { episodeShapeFor, episodeShapeText } from './episodeShape'
import { formatHM, formatTime } from './format'
import type { FirewallEvent } from './types'

// Expected strings are built through the same locale-dependent
// formatHM/formatTime the module itself uses (see format.test.ts's own
// convention) -- exact HH:MM text depends on the test runner's
// timezone, but going through the same formatter on both sides makes
// the comparison timezone-agnostic.
function ms(minutesFromEpoch: number): number {
  return minutesFromEpoch * 60_000
}

function iso(m: number): string {
  return new Date(ms(m)).toISOString()
}

describe('episodeShapeText', () => {
  it('returns empty for no timestamps at all', () => {
    expect(episodeShapeText([], ms(0))).toBe('')
  })

  it('a single timestamp: the time and whether it is recent', () => {
    expect(episodeShapeText([ms(10)], ms(12))).toBe(`${formatHM(iso(10))} · still arriving`)
    expect(episodeShapeText([ms(10)], ms(60))).toBe(`${formatHM(iso(10))} · quiet since`)
  })

  // "13:34 * 13:36 * 13:39 * quiet since" -- a handful of irregular
  // events, each named, not currently recent.
  it('enumerates up to four events individually, "quiet since" when not recent', () => {
    const now = ms(30)
    expect(episodeShapeText([ms(0), ms(2), ms(5)], now)).toBe(
      `${formatHM(iso(0))} · ${formatHM(iso(2))} · ${formatHM(iso(5))} · quiet since`,
    )
  })

  it('the same handful reads "still arriving" when the last one just landed', () => {
    const now = ms(6)
    expect(episodeShapeText([ms(0), ms(2), ms(5)], now)).toBe(
      `${formatHM(iso(0))} · ${formatHM(iso(2))} · ${formatHM(iso(5))} · still arriving`,
    )
  })

  // "13:41:02 -> 13:41:42 * stopped" -- many events bunched into a short
  // (<=2 minute) span get second-level precision.
  it('a short, tightly-spaced burst gets second-precision two-point format', () => {
    const events = [0, 0.15, 0.3, 0.45, 0.6].map((m) => ms(m)) // 40 s of wall time
    const now = ms(15)
    expect(episodeShapeText(events, now)).toBe(`${formatTime(iso(0))} → ${formatTime(iso(0.6))} · stopped`)
  })

  it('the same short burst reads "still arriving" while it is still landing', () => {
    const events = [0, 0.15, 0.3, 0.45, 0.6].map((m) => ms(m))
    const now = ms(0.6) + 60_000 // 1 minute after the last event
    expect(episodeShapeText(events, now)).toBe(`${formatTime(iso(0))} → ${formatTime(iso(0.6))} · still arriving`)
  })

  // "first 13:46 * last 13:52 * still arriving" -- many events, spread
  // unevenly over more than two minutes.
  it('a longer, irregular spread gets minute-precision first/last format', () => {
    const events = [0, 0.5, 0.6, 3, 4, 6].map((m) => ms(m)) // 6 minutes, bunched toward the end
    const now = ms(6) + 3 * 60_000 // 3 minutes after the last event: recent
    expect(episodeShapeText(events, now)).toBe(`first ${formatHM(iso(0))} · last ${formatHM(iso(6))} · still arriving`)
  })

  it('the same shape reads "stopped" once the last event is no longer recent', () => {
    const events = [0, 0.5, 0.6, 3, 4, 6].map((m) => ms(m))
    const now = ms(6) + 20 * 60_000 // 20 minutes after the last event
    expect(episodeShapeText(events, now)).toBe(`first ${formatHM(iso(0))} · last ${formatHM(iso(6))} · stopped`)
  })

  // "every ~2 m since 13:28" -- several events at a steady cadence,
  // spanning more than a couple of minutes: repeated_drops' own shape,
  // independent of whether the cadence is still ongoing.
  it('a steady cadence over more than two minutes reads as its own shape', () => {
    const events = [0, 2, 4, 6, 8].map((m) => ms(m)) // every 2 minutes
    const now = ms(8) + 3 * 60_000
    expect(episodeShapeText(events, now)).toBe(`every ~2 m since ${formatHM(iso(0))}`)
  })

  it('a steady but sub-two-minute cadence stays a burst, not a cadence (port_scan stays "stopped")', () => {
    // Twenty events, evenly walked over forty seconds -- regular gaps,
    // but the whole thing is over before REGULAR_SPAN_FLOOR_MS, so it
    // must not read as "every ~2 s since ..."
    const events = Array.from({ length: 20 }, (_, i) => ms((i * 2) / 60))
    const now = ms(15)
    const shape = episodeShapeText(events, now)
    expect(shape).not.toMatch(/^every/)
    expect(shape).toMatch(/stopped$/)
  })
})

describe('episodeShapeFor', () => {
  const flag = { firstSeen: iso(0), lastSeen: iso(10) }

  it('falls back to firstSeen/lastSeen while the episode has not resolved', () => {
    expect(episodeShapeFor(flag, undefined, ms(11))).toBe(
      episodeShapeText([ms(0), ms(10)], ms(11)),
    )
    expect(episodeShapeFor(flag, 'loading', ms(11))).toBe(episodeShapeText([ms(0), ms(10)], ms(11)))
    expect(episodeShapeFor(flag, 'error', ms(11))).toBe(episodeShapeText([ms(0), ms(10)], ms(11)))
  })

  it('falls back to firstSeen/lastSeen when the episode resolved but nothing is still buffered', () => {
    expect(episodeShapeFor(flag, [], ms(11))).toBe(episodeShapeText([ms(0), ms(10)], ms(11)))
  })

  it('collapses to a single point when firstSeen equals lastSeen', () => {
    const single = { firstSeen: iso(5), lastSeen: iso(5) }
    expect(episodeShapeFor(single, undefined, ms(6))).toBe(episodeShapeText([ms(5)], ms(6)))
  })

  it('prefers the real per-event episode once it has resolved with something', () => {
    const events: FirewallEvent[] = [
      { id: 1, time: iso(0), deviceId: 'd1', sourceIp: '10.0.0.1', action: 'drop', ruleLabel: 'r', chain: 'forward', raw: '' },
      { id: 2, time: iso(1), deviceId: 'd1', sourceIp: '10.0.0.1', action: 'drop', ruleLabel: 'r', chain: 'forward', raw: '' },
    ]
    // The flag's own firstSeen/lastSeen span ten minutes; the real
    // episode only spans one -- the real events must win.
    expect(episodeShapeFor(flag, events, ms(2))).toBe(episodeShapeText([ms(0), ms(1)], ms(2)))
  })
})
