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
  import { formatHM, formatRelative } from '../lib/format'
  import { nightlySummary, windowLabel } from '../lib/watchWindow'
  import { topologyNavState, type PendingWatchDraft } from '../lib/topologyNav.svelte'
  import { fetchWatchlistMatches } from '../lib/api'
  import type {
    Suggestion,
    WatchlistEntry,
    WatchlistIdentity,
    WatchlistMatch,
    WatchlistPermittedDest,
  } from '../lib/types'
  import type { WatchlistEntryRequest } from '../lib/api'

  onMount(() => {
    watchlistState.refresh()
    suggestState.refresh()
    // The ratified table's "last event" column, and round 33's match
    // list in the drawer, both read matchesState's own bulk feed
    // (GET /api/matches?entries=all) -- one fetch for the whole table
    // rather than an N+1 per row. `older ▸` in a drawer pages further
    // back per entry; see loadOlderMatches.
    matchesState.load()
  })

  function sourceLabel(e: WatchlistEntry): string {
    if (e.source?.mac) return e.source.mac
    if (e.source?.ip) return e.source.ip
    return 'any source'
  }

  // "where it has reached · since Sunday" (watchlist-managed.html:775):
  // the day the entry started, in UTC per every other timestamp in this
  // codebase (types.ts's own comment on Window.zone). createdAt is the
  // one always-present timestamp an entry carries -- there is no
  // separate "this observation period began" field, so a fenced entry
  // sent back to `learn again` still reads its original creation day,
  // the same honest-but-imprecise trade-off the rest of this page makes
  // rather than inventing a field to be exact.
  const WEEKDAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  function sinceDay(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? '' : WEEKDAY_NAMES[d.getUTCDay()]
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

  // --- Round 33: what a watch matched, in the watch's own drawer -------
  //
  // Round 30 put one last-match line at the foot of the drawer's story
  // column. Round 33 grows that line into a short list -- the one
  // accepted thing that changes shape, and it changes by growing rather
  // than being redrawn (round-33/README.md). The verbatim `.lines`
  // element the watch drawer used to carry is gone: round 33's watch
  // drawers draw `.matches` in its place (the six `.lines` left in
  // suggestions-matches.html are all in the flags panel).
  const MATCH_LINES = 3
  // 20 per `older ▸`, as #771 specifies -- a drawer is a glance, not the
  // 100-row page the retired Matches tab walked back through.
  const OLDER_PAGE = 20

  // Older pages, per entry. Keyed by entry id and kept separate from
  // matchesState.records, which is the shared newest-100-network-wide
  // feed every row's "last event" reads: paging one drawer back in time
  // must not move another row's column.
  let olderMatches = $state<Record<string, WatchlistMatch[]>>({})
  let olderLoadingId = $state<string | null>(null)
  let olderExhausted = $state<Record<string, boolean>>({})
  let olderError = $state<Record<string, string>>({})

  // Every match this drawer can show, newest first: the shared feed's
  // records for this entry plus anything `older ▸` has pulled in,
  // de-duplicated by id because the two sources overlap by construction.
  function matchesFor(entryId: string): WatchlistMatch[] {
    const seen = new Set<string>()
    const out: WatchlistMatch[] = []
    for (const m of [...matchesState.records, ...(olderMatches[entryId] ?? [])]) {
      if (m.entryId !== entryId || seen.has(m.id)) continue
      seen.add(m.id)
      out.push(m)
    }
    out.sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())
    return out
  }

  // `older ▸` asks the per-device query for this watch's own source and
  // keeps the records whose entryId is this watch. The backend has no
  // per-entry filter (internal/api/matches.go's handleMatchesQuery takes
  // mac/ip or entries=all, never an entry id), so the filtering is the
  // client's -- exactly the gap round-33/README.md records against #691.
  //
  // An unscoped entry has no mac or ip to ask by, and fetchWatchlistMatches
  // requires one, so those drawers are not offered the control at all
  // rather than being offered one that can only 400.
  function canLoadOlder(e: WatchlistEntry): boolean {
    return !!(e.source?.mac || e.source?.ip) && !olderExhausted[e.id]
  }

  async function loadOlderMatches(e: WatchlistEntry) {
    if (olderLoadingId === e.id || !canLoadOlder(e)) return
    const shown = matchesFor(e.id)
    const oldest = shown[shown.length - 1]
    olderLoadingId = e.id
    olderError = { ...olderError, [e.id]: '' }
    try {
      const page = await fetchWatchlistMatches({
        mac: e.source?.mac,
        ip: e.source?.ip,
        // The cursor is the oldest record already shown. `until` filters
        // on firstSeen server-side, so this cannot skip a record; it can
        // repeat one, which matchesFor's own de-duplication absorbs (the
        // same argument matches.svelte.ts makes at length for the merged
        // feed's cursor).
        until: oldest?.lastSeen,
        limit: OLDER_PAGE,
      })
      const mine = page.filter((m) => m.entryId === e.id)
      const known = new Set(shown.map((m) => m.id))
      const fresh = mine.filter((m) => !known.has(m.id))
      olderMatches = { ...olderMatches, [e.id]: [...(olderMatches[e.id] ?? []), ...fresh] }
      // A page that adds nothing this entry has not already got is the
      // end of the walk. Reported as "nothing older" rather than left
      // offering a control that can no longer do anything.
      olderExhausted = { ...olderExhausted, [e.id]: fresh.length === 0 || page.length < OLDER_PAGE }
    } catch (err) {
      olderError = { ...olderError, [e.id]: err instanceof Error ? err.message : String(err) }
    } finally {
      olderLoadingId = null
    }
  }

  // `when · source → destination:port · n× · rule`, as drawn. The
  // identity is the matching event's own, never the entry's possibly
  // unscoped Source -- see matchlog.Tuple and the argument the retired
  // MatchesTab made for the same choice.
  function matchSource(m: WatchlistMatch): string {
    return m.event.srcHostName || m.tuple.source.mac || m.tuple.source.ip || 'unknown source'
  }

  function matchDest(m: WatchlistMatch): string {
    return m.event.dstHostName || m.tuple.destIp
  }

  // The `when` the drawing shows: a clock time for today, a weekday and
  // a clock time for the rest of the week, a date beyond that (02:14 /
  // Sat 02:13 / 23 Aug).
  //
  // Not formatRelative, which the "last event" column uses: that answers
  // "how long ago", and a list walking backwards through days needs to
  // say *which* day or three lines all read "2d ago". Not
  // toLocaleString() either, which the retired Matches tab used -- a
  // full date and time does not fit the 74px the drawing gives this
  // column, and would wrap every line.
  const SHORT_DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
  function matchWhen(iso: string): string {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    const now = new Date(appState.now)
    const sameDay =
      d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
    if (sameDay) return formatHM(iso)
    const ageDays = (now.getTime() - d.getTime()) / 86_400_000
    if (ageDays < 7) return `${SHORT_DAYS[d.getDay()]} ${formatHM(iso)}`
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
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
  function watchState(e: WatchlistEntry): {
    glyph: string
    text: string
    broken: boolean
    learning: boolean
    paused: boolean
  } {
    // The record's own paused chip (watchlist-managed.html:801, :860):
    // `‖ paused`, dim ink, no alarm/broken styling -- a paused watch is
    // not a failure, so it must not read as one.
    if (!e.enabled) return { glyph: '‖', text: 'paused', broken: false, learning: false, paused: true }
    if (watchlistState.coverage[e.id] === 'no-logging') {
      return { glyph: '○', text: 'ring broken — no logging visible', broken: true, learning: false, paused: false }
    }
    if (e.invert && e.observing) {
      // "places seen" is a running total of everywhere this device has
      // ever reached, decided or not -- Entry.Promote (internal/watchlist/
      // invert.go) moves a destination out of Observed once permitted, so
      // counting Observed alone would shrink "seen" every time something
      // is promoted, reading as though mikroview had forgotten it.
      const permitted = (e.permitted ?? []).length
      const seen = (e.observed ?? []).length + permitted
      return {
        glyph: '◌',
        text: `learning — ${seen} places seen · ${permitted} permitted`,
        broken: false,
        learning: true,
        paused: false,
      }
    }
    if (e.ring?.broken) {
      return { glyph: '○', text: 'ring broken — nothing in the window', broken: true, learning: false, paused: false }
    }
    if (e.invert) {
      const n = (e.permitted ?? []).length
      return {
        glyph: '◉',
        text: `fencing — ${n} ${n === 1 ? 'place' : 'places'} permitted`,
        broken: false,
        learning: false,
        paused: false,
      }
    }
    return { glyph: '◉', text: 'watching', broken: false, learning: false, paused: false }
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
  // A suggestion's drawer opens exactly as a watch's does, but the two
  // bodies keep separate ids: a suggested row is not a watch, and
  // opening one should not close the watch drawer an operator was
  // reading it against.
  let suggestDrawerId: string | null = $state(null)
  let pausingId = $state<string | null>(null)
  let wtError = $state<string | null>(null)

  function toggleWatchDrawer(id: string) {
    watchDrawerId = watchDrawerId === id ? null : id
  }

  function toggleSuggestDrawer(id: string) {
    suggestDrawerId = suggestDrawerId === id ? null : id
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
    wtError = null
    if (armedRemoveId !== e.id) {
      armedRemoveId = e.id
      return
    }
    armedRemoveId = null
    watchlistState.remove(e.id).then((err) => {
      if (err) wtError = err
    })
  }

  // Permit, permit all, and fence now / learn again (item 4).
  let permittingKey = $state<string | null>(null)
  let fencingId = $state<string | null>(null)

  async function permitOne(e: WatchlistEntry, d: WatchlistPermittedDest) {
    wtError = null
    permittingKey = e.id + d.destIp + d.port
    try {
      const err = await watchlistState.promote(e.id, [d])
      if (err) wtError = err
    } finally {
      permittingKey = null
    }
  }

  async function permitAll(e: WatchlistEntry) {
    wtError = null
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
    wtError = null
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
    paused: boolean
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
        paused: st.paused,
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

  // --- Round 33: suggestions, a second body under the watches ---------
  //
  // "A suggestion is a watch that has not been said yes to, and a match
  // is a line in the watch's own drawer" (round-33/README.md). So there
  // are no sub-tabs: the Watchlist/Matches/Suggestions strip and the two
  // components behind it are gone outright rather than left unmounted
  // behind a flag, because #771 gives both the capabilities they carried
  // a drawn home here. AGENTS.md's "removals are wholesale" applies once
  // nothing still needs the old surface to reach them.
  //
  // The cross-watch matches view the retired Matches tab also offered
  // (GET /api/matches?entries=all as a page of its own) is deliberately
  // not rehoused: round 33 records it as a stream lens rather than a
  // docket thing, and it is tracked on #691. fetchRecentMatches stays --
  // it is what feeds the table's own "last event" column and every
  // drawer's match list.
  let showAside = $state(false)
  let suggestBusyId = $state<string | null>(null)
  let suggestError = $state<string | null>(null)
  // `start over` uses round 28's arm-then-confirm, the same idiom as
  // `remove` above: one click arms, a second commits, any other click
  // disarms (the svelte:window handler below).
  let armedReset = $state(false)
  let resetting = $state(false)
  // Set once a reset has been confirmed, so the watch body can say
  // "Started over." where the rows were rather than falling back to the
  // ordinary "No watches yet" empty row -- a different sentence for a
  // different cause.
  let startedOverAt = $state<string | null>(null)

  // Suggestions never join watchRows, so round 31's sort and filter --
  // which derive from watchlistState.entries alone -- leave this body
  // alone by construction, as the drawing requires. A suggested row
  // sorts and filters with nothing: it is not a watch.
  const openSuggestions = $derived(suggestState.candidates.filter((c) => c.status === 'off'))
  const asideSuggestions = $derived(suggestState.candidates.filter((c) => c.status === 'hide'))
  // Shown rows: the open ones, and the set-aside ones after `show them`.
  const suggestionRows = $derived(showAside ? [...openSuggestions, ...asideSuggestions] : openSuggestions)

  // "from what rb5009 and hap-ax2 pushed" -- the distinct routers the
  // visible candidates came from, in first-seen order. Falls back to a
  // shorter heading when nothing names a router, rather than printing
  // "from what  pushed".
  const routerNames = $derived.by((): string[] => {
    const seen: string[] = []
    for (const c of suggestState.candidates) {
      if (c.routerDevice && !seen.includes(c.routerDevice)) seen.push(c.routerDevice)
    }
    return seen
  })

  const suggestHeading = $derived(
    routerNames.length > 0
      ? `mikroview suggests · from what ${formatList(routerNames)} pushed`
      : 'mikroview suggests',
  )

  function formatList(names: string[]): string {
    if (names.length <= 1) return names[0] ?? ''
    return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
  }

  // The dashed chip per kind, as drawn: where the suggestion came from,
  // and `· stale — the <thing> is gone` when the justification that
  // generated it has stopped holding.
  function suggestionChip(c: Suggestion): string {
    if (c.status === 'hide') return `◇ set aside${c.updatedAt ? ` — ${sinceDay(c.updatedAt)}` : ''}`
    if (c.stale) return `◇ suggested · stale — the ${staleThing(c)} is gone`
    switch (c.kind) {
      case 'device':
        return `◇ suggested — a new lease on ${zoneOfSuggestion(c)}`
      case 'port':
        return `◇ suggested — from drop rule ${c.name}`
      case 'addressList':
        return `◇ suggested — address list ${c.addressList ?? c.name}`
    }
  }

  function staleThing(c: Suggestion): string {
    return c.kind === 'device' ? 'lease' : c.kind === 'port' ? 'rule' : 'list'
  }

  // Where a suggested device sits, for the chip's "a new lease on iot".
  //
  // Deliberately not CIDR arithmetic: the pushed address table gives
  // zones a cidr, but nothing in this codebase matches an address into
  // one, and inventing subnet maths here would be a new rule in a
  // component rather than a shared one. An address already observed as a
  // host of a zone resolves to that zone's name; anything else reads as
  // its own address, which says where the lease is without claiming a
  // zone mikroview has not actually placed it in.
  function zoneOfSuggestion(c: Suggestion): string {
    const ip = c.source?.ip
    if (!ip) return sourceOfSuggestion(c)
    const zone = zonesState.zones.find((z) => z.hosts.some((h) => h.ip === ip))
    return zone?.name ?? ip
  }

  // The identity a suggestion would be scoped to, in the same grammar
  // sourceLabel gives a real watch.
  function sourceOfSuggestion(c: Suggestion): string {
    return c.source?.mac || c.source?.ip || c.name || 'any source'
  }

  // The boundary column, in the watch row grammar the drawing uses: a
  // device fence reads "<zone> → wherever it goes", a port suggestion
  // carries the ports it would record, and an address list names the
  // list. A port candidate's destination is not something the candidate
  // carries (internal/suggest records the rule's ports, not a resolved
  // destination zone), so the row says the ports rather than inventing
  // a "→ lan" the data does not support.
  function suggestionBoundary(c: Suggestion): string {
    switch (c.kind) {
      case 'device':
        return `${zoneOfSuggestion(c)} → wherever it goes`
      case 'port': {
        const ports = c.ports?.length ? c.ports.map((p) => `:${p}`).join(', ') : 'any port'
        return `${sourceOfSuggestion(c)} → ${ports}`
      }
      case 'addressList':
        return `internet → address list ${c.addressList ?? c.name}`
    }
  }

  // The side column's label: "the lease / the rule / the list, as
  // pushed" -- and "as last pushed" for a stale one, which is the
  // honest tense for something the router no longer has.
  function suggestionSideLabel(c: Suggestion): string {
    const thing = staleThing(c)
    return c.stale ? `the ${thing}, as last pushed` : `the ${thing}, as pushed`
  }

  // The side column, "as pushed": what the router actually said, so the
  // operator judges the suggestion against its evidence rather than
  // against its wording. Only fields the candidate really carries are
  // listed -- an absent one is dropped rather than rendered empty.
  function suggestionFacts(c: Suggestion): { label: string; value: string }[] {
    const facts: { label: string; value: string }[] = []
    if (c.kind === 'device') {
      if (c.name) facts.push({ label: 'host', value: c.name })
      if (c.source?.mac) facts.push({ label: 'mac', value: c.source.mac })
      if (c.source?.ip) {
        const zone = zoneOfSuggestion(c)
        facts.push({ label: 'address', value: zone === c.source.ip ? c.source.ip : `${c.source.ip} · ${zone}` })
      }
    } else if (c.kind === 'port') {
      if (c.name) facts.push({ label: 'rule', value: c.name })
      if (c.ports?.length) facts.push({ label: 'dst-port', value: c.ports.join(', ') })
    } else {
      facts.push({ label: 'name', value: c.addressList ?? c.name })
    }
    if (c.routerDevice) facts.push({ label: c.stale ? 'last pushed' : 'pushed by', value: c.routerDevice })
    if (c.firstSeen) facts.push({ label: 'first seen', value: formatHM(c.firstSeen) })
    return facts
  }

  // The drawer's headline, per kind, as drawn: a device is a host
  // nothing covers, a port is a rule the router already enforces, and a
  // list is a list. The sentence after it is always the candidate's own
  // justification, so the reasoning an operator judges is the
  // generator's rather than a UI paraphrase of it.
  function suggestionHeadline(c: Suggestion): string {
    if (c.stale) return 'Was on the router; is not now.'
    switch (c.kind) {
      case 'device':
        return 'A new host, not yet watched.'
      case 'port':
        return 'Your router already refuses this.'
      case 'addressList':
        return 'A list the router keeps.'
    }
  }

  // A candidate id routinely carries a raw NUL byte -- it is the
  // generator's own join separator (see Suggestion's doc comment in
  // types.ts). That is fine in a JS string and fine in a URL once
  // encodeURIComponent has had it, but it has no business in a DOM id,
  // where it would produce an attribute no CSS selector can address.
  // Reduced to a hook that is safe to select on; the real id is what
  // every API call still uses.
  function suggestionDomId(c: Suggestion): string {
    return c.id.replace(/[^a-zA-Z0-9_-]+/g, '-')
  }

  // The two accept verbs the drawing names, by kind: a device becomes an
  // inverted entry that observes first, a port becomes a non-inverted
  // one where every attempt is a match. That is what the server actually
  // does with each kind (internal/api's handleSuggestionsAccept), so the
  // verb is a promise the backend keeps.
  function acceptVerb(c: Suggestion): string {
    return c.kind === 'device' ? 'watch it — it learns first' : 'watch it — every attempt is a match'
  }

  async function acceptOne(c: Suggestion) {
    suggestBusyId = c.id
    suggestError = null
    const result = await suggestState.accept(c.id)
    if (typeof result === 'string') suggestError = result
    else {
      // The accepted row moves up among the watches, so the watch list
      // has to be refetched for it to appear there at all -- accept
      // creates a real entry server-side, and suggestState.refresh()
      // only reloads the candidate pool.
      await watchlistState.refresh()
      // A watch that did not exist a moment ago is what the drawer now
      // describes, so open it there rather than leaving the operator to
      // find where the row went.
      watchDrawerId = result.entry.id
      startedOverAt = null
      await tick()
      document.getElementById(`watch-${result.entry.id}`)?.scrollIntoView?.({ block: 'center' })
    }
    suggestBusyId = null
  }

  async function hideOne(c: Suggestion) {
    suggestBusyId = c.id
    suggestError = null
    const err = await suggestState.hide(c.id)
    if (err) suggestError = err
    else if (suggestDrawerId === c.id) suggestDrawerId = null
    suggestBusyId = null
  }

  async function unhideOne(c: Suggestion) {
    suggestBusyId = c.id
    suggestError = null
    const err = await suggestState.unhide(c.id)
    if (err) suggestError = err
    else if (suggestDrawerId === c.id) suggestDrawerId = null
    suggestBusyId = null
  }

  // `start over — wipe every watch`: the reset deletes every watchlist
  // entry and regenerates the candidate pool from a fresh look at what
  // the routers pushed. Armed first, per round 28.
  async function armOrStartOver() {
    if (!armedReset) {
      armedReset = true
      return
    }
    armedReset = false
    resetting = true
    suggestError = null
    const err = await suggestState.reset()
    if (err) suggestError = err
    else {
      // The entries really are gone server-side, so the table has to be
      // refetched rather than assumed empty -- and the eye in the chrome
      // reads whatever watchlistState now holds, which is nothing.
      await watchlistState.refresh()
      watchDrawerId = null
      suggestDrawerId = null
      olderMatches = {}
      olderExhausted = {}
      startedOverAt = new Date().toISOString()
    }
    resetting = false
  }

  // --- Round-30 fidelity flag (#700, #691) ----------------------------
  //
  // Round 30: "no page heading and no strap, anywhere" (owner,
  // 2026-08-31). The heading stays unmounted behind its own typed flag
  // rather than deleted, same pattern as LiveTable's
  // RESIZE_HANDLES_ENABLED; bringing it back is tracked on #691.
  const WATCH_HEADING_ENABLED: boolean = false
</script>

<!-- "remove" (#761 item 5) and "start over" (#771 item 6) both use round
     28's clear-all gesture: one click arms it, a second commits, any
     other click disarms -- same idiom as Docket.svelte's own clear-all
     bubble. -->
<svelte:window
  onclick={() => {
    armedRemoveId = null
    armedReset = false
  }}
/>

<div class="watchlist-page">
  <div class="page scrollbar" id="panel-watchlist">
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
    <!-- The table always renders, even with no watches at all: round 33
         hangs the suggestions body off the same table, so a page that
         dropped the table when the watchlist was empty would take the
         suggestions -- the one thing that tells an operator with no
         watches what to do next -- down with it. The empty case is a row
         inside the body instead, which is also what the drawing shows
         after `start over`. -->
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
          {#if sortedWatchRows.length === 0 && startedOverAt && watchlistState.entries.length === 0}
            <!-- ROUND 33 item 6: what `start over` leaves behind. A
                 different sentence from an ordinary empty watchlist,
                 because it has a different cause and an operator who
                 just wiped every watch needs telling what happens next
                 rather than being told there is nothing here. -->
            <tr class="wempty">
              <td colspan="6">
                <span class="cae-mark">↺</span>
                <b>Started over.</b><br />
                Every watch is gone as of {formatHM(startedOverAt)}; the suggestions below are being rebuilt from the
                next push. The audit log has who did it.
              </td>
            </tr>
          {:else if sortedWatchRows.length === 0}
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
                <td><span class="wchip2" class:broken={row.broken} class:learn={row.learning} class:paused={row.paused}>{row.stateGlyph} {row.stateText}</span></td>
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
                        </div>
                        <!-- ROUND 33 item 1: what it matched. This block
                             stands where round 30 put the single
                             last-match line -- the foot of the drawer's
                             story column -- and is the same thing grown
                             into a list. No `provisional` mark: the
                             field is always false today (#406). -->
                        {@const shown = matchesFor(row.entry.id)}
                        <div class="matches">
                          {#if shown.length === 0}
                            <span class="lab">what it matched</span>
                            <p class="ep-note">Nothing in the recent log yet.</p>
                          {:else}
                            <span class="lab">what it matched · last {Math.min(MATCH_LINES, shown.length)} of {shown.length}</span>
                            <ul class="mlist">
                              {#each shown.slice(0, MATCH_LINES) as m (m.id)}
                                <li>
                                  <span class="w">{matchWhen(m.lastSeen)}</span>
                                  <span class="k">{matchSource(m)} → {matchDest(m)}<i>:{m.tuple.port}</i></span>
                                  <span class="t">{m.count}× · {m.event.ruleLabel}</span>
                                </li>
                              {/each}
                            </ul>
                            {#if olderError[row.entry.id]}
                              <p class="error" role="alert">{olderError[row.entry.id]}</p>
                            {/if}
                            {#if canLoadOlder(row.entry)}
                              <button
                                class="slink older"
                                disabled={olderLoadingId === row.entry.id}
                                onclick={(ev) => {
                                  ev.stopPropagation()
                                  loadOlderMatches(row.entry)
                                }}
                              >
                                {olderLoadingId === row.entry.id ? 'loading…' : 'older ▸'}
                              </button>
                            {/if}
                          {/if}
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
                            <span class="lab">where it has reached · since {sinceDay(row.entry.createdAt)}</span>
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

        <!-- ROUND 33 items 2-6: SUGGESTIONS. What mikroview would watch,
             from what the routers pushed -- a second body of the same
             table, under the watches, in the same row grammar. A
             suggestion is a watch that has not been said yes to, so it
             is not in watchRows and round 31's sort and filter (which
             derive from watchlistState.entries) leave it alone. -->
        <tbody id="sugg">
          <tr class="sdiv">
            <td colspan="6">
              <span class="sdl">{suggestHeading} · <b>{openSuggestions.length}</b></span>
              <span class="sdr">
                {#if asideSuggestions.length > 0}
                  <button
                    type="button"
                    class="slink"
                    onclick={(ev) => {
                      ev.stopPropagation()
                      showAside = !showAside
                    }}
                  >
                    <b>{asideSuggestions.length}</b> set aside · {showAside ? 'hide them' : 'show them'}
                  </button>
                {/if}
                <button
                  type="button"
                  class="slink quiet"
                  class:armed={armedReset}
                  disabled={resetting}
                  onclick={(ev) => {
                    ev.stopPropagation()
                    armOrStartOver()
                  }}
                >
                  {resetting
                    ? 'starting over…'
                    : armedReset
                      ? 'confirm — every watch goes, and it suggests afresh'
                      : 'start over — wipe every watch'}
                </button>
              </span>
            </td>
          </tr>

          {#if suggestError}
            <tr><td colspan="6"><p class="error" role="alert">{suggestError}</p></td></tr>
          {/if}

          {#each suggestionRows as c (c.id)}
            {@const aside = c.status === 'hide'}
            <tr
              class="wt-row"
              class:wt-sugg={!aside}
              class:wt-aside={aside}
              id="suggestion-{suggestionDomId(c)}"
              onclick={() => toggleSuggestDrawer(c.id)}
            >
              <td class="k">{c.name || '(unnamed)'}</td>
              <td>{suggestionBoundary(c)}</td>
              <td class="t">always</td>
              <td><span class="wchip2 sugg">{suggestionChip(c)}</span></td>
              <td class="t">—</td>
              <td>
                <button
                  class="openc"
                  aria-expanded={suggestDrawerId === c.id}
                  aria-label="{suggestDrawerId === c.id ? 'Close' : 'Open'} the drawer for the suggestion {c.name}"
                  onclick={(ev) => {
                    ev.stopPropagation()
                    toggleSuggestDrawer(c.id)
                  }}
                >
                  ▸
                </button>
              </td>
            </tr>
            {#if suggestDrawerId === c.id}
              <tr class="wt-drawer" class:wt-sugg={!aside} class:wt-aside={aside}>
                <td colspan="6">
                  <div class="dwr">
                    <div class="dcol">
                      <!-- The story is the candidate's own justification:
                           why the router's data suggested this at all.
                           Written server-side (internal/suggest), so the
                           sentence an operator judges is the generator's
                           own reasoning, not a UI paraphrase of it. -->
                      <p class="story">
                        {#if aside}
                          <b>Set aside.</b> Kept here, quiet, until you bring it back; if what suggested it goes, it
                          goes with it.
                        {:else if c.stale}
                          <b>{suggestionHeadline(c)}</b>
                          {c.justification} Watch it anyway and it watches nothing. Better to let it go.
                        {:else}
                          <b>{suggestionHeadline(c)}</b>
                          {c.justification}
                        {/if}
                      </p>
                    </div>
                    <div class="side">
                      <span class="lab">{suggestionSideLabel(c)}</span>
                      <ul class="seen facts">
                        {#each suggestionFacts(c) as f (f.label)}
                          <li><span class="t">{f.label}</span><span class="k">{f.value}</span></li>
                        {/each}
                      </ul>
                    </div>
                    <div class="dwr-acts">
                      {#if aside}
                        <!-- ROUND 33 item 5: one verb, and nothing is
                             ever thrown away from here. -->
                        <button class="act quiet" disabled={suggestBusyId === c.id} onclick={() => unhideOne(c)}>
                          {suggestBusyId === c.id ? 'Saving…' : 'bring it back'}
                        </button>
                      {:else if c.stale}
                        <!-- ROUND 33 item 4: a stale one leads with the
                             honest verb, and keeps the other quiet. -->
                        <button class="act" disabled={suggestBusyId === c.id} onclick={() => hideOne(c)}>
                          {suggestBusyId === c.id ? 'Saving…' : 'let it go'}
                        </button>
                        <button class="act quiet" disabled={suggestBusyId === c.id} onclick={() => acceptOne(c)}>
                          watch it anyway
                        </button>
                      {:else}
                        <button class="act" disabled={suggestBusyId === c.id} onclick={() => acceptOne(c)}>
                          {suggestBusyId === c.id ? 'Saving…' : acceptVerb(c)}
                        </button>
                        <button class="act quiet" disabled={suggestBusyId === c.id} onclick={() => hideOne(c)}>
                          not this
                        </button>
                      {/if}
                    </div>
                  </div>
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
  </section>
  </div>
</div>

<style>
  .watchlist-page {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  /* `.tab-panel`, and the `[hidden]` override that stopped a
     "hidden" panel rendering anyway, both went with the sub-tabs
     (#771): there is one panel now, and it is never hidden. */
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

  /* Round 33 replaced the drawer's verbatim `.lines` element with the
     `.matches` list below -- round 33's watch drawers draw no `.lines`
     at all (the six left in suggestions-matches.html are the flags
     panel's). Removed rather than left unused: an unreferenced selector
     is dead style, and svelte-check reports it as one. */

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

  /* `‖ paused` (watchlist-managed.html:801, :860): dim ink, no alarm
     styling -- a paused watch is a choice, not a failure. */
  .wchip2.paused {
    color: var(--fg-dim);
    border-color: var(--border);
    background: transparent;
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

  /* ============================================================
     ROUND 33 — SUGGESTIONS AND MATCHES
     (docs/design/concepts/round-33/suggestions-matches.html).
     Additive to round 31's docket; the one thing that grows is
     round 30's single last-match line, which becomes the first
     line of a list.
     ============================================================ */

  /* what a watch matched: the drawer's log, where the last-match
     line was. Column 1, so it sits at the foot of the story
     column rather than beside it. */
  .matches {
    grid-column: 1;
  }

  .matches .lab {
    display: block;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .matches .ep-note {
    margin: 4px 0 0;
    font-family: var(--font-sans);
    font-size: 12px;
    color: var(--fg-muted);
  }

  .mlist {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
  }

  .mlist li {
    display: grid;
    grid-template-columns: 74px 1fr auto;
    align-items: center;
    gap: 12px;
    padding: 3px 0;
    border-bottom: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .mlist .w,
  .mlist .t {
    color: var(--fg-dim);
  }

  .mlist .k {
    color: var(--fg);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .mlist .k i {
    color: var(--fg-dim);
    font-style: normal;
  }

  .slink {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--accent);
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
    text-decoration: none;
  }

  .slink:hover {
    text-decoration: underline;
  }

  .slink:disabled {
    cursor: default;
    opacity: 0.6;
    text-decoration: none;
  }

  .slink.quiet {
    color: var(--fg-dim);
  }

  .slink.armed {
    color: var(--alarm);
    text-decoration: none;
  }

  .matches .older {
    display: inline-block;
    margin-top: 6px;
  }

  /* suggestions: a second body, its own quiet heading, dashed ink
     until said yes to */
  #sugg .sdiv td {
    padding: 22px 10px 6px;
    border-bottom: 1px solid var(--border);
  }

  #sugg .sdiv .sdl {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  #sugg .sdiv .sdl b {
    color: var(--fg-muted);
    font-weight: 600;
  }

  #sugg .sdiv .sdr {
    float: right;
    display: inline-flex;
    gap: 18px;
  }

  #sugg .sdiv .sdr b {
    font-weight: 600;
  }

  /* the heading's two verbs wear the drawer's pill, so they read as
     things to do, not notes (owner, 2026-09-01: "you didn't explain
     start over" -- so the reset says what it does before it is
     clicked) */
  #sugg .sdiv .sdr .slink {
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 600;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 3px 12px;
  }

  #sugg .sdiv .sdr .slink:hover {
    border-color: var(--accent);
    text-decoration: none;
  }

  #sugg .sdiv .sdr .slink.quiet:hover {
    border-color: var(--fg-dim);
  }

  #sugg .sdiv .sdr .slink.armed {
    border-color: var(--alarm);
  }

  .wchip2.sugg {
    color: var(--fg-dim);
    border-style: dashed;
    border-color: var(--border);
    background: transparent;
  }

  tr.wt-row.wt-sugg td.k,
  tr.wt-row.wt-aside td.k {
    color: var(--fg-muted);
  }

  tr.wt-row.wt-sugg td:first-child,
  tr.wt-drawer.wt-sugg > td {
    box-shadow: inset 3px 0 0 var(--border);
  }

  tr.wt-row.wt-aside td:first-child,
  tr.wt-drawer.wt-aside > td {
    box-shadow: inset 3px 0 0 transparent;
  }

  /* set aside: dimmer ink, still legible -- nothing is ever thrown
     away from here */
  tr.wt-row.wt-aside td {
    opacity: 0.6;
  }

  .seen.facts li {
    grid-template-columns: 84px 1fr;
  }

  .seen.facts .t {
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-size: 9.5px;
  }

  /* what `start over` leaves behind, where the rows were */
  tr.wempty td {
    text-align: center;
    padding: 48px 0 40px;
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.7;
  }

  tr.wempty b {
    color: var(--fg);
  }

  tr.wempty .cae-mark {
    display: block;
    font-size: 20px;
    color: var(--accent);
    margin-bottom: 6px;
  }
</style>
