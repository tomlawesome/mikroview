// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  formatRelative,
  formatDurationShort,
  formatTimeMs,
  formatUptimeDaysHours,
  formatBufferDepth,
  parseGoDurationSeconds,
  formatDaysSince,
} from './format'

describe('formatTimeMs', () => {
  // Asserted on the tail only: the hour/minute/second part goes through
  // toLocaleTimeString, whose exact rendering depends on the runner's
  // locale and timezone -- the milliseconds suffix is what this function
  // adds over formatTime, so it is what these tests pin.
  it('appends the milliseconds, zero-padded to three digits', () => {
    expect(formatTimeMs('2026-08-08T14:02:11.482Z')).toMatch(/:\d{2}\.482$/)
    expect(formatTimeMs('2026-08-08T14:02:11.007Z')).toMatch(/:\d{2}\.007$/)
    expect(formatTimeMs('2026-08-08T14:02:11.000Z')).toMatch(/:\d{2}\.000$/)
  })

  it('returns the original string unchanged for an unparseable value', () => {
    expect(formatTimeMs('not-a-date')).toBe('not-a-date')
  })
})

describe('formatRelative', () => {
  const now = new Date('2026-01-01T12:00:00.000Z').getTime()

  it('reads "just now" for anything under 5 seconds', () => {
    expect(formatRelative(new Date(now - 2000).toISOString(), now)).toBe('just now')
    expect(formatRelative(new Date(now).toISOString(), now)).toBe('just now')
  })

  it('renders seconds under a minute', () => {
    expect(formatRelative(new Date(now - 45_000).toISOString(), now)).toBe('45s ago')
  })

  it('renders minutes under an hour', () => {
    expect(formatRelative(new Date(now - 5 * 60_000).toISOString(), now)).toBe('5m ago')
    expect(formatRelative(new Date(now - 59 * 60_000).toISOString(), now)).toBe('59m ago')
  })

  it('renders hours under a day', () => {
    expect(formatRelative(new Date(now - 3 * 3_600_000).toISOString(), now)).toBe('3h ago')
  })

  it('renders days at a day or beyond', () => {
    expect(formatRelative(new Date(now - 2 * 86_400_000).toISOString(), now)).toBe('2d ago')
  })

  it('never goes negative when the timestamp is slightly ahead of now (clock skew)', () => {
    expect(formatRelative(new Date(now + 5000).toISOString(), now)).toBe('just now')
  })

  it('returns the original string unchanged for an unparseable value', () => {
    expect(formatRelative('not-a-date', now)).toBe('not-a-date')
  })
})

describe('formatDurationShort', () => {
  it('renders seconds under a minute with no second unit', () => {
    expect(formatDurationShort(45)).toBe('45s')
    expect(formatDurationShort(0)).toBe('0s')
  })

  it('renders minutes and seconds under an hour', () => {
    expect(formatDurationShort(172)).toBe('2m 52s')
    expect(formatDurationShort(3599)).toBe('59m 59s')
  })

  it('renders hours and minutes under a day', () => {
    expect(formatDurationShort(20_013)).toBe('5h 33m')
  })

  it('renders days and hours at a day or beyond', () => {
    expect(formatDurationShort(3 * 86_400 + 4 * 3600)).toBe('3d 4h')
  })

  it('never goes negative', () => {
    expect(formatDurationShort(-50)).toBe('0s')
  })
})

describe('formatUptimeDaysHours', () => {
  it('renders the drawn form -- days and hours, spaced', () => {
    expect(formatUptimeDaysHours(12 * 86_400 + 4 * 3600)).toBe('12 d 4 h')
  })

  it('shows a zero days unit rather than dropping it under a day', () => {
    expect(formatUptimeDaysHours(3 * 3600 + 9 * 60)).toBe('0 d 3 h')
  })

  // The point of the two-unit form: minutes and seconds are discarded,
  // so a menu left open for a minute renders the same string throughout.
  it('ignores the minutes and seconds under the hour', () => {
    expect(formatUptimeDaysHours(2 * 86_400 + 5 * 3600 + 59 * 60 + 59)).toBe('2 d 5 h')
  })

  it('renders zero as all-zero units', () => {
    expect(formatUptimeDaysHours(0)).toBe('0 d 0 h')
  })

  it('never goes negative', () => {
    expect(formatUptimeDaysHours(-50)).toBe('0 d 0 h')
  })
})

describe('formatBufferDepth', () => {
  it('reports a percentage while the ring has not filled yet', () => {
    expect(formatBufferDepth(200_000, 126_004, 1348.5)).toBe('63% of buffer used')
  })

  it('rounds down to 0% for an empty ring rather than hiding the indicator', () => {
    expect(formatBufferDepth(200_000, 0, 0)).toBe('0% of buffer used')
  })

  // Real numbers from the instance that motivated issue #244: a full
  // 200,000-capacity ring at a measured 1,348.5 events/sec holds about
  // 148 seconds, not the 24h store.retention implies.
  it('reports how far back it reaches once the ring is full', () => {
    expect(formatBufferDepth(200_000, 200_000, 1348.5)).toBe('holding last 2m 28s')
  })

  it('falls back to "buffer full" rather than a noise-dominated estimate at a near-zero rate', () => {
    expect(formatBufferDepth(200_000, 200_000, 0.05)).toBe('buffer full')
    expect(formatBufferDepth(200_000, 200_000, 0)).toBe('buffer full')
  })

  it('returns nothing for a zero or negative capacity rather than dividing by it', () => {
    expect(formatBufferDepth(0, 0, 10)).toBe('')
    expect(formatBufferDepth(-1, 0, 10)).toBe('')
  })
})

// #677's port-scan window row reads a definition's "window" param back
// as Go's time.Duration.String() output (see internal/engine/params.go's
// validateDurationParam) and needs it as plain seconds.
describe('parseGoDurationSeconds', () => {
  it('parses a bare seconds value', () => {
    expect(parseGoDurationSeconds('60s')).toBe(60)
  })

  it('parses Go\'s compound minutes+seconds notation', () => {
    expect(parseGoDurationSeconds('1m0s')).toBe(60)
    expect(parseGoDurationSeconds('1m30s')).toBe(90)
  })

  it('parses hours and sub-second units', () => {
    expect(parseGoDurationSeconds('1h30m0s')).toBe(5400)
    expect(parseGoDurationSeconds('500ms')).toBe(0.5)
  })
})

describe('formatDaysSince', () => {
  it('renders a whole-day count', () => {
    const fourDaysAgo = new Date(Date.now() - 4.5 * 86_400_000).toISOString()
    expect(formatDaysSince(fourDaysAgo)).toBe('4 d')
  })

  it('says "under a day" inside the first 24h rather than "0 d"', () => {
    const anHourAgo = new Date(Date.now() - 3600_000).toISOString()
    expect(formatDaysSince(anHourAgo)).toBe('under a day')
  })
})
