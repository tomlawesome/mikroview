// SPDX-License-Identifier: AGPL-3.0-only

import { lookupPort, type PortInfo } from './commonPorts'
import { appState } from './state.svelte'

interface Anchor {
  port: number
  x: number
  y: number
}

// Drives the single PortLookupPopover instance mounted at the app root
// (see App.svelte) -- same singleton-plus-trigger-button shape as
// lib/ipLookup.svelte.ts, minus the async loading/error states, since this
// is a synchronous local lookup rather than a network call.
class PortLookupState {
  anchor = $state<Anchor | null>(null)
  results = $state<PortInfo[]>([])

  open(port: number, rect: DOMRect) {
  // Hold the stream while this is open (#413's "the stream holds while
  // you edit", stated once for every row-anchored surface). Newest-at-top
  // pushes rows down as events arrive, and a popover anchored to a row
  // that keeps moving is hostile. Guarded on anchor so re-opening for a
  // different token does not take a second hold it will never release.
    if (this.anchor === null) appState.holdStream()
    this.anchor = { port, x: rect.left, y: rect.bottom }
    this.results = lookupPort(port) ?? []
  }

  close() {
    if (this.anchor === null) return
    this.anchor = null
    appState.releaseStream()
  }
}

export const portLookupState = new PortLookupState()
