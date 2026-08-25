// SPDX-License-Identifier: AGPL-3.0-only

import { fetchDevices, fetchEvents, fetchStats } from './api'
import { matchesAddressQuery, type AddressCandidate } from './addressMatch'
import { MAX_CLIENT_EVENTS } from './constants'
import { matchesCountry, UNKNOWN_COUNTRY } from './countryMatch'
import { countryFlag, isPublicIp } from './format'
import { matchesPortQuery } from './portMatch'
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

// The RouterOS chain vocabulary the Chain select always offers, regardless
// of whether one has been seen yet -- see chainOptions below and #438's
// "Chain" section (a select, not free text, because this is a small,
// closed-ish vocabulary). srcnat/dstnat spelled lower-case to match what
// RouterOS actually emits (internal/routeros/parser.go's isNATChain).
const BUILTIN_CHAINS = ['input', 'forward', 'output', 'srcnat', 'dstnat']

function stamp(events: FirewallEvent[]): ClientEvent[] {
  const receivedAt = Date.now()
  return events.map((e) => ({ ...e, receivedAt }))
}

export type ConnState = 'connecting' | 'open' | 'closed'

// 'live' is the scrolling event table + filter bar; 'metrics' is the
// metrics page (see Metrics.svelte); 'watchlist' (issue #243) is the
// admin-only watched-ports/watched-devices management tab (see
// Watchlist.svelte, successor to the old Control Ports tab) -- it also
// carries a Suggestions tab (#243 slice 5, merged in by #547) for
// watchlist entries suggested from data RouterOS has already pushed,
// since accepting/hiding a suggestion is a different workflow from
// managing an entry directly but belongs alongside it rather than on a
// route of its own; 'flags' is the behavioral-flags review tab (see
// Flags.svelte) -- it also carries an Exclusions tab (issue #207,
// merged in by #547) listing every permanently-excluded (detector,
// target) pair, with its own quiet, outlined count per
// docs/design/screens/navigation/DESIGN.md's badge rules; 'entities' is
// the admin-only persisted host/rule label+tag management tab (see
// Entities.svelte, issue #107); 'fleet' (issue #98) is the
// multi-router-fleet health table (see Fleet.svelte) -- every known
// device, live/stale/never-seen status, last-seen, and event counts in
// one place, richer than the toolbar's always-on DeviceStatus dot-strip;
// 'audit' (issue #112) is the admin-only, read-only log of
// admin-privileged mutations (see AuditLog.svelte); 'engineroom' (#490)
// is mikroview's own signal path drawn as a live vertical diagram, with
// every setting on the station it governs (see EngineRoom.svelte) --
// viewer-readable, unlike every other Admin-group view, per the design
// record's authz-matrix clause. It absorbed three former admin-only
// pages wholesale: 'users' and 'tokens' (#548's account/API-token
// management pages, successors to the UsersOverlay/TokensOverlay
// modals) and 'detectors' (the per-detector on/off + scope settings
// tab) -- all three retired with no alias once the engine room's doors
// and watchers station existed to replace them (see
// EngineRoomDoors.svelte/EngineRoomWatchers.svelte, which reuse their
// state modules and API calls unchanged). A real (if minimal) view
// switch -- only one is ever mounted at a time -- rather than a modal
// layered over the live table, which used to leave LiveTable running
// underneath.
//
// 'exclusions' and 'suggestions' are deliberately not views any more:
// #544 dropped their rail rows and #547 removed the routes themselves
// wholesale (no aliases) once the tabs above existed to replace them.
// 'setup' went the same way in #487: the guided wizard is a modal over
// the shell now (see SetupWizard.svelte), not a page to navigate to, so
// the route is gone rather than aliased or redirected -- "Run setup…"
// opens the modal from wherever the operator already is.
export type View =
  | 'live'
  | 'metrics'
  | 'watchlist'
  | 'flags'
  | 'entities'
  | 'fleet'
  | 'audit'
  | 'engineroom'

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

  // True when the most recent loadInitial()/refetchWithFilters() call
  // failed (server error, 503, dropped connection, etc.) rather than
  // succeeding with a real (possibly empty) result. Without this, a
  // failed request left `events` exactly as it was before the attempt --
  // stale, or empty on first load -- and LiveTable had no way to tell
  // that apart from a genuinely empty answer, so it asserted "No events
  // match the current filters" (or, on first load, sat on the ambiguous
  // "Waiting for events…") when the true state was "the query that would
  // have answered that never completed" (#373). Cleared on the next
  // successful call, whichever of the two runs it.
  fetchFailed = $state(false)

  // True once the app's one loadInitial() call (App.svelte's mount
  // effect) has settled, success or failure -- never cleared afterward.
  // #549's "Loading" chrome state (shell plus ghost rows, never a
  // spinner page) needs to tell "the first fetch just hasn't come back
  // yet" apart from "it came back and there is genuinely nothing" --
  // both look like an empty `events`/`devices` array, and only the first
  // one should render ghost rows. Deliberately not derived from
  // `fetchFailed` or from `events.length`/`devices.length`: neither
  // stays false-while-loading, true-once-settled on its own (fetchFailed
  // is false in both the "still loading" and "loaded, no error" cases;
  // an empty buffer is equally true before the fetch and after a
  // confirmed-empty one).
  initialLoadDone = $state(false)

  private matcher = new RuleMatcher()
  private ruleDebounce: ReturnType<typeof setTimeout> | null = null
  private matchedPattern = ''

  paused = $state(false)
  pendingCount = $state(0)
  autoscroll = $state(true)

  // Open row-anchored surfaces. Newest-at-top (#363) pushes rows *down*
  // as events arrive, so a popover anchored to a row it is about would
  // slide away from under itself; the decision taken once for #413,
  // #439's lookup popovers and #445's NAT popup is that opening any of
  // them holds the stream until it closes.
  //
  // A count rather than a flag because these surfaces are not
  // necessarily exclusive, and a boolean would let the first one to
  // close release a hold the second still needs.
  //
  // Deliberately separate from `autoscroll`: this is a transient hold,
  // not a change to the operator's Autoscroll preference, so the toggle's
  // own state and its button are untouched and the preference is exactly
  // as they left it when the surface closes. Where the view is already
  // frozen the hold composes as a no-op -- LiveTable freezes on either.
  //
  // The count is a plain field and only the boolean is reactive. That
  // split is load-bearing, not tidiness: holders take the hold from
  // inside an $effect (that is what makes the release survive an
  // unmount), and `count++` *reads* the count before writing it. Had the
  // count been $state, the read would have made the effect depend on a
  // signal it was itself changing, so it would re-run, increment again,
  // and re-run again -- Svelte aborts that with
  // effect_update_depth_exceeded, which does not just break the hold: it
  // stops the whole app re-rendering, so a popover sticks on "Loading…"
  // and Esc silently does nothing. Found by running it; nothing in the
  // type system or the test suite objects to the reactive version.
  // Writing the boolean is safe because assigning the value it already
  // holds notifies nobody.
  private holds = 0

  private heldOpen = $state(false)

  // True when the view must not move: either Autoscroll is off, or
  // something is holding it open. The composition lives here rather than
  // in LiveTable so every reader gets the same answer.
  get streamHeld(): boolean {
    return !this.autoscroll || this.heldOpen
  }

  holdStream() {
    this.holds++
    this.heldOpen = true
  }

  releaseStream() {
    if (this.holds > 0) this.holds--
    this.heldOpen = this.holds > 0
  }

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

  // How many row-anchored surfaces are open right now (issue #413's
  // "the stream holds while you edit", decided once for the inline
  // editor, the lookup popovers and #445's NAT popup alike).
  //
  // Newest-at-top (#363) pushes rows *down* as events arrive, so a
  // popover anchored to a row the stream is shoving around is hostile
  // to use. Opening one therefore takes the same freeze Autoscroll
  // already implements -- but transiently, without touching
  // `autoscroll` itself: the toggle is a stated preference, and
  // silently flipping it back and forth under the operator would leave
  // the button lying about what it will do next time.
  //
  // A count rather than a boolean because two surfaces can legitimately
  // overlap (a lookup popover open while the editor opens over it), and
  // the first one to close must not release a hold the second still
  // needs.

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

  // chainOptions backs the Chain select (#438): BUILTIN_CHAINS always,
  // plus anything else observed in the current buffer -- a custom chain
  // (RouterOS lets you name your own) appears the moment it's seen rather
  // than needing to be typed, since this is a select, not free text.
  // Off `events`, not `ageFilteredEvents` or `filteredEvents` -- the
  // option list should not shrink just because the display-duration
  // window or some other active filter currently hides a chain's rows.
  chainOptions = $derived.by(() => {
    const seen = new Set(BUILTIN_CHAINS)
    const extra: string[] = []
    for (const e of this.events) {
      if (e.chain && !seen.has(e.chain)) {
        seen.add(e.chain)
        extra.push(e.chain)
      }
    }
    extra.sort()
    return [...BUILTIN_CHAINS, ...extra]
  })

  srcCountryOptions = $derived.by(() => countryOptionsFor(this.events, 'src'))
  dstCountryOptions = $derived.by(() => countryOptionsFor(this.events, 'dst'))

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

  // relabel rewrites the friendly name already stamped onto every
  // buffered event for one raw value, so a rename taken in the live
  // view visibly takes on the rows that are on screen when it is saved
  // (#413: "a rename that does not visibly take reads as broken").
  //
  // Necessary because names are resolved once, at ingest, by
  // internal/naming -- events already in this buffer carry the name
  // that was true when they arrived and will never re-resolve on their
  // own. Events arriving *after* the save need nothing from this: the
  // server resolves them against the entity that now exists, for every
  // connected session rather than just the one that made the edit.
  //
  // Deliberately a one-shot rewrite rather than a standing client-side
  // overlay consulted at render. An overlay would keep applying to
  // events that arrive later, including any whose name RouterOS starts
  // supplying -- re-creating, on the client, exactly the shadowing this
  // issue exists to prevent, and this time in the direction the owner
  // ruled against. A rewrite of what is already here cannot outlive the
  // buffer it edited.
  //
  // `label` is '' for a removed label, which restores the raw value --
  // matching what the server will resolve for the next event, since the
  // entity is gone.
  relabel(type: 'host' | 'port' | 'rule', key: string, label: string) {
    const name = label || undefined
    const rewrite = (e: ClientEvent): ClientEvent => {
      switch (type) {
        case 'host': {
          const src = e.srcIp === key
          const dst = e.dstIp === key
          if (!src && !dst) return e
          return { ...e, ...(src && { srcHostName: name }), ...(dst && { dstHostName: name }) }
        }
        case 'port': {
          const src = e.srcPort !== undefined && String(e.srcPort) === key
          const dst = e.dstPort !== undefined && String(e.dstPort) === key
          if (!src && !dst) return e
          return { ...e, ...(src && { srcPortName: name }), ...(dst && { dstPortName: name }) }
        }
        case 'rule':
          return e.ruleLabel === key ? { ...e, ruleName: name } : e
      }
    }

    // Every buffer that can still reach the screen, not just `events`:
    // a rename made while paused, or in the moment between a websocket
    // batch landing and the next flush, would otherwise show the old
    // name on rows that appear seconds later. frozenPool matters most
    // of all -- with the stream held open for the editor (see
    // streamHolds), it is the pool the rows on screen are drawn from.
    this.events = this.events.map(rewrite)
    if (this.frozenPool !== null) this.frozenPool = this.frozenPool.map(rewrite)
    this.incomingBuffer = this.incomingBuffer.map(rewrite)
    this.pendingBuffer = this.pendingBuffer.map(rewrite)
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

  // Swaps the Source and Destination groups -- query, scope and country
  // together (#438's swap control: "clicked the wrong side" answered in
  // two clicks instead of retyping). Everything else in the bar is
  // untouched.
  swapSourceDestination() {
    const f = this.filters
    this.filters = {
      ...f,
      srcQuery: f.dstQuery,
      dstQuery: f.srcQuery,
      srcScope: f.dstScope,
      dstScope: f.srcScope,
      srcCountry: f.dstCountry,
      dstCountry: f.srcCountry,
    }
  }

  async loadInitial() {
    // Uses whatever's already in this.filters -- App.svelte sets this from
    // the URL's query string (if present) before calling loadInitial(), so
    // a shared/bookmarked filtered link loads pre-filtered instead of
    // fetching everything and only filtering after the fact.
    try {
      const [eventsRes, devices, stats] = await Promise.all([
        fetchEvents({ ...this.filters, limit: 500 }),
        fetchDevices(),
        fetchStats(),
      ])
      this.setInitialEvents(eventsRes.events)
      this.devices = devices
      this.stats = stats
      this.fetchFailed = false
    } catch (err) {
      // Left the buffer exactly as it was (empty, on first load) rather
      // than treating the rejection as "zero events" -- see fetchFailed's
      // doc comment. Rethrown so App.svelte's existing
      // .catch(handleApiError) still handles a 401 the same way it always
      // has; this only adds the on-screen honesty signal alongside that.
      this.fetchFailed = true
      throw err
    } finally {
      // Settled either way -- a failure still means the "still loading"
      // window is over, and the fetchFailed branch above is what tells
      // that apart from a confirmed-empty result from here on.
      this.initialLoadDone = true
    }
  }

  // Re-queries the server with the current filters and replaces `events`
  // with the result. See the class doc comment above for why this needs
  // to exist alongside client-side filtering, not instead of it.
  async refetchWithFilters() {
    try {
      const res = await fetchEvents({ ...this.filters, limit: 500 })
      this.setInitialEvents(res.events)
      this.fetchFailed = false
    } catch (err) {
      // Deliberately does not touch `events` -- the pre-refetch buffer is
      // left as-is (see the class doc comment: it is the "instant but
      // possibly incomplete" layer). fetchFailed is what stops that
      // untouched buffer from being read as a definite, complete answer.
      this.fetchFailed = true
      throw err
    }
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

// isNatSide reports which side (if either) a NAT annotation's translated
// address belongs to, mirroring internal/routeros/parser.go's
// isNATChain exactly: only the two dedicated NAT chains say which side
// was rewritten. A NAT annotation inherited onto a forward/input/output
// line by an earlier NAT rule (see that file's parseNAT doc comment) has
// no such chain to read the direction off, so it is left out of address
// matching entirely rather than guessed -- matching the wrong side would
// be worse than not matching it at all.
function natSide(e: FirewallEvent): 'src' | 'dst' | null {
  const chain = e.chain?.toLowerCase()
  if (chain === 'srcnat') return 'src'
  if (chain === 'dstnat') return 'dst'
  return null
}

// srcCandidates/dstCandidates feed matchesAddressQuery (lib/addressMatch.ts):
// the row's own address plus, for a srcnat/dstnat row, the NAT-translated
// counterpart on that side -- #438's NAT-parity section ("filtering on an
// internal host's address finds the dst-natted flows that reach it"). The
// NAT candidate carries no hostName: there's no resolved label for it (see
// EventRow.svelte's own comment on why the NAT token gets no copy glyph),
// so it only ever participates via its raw address text.
function srcCandidates(e: FirewallEvent): AddressCandidate[] {
  const c: AddressCandidate[] = [{ ip: e.srcIp, hostName: e.srcHostName }]
  if (e.natIp && natSide(e) === 'src') c.push({ ip: e.natIp })
  return c
}

function dstCandidates(e: FirewallEvent): AddressCandidate[] {
  const c: AddressCandidate[] = [{ ip: e.dstIp, hostName: e.dstHostName }]
  if (e.natIp && natSide(e) === 'dst') c.push({ ip: e.natIp })
  return c
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
    if (f.srcQuery && !matchesAddressQuery(f.srcQuery, srcCandidates(e))) return false
    if (f.dstQuery && !matchesAddressQuery(f.dstQuery, dstCandidates(e))) return false
    if (
      f.port &&
      !matchesPortQuery(f.port, [
        { port: e.srcPort, portName: e.srcPortName },
        { port: e.dstPort, portName: e.dstPortName },
      ])
    ) {
      return false
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
    if (f.srcCountry && !matchesCountry(!!e.srcIp, e.srcCountry, f.srcCountry)) return false
    if (f.dstCountry && !matchesCountry(!!e.dstIp, e.dstCountry, f.dstCountry)) return false
    if (f.rule) {
      if (f.ruleRegex) {
        // A null set means the pattern has not been evaluated yet, was
        // invalid, or was refused for taking too long. All three disable
        // the rule filter rather than throwing or hiding everything --
        // the user is usually mid-typing, and matching internal/store/
        // query.go's behaviour for an unusable pattern.
        if (ruleMatches && !ruleMatches.has(e.id)) return false
      } else {
        // ruleName (#413's operator alias) joins ruleLabel/raw as of
        // #438 -- regex mode is deliberately unchanged (see the issue's
        // own "Regex mode unchanged" note): the ruleMatcher Worker still
        // classifies off ruleLabel/raw only (toCandidate above).
        const needle = f.rule.toLowerCase()
        const haystacks = [e.ruleLabel, e.raw, e.ruleName ?? '']
        if (!haystacks.some((h) => h.toLowerCase().includes(needle))) return false
      }
    }
    return true
  })
}

// countryOptionsFor backs srcCountryOptions/dstCountryOptions: every
// country code observed on this side in the current buffer, plus an
// "Unknown" entry (lib/countryMatch.ts's UNKNOWN_COUNTRY) when at least
// one event has an address on this side but no resolved country --
// mirroring matchesCountry's own "has an address" rule, so the option
// only appears when it would actually select something.
function countryOptionsFor(
  events: readonly FirewallEvent[],
  side: 'src' | 'dst',
): { value: string; label: string }[] {
  const addrKey = side === 'src' ? 'srcIp' : 'dstIp'
  const countryKey = side === 'src' ? 'srcCountry' : 'dstCountry'
  const codes = new Set<string>()
  let hasUnknown = false
  for (const e of events) {
    const country = e[countryKey]
    if (country) {
      codes.add(country.toUpperCase())
    } else if (e[addrKey]) {
      hasUnknown = true
    }
  }
  const options = [...codes].sort().map((code) => ({ value: code, label: `${countryFlag(code)} ${code}`.trim() }))
  if (hasUnknown) options.push({ value: UNKNOWN_COUNTRY, label: 'Unknown' })
  return options
}

export const appState = new AppState()
