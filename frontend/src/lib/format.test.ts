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
  formatDayMonth,
  formatLastHeard,
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

// #640's returning cards say when a pair was last judged. Day and month
// only: the locale decides the order ("2 Sept" here, "Sep 2" in a US
// one), so these assert on what the helper does rather than on one
// locale's spelling of it.
describe('formatDayMonth', () => {
  it('renders a bare day and short month, with no year and no clock time', () => {
    const out = formatDayMonth('2026-09-02T09:00:00Z')
    expect(out).toContain('2')
    expect(out).toContain('Sep')
    expect(out).not.toContain('2026')
    expect(out).not.toContain(':')
  })

  it('returns the input unchanged when it does not parse, same as its neighbours', () => {
    expect(formatDayMonth('not a date')).toBe('not a date')
  })
})

// #710's round-38 cutover: Fleet's and Entities' "last heard" line moved
// off formatRelative's bare "3d ago" onto this rule. TZ is pinned to UTC
// by vitest.config.ts, so the ISO fixtures below double as wall-clock
// times and the day-boundary math is exact rather than approximate.
describe('formatLastHeard', () => {
  it('under an hour, matches formatSpacedAge\'s short form', () => {
    const now = new Date('2026-09-08T14:00:00Z').getTime()
    const fiftyNineAgo = new Date(now - 59 * 60_000).toISOString()
    expect(formatLastHeard(fiftyNineAgo, now)).toBe('59 m')
  })

  it('just past an hour but still today, renders the clock time alone', () => {
    const now = new Date('2026-09-08T14:00:00Z').getTime()
    const sixtyOneAgo = new Date(now - 61 * 60_000).toISOString()
    expect(formatLastHeard(sixtyOneAgo, now)).toBe('12:59')
  })

  it('yesterday, renders weekday name and clock time', () => {
    const now = new Date('2026-09-08T14:00:00Z').getTime()
    expect(formatLastHeard('2026-09-07T21:14:00Z', now)).toBe('Monday 21:14')
  })

  it('six days ago, still within the 7-day window, renders weekday and time', () => {
    const now = new Date('2026-09-08T14:00:00Z').getTime()
    expect(formatLastHeard('2026-09-02T14:00:00Z', now)).toBe('Wednesday 14:00')
  })

  it('eight days ago, past the 7-day window, renders day + short month + time', () => {
    const now = new Date('2026-09-08T14:00:00Z').getTime()
    expect(formatLastHeard('2026-08-31T14:00:00Z', now)).toBe('31 Aug 14:00')
  })

  it('crosses a year boundary without leaking a year into the output', () => {
    const now = new Date('2027-01-02T09:15:00Z').getTime()
    const out = formatLastHeard('2026-12-25T09:15:00Z', now)
    expect(out).toBe('25 Dec 09:15')
    expect(out).not.toContain('2026')
    expect(out).not.toContain('2027')
  })

  it('returns the input unchanged when it does not parse, same as its neighbours', () => {
    expect(formatLastHeard('not a date', Date.now())).toBe('not a date')
  })
})
