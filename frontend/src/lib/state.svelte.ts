// SPDX-License-Identifier: AGPL-3.0-only

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
import { mergeOutcome, RuleMatcher, type MatchCandidate } from './ruleMatcher'

function stamp(events: FirewallEvent[]): ClientEvent[] {
  const receivedAt = Date.now()
  return events.map((e) => ({ ...e, receivedAt }))
}

export type ConnState = 'connecting' | 'open' | 'closed'

// 'live' is the scrolling event table + filter bar; 'metrics' is the
// dashboard (see Dashboard.svelte); 'watchlist' (issue #243) is the
// admin-only watched-ports/watched-devices management tab (see
// Watchlist.svelte, successor to the old Control Ports tab); 'flags' is the
// behavioral-flags review tab (see Flags.svelte); 'detectors' is the
// admin-only per-detector on/off + scope settings tab (see
// Detectors.svelte); 'entities' is the admin-only persisted host/rule
// label+tag management tab (see Entities.svelte, issue #107); 'fleet'
// (issue #98) is the multi-router-fleet health table (see Fleet.svelte)
// -- every known device, live/stale/never-seen status, last-seen, and
// event counts in one place, richer than the toolbar's always-on
// DeviceStatus dot-strip; 'audit' (issue #112) is the admin-only,
// read-only log of admin-privileged mutations (see AuditLog.svelte);
// 'exclusions' (issue #207) is the admin-only page listing every
// permanently-excluded (detector, target) pair, split out of the bottom
// of Flags.svelte since reviewing exclusions underneath a list of
// hundreds of active flags was a pain. 'suggestions' (#243 slice 5) is
// the admin-only review page for watchlist entries suggested from data
// RouterOS has already pushed (see Suggestions.svelte) -- kept separate
// from Watchlist.svelte itself since accepting/hiding a suggestion is a
// different workflow from managing an entry directly. A real (if
// minimal) view switch -- only one is ever mounted at a time -- rather
// than a modal layered over the live table, which used to leave
// LiveTable running underneath.
export type View =
  | 'live'
  | 'metrics'
  | 'watchlist'
  | 'suggestions'
  | 'setup'
  | 'flags'
  | 'detectors'
  | 'entities'
  | 'fleet'
  | 'audit'
  | 'exclusions'

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
  // $state.raw, not $state: every write to this array replaces it whole
  // (setInitialEvents, appendUnseen and flushIncoming all reassign rather
  // than mutate), so the deep per-element proxy a plain $state would build
  // over 20,000 events buys no reactivity that is ever used and taxes
  // every read in the ageFiltered -> liveFiltered -> rendered chain, which
  // re-runs on each flush and on each 250 ms tick (#381).
  events = $state.raw<ClientEvent[]>([])
  filters = $state<Filters>(emptyFilters())
  devices = $state<Device[]>([])
  stats = $state<Stats | null>(null)
  connState = $state<ConnState>('connecting')
  wsDropped = $state(0)
  // ruleMatches holds the ids matching the current regex pattern, or
  // null when there is nothing usable to filter by. Kept here rather than
  // inside the Worker so eviction is handled where eviction already
  // happens -- see the slice(-MAX_CLIENT_EVENTS) call sites, which prune
  // this in the same breath -- and so a terminated Worker loses nothing.
  ruleMatches = $state<ReadonlySet<number> | null>(null)
  // ruleMatchStatus surfaces *why* the rule filter is inactive, so a
  // pattern that was refused for being too slow reads as that rather than
  // as "no results".
  ruleMatchStatus = $state<'idle' | 'evaluating' | 'invalid' | 'too-slow'>('idle')

  private matcher = new RuleMatcher()
  private ruleDebounce: ReturnType<typeof setTimeout> | null = null
  private matchedPattern = ''

  paused = $state(false)
  pendingCount = $state(0)
  autoscroll = $state(true)

  // The raw event pool captured the moment Autoscroll is switched off
  // (issue #232). Null means "not frozen"; cleared when Autoscroll goes
  // back on.
  //
  // Lives here rather than inside LiveTable because switching to another
  // view unmounts that component. Component-local state resets to null on
  // unmount, so returning to the live view would re-capture against the
  // now-current buffer and jump the view forward by everything that
  // arrived while you were away -- the exact symptom #232 reports, just
  // triggered by navigation instead of by new events.
  //
  // Deliberately the raw pool, not the rendered slice: LiveTable
  // re-applies the *current* filters to it, so narrowing and widening the
  // filter still work while frozen, but only ever within what was already
  // captured. An event that arrives after the freeze can never appear.
  // Raw for the same reason as `events` above: replaced whole, never
  // mutated in place.
  frozenPool = $state.raw<ClientEvent[] | null>(null)

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
  // memoized derived (rather than a plain method) so every configured
  // CustomTopTalkerCard widget -- which needs the display-duration-windowed
  // buffer but can't express its own match criteria as a Filters object --
  // reads the same cached scan instead of each independently re-filtering
  // up to MAX_CLIENT_EVENTS items on every tick. Watchlist.svelte does NOT
  // use this: its matches come from the server's own persisted match log
  // (internal/matchlog), not the client's volatile recent-events buffer.
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
    return applyFilters(this.ageFilteredEvents, filters, this.ruleMatches)
  }

  // ruleRegex is excluded here: it's a modifier on `rule`, not a filter of
  // its own, so toggling it on with an empty rule shouldn't count as an
  // active filter (it's a boolean, so `!== ''` would always be true).
  hasActiveFilters = $derived.by(() =>
    Object.entries(this.filters).some(([k, v]) => k !== 'ruleRegex' && v !== ''),
  )

  // syncRuleMatches is called whenever the rule filter changes.
  //
  // Debounced because the input is bound directly to filters.rule, so
  // every keystroke is a new pattern -- without this, typing "drop" would
  // classify the whole buffer four times.
  syncRuleMatches() {
    if (this.ruleDebounce) clearTimeout(this.ruleDebounce)
    if (!this.filters.rule || !this.filters.ruleRegex) {
      this.ruleMatches = null
      this.ruleMatchStatus = 'idle'
      this.matchedPattern = ''
      return
    }
    this.ruleMatchStatus = 'evaluating'
    this.ruleDebounce = setTimeout(() => void this.rematchAll(), RULE_MATCH_DEBOUNCE_MS)
  }

  /** Reclassifies the whole buffer against the current pattern. */
  private async rematchAll() {
    const pattern = this.filters.rule
    const outcome = await this.matcher.run(pattern, this.events.map(toCandidate))
    // The user kept typing while that was in flight.
    if (pattern !== this.filters.rule || !this.filters.ruleRegex) return
    this.matchedPattern = pattern
    const merged = mergeOutcome(outcome, null, false)
    this.ruleMatches = merged.matches
    this.ruleMatchStatus = merged.status
  }

  /**
   * Classifies just-arrived events, so a live view with a regex filter
   * keeps up without reclassifying everything already in the buffer.
   */
  private async classifyNew(arrived: ClientEvent[]) {
    if (!this.matchedPattern || this.matchedPattern !== this.filters.rule) return
    if (!this.ruleMatches || arrived.length === 0) return
    const pattern = this.matchedPattern
    const outcome = await this.matcher.run(pattern, arrived.map(toCandidate))
    if (pattern !== this.filters.rule) return
    const merged = mergeOutcome(outcome, this.ruleMatches, true)
    this.ruleMatches = merged.matches
    this.ruleMatchStatus = merged.status
  }

  /**
   * Drops ids for events the buffer has already evicted.
   *
   * Server-assigned ids are monotonic and eviction is always from the
   * front, so everything below the oldest surviving id is gone. Called
   * from the same places that slice the buffer, which is the whole reason
   * the set lives here rather than inside the Worker.
   */
  private pruneRuleMatches() {
    if (!this.ruleMatches || this.events.length === 0) return
    const oldest = this.events[0].id
    let dropped = false
    const next = new Set<number>()
    for (const id of this.ruleMatches) {
      if (id >= oldest) next.add(id)
      else dropped = true
    }
    if (dropped) this.ruleMatches = next
  }

  // Skipped while paused so the age-based display-duration cutoff in
  // filteredEvents freezes at the moment of pausing instead of continuing
  // to age out whatever's on screen -- otherwise a short "Last Xs" window
  // would keep shrinking the paused view out from under you, defeating
  // the point of pausing to look at something before it scrolls past.
  tick() {
    if (this.paused) return
    this.now = Date.now()
  }

  /**
   * Appends events the buffer has not already got, returning the ones
   * that were genuinely new.
   *
   * Needed because the initial `GET /api/events` fetch and the WebSocket
   * stream overlap: an event that arrives in both lands twice, and
   * `LiveTable`'s `{#each rendered as event (event.id)}` then has
   * duplicate keys. Svelte logs `each_key_duplicate` and keyed-each
   * behaviour is undefined from there -- the row renders twice and any
   * count taken off the buffer is inflated.
   *
   * Server ids are unique (verified against the API), so this is purely
   * about the two client-side entry points meeting in the middle. The
   * incoming batch is deduped against itself as well as against the
   * buffer, since a single flush can carry the same event twice for the
   * same reason.
   */
  private appendUnseen(incoming: ClientEvent[]): ClientEvent[] {
    if (incoming.length === 0) return []
    const seen = new Set(this.events.map((e) => e.id))
    const fresh = incoming.filter((e) => {
      if (seen.has(e.id)) return false
      seen.add(e.id)
      return true
    })
    if (fresh.length === 0) return []
    this.events = [...this.events, ...fresh].slice(-MAX_CLIENT_EVENTS)
    return fresh
  }

  setInitialEvents(events: FirewallEvent[]) {
    // Deduped even though this replaces the buffer outright: it is the
    // one place a fresh id set is established, and letting a duplicate
    // in here would seed every later comparison with it.
    const seen = new Set<FirewallEvent['id']>()
    this.events = stamp(events)
      .filter((e) => (seen.has(e.id) ? false : (seen.add(e.id), true)))
      .slice(-MAX_CLIENT_EVENTS)
    this.syncRuleMatches()
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
    const arrived = this.appendUnseen(this.incomingBuffer)
    this.incomingBuffer = []
    if (arrived.length === 0) return
    this.pruneRuleMatches()
    // Only the genuinely new ones: reclassifying a duplicate costs a
    // Worker round-trip to reach the answer already held for it.
    void this.classifyNew(arrived)
  }

  togglePause() {
    this.paused = !this.paused
    if (!this.paused && this.pendingBuffer.length) {
      // The third insert path, and the easiest to forget: a pause that
      // spans a reconnect can hold events the refreshed buffer already
      // has.
      const resumed = this.appendUnseen(this.pendingBuffer)
      this.pendingBuffer = []
      this.pendingCount = 0
      if (resumed.length === 0) return
      this.pruneRuleMatches()
      void this.classifyNew(resumed)
    }
  }

  clearBuffer() {
    this.events = []
    this.pendingBuffer = []
    this.pendingCount = 0
    this.incomingBuffer = []
    // Release the #232 freeze snapshot too, or Clear is a no-op on screen
    // whenever autoscroll is off: the buffer empties and the table keeps
    // rendering the frozen pool, with nothing to explain why and no way
    // out but toggling autoscroll back on (#381).
    //
    // Nulling it is enough on its own. LiveTable's freeze effect reads
    // frozenPool inside the branch it takes while autoscroll is false, so
    // this write re-triggers that effect, which re-enters the same branch,
    // finds no pool, and captures a fresh (now empty) one.
    this.frozenPool = null
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

// applyFilters runs no regex.
//
// When the rule filter is in regex mode it consults ruleMatches -- a set
// of event ids computed off the main thread (see lib/ruleMatcher.ts and
// issue #157). A null set means "not evaluated yet, or the pattern was
// refused", and behaves exactly as an invalid pattern always has: the
// rule filter is skipped rather than throwing or hiding everything.
//
// This is also less work than it replaces. The old path ran the pattern
// against ruleLabel *and* raw for every event on every call -- up to
// 10,000 regex executions across a 5,000-event buffer, repeated per
// top-talker widget. This is a set lookup.
/** RULE_MATCH_DEBOUNCE_MS: the filter input is bound per keystroke. */
const RULE_MATCH_DEBOUNCE_MS = 250

function toCandidate(e: FirewallEvent): MatchCandidate {
  return { id: e.id, ruleLabel: e.ruleLabel, raw: e.raw }
}

export function applyFilters(
  events: FirewallEvent[],
  f: Filters,
  ruleMatches: ReadonlySet<number> | null = null,
): FirewallEvent[] {

  return events.filter((e) => {
    if (f.device && e.deviceId !== f.device) return false
    if (f.action && e.action !== f.action) return false
    if (f.protocol && (e.protocol ?? '').toLowerCase() !== f.protocol.toLowerCase()) return false
    if (f.chain && e.chain !== f.chain) return false
    if (f.interface && e.inInterface !== f.interface && e.outInterface !== f.interface) return false
    if (f.ip && e.srcIp !== f.ip && e.dstIp !== f.ip) return false
    if (f.port) {
      const p = Number(f.port)
      // A non-numeric value (still mid-typing, or just unusable) is
      // NaN, and every `!==` comparison against NaN is true -- without
      // this guard every event would fail the filter and the live
      // table would read as "no traffic" until the field is cleared.
      // Skip the filter instead, matching the rule/ruleRegex convention
      // below for an unusable pattern.
      if (!Number.isNaN(p) && e.srcPort !== p && e.dstPort !== p) return false
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
        // A null set means the pattern has not been evaluated yet, was
        // invalid, or was refused for taking too long. All three disable
        // the rule filter rather than throwing or hiding everything --
        // the user is usually mid-typing, and matching internal/store/
        // query.go's behaviour for an unusable pattern.
        if (ruleMatches && !ruleMatches.has(e.id)) return false
      } else {
        const needle = f.rule.toLowerCase()
        if (!e.ruleLabel.toLowerCase().includes(needle) && !e.raw.toLowerCase().includes(needle)) return false
      }
    }
    return true
  })
}

export const appState = new AppState()
