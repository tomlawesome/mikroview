<script lang="ts">
  // Collapsed by default (just a "+" trigger card, same footprint as the
  // other dashboard panels); expands into a small filter form -- a
  // purpose-built subset of FilterBar's fields bound to local draft state
  // rather than appState.filters, since a widget's filter is independent
  // of whatever's active in the live view.
  import { appState } from '../lib/state.svelte'
  import { emptyFilters, type Action, type Filters } from '../lib/types'
  import { GROUP_BY_FIELDS, GROUP_BY_LABELS, type GroupByField } from '../lib/groupBy'
  import { topTalkerWidgetsState } from '../lib/topTalkers.svelte'

  const actions: { value: Action | ''; label: string }[] = [
    { value: '', label: 'Any action' },
    { value: 'accept', label: 'Accept' },
    { value: 'drop', label: 'Drop' },
    { value: 'reject', label: 'Reject' },
    { value: 'log', label: 'Log' },
    { value: 'unknown', label: 'Unknown' },
  ]

  let expanded = $state(false)
  let title = $state('')
  let groupBy = $state<GroupByField>('srcIp')
  let filters = $state<Filters>(emptyFilters())

  function reset() {
    title = ''
    groupBy = 'srcIp'
    filters = emptyFilters()
    expanded = false
  }

  function save() {
    if (!title.trim()) return
    topTalkerWidgetsState.add(title, groupBy, filters)
    reset()
  }
</script>

{#if !expanded}
  <button class="trigger" onclick={() => (expanded = true)}>
    + Add custom top talkers
  </button>
{:else}
  <div class="form">
    <div class="header">Add custom top talkers</div>

    <input
      type="text"
      placeholder="Title (e.g. SSH attempts)"
      bind:value={title}
      aria-label="Widget title"
    />

    <label class="field">
      <span>Group by</span>
      <select bind:value={groupBy} aria-label="Group by">
        {#each GROUP_BY_FIELDS as f (f)}
          <option value={f}>{GROUP_BY_LABELS[f]}</option>
        {/each}
      </select>
    </label>

    <div class="filters">
      <select bind:value={filters.device} aria-label="Device">
        <option value="">Any device</option>
        {#each appState.devices as d (d.id)}
          <option value={d.id}>{d.name}</option>
        {/each}
      </select>

      <select bind:value={filters.action} aria-label="Action">
        {#each actions as a (a.value)}
          <option value={a.value}>{a.label}</option>
        {/each}
      </select>

      <input type="text" placeholder="Protocol" bind:value={filters.protocol} aria-label="Protocol" />
      <input type="text" placeholder="IP or CIDR" bind:value={filters.ip} aria-label="IP address or CIDR" />
      <input type="text" inputmode="numeric" placeholder="Port" bind:value={filters.port} aria-label="Port" />
      <input type="text" placeholder="Interface" bind:value={filters.interface} aria-label="Interface" />

      <select bind:value={filters.srcScope} aria-label="Source scope">
        <option value="">Any source</option>
        <option value="internal">Internal source</option>
        <option value="external">External source</option>
      </select>

      <select bind:value={filters.dstScope} aria-label="Destination scope">
        <option value="">Any destination</option>
        <option value="internal">Internal destination</option>
        <option value="external">External destination</option>
      </select>

      <div class="rule-group">
        <input
          type="text"
          placeholder={filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…'}
          bind:value={filters.rule}
          aria-label="Rule filter"
        />
        <button
          class="regex-toggle"
          class:active={filters.ruleRegex}
          onclick={() => (filters.ruleRegex = !filters.ruleRegex)}
          title="Treat the rule filter as a regular expression"
          aria-pressed={filters.ruleRegex}
        >
          .*
        </button>
      </div>
    </div>

    <div class="actions">
      <button class="cancel" onclick={reset}>Cancel</button>
      <button class="save" onclick={save} disabled={!title.trim()}>Save</button>
    </div>
  </div>
{/if}

<style>
  .trigger {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 80px;
    background: transparent;
    border: 1px dashed var(--border);
    border-radius: 8px;
    color: var(--fg-muted);
    font-size: 13px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
  }

  .header {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  input,
  select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 9px;
    font-size: 13px;
    width: 100%;
  }

  input::placeholder {
    color: var(--fg-dim);
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .field select {
    flex: 1;
  }

  .filters {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }

  .rule-group {
    grid-column: 1 / -1;
    display: flex;
    gap: 4px;
  }

  .rule-group input {
    flex: 1;
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

  .regex-toggle.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save {
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:hover {
    opacity: 0.9;
  }

  .save:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
