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

import { retentionState } from './retention.svelte'
import { preferencesState } from './preferences.svelte'

function reset() {
  retentionState.maxAgeSeconds = null
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('retention preference (#1283)', () => {
  it('defaults to no limit', () => {
    expect(retentionState.maxAgeSeconds).toBeNull()
  })

  it('sets a window and persists it through the shared record', () => {
    retentionState.set(300)

    expect(retentionState.maxAgeSeconds).toBe(300)
    expect(preferencesState.get('retention')).toBe(300)
  })

  it('sanitizes a stored value on load, exercised via a fresh module load', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ retention: '900' })
    const fresh = await import('./retention.svelte')

    // sanitize() runs stored values through Number(), so a numeric
    // string saved by an older client still passes through.
    expect(fresh.retentionState.maxAgeSeconds).toBe(900)
  })

  it('refuses a non-positive or non-numeric stored value, falling back to no limit', async () => {
    for (const bad of [0, -30, 'not-a-number', {}]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ retention: bad })
      const fresh = await import('./retention.svelte')

      expect(fresh.retentionState.maxAgeSeconds, JSON.stringify(bad)).toBeNull()
    }
  })

  it('treats a missing key the same as an explicit no-limit', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({})
    const fresh = await import('./retention.svelte')

    expect(fresh.retentionState.maxAgeSeconds).toBeNull()
  })
})
