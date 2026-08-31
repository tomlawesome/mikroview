<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from '../lib/state.svelte'
  import { ACTION_FILTER_OPTIONS } from '../lib/actions'
  import { viewportState } from '../lib/viewport.svelte'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'

  // Presets and Export to CSV are later additions round 29's ratified
  // filter row does not draw (#683 correction, 2026-08-31: "anything the
  // ratified scene does not draw at all ... does not get a home
  // invented for it"). Their own code and tests are untouched --
  // FilterPresetsMenu.svelte and lib/export.ts still work, just aren't
  // mounted here; see the issue's gap list for where they go next.

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

  // Desktop's own fold state (#644, round 8 "accepted -- yeah much
  // better!", round 23: "the filter row stays"). The box defaults
  // folded and slides out into the thin bar on demand -- the round-7
  // always-open fat panel this replaces was rejected verbatim ("no, you
  // ignored my instruction, sliding out to the left, as a thin bar").
  // A separate flag from drawerOpen: the two breakpoints render
  // different chrome (a bottom-sheet drawer vs. an inline thin row) and
  // must be independently togglable.
  let expanded = $state(false)

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    if (drawerOpen) drawerOpen = false
    if (expanded) expanded = false
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
{:else if !expanded}
  <!-- The folded box (#644): a quiet trigger standing in for the whole
       bar, matching the mobile trigger's own "dot means something's
       active" convention rather than inventing a second one. -->
  <button
    class="fold-trigger"
    onclick={() => (expanded = true)}
    aria-haspopup="true"
    aria-expanded={expanded}
  >
    Filters ▸
    {#if appState.hasActiveFilters}<span class="dot" aria-label="Filters active"></span>{/if}
  </button>
{/if}

{#if (viewportState.isMobile && drawerOpen) || (!viewportState.isMobile && expanded)}
  {#if viewportState.isMobile}
    <div class="scrim" onclick={() => (drawerOpen = false)} role="presentation"></div>
  {/if}
  <div class="bar" class:drawer={viewportState.isMobile} class:thin={!viewportState.isMobile}>
    {#if viewportState.isMobile}
      <div class="handle"></div>
      <div class="drawer-header">
        <span class="drawer-title">Filters</span>
        <button class="done" onclick={() => (drawerOpen = false)}>Done</button>
      </div>
    {/if}

    <!-- fb-label spans below are visible on the desktop thin bar only
         (.thin .fb-label; hidden in the mobile drawer, which already
         names each field via its placeholder/aria-label -- see round 8's
         "dim micro-labels over hairline-underlined values, no boxes").
         The desktop thin row additionally drops the "Any X"/placeholder
         prose the mobile drawer still shows (#683, round 29: "no
         placeholder prose inside the fields") -- the fb-label above
         already names the field there, so the empty state is just the
         hairline with nothing on it. -->
    <div class="fb-field">
      <span class="fb-label">Device</span>
      <select bind:value={appState.filters.device} aria-label="Device">
        <option value="">{viewportState.isMobile ? 'Any device' : '—'}</option>
        {#each appState.devices as d (d.id)}
          <option value={d.id}>{d.name}</option>
        {/each}
      </select>
    </div>

    <div class="fb-field">
      <span class="fb-label">Action</span>
      <select bind:value={appState.filters.action} aria-label="Action">
        {#each actions as a (a.value)}
          <option value={a.value}>{viewportState.isMobile || a.value !== '' ? a.label : '—'}</option>
        {/each}
      </select>
    </div>

    <!-- #438: existed as a filter field (EventRow's chain cell already
         called setFilter('chain', …)) with no control here to show, edit
         or clear it -- the issue's own worked example of the bidirectional
         contract being one-way. A select, not free text: the built-in
         chains plus anything else observed appear in appState.chainOptions,
         so a custom chain can't be typo'd and shows up the moment it's seen. -->
    <div class="fb-field">
      <span class="fb-label">Chain</span>
      <select bind:value={appState.filters.chain} aria-label="Chain">
        <option value="">{viewportState.isMobile ? 'Any chain' : '—'}</option>
        {#each appState.chainOptions as c (c)}
          <option value={c}>{c}</option>
        {/each}
      </select>
    </div>

    <div class="fb-field">
      <span class="fb-label">Proto</span>
      <input
        type="text"
        placeholder={viewportState.isMobile ? 'Protocol (tcp, udp, icmp…)' : ''}
        bind:value={appState.filters.protocol}
        aria-label="Protocol"
      />
    </div>

    <!-- #438: the single "IP or CIDR" box (matched src OR dst, raw address
         only) is replaced by side-scoped Source/Destination groups, each
         pairing its query box with the existing scope select and a new
         country select -- matching, label or IP or CIDR, lives in
         lib/addressMatch.ts. The swap button between them answers "clicked
         the wrong side" in two clicks instead of retyping. Ratified as
         "source ⇄ destination (scope + country)" -- the free-text query
         stays too (#683: "do not delete working features"), just folded
         into the same compact group rather than dropped. -->
    <div class="fb-field">
      <span class="fb-label">Source</span>
      <div class="addr-group">
        <select bind:value={appState.filters.srcScope} aria-label="Source scope" title="Restrict by whether the source is on your LAN">
          <option value="">{viewportState.isMobile ? 'Any source' : '—'}</option>
          <option value="internal">Internal</option>
          <option value="external">External</option>
        </select>
        <input
          type="text"
          placeholder={viewportState.isMobile ? 'Source — name, IP or CIDR' : ''}
          bind:value={appState.filters.srcQuery}
          aria-label="Source — name, IP or CIDR"
        />
        <select bind:value={appState.filters.srcCountry} aria-label="Source country">
          <option value="">{viewportState.isMobile ? 'Any country' : '—'}</option>
          {#each appState.srcCountryOptions as opt (opt.value)}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </div>
    </div>

    <button
      class="swap"
      onclick={() => appState.swapSourceDestination()}
      aria-label="Swap source and destination filters"
      title="Swap source and destination filters"
    >
      ⇄
    </button>

    <div class="fb-field">
      <span class="fb-label">Destination</span>
      <div class="addr-group">
        <select bind:value={appState.filters.dstScope} aria-label="Destination scope" title="Restrict by whether the destination is on your LAN">
          <option value="">{viewportState.isMobile ? 'Any destination' : '—'}</option>
          <option value="internal">Internal</option>
          <option value="external">External</option>
        </select>
        <input
          type="text"
          placeholder={viewportState.isMobile ? 'Destination — name, IP or CIDR' : ''}
          bind:value={appState.filters.dstQuery}
          aria-label="Destination — name, IP or CIDR"
        />
        <select bind:value={appState.filters.dstCountry} aria-label="Destination country">
          <option value="">{viewportState.isMobile ? 'Any country' : '—'}</option>
          {#each appState.dstCountryOptions as opt (opt.value)}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </div>
    </div>

    <!-- #438: text now, not numeric-only -- a bare integer is still an
         exact port match on either side, but anything else searches the
         displayed label (an operator name, or a well-known service name
         from lib/commonPorts.ts) via lib/portMatch.ts. -->
    <div class="fb-field">
      <span class="fb-label">Port</span>
      <input
        type="text"
        placeholder={viewportState.isMobile ? 'Port — number or service' : ''}
        bind:value={appState.filters.port}
        aria-label="Port — number or service"
      />
    </div>

    <div class="fb-field">
      <span class="fb-label">Interface</span>
      <input
        type="text"
        placeholder={viewportState.isMobile ? 'Interface' : ''}
        bind:value={appState.filters.interface}
        aria-label="Interface"
      />
    </div>

    <div class="fb-field">
      <span class="fb-label">Rule</span>
      <div class="rule-group">
        <input
          type="text"
          placeholder={viewportState.isMobile ? (appState.filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…') : ''}
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
    </div>

    {#if appState.hasActiveFilters && !viewportState.isMobile}
      <button class="tf-clear" onclick={() => appState.resetFilters()} aria-label="Clear all filters" title="Clear all filters">×</button>
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

    {#if !viewportState.isMobile}
      <!-- Fold slides the bar back into the box (#644, round 8) -- the
           typed grammar/click model stays appState.filters either way,
           so nothing here is lost by folding, only hidden. -->
      <button class="tf-fold" onclick={() => (expanded = false)} aria-label="Fold filters back into the box" title="Fold filters back into the box">▸</button>
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

  /* The folded box (#644, round 8): a quieter pill than .trigger's --
     this is chrome sitting over the live table on every desktop visit,
     not a modal's one-shot entry point. */
  .fold-trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    border-radius: 999px;
    padding: 4px 14px;
    font-size: 12px;
    font-family: var(--font-mono);
    cursor: pointer;
  }

  .fold-trigger:hover {
    color: var(--fg-muted);
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

  /* The thin bar (#644, round 8's correction of round 7's rejected fat
     panel: "the box slides out to the left as a thin bar... reminiscent
     of the old live view"). Overrides the boxed/elevated look .bar
     carries for .drawer above -- one quiet row, no border box, dim
     micro-labels over hairline-underlined values. */
  .bar.thin {
    background: color-mix(in srgb, var(--bg) 55%, transparent);
    backdrop-filter: blur(6px);
    border: none;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    padding: 6px 4px 8px;
    gap: 6px;
    align-items: flex-end;
    animation: unfurl 0.35s ease-out;
    transform-origin: right center;
  }

  @keyframes unfurl {
    from {
      transform: scaleX(0.05);
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .bar.thin {
      animation: none;
    }
  }

  .fb-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .fb-label {
    display: none;
    font: 500 8px var(--font-mono);
    letter-spacing: 0.14em;
    color: var(--fg-dim);
    text-transform: uppercase;
  }

  /* Visible on the thin bar only -- the mobile drawer already names each
     field via its placeholder/aria-label, and showing this too would be
     a mobile visual change nothing here asked for. */
  .thin .fb-label {
    display: block;
  }

  .thin input,
  .thin select {
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--hair-2);
    border-radius: 0;
    padding: 1px 2px 3px;
    font: 12px var(--font-mono);
    /* Compact by design (#683, round 29's "one quiet row ... fits on one
       line at 1600px wide") -- a filled field shows its own short value
       (a device name, "drop", "DE"), not prose, so a value that's still
       too long to fit is clipped rather than allowed to reflow the row. */
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .thin select {
    width: 72px;
  }

  .thin input[type='text'] {
    width: 80px;
  }

  .thin .addr-group select {
    width: 58px;
  }

  .thin .rule {
    width: 130px;
  }

  .thin input:focus,
  .thin select:focus {
    border-bottom-color: var(--accent);
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

  .thin .addr-group {
    gap: 4px;
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

  /* Matches round 8's own .fb-swap: a plain glyph, no button chrome. */
  .thin .swap {
    background: none;
    border: none;
    padding: 0 2px;
    font-size: 14px;
    align-self: flex-end;
  }

  .thin .swap:hover {
    color: var(--fg-muted);
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

  /* Matches .thin .swap's own rule above: a plain glyph riding the
     hairline row, no button-box chrome. */
  .thin .regex-toggle {
    background: none;
    border: none;
    border-bottom: 1px solid var(--hair-2);
    border-radius: 0;
    padding: 1px 2px 3px;
    font-size: 11px;
    width: auto;
  }

  .thin .regex-toggle.active {
    background: none;
    border-bottom-color: var(--accent);
  }

  .thin .regex-toggle.refused {
    border-bottom-color: var(--danger, #c0392b);
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

  /* The thin bar's own clear/fold -- round 8's "× clear" and "fold ▸",
     plain text rather than .clear's bordered button to stay quiet. */
  .tf-clear,
  .tf-fold {
    align-self: center;
    background: none;
    border: none;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }

  .tf-clear {
    color: var(--fg-dim);
    margin-left: auto;
  }

  .tf-clear:hover {
    color: var(--alarm);
  }

  .tf-fold {
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: 10.5px;
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
