// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { duplicateDrift, duplicateDriftMessage, duplicateCleanupCommand } from './ingestDuplicates'
import type { IngestLossHostCounter, SyslogListenerStats } from './types'

function statsWithDuplicate(duplicate: IngestLossHostCounter): SyslogListenerStats {
  return {
    inUse: 0,
    capacity: 256,
    reservedForConfigured: 0,
    rejected: 0,
    rejectedConfigured: 0,
    dropped: 0,
    oversized: 0,
    rejectedConfiguredHosts: [],
    oversizedHost: '',
    loss: {
      dropped: { recent: 0, lastAt: null, active: false },
      rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
      rejected: { recent: 0, lastAt: null, active: false },
      oversized: { recent: 0, lastAt: null, active: false, declared: false, runs: 0 },
      duplicate,
    },
  }
}

describe('duplicateDrift', () => {
  it('is undefined when there is no loss block at all (an older server)', () => {
    expect(duplicateDrift(undefined)).toBeUndefined()
  })

  it('is undefined before any duplicate has been reported', () => {
    const stats = statsWithDuplicate({ recent: 0, lastAt: null, active: false, declared: false, runs: 0 })
    expect(duplicateDrift(stats)).toBeUndefined()
  })

  it('is undefined once the server reports the source no longer active', () => {
    const stats = statsWithDuplicate({
      recent: 40,
      lastAt: '2026-09-14T12:00:00Z',
      active: false,
      declared: true,
      runs: 0,
      host: '198.51.100.10',
      copyCount: 2,
    })
    expect(duplicateDrift(stats)).toBeUndefined()
  })

  it('reports the host and copy count while the server says it is active', () => {
    const stats = statsWithDuplicate({
      recent: 25,
      lastAt: '2026-09-14T12:00:00Z',
      active: true,
      declared: true,
      runs: 0,
      host: '198.51.100.10',
      copyCount: 3,
    })
    expect(duplicateDrift(stats)).toEqual({ host: '198.51.100.10', copyCount: 3 })
  })
})

describe('duplicateDriftMessage', () => {
  it('says "twice" for the common double-paste case rather than "2×"', () => {
    expect(duplicateDriftMessage({ host: '198.51.100.10', copyCount: 2 })).toBe(
      '198.51.100.10 appears to be sending every line twice over',
    )
  })

  it('uses the numeral for three or more copies', () => {
    expect(duplicateDriftMessage({ host: '198.51.100.10', copyCount: 3 })).toBe(
      '198.51.100.10 appears to be sending every line 3× over',
    )
  })
})

describe('duplicateCleanupCommand', () => {
  it('matches the RouterOS command the wizard rule cleanup expects', () => {
    expect(duplicateCleanupCommand).toBe('/system logging remove [find action=mikroview]')
  })
})
