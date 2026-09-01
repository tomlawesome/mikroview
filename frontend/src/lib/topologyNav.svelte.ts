// SPDX-License-Identifier: AGPL-3.0-only
//
// Cross-page "descend into this host" handoff (#678): the docket's
// ratified record says every named where is a link into the topography
// at its sensible level -- host-level when the target resolves to a
// known device, zone-level (the map itself) otherwise, never the
// stream. Topography.svelte's own reach (host-level zoom) is local
// component state with no route param to carry a target across the view
// switch (appState.view is a bare string), so this is the one shared
// slot a caller fills in before flipping to 'topography'; the scene
// reads and clears it on arrival.
export interface PendingDescend {
  zoneId: string
  host: string
  ip: string
}

class TopologyNavState {
  pendingDescend = $state<PendingDescend | null>(null)

  // #724's second click, the reverse handoff: a dial's quick-view panel
  // row knows which flag or watch it represents but can't reach into
  // Flags.svelte's/Watchlist.svelte's own private drawer state to open
  // it. Same fix as pendingDescend above, mirrored -- the row fills in
  // the slot and flips appState.view, and the destination tab reads and
  // clears it, whether that happens on its own arrival or (since these
  // tabs stay mounted once visited, per Deck.svelte's keep-alive cards)
  // the instant the slot changes under an already-mounted tab. Two
  // separate slots rather than one tagged union: the flags tab never
  // looks at pendingWatchId and vice versa, so nothing has to switch on
  // a kind field to know whether a change applies to it.
  pendingFlagId = $state<string | null>(null)
  pendingWatchId = $state<string | null>(null)

  requestHost(zoneId: string, host: string, ip: string) {
    this.pendingDescend = { zoneId, host, ip }
  }

  requestFlag(id: string) {
    this.pendingFlagId = id
  }

  requestWatch(id: string) {
    this.pendingWatchId = id
  }
}

export const topologyNavState = new TopologyNavState()
