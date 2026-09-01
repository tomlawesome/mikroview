// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck's card table (#647, #634 round 23; regated three ways by
// #657). Entities and Settings joined as the deck's last two cards --
// seven for the user/admin tiers, who can act on both (deckCards'
// `canEdit` gate, not a bare admin one: #653 already widened GET
// /api/entities to the user tier). A viewer gets six cards instead,
// Fleet standing in for both -- #657's ratified matrix keeps Fleet
// viewer-visible while ruling Entities and Settings out of a viewer's
// navigation entirely. Covers the contract Deck.svelte, App.svelte's
// DECK_VIEWS, EngineRoom.svelte's shelf and deckOrder.svelte.ts's
// normalize() all lean on.

import { describe, expect, it } from 'vitest'
import { deckCards, LANDING_BY_CARD } from './deckCards'

describe('deckCards', () => {
  it('carries seven cards for an admin, Entities and Settings last', () => {
    const keys = deckCards(true).map((c) => c.key)
    expect(keys).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'entities', 'engineroom'])
  })

  it('carries the same seven cards for a user -- #653 widened Entities to that tier', () => {
    const keys = deckCards(false, true).map((c) => c.key)
    expect(keys).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'entities', 'engineroom'])
  })

  it('carries six cards for a viewer -- Fleet stands in for Entities and Settings, neither of which appears at all', () => {
    const keys = deckCards(false, false).map((c) => c.key)
    expect(keys).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'fleet'])
  })

  it("the docket's own views widen with the tier: flags only for a viewer, plus watchlist for a user, plus audit for an admin", () => {
    const docketViews = (isAdmin: boolean, canEdit: boolean) =>
      deckCards(isAdmin, canEdit).find((c) => c.key === 'docket')?.views
    expect(docketViews(false, false)).toEqual(['flags'])
    expect(docketViews(false, true)).toEqual(['flags', 'watchlist'])
    expect(docketViews(true, true)).toEqual(['flags', 'watchlist', 'audit'])
  })

  it('names the merged Entities card "Entities", never "Fleet"', () => {
    const entities = deckCards(true).find((c) => c.key === 'entities')
    expect(entities?.name).toBe('Entities')
  })

  it('names the settings card "Settings"', () => {
    const settings = deckCards(true).find((c) => c.key === 'engineroom')
    expect(settings?.name).toBe('Settings')
  })

  it('every card key has a role-safe landing view, for every tier', () => {
    for (const [isAdmin, canEdit] of [
      [true, true],
      [false, true],
      [false, false],
    ] as const) {
      for (const card of deckCards(isAdmin, canEdit)) {
        expect(LANDING_BY_CARD[card.key]).toBe(card.views[0])
      }
    }
  })
})
