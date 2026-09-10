// SPDX-License-Identifier: AGPL-3.0-only
//
// #1015: the component-level tests lib/ingestLossBanners.test.ts cannot
// reach on its own -- what actually lands in the DOM, the handle's
// open/hidden toggle, the reopen rule wired to real reactive state, and
// Clear all's call chain. Row selection and the reopen rule's own logic
// are unit tested there without a component harness; this file only
// proves the component wires that logic up correctly.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  clearIngestLoss: vi.fn(async () => {}),
  fetchDevices: vi.fn(async () => []),
  fetchStats: vi.fn(async () => ({})),
}))

import { clearIngestLoss, fetchDevices, fetchStats } from '../lib/api'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { EMPTY_WS_DROPPED_EPISODE } from '../lib/ingestLossBanners'
import type { SyslogIngestLoss, SyslogListenerStats } from '../lib/types'
import IngestLossDrawer from './IngestLossDrawer.svelte'

function loss(overrides: Partial<SyslogIngestLoss> = {}): SyslogIngestLoss {
  return {
    dropped: { recent: 0, lastAt: null, active: false },
    rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
    rejected: { recent: 0, lastAt: null, active: false },
    oversized: { recent: 0, lastAt: null, active: false },
    ...overrides,
  }
}

function statsWithLoss(l: SyslogIngestLoss): { syslog: SyslogListenerStats } {
  return {
    syslog: {
      inUse: 0,
      capacity: 256,
      reservedForConfigured: 0,
      rejected: 0,
      rejectedConfigured: 0,
      dropped: 0,
      oversized: 0,
      rejectedConfiguredHosts: [],
      oversizedHost: '',
      loss: l,
    },
  }
}

const ALL_FIVE = loss({
  dropped: { recent: 812, lastAt: '2026-09-06T19:49:15Z', active: true },
  rejectedConfigured: { recent: 43, lastAt: '2026-09-06T19:49:02Z', active: true, hosts: ['branch-e4a1'] },
  rejected: { recent: 2867, lastAt: '2026-09-06T19:49:00Z', active: true },
  oversized: { recent: 7, lastAt: '2026-09-06T19:48:40Z', active: true, host: '10.20.3.9' },
})

beforeEach(() => {
  appState.stats = null
  appState.wsDroppedEpisode = EMPTY_WS_DROPPED_EPISODE
  appState.wsDropped = 0
  appState.now = Date.now()
  authState.state = 'authenticated'
  authState.role = 'user'
  vi.mocked(clearIngestLoss).mockClear()
  vi.mocked(fetchDevices).mockClear()
  vi.mocked(fetchStats).mockClear()
})

afterEach(() => {
  cleanup()
})

describe('IngestLossDrawer', () => {
  it('renders nothing when every row is inactive -- healthy is silent', () => {
    render(IngestLossDrawer)
    flushSync()
    expect(document.getElementById('ingest-loss-drawer')).toBeNull()
  })

  it('renders one row, open, at a single active signal', () => {
    appState.stats = statsWithLoss(loss({ oversized: ALL_FIVE.oversized })) as never
    render(IngestLossDrawer)
    flushSync()

    expect(screen.getAllByRole('status')).toHaveLength(1)
    const handle = screen.getByTestId('ingest-loss-handle')
    expect(handle.getAttribute('aria-expanded')).toBe('true')
    expect(handle.getAttribute('aria-controls')).toBe('ingest-loss-drawer')
    expect(handle.title).toBe('Hide ingest-loss banners')
  })

  it('renders all five rows, worst severity first, when every signal is active', () => {
    appState.stats = statsWithLoss(ALL_FIVE) as never
    appState.wsDroppedEpisode = { total: 156, recent: 156, lastAt: appState.now }
    render(IngestLossDrawer)
    flushSync()

    const rows = screen.getAllByRole('status')
    expect(rows).toHaveLength(5)
    expect(rows.map((r) => r.className)).toEqual([
      expect.stringContaining('banner-critical'),
      expect.stringContaining('banner-warn'),
      expect.stringContaining('banner-caution'),
      expect.stringContaining('banner-caution'),
      expect.stringContaining('banner-info'),
    ])
  })

  it('opens on mount whenever a row is active, with no click needed', () => {
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped })) as never
    render(IngestLossDrawer)
    flushSync()
    expect(screen.getByTestId('ingest-loss-handle').getAttribute('aria-expanded')).toBe('true')
  })

  it('hides on handle click: rows disappear from the accessible tree state, sill collapses', async () => {
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped })) as never
    render(IngestLossDrawer)
    flushSync()

    const handle = screen.getByTestId('ingest-loss-handle')
    await fireEvent.click(handle)
    flushSync()

    expect(handle.getAttribute('aria-expanded')).toBe('false')
    expect(handle.title).toBe('Show ingest-loss banners (1)')
    expect(document.getElementById('ingest-loss-drawer')?.className).toContain('closed')
  })

  it('reopens when a kind outside the hidden-at set appears -- the reopen rule', async () => {
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped })) as never
    render(IngestLossDrawer)
    flushSync()

    await fireEvent.click(screen.getByTestId('ingest-loss-handle'))
    flushSync()
    expect(screen.getByTestId('ingest-loss-handle').getAttribute('aria-expanded')).toBe('false')

    // A second, previously-unseen kind goes active -- as if the next
    // poll landed a new counter moving.
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped, oversized: ALL_FIVE.oversized })) as never
    flushSync()

    expect(screen.getByTestId('ingest-loss-handle').getAttribute('aria-expanded')).toBe('true')
  })

  it('does not reopen when the same kind that was hidden is still the only one active', async () => {
    appState.stats = statsWithLoss(loss({ dropped: { recent: 5, lastAt: '2026-09-06T19:00:00Z', active: true } })) as never
    render(IngestLossDrawer)
    flushSync()

    await fireEvent.click(screen.getByTestId('ingest-loss-handle'))
    flushSync()

    // The same row's numbers move (recent grows) but it is still the
    // same kind -- not a newcomer, so it stays hidden.
    appState.stats = statsWithLoss(loss({ dropped: { recent: 9, lastAt: '2026-09-06T19:01:00Z', active: true } })) as never
    flushSync()

    expect(screen.getByTestId('ingest-loss-handle').getAttribute('aria-expanded')).toBe('false')
  })

  it('offers Clear all to a non-viewer, and it clears the server, wsDropped, and refreshes', async () => {
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped })) as never
    appState.wsDropped = 42
    appState.wsDroppedEpisode = { total: 42, recent: 42, lastAt: appState.now }
    render(IngestLossDrawer)
    flushSync()

    const clearAll = screen.getByTestId('ingest-loss-clear-all')
    await fireEvent.click(clearAll)
    await vi.waitFor(() => {
      expect(clearIngestLoss).toHaveBeenCalledTimes(1)
      expect(fetchDevices).toHaveBeenCalledTimes(1)
      expect(fetchStats).toHaveBeenCalledTimes(1)
    })
    expect(appState.wsDropped).toBe(0)
    expect(appState.wsDroppedEpisode.recent).toBe(0)
  })

  it('offers no Clear all and no `details` link to a viewer', () => {
    authState.role = 'viewer'
    appState.stats = statsWithLoss(loss({ dropped: ALL_FIVE.dropped })) as never
    render(IngestLossDrawer)
    flushSync()

    expect(screen.queryByTestId('ingest-loss-clear-all')).toBeNull()
    expect(document.querySelector('.banner .detail')).toBeNull()
  })

  it('gives every real-loss row a `details` control for a non-viewer, and wsDropped none', () => {
    appState.stats = statsWithLoss(ALL_FIVE) as never
    appState.wsDroppedEpisode = { total: 1, recent: 1, lastAt: appState.now }
    render(IngestLossDrawer)
    flushSync()

    // Four real-loss rows carry `details`; wsDropped (the fifth, info-
    // tier row) does not.
    expect(document.querySelectorAll('.banner .detail')).toHaveLength(4)
  })
})
