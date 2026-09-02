<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  //
  // The ratified surface (#688, round 29's `#s7`): a table --
  // flag · where · evidence · count · age -- one row per open flag, each
  // row opening as a drawer beneath itself holding the story, the
  // episode's shape, the matched lines and the actions. Every flag type
  // wears its own family ink as a left stripe and mark ink, one unbroken
  // line running row into drawer. Ported from the record's own markup and
  // CSS (`.frow`/`.fmark`/`.openc`/`tr.drawer`/`.dwr-in`/`.story`/`.side`/
  // `.lines`/`.dwr-acts`), not from an impression of it; the peer
  // watchlist table (#676) reads as the same surface because it was
  // ported from the same scene.
  //
  // #780 (rounds 34-35) gave three of round 29's recorded gaps a home:
  // the verdict trio now lives in every row's own `CALL IT` column
  // (`.vc`/`.vrow`/`.stamp`) rather than a drawer, no longer gated on
  // `provisional` since the backend takes a verdict on any flag
  // (store.go:915); a called flag stays in place, dimmed with its stamp
  // and (where reversible) an undo, until the tab is left -- the
  // recently-cleared list, in place rather than a separate page; and the
  // exclusions body is a second `<tbody>` under the flags, admin-only,
  // fed by `fetchExclusions`/`removeExclusion`. `never again` stays in
  // the drawer alone, since it is the one verdict that wants a second
  // look. Confidence, campaign grouping, the density picker and the
  // reputation/evidence panels are still deliberately absent -- see
  // #691 for what remains.
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { anyBaselineWarming } from '../lib/learningShelf'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { fetchExclusions, fetchFlagEpisode, removeExclusion } from '../lib/api'
  import { familyOf } from '../lib/flagPalette'
  import { formatHM, formatTime } from '../lib/format'
  import { compareNumeric, compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import { headlineFor, storyFor } from '../lib/flagNarrative'
  import { episodeShapeFor } from '../lib/episodeShape'
  import { zonesState } from '../lib/zones.svelte'
  import { parseCidr, addressInCidr } from '../lib/addressMatch'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import type { Exclusion, Flag, FlagType, FirewallEvent } from '../lib/types'

  // "watch this pathway" / "watch this source" (#761 item 3): a flag
  // writes the watchlist tab's draft for it rather than making the
  // operator retype what mikroview already knows. Both switch to the
  // watchlist tab through the same shared handoff Docket's `+ watch` and
  // #724's dial rows already use (topologyNav.svelte.ts) -- Watchlist's
  // own draft state is private to that component.
  //
  // "watch this source" fences the flag's own source -- an inverted
  // entry, scoped to that device, learning where it goes before
  // anything fires. Gated on extractSourceIp actually resolving a
  // single source IP, which is narrower than isFilterable(): that also
  // covers rule_spike/stale_rule (target is a rule label),
  // distributed_brute_force (target is "port N") and device_silence
  // (target is a device id), none of which name a device to fence.
  // Falling back to the raw target for those would pre-fill the
  // draft's identity with a rule name or a bare port number -- a
  // confident-looking but wrong "who".
  function canWatchSource(f: Flag): boolean {
    return extractSourceIp(f.target) !== null
  }

  // "watch this pathway" only ever fires for critical_port: it is the
  // one detector whose Evidence carries a real host:port destination
  // (evidence.pairs, #654) rather than a free-text sentence. Every other
  // type's `target`/`detail` is prose or a list with no single named
  // destination to expect -- guessing one from text would be inventing
  // evidence this page did not actually see, so those flags offer
  // `watch this source` only (and only when canWatchSource agrees).
  function canWatchPathway(f: Flag): boolean {
    return f.type === 'critical_port' && (f.evidence?.pairs?.length ?? 0) > 0
  }

  // The pathway's `toward`: the first evidence pair's host, and every
  // port evidence recorded against that same host -- the shape the
  // record's own draft `toward` field takes ("nas · :445, :139").
  function pathwayToward(f: Flag): string {
    const pairs = f.evidence?.pairs ?? []
    const host = pairs[0]?.host ?? ''
    const ports = pairs.filter((p) => p.host === host).map((p) => `:${p.port}`)
    return ports.length > 0 ? `${host} · ${ports.join(', ')}` : host
  }

  // Both are only ever wired to a button gated on canWatchSource/
  // canWatchPathway, so the source IP this reads has already been
  // confirmed to exist -- never falls back to the raw target.
  function watchThisSource(f: Flag) {
    const who = extractSourceIp(f.target)
    if (!who) return
    topologyNavState.requestWatchDraft({ who, mode: 'fence' })
    appState.view = 'watchlist'
  }

  function watchThisPathway(f: Flag) {
    const who = extractSourceIp(f.target)
    if (!who) return
    topologyNavState.requestWatchDraft({ who, toward: pathwayToward(f), mode: 'expect' })
    appState.view = 'watchlist'
  }

  // Same gate the rail uses for the engine room's watchers station --
  // here it decides whether the empty state offers the audit log, and
  // (#780) whether `never again`/the exclusions body render at all:
  // POST /api/flags/{id}/clear-permanent and both exclusions endpoints
  // are accessAdmin (internal/api/authz_matrix_test.go), unlike the
  // verdict trio and its undo below, which are user tier.
  const isAdminOrOpen = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  // #653: clearing a flag is a user-tier action -- a viewer may watch
  // what mikroview is seeing but not change what it shows. Absent rather
  // than disabled, the same grammar the rest of the app uses.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)

  // lib/flags.svelte.ts's clear/clearAll optimistically update, then roll
  // back and *rethrow* on failure. Caught here so a transient 500 or an
  // expired session reads as an error rather than as a button that did
  // nothing. Reported the same way Watchlist and Entities report theirs.
  let error = $state<string | null>(null)

  function reportFailure(action: string, err: unknown) {
    error = err instanceof Error ? `${action}: ${err.message}` : `${action} failed`
  }

  let expandedId: string | null = $state(null)

  // toggleExpanded is defined further down (#780), once verdictKind
  // exists for it to consult -- a resolved, non-real row no longer
  // opens a drawer (see its own doc comment there).

  // The drawer's episode (#633, rounds 18-19/29): the flag's own events,
  // fetched once per flag on first open via the #29 around+window
  // lookback centred on lastSeen. Cached for the component's lifetime
  // rather than refetched per open -- a drawer that redraws its episode
  // differently each time it opens reads as noise, not evidence.
  let episodes = $state<Record<string, FirewallEvent[] | 'loading' | 'error'>>({})

  // The same target mapping filterToTarget uses, as query params: which
  // server-side filter this flag's target actually is (see that
  // function's own comment). Empty for global_spike (the surge is the
  // whole window) and new_device (a MAC has no server-side match).
  function episodeParams(f: Flag): { ip?: string; port?: string; rule?: string; device?: string } {
    switch (f.type) {
      case 'port_scan':
      case 'activity_spike':
      case 'critical_port':
      case 'outbound_anomaly':
      case 'internal_recon':
      case 'low_slow_scan':
      case 'off_hours_activity':
      case 'unexpected_mail_sender':
      case 'known_bad_ip':
        return { ip: f.target }
      case 'distributed_brute_force':
        return { port: f.target.replace(/^port /, '') }
      case 'rule_spike':
      case 'stale_rule':
        return { rule: f.target }
      case 'repeated_drops':
        return { ip: f.target.split(' -> ')[0] }
      case 'device_silence':
        return { device: f.target }
      case 'global_spike':
      case 'new_device':
        return {}
    }
  }

  async function loadEpisode(f: Flag) {
    if (episodes[f.id]) return
    episodes[f.id] = 'loading'
    try {
      const res = await fetchFlagEpisode({ ...episodeParams(f), around: f.lastSeen, window: '30m', limit: 120 })
      episodes[f.id] = res.events
    } catch {
      episodes[f.id] = 'error'
    }
  }

  // #724's second click: a dial panel row's own destination, not just the
  // right tab. Consumed (cleared) the instant it's read -- same idiom as
  // topologyNavState.pendingDescend's own consumer in Topography.svelte --
  // so a later manual visit to this tab doesn't silently reopen a stale
  // drawer. Opens through loadEpisode directly, not a bare expandedId
  // assignment, since the row's drawer needs the same episode fetch a
  // click on the row itself triggers (Care, #724). Matched against
  // `active` (every open flag, not the filtered/sorted view) so a filter
  // box left over from an earlier visit can't hide the very row the dial
  // just promised to open; a flag cleared between the click and landing
  // here has nothing to match, so nothing opens -- never an error, never
  // a blank drawer.
  $effect(() => {
    const id = topologyNavState.pendingFlagId
    if (id === null) return
    topologyNavState.pendingFlagId = null
    // Either table can hold the promised row (#642): a dial can point
    // at a provisional flag as readily as a settled one.
    const f = active.find((x) => x.id === id) ?? provisionalActive.find((x) => x.id === id)
    expandedId = f?.id ?? null
    if (f) loadEpisode(f)
  })

  // Tick positions for the episode strip, one per event, normalised
  // across the fetched span (the record's own geometry: 260-wide
  // viewBox, ticks inset 8px each side). A single event centres.
  function episodeTicks(events: FirewallEvent[]): number[] {
    const times = events
      .map((e) => new Date(e.time).getTime())
      .filter((t) => !Number.isNaN(t))
      .sort((a, b) => a - b)
    if (times.length === 0) return []
    const t0 = times[0]
    const span = times[times.length - 1] - t0
    return times.map((t) => (span === 0 ? 130 : 8 + ((t - t0) / span) * 244))
  }

  // One matched line, composed from the structured event the same way
  // the stream renders it -- raw lines are not retained, and composing
  // beats showing nothing.
  function eventLine(e: FirewallEvent): string {
    const io = [e.inInterface ? `in:${e.inInterface}` : '', e.outInterface ? `out:${e.outInterface}` : '']
      .filter(Boolean)
      .join(' ')
    const proto = e.protocol ? ` proto ${e.protocol.toUpperCase()}` : ''
    const src = e.srcIp ? `${e.srcIp}${e.srcPort ? `:${e.srcPort}` : ''}` : ''
    const dst = e.dstIp ? `${e.dstIp}${e.dstPort ? `:${e.dstPort}` : ''}` : ''
    const flow = src && dst ? `, ${src}->${dst}` : ''
    return `${formatTime(e.time)} ${e.action}|${e.ruleLabel}| ${e.chain}: ${io}${proto}${flow}`
  }

  // Same labels Exclusions.svelte and lib/metricsSeries.ts use --
  // duplicated rather than shared, which is the long-standing convention
  // for these two tables in this codebase. The record sets the flag
  // column in caps; that is done in CSS, so the label a filter matches
  // on stays the label everything else in the app uses.
  const TYPE_LABELS: Record<FlagType, string> = {
    port_scan: 'Port scan',
    activity_spike: 'Activity spike',
    critical_port: 'Critical-port attempts',
    global_spike: 'Network-wide volume spike',
    distributed_brute_force: 'Distributed brute-force',
    outbound_anomaly: 'Outbound anomaly',
    internal_recon: 'Internal reconnaissance',
    rule_spike: 'Rule hit-rate spike',
    repeated_drops: 'Repeated drops on a port',
    low_slow_scan: 'Low-and-slow port scan',
    off_hours_activity: 'Off-hours activity',
    device_silence: 'Device gone quiet',
    new_device: 'New device',
    stale_rule: 'Stale firewall rule',
    unexpected_mail_sender: 'Unexpected mail sender',
    known_bad_ip: 'Known-bad IP (blocklist match)',
  }

  // A custom detection's type is its author's own name for it -- the
  // honest label, not a key the sixteen-entry table above could know.
  const labelFor = (t: FlagType) => TYPE_LABELS[t] ?? t

  // Ids this visit judged/cleared/excluded, kept in the settled or
  // shelf table -- dimmed, carrying their stamp -- rather than dropped
  // the instant the server marks them cleared (#780 items 2/4: "the
  // recently-cleared list, in place", staying until the tab is left).
  // `real` needs no entry here: judgeReal never sets `cleared`, so that
  // row stays in `active`/`provisionalActive` on its own. Reset only by
  // remounting the component -- there is no other "leaving the tab"
  // hook available to a page that stays mounted underneath a deck card
  // (see App.svelte), so a fresh visit's own actions start the list
  // over, same as `episodes`/`expandedId` above already do.
  let pinnedIds = $state<string[]>([])
  // The stamp text for a pinned row that carries no verdict at all --
  // `clear with a note` and `never again` both just set `cleared`
  // (flags.svelte.ts's `clear`/`clearPermanent`), so there is nothing
  // on the Flag itself to read the right word back off, unlike
  // expected/noise/real (verdictKind below reads those straight off
  // `f.verdict`).
  let pinnedKind = $state<Record<string, 'cleared' | 'never'>>({})

  function pin(id: string, kind?: 'cleared' | 'never') {
    if (!pinnedIds.includes(id)) pinnedIds = [...pinnedIds, id]
    if (kind) pinnedKind = { ...pinnedKind, [id]: kind }
  }

  function unpin(id: string) {
    pinnedIds = pinnedIds.filter((pid) => pid !== id)
    if (id in pinnedKind) {
      const { [id]: _removed, ...rest } = pinnedKind
      pinnedKind = rest
    }
  }

  // Sorted by firstSeen (not the fetch response's lastSeen-desc order --
  // see internal/flags.Store.List()) so a flag's position is fixed the
  // moment it first appears. lastSeen updates on every re-fire, not just
  // creation, so sorting by it made an already-visible row you're
  // reading jump to the top of the list the instant it (or anything
  // else) re-fired on the next 5s poll. `pinnedIds` keeps a just-called
  // row exactly here rather than letting `!f.cleared` drop it (#780).
  const active = $derived(
    flagsState.list
      .filter((f) => !f.provisional && (!f.cleared || pinnedIds.includes(f.id)))
      .sort((a, b) => new Date(b.firstSeen).getTime() - new Date(a.firstSeen).getTime()),
  )
  const cleared = $derived(flagsState.list.filter((f) => f.cleared))

  // The learning shelf's rows (#642): open *provisional* flags -- raised
  // while their judgement's baseline was still below its history floor,
  // so mikroview does not yet trust them. Kept out of `active` (and out
  // of flagsState.activeCount) so trusted and untrusted judgements are
  // never interleaved in one time-ordered list -- the issue's ruling.
  // Same fixed firstSeen order as the settled table, for the same
  // reason: a row must not jump mid-read when it re-fires. Same pinning
  // as `active` above once a shelf row is judged/cleared.
  const provisionalActive = $derived(
    flagsState.list
      .filter((f) => f.provisional && (!f.cleared || pinnedIds.includes(f.id)))
      .sort((a, b) => new Date(b.firstSeen).getTime() - new Date(a.firstSeen).getTime()),
  )

  // "Any baseline warming" (#642, ruling amendment 1): readable only
  // from GET /api/definitions' learning field, which #653 gates at the
  // user tier -- so it is evaluated for user/admin only, and a viewer's
  // shelf appears only when it has contents. Degrades by absence, the
  // app's grammar, never by a disabled control or an unverifiable claim.
  const warming = $derived(canEdit && anyBaselineWarming(detectorSettingsState.list))

  // Fetch the warm-up projection where this scene is allowed to read
  // it. A failure is deliberately swallowed: with no data the shelf
  // falls back to contents-only -- the absence of a claim, not a false
  // one -- and EngineRoom's own use of this store is unaffected.
  $effect(() => {
    if (canEdit) detectorSettingsState.refresh().catch(() => {})
  })

  const showShelf = $derived(provisionalActive.length > 0 || warming)

  // The verdict trio (#638's store paths, #780's row column): offered on
  // every open row, settled or shelf -- the backend takes a verdict on
  // any flag (store.go:915), so this is no longer gated on
  // `provisional` the way the old drawer-only buttons were (#688's own
  // recorded gap, closed by this issue). What a done row's `CALL IT`
  // cell shows -- null while still open and callable.
  type StampKind = 'expected' | 'noise' | 'real' | 'cleared' | 'never'

  function verdictKind(f: Flag): StampKind | null {
    if (f.verdict === 'real' || f.verdict === 'expected' || f.verdict === 'noise') return f.verdict
    return pinnedKind[f.id] ?? null
  }

  // The three verdict inks are `.stamp.expected/.noise/.real`; `cleared`
  // and `never` fall through to the stamp's own quiet default ink
  // (ink-3), per the issue's "stamps CLEARED/NEVER AGAIN in ink-3".
  function stampInk(kind: StampKind): string {
    return kind === 'expected' || kind === 'noise' || kind === 'real' ? kind : ''
  }

  function stampText(kind: StampKind): string {
    return kind === 'never' ? 'never again' : kind
  }

  // Only a verdict the backend can actually reverse gets an Undo:
  // expected/noise/real all go through DELETE /api/flags/verdict/{id}
  // (flagsState.undoVerdict), but a plain clear or a permanent exclude
  // set no verdict at all, so there is nothing for that call to undo --
  // store.go's UndoVerdict only reopens a flag its own verdict cleared.
  function canUndo(kind: StampKind): boolean {
    return kind === 'expected' || kind === 'noise' || kind === 'real'
  }

  // A resolved, non-real row is inert (round 35's `close(r)`): its
  // caret is gone from the CALL IT cell (see the flagRows snippet
  // below), so a click elsewhere on the row must not still open a
  // drawer nothing points back to.
  function toggleExpanded(f: Flag) {
    const kind = verdictKind(f)
    if (kind !== null && kind !== 'real') return
    expandedId = expandedId === f.id ? null : f.id
    if (expandedId === f.id) loadEpisode(f)
  }

  async function callVerdict(f: Flag, verdict: 'expected' | 'noise') {
    error = null
    // Pinned *before* the call, not after: flagsState.judgeAndClear
    // flips `cleared` optimistically the instant it runs, synchronously,
    // well before its own network request resolves. Pinning only on
    // success left a window where `!f.cleared` alone decided whether the
    // row stayed in `active`/`provisionalActive` -- it did not, so the
    // row (and the whole shelf section, if this was its last one)
    // vanished for the round trip and reappeared as a fresh element
    // once the response landed, rather than staying put for the flash.
    pin(f.id)
    if (expandedId === f.id) expandedId = null
    try {
      await flagsState.judgeAndClear(f.id, verdict)
    } catch (err) {
      unpin(f.id)
      reportFailure('Could not record the verdict', err)
    }
  }

  async function callReal(f: Flag) {
    error = null
    try {
      await flagsState.judgeReal(f.id, authState.username ?? '')
    } catch (err) {
      reportFailure('Could not record the verdict', err)
    }
  }

  async function undoCall(f: Flag) {
    error = null
    try {
      await flagsState.undoVerdict(f.id)
      unpin(f.id)
    } catch (err) {
      // Left pinned: flagsState.undoVerdict reverts its own optimistic
      // reopen on failure, so the flag is still exactly as done as it
      // was before this click -- unpinning it here would drop the row
      // from the table out from under a stamp that is still true.
      reportFailure('Could not undo the verdict', err)
    }
  }

  // `never again` (#780 item 4, round 28's arm-then-confirm -- the same
  // idiom Watchlist's own `remove` uses): one click arms it red as
  // "confirm", a second calls clearPermanent, any other click disarms.
  // Admin-only (POST /api/flags/{id}/clear-permanent is accessAdmin),
  // unlike the trio above.
  let neverArmedId: string | null = $state(null)

  function neverLabel(f: Flag): string {
    return neverArmedId === f.id ? `confirm — ${labelFor(f.type)} never fires again for ${f.target}` : 'never again'
  }

  async function clickNever(f: Flag) {
    if (neverArmedId !== f.id) {
      neverArmedId = f.id
      return
    }
    neverArmedId = null
    error = null
    // Pinned before the call, same reasoning as callVerdict above --
    // clearPermanent optimistically flips `cleared` synchronously too.
    pin(f.id, 'never')
    if (expandedId === f.id) expandedId = null
    try {
      await flagsState.clearPermanent(f.id)
      await loadExclusions()
    } catch (err) {
      unpin(f.id)
      reportFailure('Could not exclude this flag permanently', err)
    }
  }

  // The exclusions body (#780 item 5): a second `<tbody>` under the
  // flags, admin-only (both endpoints are accessAdmin, see
  // isAdminOrOpen's own comment above). Hidden until asked for, like
  // round 33's set-aside rows. Exclusion carries no `since`/`by` yet
  // (store.go:362, #750) -- left out here rather than invented.
  let exclusions = $state<Exclusion[]>([])
  let showExclusions = $state(false)
  let expandedExclusionId: string | null = $state(null)

  async function loadExclusions() {
    if (!isAdminOrOpen) return
    try {
      exclusions = await fetchExclusions()
    } catch (err) {
      reportFailure('Could not load the exclusions list', err)
    }
  }

  $effect(() => {
    if (isAdminOrOpen) loadExclusions()
  })

  function toggleExclusion(e: Exclusion) {
    expandedExclusionId = expandedExclusionId === e.id ? null : e.id
  }

  async function letItFireAgain(e: Exclusion) {
    error = null
    const prev = exclusions
    exclusions = exclusions.filter((x) => x.id !== e.id)
    if (expandedExclusionId === e.id) expandedExclusionId = null
    try {
      await removeExclusion(e.id)
    } catch (err) {
      exclusions = prev
      reportFailure('Could not remove this exclusion', err)
    }
  }

  // The honest cleared state (round 26, drawn as `.caempty` in round
  // 29): when nothing is open, say when the last clear happened rather
  // than pretending nothing ever fired. Null when no flag has ever been
  // cleared -- then "nothing open" is the whole truth and carries no
  // timestamp.
  const lastClearedAt = $derived.by((): string | null => {
    let latest: string | null = null
    for (const f of cleared) {
      if (f.clearedAt && (!latest || new Date(f.clearedAt) > new Date(latest))) latest = f.clearedAt
    }
    return latest
  })

  // The age column (#688, round 29/30's `#s7`): the record writes a bare
  // "<number> <unit>" -- no "ago" suffix, and no seconds unit ever
  // appears there (its youngest flag is `6 m`). That is the same
  // spaced-letter idiom the record uses for every other duration on the
  // scene (the scene-bar's own `15 m`/`1 h`/`24 h`/`14 d` span picker),
  // so seconds gets the same "N s" shape rather than "just now" or a
  // borrowed "Xs ago" -- sub-minute is still a number, not a phrase.
  // Local rather than a shared lib/format.ts helper: formatRelative is
  // the "ago" phrasing other views (Fleet's last-seen) still want.
  function formatFlagAge(iso: string, nowMs: number): string {
    const t = new Date(iso).getTime()
    if (Number.isNaN(t)) return iso
    const deltaMs = Math.max(0, nowMs - t)
    const s = Math.floor(deltaMs / 1000)
    if (s < 60) return `${s} s`
    const m = Math.floor(s / 60)
    if (m < 60) return `${m} m`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} h`
    const d = Math.floor(h / 24)
    return `${d} d`
  }

  function clearedWhen(iso: string): string {
    const d = new Date(iso)
    const now = new Date()
    const sameDay =
      d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
    return sameDay ? `today at ${formatHM(iso)}` : formatTime(iso)
  }

  // Every column sorts and filters (#649) -- the record's own table does
  // both from the column heads themselves: clicking a head sorts by it,
  // again to reverse, and the quiet dashed row inside the head group
  // narrows the list. Age defaults to newest-first, reproducing the
  // fixed order `active` used to be stuck with.
  type FlagSortKey = 'type' | 'where' | 'evidence' | 'count' | 'age'
  let sortKey = $state<FlagSortKey>('age')
  let sortDir = $state<SortDir>('asc')
  let filters = $state({ type: '', where: '', evidence: '', count: '', age: '' })

  function toggleSort(key: FlagSortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = 'asc'
    }
  }

  function dirGlyph(key: FlagSortKey): string {
    if (sortKey !== key) return ''
    return sortDir === 'asc' ? '▲' : '▼'
  }

  const filteredActive = $derived(
    active.filter(
      (f) =>
        matchesFilter(labelFor(f.type), filters.type) &&
        matchesFilter(f.target, filters.where) &&
        matchesFilter(f.detail, filters.evidence) &&
        matchesFilter(String(f.count), filters.count) &&
        matchesFilter(formatFlagAge(f.lastSeen, appState.now), filters.age),
    ),
  )

  const sortedActive = $derived.by((): Flag[] => {
    const list = [...filteredActive]
    list.sort((a, b) => {
      switch (sortKey) {
        case 'type':
          return compareText(labelFor(a.type), labelFor(b.type), sortDir)
        case 'where':
          return compareText(a.target, b.target, sortDir)
        case 'evidence':
          return compareText(a.detail, b.detail, sortDir)
        case 'count':
          return compareNumeric(a.count, b.count, sortDir)
        case 'age': {
          // Elapsed time since firstSeen -- ascending means smallest
          // elapsed (newest) first, the default that reproduces today's
          // fixed order.
          const ageA = appState.now - new Date(a.firstSeen).getTime()
          const ageB = appState.now - new Date(b.firstSeen).getTime()
          return compareNumeric(ageA, ageB, sortDir)
        }
      }
    })
    return list
  })

  // What a flag's target actually *is* varies by detector -- most are a
  // plain source IP, but distributed_brute_force is keyed by port,
  // rule_spike/stale_rule by rule label, repeated_drops by
  // "ip -> port N", device_silence by a device ID, and global_spike has
  // no filterable target at all. new_device's target is a MAC address
  // (see internal/flags.TypeNewDevice) -- the live view's Filters has no
  // MAC field to filter on, so it's not filterable either.
  function isFilterable(f: Flag): boolean {
    return f.type !== 'global_spike' && f.type !== 'new_device'
  }

  function filterToTarget(f: Flag) {
    switch (f.type) {
      case 'port_scan':
      case 'activity_spike':
      case 'critical_port':
      case 'outbound_anomaly':
      case 'internal_recon':
      case 'low_slow_scan':
      case 'off_hours_activity':
      case 'unexpected_mail_sender':
      case 'known_bad_ip':
        appState.setFilter('srcQuery', f.target)
        break
      case 'distributed_brute_force':
        appState.setFilter('port', f.target.replace(/^port /, ''))
        break
      case 'rule_spike':
      case 'stale_rule':
        appState.setFilter('rule', f.target)
        break
      case 'repeated_drops':
        appState.setFilter('srcQuery', f.target.split(' -> ')[0])
        break
      case 'device_silence':
        appState.setFilter('device', f.target)
        break
      case 'global_spike':
      case 'new_device':
        return
    }
    appState.view = 'live'
  }

  // "where" (#678): every named where is a link into the topography at
  // its sensible level, not the stream -- filterToTarget above is what
  // the drawer's "open in stream" still uses. extractSourceIp already
  // knows which target shapes are a single IP (see its own doc comment);
  // when one resolves to a zone mikroview has actually observed, this
  // hands that zone/host off to Topography.svelte's own reach (see
  // topologyNav.svelte.ts) so the map opens straight into it. A target
  // with no resolvable IP (a rule label, "global", a MAC, a port) or one
  // outside every known zone still lands on the map itself -- the map's
  // own "degrades honestly" stance, never a wrong guess.
  function openWhere(f: Flag) {
    const ip = extractSourceIp(f.target)
    if (ip) {
      const zone = zonesState.zones.find((z) => {
        if (!z.cidr) return false
        const cidr = parseCidr(z.cidr)
        return cidr ? addressInCidr(ip, cidr) : false
      })
      if (zone) {
        const host = zone.hosts.find((h) => h.ip === ip)?.label ?? ip
        topologyNavState.requestHost(zone.id, host, ip)
      }
    }
    appState.view = 'topography'
  }

  // "clear with a note" (#678, and the record's own third action): the
  // note is the reason the clear happened, and #679's ruling put it in
  // the existing admin-mutation audit log rather than a second record.
  // One capture open at a time, in the drawer's action area.
  let noteFor: string | null = $state(null)
  let noteDraft = $state('')

  function openNote(id: string) {
    noteFor = id
    noteDraft = ''
  }

  function cancelNote() {
    noteFor = null
    noteDraft = ''
  }

  function confirmClear(id: string) {
    const note = noteDraft.trim()
    noteFor = null
    noteDraft = ''
    clear(id, note || undefined)
  }

  async function clear(id: string, note?: string) {
    error = null
    // #780 item 2: "clear with a note" stamps CLEARED the same way a
    // verdict does -- pinned in place, dimmed, drawer closed. No Undo:
    // a plain clear sets no verdict, and store.go's UndoVerdict only
    // reopens a flag its own verdict cleared. Pinned *before* the call
    // -- same reasoning as callVerdict above -- since flagsState.clear
    // flips `cleared` optimistically, synchronously, ahead of its own
    // network request.
    pin(id, 'cleared')
    if (expandedId === id) expandedId = null
    try {
      await flagsState.clear(id, note)
    } catch (err) {
      unpin(id)
      reportFailure('Could not clear this flag', err)
    }
  }
</script>

<div class="flags-page">
  <div class="flags scrollbar">
    {#if error}
      <p class="mutation-error" role="alert">{error}</p>
    {/if}

    <!-- No heading over the table (#697/#700): round 30 draws the flags
         panel as a bare table under the bar, and the count already
         lives in the bar's own ⚑ mark. The name survives for screen
         readers, where it is not competing for space. -->
    <section aria-label="Active flags ({active.length})">
      {#if active.length === 0 && !(isAdminOrOpen && exclusions.length > 0)}
        <!-- The honest cleared state (round 26, drawn as `.caempty` in
             round 29's scene): zero open is a fact with a history, not a
             blank. When something was cleared, say when, and stand by
             the audit-log promise the clear-all bubble makes. Skipped
             when the exclusions body (#780 item 5) has something to
             show even with nothing open, below. -->
        <div class="caempty">
          <span class="cae-mark">✓</span>
          <div>
            <b>Nothing open.</b>
            {#if lastClearedAt}
              Cleared {clearedWhen(lastClearedAt)} — they keep their place{#if isAdminOrOpen}&nbsp;in the
                <button class="olink" onclick={() => (appState.view = 'audit')}>audit log</button>{/if}.
            {:else if provisionalActive.length > 0}
              <!-- "Nothing has been flagged yet" cannot sit above an
                   occupied shelf (#642, ruling amendment 3): something
                   plainly fired, it just is not trusted yet. -->
              What has fired so far is not yet trusted — it sits on the learning shelf below.
            {:else}
              Nothing has been flagged yet.
            {/if}
          </div>
        </div>
      {:else}
        <!-- The ratified table (#688, round 29's `#s7`): flag · where ·
             evidence · count · age, then the disclosure. Both the sort
             (the head) and the filter (the quiet dashed row beneath it)
             live in the head group, as the record's own table has
             them. -->
        <table class="ftable">
          <thead>
            <tr>
              <th>
                <button class="sorth" class:on={sortKey === 'type'} onclick={() => toggleSort('type')}>
                  flag <span class="dir">{dirGlyph('type')}</span>
                </button>
              </th>
              <th>
                <button class="sorth" class:on={sortKey === 'where'} onclick={() => toggleSort('where')}>
                  where <span class="dir">{dirGlyph('where')}</span>
                </button>
              </th>
              <th>
                <button class="sorth" class:on={sortKey === 'evidence'} onclick={() => toggleSort('evidence')}>
                  evidence <span class="dir">{dirGlyph('evidence')}</span>
                </button>
              </th>
              <th class="num">
                <button class="sorth" class:on={sortKey === 'count'} onclick={() => toggleSort('count')}>
                  count <span class="dir">{dirGlyph('count')}</span>
                </button>
              </th>
              <th>
                <button class="sorth" class:on={sortKey === 'age'} onclick={() => toggleSort('age')}>
                  age <span class="dir">{dirGlyph('age')}</span>
                </button>
              </th>
              <!-- CALL IT (#780 item 1): no filter input under this head --
                   verdicts are given, not searched. Blank for a viewer,
                   who gets no chips to call anything with. -->
              <th class="vc">{canEdit ? 'call it' : ''}</th>
            </tr>
            <tr class="filters">
              <td><input bind:value={filters.type} placeholder="filter" aria-label="Filter by flag type" /></td>
              <td><input bind:value={filters.where} placeholder="filter" aria-label="Filter by where" /></td>
              <td><input bind:value={filters.evidence} placeholder="filter" aria-label="Filter by evidence" /></td>
              <td><input bind:value={filters.count} placeholder="filter" aria-label="Filter by count" /></td>
              <td><input bind:value={filters.age} placeholder="filter" aria-label="Filter by age" /></td>
              <td></td>
            </tr>
          </thead>
          {#if active.length > 0}
            <tbody>
              {#if sortedActive.length === 0}
                <tr>
                  <td class="empty" colspan="6">No flags match these filters.</td>
                </tr>
              {/if}
              {#each sortedActive as f (f.id)}
                {@render flagRows(f, false)}
              {/each}
            </tbody>
          {/if}
          {#if isAdminOrOpen && exclusions.length > 0}
            {@render exclusionsBody()}
          {/if}
        </table>
      {/if}
    </section>

    <!-- The learning shelf (#642): the bounded region for provisional
         flags, below the settled table -- an untrusted item never
         outranks a trusted one -- and never a fourth docket tab, which
         would hide the learning state from the very person reviewing
         flags. Present when it has contents or (for user/admin, who can
         read the warming signal) while any baseline is warming; absent
         otherwise, as everywhere else in the app (#653). -->
    {#if showShelf}
      <section class="shelf" aria-label="Learning shelf ({provisionalActive.length} provisional)">
        <h2 class="shelf-head">
          learning
          {#if provisionalActive.length > 0}
            <span class="shelf-count">— {provisionalActive.length} provisional</span>
          {/if}
        </h2>
        {#if provisionalActive.length > 0}
          <!-- The same five ratified columns as the settled table, so
               the two read as one surface; no sort or filter row -- a
               shelf is a holding area, not a second ledger. -->
          <table class="ftable">
            <thead>
              <tr class="shelf-heads">
                <th>flag</th>
                <th>where</th>
                <th>evidence</th>
                <th class="num">count</th>
                <th>age</th>
                <th class="vc">{canEdit ? 'call it' : ''}</th>
              </tr>
            </thead>
            <tbody>
              {#each provisionalActive as f (f.id)}
                {@render flagRows(f, true)}
              {/each}
            </tbody>
          </table>
        {:else}
          <!-- The warming case with nothing on the shelf yet: the
               answer to "why is it silent", in words -- the point of
               the issue, not decoration. -->
          <p class="shelf-warm">
            Baselines are still warming. A spike seen now is not thrown away — it appears here as a provisional flag,
            marked as one mikroview does not yet trust. Nothing has fired during warm-up yet.
          </p>
        {/if}
      </section>
    {/if}
  </div>
</div>

<!-- Disarms `never again`'s confirm on any click that isn't the button
     itself -- the button's own onclick stops propagation, so this only
     ever fires for a click elsewhere (#780 item 4, same idiom as
     Watchlist's own armed `remove`). -->
<svelte:window onclick={() => (neverArmedId = null)} />

{#snippet flagRows(f: Flag, provisional: boolean)}
  {@const family = familyOf(f.type)}
  {@const open = expandedId === f.id}
  {@const ep = episodes[f.id]}
  {@const kind = verdictKind(f)}
  <tr
    class="frow"
    class:open
    class:provisional
    class:struck={kind !== null}
    class:fdone={kind !== null && kind !== 'real'}
    class:isreal={kind === 'real'}
    class:expected={kind === 'expected'}
    class:noise={kind === 'noise'}
    class:real={kind === 'real'}
    style="--ft: {family.ink}"
    onclick={() => toggleExpanded(f)}
  >
    <td class="fmark">{family.mark} {labelFor(f.type)}{#if provisional}<span class="ptag">provisional</span>{/if}</td>
    <td class="k">
      {#if isFilterable(f)}
        <button
          class="wl"
          title="Open {f.target} in the topography"
          onclick={(ev) => {
            ev.stopPropagation()
            openWhere(f)
          }}
        >
          {f.target}
        </button>
      {:else}
        <span class="wl-plain">network-wide</span>
      {/if}
    </td>
    <td>{f.detail}</td>
    <td class="num">{f.count}×</td>
    <td class="t">{formatFlagAge(f.lastSeen, appState.now)}</td>
    <td class="vc">
      <!-- CALL IT (#780): a stamp once judged, the trio while it's
           still open and callable. A chip/undo click never toggles the
           drawer, same stopPropagation guard the "where" link and
           caret already use above. -->
      {#if kind}
        <span class="vdone">
          <span class="stamp {stampInk(kind)}">{stampText(kind)}</span>
          {#if canUndo(kind)}
            <button
              class="olink"
              onclick={(ev) => {
                ev.stopPropagation()
                undoCall(f)
              }}>undo</button
            >
          {/if}
        </span>
      {:else if canEdit}
        <span class="vrow">
          <button
            class="v expected"
            title="Normal for this network — records the verdict and clears the flag"
            onclick={(ev) => {
              ev.stopPropagation()
              callVerdict(f, 'expected')
            }}><i>✓</i>expected</button
          >
          <button
            class="v noise"
            title="Not meaningful — records the verdict and clears the flag"
            onclick={(ev) => {
              ev.stopPropagation()
              callVerdict(f, 'noise')
            }}><i>~</i>noise</button
          >
          <button
            class="v real"
            title="A real finding — records the verdict; the flag stays"
            onclick={(ev) => {
              ev.stopPropagation()
              callReal(f)
            }}><i>✱</i>real</button
          >
        </span>
      {/if}
      {#if kind === null || kind === 'real'}
        <!-- The row's one affordance (rounds 18-19/29): the
             chevron rotates rather than swapping glyphs, so the
             open state reads at a glance down a striped list.
             Dropped once a non-real verdict lands (#780 item 2,
             round 35's `close(r)`): a dimmed row is inert, beyond
             its stamp and undo. -->
        <button
          class="openc"
          aria-expanded={open}
          aria-label="{open ? 'Close' : 'Open'} the drawer for this flag"
          onclick={(ev) => {
            ev.stopPropagation()
            toggleExpanded(f)
          }}
        >
          ▸
        </button>
      {/if}
    </td>
  </tr>
  {#if open}
    <!-- The drawer (round 29): the story and the matched
         lines down the left, the episode's shape on the
         right, the actions across the foot. The type's
         stripe runs on through it unbroken. -->
    <tr class="drawer" class:provisional style="--ft: {family.ink}">
      <td colspan="6">
        <div class="dwr-in">
          {#if provisional}
            <!-- The label's meaning, in a sentence (#616:
                 worded, never shape alone). -->
            <p class="pwhy">
              provisional — its baseline was still warming when this fired, so mikroview does not yet trust
              the comparison behind it. Not counted as an open flag.
            </p>
          {/if}
          <!-- The headline and story (#678): plain-English
               writing generated per flag type from the
               evidence the flag already carries, not the raw
               evidence itself -- the headline stands alone
               as the drawer's first words, the story running
               on from it in sentences. See flagNarrative.ts. -->
          <p class="story">
            {#if f.verdict === 'real' && f.verdictAt}
              <!-- #780 item 3: the story leads with this rather
                   than only the row's stamp carrying the news --
                   a flag arriving already verdict==='real' (from
                   another session) reads the same way. -->
              <span class="called"
                >Called real at {formatHM(f.verdictAt)} by {f.verdictBy}. It stays open until it is cleared; a fresh
                episode asks again.</span
              >
            {/if}
            <b class="headline">{headlineFor(f)}</b> {storyFor(f)}
          </p>
          <div class="side">
            <span class="lab">the episode</span>
            {#if Array.isArray(ep) && ep.length > 0}
              <svg
                viewBox="0 0 260 34"
                preserveAspectRatio="none"
                role="img"
                aria-label="{ep.length} events, drawn on a strip of the half hour around last seen"
              >
                {#each episodeTicks(ep) as x, i (i)}
                  <line
                    x1={x}
                    y1="6"
                    x2={x}
                    y2="28"
                    stroke="var(--ft)"
                    stroke-width="2.5"
                    stroke-linecap="round"
                  />
                {/each}
              </svg>
            {/if}
            <!-- The episode's shape (#678): still arriving,
                 stopped, or intermittent -- derived from the
                 flag's own timestamps (the richer per-event
                 episode once it's fetched, the flag's
                 firstSeen/lastSeen before then). See
                 episodeShape.ts. -->
            <span class="span">{episodeShapeFor(f, ep, appState.now)}</span>
            {#if ep === 'loading'}
              <p class="ep-note">fetching the events…</p>
            {:else if ep === 'error'}
              <p class="ep-note">could not fetch the events</p>
            {:else if Array.isArray(ep) && ep.length === 0}
              <!-- Raw events are only retained in the
                   buffer; an old flag honestly says the
                   window has moved on rather than drawing an
                   empty strip. -->
              <p class="ep-note">no matching events still buffered</p>
            {/if}
          </div>
          {#if Array.isArray(ep) && ep.length > 0}
            <div class="lines">
              {#each ep.slice(0, 3) as e (e.id)}
                <div>{eventLine(e)}</div>
              {/each}
            </div>
          {/if}
          <!-- #750 group B item 3, the evidence-truncation line, goes
               here: one foot closing the pairs list -- "12 of 340
               pairs", or "12 of at least 340 pairs" when the total is a
               floor, and no line at all when nothing is cut. The
               wording is ruled and built (evidencePairs.ts's
               pairsTruncationLabel, with its own tests); what it counts
               is not on screen. 68fd460 dropped the per-host pairs
               panel when this drawer was rebuilt to round 29, and #791
               is where it comes back -- placement inside this drawer is
               that issue's open design call, not this one's. A foot
               reading "12 of 340 pairs" above no pairs would disclose
               a truncation of nothing, so the line lands with the list
               it closes. -->

          <div class="dwr-acts">
            {#if isFilterable(f)}
              <button class="act" onclick={() => filterToTarget(f)}>open in stream ▸</button>
            {/if}
            {#if canEdit && canWatchPathway(f)}
              <button class="act" onclick={() => watchThisPathway(f)}>watch this pathway</button>
            {:else if canEdit && canWatchSource(f)}
              <button class="act" onclick={() => watchThisSource(f)}>watch this source</button>
            {/if}
            {#if noteFor === f.id}
              <!-- The note is optional: confirming with an
                   empty one is exactly a plain clear. -->
              <div class="clear-note" role="group" aria-label="Clear with a note">
                <input
                  type="text"
                  class="clear-note-input"
                  placeholder="add a note (optional)"
                  aria-label="Note for clearing this flag"
                  bind:value={noteDraft}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') confirmClear(f.id)
                    if (e.key === 'Escape') cancelNote()
                  }}
                />
                <button class="act" onclick={() => confirmClear(f.id)}>Clear</button>
                <button class="act quiet" onclick={cancelNote}>Cancel</button>
              </div>
            {:else if canEdit}
              <button class="act quiet" onclick={() => openNote(f.id)}>clear with a note</button>
            {/if}
            {#if isAdminOrOpen}
              <!-- `never again` (#780 item 4): the one verdict left in
                   the drawer, alone at the right of the action row
                   (`.never { margin-left: auto }`) -- it wants a
                   second look and the drawer is where the evidence is.
                   Round 28's arm-then-confirm, admin-only per
                   clear-permanent's own authz tier. -->
              <button
                class="act quiet never"
                class:armed={neverArmedId === f.id}
                onclick={(ev) => {
                  ev.stopPropagation()
                  clickNever(f)
                }}
              >
                {neverLabel(f)}
              </button>
            {/if}
          </div>
        </div>
      </td>
    </tr>
  {/if}
{/snippet}

{#snippet exclusionsBody()}
  <!-- The exclusions body (#780 item 5): a second tbody under the
       flags, in the flag row's own grammar with the type's ink off --
       mikroview does not look at these pairs any more. Sort and filter
       (above) leave this body alone; it is fed and iterated on its own. -->
  <tbody>
    <tr class="excl-divider">
      <td colspan="6">
        <span class="excl-label"
          >never again · <b>{exclusions.length}</b> pair{exclusions.length === 1 ? '' : 's'} mikroview no longer flags</span
        >
        <button class="olink excl-toggle" onclick={() => (showExclusions = !showExclusions)}>
          {showExclusions ? 'hide them' : 'show them'}
        </button>
      </td>
    </tr>
    {#if showExclusions}
      {#each exclusions as e (e.id)}
        {@const exFamily = familyOf(e.type)}
        {@const exOpen = expandedExclusionId === e.id}
        <tr class="frow fx" class:open={exOpen} onclick={() => toggleExclusion(e)}>
          <td class="fmark">{exFamily.mark} {labelFor(e.type)}</td>
          <td class="k"><span class="wl-plain">{e.target === 'global' ? 'network-wide' : e.target}</span></td>
          <td>never again</td>
          <td class="num">—</td>
          <td class="t">—</td>
          <td class="vc">
            <button
              class="openc"
              aria-expanded={exOpen}
              aria-label="{exOpen ? 'Close' : 'Open'} the drawer for this exclusion"
              onclick={(ev) => {
                ev.stopPropagation()
                toggleExclusion(e)
              }}
            >
              ▸
            </button>
          </td>
        </tr>
        {#if exOpen}
          <tr class="drawer fx">
            <td colspan="6">
              <div class="dwr-in">
                <p class="story">
                  Told never to flag this again. Nothing about this pair is counted or held now — mikroview simply
                  does not look.
                </p>
                <dl class="pair-facts">
                  <div><dt>flag</dt><dd>{labelFor(e.type)}</dd></div>
                  <div><dt>target</dt><dd>{e.target === 'global' ? 'network-wide' : e.target}</dd></div>
                </dl>
                <div class="dwr-acts">
                  <button class="act" onclick={() => letItFireAgain(e)}>let it fire again</button>
                </div>
              </div>
            </td>
          </tr>
        {/if}
      {/each}
    {/if}
  </tbody>
{/snippet}

<style>
  .flags-page {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    /* #616's dark-not-quiet grammar, as the fall already draws it (its
       45-degree hatch pattern): the same claim -- absence of a trusted
       signal, not absence of traffic -- so the same shape, quiet enough
       to sit under the row's own inks. */
    --shelf-hatch: repeating-linear-gradient(
      45deg,
      transparent 0 5px,
      color-mix(in srgb, var(--fg-dim) 14%, transparent) 5px 6px
    );
  }

  .flags {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  /* Same treatment Watchlist and Entities give their own mutation
     errors, so a failed clear reads the same way everywhere. */
  .mutation-error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }


  /* The ratified table (round 29, `#s7`'s `.panel table`): the record's
     own geometry and inks, with this app's theme variables standing in
     for the mockup's --ink/--hair/--mono names. The peer watchlist table
     (#676) was ported from the same scene, so the two read as one
     surface. */
  .ftable {
    border-collapse: collapse;
    width: 100%;
    font-family: var(--font-mono);
    font-size: 12px;
  }

  .ftable th,
  .ftable td {
    padding: 8px 12px;
    text-align: left;
  }

  .ftable thead th {
    padding: 0 12px 6px;
    border-bottom: 1px solid var(--border);
  }

  .ftable thead th.num {
    text-align: right;
  }

  /* `.panel thead th` in the record: the head *is* the sort control. A
     button rather than a click handler on the th, so it is reachable
     from the keyboard. */
  .sorth {
    background: transparent;
    border: none;
    padding: 0;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    white-space: nowrap;
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
    margin-left: 5px;
    color: var(--accent);
    font-size: 9px;
  }

  /* Every column doubles as a filter: the record's quiet dashed inline
     row under the heads (`.panel tr.filters`). */
  .ftable tr.filters td {
    padding: 2px 12px 8px;
    border-bottom: 1px solid var(--border);
  }

  .ftable tr.filters input {
    width: 100%;
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-muted);
    padding: 2px 0;
    outline: none;
  }

  .ftable tr.filters input::placeholder {
    color: var(--fg-dim);
    opacity: 0.6;
  }

  .ftable tr.filters input:focus {
    border-bottom-color: var(--accent);
  }

  .ftable tbody td {
    color: var(--fg-muted);
    border-bottom: 1px solid var(--border);
  }

  .ftable tbody td.k {
    color: var(--fg);
  }

  .ftable tbody td.t {
    color: var(--fg-dim);
  }

  .ftable tbody td.num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  /* CALL IT (#780): right-aligned like the record's `.panel td.vc` --
     the trio/stamp and caret hug the row's own right edge. */
  .ftable thead th.vc,
  .shelf-heads th.vc {
    text-align: right;
  }

  .ftable tbody td.vc {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }

  .frow {
    cursor: pointer;
  }

  /* background-color, not the background shorthand: a provisional
     row's hatch rides in background-image and must survive hover and
     open (#642). */
  .frow:hover td {
    background-color: var(--bg-hover);
  }

  /* An open row hands its bottom edge to the drawer, so the two read as
     one block rather than as two stacked rows. */
  .frow.open td {
    background-color: var(--bg-hover);
    border-bottom-color: transparent;
  }

  /* The shelf's rows and drawers are hatched -- shape -- and every one
     also wears the worded .ptag/.pwhy -- label -- per #616: never
     meaning by colour (or texture) alone. */
  .frow.provisional td,
  tr.drawer.provisional > td {
    background-image: var(--shelf-hatch);
  }

  /* One unbroken line: row and drawer share the type's stripe, and the
     mark wears the same ink (`.ft-*` in the record -- six fixed hexes,
     carried here by lib/flagPalette.ts as --ft so a custom detector's
     accent ink works the same way). */
  .frow td:first-child,
  tr.drawer > td {
    box-shadow: inset 3px 0 0 var(--ft);
  }

  .fmark {
    font-weight: 700;
    white-space: nowrap;
    text-transform: uppercase;
  }

  /* `.ftable tbody td` above sets the muted body ink at higher CSS
     specificity (class+type+type) than a bare `.fmark` (class alone)
     can beat, which is why the label text was landing on --fg-muted
     instead of its family ink even though --ft was already wired
     through onto the row. Round 30's label wears the flag's own
     severity colour (the record's `.ft-* .fmark` rule), matching the
     ratified six-hex palette in lib/flagPalette.ts -- only the
     selector's specificity needed fixing, not the colour source. */
  .ftable tbody td.fmark {
    color: var(--ft);
  }

  /* `.wl` in the record: a named where is a link, dotted underneath, and
     it goes into the topography rather than the stream (#678). */
  .wl {
    background: none;
    border: none;
    border-bottom: 1px dotted var(--border);
    padding: 0;
    font: inherit;
    color: var(--fg);
    cursor: pointer;
  }

  .wl:hover {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  .wl-plain {
    color: var(--fg-dim);
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

  /* ============================================================
     CALL IT (#780, rounds 34-35): the verdict trio moves from the
     drawer into the row itself. Ported from docs/design/concepts/
     round-35/verdicts-in-row.html's .vrow/.stamp/tr.frow.struck/
     .vdone/the lens bar, mapped onto this app's own tokens the way
     EngineRoom.svelte's settings-doors port already does (--ok ->
     --accept); --now/--alarm/--hair-2 are this app's own names
     verbatim, so they carry over unchanged.
     ============================================================ */
  .vrow {
    display: inline-flex;
    gap: 6px;
    vertical-align: middle;
    margin-right: 12px;
  }

  .vrow button.v {
    --vc: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-muted);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 1px 9px 1px 7px;
    cursor: pointer;
    line-height: 16px;
    transition:
      color 0.15s,
      border-color 0.15s,
      background 0.15s,
      transform 0.15s;
  }

  .vrow button.v i {
    font-style: normal;
    color: var(--vc);
    margin-right: 5px;
    font-weight: 700;
  }

  .vrow button.v.expected {
    --vc: var(--accept);
  }

  .vrow button.v.noise {
    --vc: var(--now);
  }

  .vrow button.v.real {
    --vc: var(--alarm);
  }

  .vrow button.v:hover {
    color: var(--vc);
    border-color: var(--vc);
    background: color-mix(in srgb, var(--vc) 12%, transparent);
    transform: translateY(-1px);
  }

  /* The stamp: a verdict pressed onto the row in its own ink. */
  .stamp {
    --vc: var(--fg-dim);
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--vc);
    border: 1.5px solid var(--vc);
    border-radius: 3px;
    padding: 1px 7px 1px 8px;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--vc) 35%, transparent);
    animation: stamp-in 0.28s cubic-bezier(0.2, 1.6, 0.4, 1) both;
    vertical-align: middle;
  }

  .stamp.expected {
    --vc: var(--accept);
  }

  .stamp.noise {
    --vc: var(--now);
  }

  .stamp.real {
    --vc: var(--alarm);
  }

  @keyframes stamp-in {
    from {
      transform: scale(1.9);
      opacity: 0;
    }
    60% {
      opacity: 1;
    }
    to {
      transform: scale(1);
    }
  }

  /* The row takes the ink for a moment as the stamp lands. */
  .frow.struck td {
    animation: struck 0.7s ease-out both;
  }

  @keyframes struck {
    from {
      background: color-mix(in srgb, var(--sc, var(--fg-dim)) 16%, transparent);
    }
  }

  .frow.struck.expected {
    --sc: var(--accept);
  }

  .frow.struck.noise {
    --sc: var(--now);
  }

  .frow.struck.real {
    --sc: var(--alarm);
  }

  /* Takes the trio's place in the same cell, so the column never
     moves. */
  .vdone {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    vertical-align: middle;
  }

  .vdone + .openc {
    margin-left: 16px;
  }

  /* Called real: leads the story (see the drawer's .story above). */
  .story .called {
    display: block;
    color: var(--fg-dim);
    font-size: 11px;
    margin-bottom: 6px;
  }

  /* Called real: the stamp takes the chips' place and the row's own
     bar swells -- a lens over the 3px family stripe, its ends arcing
     back into it rather than stepping against the rows around it.
     Always alarm ink, regardless of the flag's own family colour: real
     is the one state every family shares the same urgency for. */
  .frow td:first-child {
    position: relative;
  }

  .frow td:first-child::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--alarm);
    opacity: 0;
    border-radius: 0 4px 4px 0 / 0 10px 10px 0;
    transition:
      width 0.35s ease,
      opacity 0.35s ease;
  }

  .frow.isreal td:first-child::before {
    width: 7px;
    opacity: 1;
  }

  /* A flag that has been called (anything but real): dims in place,
     keeps its row, until the tab is left (#780 items 2/4 -- the
     recently-cleared list, in place). Its CALL IT cell stays at full
     opacity so the stamp and undo stay legible. */
  .frow.fdone td {
    opacity: 0.45;
  }

  .frow.fdone td:first-child {
    box-shadow: none !important;
  }

  .frow.fdone td:last-child {
    opacity: 1;
  }

  /* never again (#780 item 4): round 28's arm-then-confirm, same idiom
     as Watchlist's own armed `remove`. */
  .dwr-acts .never {
    margin-left: auto;
  }

  .dwr-acts .never.armed {
    color: var(--alarm);
    border-color: var(--alarm);
  }

  /* The exclusions body (#780 item 5): a second tbody under the flags,
     type ink off -- mikroview does not look at these pairs any more. */
  .excl-divider td {
    padding-top: 16px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    letter-spacing: 0.06em;
    color: var(--fg-dim);
  }

  .excl-divider b {
    color: var(--fg-muted);
    font-weight: 600;
  }

  .excl-toggle {
    margin-left: 12px;
  }

  .frow.fx .fmark {
    color: var(--fg-dim) !important;
  }

  .frow.fx td:first-child,
  tr.drawer.fx > td {
    box-shadow: none !important;
  }

  .frow.fx td {
    opacity: 0.7;
  }

  .frow.fx.open td {
    opacity: 1;
  }

  .pair-facts {
    grid-column: 1 / -1;
    display: flex;
    gap: 24px;
    margin: 0 0 4px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
  }

  .pair-facts div {
    display: flex;
    gap: 6px;
  }

  .pair-facts dt {
    text-transform: uppercase;
    letter-spacing: 0.1em;
    font-size: 9px;
  }

  .pair-facts dd {
    margin: 0;
    color: var(--fg-muted);
  }

  .empty {
    color: var(--fg-dim);
    font-size: 12px;
  }

  /* The drawer's grid, straight from the record's `.dwr-in`: story over
     matched lines down the left, the episode spanning both rows on the
     right, the actions across the foot. */
  tr.drawer > td {
    padding: 0 12px 14px;
    border-bottom: 1px solid var(--border);
  }

  .dwr-in {
    display: grid;
    grid-template-columns: 1.3fr 1fr;
    gap: 10px 32px;
    padding: 6px 8px 4px;
  }

  .dwr-in .story {
    grid-column: 1;
    margin: 0;
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--fg-muted);
    line-height: 1.55;
  }

  .dwr-in .story .headline {
    color: var(--fg);
    font-weight: 600;
  }

  .dwr-in .lines {
    grid-column: 1;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .dwr-in .side {
    grid-column: 2;
    grid-row: 1 / span 2;
    min-width: 0;
  }

  .dwr-in .side .lab {
    display: block;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .dwr-in .side svg {
    display: block;
    width: 100%;
    height: 34px;
    margin: 6px 0 2px;
  }

  .dwr-in .side .span {
    display: block;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .dwr-in .ep-note {
    margin: 4px 0 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .dwr-acts {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 2px;
  }

  .act {
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 16px;
    cursor: pointer;
  }

  .act:hover {
    border-color: var(--accent);
  }

  .act.quiet {
    color: var(--fg-dim);
  }

  .clear-note {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    min-width: 180px;
  }

  .clear-note-input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 12px;
    font-size: 12px;
    color: var(--fg);
    outline: none;
  }

  .clear-note-input:focus {
    border-color: var(--accent);
  }

  /* The honest cleared state (round 26, `.caempty` in round 29). */
  .caempty {
    display: flex;
    align-items: baseline;
    gap: 10px;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .caempty b {
    color: var(--fg);
  }

  .cae-mark {
    color: var(--accept);
    font-weight: 700;
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover {
    text-decoration-color: currentColor;
  }

  /* The learning shelf (#642). Its heading is the section's own label
     -- the docket switcher carries no counts (round 30), so the shelf's
     number lives here -- wearing a small hatch swatch so the section
     and its rows share one grammar. */
  .shelf-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 4px 0 0;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .shelf-head::before {
    content: '';
    display: inline-block;
    width: 18px;
    height: 9px;
    border: 1px solid var(--border);
    background-image: var(--shelf-hatch);
  }

  .shelf-count {
    color: var(--fg);
  }

  .shelf .ftable {
    margin-top: 8px;
  }

  /* The shelf's column heads: the settled table's head typography
     without its sort/filter machinery -- a shelf is a holding area,
     not a second ledger. */
  .shelf-heads th {
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .shelf-warm {
    margin: 8px 0 0;
    max-width: 62ch;
    font-family: var(--font-sans);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--fg-muted);
  }

  /* The worded half of the provisional marking (#616). Inside .fmark it
     must not inherit the family ink -- it is a trust status, not a
     family. */
  .ptag {
    margin-left: 8px;
    padding: 1px 5px;
    border: 1px dashed var(--fg-dim);
    border-radius: 3px;
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.1em;
    color: var(--fg-muted);
  }

  .pwhy {
    grid-column: 1 / -1;
    margin: 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  @media (prefers-reduced-motion: reduce) {
    .openc {
      transition: none;
    }

    /* #780: the stamp's thump, the row's flash and the chip's hover
       lift all turn off -- the lens bar's width/opacity transition
       already shares .frow td:first-child::before's own declaration,
       so it is covered by the blanket rule below rather than repeated. */
    .stamp,
    .frow.struck td,
    .frow td:first-child::before {
      animation: none;
      transition: none;
    }

    .vrow button.v:hover {
      transform: none;
    }
  }
</style>
