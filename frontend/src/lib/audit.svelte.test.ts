// SPDX-License-Identifier: AGPL-3.0-only
//
// #1089: refresh() used to have no catch at all -- a rejected
// fetchAuditLog() call left AuditLog.svelte's onMount call unguarded and
// `loaded` didn't exist, so a failed fetch and a genuinely empty log were
// indistinguishable (both read as an empty `list`).

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuditEntry } from './types'

vi.mock('./api', () => ({
  fetchAuditLog: vi.fn(),
}))

import { fetchAuditLog } from './api'
import { auditState } from './audit.svelte'

function entry(overrides: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: 1,
    timestamp: '2026-08-30T10:00:00Z',
    actor: 'tom',
    action: 'entity.upsert',
    target: 'nas',
    detail: '',
    ...overrides,
  }
}

describe('auditState.refresh', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auditState.list = []
    auditState.hasMore = false
    auditState.loaded = false
    auditState.error = null
  })

  it('is not loaded before the first refresh resolves', () => {
    expect(auditState.loaded).toBe(false)
    expect(auditState.error).toBeNull()
  })

  it('sets loaded and populates list on success, leaving error null', async () => {
    vi.mocked(fetchAuditLog).mockResolvedValue({ entries: [entry()], hasMore: true })

    await auditState.refresh()

    expect(auditState.loaded).toBe(true)
    expect(auditState.error).toBeNull()
    expect(auditState.list).toHaveLength(1)
    expect(auditState.hasMore).toBe(true)
  })

  it('catches a rejected fetch, sets loaded and error, and leaves list untouched', async () => {
    auditState.list = [entry({ id: 9 })]
    vi.mocked(fetchAuditLog).mockRejectedValue(new Error('network unreachable'))

    await auditState.refresh()

    expect(auditState.loaded).toBe(true)
    expect(auditState.error).toBe('network unreachable')
    // The stale list from the prior successful fetch is left alone --
    // same reasoning as appState.fetchFailed's own doc comment: a failed
    // refresh is not "zero entries".
    expect(auditState.list).toHaveLength(1)
  })

  it('clears a previous error once a later refresh succeeds', async () => {
    vi.mocked(fetchAuditLog).mockRejectedValueOnce(new Error('boom'))
    await auditState.refresh()
    expect(auditState.error).toBe('boom')

    vi.mocked(fetchAuditLog).mockResolvedValueOnce({ entries: [], hasMore: false })
    await auditState.refresh()

    expect(auditState.error).toBeNull()
    expect(auditState.loaded).toBe(true)
  })
})
