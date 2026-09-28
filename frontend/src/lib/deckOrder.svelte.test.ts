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

import { deckOrderState } from './deckOrder.svelte'
import { preferencesState } from './preferences.svelte'

// The ratified default order, captured once before any test mutates the
// shared singleton -- deckOrder.svelte.ts keeps it private, so this is
// the same "read it back off the freshly-constructed state" approach
// used wherever a module doesn't export its own default.
const DEFAULT_ORDER = [...deckOrderState.order]

function reset() {
  deckOrderState.order = [...DEFAULT_ORDER]
  preferencesState.reset()
}

beforeEach(reset)
afterEach(reset)

describe('deck order preference (#633, #1283)', () => {
  it('defaults to the ratified order', () => {
    expect(deckOrderState.order).toEqual(DEFAULT_ORDER)
  })

  it('moves a card and persists the new order through the shared record', () => {
    deckOrderState.move('metrics', 'fall')

    expect(deckOrderState.order[0]).toBe('metrics')
    expect(preferencesState.get('deckOrder')).toEqual(deckOrderState.order)
  })

  it('drops an unknown key and appends any missing card, exercised via a fresh module load', async () => {
    const stored = ['made-up-card', ...DEFAULT_ORDER.slice(1)]

    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({ deckOrder: stored })
    const fresh = await import('./deckOrder.svelte')

    expect(fresh.deckOrderState.order).not.toContain('made-up-card')
    // The card missing from the stored value (DEFAULT_ORDER[0]) is
    // appended rather than lost.
    expect(fresh.deckOrderState.order).toContain(DEFAULT_ORDER[0])
  })

  it('falls back to the default order for anything that is not a string array', async () => {
    for (const bad of ['fall,metrics', [1, 2, 3], null, {}]) {
      vi.resetModules()
      const freshPrefs = await import('./preferences.svelte')
      freshPrefs.preferencesState.seedForTest({ deckOrder: bad })
      const fresh = await import('./deckOrder.svelte')

      expect(fresh.deckOrderState.order, JSON.stringify(bad)).toEqual(DEFAULT_ORDER)
    }
  })

  it('treats a missing key the same as the default order', async () => {
    vi.resetModules()
    const freshPrefs = await import('./preferences.svelte')
    freshPrefs.preferencesState.seedForTest({})
    const fresh = await import('./deckOrder.svelte')

    expect(fresh.deckOrderState.order).toEqual(DEFAULT_ORDER)
  })
})
