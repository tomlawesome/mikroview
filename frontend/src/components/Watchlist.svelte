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
  // Admin-only throughout, matching GET /api/watchlist/entries' own gate
  // (see internal/api's authzMatrix) -- unlike the match query API
  // (accessUser, and reachable via a read-only token for external
  // correlation), entry management itself is administrative
  // configuration about the network, the same tier as Entities/Audit/
  // Exclusions.
  import { onMount, tick } from 'svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { suggestState } from '../lib/suggest.svelte'
  import { matchesState } from '../lib/matches.svelte'
  import { compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import TabList from './TabList.svelte'
  import Suggestions from './Suggestions.svelte'
  import MatchesTab from './MatchesTab.svelte'
  import type { WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from '../lib/types'

  onMount(() => {
    watchlistState.refresh()
    suggestState.refresh()
  })

  // Suggestions is a tab of Watchlist (#547) and Matches is a third
  // (#584), both per the ratified navigation record. No admin-gating
  // needed on the tabs themselves -- Watchlist only ever mounts for an
  // admin in the first place (see navGroups.ts's `admin: true` on the
  // Watchlist row), and /api/suggestions* agrees server-side
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
  function stateLabel(e: WatchlistEntry): string {
    if (!e.enabled) return 'paused'
    if (e.enabled && watchlistState.coverage[e.id] === 'no-logging') return 'ring broken'
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
</script>

<div class="watchlist-page">
  <TabList {tabs} selected={activeTab} onselect={selectTab} label="Watchlist views" />
  <div
    class="page scrollbar"
    role="tabpanel"
    id="panel-watchlist"
    aria-labelledby="tab-watchlist"
    tabindex="0"
    hidden={activeTab !== 'watchlist'}
  >
  <p class="intro">
    Watch attempts against specific ports (<strong>record</strong>), or flip an
    entry around to watch what one device does (<strong>invert</strong>): "this device should only ever reach X" --
    everything else it touches gets recorded. A new inverted entry starts <strong>observing</strong>: nothing fires
    until you review what it actually saw and promote the destinations that are expected. Matches are recorded to
    disk and survive a restart, unlike the live view's own volatile buffer.
  </p>

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

  <section class="section">
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
  </div>

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
</style>
