// SPDX-License-Identifier: AGPL-3.0-only
//
// The topography's own way in to Tune logging (#435 decision 2): a dark
// connection in the coverage lens is the thing prompting the page, so
// clicking one hands off which device and boundary-pair key it was
// clicked on. Same idiom as lib/topologyNav.svelte.ts's pendingDescend
// -- a module-lifetime slot one component fills in and another reads and
// clears on arrival, since appState.view is a bare string with no route
// param to carry a target across the switch. This one runs the opposite
// direction (out of the topography, not into it), which is why it is a
// module of its own rather than a field added to that one.
export interface PendingTuneLogging {
  device: string
  /** PolicyEdge.key form, `${from}|${to}` -- the dark pair that was clicked. */
  boundaryKey: string
}

class TuneLoggingNavState {
  pending = $state<PendingTuneLogging | null>(null)

  request(device: string, boundaryKey: string) {
    this.pending = { device, boundaryKey }
  }

  /** Reads and clears in one call, so a second arrival without a fresh
   * request sees nothing left over from the first. */
  consume(): PendingTuneLogging | null {
    const p = this.pending
    this.pending = null
    return p
  }
}

export const tuneLoggingNavState = new TuneLoggingNavState()
