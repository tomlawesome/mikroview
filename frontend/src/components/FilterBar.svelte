<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from '../lib/state.svelte'
  import { ACTION_FILTER_OPTIONS } from '../lib/actions'
  import FilterPresetsMenu from './FilterPresetsMenu.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import { downloadEventsCsv } from '../lib/export'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'

  // Phone-width only, mirroring what the retired hamburger did (#544):
  // Toolbar.svelte:64 already carries this control at desktop width, so
  // duplicating it here unconditionally would put two of them on screen.
  function onMaxAgeChange(e: Event) {
    const raw = (e.currentTarget as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }

  const actions = ACTION_FILTER_OPTIONS

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

    <!-- #438: existed as a filter field (EventRow's chain cell already
         called setFilter('chain', …)) with no control here to show, edit
         or clear it -- the issue's own worked example of the bidirectional
         contract being one-way. A select, not free text: the built-in
         chains plus anything else observed appear in appState.chainOptions,
         so a custom chain can't be typo'd and shows up the moment it's seen. -->
    <select bind:value={appState.filters.chain} aria-label="Chain">
      <option value="">Any chain</option>
      {#each appState.chainOptions as c (c)}
        <option value={c}>{c}</option>
      {/each}
    </select>

    <input
      type="text"
      placeholder="Protocol (tcp, udp, icmp…)"
      bind:value={appState.filters.protocol}
      aria-label="Protocol"
    />

    <!-- #438: the single "IP or CIDR" box (matched src OR dst, raw address
         only) is replaced by side-scoped Source/Destination groups, each
         pairing its query box with the existing scope select and a new
         country select -- matching, label or IP or CIDR, lives in
         lib/addressMatch.ts. The swap button between them answers "clicked
         the wrong side" in two clicks instead of retyping. -->
    <div class="addr-group">
      <select bind:value={appState.filters.srcScope} aria-label="Source scope" title="Restrict by whether the source is on your LAN">
        <option value="">Any source</option>
        <option value="internal">Internal source</option>
        <option value="external">External source</option>
      </select>
      <input
        type="text"
        placeholder="Source — name, IP or CIDR"
        bind:value={appState.filters.srcQuery}
        aria-label="Source — name, IP or CIDR"
      />
      <select bind:value={appState.filters.srcCountry} aria-label="Source country">
        <option value="">Any country</option>
        {#each appState.srcCountryOptions as opt (opt.value)}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    </div>

    <button
      class="swap"
      onclick={() => appState.swapSourceDestination()}
      aria-label="Swap source and destination filters"
      title="Swap source and destination filters"
    >
      ⇄
    </button>

    <div class="addr-group">
      <select bind:value={appState.filters.dstScope} aria-label="Destination scope" title="Restrict by whether the destination is on your LAN">
        <option value="">Any destination</option>
        <option value="internal">Internal destination</option>
        <option value="external">External destination</option>
      </select>
      <input
        type="text"
        placeholder="Destination — name, IP or CIDR"
        bind:value={appState.filters.dstQuery}
        aria-label="Destination — name, IP or CIDR"
      />
      <select bind:value={appState.filters.dstCountry} aria-label="Destination country">
        <option value="">Any country</option>
        {#each appState.dstCountryOptions as opt (opt.value)}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    </div>

    <!-- #438: text now, not numeric-only -- a bare integer is still an
         exact port match on either side, but anything else searches the
         displayed label (an operator name, or a well-known service name
         from lib/commonPorts.ts) via lib/portMatch.ts. -->
    <input
      type="text"
      placeholder="Port — number or service"
      bind:value={appState.filters.port}
      aria-label="Port — number or service"
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
        title={appState.ruleMatchStatus === 'too-slow'
          ? 'That pattern took too long to evaluate and was stopped, so the rule filter is inactive. Try a simpler one.'
          : appState.ruleMatchStatus === 'invalid'
            ? 'That is not a valid regular expression, so the rule filter is inactive.'
            : 'Treat the rule search above as a regular expression (matches rule label or raw log line)'}
        class:refused={appState.ruleMatchStatus === 'too-slow' || appState.ruleMatchStatus === 'invalid'}
        aria-pressed={appState.filters.ruleRegex}
      >
        .*
      </button>
    </div>

    {#if appState.hasActiveFilters && !viewportState.isMobile}
      <button class="clear" onclick={() => appState.resetFilters()}>Clear filters</button>
    {/if}

    <!-- Also moved off the retired hamburger (#544). Phone-width only:
         the desktop control is Toolbar's. -->
    {#if viewportState.isMobile}
      <label class="duration">
        Display duration
        <select
          value={retentionState.maxAgeSeconds === null ? 'null' : String(retentionState.maxAgeSeconds)}
          onchange={onMaxAgeChange}
          aria-label="Display duration"
        >
          {#each MAX_AGE_OPTIONS as opt (opt.value)}
            <option value={opt.value === null ? 'null' : String(opt.value)}>{opt.label}</option>
          {/each}
        </select>
      </label>
    {/if}

    <!-- Moved here from the retired hamburger menu (#544). It acts on the
         events this bar has filtered, so it belongs to the live view
         rather than to the chrome. Deliberately one entry, not one per
         format: #94 defers additional formats, and when they land this
         becomes a submenu rather than a flat item each. -->
    <button
      class="export"
      onclick={() => downloadEventsCsv(appState.filteredEvents)}
      disabled={appState.filteredEvents.length === 0}
      title="Export the currently shown/filtered events to a CSV file"
    >
      Export to CSV
    </button>
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

  .drawer input[type='text'],
  .drawer select {
    width: 100%;
  }

  /* #438: keeps a Source or Destination group's scope/query/country
     controls together while the bar's own flex-wrap moves whole groups
     around, rather than letting the three drift apart mid-wrap. */
  .addr-group {
    display: flex;
    gap: 8px;
  }

  .drawer .addr-group {
    flex-direction: column;
  }

  .addr-group select {
    flex: none;
    width: auto;
  }

  .drawer .addr-group select {
    width: 100%;
  }

  .swap {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 16px;
    line-height: 1;
    flex: none;
    align-self: center;
  }

  .swap:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .drawer .swap {
    align-self: flex-start;
    min-height: 44px;
  }

  /* A pattern that was invalid, or refused for overrunning its time
     budget (see lib/ruleMatcher.ts). The filter is inactive rather than
     silently matching nothing, so say so. */
  .regex-toggle.refused {
    border-color: var(--danger, #c0392b);
    color: var(--danger, #c0392b);
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

  .export {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 14px;
    font-size: 14px;
  }

  .export:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .export:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .duration {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 14px;
    min-height: 44px;
  }

  /* Below this width the fixed input widths (145px/200px) leave several
     fields too narrow to comfortably type into once wrapped one per line
     -- let them fill the row instead. Only reachable today via a narrow
     desktop window (viewportState.isMobile's drawer already covers real
     phone widths, and already applies this same full-width treatment
     unconditionally). */
  @media (max-width: 520px) {
    .bar:not(.drawer) {
      flex-direction: column;
      align-items: stretch;
    }

    .bar:not(.drawer) input[type='text'],
    .bar:not(.drawer) select {
      width: 100%;
    }

    .bar:not(.drawer) .addr-group {
      flex-direction: column;
    }

    .bar:not(.drawer) .addr-group select {
      width: 100%;
    }

    .bar:not(.drawer) .swap {
      align-self: flex-start;
    }

    .rule-group {
      flex: 1 1 auto;
    }
  }
</style>
