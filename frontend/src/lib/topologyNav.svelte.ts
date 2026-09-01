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

  // #761's own handoffs, same shape as the two above: the docket's
  // `+ watch` button lives in Docket.svelte, outside Watchlist.svelte's
  // component boundary, and a flag's `watch this pathway`/`watch this
  // source` lives in Flags.svelte -- neither can reach into Watchlist's
  // own private draft state directly.
  //
  // Both are consumed instantly and reset to null, same idiom as
  // pendingFlagId/pendingWatchId above -- and for a closely related
  // reason: Docket.svelte fully unmounts and remounts Watchlist.svelte on
  // every tab switch (`{#if tab === 'watchlist'} <Watchlist />`), so a
  // slot left non-null after being acted on would silently reopen the
  // draft the next time anything (a later `+ watch`-free visit included)
  // brings that component back to life. pendingNewWatch is a fresh object
  // each request (not a bare boolean already at its target value) so two
  // `+ watch` clicks in a row are each seen as a distinct request rather
  // than the second being a no-op against an unchanged `true`.
  pendingNewWatch = $state<object | null>(null)
  pendingWatchDraft = $state<PendingWatchDraft | null>(null)

  requestHost(zoneId: string, host: string, ip: string) {
    this.pendingDescend = { zoneId, host, ip }
  }

  requestFlag(id: string) {
    this.pendingFlagId = id
  }

  requestWatch(id: string) {
    this.pendingWatchId = id
  }

  requestNewWatch() {
    this.pendingNewWatch = {}
  }

  requestWatchDraft(fill: PendingWatchDraft) {
    this.pendingWatchDraft = fill
  }
}

// PendingWatchDraft is what a flag's `watch this pathway`/`watch this
// source` action (#761 item 3) hands the watchlist tab to pre-fill the
// draft with -- `toward` is only ever set for `mode: 'expect'`, since a
// `fence`-mode draft's toward greys out to "wherever it goes" (the
// backend always creates an inverted entry observing, never scoped to a
// single destination up front).
export interface PendingWatchDraft {
  who: string
  toward?: string
  mode: 'expect' | 'fence'
}

export const topologyNavState = new TopologyNavState()
