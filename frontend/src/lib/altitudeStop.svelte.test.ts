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

import { altitudeStopState } from './altitudeStop.svelte'
import { preferencesState } from './preferences.svelte'

function reset() {
  altitudeStopState.stop = 'city'
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('altitude stop preference (#869, #1283)', () => {
  it('defaults to city', () => {
    expect(altitudeStopState.stop).toBe('city')
  })

  it('sets a stop and persists it through the shared record', () => {
    altitudeStopState.set('zones')

    expect(altitudeStopState.stop).toBe('zones')
    expect(preferencesState.get('altitudeStop')).toBe('zones')
  })

  it('honours a valid stored label, exercised via a fresh module load', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ altitudeStop: 'services' })
    const fresh = await import('./altitudeStop.svelte')

    expect(fresh.altitudeStopState.stop).toBe('services')
  })

  it('falls back to the default for a label that is not one of ALTITUDE_LABELS', async () => {
    for (const bad of ['not-a-real-stop', '', 42, null]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ altitudeStop: bad })
      const fresh = await import('./altitudeStop.svelte')

      expect(fresh.altitudeStopState.stop, JSON.stringify(bad)).toBe('city')
    }
  })

  it('treats a missing key the same as the default', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({})
    const fresh = await import('./altitudeStop.svelte')

    expect(fresh.altitudeStopState.stop).toBe('city')
  })
})
