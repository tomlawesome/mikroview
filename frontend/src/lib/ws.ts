// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from './state.svelte'
import type { FirewallEvent } from './types'

const MIN_BACKOFF_MS = 250
const MAX_BACKOFF_MS = 5000

interface WSEnvelope {
  type: string
  events?: FirewallEvent[]
  dropped?: number
  change?: ServerChange
}

/**
 * ServerChange names something on the server that a screen may now be
 * showing a stale answer about (internal/hub's Change).
 *
 * A name and nothing else, deliberately: the listener refetches through
 * the ordinary API, so this socket never becomes a second, partial copy
 * of an endpoint's answer that could drift from it.
 */
export type ServerChange = 'router-state' | 'definitions'

// LiveSocket is the client side of the live-tail feed: connects to
// /api/ws, applies exponential backoff with jitter on disconnect, and
// forwards every batched frame straight into appState (unfiltered — the
// server doesn't filter the WS stream either, see internal/api/ws.go).
export class LiveSocket {
  private socket: WebSocket | null = null
  private backoff = MIN_BACKOFF_MS
  private stopped = true
  // Which connection attempt each handler belongs to.
  //
  // WebSocket.close() fires onclose asynchronously, and App.svelte
  // calls disconnect() then connect() synchronously on an auth-state
  // change. A fast logout->login (a password manager filling the form,
  // say) therefore has the new socket already open by the time the old
  // one's onclose arrives -- and that handler unconditionally reported
  // 'closed' over a live connection and scheduled a reconnect the new
  // socket then abandoned. Every other place in this codebase with the
  // same staleness problem already carries a counter like this
  // (ipLookup, routerLookup, ruleMatcher's seq/id); ws.ts was the
  // exception.
  private generation = 0
  // Listeners for change notices. A subscriber list rather than a direct
  // call into the store that cares: which screens act on a change, and
  // whether the signed-in role may even read what they would refetch, is
  // App.svelte's decision -- this file's job is to deliver the notice.
  private changeListeners = new Set<(change: ServerChange) => void>()

  /**
   * onChange registers fn for every change notice the server sends, and
   * returns the function that unregisters it. Call that on teardown: a
   * listener left behind would keep refetching for a session that has
   * ended.
   */
  onChange(fn: (change: ServerChange) => void): () => void {
    this.changeListeners.add(fn)
    return () => this.changeListeners.delete(fn)
  }

  connect() {
    this.stopped = false
    this.open()
  }

  disconnect() {
    this.stopped = true
    this.socket?.close()
  }

  private open() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/api/ws`
    appState.connState = 'connecting'

    const gen = ++this.generation
    const ws = new WebSocket(url)
    this.socket = ws
    // True only while this attempt is still the current one. Checked in
    // every handler below rather than only in onclose: a superseded
    // socket's onmessage would otherwise keep appending events and
    // resetting the drop counter after it had been replaced.
    const current = () => gen === this.generation

    ws.onopen = () => {
      if (!current()) return
      this.backoff = MIN_BACKOFF_MS
      appState.connState = 'open'
      // A new connection is a new server-side client registration, whose
      // dropped counter starts back at 0 -- see hub.go.
      appState.resetWsDropped()
    }

    ws.onmessage = (ev) => {
      if (!current()) return
      let msg: WSEnvelope
      try {
        msg = JSON.parse(ev.data)
      } catch {
        return
      }
      if (msg.type === 'events' && msg.events) {
        appState.appendLive(msg.events)
      }
      // A frame of its own, not a field on an event batch -- a quiet
      // network produces no batches at all, and an instance nothing is
      // currently logging is exactly when a pushed table matters most.
      if (msg.type === 'changed' && msg.change) {
        for (const fn of this.changeListeners) fn(msg.change)
      }
      if (typeof msg.dropped === 'number') {
        // Cumulative total for this connection, not a delta -- see
        // ws.go. noteWsDropped folds it into the #1015 freshness
        // episode as well as updating the total.
        appState.noteWsDropped(msg.dropped)
      }
    }

    ws.onclose = () => {
      if (!current()) return
      appState.connState = 'closed'
      if (this.stopped) return
      const delay = this.backoff + Math.random() * this.backoff * 0.2
      this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS)
      setTimeout(() => {
        if (!this.stopped) this.open()
      }, delay)
    }

    ws.onerror = () => {
      ws.close()
    }
  }
}

export const liveSocket = new LiveSocket()
