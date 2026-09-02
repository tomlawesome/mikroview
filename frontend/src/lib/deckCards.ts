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
// views, so deep links to any of them land on it. Watchlist needs the
// user tier (#653's edit gate); audit stays admin-only throughout
// (#490's grammar: absent for a lower tier, never disabled).
//
// Entities and Settings joined the deck as its last two cards in #647
// (#634 round 23): "combined fleet/entities followed by settings the
// final page" -- seven cards for an admin, and, since #653 widened
// GET /api/entities to the user tier, seven for a user too. #657 then
// ruled Settings and Entities out of a viewer's navigation entirely --
// a page whose purpose is making a change is noise to someone who
// cannot -- so canEdit gates both cards now rather than isAdmin gating
// only Entities. Fleet's routers table folded into Entities' leading
// section under #647, but #657's ratified matrix keeps Fleet itself
// viewer-visible ("a stale router is why the log looks wrong"), so a
// viewer gets the standalone Fleet card (frontend/src/components/
// Fleet.svelte, otherwise reachable only from the phone-width bottom
// bar) in place of the two cards they cannot use.
export function deckCards(isAdmin: boolean, canEdit: boolean = isAdmin): DeckCard[] {
  const docketViews: View[] = ['flags']
  if (canEdit) docketViews.push('watchlist')
  if (isAdmin) docketViews.push('audit')
  const cards: DeckCard[] = [
    { key: 'fall', name: 'The fall', views: ['fall'] },
    { key: 'topography', name: 'Topography', views: ['topography'] },
    { key: 'metrics', name: 'Metrics', views: ['metrics'] },
    { key: 'live', name: 'Stream', views: ['live'] },
    { key: 'docket', name: 'The docket', views: docketViews },
  ]
  if (canEdit) {
    // Entities answers for the `fleet` view too. #647 folded Fleet's
    // routers table into Entities' leading section, but the phone-width
    // bottom bar still offers a Fleet row to every tier (navGroups.ts's
    // row carries no `edit` gate), and that row sets view 'fleet'
    // directly (BottomBar.svelte:174). Without this, an admin or user
    // tapping it leaves activeIndex at -1 and the deck draws no card at
    // all -- see #785. A viewer is unaffected: they get the standalone
    // `fleet` card below instead. views[0] stays 'entities', so the
    // LANDING_BY_CARD contract is unchanged.
    cards.push({ key: 'entities', name: 'Entities', views: ['entities', 'fleet'] })
    cards.push({ key: 'engineroom', name: 'Settings', views: ['engineroom'] })
  } else {
    cards.push({ key: 'fleet', name: 'Fleet', views: ['fleet'] })
  }
  return cards
}

// Where signing in lands per first card. Always a role-safe view: cards
// are reordered whole, and every card's first view is visible to every
// role (the docket's is flags, never its higher-tier tabs).
export const LANDING_BY_CARD: Record<string, View> = {
  fall: 'fall',
  topography: 'topography',
  metrics: 'metrics',
  live: 'live',
  docket: 'flags',
  entities: 'entities',
  engineroom: 'engineroom',
  fleet: 'fleet',
}
