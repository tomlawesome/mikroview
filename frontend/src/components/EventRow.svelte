<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import type { FirewallEvent } from '../lib/types'
  import { formatAddr, formatTimeMs, rawTooltip } from '../lib/format'
  import { appState } from '../lib/state.svelte'
  import ActionBadge from './ActionBadge.svelte'
  import RouterRuleButton from './RouterRuleButton.svelte'
  import CopyButton from './CopyButton.svelte'
  import EditNameButton from './EditNameButton.svelte'
  // Asked per pencil below rather than left to EditNameButton's own
  // guard: a viewer's row would otherwise still build components that
  // render nothing, and the live view renders up to
  // MAX_RENDERED_ROWS of these rows.
  import { nameEditorState } from '../lib/nameEditor.svelte'

  let {
    event,
    // The resolved friendly name for event.deviceId (falls back to the
    // id itself, same convention as EventCardMobile/EventDetailSheet's
    // own deviceName prop) -- #717 restores Device as a column, so every
    // row needs it, not just the mobile card and the sheet.
    deviceName = '',
    // Grouping (#341). count > 1 means this row stands for several
    // identical connections and the time cell shows the count instead --
    // the time is the same second on every row at any real rate, so it
    // is the least useful thing in the most prominent column.
    count = 1,
    // flagged means "this row's source has an active flag against it",
    // not "this event caused that flag": a flag records what it was
    // raised about, not which events evidenced it (#341). Drives the
    // full-row wash (the-whole.html's tr.hl) *and* a ⚑ after the time
    // (its .rmk): round 30 draws both, and the mark annotates the wash
    // rather than replacing it. Round 29 drew no mark, which is why #685
    // took one out; that ruling is superseded (#691's round-30 audit).
    // The mark rides after the time, never before it -- ahead of the
    // figures it pushes the first digit right and breaks the left edge
    // the tabular numerals line up on.
    flagged = false,
    expandable = false,
    expanded = false,
    onToggle,
    // member rows are the individual events shown under an opened group.
    member = false,
    // dimmed (#644's whisper fence): true when this row's receipt time
    // falls outside a drawn fence range. Visual only, same as the-whole.
    // html's own .outside{opacity} treatment -- the row is still here,
    // still filterable, just quiet.
    dimmed = false,
    // #644's squared columns retired the per-action row washes; alternate
    // rows carry a faint band instead, decided by the caller from display
    // order (a CSS nth-child can't see it: rows share their grid with
    // header cells and drawer notes, and grouping shifts the parity).
    banded = false,
    // Opens the row's detail surface (EventDetailSheet) -- the raw line,
    // NAT lookup and MAC lookup live there, and every row must be able
    // to reach it, not just the mobile cards that always could. Device,
    // chain, interfaces, src port, NAT and MAC are no longer exclusive
    // to the sheet (#717 restores them as columns too, see
    // columns.svelte.ts), but the sheet stays the one place for the
    // rest of a row's detail.
    onOpen,
  }: {
    event: FirewallEvent
    deviceName?: string
    count?: number
    flagged?: boolean
    expandable?: boolean
    expanded?: boolean
    onToggle?: () => void
    member?: boolean
    dimmed?: boolean
    banded?: boolean
    onOpen?: () => void
  } = $props()

  // Which Filters field (if either) a NAT token's translated address
  // belongs to (#438's NAT-parity section). Only the two dedicated NAT
  // chains say which side was rewritten -- see
  // internal/routeros/parser.go's isNATChain, mirrored exactly here and
  // in lib/state.svelte.ts's srcCandidates/dstCandidates. The NAT column
  // itself is gone (#644), but the rule cell's lookup trigger still has
  // to resolve against the same table the translation came from -- a NAT
  // event's log-prefix names a rule in the NAT table, not the filter
  // table (#445).
  const natFilterKey = $derived(
    event.chain?.toLowerCase() === 'srcnat'
      ? 'srcQuery'
      : event.chain?.toLowerCase() === 'dstnat'
        ? 'dstQuery'
        : null,
  )

  // #439: row tokens (action, addresses, port, protocol, rule) used to be
  // <button> elements. That's *why* row text couldn't be selected/copied
  // before this -- every browser's UA stylesheet makes button content
  // unselectable, independent of anything app.css did or didn't set
  // (there was no explicit `user-select: none` anywhere to remove).
  // They're plain elements with role="button" now, so their text is
  // ordinary selectable content, and click-to-filter is wired up by hand
  // below instead of coming for free from <button onclick>.
  let rowEl: HTMLDivElement | undefined = $state()

  // Tracks which element a mouse press started on, so a release is only
  // treated as "activating this token" if it started there too --
  // mirrors a native <button>'s own click semantics (mousedown+mouseup
  // on the same element), which stopped being automatic once these
  // cells stopped being <button>s. A plain closure variable rather than
  // $state: nothing reads it reactively, only the next mouseup for the
  // same press, so there's nothing for Svelte to track.
  let pressedTarget: EventTarget | null = null

  // True if the current selection has any content inside this row. Used
  // to tell "clicked" from "just finished dragging to select text"
  // apart at mouseup -- the one moment both look identical (a mousedown
  // and mouseup on the same token) but mean opposite things. Deliberately
  // checked at mouseup, not mousedown: Chrome does not collapse a
  // pre-existing selection until mouseup when the press started inside
  // it (so the selection can still be dragged), so mousedown is too
  // early to see it.
  function selectionWithinRow(): boolean {
    if (!rowEl) return false
    const sel = window.getSelection()
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) return false
    return rowEl.contains(sel.getRangeAt(0).commonAncestorContainer)
  }

  // Wires one token's three activation paths -- mouse press+release on
  // the same element with no selection left behind, or Enter/Space -- to
  // `run`. Every click-to-filter cell below uses this instead of each
  // separately re-deriving the same three handlers.
  //
  // A Svelte action (use:activate) rather than a spread of handler
  // props, and that is a licensing decision as much as a style one: an
  // element spread compiles through svelte's set_attributes runtime,
  // which imports clsx -- pulling a package into the shipped bundle
  // that nothing here uses, and one whose exports map hides its
  // package.json from tools/licenses/generate-notices.mjs, failing the
  // attribution gate. An action attaches the same listeners directly to
  // the node and compiles to none of that.
  function activate(node: HTMLElement, run: () => void) {
    const onmousedown = (e: MouseEvent) => {
      pressedTarget = e.currentTarget
    }
    const onmouseup = (e: MouseEvent) => {
      const startedHere = pressedTarget === e.currentTarget
      pressedTarget = null
      if (!startedHere) return
      if (selectionWithinRow()) return
      run()
    }
    const onkeydown = (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== ' ') return
      e.preventDefault()
      run()
    }
    node.addEventListener('mousedown', onmousedown)
    node.addEventListener('mouseup', onmouseup)
    node.addEventListener('keydown', onkeydown)
    return {
      destroy() {
        node.removeEventListener('mousedown', onmousedown)
        node.removeEventListener('mouseup', onmouseup)
        node.removeEventListener('keydown', onkeydown)
      },
    }
  }
</script>

<div
  bind:this={rowEl}
  class="row"
  class:member
  class:expandable
  class:dimmed
  class:banded
  class:flagged
  data-chain={event.chain}
  title={rawTooltip(event.raw, event.rawTruncated)}
>
  {#if expandable}
    <button
      class="cell time cell-btn count-cell"
      onclick={() => onToggle?.()}
      aria-expanded={expanded}
      title={expanded ? 'Hide these events' : `Show the ${count} events in this group`}
    >
      <span class="count">{count}</span>
      <span class="chev" class:open={expanded}>›</span>
    </button>
  {:else}
    <span class="cell time">
      <span
        class="cell-btn time-btn"
        role="button"
        tabindex="0"
        title="Show this event's details"
        use:activate={() => onOpen?.()}
      >{formatTimeMs(event.time)}</span>{#if flagged}<i
          class="rmk"
          title="this row's source has an open flag against it"
          aria-hidden="true">&#9873;</i>{/if}
    </span>
  {/if}

  <!-- #717: restored, in its pre-#644 spot right after Time -- see
       columns.svelte.ts's own comment. Only a copy button beside it,
       no pencil: there is no device-name entity type for
       EditNameButton to open (see nameEditor.svelte.ts). -->
  <span class="cell device">
    <span
      class="cell-btn device-btn"
      role="button"
      tabindex="0"
      title="Filter to device: {deviceName || event.deviceId}"
      use:activate={() => appState.setFilter('device', event.deviceId)}
    >{deviceName || event.deviceId}</span>
    <CopyButton value={event.deviceId} label="device id" />
  </span>

  <span
    class="cell action cell-btn"
    role="button"
    tabindex="0"
    title="Filter to action: {event.action}"
    use:activate={() => appState.setFilter('action', event.action)}
  >
    <ActionBadge action={event.action} />
  </span>

  <!-- #717: restored, in its pre-#644 spot right after Action -- see
       columns.svelte.ts's own comment. Plain click-to-filter text, no
       buttons, same treatment Proto gets. -->
  {#if event.chain}
    <span
      class="cell chain cell-btn"
      role="button"
      tabindex="0"
      title="Filter to chain: {event.chain}"
      use:activate={() => appState.setFilter('chain', event.chain)}
    >{event.chain}</span>
  {:else}
    <span class="cell chain">—</span>
  {/if}

  <!-- Source and Destination each split into a name column and a dim
       address column (#644): the name column shows the resolved host
       name when one exists, otherwise the bare address itself (dim, with
       the country code appended) -- and the address column then repeats
       nothing, showing the raw IP only where the name column is showing
       a name, an em dash otherwise. -->
  {#if event.srcIp}
    <span class="cell addr">
      <span
        class="cell-btn addr-btn"
        class:bare={!event.srcHostName}
        role="button"
        tabindex="0"
        title={event.srcHostName ? `${event.srcHostName} — filter to source: ${event.srcIp}` : `Filter to source: ${event.srcIp}`}
        use:activate={() => appState.setFilter('srcQuery', event.srcIp ?? '')}
      >
        {event.srcHostName || event.srcIp}{#if !event.srcHostName && event.srcCountry}
          <span class="geo">{event.srcCountry}</span>{/if}
      </span>
      <CopyButton value={event.srcIp} label="source IP" />
      {#if nameEditorState.available}
        <EditNameButton type="host" value={event.srcIp} device={event.deviceId} label={event.srcIp} />
      {/if}
    </span>
    <span class="cell ip">{event.srcHostName ? event.srcIp : '—'}</span>
  {:else}
    <span class="cell addr">—</span>
    <span class="cell ip">—</span>
  {/if}

  <!-- #717: restored, riding beside Source's own facts (its
       pre-#644 neighbour was Destination -- see columns.svelte.ts).
       Same minimal treatment as the existing dst Port column: the bare
       number, no copy/edit/investigate chrome, friendly name (if any)
       in the tooltip only. -->
  {#if event.srcPort}
    <span class="cell port srcport">
      <span
        class="cell-btn port-btn"
        role="button"
        tabindex="0"
        title={event.srcPortName
          ? `${event.srcPortName} — filter to port: ${event.srcPort}`
          : `Filter to port: ${event.srcPort}`}
        use:activate={() => appState.setFilter('port', String(event.srcPort))}
      >{event.srcPort}</span>
    </span>
  {:else}
    <span class="cell port srcport">—</span>
  {/if}

  <!-- #717: restored. Plain text, not click-to-filter -- no Filters
       field takes a MAC (see EventDetailSheet.svelte's own comment on
       its Src MAC row), so a link that filtered to nothing would be a
       promise the bar can't keep. Sourced straight off the event, the
       same field the sheet's Src MAC row already reads -- RouterOS
       includes src-mac on some chains/firmwares and not others (see
       internal/routeros/parser.go), never a separate per-row lookup. -->
  <span class="cell mac">{event.srcMac || '—'}</span>

  {#if event.dstIp}
    <span class="cell addr">
      <span
        class="cell-btn addr-btn"
        class:bare={!event.dstHostName}
        role="button"
        tabindex="0"
        title={event.dstHostName ? `${event.dstHostName} — filter to destination: ${event.dstIp}` : `Filter to destination: ${event.dstIp}`}
        use:activate={() => appState.setFilter('dstQuery', event.dstIp ?? '')}
      >
        {event.dstHostName || event.dstIp}{#if !event.dstHostName && event.dstCountry}
          <span class="geo">{event.dstCountry}</span>{/if}
      </span>
      <CopyButton value={event.dstIp} label="destination IP" />
      {#if nameEditorState.available}
        <EditNameButton type="host" value={event.dstIp} device={event.deviceId} label={event.dstIp} />
      {/if}
    </span>
    <span class="cell ip">{event.dstHostName ? event.dstIp : '—'}</span>
  {:else}
    <span class="cell addr">—</span>
    <span class="cell ip">—</span>
  {/if}

  {#if event.protocol}
    <span
      class="cell proto cell-btn"
      role="button"
      tabindex="0"
      title="Filter to protocol: {event.protocol}"
      use:activate={() => appState.setFilter('protocol', event.protocol ?? '')}
    >
      {event.protocol}
    </span>
  {:else}
    <span class="cell proto">—</span>
  {/if}

  <!-- #717: restored, in its pre-#644 spot right after Proto -- see
       columns.svelte.ts's own comment. Split into its two tokens (in/
       out) so either can be clicked independently; both write the same
       shared `interface` filter (matches either side). -->
  <span class="cell iface">
    {#if event.inInterface}
      <span
        class="cell-btn iface-btn"
        role="button"
        tabindex="0"
        title="Filter to interface: {event.inInterface}"
        use:activate={() => appState.setFilter('interface', event.inInterface ?? '')}
      >{event.inInterface}</span>
    {/if}
    {#if event.inInterface && event.outInterface}
      <span class="iface-sep">→</span>
    {/if}
    {#if event.outInterface}
      <span
        class="cell-btn iface-btn"
        role="button"
        tabindex="0"
        title="Filter to interface: {event.outInterface}"
        use:activate={() => appState.setFilter('interface', event.outInterface ?? '')}
      >{event.outInterface}</span>
    {/if}
    {#if !event.inInterface && !event.outInterface}—{/if}
  </span>

  {#if event.dstPort}
    <span class="cell port">
      <span
        class="cell-btn port-btn"
        role="button"
        tabindex="0"
        title={event.dstPortName
          ? `${event.dstPortName} — filter to port: ${event.dstPort}`
          : `Filter to port: ${event.dstPort}`}
        use:activate={() => appState.setFilter('port', String(event.dstPort))}
      >
        {event.dstPort}
      </span>
    </span>
  {:else}
    <span class="cell port">—</span>
  {/if}

  <!-- #717: restored, beside Port, its pre-#644 neighbour -- see
       columns.svelte.ts's own comment on why it lands after Proto/
       Interfaces rather than before them. The translated address only
       (no lookup trigger here): the pushed-table "which NAT rule did
       this" trigger stays exactly where it already lived, in the Rule
       cell's RouterRuleButton above, since #644's own commit message
       records fixing a duplicate of that same button once already. -->
  <span class="cell nat" class:has-value={!!event.natIp} title={event.natRaw}>
    {#if event.natIp}
      {#if natFilterKey}
        <span
          class="cell-btn nat-value"
          role="button"
          tabindex="0"
          title="Filter to {natFilterKey === 'srcQuery' ? 'source' : 'destination'}: {event.natIp}"
          use:activate={() => {
            if (natFilterKey) appState.setFilter(natFilterKey, event.natIp ?? '')
          }}
        >→ {formatAddr(event.natIp, event.natPort)}</span>
      {:else}
        <span class="nat-value">→ {formatAddr(event.natIp, event.natPort)}</span>
      {/if}
    {:else}
      —
    {/if}
  </span>

  {#if event.ruleLabel}
    <span class="cell rule">
      <span
        class="cell-btn rule-btn"
        role="button"
        tabindex="0"
        title={event.ruleName ? `${event.ruleName} — filter to rule: ${event.ruleLabel}` : `Filter to rule: ${event.ruleLabel}`}
        use:activate={() => (appState.filters = { ...appState.filters, rule: event.ruleLabel, ruleRegex: false })}
      >
        {event.ruleName || event.ruleLabel}
      </span>
      <CopyButton value={event.ruleLabel} label="rule label" />
      {#if nameEditorState.available}
        <EditNameButton type="rule" value={event.ruleLabel} label={event.ruleLabel} />
      {/if}
      <!-- A NAT event's log-prefix names a rule in the NAT table, not in
           the filter table, so the rule cell resolves against the same
           table the NAT cell used to (#445). Pointing it at the filter
           table would report every logged translation as unresolvable. -->
      <RouterRuleButton
        mode={natFilterKey ? 'nat' : 'rule'}
        device={event.deviceId}
        ruleLabel={event.ruleLabel}
        {event}
        evidence={count > 1 ? 'group-head' : 'row'}
      />
    </span>
  {:else}
    <span class="cell rule">—</span>
  {/if}
</div>

<style>
  .row {
    display: contents;
  }

  /* A grouped collapsed row: the count takes the time column, big
     enough to scan down the left edge. A singleton keeps its timestamp
     instead -- a large "1" on every unrepeated row would be noise in a
     bigger font. */
  .count-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    text-align: left;
  }

  .count {
    font-size: 15px;
    font-weight: 700;
    color: var(--fg);
    font-variant-numeric: tabular-nums;
  }

  .chev {
    color: var(--fg-muted);
    transition: transform 0.12s ease;
    display: inline-block;
  }

  .chev.open {
    transform: rotate(90deg);
  }

  /* An individual event inside an opened group. Indented and dimmed so
     the group's own row still reads as the thing on the page. */
  .row.member > :global(.cell:first-child) {
    padding-left: 22px;
    border-left: 2px solid var(--accent);
  }

  .row.member :global(.cell) {
    opacity: 0.75;
  }

  /* #644's whisper fence: matches the-whole.html's own .outside{opacity}
     rule exactly -- everything outside a drawn fence range dims, it
     doesn't disappear (fencing is a display lens, not a filter). */
  .row.dimmed :global(.cell) {
    opacity: 0.25;
  }

  .cell {
    padding: 9px 10px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 14px;
    line-height: 1.4;
  }

  /* #644's squared columns: the per-action row washes are gone -- what a
     row did is the badge's job -- and alternate rows carry a faint band
     instead, mixed down from the hover token rather than introducing a
     color of its own. Hover keeps the full-strength token; same
     specificity, so source order lets it win over the band. */
  .row.banded .cell {
    background: color-mix(in srgb, var(--bg-hover) 55%, transparent);
  }

  /* #685: built as drawn, not as a per-row glyph -- the-whole.html marks
     a row on a flagged pathway with a full-row wash (`table.stream
     tr.hl td { background: rgba(255, 84, 112, 0.05); }`, its rgb triplet
     is var(--alarm)) rather than an icon in any cell. Declared after
     .banded so it wins when a flagged row also happens to fall on a
     banded stripe -- the design shows one wash per row, not two layered
     -- and before :hover so hovering still shows the full-strength token. */
  .row.flagged .cell {
    background: color-mix(in srgb, var(--alarm) 5%, transparent);
  }

  /* The mark that annotates that wash (the-whole.html's .rmk). Sized and
     coloured from the scene: alarm ink, a step down from the row's text,
     and set clear of the time so the figures keep their own edge. */
  .rmk {
    font-style: normal;
    color: var(--alarm);
    margin-left: 8px;
    font-size: 11px;
  }

  .row:hover .cell {
    background: var(--bg-hover);
  }

  .time,
  .addr,
  .ip,
  .port,
  .proto,
  .device,
  .chain,
  .mac,
  .iface,
  .nat {
    font-family: var(--font-mono);
    color: var(--fg-muted);
  }

  /* Keeps the timestamp in view while horizontally scrolling the table on
     narrow viewports, where the full column set no longer fits -- without
     it there's no fixed reference point tying a scrolled-out row back to
     when it happened. Later columns scroll underneath it, so its
     background must be opaque: the translucent band/hover washes are
     wrapped as a same-color-to-itself gradient (a valid image layer) over
     an opaque --bg-elevated color layer -- a background shorthand only
     allows a plain color in its *last* layer -- flattening the cell to
     one opaque paint before it is composited over the page. */
  .time {
    position: sticky;
    left: 0;
    z-index: 1;
    background: var(--bg-elevated);
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    font-variant-numeric: tabular-nums;
  }

  .row.banded .time {
    background:
      linear-gradient(
        color-mix(in srgb, var(--bg-hover) 55%, transparent),
        color-mix(in srgb, var(--bg-hover) 55%, transparent)
      ),
      var(--bg-elevated);
  }

  /* Same wash, same opaque-flattening trick as .banded's -- the sticky
     time cell needs its own paint so later columns scrolling underneath
     it don't show through. */
  .row.flagged .time {
    background:
      linear-gradient(
        color-mix(in srgb, var(--alarm) 5%, transparent),
        color-mix(in srgb, var(--alarm) 5%, transparent)
      ),
      var(--bg-elevated);
  }

  .row:hover .time {
    background: linear-gradient(var(--bg-hover), var(--bg-hover)), var(--bg-elevated);
  }

  .time-btn {
    flex: none;
    width: auto;
  }

  /* The name columns: a resolved host name reads bright; a bare address
     standing in for one reads dim (the-whole.html's .host / .geo split),
     with its country code dimmer and smaller beside it. */
  .cell.addr {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--fg);
  }

  .addr-btn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .addr-btn.bare {
    color: var(--fg-dim);
  }

  .geo {
    color: var(--fg-dim);
    font-size: 11px;
  }

  /* The dim address columns beside Source/Destination: the raw IP where
     the name column shows a name, an em dash where the address is
     already the name column's content. Right-aligned, never
     interactive -- the name token beside it is the filter target. */
  .ip {
    text-align: right;
    color: var(--fg-dim);
    font-variant-numeric: tabular-nums;
  }

  .proto {
    text-transform: lowercase;
  }

  /* #717: Device restored -- same click-to-filter-button-plus-copy-button
     shape as .cell.addr above, but its own rule rather than sharing that
     class: reusing "addr" here would put Device into the same
     querySelectorAll('.cell.addr') list Source/Destination's name
     columns are indexed by. Stays muted (the shared rule above), not
     bright like a name column -- it is context for the row, not the row's
     subject. */
  .cell.device {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .device-btn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rule {
    font-family: var(--font-mono);
    color: var(--fg-muted);
  }

  /* The rule cell holds the click-to-filter button, its copy button, the
     pencil, and the pushed-table lookup trigger side by side -- same
     layout the name cells use. */
  .cell.rule {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .rule-btn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Port stands alone, right-aligned since port numbers are numeric. */
  .cell.port {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
  }

  .port-btn {
    flex: none;
    width: auto;
    text-align: right;
  }

  /* #717: Interfaces restored, split into its in/out tokens same as it
     was pre-#644. */
  .cell.iface {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .iface-btn {
    flex: none;
    width: auto;
  }

  .iface-sep {
    color: var(--fg-dim);
  }

  /* #717: NAT restored. Dim until there is a translated address to show,
     then reads in the accent color -- the same "this is worth noticing"
     treatment EventDetailSheet.svelte's own NAT row (.v.accent) already
     gives it. No copy button: unlike the address/port/rule tokens, its
     value is never a resolved label standing in for a different raw
     value, so there's no gap for a copy button to bridge (#439). */
  .cell.nat {
    display: flex;
    align-items: center;
  }

  .cell.nat.has-value {
    color: var(--accent);
    font-weight: 600;
  }

  .nat-value {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Reset button chrome on click-to-filter cells so they read exactly
     like the plain-text cells they replace -- only a hover underline
     hints they're interactive. These are role="button" elements, not
     <button>s (see #439 in the script block above: <button> content is
     never selectable, in any browser, regardless of this stylesheet) --
     `display: block` replaces the block-level box a <button> used to
     provide by default. */
  .cell-btn {
    display: block;
    background: none;
    border: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    width: 100%;
  }

  .cell-btn:hover {
    text-decoration: underline;
  }

  /* outline-offset is negative (inset), not positive like most of the
     app's other :focus-visible rings (e.g. Toolbar.svelte's
     .logo-button) -- .cell clips overflow, so an outline drawn outside
     the box would be cut off here. */
  .cell-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: 2px;
  }

  /* #439: hover-revealed per-token copy glyph (CopyButton.svelte).
     Hidden by opacity (never display/visibility) so it stays in the tab
     order and reachable by keyboard -- :focus-within covers tabbing to
     it directly, without first hovering the row. Scoped to `.row` (not
     each `.cell`) to match hovering *anywhere* in the row revealing
     every token's glyph at once, not just the one directly under the
     pointer -- and scoped to *this component's* rows at all, not bare
     :global, because EventDetailSheet renders the same components
     always-visible on a surface with no hover concept. */
  /* #413's pencil rides in the same reveal, immediately after the copy
     glyph -- the slot #439 reserved for it. #644 adds the rule cell's
     pushed-table lookup trigger: the quiet rows the restyle asks for
     have no permanently-visible widgets, so it reveals on the same
     terms rather than sitting on every row. All listed together rather
     than given rules of their own so the three can never drift into
     revealing at different moments. */
  .row :global(.copy-btn),
  .row :global(.edit-btn),
  .row :global(.investigate) {
    opacity: 0;
  }

  .row:hover :global(.copy-btn),
  .row:focus-within :global(.copy-btn),
  .row:hover :global(.edit-btn),
  .row:focus-within :global(.edit-btn),
  .row:hover :global(.investigate),
  .row:focus-within :global(.investigate) {
    opacity: 1;
  }

  @media (prefers-reduced-motion: no-preference) {
    .row :global(.copy-btn),
    .row :global(.edit-btn),
    .row :global(.investigate) {
      transition: opacity 0.12s ease;
    }
  }
</style>
