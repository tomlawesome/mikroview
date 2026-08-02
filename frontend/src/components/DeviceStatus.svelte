<script lang="ts">
  import { appState } from '../lib/state.svelte'

  const STALE_MS = 2 * 60 * 1000

  let now = $state(Date.now())
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 5000)
    return () => clearInterval(id)
  })

  function isLive(lastSeen: string): boolean {
    return now - new Date(lastSeen).getTime() < STALE_MS
  }
</script>

<div class="devices">
  {#if appState.devices.length === 0}
    <span class="none">No RouterOS devices seen yet</span>
  {/if}
  {#each appState.devices as d (d.id)}
    <span class="device" class:stale={!isLive(d.lastSeen)} title="{d.eventCount} events · {d.sourceIp}">
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
