// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { SPANS, describeReach, reachSeconds, spanAvailable, unavailableReason } from './spans'

const NOW = Date.parse('2026-08-31T14:00:00Z')

describe('the buffer reach', () => {
  it('is null when the buffer holds nothing, rather than zero', () => {
    // Zero would read as "reaches back no distance", which is true but
    // indistinguishable from a buffer holding one event this instant.
    expect(reachSeconds(null, NOW)).toBeNull()
    expect(reachSeconds(undefined, NOW)).toBeNull()
  })

  it('is the distance from the oldest held event to now', () => {
    expect(reachSeconds('2026-08-31T13:00:00Z', NOW)).toBe(3600)
  })

  it('is null rather than NaN when the server sends something unparseable', () => {
    expect(reachSeconds('not a time', NOW)).toBeNull()
  })

  it('never goes negative when a clock skews', () => {
    expect(reachSeconds('2026-08-31T14:05:00Z', NOW)).toBe(0)
  })
})

describe('which spans are offered', () => {
  const [fifteen, hour, day, fortnight] = SPANS

  it('always offers the shortest, so there is a defined state on an empty buffer', () => {
    expect(spanAvailable(fifteen, null)).toBe(true)
  })

  it('withholds every longer span while the buffer holds nothing', () => {
    for (const span of [hour, day, fortnight]) {
      expect(spanAvailable(span, null)).toBe(false)
    }
  })

  // The failure this whole control exists to prevent: nine hours of
  // buffer offering a fortnight, and answering with nine hours.
  it('withholds a fortnight from a buffer holding nine hours', () => {
    const nineHours = 9 * 3600
    expect(spanAvailable(hour, nineHours)).toBe(true)
    expect(spanAvailable(day, nineHours)).toBe(false)
    expect(spanAvailable(fortnight, nineHours)).toBe(false)
  })

  it('offers a span exactly when the reach meets it', () => {
    expect(spanAvailable(hour, 3599)).toBe(false)
    expect(spanAvailable(hour, 3600)).toBe(true)
  })
})

describe('what it says about the reach', () => {
  it('says nothing is held rather than naming a duration', () => {
    expect(describeReach(null)).toBe('nothing held yet')
  })

  it('scales its words to the distance', () => {
    expect(describeReach(45)).toBe('holding 45 s')
    expect(describeReach(600)).toBe('holding 10 min')
    expect(describeReach(9 * 3600)).toBe('holding 9 h')
    expect(describeReach(3 * 86400)).toBe('holding 3 d')
  })

  it('explains a withheld span in terms of what is really there', () => {
    expect(unavailableReason(SPANS[3], 9 * 3600)).toBe(
      '14 d of history is not held — the buffer is holding 9 h',
    )
  })
})
