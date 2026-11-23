// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283 moved this off its own localStorage key onto the shared
// preferences record, but left its load/sanitize path with no test of
// its own -- unlike columns.svelte.ts (columns.svelte.test.ts), the one
// migrated module that already covers this shape. Same pattern here.
// The cursor's minute is deliberately not part of this: it is never
// persisted (see this module's own comment), so there is nothing to
// sanitize on load for it.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchMyPreferences: vi.fn().mockResolvedValue({ version: 1, prefs: {} }),
  saveMyPreferences: vi.fn().mockResolvedValue(null),
}))

import { metricsPref } from './metrics.svelte'
import { preferencesState } from './preferences.svelte'

function reset() {
  metricsPref.view = 'seismograph'
  metricsPref.minute = null
  metricsPref.announcement = ''
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('metrics view preference (#488, #1283)', () => {
  it('defaults to seismograph', () => {
    expect(metricsPref.view).toBe('seismograph')
  })

  it('sets a view and persists it through the shared record', () => {
    metricsPref.setView('table')

    expect(metricsPref.view).toBe('table')
    expect(preferencesState.get('metrics')).toBe('table')
  })

  it('honours a valid stored view, exercised via a fresh module load', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ metrics: 'register' })
    const fresh = await import('./metrics.svelte')

    expect(fresh.metricsPref.view).toBe('register')
  })

  it('falls back to seismograph for anything that is not a known view', async () => {
    for (const bad of ['chart', '', 1, null, {}]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ metrics: bad })
      const fresh = await import('./metrics.svelte')

      expect(fresh.metricsPref.view, JSON.stringify(bad)).toBe('seismograph')
    }
  })

  it('treats a missing key the same as the default', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({})
    const fresh = await import('./metrics.svelte')

    expect(fresh.metricsPref.view).toBe('seismograph')
  })
})
