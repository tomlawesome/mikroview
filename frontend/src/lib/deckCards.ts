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
//
// Entities and Settings joined the deck as its last two cards in #647
// (#634 round 23): "combined fleet/entities followed by settings the
// final page" -- seven cards for an admin. Entities keeps its own
// admin gate (its GET route still 403s for a viewer, per
// navGroups.ts's own comment on the same point) so its card is absent
// rather than present-and-broken; Settings/engineroom stays present for
// every role, unchanged from before (it was already viewer-readable).
export function deckCards(admin: boolean): DeckCard[] {
  const cards: DeckCard[] = [
    { key: 'fall', name: 'The fall', views: ['fall'] },
    { key: 'topography', name: 'Topography', views: ['topography'] },
    { key: 'metrics', name: 'Metrics', views: ['metrics'] },
    { key: 'live', name: 'Stream', views: ['live'] },
    { key: 'docket', name: 'The docket', views: admin ? ['flags', 'watchlist', 'audit'] : ['flags'] },
  ]
  if (admin) cards.push({ key: 'entities', name: 'Entities', views: ['entities'] })
  cards.push({ key: 'engineroom', name: 'Settings', views: ['engineroom'] })
  return cards
}

// Where signing in lands per first card. Always a role-safe view: cards
// are reordered whole, and every card's first view is visible to every
// role (the docket's is flags, never its admin-only tabs).
export const LANDING_BY_CARD: Record<string, View> = {
  fall: 'fall',
  topography: 'topography',
  metrics: 'metrics',
  live: 'live',
  docket: 'flags',
  entities: 'entities',
  engineroom: 'engineroom',
}
