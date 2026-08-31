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
    action: 'entity.upsert',
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

    // Round 30 renames the sortable "actor" column head to "Who" (#s7's
    // when|who|what table) -- see AuditLog.svelte's SortKey.
    await fireEvent.click(screen.getByRole('columnheader', { name: /Who/ }))
    flushSync()
    expect(actorTexts()).toEqual(['alice', 'bea'])

    await fireEvent.click(screen.getByRole('columnheader', { name: /Who/ }))
    flushSync()
    expect(actorTexts()).toEqual(['bea', 'alice'])
  })

  it('a filter narrows to matching rows only, case-insensitively', async () => {
    await renderLog([
      entry({ id: 1, actor: 'tom', action: 'entity.upsert', target: 'nas' }),
      entry({ id: 2, actor: 'router', action: 'ingest.routeros', target: 'ip-address-table' }),
    ])

    const actorFilter = screen.getByLabelText('Filter by actor')
    await fireEvent.input(actorFilter, { target: { value: 'ROU' } })
    flushSync()

    expect(actorTexts()).toEqual(['router'])
    expect(screen.queryByText('tom')).toBeNull()
  })

  it('the what filter matches the composed sentence, not a raw action string', async () => {
    await renderLog([
      entry({ id: 1, actor: 'tom', action: 'entity.upsert', target: 'nas' }),
      entry({ id: 2, actor: 'tom', action: 'token.revoke', target: 'ci-deploy' }),
    ])

    await fireEvent.input(screen.getByLabelText('Filter by what'), { target: { value: 'revoked' } })
    flushSync()

    expect(actorTexts()).toEqual(['tom'])
    expect(document.querySelector('tbody')?.textContent).toContain('revoked API token')
    expect(document.querySelector('tbody')?.textContent).not.toContain('updated entity')
  })

  it('says plainly when nothing matches the filters, rather than an empty table', async () => {
    await renderLog([entry({ actor: 'tom' })])

    await fireEvent.input(screen.getByLabelText('Filter by actor'), { target: { value: 'nobody' } })
    flushSync()

    expect(screen.getByText('No entries match these filters.')).toBeTruthy()
  })
})

// Round 30 (docs/design/concepts/round-30/the-whole.html #s7) draws three
// columns -- when | who | what -- where "what" is one human-readable
// sentence composed from action/target/detail, not those raw fields
// spread across their own columns.
describe('AuditLog composes a three-column, human-readable table (round 30)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    appState.now = new Date('2026-08-30T12:00:00Z').getTime()
  })

  function headerTexts() {
    return Array.from(document.querySelectorAll('thead tr:first-child th')).map((el) =>
      el.textContent?.replace(/\s+/g, ' ').trim(),
    )
  }

  it('draws exactly three columns: When, Who, What', async () => {
    await renderLog([entry()])
    expect(headerTexts()).toEqual(['When ▼', 'Who', 'What'])
  })

  it('composes a plain sentence for a known action, not the raw action string', async () => {
    await renderLog([
      entry({
        actor: 'tom',
        action: 'flag.clear',
        target: 'port_scan:198.51.100.77',
        detail: 'expected, speed test',
      }),
    ])

    const cell = document.querySelector('tbody td:nth-child(3)')
    expect(cell?.textContent).toContain('cleared flag')
    expect(cell?.textContent).toContain('PORT SCAN · 198.51.100.77')
    expect(cell?.textContent).toContain('expected, speed test')
    // The raw action string never appears verbatim in the row.
    expect(cell?.textContent).not.toContain('flag.clear')
  })

  it('falls back to a readable phrase, not a raw dump, for an action it does not recognize', async () => {
    await renderLog([entry({ actor: 'tom', action: 'widget.reticulate', target: 'sprocket-9', detail: '' })])

    const cell = document.querySelector('tbody td:nth-child(3)')
    // Humanized (dots/underscores become spaces), not the literal
    // dotted action string, and the target is not silently dropped.
    expect(cell?.textContent).toContain('widget reticulate')
    expect(cell?.textContent).toContain('sprocket-9')
  })

  it('shows an absolute clock time for a same-day entry, not a uniform relative one', async () => {
    await renderLog([entry({ timestamp: '2026-08-30T11:47:00Z' })])
    const when = document.querySelector('tbody td:first-child')
    expect(when?.textContent?.trim()).toMatch(/^\d{2}:\d{2}$/)
    expect(when?.textContent).not.toContain('ago')
  })

  it('shows "yesterday" for an entry from the previous calendar day', async () => {
    await renderLog([entry({ timestamp: '2026-08-29T09:00:00Z' })])
    const when = document.querySelector('tbody td:first-child')
    expect(when?.textContent?.trim()).toBe('yesterday')
  })

  it('draws no explanatory paragraph above the table (round 30 apparatus removal)', async () => {
    await renderLog([entry()])
    expect(document.querySelector('.intro')).toBeNull()
    expect(screen.queryByText(/Every admin-privileged mutation/)).toBeNull()
  })
})
