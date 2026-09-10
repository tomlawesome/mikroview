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

  // #1069's own version of the same handoff: the decommission ghost's
  // card carries a `watchlist ▸` door (DecommissionCard.svelte) promising
  // the watch that outlives it, and the watchlist tab is the only place
  // that watch is ever drawn. Same one-shot slot, same reason -- the card
  // knows the watch's id and cannot reach into Watchlist.svelte's own
  // decommission-drawer state directly.
  pendingDecommissionWatchId = $state<string | null>(null)

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

  // #961: set the instant Watchlist's closeDraft (save or discard alike)
  // sends the operator back to the flags tab, so Flags.svelte's own
  // mount can tell that arrival apart from a fresh visit to the tab --
  // see flagsState.pinnedIds' doc comment for why the distinction
  // matters. One-shot like every slot above: Flags.svelte reads and
  // clears it on arrival, since Docket.svelte recreates the component on
  // every tab switch and a slot left set would wrongly survive into a
  // later, unrelated visit.
  pendingFlagsReturn = $state(false)

  // #1018's trace, the same one-shot slot as pendingDescend above and
  // for the identical reason: a stream row knows which event it is and
  // the map owns the drawing, and there is no route param to carry one
  // across the view switch. The row fills this in and flips
  // appState.view; Topography.svelte reads it and clears it on arrival,
  // whether that is its own mount or the instant the slot changes under
  // an already-mounted tab (Deck.svelte keeps visited cards alive).
  //
  // A fresh object per request, never a bare id already at its target
  // value, so tracing the same event twice in a row is seen as two
  // requests rather than the second being a no-op against an unchanged
  // value.
  pendingTrace = $state<PendingTrace | null>(null)

  requestHost(zoneId: string, host: string, ip: string) {
    this.pendingDescend = { zoneId, host, ip }
  }

  requestFlag(id: string) {
    this.pendingFlagId = id
  }

  requestWatch(id: string) {
    this.pendingWatchId = id
  }

  requestDecommissionWatch(id: string) {
    this.pendingDecommissionWatchId = id
  }

  requestNewWatch() {
    this.pendingNewWatch = {}
  }

  requestWatchDraft(fill: PendingWatchDraft) {
    this.pendingWatchDraft = fill
  }

  signalFlagsReturn() {
    this.pendingFlagsReturn = true
  }

  requestTrace(trace: PendingTrace) {
    this.pendingTrace = trace
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
  // provenance (#641) is where a prefilled draft's values came from,
  // stated in the form beside them: "from the last firing window, 6 of
  // at least 14 pairs", and whether the identity is MAC- or IP-bound.
  // Absent for a draft the operator opened themselves, which has no
  // provenance to state. See lib/watchDraft.ts for the wording.
  provenance?: string
  // returnTo (#641) is the view the operator came from, which they are
  // taken back to when the draft is saved or discarded -- taking a
  // watcher offered by a flag must never cost a manual switch back to
  // the inbox. Absent means stay where you are, which is right for
  // every caller already on the watchlist.
  returnTo?: 'flags'
}

// PendingTrace is how a caller outside the map names the line to trace
// (#1018). `event` is the id where the caller holds one -- a stream row
// -- and the pair/port form is for a caller that holds a rolled-up pair
// instead. Mirrors api.ts's TraceRequest, deliberately by hand rather
// than by import: this module is the handoff contract between two
// components and must not gain a dependency on the fetch layer.
export interface PendingTrace {
  event?: number
  in?: string
  out?: string
  port?: number
  proto?: string
  src?: string
  dst?: string
  noOut?: boolean
}

export const topologyNavState = new TopologyNavState()
