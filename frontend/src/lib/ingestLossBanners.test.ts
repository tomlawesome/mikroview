// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  EMPTY_WS_DROPPED_EPISODE,
  noteWsDropped,
  selectIngestLossRows,
  shouldReopen,
  toIngestLossInputs,
  wsDroppedActive,
  WS_DROPPED_WINDOW_MS,
  type IngestLossBannerId,
  type IngestLossRowInputs,
  type WsDroppedEpisode,
} from './ingestLossBanners'
import type { SyslogIngestLoss } from './types'

function inactive(): SyslogIngestLoss {
  return {
    dropped: { recent: 0, lastAt: null, active: false },
    rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
    rejected: { recent: 0, lastAt: null, active: false },
    oversized: { recent: 0, lastAt: null, active: false },
  }
}

function rowInputs(overrides: Partial<SyslogIngestLoss> = {}, wsDropped = { recent: 0, active: false }): IngestLossRowInputs {
  return { loss: { ...inactive(), ...overrides }, wsDropped }
}

describe('selectIngestLossRows', () => {
  it('returns nothing when loss is absent -- stats not loaded yet, or an older server', () => {
    expect(selectIngestLossRows({ wsDropped: { recent: 0, active: false } })).toEqual([])
  })

  it('returns nothing when every counter is inactive, however large its total was', () => {
    expect(selectIngestLossRows(rowInputs())).toEqual([])
  })

  it('never shows a row for an inactive counter even if recent is somehow nonzero', () => {
    // active is the gate, not recent -- a stale recent value from a
    // counter that has since gone quiet must not resurrect its row.
    const rows = selectIngestLossRows(
      rowInputs({ dropped: { recent: 812, lastAt: '2026-09-06T19:00:00Z', active: false } }),
    )
    expect(rows).toEqual([])
  })

  it('shows exactly the active rows, worst severity first, regardless of input order', () => {
    const rows = selectIngestLossRows(
      rowInputs(
        {
          dropped: { recent: 812, lastAt: '2026-09-06T19:49:15Z', active: true },
          rejectedConfigured: {
            recent: 43,
            lastAt: '2026-09-06T19:49:02Z',
            active: true,
            hosts: ['branch-e4a1'],
          },
          rejected: { recent: 2867, lastAt: '2026-09-06T19:49:00Z', active: true },
          oversized: { recent: 7, lastAt: '2026-09-06T19:48:40Z', active: true, host: '10.20.3.9' },
        },
        { recent: 156, active: true },
      ),
    )
    expect(rows.map((r) => r.id)).toEqual([
      'dropped',
      'rejectedConfigured',
      'rejectedUndeclared',
      'oversized',
      'wsDropped',
    ])
    expect(rows.map((r) => r.severity)).toEqual(['critical', 'warn', 'caution', 'caution', 'info'])
  })

  it('renders the ratified copy using `recent`, never a total', () => {
    const rows = selectIngestLossRows(
      rowInputs({
        dropped: { recent: 12, lastAt: '2026-09-06T19:49:15Z', active: true },
        rejectedConfigured: {
          recent: 3,
          lastAt: '2026-09-06T19:49:02Z',
          active: true,
          hosts: ['branch-e4a1'],
        },
        rejected: { recent: 1, lastAt: '2026-09-06T19:49:00Z', active: true },
        oversized: { recent: 7, lastAt: '2026-09-06T19:48:40Z', active: true, host: '10.20.3.9' },
      }),
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId.dropped.detail).toBe('12 log lines lost')
    expect(byId.rejectedConfigured.detail).toBe('3 refused (branch-e4a1 locked out)')
    expect(byId.rejectedUndeclared.detail).toBe('1 connection refused')
    expect(byId.oversized.detail).toBe('7 oversized messages received from 10.20.3.9 were truncated')
  })

  it('omits the locked-out router name when no host is retained', () => {
    const [row] = selectIngestLossRows(
      rowInputs({ rejectedConfigured: { recent: 43, lastAt: '2026-09-06T19:49:02Z', active: true, hosts: [] } }),
    )
    expect(row.detail).toBe('43 refused')
  })

  it('omits "received from" when no oversized sender host is known yet', () => {
    const [row] = selectIngestLossRows(
      rowInputs({ oversized: { recent: 7, lastAt: '2026-09-06T19:48:40Z', active: true } }),
    )
    expect(row.detail).toBe('7 oversized messages received were truncated')
  })

  it('gives wsDropped no `details` target, unlike every real-loss row', () => {
    const rows = selectIngestLossRows(rowInputs({}, { recent: 156, active: true }))
    expect(rows).toHaveLength(1)
    expect(rows[0].id).toBe('wsDropped')
    expect(rows[0].details).toBeUndefined()
    expect(rows[0].detail).toBe('156 events not shown (but still logged)')
  })

  it('demotes the feed-drop notice: wsDropped is info, never warn', () => {
    const [row] = selectIngestLossRows(rowInputs({}, { recent: 5, active: true }))
    expect(row.severity).toBe('info')
  })
})

describe('shouldReopen -- the reopen rule', () => {
  it('does not reopen when every active kind was already seen at hide time', () => {
    const hiddenAt = new Set<IngestLossBannerId>(['dropped', 'oversized'])
    expect(shouldReopen(hiddenAt, ['dropped'])).toBe(false)
    expect(shouldReopen(hiddenAt, ['dropped', 'oversized'])).toBe(false)
  })

  it('reopens when a kind outside the hidden-at set is active -- new, or come back', () => {
    const hiddenAt = new Set<IngestLossBannerId>(['dropped'])
    expect(shouldReopen(hiddenAt, ['dropped', 'wsDropped'])).toBe(true)
  })

  it('does not reopen with no active kinds at all', () => {
    expect(shouldReopen(new Set(['dropped']), [])).toBe(false)
  })

  it('reopens from an empty hidden-at set the moment anything is active', () => {
    expect(shouldReopen(new Set(), ['oversized'])).toBe(true)
  })
})

describe('wsDropped episode tracking (noteWsDropped/wsDroppedActive)', () => {
  it('starts empty and inactive', () => {
    expect(wsDroppedActive(EMPTY_WS_DROPPED_EPISODE, Date.now())).toBe(false)
  })

  it('opens a fresh episode on the first increase', () => {
    const episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    expect(episode).toEqual({ total: 5, recent: 5, lastAt: 1_000 })
    expect(wsDroppedActive(episode, 1_000)).toBe(true)
  })

  it('accumulates within the window rather than restarting', () => {
    let episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    episode = noteWsDropped(episode, 8, 1_000 + WS_DROPPED_WINDOW_MS - 1)
    expect(episode.recent).toBe(8) // 5 + 3, still one episode
    expect(episode.total).toBe(8)
  })

  it('restarts the episode once the gap since the last move exceeds the window', () => {
    let episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    episode = noteWsDropped(episode, 8, 1_000 + WS_DROPPED_WINDOW_MS + 1)
    expect(episode.recent).toBe(3) // only this move's delta, not 5 + 3
  })

  it('falls inactive once now moves past lastAt + window, with no new message', () => {
    const episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    expect(wsDroppedActive(episode, 1_000 + WS_DROPPED_WINDOW_MS)).toBe(true)
    expect(wsDroppedActive(episode, 1_000 + WS_DROPPED_WINDOW_MS + 1)).toBe(false)
  })

  it('an unchanged total leaves the episode untouched', () => {
    const episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    expect(noteWsDropped(episode, 5, 50_000)).toBe(episode)
  })

  it('a total that goes backwards (a new WS connection) starts a clean episode', () => {
    const episode = noteWsDropped(EMPTY_WS_DROPPED_EPISODE, 5, 1_000)
    const reset: WsDroppedEpisode = noteWsDropped(episode, 0, 2_000)
    expect(reset).toEqual({ total: 0, recent: 0, lastAt: null })
  })
})

describe('toIngestLossInputs -- the totals-based path EngineRoom.svelte still uses', () => {
  it('derives the undeclared-only count from the two backend totals', () => {
    const result = toIngestLossInputs(
      {
        rejected: 2910,
        rejectedConfigured: 43,
        rejectedConfiguredHosts: ['branch-e4a1'],
        dropped: 812,
        oversized: 7,
        oversizedHost: '10.20.3.9',
      },
      156,
    )
    expect(result.rejectedUndeclared).toBe(2867)
    expect(result.rejectedConfigured).toBe(43)
    expect(result.wsDropped).toBe(156)
  })

  it('never goes negative if the two backend totals are inconsistent', () => {
    const result = toIngestLossInputs(
      {
        rejected: 5,
        rejectedConfigured: 9,
        rejectedConfiguredHosts: [],
        dropped: 0,
        oversized: 0,
        oversizedHost: '',
      },
      0,
    )
    expect(result.rejectedUndeclared).toBe(0)
  })
})
