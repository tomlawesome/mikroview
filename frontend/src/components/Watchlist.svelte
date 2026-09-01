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
  // The "Watchlist" tab panel below is the ratified round-29/31 docket
  // table (watch · boundary · window · state · last event, drawers with
  // a story/verbatim line/actions) -- and, since #761, the surface that
  // creates, edits, removes, permits and fences a watch too. The
  // separate "Manage entries" card list and its own add/edit form
  // (#243, #649) pre-dated that table and were retired wholesale once
  // every capability they carried was reachable from the drawer instead
  // (AGENTS.md's "removals are wholesale" -- no flag, no dead code kept
  // reachable behind one, since nothing here still needs it).
  import { onMount, tick } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { suggestState } from '../lib/suggest.svelte'
  import { matchesState } from '../lib/matches.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { compareNumeric, compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import { formatRelative } from '../lib/format'
  import { nightlySummary, windowLabel } from '../lib/watchWindow'
  import { topologyNavState, type PendingWatchDraft } from '../lib/topologyNav.svelte'
  import TabList from './TabList.svelte'
  import Suggestions from './Suggestions.svelte'
  import MatchesTab from './MatchesTab.svelte'
  import type { WatchlistEntry, WatchlistIdentity, WatchlistMatch, WatchlistPermittedDest } from '../lib/types'
  import type { WatchlistEntryRequest } from '../lib/api'

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
  // Watchlist tab, that entry's drawer open on the ratified table,
  // scrolled to. The scroll is deliberate -- the table can be long, and
  // switching tabs to a row that is open somewhere off-screen looks like
  // nothing happened. Targets the ratified table's own row (#761
  // retired the second "Entries" card list this used to point at, see
  // watchDrawerId below) rather than a dead anchor.
  async function openEntry(entryId: string) {
    selectTab('watchlist')
    watchDrawerId = entryId
    await tick()
    // Optional-call rather than assumed: jsdom has no layout, so
    // scrollIntoView is not implemented there.
    document.getElementById(`watch-${entryId}`)?.scrollIntoView?.({ block: 'center' })
  }

  function sourceLabel(e: WatchlistEntry): string {
    if (e.source?.mac) return e.source.mac
    if (e.source?.ip) return e.source.ip
    return 'any source'
  }

  function detailLabel(e: WatchlistEntry): string {
    return e.invert
      ? `${(e.permitted ?? []).length} permitted, ${(e.observed ?? []).length} to review`
      : `ports ${(e.ports ?? []).join(', ')}${e.destIp ? ` → ${e.destIp}` : ''}`
  }

  // --- The ratified table (#676/#761, round 29/31's "watch · boundary ·
  // window · state · last event") ---------------------------------------
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
  // #761 adds two states an inverted entry passes through that #680
  // never had to draw: learning (still Observing -- the dashed "◌"
  // chip) and fencing (Observing turned off -- back to "◉", but worded
  // by what it fences rather than a bare "watching", so the row still
  // says how many places it trusts). Placed either side of the recorded
  // ring: an inverted entry can carry a Window too, so a fenced one that
  // genuinely goes quiet inside it still reads "ring broken", the same
  // as any other watch.
  function watchState(e: WatchlistEntry): { glyph: string; text: string; broken: boolean; learning: boolean } {
    if (!e.enabled) return { glyph: '○', text: 'paused', broken: false, learning: false }
    if (watchlistState.coverage[e.id] === 'no-logging') {
      return { glyph: '○', text: 'ring broken — no logging visible', broken: true, learning: false }
    }
    if (e.invert && e.observing) {
      // "places seen" is a running total of everywhere this device has
      // ever reached, decided or not -- Entry.Promote (internal/watchlist/
      // invert.go) moves a destination out of Observed once permitted, so
      // counting Observed alone would shrink "seen" every time something
      // is promoted, reading as though mikroview had forgotten it.
      const permitted = (e.permitted ?? []).length
      const seen = (e.observed ?? []).length + permitted
      return { glyph: '◌', text: `learning — ${seen} places seen · ${permitted} permitted`, broken: false, learning: true }
    }
    if (e.ring?.broken) {
      return { glyph: '○', text: 'ring broken — nothing in the window', broken: true, learning: false }
    }
    if (e.invert) {
      const n = (e.permitted ?? []).length
      return { glyph: '◉', text: `fencing — ${n} ${n === 1 ? 'place' : 'places'} permitted`, broken: false, learning: false }
    }
    return { glyph: '◉', text: 'watching', broken: false, learning: false }
  }

  // While an inverted entry is still learning, `toward` greys out to
  // "wherever it goes" -- there is nothing scoped yet to name (#761 item
  // 2/4). Once fenced, it names what it actually trusts rather than a
  // generic "its observed destinations".
  function boundaryLabel(e: WatchlistEntry): string {
    if (e.invert) {
      if (e.observing) return `${sourceLabel(e)} → wherever it goes`
      const n = (e.permitted ?? []).length
      return `${sourceLabel(e)} → ${n} permitted ${n === 1 ? 'place' : 'places'}`
    }
    const dest = e.destIp ? e.destIp : 'any destination'
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
    if (e.invert && e.observing) {
      // Same running total as watchState's own chip -- see its comment.
      const seen = (e.observed ?? []).length + (e.permitted ?? []).length
      return {
        headline: 'Learning, not fencing yet.',
        body: `${sourceLabel(e)} has reached ${seen} ${seen === 1 ? 'place' : 'places'} so far. Nothing fires while it learns -- permit the ones you recognise, then fence it.`,
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
    if (e.invert) {
      const n = (e.permitted ?? []).length
      return {
        headline: 'Fenced.',
        body: `${sourceLabel(e)} may reach its ${n} permitted ${n === 1 ? 'place' : 'places'}. Anything else it reaches from now on is a flag; permit it here and the flag clears.`,
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

  // #724's second click: a dial panel row's own destination, not just the
  // right tab. Consumed (cleared) the instant it's read -- same idiom as
  // topologyNavState.pendingDescend's own consumer in Topography.svelte --
  // so a later manual visit to this tab doesn't silently reopen a stale
  // drawer. Matched against watchlistState.entries (every entry, not
  // sortedWatchRows) so a filter box left over from an earlier visit
  // can't hide the very row the dial just promised to open; a watch
  // removed between the click and landing here has nothing to match, so
  // nothing opens -- never an error, never a blank drawer.
  $effect(() => {
    const id = topologyNavState.pendingWatchId
    if (id === null) return
    topologyNavState.pendingWatchId = null
    const exists = watchlistState.entries.some((e) => e.id === id)
    watchDrawerId = exists ? id : null
  })

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

  // --- The draft, edit and remove gestures (#761) -----------------------
  //
  // One editing surface for the docket, per the ratified round-31
  // record: a watch is written, mended and removed in the row it lives
  // in, never in a second panel. `+ watch` (Docket.svelte) and a flag's
  // own `watch this pathway`/`watch this source` (Flags.svelte) both
  // reach this through the shared handoff in topologyNav.svelte.ts,
  // since neither lives inside this component.

  const MAC_RE = /^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$/

  // Best-effort "who" resolution against hosts mikroview has actually
  // observed (zonesState, #485/#627): a MAC is used as typed, a name
  // matching an observed host's label resolves to that host's IP, and
  // anything else is sent through as a literal IP. Never invented -- a
  // typo or an unrecognised name is simply stored as the literal text
  // the operator typed, the same as if this lookup didn't exist. The
  // backend's own validation (a source is required for a fenced entry,
  // at least one port for an expected one) is what surfaces a genuinely
  // empty or unusable value, not a guess made here.
  function resolveIdentity(raw: string): WatchlistIdentity | undefined {
    const v = raw.trim()
    if (!v) return undefined
    if (MAC_RE.test(v)) return { mac: v }
    const known = zonesState.zones.flatMap((z) => z.hosts).find((h) => h.label.toLowerCase() === v.toLowerCase())
    return { ip: known ? known.ip : v }
  }

  // "toward" is one compound field in the draft/edit form, the record's
  // own "nas · :445, :139": a host before the "·", its ports after it.
  // Mirrors resolveIdentity's own name lookup for the host half.
  function parseToward(raw: string): { destIp?: string; ports: number[] } {
    const v = raw.trim()
    if (!v) return { ports: [] }
    const [hostPart, portsPart] = v.split('·')
    const host = hostPart.trim()
    const known = zonesState.zones.flatMap((z) => z.hosts).find((h) => h.label.toLowerCase() === host.toLowerCase())
    const destIp = host ? (known ? known.ip : host) : undefined
    const ports = Array.from((portsPart ?? '').matchAll(/(\d+)/g)).map((m) => Number(m[1]))
    return { destIp, ports }
  }

  function towardLabel(destIp: string | undefined, ports: number[] | undefined): string {
    if (!destIp) return ''
    const p = (ports ?? []).map((n) => `:${n}`).join(', ')
    return p ? `${destIp} · ${p}` : destIp
  }

  // The draft (item 2): a row at the top of the table, open as a
  // drawer, gone the instant it starts watching or is discarded.
  type DraftMode = 'expect' | 'fence'

  let draftOpen = $state(false)
  let draftName = $state('')
  let draftWho = $state('')
  let draftToward = $state('')
  let draftMode = $state<DraftMode>('expect')
  let draftIncludeStructuralNoise = $state(false)
  let draftSaving = $state(false)
  let draftError = $state<string | null>(null)

  // The row's cells fill in as the form is typed, so the watch is read
  // the way it will be listed before it exists.
  const draftPreviewName = $derived(
    draftName.trim() || (draftWho.trim() ? `${draftWho.trim()}${draftMode === 'fence' ? ' fenced' : ''}` : 'new watch'),
  )
  const draftPreviewBoundary = $derived.by(() => {
    const who = draftWho.trim()
    if (!who) return '—'
    if (draftMode === 'fence') return `${who} → wherever it goes`
    const toward = draftToward.trim()
    return toward ? `${who} → ${toward}` : who
  })

  function openDraft(fill?: PendingWatchDraft) {
    draftName = ''
    draftWho = fill?.who ?? ''
    draftToward = fill?.toward ?? ''
    draftMode = fill?.mode ?? 'expect'
    draftIncludeStructuralNoise = false
    draftError = null
    draftOpen = true
  }

  function discardDraft() {
    draftOpen = false
  }

  async function startWatching() {
    draftError = null
    const source = resolveIdentity(draftWho)
    const { destIp, ports } = draftMode === 'expect' ? parseToward(draftToward) : { destIp: undefined, ports: [] }
    const req: WatchlistEntryRequest = {
      name: draftName.trim() || undefined,
      invert: draftMode === 'fence',
      source,
      destIp: draftMode === 'expect' ? destIp : undefined,
      ports: draftMode === 'expect' ? ports : undefined,
      includeStructuralNoise: draftMode === 'fence' ? draftIncludeStructuralNoise : undefined,
    }
    draftSaving = true
    try {
      // The server is the validator (unscoped ports on an `expect` watch,
      // a missing source on a `fence` one) -- its message is shown as-is,
      // the same house style every other mutation on this page uses.
      const err = await watchlistState.create(req)
      if (err) draftError = err
      else draftOpen = false
    } finally {
      draftSaving = false
    }
  }

  // The docket's `+ watch` (Docket.svelte) and a flag's `watch this
  // pathway`/`watch this source` (Flags.svelte) both live outside this
  // component -- see topologyNav.svelte.ts's own doc comment for why the
  // handoff is shaped the way it is, including why both slots are
  // cleared the instant they're read (these tabs stay mounted once
  // visited, so a slot left non-null would reopen the draft on a later,
  // unrelated visit).
  $effect(() => {
    if (topologyNavState.pendingNewWatch === null) return
    topologyNavState.pendingNewWatch = null
    openDraft()
  })

  $effect(() => {
    const fill = topologyNavState.pendingWatchDraft
    if (fill === null) return
    topologyNavState.pendingWatchDraft = null
    openDraft(fill)
  })

  // Edit (item 5): the same drawer, its story swapped for the form,
  // pre-filled. "Mend" (item 6) would be edit with a suggested fix
  // already typed in, but the definitions API has no way to set or
  // change an entry's window at all -- internal/api/definitions.go's
  // expectationRequest (the wire shape both create and update take)
  // carries source/destIp/ports/invert/includeStructuralNoise only,
  // never Window, so there is no window fix this page could ever save.
  // Per the issue's own fallback for exactly this case ("if the app has
  // no suggestion to make, the button is plain edit; do not invent
  // one"), plain edit is what's offered here, and its own window row is
  // read-only text rather than a control that would silently fail to
  // persist what it shows.
  let editingId = $state<string | null>(null)
  let editName = $state('')
  let editWho = $state('')
  let editToward = $state('')
  let editMode = $state<DraftMode>('expect')
  let editIncludeStructuralNoise = $state(false)
  let editSaving = $state(false)

  function startEditWatch(e: WatchlistEntry) {
    editingId = e.id
    editName = e.name ?? ''
    editWho = e.source?.mac || e.source?.ip || ''
    editToward = towardLabel(e.destIp, e.ports)
    editMode = e.invert ? 'fence' : 'expect'
    editIncludeStructuralNoise = !!e.includeStructuralNoise
    wtError = null
  }

  function cancelEditWatch() {
    editingId = null
  }

  async function saveEditWatch() {
    if (!editingId) return
    const source = resolveIdentity(editWho)
    const { destIp, ports } = editMode === 'expect' ? parseToward(editToward) : { destIp: undefined, ports: [] }
    const req: WatchlistEntryRequest = {
      name: editName.trim() || undefined,
      invert: editMode === 'fence',
      source,
      destIp: editMode === 'expect' ? destIp : undefined,
      ports: editMode === 'expect' ? ports : undefined,
      includeStructuralNoise: editMode === 'fence' ? editIncludeStructuralNoise : undefined,
    }
    editSaving = true
    try {
      const err = await watchlistState.update(editingId, req)
      if (err) wtError = err
      else editingId = null
    } finally {
      editSaving = false
    }
  }

  // Remove (item 5): round 28's clear-all gesture -- one click arms it
  // red as "confirm", a second removes, any other click disarms.
  let armedRemoveId = $state<string | null>(null)

  function armOrRemoveWatch(e: WatchlistEntry) {
    if (armedRemoveId !== e.id) {
      armedRemoveId = e.id
      return
    }
    armedRemoveId = null
    watchlistState.remove(e.id).then((err) => {
      if (err) wtError = err
    })
  }

  function disarmRemoveWatch() {
    armedRemoveId = null
  }

  // Permit, permit all, and fence now / learn again (item 4).
  let permittingKey = $state<string | null>(null)
  let fencingId = $state<string | null>(null)

  async function permitOne(e: WatchlistEntry, d: WatchlistPermittedDest) {
    permittingKey = e.id + d.destIp + d.port
    try {
      const err = await watchlistState.promote(e.id, [d])
      if (err) wtError = err
    } finally {
      permittingKey = null
    }
  }

  async function permitAll(e: WatchlistEntry) {
    const dests = (e.observed ?? []).map((o) => ({ destIp: o.destIp, port: o.port }))
    if (dests.length === 0) return
    permittingKey = e.id + '*'
    try {
      const err = await watchlistState.promote(e.id, dests)
      if (err) wtError = err
    } finally {
      permittingKey = null
    }
  }

  // "fence now" / "learn again": the observe-only toggle (#243's
  // Observing), named and worded here for what it does rather than a
  // bare observe/enforce flip.
  async function toggleObserving(e: WatchlistEntry) {
    fencingId = e.id
    try {
      const err = await watchlistState.setObserving(e.id, !e.observing)
      if (err) wtError = err
    } finally {
      fencingId = null
    }
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
    learning: boolean
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
        learning: st.learning,
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
  // docket-watchlist.png, the-whole.html #s7's `#p-watch` panel, carried
  // forward verbatim by round 31's watchlist-managed.html) draws one
  // flat watch table under the docket's tabs -- watch / boundary /
  // window / state / last event, one header row, one filter row
  // directly beneath it, nothing else. What it does not draw -- the
  // Watchlist/Matches/Suggestions sub-tab row and the "Watches" page
  // heading (round 30: "no page heading and no strap, anywhere" --
  // owner, 2026-08-31) -- is real, shipped capability (#547, #584) the
  // ratified design simply doesn't describe, and stays unmounted behind
  // its own typed flag rather than deleted, same pattern as LiveTable's
  // RESIZE_HANDLES_ENABLED. Bringing either back is tracked on #691.
  //
  // The second surface these flags used to guard -- "Manage entries",
  // the add/edit form and the "Entries" card list (#243, #649) -- is
  // gone outright rather than joining this list: #761 made every
  // capability it carried reachable from the ratified table's own
  // drawer, and AGENTS.md's "removals are wholesale" applies once
  // nothing still needs the old surface to reach it.
  const WATCHLIST_SUBTABS_ENABLED: boolean = false
  const WATCH_HEADING_ENABLED: boolean = false
</script>

<!-- "remove" (#761 item 5, round 28's clear-all gesture): one click arms
     it, a second removes, any other click disarms -- same idiom as
     Docket.svelte's own clear-all bubble. -->
<svelte:window onclick={() => (armedRemoveId = null)} />

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
  <!-- The ratified table (#676/#761, round 29/31's docket scene: watch ·
       boundary · window · state · last event, rows opening as drawers
       like the flags tab -- and, since #761, the table's own draft row
       and every drawer's edit/remove/permit/fence actions). See the
       script's own section comment for what "window" and the
       seven-night strip could not honestly carry, and why.

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
    {#if watchlistState.entries.length === 0 && !draftOpen}
      <p class="empty">No watches yet -- add one with the button above.</p>
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
          <!-- THE DRAFT (#761 item 2): a row at the top of the table, open
               as a drawer, gone the moment it starts watching or is
               discarded. `+ watch` (Docket.svelte) makes it blank; a
               flag's `watch this pathway`/`watch this source` (Flags.svelte)
               make it filled in -- see the shared handoff in
               topologyNav.svelte.ts. -->
          {#if draftOpen}
            <tr class="wt-row wt-draft">
              <td class="k">{draftPreviewName}</td>
              <td>{draftPreviewBoundary}</td>
              <td class="t">always</td>
              <td><span class="wchip2 draft">✎ not watching yet</span></td>
              <td class="t">—</td>
              <td></td>
            </tr>
            <tr class="wt-drawer wt-draft">
              <td colspan="6">
                <div class="dwr">
                  <div class="dcol wform">
                    <label class="wf-field"
                      ><span class="lab">watch</span><input
                        bind:value={draftName}
                        placeholder="a name — or leave it, and it takes its boundary's"
                        aria-label="Watch name"
                      /></label
                    >
                    <label class="wf-field"
                      ><span class="lab">who</span><input
                        bind:value={draftWho}
                        placeholder="a host — its name, MAC or address"
                        aria-label="Who this watch scopes to"
                      /></label
                    >
                    <label class="wf-field"
                      ><span class="lab">toward</span><input
                        bind:value={draftToward}
                        disabled={draftMode === 'fence'}
                        placeholder={draftMode === 'fence'
                          ? 'wherever it goes — it learns, you permit'
                          : 'a host, and ports — nas · :445, :139'}
                        aria-label="Toward"
                      /></label
                    >
                    <!-- The window control the record draws (always/between,
                         days) is not offered here: the definitions API has
                         no field to carry a window on create or update
                         (internal/api/definitions.go's expectationRequest),
                         so a "between" choice could never actually be
                         saved -- offering it would silently lose what the
                         operator set. See startWatching's own comment. -->
                    <div class="wf-row"><span class="lab">window</span><span class="t">always</span></div>
                    {#if draftError}<p class="error" role="alert">{draftError}</p>{/if}
                  </div>
                  <div class="side wf-mode">
                    <span class="lab">and it means</span>
                    <button type="button" class="mode" class:on={draftMode === 'expect'} onclick={() => (draftMode = 'expect')}>
                      <b>expect it</b><i>this pathway should happen. The ring breaks when a kept window passes and it does not.</i>
                    </button>
                    <button type="button" class="mode" class:on={draftMode === 'fence'} onclick={() => (draftMode = 'fence')}>
                      <b>fence it</b><i
                        >this host may go only where you permit. It learns first — every place it reaches is listed,
                        nothing fires — and the fence starts when you say so.</i
                      >
                      {#if draftMode === 'fence'}
                        <label class="sub"
                          ><input type="checkbox" bind:checked={draftIncludeStructuralNoise} /> count broadcast, multicast
                          and link-local too</label
                        >
                      {/if}
                    </button>
                  </div>
                  <div class="dwr-acts">
                    <button class="act" disabled={draftSaving} onclick={startWatching}>
                      {draftSaving ? 'Saving…' : 'start watching'}
                    </button>
                    <button class="act quiet" onclick={discardDraft}>discard</button>
                  </div>
                </div>
              </td>
            </tr>
          {/if}
          {#if sortedWatchRows.length === 0}
            <tr>
              <td class="empty-row" colspan="6">
                {watchlistState.entries.length === 0 ? 'No watches yet.' : 'No watches match these filters.'}
              </td>
            </tr>
          {:else}
            {#each sortedWatchRows as row (row.entry.id)}
              {@const story = watchStory(row.entry, row.lastMatch)}
              {@const nights = nightlySummary(row.entry.nights)}
              <!-- Every entry in Observed is, by construction, not yet
                   decided: Entry.Promote (internal/watchlist/invert.go)
                   removes a destination from Observed the moment it is
                   permitted, so there is no "observed but already
                   permitted" case to filter out here. -->
              {@const unpermitted = row.entry.observed ?? []}
              <tr
                id="watch-{row.entry.id}"
                class="wt-row"
                class:watching={!row.broken && row.entry.enabled}
                class:ring-broken={row.broken}
                class:learning={row.learning}
                onclick={() => toggleWatchDrawer(row.entry.id)}
              >
                <td class="k">{row.entry.name || '(unnamed)'}</td>
                <td>{row.boundary}</td>
                <td class="t">{row.window}</td>
                <td><span class="wchip2" class:broken={row.broken} class:learn={row.learning}>{row.stateGlyph} {row.stateText}</span></td>
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
                      {#if editingId === row.entry.id}
                        <!-- EDIT (#761 item 5): the same drawer, its story
                             swapped for the form, pre-filled. "Mend" (item
                             6) is not offered -- see startEditWatch's own
                             comment for why there is no window fix this
                             page could ever save. -->
                        <div class="dcol wform">
                          <label class="wf-field"><span class="lab">watch</span><input bind:value={editName} aria-label="Watch name" /></label>
                          <label class="wf-field"><span class="lab">who</span><input bind:value={editWho} aria-label="Who this watch scopes to" /></label>
                          <label class="wf-field"
                            ><span class="lab">toward</span><input
                              bind:value={editToward}
                              disabled={editMode === 'fence'}
                              placeholder={editMode === 'fence' ? 'wherever it goes — it learns, you permit' : ''}
                              aria-label="Toward"
                            /></label
                          >
                          <div class="wf-row"><span class="lab">window</span><span class="t">{row.window}</span></div>
                          {#if wtError}<p class="error" role="alert">{wtError}</p>{/if}
                        </div>
                        <div class="side wf-mode">
                          <span class="lab">and it means</span>
                          <button type="button" class="mode" class:on={editMode === 'expect'} onclick={() => (editMode = 'expect')}>
                            <b>expect it</b>
                          </button>
                          <button type="button" class="mode" class:on={editMode === 'fence'} onclick={() => (editMode = 'fence')}>
                            <b>fence it</b>
                            {#if editMode === 'fence'}
                              <label class="sub"
                                ><input type="checkbox" bind:checked={editIncludeStructuralNoise} /> count broadcast,
                                multicast and link-local too</label
                              >
                            {/if}
                          </button>
                        </div>
                        <div class="dwr-acts">
                          <button class="act" disabled={editSaving} onclick={saveEditWatch}>
                            {editSaving ? 'Saving…' : 'save'}
                          </button>
                          <button class="act quiet" onclick={cancelEditWatch}>cancel</button>
                        </div>
                      {:else}
                        <div class="dcol">
                          <p class="story"><b>{story.headline}</b> {story.body}</p>
                          <div class="lines">{row.lastMatch ? row.lastMatch.event.raw : 'No matching line in the recent log.'}</div>
                        </div>
                        <div class="side">
                          {#if row.learning}
                            <!-- THE LEARNING WATCH (#761 item 4): where it
                                 has reached -- every place ever seen,
                                 decided or not. Entry.Promote
                                 (internal/watchlist/invert.go) moves a
                                 destination out of Observed once
                                 permitted, so the permitted half of this
                                 list is read from Permitted directly
                                 rather than from an "observed AND
                                 permitted" combination that never
                                 actually occurs. -->
                            <span class="lab">where it has reached</span>
                            <ul class="seen">
                              {#each row.entry.permitted ?? [] as p (p.destIp + ':' + p.port)}
                                <li>
                                  <span class="k">{p.destIp}</span>
                                  <span class="t">:{p.port}</span>
                                  <span class="ok">✓ permitted</span>
                                </li>
                              {/each}
                              {#each row.entry.observed ?? [] as o (o.destIp + ':' + o.port)}
                                <li>
                                  <span class="k">{o.destIp}</span>
                                  <span class="t">:{o.port} · {o.count}×</span>
                                  <button
                                    class="permit"
                                    disabled={permittingKey === row.entry.id + o.destIp + o.port}
                                    onclick={() => permitOne(row.entry, { destIp: o.destIp, port: o.port })}
                                  >
                                    permit
                                  </button>
                                </li>
                              {/each}
                            </ul>
                          {:else}
                            <span class="lab">the pathway</span>
                            <p class="ep-note">{detailLabel(row.entry)}</p>
                            {#if nights}
                              <span class="lab">the last seven nights</span>
                              <p class="ep-note">{nights}</p>
                            {/if}
                          {/if}
                        </div>
                        <div class="dwr-acts">
                          {#if row.entry.invert}
                            {#if row.entry.observing && unpermitted.length > 0}
                              <button class="act" disabled={permittingKey === row.entry.id + '*'} onclick={() => permitAll(row.entry)}>
                                permit all {unpermitted.length}
                              </button>
                            {/if}
                            <button class="act" disabled={fencingId === row.entry.id} onclick={() => toggleObserving(row.entry)}>
                              {fencingId === row.entry.id
                                ? 'Saving…'
                                : row.entry.observing
                                  ? `fence now · ${(row.entry.permitted ?? []).length} permitted`
                                  : 'learn again'}
                            </button>
                          {:else}
                            <button class="act" disabled={pausingId === row.entry.id} onclick={() => togglePause(row.entry)}>
                              {pausingId === row.entry.id ? 'Saving…' : row.entry.enabled ? 'pause watch' : 'resume watch'}
                            </button>
                          {/if}
                          {#if row.entry.source?.mac || row.entry.source?.ip}
                            <button class="act quiet" onclick={() => openWatchInStream(row.entry)}>open in stream ▸</button>
                          {/if}
                          <button class="act quiet edit" onclick={() => startEditWatch(row.entry)}>edit</button>
                          <button
                            class="act quiet remove"
                            class:armed={armedRemoveId === row.entry.id}
                            onclick={(ev) => {
                              ev.stopPropagation()
                              armOrRemoveWatch(row.entry)
                            }}
                          >
                            {armedRemoveId === row.entry.id ? 'confirm — its matches stay' : 'remove'}
                          </button>
                        </div>
                      {/if}
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

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
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

  button:disabled {
    opacity: 0.6;
  }

  /* The ratified table (#676/#761, round 29/31): watch · boundary ·
     window · state · last event. The drawer reuses Flags.svelte's own
     class names for the parts they share (.dwr/.dcol/.side/.lab/.lines/
     .dwr-acts/.act) -- scoped separately per component the same way
     .section/.watch-table-section already are across this file and
     that one -- plus .story for the standalone headline the round-29
     record adds, which Flags' own drawer doesn't carry. */
  .watch-table-section {
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
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

  /* THE DRAFT, and the same stripe/state idioms carried onto the two
     more states an inverted entry passes through (#761): learning
     (dashed, still the watchers' purple) and the draft itself (dashed,
     dim -- "not watching yet" is not a state to alarm on). */
  .wt-row.learning td:first-child {
    box-shadow: inset 3px 0 0 color-mix(in srgb, var(--marked) 50%, transparent);
  }

  .wt-row.wt-draft td:first-child,
  .wt-drawer.wt-draft td {
    box-shadow: inset 3px 0 0 var(--accent);
  }

  .wchip2.learn {
    border-style: dashed;
  }

  .wchip2.draft {
    color: var(--fg-dim);
    border-style: dashed;
    border-color: var(--border);
    background: transparent;
  }

  /* The form -- the drawer's story column, as fields, shared by the
     draft and by edit. */
  .wform {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .wf-field {
    display: grid;
    grid-template-columns: 64px 1fr;
    align-items: baseline;
    gap: 12px;
  }

  .wf-field input {
    width: 100%;
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 3px 0;
    outline: none;
  }

  .wf-field input:focus {
    border-bottom-color: var(--accent);
  }

  .wf-field input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  .wf-field input:disabled {
    color: var(--fg-dim);
    border-bottom-style: dotted;
  }

  .wf-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .wf-row .lab {
    width: 64px;
    flex: none;
  }

  .wform .lab {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  /* "and it means": expect it / fence it, the record's own two cards. */
  .wf-mode .mode {
    display: block;
    width: 100%;
    text-align: left;
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 8px 12px;
    margin-top: 6px;
    background: transparent;
    cursor: pointer;
  }

  .wf-mode .mode.on {
    border-color: color-mix(in srgb, var(--marked) 55%, transparent);
    background: color-mix(in srgb, var(--marked) 6%, transparent);
  }

  .wf-mode .mode b {
    display: block;
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 600;
    color: var(--fg);
  }

  .wf-mode .mode i {
    display: block;
    font-family: var(--font-sans);
    font-style: normal;
    font-size: 11px;
    color: var(--fg-dim);
    line-height: 1.5;
    margin-top: 2px;
  }

  .wf-mode .mode .sub {
    display: flex;
    align-items: center;
    gap: 6px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    margin-top: 6px;
  }

  /* "where it has reached" (#761 item 4): the learning watch's own
     per-place permit list. */
  .seen {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
  }

  .seen li {
    display: grid;
    grid-template-columns: 1fr auto 72px;
    align-items: center;
    gap: 12px;
    padding: 4px 0;
    border-bottom: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .seen .k {
    color: var(--fg);
  }

  .seen .t {
    color: var(--fg-dim);
  }

  .seen .ok {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: 10px;
    color: var(--marked);
    text-align: right;
  }

  .seen .permit {
    font-family: var(--font-sans);
    font-weight: 600;
    font-size: 10px;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 1px 10px;
    cursor: pointer;
    justify-self: end;
  }

  .seen .permit:hover {
    border-color: var(--accent);
  }

  /* "remove" (#761 item 5): armed red on the first click, same idiom as
     Docket.svelte's own clear-all bubble. */
  .dwr .act.remove.armed {
    color: var(--alarm);
    border-color: var(--alarm);
  }

  @media (prefers-reduced-motion: reduce) {
    .openc {
      transition: none;
    }
  }
</style>
