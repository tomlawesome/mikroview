<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import type { Action } from '../lib/types'

  const actions: { value: Action | ''; label: string }[] = [
    { value: '', label: 'Any action' },
    { value: 'accept', label: 'Accept' },
    { value: 'drop', label: 'Drop' },
    { value: 'reject', label: 'Reject' },
    { value: 'log', label: 'Log' },
    { value: 'unknown', label: 'Unknown' },
  ]
</script>

<div class="bar">
  <select bind:value={appState.filters.device} aria-label="Device">
    <option value="">Any device</option>
    {#each appState.devices as d (d.id)}
      <option value={d.id}>{d.name}</option>
    {/each}
  </select>

  <select bind:value={appState.filters.action} aria-label="Action">
    {#each actions as a (a.value)}
      <option value={a.value}>{a.label}</option>
    {/each}
  </select>

  <input
    type="text"
    placeholder="Protocol (tcp, udp, icmp…)"
    bind:value={appState.filters.protocol}
    aria-label="Protocol"
  />

  <input
    type="text"
    placeholder="IP or CIDR"
    bind:value={appState.filters.ip}
    aria-label="IP address or CIDR"
  />

  <input
    type="text"
    inputmode="numeric"
    placeholder="Port"
    bind:value={appState.filters.port}
    aria-label="Port"
  />

  <input
    type="text"
    placeholder="Interface"
    bind:value={appState.filters.interface}
    aria-label="Interface"
  />

  <input
    type="text"
    placeholder="Rule / label contains…"
    bind:value={appState.filters.rule}
    class="rule"
    aria-label="Rule label search"
  />

  {#if appState.hasActiveFilters}
    <button class="clear" onclick={() => appState.resetFilters()}>Clear filters</button>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 14px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  input,
  select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 14px;
    min-width: 0;
  }

  input::placeholder {
    color: var(--fg-dim);
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  input[type='text'] {
    width: 145px;
  }

  input[inputmode='numeric'] {
    width: 80px;
  }

  .rule {
    width: 200px;
    flex: 1 1 200px;
  }

  .clear {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 14px;
    font-size: 14px;
  }

  .clear:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
