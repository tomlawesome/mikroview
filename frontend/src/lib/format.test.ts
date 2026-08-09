// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { formatRelative, formatDurationShort, formatBufferDepth } from './format'

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
