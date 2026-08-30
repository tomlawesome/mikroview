// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck's card table (#647, #634 round 23): Entities and Settings
// joined as the deck's last two cards -- seven for an admin, six for a
// viewer (Entities keeps its own admin gate; its GET route still 403s
// for anyone else). Covers the contract Deck.svelte, EngineRoom.svelte's
// shelf and deckOrder.svelte.ts's normalize() all lean on.

import { describe, expect, it } from 'vitest'
import { deckCards, LANDING_BY_CARD } from './deckCards'

describe('deckCards', () => {
  it("carries seven cards for an admin, Entities and Settings last", () => {
    const keys = deckCards(true).map((c) => c.key)
    expect(keys).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'entities', 'engineroom'])
  })

  it('carries six cards for a viewer -- no Entities card at all, not a disabled one', () => {
    const keys = deckCards(false).map((c) => c.key)
    expect(keys).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'engineroom'])
  })

  it('names the merged Entities card "Entities", never "Fleet"', () => {
    const entities = deckCards(true).find((c) => c.key === 'entities')
    expect(entities?.name).toBe('Entities')
  })

  it('names the settings card "Settings"', () => {
    const settings = deckCards(true).find((c) => c.key === 'engineroom')
    expect(settings?.name).toBe('Settings')
  })

  it('every card key has a role-safe landing view', () => {
    for (const card of deckCards(true)) {
      expect(LANDING_BY_CARD[card.key]).toBe(card.views[0])
    }
  })
})
