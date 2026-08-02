import { appState } from './state.svelte'
import type { FirewallEvent } from './types'

const MIN_BACKOFF_MS = 250
const MAX_BACKOFF_MS = 5000

interface WSEnvelope {
  type: string
  events?: FirewallEvent[]
}

// LiveSocket is the client side of the live-tail feed: connects to
// /api/ws, applies exponential backoff with jitter on disconnect, and
// forwards every batched frame straight into appState (unfiltered — the
// server doesn't filter the WS stream either, see internal/api/ws.go).
export class LiveSocket {
  private socket: WebSocket | null = null
  private backoff = MIN_BACKOFF_MS
  private stopped = true

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

    const ws = new WebSocket(url)
    this.socket = ws

    ws.onopen = () => {
      this.backoff = MIN_BACKOFF_MS
      appState.connState = 'open'
    }

    ws.onmessage = (ev) => {
      let msg: WSEnvelope
      try {
        msg = JSON.parse(ev.data)
      } catch {
        return
      }
      if (msg.type === 'events' && msg.events) {
        appState.appendLive(msg.events)
      }
    }

    ws.onclose = () => {
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
