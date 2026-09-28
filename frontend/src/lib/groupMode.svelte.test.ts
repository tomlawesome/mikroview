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

import { groupModeState } from './groupMode.svelte'
import { preferencesState } from './preferences.svelte'

function reset() {
  groupModeState.enabled = false
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('group mode preference (#341, #1283)', () => {
  it('defaults to off', () => {
    expect(groupModeState.enabled).toBe(false)
  })

  it('toggles and persists through the shared record', () => {
    groupModeState.toggle()

    expect(groupModeState.enabled).toBe(true)
    expect(preferencesState.get('groupMode')).toBe(true)
  })

  it('honours a stored true, exercised via a fresh module load', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ groupMode: true })
    const fresh = await import('./groupMode.svelte')

    expect(fresh.groupModeState.enabled).toBe(true)
  })

  it('treats anything but a literal true as off, including the legacy "1" string', async () => {
    for (const bad of ['1', 1, 'true', {}, null]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ groupMode: bad })
      const fresh = await import('./groupMode.svelte')

      expect(fresh.groupModeState.enabled, JSON.stringify(bad)).toBe(false)
    }
  })

  it('treats a missing key the same as off', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({})
    const fresh = await import('./groupMode.svelte')

    expect(fresh.groupModeState.enabled).toBe(false)
  })
})
