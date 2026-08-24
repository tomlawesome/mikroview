<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Permanent flag exclusions (issue #207), rendered inside Flags.svelte's
  // admin-only "Exclusions" tab (#547) -- briefly its own page after
  // #544 dropped its rail row, now folded back in per the ratified
  // navigation record (docs/design/screens/navigation/DESIGN.md:
  // "Exclusions is a tab of Flags"). Admin-only because the backend is:
  // GET/DELETE /api/flags/exclusions both 403 a non-admin caller (see
  // internal/api/authz_matrix_test.go), so Flags.svelte only mounts this
  // tab at all when the signed-in account is an admin.
  //
  // Every (detector, target) pair permanently silenced via Flags.svelte's
  // "Permanently clear" action -- removing one here lets it raise
  // normally again. This is the one undo path a permanent clear has; a
  // regular clear has none, by design (see Flags.svelte).
  import { onMount } from 'svelte'
  import { exclusionsState } from '../lib/exclusions.svelte'
  import type { FlagType } from '../lib/types'

  onMount(() => {
    exclusionsState.refresh()
  })

  // exclusionsState.remove optimistically drops the row, then restores
  // it and rethrows on failure (lib/exclusions.svelte.ts). With no catch
  // that was an unhandled rejection, and all the operator saw was the
  // exclusion reappearing -- indistinguishable from the button not
  // having worked.
  let error = $state<string | null>(null)

  // Same labels Flags.svelte/FlagsChart.svelte use -- duplicated rather
  // than shared, matching how ACTION_LABELS is already independently
  // duplicated in both EventsChart.svelte and Dashboard.svelte in this
  // codebase.
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

  let removingId = $state<string | null>(null)
  let typeFilter = $state<FlagType | ''>('')
  let targetFilter = $state('')

  async function remove(id: string) {
    removingId = id
    error = null
    try {
      await exclusionsState.remove(id)
    } catch (err) {
      error = err instanceof Error ? `Could not remove this exclusion: ${err.message}` : 'Could not remove this exclusion'
    } finally {
      removingId = null
    }
  }

  // Sorted however the server returns them (ListExclusions sorts by ID,
  // which is stable but not chronological -- Exclusion carries no
  // timestamp, and this page is scoped to be a client-side filter over
  // the existing two endpoints, not a reason to add one). Filtering
  // client-side is enough at the sizes this list actually reaches.
  const filtered = $derived(
    exclusionsState.list.filter((e) => {
      if (typeFilter && e.type !== typeFilter) return false
      if (targetFilter && !e.target.toLowerCase().includes(targetFilter.trim().toLowerCase())) return false
      return true
    }),
  )

  const typeOptions = $derived(
    [...new Set(exclusionsState.list.map((e) => e.type))].sort((a, b) =>
      TYPE_LABELS[a].localeCompare(TYPE_LABELS[b]),
    ),
  )
</script>

<div class="page scrollbar">
  {#if error}
    <p class="mutation-error" role="alert">{error}</p>
  {/if}

  <p class="intro">
    Every (detector, target) pair permanently silenced via "Permanently clear" on the Flags page -- removing one here
    lets it raise normally again, undoing a mistaken exclusion.
  </p>

  {#if exclusionsState.list.length > 0}
    <div class="toolbar">
      <span class="count">
        {filtered.length === exclusionsState.list.length
          ? `${exclusionsState.list.length} exclusion${exclusionsState.list.length === 1 ? '' : 's'}`
          : `${filtered.length} of ${exclusionsState.list.length}`}
      </span>

      <label class="filter">
        Detector
        <select bind:value={typeFilter}>
          <option value="">All</option>
          {#each typeOptions as t (t)}
            <option value={t}>{TYPE_LABELS[t]}</option>
          {/each}
        </select>
      </label>

      <label class="filter">
        Target
        <input type="search" placeholder="Filter by IP, host, or 'global'…" bind:value={targetFilter} />
      </label>
    </div>
  {/if}

  {#if exclusionsState.list.length === 0}
    <p class="empty">No permanent exclusions.</p>
  {:else if filtered.length === 0}
    <p class="empty">No exclusions match this filter.</p>
  {:else}
    <ul class="list">
      {#each filtered as e (e.id)}
        <li class="card">
          <div class="card-main">
            <span class="type">{TYPE_LABELS[e.type]}</span>
            <span class="target">{e.target === 'global' ? 'network-wide' : e.target}</span>
          </div>
          <button class="remove" disabled={removingId === e.id} onclick={() => remove(e.id)}>
            {removingId === e.id ? 'Removing…' : 'Remove exclusion'}
          </button>
        </li>
      {/each}
    </ul>
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

  /* Matches Flags/Watchlist/Entities, so a failed mutation reads the
     same way wherever it happens. */
  .mutation-error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 14px;
  }

  .count {
    font-size: 12px;
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }

  .filter {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .filter select,
  .filter input {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 5px 8px;
    font-size: 12px;
  }

  .filter input {
    width: 200px;
  }

  .filter select:focus,
  .filter input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
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
    align-items: baseline;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
  }

  .type {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .target {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
    overflow-wrap: anywhere;
  }

  .remove {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 6px 11px;
    font-size: 12px;
    flex: none;
  }

  .remove:hover:not(:disabled) {
    background: var(--drop-bg);
    color: var(--drop);
    border-color: var(--drop);
  }

  .remove:disabled {
    opacity: 0.6;
  }

  @media (max-width: 700px) {
    .filter input {
      width: 140px;
    }
  }
</style>
