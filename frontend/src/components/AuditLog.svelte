<script lang="ts">
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

  onMount(() => {
    auditState.refresh()
  })

  const rows = $derived([...auditState.list].reverse())
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

  {#if rows.length === 0}
    <div class="empty">No admin actions recorded yet.</div>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Action</th>
            <th>Target</th>
            <th>Detail</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as e (e.id)}
            <tr>
              <td class="mono" title={formatHM(e.timestamp)}>{formatRelative(e.timestamp, appState.now)}</td>
              <td class="actor">{e.actor}</td>
              <td class="mono action">{e.action}</td>
              <td class="mono target">{e.target}</td>
              <td class="dim">{e.detail || '—'}</td>
            </tr>
          {/each}
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
