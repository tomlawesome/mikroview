// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283 moved this off its own localStorage key onto the shared
// preferences record, but left its load/sanitize path with no test of
// its own -- unlike columns.svelte.ts (columns.svelte.test.ts), the one
// migrated module that already covers this shape. Same pattern here.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchMyPreferences: vi.fn().mockResolvedValue({ version: 1, prefs: {} }),
  saveMyPreferences: vi.fn().mockResolvedValue(null),
}))

import { topTalkerWidgetsState } from './topTalkers.svelte'
import { preferencesState } from './preferences.svelte'
import { emptyFilters } from './types'

function reset() {
  topTalkerWidgetsState.widgets = []
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('top talker widgets (#1283)', () => {
  it('defaults to none saved', () => {
    expect(topTalkerWidgetsState.widgets).toEqual([])
  })

  it('adds a widget and persists it through the shared record', () => {
    topTalkerWidgetsState.add('busy sources', 'srcIp', emptyFilters())

    expect(topTalkerWidgetsState.widgets.map((w) => w.title)).toEqual(['busy sources'])
    expect(preferencesState.get('topTalkers')).toEqual(topTalkerWidgetsState.widgets)
  })

  it('merges a stored widget over emptyFilters() and keeps its id, exercised via a fresh module load', async () => {
    const stored = [{ id: 'tt-1', title: 'old widget', groupBy: 'dstIp', filters: { device: 'router1' } }]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ topTalkers: stored })
    const fresh = await import('./topTalkers.svelte')
    const freshTypes = await import('./types')

    expect(fresh.topTalkerWidgetsState.widgets).toEqual([
      { id: 'tt-1', title: 'old widget', groupBy: 'dstIp', filters: { ...freshTypes.emptyFilters(), device: 'router1' } },
    ])
  })

  it('falls back to the default groupBy for a widget saved before it had one', async () => {
    const stored = [{ id: 'tt-1', title: 'legacy widget', filters: {} }]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ topTalkers: stored })
    const fresh = await import('./topTalkers.svelte')

    expect(fresh.topTalkerWidgetsState.widgets[0].groupBy).toBe('srcIp')
  })

  it('drops a malformed entry (no id, or no title) rather than trusting it', async () => {
    const stored = [
      { id: 'tt-kept', title: 'kept', filters: {} },
      { title: 'no id', filters: {} },
      { id: 'tt-no-title', filters: {} },
    ]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ topTalkers: stored })
    const fresh = await import('./topTalkers.svelte')

    expect(fresh.topTalkerWidgetsState.widgets.map((w) => w.id)).toEqual(['tt-kept'])
  })

  it('falls back to an empty list for anything that is not an array', async () => {
    for (const bad of [null, {}, 'not-an-array', 42]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ topTalkers: bad })
      const fresh = await import('./topTalkers.svelte')

      expect(fresh.topTalkerWidgetsState.widgets, JSON.stringify(bad)).toEqual([])
    }
  })
})
