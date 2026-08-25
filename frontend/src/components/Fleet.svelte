<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Multi-router-fleet health view (issue #98): every known device (both
  // configured, from config.yaml's `devices` list, and auto-discovered --
  // seen on the wire but not yet added there) in one table, with the
  // server-computed live/stale/never-seen status GET /api/devices now
  // reports (see internal/api/rest.go's deviceView/deviceStatus). This is
  // the richer, dedicated view the toolbar's small DeviceStatus dot-strip
  // was never meant to replace -- that one stays a glance-and-go
  // indicator (see its own doc comment); this one is where you'd actually
  // come to check on a whole fleet.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { formatRelative, formatHM } from '../lib/format'
  import type { Device } from '../lib/types'
  import GhostRows from './GhostRows.svelte'
  import PageHeader from './PageHeader.svelte'

  // How far back "recent activity" looks, client-side, from the live
  // event buffer -- a rough per-device rate to complement the lifetime
  // eventCount GET /api/devices already reports, without needing a new
  // backend endpoint. 5 minutes mirrors globalSpikeCheckInterval's
  // neighborhood of "recent enough to mean something, not so short it's
  // noisy between polls."
  const RECENT_WINDOW_MS = 5 * 60 * 1000

  const STATUS_LABEL: Record<Device['status'], string> = {
    live: 'Live',
    stale: 'Stale',
    never_seen: 'Never seen',
  }

  const rows = $derived(
    [...appState.devices].sort((a, b) => {
      // Configured devices first (an auto-discovered source is secondary
      // information, not something you set out to monitor), then by
      // status severity (stale/never-seen surfaced above live -- the
      // whole point of a fleet view is spotting the ones that need a
      // look), then alphabetically so the order is otherwise stable.
      if (a.configured !== b.configured) return a.configured ? -1 : 1
      const severity: Record<Device['status'], number> = { stale: 0, never_seen: 1, live: 2 }
      if (severity[a.status] !== severity[b.status]) return severity[a.status] - severity[b.status]
      return a.name.localeCompare(b.name)
    }),
  )

  function recentCount(deviceId: string): number {
    const cutoff = appState.now - RECENT_WINDOW_MS
    let n = 0
    for (const e of appState.events) {
      if (e.deviceId === deviceId && e.receivedAt >= cutoff) n++
    }
    return n
  }

  // True when this device has an active (unacknowledged) device_silence
  // flag -- distinct from status === 'stale': the flag only exists for a
  // *configured* device that was active and went quiet past
  // deviceStaleAfter, while `status` also covers auto-discovered devices
  // and a shorter/different threshold could in principle apply (today
  // they share deviceStaleAfter, but the API doesn't guarantee that).
  function hasActiveSilenceFlag(deviceId: string): boolean {
    return flagsState.list.some((f) => f.type === 'device_silence' && f.target === deviceId && !f.cleared)
  }

  // Mirrors LiveTable's own emptyState derived (#549): a zero-row table
  // is either "the app's one loadInitial() call hasn't come back yet" or
  // "it has, and mikroview has never seen a device" -- the second is
  // first-run's sharpest client-side signal, since seeing a device is
  // exactly what running setup produces. See appState.initialLoadDone's
  // doc comment for why that flag, rather than rows.length or fetchFailed
  // alone, is what tells the two apart.
  const emptyState = $derived.by((): { kind: 'ghost' } | { kind: 'text'; text: string } => {
    if (!appState.initialLoadDone) return { kind: 'ghost' }
    return {
      kind: 'text',
      text:
        authState.role === 'admin'
          ? 'No RouterOS devices seen yet — Admin ▸ Run setup… to point one at mikroview.'
          : 'No RouterOS devices seen yet. Ask an administrator to run setup.',
    }
  })
</script>

<div class="page scrollbar">
  <!-- No readOnly chip (#548/#490's grammar): this table has no edit
       affordance for anyone, admin included, so there is no
       admin-vs-viewer distinction here for a chip to declare. -->
  <PageHeader title="Fleet" />
  <p class="intro">
    Every RouterOS device mikroview has seen syslog from, or that's configured in <code>devices</code> but hasn't
    sent anything yet. A <strong>configured</strong> device that goes quiet for longer than the configured staleness
    threshold also raises a flag (see the Flags tab) -- this view is where you'd notice it happening, or notice a
    device that's simply never been wired up correctly in the first place.
  </p>

  {#if rows.length === 0}
    {#if emptyState.kind === 'ghost'}
      <GhostRows label="Loading devices…" rows={4} />
    {:else}
      <div class="empty">{emptyState.text}</div>
    {/if}
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Device</th>
            <th>Status</th>
            <th>Last seen</th>
            <th>Source IP</th>
            <th class="num">Events (total)</th>
            <th class="num">Recent (5m)</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as d (d.id)}
            <tr class:row-stale={d.status === 'stale'} class:row-never={d.status === 'never_seen'}>
              <td class="name-cell">
                <span class="name">{d.name}</span>
                {#if !d.configured}<span class="badge badge-unregistered">unregistered</span>{/if}
                {#if hasActiveSilenceFlag(d.id)}<span class="badge badge-flag">flagged</span>{/if}
              </td>
              <td>
                <span class="status status-{d.status}">
                  <span class="dot"></span>
                  {STATUS_LABEL[d.status]}
                </span>
              </td>
              <td class="mono" title={d.lastSeen ? formatHM(d.lastSeen) : '—'}>
                {d.status === 'never_seen' ? '—' : formatRelative(d.lastSeen, appState.now)}
              </td>
              <td class="mono dim">{d.sourceIp}</td>
              <td class="num mono">{d.eventCount}</td>
              <td class="num mono">{recentCount(d.id)}</td>
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

  .intro code {
    font-family: var(--font-mono);
    font-size: 12px;
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

  th.num,
  td.num {
    text-align: right;
  }

  tbody tr {
    border-bottom: 1px solid var(--border);
  }

  tbody tr:last-child {
    border-bottom: none;
  }

  .row-stale {
    background: color-mix(in srgb, var(--drop) 6%, transparent);
  }

  .row-never {
    background: color-mix(in srgb, var(--fg-dim) 6%, transparent);
  }

  .name-cell {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .name {
    color: var(--fg);
    font-weight: 600;
  }

  .mono {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .mono.dim {
    color: var(--fg-muted);
  }

  .badge {
    font-size: 11px;
    font-weight: 600;
    border-radius: 3px;
    padding: 1px 5px;
    white-space: nowrap;
  }

  .badge-unregistered {
    color: var(--drop);
    border: 1px solid var(--drop);
  }

  .badge-flag {
    color: var(--reject);
    background: var(--reject-bg);
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex: none;
  }

  .status-live .dot {
    background: var(--accept);
    box-shadow: 0 0 6px var(--accept);
  }

  .status-live {
    color: var(--accept);
  }

  .status-stale .dot {
    background: var(--drop);
    box-shadow: 0 0 6px var(--drop);
  }

  .status-stale {
    color: var(--drop);
  }

  .status-never_seen .dot {
    background: var(--fg-dim);
  }

  .status-never_seen {
    color: var(--fg-dim);
  }

  @media (max-width: 700px) {
    th,
    td {
      padding: 8px 10px;
    }
  }
</style>
