// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// AuditLog.svelte itself makes no requests directly -- this stops
// auditState.refresh() (called onMount) from reaching for the network
// under jsdom.
vi.mock('../lib/api', () => ({
  fetchAuditLog: vi.fn(async () => ({ entries: [], hasMore: false })),
}))

import { fetchAuditLog } from '../lib/api'
import { auditState } from '../lib/audit.svelte'
import { appState } from '../lib/state.svelte'
import type { AuditEntry } from '../lib/types'
import AuditLog from './AuditLog.svelte'

function entry(overrides: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: 1,
    timestamp: '2026-08-30T10:00:00Z',
    actor: 'tom',
    action: 'entity.update',
    target: 'nas',
    detail: '',
    ...overrides,
  }
}

async function renderLog(entries: AuditEntry[]) {
  vi.mocked(fetchAuditLog).mockResolvedValue({ entries, hasMore: false })
  render(AuditLog)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

// Issue #649: every column on the docket's three tabs sorts (click a
// head, again to reverse) and filters (a quiet dashed row beneath the
// heads). The audit log is the one tab that was already a real <table>,
// so this is the most direct mapping onto the round-18/19 mockup.
describe('AuditLog sort and filter (#649)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    appState.now = new Date('2026-08-30T12:00:00Z').getTime()
  })

  function actorTexts() {
    return Array.from(document.querySelectorAll('tbody tr .actor')).map((el) => el.textContent?.trim())
  }

  it('defaults to newest first, matching the fixed order this replaces', async () => {
    await renderLog([
      entry({ id: 1, actor: 'first', timestamp: '2026-08-30T09:00:00Z' }),
      entry({ id: 2, actor: 'second', timestamp: '2026-08-30T11:00:00Z' }),
    ])
    expect(actorTexts()).toEqual(['second', 'first'])
  })

  it('clicking a column head sorts by it, and again reverses', async () => {
    await renderLog([
      entry({ id: 1, actor: 'bea', timestamp: '2026-08-30T09:00:00Z' }),
      entry({ id: 2, actor: 'alice', timestamp: '2026-08-30T11:00:00Z' }),
    ])

    await fireEvent.click(screen.getByRole('columnheader', { name: /Actor/ }))
    flushSync()
    expect(actorTexts()).toEqual(['alice', 'bea'])

    await fireEvent.click(screen.getByRole('columnheader', { name: /Actor/ }))
    flushSync()
    expect(actorTexts()).toEqual(['bea', 'alice'])
  })

  it('a filter narrows to matching rows only, case-insensitively', async () => {
    await renderLog([
      entry({ id: 1, actor: 'tom', action: 'entity.update', target: 'nas' }),
      entry({ id: 2, actor: 'router', action: 'push', target: 'ip-address-table' }),
    ])

    const actorFilter = screen.getByLabelText('Filter by actor')
    await fireEvent.input(actorFilter, { target: { value: 'ROU' } })
    flushSync()

    expect(actorTexts()).toEqual(['router'])
    expect(screen.queryByText('tom')).toBeNull()
  })

  it('says plainly when nothing matches the filters, rather than an empty table', async () => {
    await renderLog([entry({ actor: 'tom' })])

    await fireEvent.input(screen.getByLabelText('Filter by actor'), { target: { value: 'nobody' } })
    flushSync()

    expect(screen.getByText('No entries match these filters.')).toBeTruthy()
  })
})
