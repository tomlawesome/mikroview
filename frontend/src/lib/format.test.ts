import { describe, expect, it } from 'vitest'
import { formatRelative } from './format'

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
