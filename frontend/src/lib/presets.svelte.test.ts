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

import { presetState } from './presets.svelte'
import { preferencesState } from './preferences.svelte'
import { emptyFilters } from './types'

function reset() {
  presetState.presets = []
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('filter presets (#1283)', () => {
  it('defaults to none saved', () => {
    expect(presetState.presets).toEqual([])
  })

  it('saves a preset and persists it through the shared record', () => {
    presetState.save('busy port', { ...emptyFilters(), port: '443' })

    expect(presetState.presets.map((p) => p.name)).toEqual(['busy port'])
    expect(preferencesState.get('presets')).toEqual(presetState.presets)
  })

  it('merges a stored preset over emptyFilters(), exercised via a fresh module load', async () => {
    // A preset saved before `ruleRegex` existed -- missing the field
    // entirely, not merely false.
    const stored = [{ name: 'old preset', filters: { device: 'router1' } }]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ presets: stored })
    const fresh = await import('./presets.svelte')
    const freshTypes = await import('./types')

    expect(fresh.presetState.presets).toEqual([
      { name: 'old preset', filters: { ...freshTypes.emptyFilters(), device: 'router1' } },
    ])
  })

  it('drops a malformed entry (no name, or no filters) rather than trusting it', async () => {
    const stored = [
      { name: 'kept', filters: {} },
      { filters: {} }, // no name
      { name: 'no filters' }, // no filters object
      'not even an object',
    ]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ presets: stored })
    const fresh = await import('./presets.svelte')

    expect(fresh.presetState.presets.map((p) => p.name)).toEqual(['kept'])
  })

  it('falls back to an empty list for anything that is not an array', async () => {
    for (const bad of [null, {}, 'not-an-array', 42]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ presets: bad })
      const fresh = await import('./presets.svelte')

      expect(fresh.presetState.presets, JSON.stringify(bad)).toEqual([])
    }
  })
})
