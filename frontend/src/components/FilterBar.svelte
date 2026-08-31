<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Round 30's stream filter (#697, #700/#691): "one filter, two hands".
  // The box (`.fbox`) carries the full typed grammar and is ALWAYS on
  // screen -- clear every term and it says so instead of vanishing, so
  // there is always a way in and no second filter control needs to
  // exist. `bar ▸`/`◂ bar`, welded to the box's own left edge, unfurls
  // round 8's thin strip out of that edge -- the same filter as named
  // fields. Editing either writes the same appState.filters; clicking a
  // value in a row (EventRow's own gesture) writes both. The span pills
  // (15 m/1 h/24 h/14 d, #703) and the "holding N" reach words ride the
  // right end of this same filter line -- moved here from SceneBar's
  // top chrome, not duplicated (see SceneBar.svelte's own comment).
  import { appState } from '../lib/state.svelte'
  import { ACTION_FILTER_OPTIONS } from '../lib/actions'
  import { viewportState } from '../lib/viewport.svelte'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { buildFilterChips, type FilterChip } from '../lib/filterChips'
  import { SPANS, describeReach, reachSeconds, spanAvailable, unavailableReason } from '../lib/spans'

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
  // better!", round 23: "the filter row stays"). The strip defaults
  // folded back into the box and unfurls out of the box's own left edge
  // on demand -- the round-7 always-open fat panel this replaces was
  // rejected verbatim ("no, you ignored my instruction, sliding out to
  // the left, as a thin bar"). A separate flag from drawerOpen: the two
  // breakpoints render different chrome (a bottom-sheet drawer vs. an
  // inline thin row) and must be independently togglable.
  let expanded = $state(false)

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    if (drawerOpen) drawerOpen = false
    if (expanded) expanded = false
  }

  // The box's own chip summary (ported from SceneBar's retired `.search`,
  // #697/#700): the same appState.filters, read as chips, always on
  // screen rather than only while a filter existed -- see the comment on
  // FILTERS_TRIGGER_ENABLED below for what that replaced.
  const filterChips = $derived(buildFilterChips(appState.filters, appState.devices))

  // Removes one chip's own term(s), leaving the rest of the filter
  // untouched -- the mockup's own per-chip ⌫ ("drop this term"), not one
  // combined clear-everything glyph. A compound chip (source/destination)
  // clears every field it summarises together, so the chip and the field
  // group it mirrors always agree on being empty or not.
  function clearChip(chip: FilterChip) {
    switch (chip.key) {
      case 'device':
        appState.setFilter('device', '')
        break
      case 'action':
        appState.setFilter('action', '')
        break
      case 'chain':
        appState.setFilter('chain', '')
        break
      case 'proto':
        appState.setFilter('protocol', '')
        break
      case 'source':
        appState.setFilter('srcQuery', '')
        appState.setFilter('srcScope', '')
        appState.setFilter('srcCountry', '')
        break
      case 'destination':
        appState.setFilter('dstQuery', '')
        appState.setFilter('dstScope', '')
        appState.setFilter('dstCountry', '')
        break
      case 'port':
        appState.setFilter('port', '')
        break
      case 'interface':
        appState.setFilter('interface', '')
        break
      case 'rule':
        appState.setFilter('rule', '')
        break
    }
  }

  // The stream's SPAN control (#703). It sets the same display window
  // the mobile drawer's duration selector sets, so the two can never
  // disagree about what the table is showing; a span reads as active
  // only when the window matches it exactly, so a duration chosen in the
  // drawer leaves every pill quiet rather than lighting the nearest one.
  //
  // Availability comes from the buffer's own reach, never from the
  // configured retention: offering a fortnight over nine hours of buffer
  // would answer with nine hours and call thirteen days quiet.
  const reach = $derived(reachSeconds(appState.stats?.oldestHeld, appState.now))
  const reachWords = $derived(describeReach(reach))

  // The old standalone "Filters ▸" trigger this replaced (#697): round
  // 29's box only ever displayed a filter and offered no way to make
  // one, so the build kept this control mounted beside it as the only
  // way in. Round 30's box is always on screen (see `.filterline`
  // below), so there is always a way in and this second control has
  // nothing left to do -- unmounted, not deleted, per #700/#691.
  const FILTERS_TRIGGER_ENABLED: boolean = false
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
{:else}
  <!-- Round 30's filter line (#697, #s5 `.filterline`): the toggle
       welded to the box's own left edge, the box itself (always on
       screen, never conditional on a filter existing), and the span
       pills at the line's right end. This is the one way in to the
       strip below -- FILTERS_TRIGGER_ENABLED's old second control is
       retired, not duplicated beside this. -->
  <div class="filterline">
    <button
      class="fb-open"
      class:on={expanded}
      onclick={() => (expanded = !expanded)}
      aria-haspopup="true"
      aria-expanded={expanded}
      aria-controls="filterbar-strip"
      title="The same filter, as named fields — slides out of the box's left edge"
    >
      {expanded ? '◂ bar' : 'bar ▸'}
    </button>
    <div class="fbox" class:empty={filterChips.length === 0} role="group" aria-label="The filter, as typed grammar">
      {#if filterChips.length > 0}
        <span class="fchips">
          {#each filterChips as chip (chip.key)}
            <span class="chip"
              >{chip.label}:<em>{chip.value}</em><button
                type="button"
                class="chip-x"
                onclick={() => clearChip(chip)}
                title="drop this term"
                aria-label="Remove the {chip.label} filter"
              >
                ⌫
              </button></span
            >
          {/each}
        </span>
        <span class="fbtype">type a term, or click a value in a row</span>
      {:else}
        <span class="fbtype">no filter — every line, as it arrived. type a term, or click a value in a row</span>
      {/if}
    </div>
    <span class="spans" role="group" aria-label="How far back the stream shows — {reachWords}">
      {#each SPANS as span (span.key)}
        {@const available = spanAvailable(span, reach)}
        <button
          type="button"
          class="span"
          class:on={retentionState.maxAgeSeconds === span.seconds}
          disabled={!available}
          aria-pressed={retentionState.maxAgeSeconds === span.seconds}
          title={available ? `Show the last ${span.label}` : unavailableReason(span, reach)}
          onclick={() => retentionState.set(span.seconds)}>{span.label}</button
        >
      {/each}
      <!-- What the buffer really holds, beside the control it qualifies.
           Not a description of the interface (round 30 struck those) but
           the same fact the unavailable spans turn on, said once in
           words instead of only on hover. -->
      <span class="reach">{reachWords}</span>
    </span>
  </div>

  {#if FILTERS_TRIGGER_ENABLED && !expanded}
    <!-- The pre-round-30 folded box (#644): a quiet trigger standing in
         for the whole bar. Retired by #697 -- the box above is always on
         screen now, so this second way in is unmounted (see
         FILTERS_TRIGGER_ENABLED's own comment), never deleted. -->
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
{/if}

{#if (viewportState.isMobile && drawerOpen) || (!viewportState.isMobile && expanded)}
  {#if viewportState.isMobile}
    <div class="scrim" onclick={() => (drawerOpen = false)} role="presentation"></div>
  {/if}
  <div
    class="bar"
    id="filterbar-strip"
    class:drawer={viewportState.isMobile}
    class:thin={!viewportState.isMobile}
  >
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
      <button class="tf-clear" onclick={() => appState.resetFilters()} aria-label="Clear all filters" title="Clear every term">× clear</button>
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
      <button class="tf-fold" onclick={() => (expanded = false)} aria-label="Fold filters back into the box" title="Fold filters back into the box">fold ▸</button>
    {/if}
  </div>
{/if}

<style>
  /* Deck.svelte mounts the stream's own card-body as `<Whisper />
     <FilterBar /> <LiveTable />`, in that document order (untouched
     here -- Deck.svelte is out of scope for this fidelity pass). Round
     30's own order is filter line -> bar -> whisper -> table (#697, "the
     top is a flow column"), so every top-level element this component
     renders sits ahead of Whisper's own root in the shared
     `.card-body` flex column via `order` -- Whisper (unordered, default
     0) then keeps its place ahead of LiveTable (also default 0) purely
     from DOM order, so nothing there needs to change either. */
  .filterline,
  .fold-trigger,
  .bar,
  .mobile-row {
    order: -1;
  }

  /* Round 30's filter line (#697, #s5 `.filterline`): the toggle, the
     always-on box, and the span pills, left-to-right on one row. */
  .filterline {
    display: flex;
    gap: 18px;
    align-items: center;
  }

  /* Welded to the box's own left edge, so the strip visibly slides out
     of the box rather than appearing beside it. */
  .fb-open {
    flex: none;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.06em;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-right: 0;
    border-radius: 7px 0 0 7px;
    padding: 6px 11px;
    cursor: pointer;
    white-space: nowrap;
  }

  .fb-open:hover {
    border-color: var(--fg-muted);
    color: var(--fg);
  }

  .fb-open.on {
    color: var(--fg);
    background: var(--bg-elevated);
  }

  .fbox {
    flex: 1;
    display: flex;
    gap: 12px;
    align-items: center;
    flex-wrap: wrap;
    min-height: 28px;
    padding: 4px 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 0 7px 7px 0;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
  }

  .fchips {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
  }

  .chip {
    display: inline-flex;
    gap: 5px;
    align-items: baseline;
    white-space: nowrap;
  }

  .chip em {
    font-style: normal;
    color: var(--accent);
  }

  .chip-x {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 12px;
    cursor: pointer;
    padding: 0;
  }

  .chip-x:hover {
    color: var(--alarm);
  }

  /* The empty box still says what it is (#697) -- round 29's box only
     rendered once a filter existed, which is exactly why the build kept
     a second "Filters ▸" control alive beside it. */
  .fbtype {
    color: var(--fg-dim);
  }

  /* Round 30's `.spans`: quiet pills, the chosen one in full ink. A span
     the buffer cannot cover is dimmed and unclickable rather than
     hidden -- the operator should see that a fortnight exists and is
     not held, not wonder where it went. */
  .spans {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 10px;
  }

  .span {
    background: transparent;
    border: none;
    padding: 0;
    font: 500 11px var(--font-sans);
    color: var(--fg-dim);
    cursor: pointer;
  }

  .span:hover:not(:disabled) {
    color: var(--fg);
  }

  .span.on {
    color: var(--fg);
  }

  .span:disabled {
    color: var(--fg-dim);
    opacity: 0.4;
    cursor: not-allowed;
  }

  .reach {
    font: 400 10.5px var(--font-sans);
    color: var(--fg-dim);
    margin-left: 2px;
  }

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
