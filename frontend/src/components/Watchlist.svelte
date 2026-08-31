<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Watchlist (#243): what Control Ports grew into. Two modes per entry:
  //
  //  - Non-inverted -- "record attempts against these ports," the same
  //    thing Control Ports did, generalised beyond SSH/Telnet and now
  //    persisted server-side (internal/matchlog) instead of only ever
  //    existing in the live view's own capped, volatile client buffer.
  //  - Inverted -- "this device should only ever reach these
  //    destinations." A new inverted entry starts Observing: it records
  //    what the device actually touches without raising anything, so you
  //    can review real evidence and promote what's expected before
  //    anything is treated as a violation.
  //
  // User tier and above throughout since #653, which is what unblocks
  // #641: judging a flag Expected and saving the expectation it drafts
  // now sit at the same tier, instead of the first being open and the
  // second 403ing. A viewer never reaches this page at all -- the nav
  // row carries `edit: true` (navGroups.ts) -- so the controls below
  // need no gate of their own, the same argument the tab comment makes.
  // The owner-level neighbours it used to sit beside, Audit and
  // Exclusions, stay admin.
  //
  // The "Watchlist" tab panel below carries two surfaces (#676): the
  // ratified round-29 docket table (watch · boundary · window · state ·
  // last event, drawers with a story/verbatim line/actions) at the top,
  // and this component's own pre-existing add/edit/invert/observe/
  // promote workflow underneath it, headed "Manage entries." The
  // ratified design describes only the former; #676 keeps the latter
  // reachable rather than removing it -- see the ratified table's own
  // script section, below, for what it could and couldn't honestly
  // carry from today's data.
  import { onMount, tick } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { suggestState } from '../lib/suggest.svelte'
  import { matchesState } from '../lib/matches.svelte'
  import { compareNumeric, compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import { formatRelative } from '../lib/format'
  import { nightlySummary, windowLabel } from '../lib/watchWindow'
  import TabList from './TabList.svelte'
  import Suggestions from './Suggestions.svelte'
  import MatchesTab from './MatchesTab.svelte'
  import type { WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from '../lib/types'

  onMount(() => {
    watchlistState.refresh()
    suggestState.refresh()
    // The ratified table's "last event" column and drawer (#676) read
    // matchesState's own bulk feed (GET /api/matches?entries=all) --
    // the same one the Matches tab already loads on arrival there. Loaded
    // here too so the column and drawer have an answer without requiring
    // a detour through that tab first.
    matchesState.load()
  })

  // Suggestions is a tab of Watchlist (#547) and Matches is a third
  // (#584), both per the ratified navigation record. No admin-gating
  // needed on the tabs themselves -- Watchlist only ever mounts for the
  // user tier or better in the first place (see navGroups.ts's
  // `edit: true` on the Watchlist row), and /api/suggestions* agrees
  // server-side
  // (internal/api/authz_matrix_test.go).
  //
  // Matches sits between the two: it is the evidence the entries beside
  // it produced, where Suggestions is a separate feed of entries that do
  // not exist yet.
  const tabs = [
    { id: 'watchlist', label: 'Watchlist' },
    { id: 'matches', label: 'Matches' },
    { id: 'suggestions', label: 'Suggestions' },
  ]
  type TabId = 'watchlist' | 'matches' | 'suggestions'
  let activeTab = $state<TabId>('watchlist')

  function selectTab(id: string) {
    activeTab = id as TabId
    // Before #547, Watchlist and Suggestions were two separate views
    // that remounted -- and so refetched -- every time you navigated
    // between them. All three now stay mounted (just hidden) once you
    // switch away, which loses that free refetch-on-arrival -- most
    // visibly for accepting a suggestion, which creates a real watchlist
    // entry that the Watchlist tab would otherwise keep showing its
    // pre-accept snapshot without. Refreshed here instead, on every
    // switch, so no tab is ever more than one switch stale.
    //
    // For Matches that also means arriving always shows the newest 100
    // rather than wherever a previous visit's "load older" had walked
    // back to -- the tab's promise is the recent list, and a stale deep
    // page is a worse thing to land on than a fresh shallow one.
    if (activeTab === 'watchlist') watchlistState.refresh()
    else if (activeTab === 'matches') {
      // Entries first, then the matches themselves. A row resolves its
      // entry's name, its mode, and the empty state's coverage sentence
      // from the entries list, so loading matches against a stale one
      // renders "(entry removed)" over entries that exist -- an evidence
      // surface calling a live entry deleted is the worst sentence this
      // tab could say, and the one it would say silently.
      //
      // Not hypothetical, and not only a race at first paint: the page
      // stays mounted, and nothing else refreshes the entries until
      // App.svelte's own 60-second coverage interval comes round
      // (WATCHLIST_COVERAGE_REFRESH_MS). An entry created, renamed or
      // deleted anywhere else is misdescribed here for up to a minute.
      // Caught by live-matches-tab.mjs, which found every row named
      // "(entry removed)" while both entries existed.
      //
      // Chained rather than fired together so the names are in place by
      // the time the rows are, and .catch so a failed entries fetch
      // still lets the matches load -- a list with imperfect names beats
      // no list at all.
      watchlistState
        .refresh()
        .catch(() => {})
        .then(() => matchesState.load())
    } else suggestState.refresh()
  }

  // Following a match's entry name back to the entry itself (#584): the
  // Watchlist tab, that entry expanded, scrolled to. The scroll is
  // deliberate -- the entry list can be long, and switching tabs to a
  // row that is expanded somewhere off-screen looks like nothing
  // happened.
  async function openEntry(entryId: string) {
    selectTab('watchlist')
    expandedId = entryId
    await tick()
    // Optional-call rather than assumed: jsdom has no layout, so
    // scrollIntoView is not implemented there.
    document.getElementById(`entry-${entryId}`)?.scrollIntoView?.({ block: 'center' })
  }

  // --- Add/edit form -----------------------------------------------

  let editingId = $state<string | null>(null)
  let draftName = $state('')
  let draftInvert = $state(false)
  let draftSourceMac = $state('')
  let draftSourceIp = $state('')
  let draftDestIp = $state('')
  let draftPorts = $state('')
  let draftIncludeStructuralNoise = $state(false)

  let error = $state<string | null>(null)
  let saving = $state(false)
  let deletingId = $state<string | null>(null)

  function resetDraft() {
    editingId = null
    draftName = ''
    draftInvert = false
    draftSourceMac = ''
    draftSourceIp = ''
    draftDestIp = ''
    draftPorts = ''
    draftIncludeStructuralNoise = false
    error = null
  }

  function startEdit(e: WatchlistEntry) {
    editingId = e.id
    draftName = e.name ?? ''
    draftInvert = !!e.invert
    draftSourceMac = e.source?.mac ?? ''
    draftSourceIp = e.source?.ip ?? ''
    draftDestIp = e.destIp ?? ''
    draftPorts = (e.ports ?? []).join(', ')
    draftIncludeStructuralNoise = !!e.includeStructuralNoise
    error = null
  }

  // Mirrors Entities.svelte's parseTags shape -- comma/whitespace
  // separated, blank entries dropped, non-numeric entries dropped rather
  // than rejecting the whole field (a stray comma or typo shouldn't lose
  // every other port already typed).
  function parsePorts(v: string): number[] {
    return v
      .split(/[,\s]+/)
      .map((s) => Number(s.trim()))
      .filter((n) => Number.isInteger(n) && n > 0)
  }

  async function submit(ev: Event) {
    ev.preventDefault()
    saving = true
    error = null
    try {
      const req = {
        name: draftName.trim() || undefined,
        invert: draftInvert,
        source:
          draftSourceMac.trim() || draftSourceIp.trim()
            ? { mac: draftSourceMac.trim() || undefined, ip: draftSourceIp.trim() || undefined }
            : undefined,
        destIp: draftInvert ? undefined : draftDestIp.trim() || undefined,
        ports: draftInvert ? undefined : parsePorts(draftPorts),
        includeStructuralNoise: draftInvert ? draftIncludeStructuralNoise : undefined,
      }
      const err = editingId ? await watchlistState.update(editingId, req) : await watchlistState.create(req)
      if (err) {
        error = err
      } else {
        resetDraft()
      }
    } finally {
      saving = false
    }
  }

  async function remove(e: WatchlistEntry) {
    if (!confirm(`Remove the watchlist entry "${e.name || e.id}"? This does not delete any matches it already recorded.`))
      return
    deletingId = e.id
    try {
      // The return value is the error text, not a throw -- see
      // lib/api.ts. Dropping it left a failed delete looking identical
      // to a successful one: the row stayed, and nothing said why.
      error = await watchlistState.remove(e.id)
    } finally {
      deletingId = null
    }
  }

  // --- Observe/promote/matches, expanded per entry ------------------

  let expandedId = $state<string | null>(null)
  let togglingObserve = $state<string | null>(null)
  let promoting = $state<string | null>(null)
  let matchesByEntry = $state<Record<string, WatchlistMatch[] | 'loading' | 'error'>>({})

  function toggleExpand(id: string) {
    expandedId = expandedId === id ? null : id
  }

  async function toggleObserving(e: WatchlistEntry) {
    togglingObserve = e.id
    try {
      error = await watchlistState.setObserving(e.id, !e.observing)
    } finally {
      togglingObserve = null
    }
  }

  async function promoteOne(e: WatchlistEntry, d: WatchlistPermittedDest) {
    promoting = e.id + d.destIp + d.port
    try {
      error = await watchlistState.promote(e.id, [d])
    } finally {
      promoting = null
    }
  }

  // loadMatches is called each time the matches panel is opened rather
  // than cached indefinitely -- a match log is append-only and can
  // change between views, and the volumes here (an entry's own recent
  // matches) are small enough that refetching on open is cheap.
  // The reason is kept rather than collapsed to "Could not load
  // matches." The backend distinguishes these -- a 503 says the match
  // log is not available, which is a configuration answer, not a
  // network blip -- and this page's own house style is that an error
  // names what to fix.
  let matchErrorByEntry = $state<Record<string, string>>({})

  async function loadMatches(e: WatchlistEntry) {
    if (!e.source?.mac && !e.source?.ip) return
    matchesByEntry[e.id] = 'loading'
    try {
      matchesByEntry[e.id] = await watchlistState.matchesFor(e.source.mac, e.source.ip)
      delete matchErrorByEntry[e.id]
    } catch (err) {
      matchErrorByEntry[e.id] = err instanceof Error ? err.message : String(err)
      matchesByEntry[e.id] = 'error'
    }
  }

  // Why an entry might never match, when that can be said for certain
  // (#274). Returns null far more often than not, deliberately: the
  // router push is optional, so most deployments have nothing to answer
  // from, and a wrong warning here is worse than no warning -- it sends
  // an operator to fix a rule set that is fine.
  function coverageWarning(id: string): string | null {
    switch (watchlistState.coverage[id]) {
      case 'no-logging':
        return (
          'Nothing can match this: no firewall rule on any router you have connected has logging turned on, ' +
          'so no traffic is being reported at all. Set log=yes on the rules you want to see (see the RouterOS setup guide).'
        )
      case 'out-of-scope':
        return (
          'Nothing can match this: your routers do log, but no logging rule covers what this entry watches, ' +
          'so no traffic in its scope is ever reported. Widen a rule, or narrow this entry to something a rule covers.'
        )
      default:
        // 'covered', 'unknown', or no answer at all. Silence.
        return null
    }
  }

  function formatTime(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
  }

  function sourceLabel(e: WatchlistEntry): string {
    if (e.source?.mac) return e.source.mac
    if (e.source?.ip) return e.source.ip
    return 'any source'
  }

  // The state a card's own stripe already shows by colour (round 19: the
  // watchers' purple for healthy, the alarm ink where the ring is
  // broken) -- named here too, as text, so it sorts and filters.
  // paused > no logging visible > ring broken > watching (#680). The
  // order matters: a watch no rule logs cannot be judged on nightly
  // presence at all, so coverage outranks the recorded ring.
  function stateLabel(e: WatchlistEntry): string {
    if (!e.enabled) return 'paused'
    if (watchlistState.coverage[e.id] === 'no-logging') return 'ring broken'
    if (e.ring?.broken) return 'ring broken'
    return 'watching'
  }

  function detailLabel(e: WatchlistEntry): string {
    return e.invert
      ? `${(e.permitted ?? []).length} permitted, ${(e.observed ?? []).length} to review`
      : `ports ${(e.ports ?? []).join(', ')}${e.destIp ? ` → ${e.destIp}` : ''}`
  }

  // Every column sorts and filters (#649, round-18/19's ratified idiom):
  // a quiet dashed row beneath the labels; clicking a label sorts by it,
  // again to reverse. There is no fixed "the record's own order" to
  // preserve here (entries render in whatever order the API returns),
  // so name is a reasonable, stable default.
  type WatchSortKey = 'watch' | 'boundary' | 'state' | 'detail'
  let sortKey = $state<WatchSortKey>('watch')
  let sortDir = $state<SortDir>('asc')
  let filters = $state({ watch: '', boundary: '', state: '', detail: '' })

  function toggleSort(key: WatchSortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = 'asc'
    }
  }

  function dirGlyph(key: WatchSortKey): string {
    if (sortKey !== key) return ''
    return sortDir === 'asc' ? '▲' : '▼'
  }

  const filteredEntries = $derived(
    watchlistState.entries.filter(
      (e) =>
        matchesFilter(e.name || '(unnamed)', filters.watch) &&
        matchesFilter(sourceLabel(e), filters.boundary) &&
        matchesFilter(stateLabel(e), filters.state) &&
        matchesFilter(detailLabel(e), filters.detail),
    ),
  )

  const sortedEntries = $derived.by((): WatchlistEntry[] => {
    const list = [...filteredEntries]
    list.sort((a, b) => {
      switch (sortKey) {
        case 'watch':
          return compareText(a.name || '(unnamed)', b.name || '(unnamed)', sortDir)
        case 'boundary':
          return compareText(sourceLabel(a), sourceLabel(b), sortDir)
        case 'state':
          return compareText(stateLabel(a), stateLabel(b), sortDir)
        case 'detail':
          return compareText(detailLabel(a), detailLabel(b), sortDir)
      }
    })
    return list
  })

  // --- The ratified table (#676, round 29's "watch · boundary · window
  // · state · last event") ---------------------------------------------
  //
  // This is a second read of the same entries the card list above
  // manages -- the round-29 docket scene's own surface, not a
  // replacement for it. Add/edit/remove and the invert/observe/promote
  // workflow (the "Manage entries" section below) are a different, real
  // feature the ratified design doesn't describe; #676 leaves it
  // reachable rather than folding or deleting it. See this component's
  // own module comment and the issue for the full account.
  //
  // "window" and the nightly summary are both real now (#680): an entry
  // carries a Window (clock range, days, IANA zone) and up to seven
  // recorded Nights against it. A row with no window still reads
  // "always", which is the honest answer for one that has none, and an
  // entry with no nights recorded yet gets no summary line at all rather
  // than a zeroed one -- see ../lib/watchWindow.ts.
  //
  // The nightly history is recorded on the entry, never derived from
  // matchlog: matchlog keeps 48 hours by default and collapses repeats
  // of the same (entry, destination, port) into one record spanning
  // firstSeen..lastSeen, so reading seven nights back out of it would
  // report a healthy watch as five empty nights and look like it had
  // worked. "mend — widen window" is still not offered: what widening
  // should propose is an interface question the issue leaves open.

  // Most recent match for one entry, from matchesState's own bulk
  // "recent across every entry" feed (loaded in onMount above) --
  // avoids an N+1 fetch per row for a table this is meant to render
  // plainly. Only the newest MATCHES_PAGE_SIZE (100) matches network-wide
  // are held, so a genuinely stale entry can read "—" even though older
  // matches exist further back than this page reaches; honest given
  // what's loaded, not a claim the entry has never matched.
  function lastMatchFor(entryId: string): WatchlistMatch | undefined {
    let best: WatchlistMatch | undefined
    for (const m of matchesState.records) {
      if (m.entryId !== entryId) continue
      if (!best || new Date(m.lastSeen) > new Date(best.lastSeen)) best = m
    }
    return best
  }

  // The ratified vocabulary (◉ watching / ○ ring broken), in the
  // precedence #680 settled: paused > no logging visible > ring broken >
  // watching.
  //
  // Coverage outranks the recorded ring because they are different kinds
  // of broken. "No logging visible" is a fact about mikroview's own
  // sight, read live from router state; a broken ring is a fact about the
  // network, read from nights that were actually watched. A watch nothing
  // logs has no nightly presence to judge -- its nights are recorded "not
  // observed" precisely so they cannot be mistaken for silence -- so the
  // sight problem is what an operator needs told first.
  function watchState(e: WatchlistEntry): { glyph: string; text: string; broken: boolean } {
    if (!e.enabled) return { glyph: '○', text: 'paused', broken: false }
    if (watchlistState.coverage[e.id] === 'no-logging') {
      return { glyph: '○', text: 'ring broken — no logging visible', broken: true }
    }
    if (e.ring?.broken) {
      return { glyph: '○', text: 'ring broken — nothing in the window', broken: true }
    }
    return { glyph: '◉', text: 'watching', broken: false }
  }

  function boundaryLabel(e: WatchlistEntry): string {
    const dest = e.destIp ? e.destIp : e.invert ? 'its observed destinations' : 'any destination'
    return `${sourceLabel(e)} → ${dest}`
  }

  // The drawer's standalone headline plus the rest of the paragraph
  // (round 29's own idiom) -- composed from real facts only: enabled,
  // coverage, and the most recent match this page has loaded. No
  // invented specifics ("inside the window," "usual size") that would
  // need the window/schedule concept this entry doesn't carry.
  function watchStory(e: WatchlistEntry, lastMatch: WatchlistMatch | undefined): { headline: string; body: string } {
    if (!e.enabled) {
      return {
        headline: 'Paused.',
        body: `This watch is turned off, so mikroview is not recording anything for ${sourceLabel(e)} right now.`,
      }
    }
    if (watchlistState.coverage[e.id] === 'no-logging') {
      return {
        headline: 'The ring is broken.',
        body: 'No firewall rule mikroview can see is logging this pathway, so nothing here can be recorded until a rule that covers it turns logging on.',
      }
    }
    if (e.ring?.broken) {
      // The recorded break, which knows *which* window closed empty --
      // that is why it is written down at the break rather than worked
      // out here (#680). The since clause is dropped rather than guessed
      // if the record does not carry one.
      const since = e.ring.since ? ` since ${formatRelative(e.ring.since, appState.now)}` : ''
      return {
        headline: 'The ring is broken.',
        body: `Nothing has matched inside this watch's window${since}. Nights mikroview could not watch are not counted against it.`,
      }
    }
    if (lastMatch) {
      return {
        headline: 'Watching.',
        body: `${sourceLabel(e)} last matched ${formatRelative(lastMatch.lastSeen, appState.now)}, reaching ${lastMatch.tuple.destIp}:${lastMatch.tuple.port}.`,
      }
    }
    return {
      headline: 'Watching.',
      body: `${sourceLabel(e)} is being watched. Nothing has matched in the recent log yet.`,
    }
  }

  let watchDrawerId: string | null = $state(null)
  let pausingId = $state<string | null>(null)
  let wtError = $state<string | null>(null)

  function toggleWatchDrawer(id: string) {
    watchDrawerId = watchDrawerId === id ? null : id
  }

  // "pause watch" / "resume watch" (#676): the plain enable toggle the
  // add/edit form never exposed on its own -- see
  // watchlistState.setEnabled's own doc comment for why this is the
  // generic definition PUT rather than a new route.
  async function togglePause(e: WatchlistEntry) {
    wtError = null
    pausingId = e.id
    try {
      const err = await watchlistState.setEnabled(e.id, !e.enabled)
      if (err) wtError = err
    } finally {
      pausingId = null
    }
  }

  // "open in stream ▸" (#676): same filter-to-live pattern Flags.svelte's
  // filterToTarget uses -- only offered for a scoped entry (a mac or ip
  // to filter on), same reason isFilterable() gates Flags' own version.
  function openWatchInStream(e: WatchlistEntry) {
    const q = e.source?.mac || e.source?.ip
    if (!q) return
    appState.setFilter('srcQuery', q)
    appState.view = 'live'
  }

  type WatchTableSortKey = 'watch' | 'boundary' | 'window' | 'state' | 'lastEvent'
  let wtSortKey = $state<WatchTableSortKey>('watch')
  let wtSortDir = $state<SortDir>('asc')
  let wtFilters = $state({ watch: '', boundary: '', window: '', state: '', lastEvent: '' })

  function wtToggleSort(key: WatchTableSortKey) {
    if (wtSortKey === key) {
      wtSortDir = wtSortDir === 'asc' ? 'desc' : 'asc'
    } else {
      wtSortKey = key
      wtSortDir = 'asc'
    }
  }

  function wtDirGlyph(key: WatchTableSortKey): string {
    if (wtSortKey !== key) return ''
    return wtSortDir === 'asc' ? '▲' : '▼'
  }

  type WatchRow = {
    entry: WatchlistEntry
    boundary: string
    window: string
    stateGlyph: string
    stateText: string
    broken: boolean
    lastMatch: WatchlistMatch | undefined
    lastEventLabel: string
  }

  const watchRows = $derived.by((): WatchRow[] =>
    watchlistState.entries.map((e) => {
      const lastMatch = lastMatchFor(e.id)
      const st = watchState(e)
      return {
        entry: e,
        boundary: boundaryLabel(e),
        // "always" for an entry with no window; the clock range, days and
        // zone for one that has (#680).
        window: windowLabel(e),
        stateGlyph: st.glyph,
        stateText: st.text,
        broken: st.broken,
        lastMatch,
        lastEventLabel: lastMatch ? formatRelative(lastMatch.lastSeen, appState.now) : '—',
      }
    }),
  )

  const filteredWatchRows = $derived(
    watchRows.filter(
      (r) =>
        matchesFilter(r.entry.name || '(unnamed)', wtFilters.watch) &&
        matchesFilter(r.boundary, wtFilters.boundary) &&
        matchesFilter(r.window, wtFilters.window) &&
        matchesFilter(r.stateText, wtFilters.state) &&
        matchesFilter(r.lastEventLabel, wtFilters.lastEvent),
    ),
  )

  const sortedWatchRows = $derived.by((): WatchRow[] => {
    const list = [...filteredWatchRows]
    list.sort((a, b) => {
      switch (wtSortKey) {
        case 'watch':
          return compareText(a.entry.name || '(unnamed)', b.entry.name || '(unnamed)', wtSortDir)
        case 'boundary':
          return compareText(a.boundary, b.boundary, wtSortDir)
        case 'window':
          return compareText(a.window, b.window, wtSortDir)
        case 'state':
          return compareText(a.stateText, b.stateText, wtSortDir)
        case 'lastEvent': {
          const ta = a.lastMatch ? new Date(a.lastMatch.lastSeen).getTime() : 0
          const tb = b.lastMatch ? new Date(b.lastMatch.lastSeen).getTime() : 0
          return compareNumeric(ta, tb, wtSortDir)
        }
      }
    })
    return list
  })

  // --- Round-30 fidelity flags (#700, #691) --------------------------
  //
  // The ratified mockup (docs/design/concepts/round-30/shots/
  // docket-watchlist.png, the-whole.html #s7's `#p-watch` panel) draws
  // one flat watch table under the docket's tabs -- watch / boundary /
  // window / state / last event, one header row, one filter row
  // directly beneath it, nothing else. Everything below that the
  // ratified design does not draw -- the Watchlist/Matches/Suggestions
  // sub-tab row, the "Watches" page heading (round 30: "no page heading
  // and no strap, anywhere" -- owner, 2026-08-31), the "Manage entries"
  // explanatory prose, the add/edit form, and the second "Entries" card
  // list -- is real, shipped capability (#243, #547, #584, #649, #676)
  // that the ratified design simply doesn't describe. Per the
  // build-to-the-mockup-first policy (#700) none of it is deleted; each
  // surface is unmounted behind its own typed flag (explicit boolean,
  // not inferred -- a bare `false` narrows to `never` and the type
  // checker reports the guarded block as unreachable), same pattern as
  // LiveTable's RESIZE_HANDLES_ENABLED, MetricsRegister's LEDGER_ENABLED
  // and Topography's DEGRADED_NOTE_ENABLED. Bringing any one back is
  // tracked on #691, independently of the others.
  const WATCHLIST_SUBTABS_ENABLED: boolean = false
  const WATCH_HEADING_ENABLED: boolean = false
  const MANAGE_ENTRIES_INTRO_ENABLED: boolean = false
  const ADD_ENTRY_FORM_ENABLED: boolean = false
  const ENTRIES_TABLE_ENABLED: boolean = false
</script>

<div class="watchlist-page">
  {#if WATCHLIST_SUBTABS_ENABLED}
    <TabList {tabs} selected={activeTab} onselect={selectTab} label="Watchlist views" />
  {/if}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -- role and tabindex
       both turn on together with WATCHLIST_SUBTABS_ENABLED (undefined/
       undefined when off, 'tabpanel'/0 when on); the checker can't
       follow that they're tied to the same flag. -->
  <div
    class="page scrollbar"
    role={WATCHLIST_SUBTABS_ENABLED ? 'tabpanel' : undefined}
    id="panel-watchlist"
    aria-labelledby={WATCHLIST_SUBTABS_ENABLED ? 'tab-watchlist' : undefined}
    tabindex={WATCHLIST_SUBTABS_ENABLED ? 0 : undefined}
    hidden={WATCHLIST_SUBTABS_ENABLED && activeTab !== 'watchlist'}
  >
  <!-- The ratified table (#676, round 29's docket scene: watch ·
       boundary · window · state · last event, rows opening as drawers
       like the flags tab). Reads the same entries the "Manage entries"
       section below edits -- see the script's own section comment for
       what "window" and the seven-night strip could not honestly carry,
       and why.

       Header + filter (round 30, the-whole.html #s7 `.panel thead`):
       one <thead> carrying both the clickable column heads (click to
       sort, again to reverse) and the filter row directly beneath them
       -- not a standalone sort/filter toolbar floating above a second,
       non-interactive <thead>, which is what this used to render (two
       header rows for one table). -->
  <section class="section watch-table-section" aria-label="Watches">
    {#if WATCH_HEADING_ENABLED}
      <h3 id="watch-heading" class="section-title">Watches</h3>
    {/if}
    {#if wtError}<p class="error" role="alert">{wtError}</p>{/if}
    {#if watchlistState.entries.length === 0}
      <p class="empty">{ADD_ENTRY_FORM_ENABLED ? 'No watches yet -- add one below.' : 'No watches yet.'}</p>
    {:else}
      <table class="watch-table">
        <thead>
          <tr>
            <th>
              <button type="button" class="th-sort" class:on={wtSortKey === 'watch'} onclick={() => wtToggleSort('watch')}>
                watch <span class="dir">{wtDirGlyph('watch')}</span>
              </button>
            </th>
            <th>
              <button type="button" class="th-sort" class:on={wtSortKey === 'boundary'} onclick={() => wtToggleSort('boundary')}>
                boundary <span class="dir">{wtDirGlyph('boundary')}</span>
              </button>
            </th>
            <th>
              <button type="button" class="th-sort" class:on={wtSortKey === 'window'} onclick={() => wtToggleSort('window')}>
                window <span class="dir">{wtDirGlyph('window')}</span>
              </button>
            </th>
            <th>
              <button type="button" class="th-sort" class:on={wtSortKey === 'state'} onclick={() => wtToggleSort('state')}>
                state <span class="dir">{wtDirGlyph('state')}</span>
              </button>
            </th>
            <th>
              <button type="button" class="th-sort" class:on={wtSortKey === 'lastEvent'} onclick={() => wtToggleSort('lastEvent')}>
                last event <span class="dir">{wtDirGlyph('lastEvent')}</span>
              </button>
            </th>
            <th></th>
          </tr>
          <tr class="filters">
            <td><input bind:value={wtFilters.watch} placeholder="filter" aria-label="Filter watches by watch name" /></td>
            <td><input bind:value={wtFilters.boundary} placeholder="filter" aria-label="Filter watches by boundary" /></td>
            <td><input bind:value={wtFilters.window} placeholder="filter" aria-label="Filter watches by window" /></td>
            <td><input bind:value={wtFilters.state} placeholder="filter" aria-label="Filter watches by state" /></td>
            <td>
              <input bind:value={wtFilters.lastEvent} placeholder="filter" aria-label="Filter watches by last event" />
            </td>
            <td></td>
          </tr>
        </thead>
        <tbody>
          {#if sortedWatchRows.length === 0}
            <tr><td class="empty-row" colspan="6">No watches match these filters.</td></tr>
          {:else}
            {#each sortedWatchRows as row (row.entry.id)}
              {@const story = watchStory(row.entry, row.lastMatch)}
              {@const nights = nightlySummary(row.entry.nights)}
              <tr
                class="wt-row"
                class:watching={!row.broken && row.entry.enabled}
                class:ring-broken={row.broken}
                onclick={() => toggleWatchDrawer(row.entry.id)}
              >
                <td class="k">{row.entry.name || '(unnamed)'}</td>
                <td>{row.boundary}</td>
                <td class="t">{row.window}</td>
                <td><span class="wchip2" class:broken={row.broken}>{row.stateGlyph} {row.stateText}</span></td>
                <td class="t">{row.lastEventLabel}</td>
                <td>
                  <button
                    class="openc"
                    aria-expanded={watchDrawerId === row.entry.id}
                    aria-label="{watchDrawerId === row.entry.id ? 'Close' : 'Open'} the drawer for {row.entry.name ||
                      'this watch'}"
                    onclick={(ev) => {
                      ev.stopPropagation()
                      toggleWatchDrawer(row.entry.id)
                    }}
                  >
                    ▸
                  </button>
                </td>
              </tr>
              {#if watchDrawerId === row.entry.id}
                <tr class="wt-drawer" class:ring-broken={row.broken}>
                  <td colspan="6">
                    <div class="dwr">
                      <div class="dcol">
                        <p class="story"><b>{story.headline}</b> {story.body}</p>
                        <div class="lines">{row.lastMatch ? row.lastMatch.event.raw : 'No matching line in the recent log.'}</div>
                      </div>
                      <div class="side">
                        <span class="lab">the pathway</span>
                        <p class="ep-note">{detailLabel(row.entry)}</p>
                        {#if nights}
                          <span class="lab">the last seven nights</span>
                          <p class="ep-note">{nights}</p>
                        {/if}
                      </div>
                      <div class="dwr-acts">
                        <button class="act" disabled={pausingId === row.entry.id} onclick={() => togglePause(row.entry)}>
                          {pausingId === row.entry.id ? 'Saving…' : row.entry.enabled ? 'pause watch' : 'resume watch'}
                        </button>
                        {#if row.entry.source?.mac || row.entry.source?.ip}
                          <button class="act quiet" onclick={() => openWatchInStream(row.entry)}>open in stream ▸</button>
                        {/if}
                      </div>
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          {/if}
        </tbody>
      </table>
    {/if}
  </section>

  <!-- "Manage entries" (#676's second surface: the add/edit/invert/
       observe/promote workflow this component pre-dates the ratified
       table with). Round 30 draws none of it -- see the flags' own
       comment above the script's watch-window section. Kept reachable,
       not deleted, per #700; the heading covers all three sub-surfaces
       so restoring any one of them still gets it back. -->
  {#if MANAGE_ENTRIES_INTRO_ENABLED || ADD_ENTRY_FORM_ENABLED || ENTRIES_TABLE_ENABLED}
    <h3 class="section-title manage-heading">Manage entries</h3>
  {/if}

  {#if MANAGE_ENTRIES_INTRO_ENABLED}
    <p class="intro">
      Watch attempts against specific ports (<strong>record</strong>), or flip an
      entry around to watch what one device does (<strong>invert</strong>): "this device should only ever reach X" --
      everything else it touches gets recorded. A new inverted entry starts <strong>observing</strong>: nothing fires
      until you review what it actually saw and promote the destinations that are expected. Matches are recorded to
      disk and survive a restart, unlike the live view's own volatile buffer.
    </p>
  {/if}

  {#if ADD_ENTRY_FORM_ENABLED}
  <form class="form" onsubmit={submit}>
    <div class="form-title">{editingId ? 'Editing entry' : 'Add entry'}</div>
    <div class="form-row">
      <label class="field">
        <span>Name</span>
        <input type="text" placeholder="SSH watch" bind:value={draftName} />
      </label>
      <label class="field checkbox-field">
        <span>
          <input type="checkbox" bind:checked={draftInvert} />
          Invert (watch what a device does, not a port list)
        </span>
      </label>
    </div>

    <div class="form-row">
      <label class="field">
        <span>Source MAC{draftInvert ? ' (required)' : ' (optional)'}</span>
        <input type="text" placeholder="aa:bb:cc:dd:ee:ff" bind:value={draftSourceMac} required={draftInvert} />
      </label>
      <label class="field">
        <span>Source IP (fallback, used only if MAC is unknown for a given event)</span>
        <input type="text" placeholder="192.168.1.50" bind:value={draftSourceIp} />
      </label>
    </div>

    {#if !draftInvert}
      <div class="form-row">
        <label class="field">
          <span>Destination IP (optional)</span>
          <input type="text" placeholder="any destination" bind:value={draftDestIp} />
        </label>
        <label class="field grow">
          <span>Ports (comma-separated, required)</span>
          <input type="text" placeholder="22, 23, 3389" bind:value={draftPorts} required />
        </label>
      </div>
    {:else}
      <div class="form-row">
        <label class="field checkbox-field">
          <span>
            <input type="checkbox" bind:checked={draftIncludeStructuralNoise} />
            Also watch broadcast/multicast/link-local traffic (usually just noise -- off by default)
          </span>
        </label>
      </div>
    {/if}

    {#if error}
      <p class="error">{error}</p>
    {/if}
    <div class="form-actions">
      {#if editingId}
        <button type="button" class="cancel" onclick={resetDraft}>Cancel</button>
      {/if}
      <button type="submit" class="save" disabled={saving}>
        {saving ? 'Saving…' : editingId ? 'Save changes' : 'Add entry'}
      </button>
    </div>
  </form>
  {/if}

  {#if ENTRIES_TABLE_ENABLED}
  <section class="section" id="entries-section">
    <h3 class="section-title">Entries</h3>
    {#if watchlistState.entries.length === 0}
      <p class="empty">No watchlist entries yet -- add one above.</p>
    {:else}
      <!-- Every column sorts and filters (#649): a quiet dashed row
           beneath the labels, matching round-18/19's ratified idiom. -->
      <div class="sortbar" role="row">
        <button class="sorth" class:on={sortKey === 'watch'} onclick={() => toggleSort('watch')}>
          watch <span class="dir">{dirGlyph('watch')}</span>
        </button>
        <button class="sorth" class:on={sortKey === 'boundary'} onclick={() => toggleSort('boundary')}>
          boundary <span class="dir">{dirGlyph('boundary')}</span>
        </button>
        <button class="sorth" class:on={sortKey === 'state'} onclick={() => toggleSort('state')}>
          state <span class="dir">{dirGlyph('state')}</span>
        </button>
        <button class="sorth" class:on={sortKey === 'detail'} onclick={() => toggleSort('detail')}>
          detail <span class="dir">{dirGlyph('detail')}</span>
        </button>
      </div>
      <div class="filterbar" role="row">
        <input bind:value={filters.watch} placeholder="filter watch…" aria-label="Filter by watch name" />
        <input bind:value={filters.boundary} placeholder="filter boundary…" aria-label="Filter by boundary" />
        <input bind:value={filters.state} placeholder="filter state…" aria-label="Filter by state" />
        <input bind:value={filters.detail} placeholder="filter detail…" aria-label="Filter by detail" />
      </div>
      {#if sortedEntries.length === 0}
        <p class="empty">No entries match these filters.</p>
      {:else}
      <ul class="list">
        {#each sortedEntries as e (e.id)}
          <!-- The id is the target a match row's entry name scrolls to
               (openEntry, #584), not decoration. -->
          <!-- The watchlist wears the docket's stripe treatment too
               (round 19): the watchers' purple for a healthy watch, the
               alarm ink where the ring is broken (same condition as
               watchlistState.brokenCount), nothing for a paused one. -->
          <li
            class="card"
            class:watching={e.enabled && watchlistState.coverage[e.id] !== 'no-logging'}
            class:ring-broken={e.enabled && watchlistState.coverage[e.id] === 'no-logging'}
            id="entry-{e.id}"
          >
            <button class="card-main" onclick={() => toggleExpand(e.id)}>
              <span class="name">{e.name || '(unnamed)'}</span>
              {#if e.invert}
                <span class="badge invert">inverted</span>
                {#if e.observing}
                  <span class="badge observing">observing</span>
                {/if}
              {/if}
              <span class="source">{sourceLabel(e)}</span>
              {#if e.invert}
                <span class="detail">{(e.permitted ?? []).length} permitted, {(e.observed ?? []).length} to review</span>
              {:else}
                <span class="detail">ports {(e.ports ?? []).join(', ')}{e.destIp ? ` → ${e.destIp}` : ''}</span>
              {/if}
            </button>
            {#if coverageWarning(e.id)}
              <p class="coverage-warning" role="status">{coverageWarning(e.id)}</p>
            {/if}

            <span class="row-actions">
              <button class="edit" onclick={() => startEdit(e)}>Edit</button>
              <button class="delete" disabled={deletingId === e.id} onclick={() => remove(e)}>
                {deletingId === e.id ? 'Removing…' : 'Remove'}
              </button>
            </span>

            {#if expandedId === e.id}
              <div class="expanded">
                {#if e.invert}
                  <div class="expanded-row">
                    <button class="observe-toggle" disabled={togglingObserve === e.id} onclick={() => toggleObserving(e)}>
                      {togglingObserve === e.id
                        ? 'Saving…'
                        : e.observing
                          ? 'Stop observing (start enforcing)'
                          : 'Resume observing'}
                    </button>
                    {#if e.observing}
                      <span class="hint">Nothing fires while observing -- review what's below and promote what's expected.</span>
                    {:else}
                      <span class="hint">Enforcing: anything not in Permitted below is recorded as a violation.</span>
                    {/if}
                  </div>

                  <div class="sub-section">
                    <h4>Permitted ({(e.permitted ?? []).length})</h4>
                    {#if (e.permitted ?? []).length === 0}
                      <p class="empty small">Nothing promoted yet.</p>
                    {:else}
                      <ul class="dest-list">
                        {#each e.permitted ?? [] as p (p.destIp + ':' + p.port)}
                          <li>{p.destIp}:{p.port}</li>
                        {/each}
                      </ul>
                    {/if}
                  </div>

                  <div class="sub-section">
                    <h4>To review ({(e.observed ?? []).length})</h4>
                    {#if (e.observed ?? []).length === 0}
                      <p class="empty small">Nothing observed yet -- it will appear here once the device is seen reaching somewhere new.</p>
                    {:else}
                      <ul class="dest-list">
                        {#each e.observed ?? [] as o (o.destIp + ':' + o.port)}
                          <li>
                            <span class="dest">{o.destIp}:{o.port}</span>
                            <span class="dest-meta">
                              seen {o.count}× · last {formatTime(o.lastSeen)}
                            </span>
                            <button
                              class="promote"
                              disabled={promoting === e.id + o.destIp + o.port}
                              onclick={() => promoteOne(e, { destIp: o.destIp, port: o.port })}
                            >
                              {promoting === e.id + o.destIp + o.port ? 'Promoting…' : 'Promote'}
                            </button>
                          </li>
                        {/each}
                      </ul>
                    {/if}
                  </div>
                {/if}

                {#if e.source?.mac || e.source?.ip}
                  <div class="sub-section">
                    <h4>Recent matches</h4>
                    {#if !matchesByEntry[e.id]}
                      <button class="load-matches" onclick={() => loadMatches(e)}>Load recent matches</button>
                    {:else if matchesByEntry[e.id] === 'loading'}
                      <p class="empty small">Loading…</p>
                    {:else if matchesByEntry[e.id] === 'error'}
                      <p class="error">Could not load matches: {matchErrorByEntry[e.id] ?? 'unknown error'}</p>
                      <button class="load-matches" onclick={() => loadMatches(e)}>Try again</button>
                    {:else if (matchesByEntry[e.id] as WatchlistMatch[]).length === 0}
                      <p class="empty small">No matches recorded yet for this entry's device.</p>
                    {:else}
                      <ul class="match-list">
                        {#each matchesByEntry[e.id] as WatchlistMatch[] as m (m.id)}
                          <li>
                            <span class="dest">{m.tuple.destIp}:{m.tuple.port}</span>
                            <span class="dest-meta">
                              {m.count}× · last {formatTime(m.lastSeen)} · via {m.event.action}
                            </span>
                          </li>
                        {/each}
                      </ul>
                    {/if}
                  </div>
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
      {/if}
    {/if}
  </section>
  {/if}
  </div>

  {#if WATCHLIST_SUBTABS_ENABLED}
    <div
      class="tab-panel"
      role="tabpanel"
      id="panel-matches"
      aria-labelledby="tab-matches"
      tabindex="0"
      hidden={activeTab !== 'matches'}
    >
      <MatchesTab entries={watchlistState.entries} coverage={watchlistState.coverage} onopenentry={openEntry} />
    </div>

    <div
      class="tab-panel"
      role="tabpanel"
      id="panel-suggestions"
      aria-labelledby="tab-suggestions"
      tabindex="0"
      hidden={activeTab !== 'suggestions'}
    >
      <Suggestions />
    </div>
  {/if}
</div>

<style>
  .watchlist-page {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  /* Every non-Watchlist panel is a full-height column holding one
     component -- one rule rather than one class per tab. */
  .tab-panel {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  /* The `hidden` attribute on each panel is not enough on its own. Its
     `display: none` comes from the UA stylesheet, and *any* author
     declaration outranks a UA one whatever its specificity -- so the
     `display: flex` above (and on .page below) wins, and a "hidden"
     panel renders anyway, stacked under the selected one. Confirmed in
     Chromium, not deduced: a hidden element carrying a class with
     `display: flex` computes to `flex` and Playwright reports it
     visible.

     Present since the tabs landed (#547) and invisible to the tests,
     which assert on the `hidden` attribute rather than on what a browser
     does with it. Fixed here rather than left, because a third panel
     makes it three surfaces deep instead of two. */
  .page[hidden],
  .tab-panel[hidden] {
    display: none;
  }

  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .form {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .form-row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
    flex: 1 1 200px;
    min-width: 160px;
  }

  .field.grow {
    flex: 2 1 260px;
  }

  .checkbox-field span {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  input[type='text'] {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 6px 8px;
    font-size: 13px;
  }

  input[type='text']:focus {
    outline: none;
    border-color: var(--accent);
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }

  /* Deliberately not the same red as .error: an entry that cannot match
     is a configuration mismatch to look at, not a failed action. */
  .coverage-warning {
    grid-column: 1 / -1;
    margin: 6px 0 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--panel);
    border-left: 3px solid var(--log);
    color: var(--text-muted);
    font-size: 12px;
    line-height: 1.45;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .empty.small {
    padding: 4px 0;
    font-size: 12px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  /* Every column sorts and filters (#649): a sort toolbar standing in
     for table column heads (the entries are cards, not a table), and
     beneath it the quiet dashed filter row round-18/19 ratified -- same
     idiom as docs/design/concepts/round-18/the-docket-opened.html's
     `.panel tr.filters input`, translated onto a card list. */
  .sortbar {
    display: flex;
    gap: 20px;
    padding: 2px 4px 4px;
  }

  .sorth {
    background: transparent;
    border: none;
    padding: 0;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--fg-dim);
    cursor: pointer;
  }

  .sorth:hover {
    color: var(--fg-muted);
  }

  .sorth.on {
    color: var(--fg);
  }

  .sorth .dir {
    display: inline-block;
    min-width: 8px;
    color: var(--accent);
    font-size: 9px;
  }

  .filterbar {
    display: flex;
    gap: 20px;
    padding: 0 4px 10px;
  }

  .filterbar input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-muted);
    padding: 2px 0;
    outline: none;
  }

  .filterbar input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  .filterbar input:focus {
    border-bottom-color: var(--accent);
  }

  .card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px 10px 14px;
  }

  /* The stripe (round 19): one unbroken line at the card's left edge,
     inset so it follows the rounded corner. */
  .card.watching {
    box-shadow: inset 3px 0 0 var(--marked);
  }

  .card.ring-broken {
    box-shadow: inset 3px 0 0 var(--alarm);
  }

  .card-main {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
    flex: 1 1 auto;
    background: transparent;
    border: none;
    text-align: left;
    padding: 0;
  }

  .name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .badge {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .badge.invert {
    background: var(--accent-bg);
    color: var(--accent);
  }

  .badge.observing {
    background: var(--drop-bg);
    color: var(--drop);
  }

  .source {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
  }

  .detail {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .row-actions {
    display: flex;
    gap: 8px;
    flex: none;
  }

  .cancel,
  .save,
  .edit,
  .delete,
  .observe-toggle,
  .promote,
  .load-matches {
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .cancel,
  .edit,
  .observe-toggle,
  .load-matches {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover,
  .edit:hover,
  .observe-toggle:hover:not(:disabled),
  .load-matches:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .save,
  .promote {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .promote {
    padding: 4px 9px;
    font-size: 11px;
  }

  .delete {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .delete:hover:not(:disabled) {
    background: var(--drop-bg);
    color: var(--drop);
    border-color: var(--drop);
  }

  button:disabled {
    opacity: 0.6;
  }

  .expanded {
    flex-basis: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding-top: 10px;
    margin-top: 4px;
    border-top: 1px solid var(--border);
  }

  .expanded-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .hint {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .sub-section h4 {
    margin: 0 0 4px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .dest-list,
  .match-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .dest-list li,
  .match-list li {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    padding: 4px 0;
  }

  .dest {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .dest-meta {
    color: var(--fg-dim);
    flex: 1 1 auto;
  }

  /* The ratified table (#676, round 29): watch · boundary · window ·
     state · last event. The drawer reuses Flags.svelte's own class
     names for the parts they share (.dwr/.dcol/.side/.lab/.lines/
     .dwr-acts/.act) -- scoped separately per component the same way
     .card/.sortbar/.filterbar already are across this file and that
     one -- plus .story for the standalone headline the round-29 record
     adds, which Flags' own drawer doesn't carry. */
  .watch-table-section {
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }

  .manage-heading {
    margin-top: 2px;
  }

  .watch-table {
    border-collapse: collapse;
    width: 100%;
    font-family: var(--font-mono);
    font-size: 12px;
  }

  .watch-table th,
  .watch-table td {
    padding: 8px 12px;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  .watch-table thead th {
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  /* Round 30's own idiom (the-whole.html #s7 `.panel thead th`): the
     column head itself is the sort control -- click it, again to
     reverse -- rather than a separate toolbar of buttons floating above
     a plain, non-interactive <th>. `all: unset` inherits color/font from
     the <th> around it (both are inherited properties), so the button
     reads identically to plain header text until it is hovered, active,
     or focused. */
  .watch-table thead th .th-sort {
    all: unset;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
  }

  .watch-table thead th .th-sort:hover {
    color: var(--fg-muted);
  }

  .watch-table thead th .th-sort.on {
    color: var(--fg);
  }

  .watch-table thead th .th-sort .dir {
    display: inline-block;
    min-width: 8px;
    color: var(--accent);
    font-size: 9px;
  }

  /* The filter row (round 30, the-whole.html #s7): a second <tr> inside
     the same <thead>, directly beneath the column heads -- one header
     row, one filter row, not a standalone filter block ahead of the
     table's own heading. Same quiet-dashed-input idiom as .filterbar
     below, scoped to this table instead. */
  .watch-table thead tr.filters td {
    padding: 2px 12px 8px;
    border-bottom: 1px solid var(--border);
  }

  .watch-table thead tr.filters input {
    width: 100%;
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-muted);
    padding: 2px 0;
    outline: none;
  }

  .watch-table thead tr.filters input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  .watch-table thead tr.filters input:focus {
    border-bottom-color: var(--accent);
  }

  .watch-table td.k {
    color: var(--fg);
  }

  .watch-table td.t {
    color: var(--fg-dim);
  }

  .watch-table td.empty-row {
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 13px;
    border-bottom: none;
  }

  .wt-row {
    cursor: pointer;
  }

  .wt-row:hover td {
    background: var(--bg-hover);
  }

  /* Same stripe idiom the "Manage entries" card list gives its own
     rows below (.card.watching/.card.ring-broken): the watchers'
     purple for a healthy watch, the alarm ink where the ring is
     broken, nothing for a paused one. */
  .wt-row.watching td:first-child {
    box-shadow: inset 3px 0 0 var(--marked);
  }

  .wt-row.ring-broken td:first-child {
    box-shadow: inset 3px 0 0 var(--alarm);
  }

  .wt-drawer td {
    padding: 0 12px 14px;
  }

  .wt-drawer.ring-broken td {
    box-shadow: inset 3px 0 0 var(--alarm);
  }

  .wchip2 {
    font: 600 10.5px var(--font-mono);
    color: var(--marked);
    border: 1px solid color-mix(in srgb, var(--marked) 40%, transparent);
    background: color-mix(in srgb, var(--marked) 10%, transparent);
    border-radius: 9px;
    padding: 1px 10px;
    white-space: nowrap;
  }

  .wchip2.broken {
    color: var(--alarm);
    border-color: color-mix(in srgb, var(--alarm) 45%, transparent);
    background: color-mix(in srgb, var(--alarm) 10%, transparent);
  }

  .openc {
    background: transparent;
    border: none;
    color: var(--accent);
    font-size: 13px;
    padding: 4px 8px;
    cursor: pointer;
    transition: transform 0.2s;
  }

  .openc[aria-expanded='true'] {
    transform: rotate(90deg);
  }

  /* The drawer (round 29, same grammar as Flags.svelte's): the story
     and the verbatim last-matching line on the left, one labelled
     detail panel on the right (the pathway, in place of the seven-night
     strip the record shows -- see the script's own comment for why),
     actions across the foot. */
  .dwr {
    display: grid;
    grid-template-columns: 1.3fr 1fr;
    gap: 10px 32px;
    padding: 10px 0 4px;
  }

  .dwr .dcol {
    grid-column: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .dwr .story {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--fg-muted);
    line-height: 1.55;
  }

  .dwr .story b {
    color: var(--fg);
    font-weight: 600;
  }

  .dwr .lines {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .dwr .side {
    grid-column: 2;
    grid-row: 1 / span 2;
    min-width: 0;
  }

  .dwr .side .lab {
    display: block;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .dwr .side .ep-note {
    margin: 6px 0 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .dwr .dwr-acts {
    grid-column: 1 / -1;
    display: flex;
    gap: 10px;
    margin-top: 4px;
  }

  .dwr .act {
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 16px;
    cursor: pointer;
  }

  .dwr .act:hover {
    border-color: var(--accent);
  }

  .dwr .act.quiet {
    color: var(--fg-dim);
  }

  .dwr .act:disabled {
    opacity: 0.6;
    cursor: default;
  }

  @media (prefers-reduced-motion: reduce) {
    .openc {
      transition: none;
    }
  }
</style>
