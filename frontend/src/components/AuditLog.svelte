<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only audit log (issue #112): a read-only, most-recent-first
  // table of every admin-privileged mutation mikroview has recorded --
  // who created a user, changed a detector setting, upserted/deleted an
  // entity, created/revoked an API token, or removed a permanent flag
  // exclusion. See internal/audit.Entry -- nothing here is editable from
  // the UI, mirroring Fleet.svelte's plain read-only table shape rather
  // than Entities.svelte's form-backed CRUD one, since there's nothing
  // to create/edit/delete about a historical log entry.
  import { onMount } from 'svelte'
  import { auditState } from '../lib/audit.svelte'
  import { appState } from '../lib/state.svelte'
  import { formatRelative, formatHM } from '../lib/format'
  import { compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import type { AuditEntry } from '../lib/types'

  onMount(() => {
    auditState.refresh()
  })

  // Every column sorts and filters (#649): click a head to sort by it,
  // again to reverse; a quiet dashed row beneath the heads narrows the
  // list, per round-18/19's ratified idiom. Time defaults to newest
  // first, matching the fixed order this replaces.
  type SortKey = 'time' | 'actor' | 'action' | 'target' | 'detail'
  let sortKey = $state<SortKey>('time')
  let sortDir = $state<SortDir>('desc')
  let filters = $state({ time: '', actor: '', action: '', target: '', detail: '' })

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = key === 'time' ? 'desc' : 'asc'
    }
  }

  function dirGlyph(key: SortKey): string {
    if (sortKey !== key) return ''
    return sortDir === 'asc' ? '▲' : '▼'
  }

  const filtered = $derived(
    auditState.list.filter(
      (e) =>
        matchesFilter(formatRelative(e.timestamp, appState.now), filters.time) &&
        matchesFilter(e.actor, filters.actor) &&
        matchesFilter(e.action, filters.action) &&
        matchesFilter(e.target, filters.target) &&
        matchesFilter(e.detail || '', filters.detail),
    ),
  )

  const rows = $derived.by((): AuditEntry[] => {
    const list = [...filtered]
    list.sort((a, b) => {
      switch (sortKey) {
        case 'time':
          return compareText(a.timestamp, b.timestamp, sortDir)
        case 'actor':
          return compareText(a.actor, b.actor, sortDir)
        case 'action':
          return compareText(a.action, b.action, sortDir)
        case 'target':
          return compareText(a.target, b.target, sortDir)
        case 'detail':
          return compareText(a.detail || '', b.detail || '', sortDir)
      }
    })
    return list
  })
</script>

<div class="page scrollbar">
  <p class="intro">
    Every admin-privileged mutation mikroview has recorded -- who created a user, changed a detector setting,
    upserted/deleted an entity, created or revoked an API token, or removed a permanent flag exclusion. Read-only
    actions (viewing pages, listing users) are never logged here, only mutations.
    {#if auditState.hasMore}
      <span class="truncated">Showing the most recent entries only.</span>
    {/if}
  </p>

  {#if auditState.list.length === 0}
    <div class="empty">No admin actions recorded yet.</div>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th onclick={() => toggleSort('time')} aria-sort={sortKey === 'time' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
              Time <span class="dir">{dirGlyph('time')}</span>
            </th>
            <th onclick={() => toggleSort('actor')} aria-sort={sortKey === 'actor' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
              Actor <span class="dir">{dirGlyph('actor')}</span>
            </th>
            <th onclick={() => toggleSort('action')} aria-sort={sortKey === 'action' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
              Action <span class="dir">{dirGlyph('action')}</span>
            </th>
            <th onclick={() => toggleSort('target')} aria-sort={sortKey === 'target' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
              Target <span class="dir">{dirGlyph('target')}</span>
            </th>
            <th onclick={() => toggleSort('detail')} aria-sort={sortKey === 'detail' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
              Detail <span class="dir">{dirGlyph('detail')}</span>
            </th>
          </tr>
          <tr class="filters">
            <td><input bind:value={filters.time} placeholder="filter…" aria-label="Filter by time" /></td>
            <td><input bind:value={filters.actor} placeholder="filter…" aria-label="Filter by actor" /></td>
            <td><input bind:value={filters.action} placeholder="filter…" aria-label="Filter by action" /></td>
            <td><input bind:value={filters.target} placeholder="filter…" aria-label="Filter by target" /></td>
            <td><input bind:value={filters.detail} placeholder="filter…" aria-label="Filter by detail" /></td>
          </tr>
        </thead>
        <tbody>
          {#if rows.length === 0}
            <tr><td colspan="5" class="empty-filtered">No entries match these filters.</td></tr>
          {:else}
            {#each rows as e (e.id)}
              <tr>
                <td class="mono" title={formatHM(e.timestamp)}>{formatRelative(e.timestamp, appState.now)}</td>
                <td class="actor">{e.actor}</td>
                <td class="mono action">{e.action}</td>
                <td class="mono target">{e.target}</td>
                <td class="dim">{e.detail || '—'}</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {/if}
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

  .truncated {
    display: block;
    margin-top: 4px;
    color: var(--fg-dim, var(--fg-muted));
    font-style: italic;
  }

  .empty {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .table-wrap {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  th,
  td {
    padding: 9px 14px;
    text-align: left;
    white-space: nowrap;
  }

  th {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    user-select: none;
  }

  th:hover {
    color: var(--fg);
  }

  th .dir {
    display: inline-block;
    min-width: 8px;
    color: var(--accent);
    font-size: 9px;
  }

  /* The quiet dashed filter row (#649), beneath the heads -- matches
     round-18's idiom (docs/design/concepts/round-18/the-docket-opened.html):
     no border of its own, a dashed underline per input, dim until focused. */
  tr.filters td {
    padding: 2px 14px 8px;
    border-bottom: 1px solid var(--border);
  }

  tr.filters input {
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

  tr.filters input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  tr.filters input:focus {
    border-bottom-color: var(--accent);
  }

  .empty-filtered {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 14px;
    white-space: normal;
  }

  tbody tr {
    border-bottom: 1px solid var(--border);
  }

  tbody tr:last-child {
    border-bottom: none;
  }

  .mono {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .actor {
    color: var(--fg);
    font-weight: 600;
  }

  .action {
    color: var(--accent);
  }

  .target {
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dim {
    color: var(--fg-muted);
    white-space: normal;
    max-width: 320px;
  }

  @media (max-width: 700px) {
    th,
    td {
      padding: 8px 10px;
    }
  }
</style>
