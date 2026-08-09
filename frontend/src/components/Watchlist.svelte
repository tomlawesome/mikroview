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
  import { onMount } from 'svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import type { WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from '../lib/types'

  onMount(() => {
    watchlistState.refresh()
  })

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
      await watchlistState.remove(e.id)
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
      await watchlistState.setObserving(e.id, !e.observing)
    } finally {
      togglingObserve = null
    }
  }

  async function promoteOne(e: WatchlistEntry, d: WatchlistPermittedDest) {
    promoting = e.id + d.destIp + d.port
    try {
      await watchlistState.promote(e.id, [d])
    } finally {
      promoting = null
    }
  }

  // loadMatches is called each time the matches panel is opened rather
  // than cached indefinitely -- a match log is append-only and can
  // change between views, and the volumes here (an entry's own recent
  // matches) are small enough that refetching on open is cheap.
  async function loadMatches(e: WatchlistEntry) {
    if (!e.source?.mac && !e.source?.ip) return
    matchesByEntry[e.id] = 'loading'
    try {
      matchesByEntry[e.id] = await watchlistState.matchesFor(e.source.mac, e.source.ip)
    } catch {
      matchesByEntry[e.id] = 'error'
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
</script>

<div class="page scrollbar">
  <p class="intro">
    Watch attempts against specific ports (<strong>record</strong>, generalising what Control Ports did), or flip an
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
      <ul class="list">
        {#each watchlistState.entries as e (e.id)}
          <li class="card">
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
                      <p class="error">Could not load matches.</p>
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
  </section>
</div>

<style>
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

  .card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px;
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
