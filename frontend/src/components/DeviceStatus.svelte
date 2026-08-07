<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Always-visible glance-and-go strip -- see Fleet.svelte (issue #98)
  // for the richer dedicated view this deliberately doesn't try to
  // replace. `status` is server-computed by GET /api/devices (live/
  // stale/never_seen, see internal/api/rest.go's deviceStatus) against
  // the operator-configured deviceStaleAfter threshold -- reused here
  // rather than this component keeping its own separate, shorter,
  // hardcoded staleness heuristic, which used to disagree with both the
  // actual TypeDeviceSilence flag and Fleet.svelte's own status column.
  import { appState } from '../lib/state.svelte'
</script>

<div class="devices">
  {#if appState.devices.length === 0}
    <span class="none">No RouterOS devices seen yet</span>
  {/if}
  {#each appState.devices as d (d.id)}
    <span class="device" class:stale={d.status !== 'live'} title="{d.eventCount} events · {d.sourceIp}">
      <span class="dot"></span>
      {d.name}
      {#if !d.configured}<span class="unregistered">unregistered</span>{/if}
    </span>
  {/each}
</div>

<style>
  .devices {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .none {
    color: var(--fg-dim);
    font-size: 13px;
  }

  .device {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accept);
    box-shadow: 0 0 6px var(--accept);
  }

  .stale .dot {
    background: var(--fg-dim);
    box-shadow: none;
  }

  .unregistered {
    font-size: 11px;
    color: var(--drop);
    border: 1px solid var(--drop);
    border-radius: 3px;
    padding: 1px 4px;
  }
</style>
