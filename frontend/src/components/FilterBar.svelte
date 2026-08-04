<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import type { Action } from '../lib/types'
  import FilterPresetsMenu from './FilterPresetsMenu.svelte'

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
  <FilterPresetsMenu />

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

  <div class="rule-group">
    <input
      type="text"
      placeholder={appState.filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…'}
      bind:value={appState.filters.rule}
      class="rule"
      aria-label={appState.filters.ruleRegex ? 'Rule/raw line regex search' : 'Rule label search'}
    />
    <button
      class="regex-toggle"
      class:active={appState.filters.ruleRegex}
      onclick={() => (appState.filters.ruleRegex = !appState.filters.ruleRegex)}
      title="Treat the rule search above as a regular expression (matches rule label or raw log line)"
      aria-pressed={appState.filters.ruleRegex}
    >
      .*
    </button>
  </div>

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

  .rule-group {
    display: flex;
    gap: 4px;
    flex: 1 1 200px;
  }

  .rule {
    width: 200px;
    flex: 1 1 200px;
  }

  .regex-toggle {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    border-radius: 5px;
    padding: 0 10px;
    font-family: var(--font-mono);
    font-size: 13px;
    flex: none;
  }

  .regex-toggle:hover {
    color: var(--fg-muted);
    border-color: var(--fg-muted);
  }

  .regex-toggle.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
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

  /* Below this width the fixed input widths (145px/200px/80px) leave
     several fields too narrow to comfortably type into once wrapped one
     per line -- let them fill the row instead. */
  @media (max-width: 520px) {
    .bar {
      flex-direction: column;
      align-items: stretch;
    }

    input[type='text'],
    input[inputmode='numeric'],
    select {
      width: 100%;
    }

    .rule-group {
      flex: 1 1 auto;
    }
  }
</style>
