// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck's card table (#616/#633), shared by Deck.svelte (which
// renders the cards) and the Settings shelf (which shows them in the
// order you keep them). One table so the two can never disagree about
// what the deck holds.
import type { View } from './state.svelte'

export interface DeckCard {
  key: string
  name: string
  views: View[]
}

// The ratified default order. A card can answer for several views: the
// docket is one card whose tabs are the flags, watchlist and audit
// views, so deep links to any of them land on it. Watchlist and audit
// are admin-only throughout (#490's grammar: absent for viewers, never
// disabled).
export function deckCards(admin: boolean): DeckCard[] {
  return [
    { key: 'fall', name: 'The fall', views: ['fall'] },
    { key: 'metrics', name: 'Metrics', views: ['metrics'] },
    { key: 'live', name: 'Stream', views: ['live'] },
    { key: 'docket', name: 'The docket', views: admin ? ['flags', 'watchlist', 'audit'] : ['flags'] },
  ]
}

// Where signing in lands per first card. Always a role-safe view: cards
// are reordered whole, and every card's first view is visible to every
// role (the docket's is flags, never its admin-only tabs).
export const LANDING_BY_CARD: Record<string, View> = {
  fall: 'fall',
  metrics: 'metrics',
  live: 'live',
  docket: 'flags',
}
