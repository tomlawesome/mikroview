<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import type { Action } from '../lib/types'
  import FilterPresetsMenu from './FilterPresetsMenu.svelte'
  import { viewportState } from '../lib/viewport.svelte'

  const actions: { value: Action | ''; label: string }[] = [
    { value: '', label: 'Any action' },
    { value: 'accept', label: 'Accept' },
    { value: 'drop', label: 'Drop' },
    { value: 'reject', label: 'Reject' },
    { value: 'log', label: 'Log' },
    { value: 'unknown', label: 'Unknown' },
  ]

  // Below the breakpoint, the ~9 fields below move into a slide-up
  // drawer behind a trigger (issue #85) rather than staying always-
  // visible -- a horizontally-wrapping strip of selects/inputs doesn't
  // fit a phone-width screen usefully even stacked one-per-line, and a
  // human scanning the live view rarely needs every filter visible at
  // once the way FilterPresetsMenu's saved presets do.
  let drawerOpen = $state(false)

  function onKeydown(e: KeyboardEvent) {
    if (drawerOpen && e.key === 'Escape') drawerOpen = false
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if viewportState.isMobile}
  <div class="mobile-row">
    <button
      class="trigger"
      onclick={() => (drawerOpen = true)}
      aria-haspopup="true"
      aria-expanded={drawerOpen}
    >
      Filters
      {#if appState.hasActiveFilters}<span class="dot" aria-label="Filters active"></span>{/if}
    </button>
    {#if appState.hasActiveFilters}
      <button class="clear" onclick={() => appState.resetFilters()}>Clear filters</button>
    {/if}
  </div>
{/if}

{#if !viewportState.isMobile || drawerOpen}
  {#if viewportState.isMobile}
    <div class="scrim" onclick={() => (drawerOpen = false)} role="presentation"></div>
  {/if}
  <div class="bar" class:drawer={viewportState.isMobile}>
    {#if viewportState.isMobile}
      <div class="handle"></div>
      <div class="drawer-header">
        <span class="drawer-title">Filters</span>
        <button class="done" onclick={() => (drawerOpen = false)}>Done</button>
      </div>
    {/if}

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

    <select bind:value={appState.filters.srcScope} aria-label="Source scope" title="Restrict by whether the source is on your LAN">
      <option value="">Any source</option>
      <option value="internal">Internal source</option>
      <option value="external">External source</option>
    </select>

    <select bind:value={appState.filters.dstScope} aria-label="Destination scope" title="Restrict by whether the destination is on your LAN">
      <option value="">Any destination</option>
      <option value="internal">Internal destination</option>
      <option value="external">External destination</option>
    </select>

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

    {#if appState.hasActiveFilters && !viewportState.isMobile}
      <button class="clear" onclick={() => appState.resetFilters()}>Clear filters</button>
    {/if}
  </div>
{/if}

<style>
  .mobile-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 9px 14px;
    font-size: 14px;
    /* 44px minimum touch target (issue #85). */
    min-height: 44px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
  }

  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 30;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 14px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .bar.drawer {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 31;
    flex-direction: column;
    max-height: 80vh;
    overflow-y: auto;
    border-radius: 16px 16px 0 0;
    border-bottom: none;
    padding: 10px 18px calc(18px + env(safe-area-inset-bottom));
    box-shadow: 0 -20px 50px rgba(0, 0, 0, 0.4);
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 10px;
    flex: none;
  }

  .drawer-header {
    display: flex;
    align-items: center;
    margin-bottom: 6px;
  }

  .drawer-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--fg);
  }

  .done {
    margin-left: auto;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    min-height: 44px;
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
    /* 44px minimum touch target (issue #85) -- desktop's tighter 8px
       vertical padding above is comfortable with a mouse but too
       cramped to reliably tap; the drawer-only override below restores
       enough height without changing the always-visible desktop bar. */
  }

  .drawer input,
  .drawer select,
  .drawer .regex-toggle {
    min-height: 44px;
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

  .drawer input[type='text'],
  .drawer input[inputmode='numeric'],
  .drawer select {
    width: 100%;
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

  .mobile-row .clear {
    min-height: 44px;
  }

  /* Below this width the fixed input widths (145px/200px/80px) leave
     several fields too narrow to comfortably type into once wrapped one
     per line -- let them fill the row instead. Only reachable today via
     a narrow desktop window (viewportState.isMobile's drawer already
     covers real phone widths, and already applies this same full-width
     treatment unconditionally). */
  @media (max-width: 520px) {
    .bar:not(.drawer) {
      flex-direction: column;
      align-items: stretch;
    }

    .bar:not(.drawer) input[type='text'],
    .bar:not(.drawer) input[inputmode='numeric'],
    .bar:not(.drawer) select {
      width: 100%;
    }

    .rule-group {
      flex: 1 1 auto;
    }
  }
</style>
