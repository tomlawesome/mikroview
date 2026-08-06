import { fetchDevices, fetchEvents, fetchStats } from './api'
import { MAX_CLIENT_EVENTS } from './constants'
import { isPublicIp } from './format'
import { retentionState } from './retention.svelte'
import {
  emptyFilters,
  type ClientEvent,
  type Device,
  type Filters,
  type FirewallEvent,
  type Stats,
} from './types'

function stamp(events: FirewallEvent[]): ClientEvent[] {
  const receivedAt = Date.now()
  return events.map((e) => ({ ...e, receivedAt }))
}

export type ConnState = 'connecting' | 'open' | 'closed'

// 'live' is the scrolling event table + filter bar; 'metrics' is the
// dashboard (see Dashboard.svelte); 'control-ports' is the SSH/Telnet/
// control-port tracking tab (see ControlPorts.svelte); 'flags' is the
// behavioral-flags review tab (see Flags.svelte); 'detectors' is the
// admin-only per-detector on/off + scope settings tab (see
// Detectors.svelte); 'entities' is the admin-only persisted host/rule
// label+tag management tab (see Entities.svelte, issue #107); 'fleet'
// (issue #98) is the multi-router-fleet health table (see Fleet.svelte)
// -- every known device, live/stale/never-seen status, last-seen, and
// event counts in one place, richer than the toolbar's always-on
// DeviceStatus dot-strip; 'audit' (issue #112) is the admin-only,
// read-only log of admin-privileged mutations (see AuditLog.svelte). A
// real (if minimal) view switch -- only one is ever mounted at a time --
// rather than a modal layered over the live table, which used to leave
// LiveTable running underneath.
export type View = 'live' | 'metrics' | 'control-ports' | 'flags' | 'detectors' | 'entities' | 'fleet' | 'audit'

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
  view = $state<View>('live')
  events = $state<ClientEvent[]>([])
  filters = $state<Filters>(emptyFilters())
  devices = $state<Device[]>([])
  stats = $state<Stats | null>(null)
  connState = $state<ConnState>('connecting')
  wsDropped = $state(0)
  paused = $state(false)
  pendingCount = $state(0)
  autoscroll = $state(true)

  // Updated periodically by App.svelte (see tick()) so the age-based cutoff
  // in filteredEvents actually re-evaluates over time, not just when the
  // buffer itself changes.
  now = $state(Date.now())

  private pendingBuffer: ClientEvent[] = []

  // WS batches land here (plain push, not a $state reassignment) and get
  // flushed into `events` on a fixed cadence by the interval started in the
  // constructor, rather than reassigning `events` once per batch frame --
  // appendLive can run as often as every ~50ms under sustained load, and
  // every reassignment of a $state array forces filteredEvents/
  // ageFilteredEvents to recompute their full scan over up to
  // MAX_CLIENT_EVENTS items. Batching the writes caps that recompute rate
  // to FLUSH_INTERVAL_MS regardless of WS traffic.
  private incomingBuffer: ClientEvent[] = []
  private static readonly FLUSH_INTERVAL_MS = 175

  constructor() {
    setInterval(() => this.flushIncoming(), AppState.FLUSH_INTERVAL_MS)
  }

  filteredEvents = $derived.by(() => this.filteredBy(this.filters))

  // The age-cutoff half of filteredBy's pipeline, exposed as its own
  // memoized derived (rather than a plain method) so ControlPorts.svelte
  // and every configured CustomTopTalkerCard widget -- both of which need
  // the display-duration-windowed buffer but can't express their own match
  // criteria as a Filters object, e.g. ControlPorts.svelte's "destination
  // port is any one of several configured control ports" OR-match, which
  // Filters.port (a single value) can't represent -- read the same cached
  // scan instead of each independently re-filtering up to MAX_CLIENT_EVENTS
  // items on every tick.
  ageFilteredEvents = $derived.by(() => {
    const cutoff =
      retentionState.maxAgeSeconds === null ? null : this.now - retentionState.maxAgeSeconds * 1000
    return cutoff === null ? this.events : this.events.filter((e) => e.receivedAt >= cutoff)
  })

  // Applies the same age-cutoff-then-filter pipeline as filteredEvents,
  // but against an arbitrary Filters object rather than appState.filters --
  // what lets a custom top-talkers widget (lib/topTalkers.svelte.ts) track
  // its own independent criteria regardless of whatever filter is
  // currently active in the live view's FilterBar.
  filteredBy(filters: Filters): FirewallEvent[] {
    return applyFilters(this.ageFilteredEvents, filters)
  }

  // ruleRegex is excluded here: it's a modifier on `rule`, not a filter of
  // its own, so toggling it on with an empty rule shouldn't count as an
  // active filter (it's a boolean, so `!== ''` would always be true).
  hasActiveFilters = $derived.by(() =>
    Object.entries(this.filters).some(([k, v]) => k !== 'ruleRegex' && v !== ''),
  )

  // Skipped while paused so the age-based display-duration cutoff in
  // filteredEvents freezes at the moment of pausing instead of continuing
  // to age out whatever's on screen -- otherwise a short "Last Xs" window
  // would keep shrinking the paused view out from under you, defeating
  // the point of pausing to look at something before it scrolls past.
  tick() {
    if (this.paused) return
    this.now = Date.now()
  }

  setInitialEvents(events: FirewallEvent[]) {
    this.events = stamp(events).slice(-MAX_CLIENT_EVENTS)
  }

  appendLive(newEvents: FirewallEvent[]) {
    if (newEvents.length === 0) return
    const stamped = stamp(newEvents)
    if (this.paused) {
      // Capped the same way as `events` below -- otherwise leaving the
      // view paused for a while grows this without bound.
      this.pendingBuffer = [...this.pendingBuffer, ...stamped].slice(-MAX_CLIENT_EVENTS)
      this.pendingCount = this.pendingBuffer.length
      return
    }
    // Buffered, not written straight to `events` -- see incomingBuffer's
    // doc comment above. flushIncoming (on its own interval) is what
    // actually lands these in `events`.
    this.incomingBuffer.push(...stamped)
  }

  // Runs on FLUSH_INTERVAL_MS regardless of WS traffic -- a no-op tick
  // when nothing arrived is cheap; skipping the reassignment entirely here
  // (rather than reassigning an unchanged `events` to itself) avoids
  // spuriously invalidating filteredEvents/ageFilteredEvents when the feed
  // is idle.
  private flushIncoming() {
    if (this.incomingBuffer.length === 0) return
    this.events = [...this.events, ...this.incomingBuffer].slice(-MAX_CLIENT_EVENTS)
    this.incomingBuffer = []
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
    this.incomingBuffer = []
  }

  resetFilters() {
    this.filters = emptyFilters()
  }

  // Sets a single filter field, used by click-to-filter cells in
  // EventRow.svelte. Reassigns the whole object (rather than mutating one
  // property) so it composes the same way resetFilters()/apply() do.
  setFilter<K extends keyof Filters>(key: K, value: Filters[K]) {
    this.filters = { ...this.filters, [key]: value }
  }

  async loadInitial() {
    // Uses whatever's already in this.filters -- App.svelte sets this from
    // the URL's query string (if present) before calling loadInitial(), so
    // a shared/bookmarked filtered link loads pre-filtered instead of
    // fetching everything and only filtering after the fact.
    const [eventsRes, devices, stats] = await Promise.all([
      fetchEvents({ ...this.filters, limit: 500 }),
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

export function applyFilters(events: FirewallEvent[], f: Filters): FirewallEvent[] {
  // Compiled once per applyFilters call, not once per event -- this
  // used to construct a new RegExp inside the per-event filter
  // callback below, recompiling the same pattern for every one of up
  // to 5000 events on every call (as often as every ~200ms under
  // load, see this file's batching comment above).
  let ruleRe: RegExp | null = null
  if (f.rule && f.ruleRegex) {
    try {
      ruleRe = new RegExp(f.rule, 'i')
    } catch {
      // invalid regex: no-op, treat as unfiltered -- same as before
    }
  }

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
    // An address that can't be classified (missing, or -- see isPublicIp's
    // own IPv4-only caveat -- IPv6) satisfies neither "internal" nor
    // "external", mirroring internal/store/query.go's scopeMatches: a
    // specific scope excludes rather than guesses.
    if (f.srcScope) {
      if (!e.srcIp) return false
      const external = isPublicIp(e.srcIp)
      if (f.srcScope === 'internal' && external) return false
      if (f.srcScope === 'external' && !external) return false
    }
    if (f.dstScope) {
      if (!e.dstIp) return false
      const external = isPublicIp(e.dstIp)
      if (f.dstScope === 'internal' && external) return false
      if (f.dstScope === 'external' && !external) return false
    }
    if (f.rule) {
      if (f.ruleRegex) {
        // An invalid pattern disables the filter (matches internal/store/
        // query.go's behavior) rather than throwing or hiding everything
        // -- the user is probably still mid-typing it. ruleRe is null in
        // exactly that case (see above), so every event falls through
        // unfiltered by rule, uniformly -- same behavior as each event
        // independently hitting the same construction error used to be.
        if (ruleRe && !ruleRe.test(e.ruleLabel) && !ruleRe.test(e.raw)) return false
      } else {
        const needle = f.rule.toLowerCase()
        if (!e.ruleLabel.toLowerCase().includes(needle) && !e.raw.toLowerCase().includes(needle)) return false
      }
    }
    return true
  })
}

export const appState = new AppState()
