import { fetchDevices, fetchEvents, fetchStats } from './api'
import { MAX_CLIENT_EVENTS } from './constants'
import { emptyFilters, type Device, type Filters, type FirewallEvent, type Stats } from './types'

export type ConnState = 'connecting' | 'open' | 'closed'

// Central reactive state for the live view. The WebSocket tail pushes
// every new event unfiltered into `events`; `filteredEvents` re-filters
// that buffer client-side on every render, which is what makes toggling a
// filter feel instant with no round-trip for events already in the
// buffer. But `events` itself only ever holds up to MAX_CLIENT_EVENTS
// recent items, so a filter matching something outside that window (an
// older event, a device that hasn't logged recently, etc.) would show
// nothing even though the server's much larger retained buffer has
// matches -- refetchWithFilters() (wired up to run on filter changes in
// App.svelte) re-queries the server with the active filters and replaces
// `events` with that server-filtered baseline, so the two layers
// together cover both "instant" and "actually complete" filtering.
class AppState {
  events = $state<FirewallEvent[]>([])
  filters = $state<Filters>(emptyFilters())
  devices = $state<Device[]>([])
  stats = $state<Stats | null>(null)
  connState = $state<ConnState>('connecting')
  paused = $state(false)
  pendingCount = $state(0)
  autoscroll = $state(true)

  private pendingBuffer: FirewallEvent[] = []

  filteredEvents = $derived.by(() => applyFilters(this.events, this.filters))

  hasActiveFilters = $derived.by(() => Object.values(this.filters).some((v) => v !== ''))

  setInitialEvents(events: FirewallEvent[]) {
    this.events = events.slice(-MAX_CLIENT_EVENTS)
  }

  appendLive(newEvents: FirewallEvent[]) {
    if (newEvents.length === 0) return
    if (this.paused) {
      // Capped the same way as `events` below -- otherwise leaving the
      // view paused for a while grows this without bound.
      this.pendingBuffer = [...this.pendingBuffer, ...newEvents].slice(-MAX_CLIENT_EVENTS)
      this.pendingCount = this.pendingBuffer.length
      return
    }
    this.events = [...this.events, ...newEvents].slice(-MAX_CLIENT_EVENTS)
  }

  togglePause() {
    this.paused = !this.paused
    if (!this.paused && this.pendingBuffer.length) {
      this.events = [...this.events, ...this.pendingBuffer].slice(-MAX_CLIENT_EVENTS)
      this.pendingBuffer = []
      this.pendingCount = 0
    }
  }

  clearBuffer() {
    this.events = []
    this.pendingBuffer = []
    this.pendingCount = 0
  }

  resetFilters() {
    this.filters = emptyFilters()
  }

  async loadInitial() {
    const [eventsRes, devices, stats] = await Promise.all([
      fetchEvents({ limit: 500 }),
      fetchDevices(),
      fetchStats(),
    ])
    this.setInitialEvents(eventsRes.events)
    this.devices = devices
    this.stats = stats
  }

  // Re-queries the server with the current filters and replaces `events`
  // with the result. See the class doc comment above for why this needs
  // to exist alongside client-side filtering, not instead of it.
  async refetchWithFilters() {
    const res = await fetchEvents({ ...this.filters, limit: 500 })
    this.setInitialEvents(res.events)
  }

  async refreshDevicesAndStats() {
    const [devices, stats] = await Promise.all([fetchDevices(), fetchStats()])
    this.devices = devices
    this.stats = stats
  }
}

function applyFilters(events: FirewallEvent[], f: Filters): FirewallEvent[] {
  return events.filter((e) => {
    if (f.device && e.deviceId !== f.device) return false
    if (f.action && e.action !== f.action) return false
    if (f.protocol && (e.protocol ?? '').toLowerCase() !== f.protocol.toLowerCase()) return false
    if (f.chain && e.chain !== f.chain) return false
    if (f.interface && e.inInterface !== f.interface && e.outInterface !== f.interface) return false
    if (f.ip && e.srcIp !== f.ip && e.dstIp !== f.ip) return false
    if (f.port) {
      const p = Number(f.port)
      if (e.srcPort !== p && e.dstPort !== p) return false
    }
    if (f.rule) {
      const needle = f.rule.toLowerCase()
      if (!e.ruleLabel.toLowerCase().includes(needle) && !e.raw.toLowerCase().includes(needle)) return false
    }
    return true
  })
}

export const appState = new AppState()
