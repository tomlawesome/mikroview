import { lookupPort, type PortInfo } from './commonPorts'

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
    this.anchor = { port, x: rect.left, y: rect.bottom }
    this.results = lookupPort(port) ?? []
  }

  close() {
    this.anchor = null
  }
}

export const portLookupState = new PortLookupState()
