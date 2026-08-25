<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import type { FirewallEvent } from '../lib/types'
  import { countryFlag, formatAddr, formatTime, isPublicIp, rawTooltip } from '../lib/format'
  import { appState } from '../lib/state.svelte'
  import ActionBadge from './ActionBadge.svelte'
  import IpInvestigateButton from './IpInvestigateButton.svelte'
  import PortInvestigateButton from './PortInvestigateButton.svelte'
  import RouterRuleButton from './RouterRuleButton.svelte'
  import CopyButton from './CopyButton.svelte'
  import EditNameButton from './EditNameButton.svelte'
  // Asked per pencil below rather than left to EditNameButton's own
  // guard: a viewer's row would otherwise still build five components
  // that render nothing, and the live view renders up to
  // MAX_RENDERED_ROWS of these rows.
  import { nameEditorState } from '../lib/nameEditor.svelte'
  import { lookupPort } from '../lib/commonPorts'

  let {
    event,
    deviceName,
    // Grouping (#341). count > 1 means this row stands for several
    // identical connections and the time cell shows the count instead --
    // the time is the same second on every row at any real rate, so it
    // is the least useful thing in the most prominent column.
    count = 1,
    // flagged means "this row's source has an active flag against it",
    // not "this event caused that flag": a flag records what it was
    // raised about, not which events evidenced it (#341).
    flagged = false,
    expandable = false,
    expanded = false,
    onToggle,
    // member rows are the individual events shown under an opened group.
    member = false,
  }: {
    event: FirewallEvent
    deviceName: string
    count?: number
    flagged?: boolean
    expandable?: boolean
    expanded?: boolean
    onToggle?: () => void
    member?: boolean
  } = $props()

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const dstFlag = $derived(countryFlag(event.dstCountry))

  // Which Filters field (if either) a NAT token's translated address
  // belongs to (#438's NAT-parity section). Only the two dedicated NAT
  // chains say which side was rewritten -- see
  // internal/routeros/parser.go's isNATChain, mirrored exactly here and
  // in lib/state.svelte.ts's srcCandidates/dstCandidates. A NAT
  // annotation inherited onto some other chain (that file's parseNAT
  // comment) has no such chain to read the direction off, so it stays
  // inert rather than filtering the wrong side.
  const natFilterKey = $derived(
    event.chain?.toLowerCase() === 'srcnat'
      ? 'srcQuery'
      : event.chain?.toLowerCase() === 'dstnat'
        ? 'dstQuery'
        : null,
  )

  // #439: row tokens (device, action, chain, addresses, ports, protocol,
  // rule) used to be <button> elements. That's *why* row text couldn't be
  // selected/copied before this -- every browser's UA stylesheet makes
  // button content unselectable, independent of anything app.css did or
  // didn't set (there was no explicit `user-select: none` anywhere to
  // remove). They're plain elements with role="button" now, so their
  // text is ordinary selectable content, and click-to-filter is wired up
  // by hand below instead of coming for free from <button onclick>.
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
  class="row row-{event.action}"
  class:member
  class:expandable
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
      {#if flagged}<span class="flag-mark" title="This source has an active flag against it">⚑</span
        >{/if}
      <span class="chev" class:open={expanded}>›</span>
    </button>
  {:else}
    <span class="cell time">
      {formatTime(event.time)}
      {#if flagged}<span class="flag-mark" title="This source has an active flag against it">⚑</span
        >{/if}
    </span>
  {/if}

  <span class="cell device">
    <span
      class="cell-btn device-btn"
      role="button"
      tabindex="0"
      title="Filter to device: {deviceName}"
      use:activate={() => appState.setFilter('device', event.deviceId)}
    >
      {deviceName}
    </span>
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

  {#if event.chain}
    <span
      class="cell chain cell-btn"
      role="button"
      tabindex="0"
      title="Filter to chain: {event.chain}"
      use:activate={() => appState.setFilter('chain', event.chain)}
    >
      {event.chain}
    </span>
  {:else}
    <span class="cell chain">—</span>
  {/if}

  {#if event.srcIp}
    <span class="cell addr">
      {#if srcFlag}
        <span
          class="cell-btn flag-btn"
          role="button"
          tabindex="0"
          title="Filter to source country: {event.srcCountry}"
          use:activate={() => appState.setFilter('srcCountry', event.srcCountry ?? '')}
        >{srcFlag}</span>
      {/if}
      <span
        class="cell-btn addr-btn"
        role="button"
        tabindex="0"
        title={event.srcHostName ? `${event.srcHostName} — filter to source: ${event.srcIp}` : `Filter to source: ${event.srcIp}`}
        use:activate={() => appState.setFilter('srcQuery', event.srcIp ?? '')}
      >
        {event.srcHostName || event.srcIp}
      </span>
      <CopyButton value={event.srcIp} label="source IP" />
      {#if nameEditorState.available}
        <EditNameButton type="host" value={event.srcIp} device={event.deviceId} label={event.srcIp} />
      {/if}
      {#if isPublicIp(event.srcIp)}
        <IpInvestigateButton ip={event.srcIp} />
      {/if}
    </span>
  {:else}
    <span class="cell addr">—</span>
  {/if}

  {#if event.srcPort}
    <span class="cell port">
      <span
        class="cell-btn port-btn"
        role="button"
        tabindex="0"
        title={event.srcPortName
          ? `${event.srcPortName} — filter to port: ${event.srcPort}`
          : `Filter to port: ${event.srcPort}`}
        use:activate={() => appState.setFilter('port', String(event.srcPort))}
      >
        {event.srcPortName || event.srcPort}
      </span>
      <CopyButton value={String(event.srcPort)} label="source port" />
      {#if nameEditorState.available}
        <EditNameButton type="port" value={String(event.srcPort)} label="port {event.srcPort}" />
      {/if}
      {#if lookupPort(event.srcPort)}
        <PortInvestigateButton port={event.srcPort} />
      {/if}
    </span>
  {:else}
    <span class="cell port">—</span>
  {/if}

  {#if event.dstIp}
    <span class="cell addr">
      {#if dstFlag}
        <span
          class="cell-btn flag-btn"
          role="button"
          tabindex="0"
          title="Filter to destination country: {event.dstCountry}"
          use:activate={() => appState.setFilter('dstCountry', event.dstCountry ?? '')}
        >{dstFlag}</span>
      {/if}
      <span
        class="cell-btn addr-btn"
        role="button"
        tabindex="0"
        title={event.dstHostName ? `${event.dstHostName} — filter to destination: ${event.dstIp}` : `Filter to destination: ${event.dstIp}`}
        use:activate={() => appState.setFilter('dstQuery', event.dstIp ?? '')}
      >
        {event.dstHostName || event.dstIp}
      </span>
      <CopyButton value={event.dstIp} label="destination IP" />
      {#if nameEditorState.available}
        <EditNameButton type="host" value={event.dstIp} device={event.deviceId} label={event.dstIp} />
      {/if}
      {#if isPublicIp(event.dstIp)}
        <IpInvestigateButton ip={event.dstIp} />
      {/if}
    </span>
  {:else}
    <span class="cell addr">—</span>
  {/if}

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
        {event.dstPortName || event.dstPort}
      </span>
      <CopyButton value={String(event.dstPort)} label="destination port" />
      {#if nameEditorState.available}
        <EditNameButton type="port" value={String(event.dstPort)} label="port {event.dstPort}" />
      {/if}
      {#if lookupPort(event.dstPort)}
        <PortInvestigateButton port={event.dstPort} />
      {/if}
    </span>
  {:else}
    <span class="cell port">—</span>
  {/if}

  <span class="cell addr nat" class:has-value={!!event.natIp} title={event.natRaw}>
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
      <!-- #445: the trigger carries the event, and whether it stands for
           a whole group, so the popup can say what it was evaluated
           against. Group keys exclude the interfaces (lib/grouping.ts),
           so a head row's answer is not automatically its members'. -->
      <RouterRuleButton
        mode="nat"
        device={event.deviceId}
        ruleLabel={event.ruleLabel}
        {event}
        evidence={count > 1 ? 'group-head' : 'row'}
      />
    {:else}
      —
    {/if}
  </span>

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

  <!-- #438: the one cell that wasn't click-to-filter, despite the bar
       already having an Interface box to receive it. Split into its two
       tokens (in/out) so either can be clicked independently -- both
       still write the same shared `interface` filter, which already
       matches either side (unchanged). -->
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
      <RouterRuleButton mode="rule" device={event.deviceId} ruleLabel={event.ruleLabel} />
      <!-- A NAT event's log-prefix names a rule in the NAT table, not in
           the filter table, so the rule cell resolves against the same
           table the NAT cell does (#445). Pointing it at the filter
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

  .flag-mark {
    color: var(--reject);
    font-size: 12px;
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

  .cell {
    padding: 9px 10px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 14px;
    line-height: 1.4;
  }

  .row:hover .cell {
    background: var(--bg-hover);
  }

  .row-accept .cell {
    background: var(--row-accept-bg);
  }
  .row-drop .cell {
    background: var(--row-drop-bg);
  }
  .row-reject .cell {
    background: var(--row-reject-bg);
  }
  .row-log .cell {
    background: var(--row-log-bg);
  }
  .row-marked .cell {
    background: var(--row-marked-bg);
  }
  .row-natted .cell {
    background: var(--row-natted-bg);
  }
  .row-unknown .cell {
    background: var(--row-unknown-bg);
  }

  /* Same specificity as `.row:hover .cell` above; defined after it so
     source order lets these win on hover instead of the plain --bg-hover. */
  .row-accept:hover .cell {
    background: var(--row-accept-bg-hover);
  }
  .row-drop:hover .cell {
    background: var(--row-drop-bg-hover);
  }
  .row-reject:hover .cell {
    background: var(--row-reject-bg-hover);
  }
  .row-log:hover .cell {
    background: var(--row-log-bg-hover);
  }
  .row-marked:hover .cell {
    background: var(--row-marked-bg-hover);
  }
  .row-natted:hover .cell {
    background: var(--row-natted-bg-hover);
  }
  .row-unknown:hover .cell {
    background: var(--row-unknown-bg-hover);
  }

  /* The row-tint backgrounds above are deliberately translucent (washed
     over --bg-elevated), which is fine while every cell scrolls together
     -- but .time stays pinned in place (see below) while later columns
     scroll underneath it, so its translucent background would let their
     text bleed through. A background shorthand only allows a plain color
     in its *last* layer, so the tint is wrapped as a same-color-to-itself
     gradient (a valid image layer) over an opaque --bg-elevated color
     layer -- both painted within this cell's own box before the sticky
     cell is composited over the page, flattening it to one opaque paint. */
  .row-accept .time {
    background: linear-gradient(var(--row-accept-bg), var(--row-accept-bg)), var(--bg-elevated);
  }
  .row-drop .time {
    background: linear-gradient(var(--row-drop-bg), var(--row-drop-bg)), var(--bg-elevated);
  }
  .row-reject .time {
    background: linear-gradient(var(--row-reject-bg), var(--row-reject-bg)), var(--bg-elevated);
  }
  .row-log .time {
    background: linear-gradient(var(--row-log-bg), var(--row-log-bg)), var(--bg-elevated);
  }
  .row-marked .time {
    background: linear-gradient(var(--row-marked-bg), var(--row-marked-bg)), var(--bg-elevated);
  }
  .row-natted .time {
    background: linear-gradient(var(--row-natted-bg), var(--row-natted-bg)), var(--bg-elevated);
  }
  .row-unknown .time {
    background: linear-gradient(var(--row-unknown-bg), var(--row-unknown-bg)), var(--bg-elevated);
  }
  .row-accept:hover .time {
    background: linear-gradient(var(--row-accept-bg-hover), var(--row-accept-bg-hover)), var(--bg-elevated);
  }
  .row-drop:hover .time {
    background: linear-gradient(var(--row-drop-bg-hover), var(--row-drop-bg-hover)), var(--bg-elevated);
  }
  .row-reject:hover .time {
    background: linear-gradient(var(--row-reject-bg-hover), var(--row-reject-bg-hover)), var(--bg-elevated);
  }
  .row-log:hover .time {
    background: linear-gradient(var(--row-log-bg-hover), var(--row-log-bg-hover)), var(--bg-elevated);
  }
  .row-marked:hover .time {
    background: linear-gradient(var(--row-marked-bg-hover), var(--row-marked-bg-hover)), var(--bg-elevated);
  }
  .row-natted:hover .time {
    background: linear-gradient(var(--row-natted-bg-hover), var(--row-natted-bg-hover)), var(--bg-elevated);
  }
  .row-unknown:hover .time {
    background: linear-gradient(var(--row-unknown-bg-hover), var(--row-unknown-bg-hover)), var(--bg-elevated);
  }

  .time,
  .addr,
  .port,
  .proto {
    font-family: var(--font-mono);
    color: var(--fg-muted);
  }

  /* Keeps the timestamp in view while horizontally scrolling the table on
     narrow viewports, where the full column set no longer fits -- without
     it there's no fixed reference point tying a scrolled-out row back to
     when it happened. Background comes from the existing .row-* rules
     above (same specificity, .cell), so it stays opaque over the cells
     scrolling underneath it. */
  .time {
    position: sticky;
    left: 0;
    z-index: 1;
  }

  .addr {
    color: var(--fg);
  }

  .nat {
    color: var(--fg-dim);
  }

  .nat.has-value {
    color: var(--accent);
    font-weight: 600;
  }

  .device {
    color: var(--fg-muted);
  }

  /* The device cell now holds the click-to-filter target plus its copy
     button side by side -- same shape as .cell.addr below. */
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

  /* The rule cell holds the click-to-filter button, its copy button, and
     the pushed-table lookup trigger side by side -- same layout the addr
     cells use. */
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

  /* Same shape for the NAT cell: value plus the NAT-table trigger. No
     copy button here -- NAT isn't one of #439's row-token categories
     (addresses/ports/rules/device names), and unlike those it has no
     resolved-label-vs-raw-value gap to bridge: what's shown already is
     the raw address. */
  .nat-value {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* #438: split into its in/out tokens (see the markup above), same
     side-by-side shape as the other multi-part cells. */
  .cell.iface {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--fg-muted);
    font-size: 13px;
  }

  .iface-btn {
    flex: none;
    width: auto;
  }

  .iface-sep {
    color: var(--fg-dim);
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

  /* Address cells hold the IP filter button, its copy button, and (for
     public IPs) the investigate trigger side by side -- overflow/ellipsis
     moves from `.cell` (which now just lays them out) onto the filter
     button itself, since that's the element with the actual long text.
     Port is its own column now (see .port below), not crammed in here. */
  .cell.addr {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .addr-btn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* #438: the country flag is now its own click-to-filter token (sets
     Source/Destination country in the bar), separate from the address
     token beside it -- a fixed-width emoji, no ellipsis needed. */
  .flag-btn {
    flex: none;
    width: auto;
  }

  /* Same side-by-side shape as .cell.addr above (filter button + copy
     button + optional investigate trigger), but right-aligned since port
     numbers are numeric. */
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

  /* #439: hover-revealed per-token copy glyph (CopyButton.svelte).
     Hidden by opacity (never display/visibility) so it stays in the tab
     order and reachable by keyboard -- :focus-within covers tabbing to
     it directly, without first hovering the row. Scoped to `.row` (not
     each `.cell`) to match hovering *anywhere* in the row revealing
     every token's glyph at once, not just the one directly under the
     pointer. */
  /* #413's pencil rides in the same reveal, immediately after the copy
     glyph -- the slot #439 reserved for it. Listed alongside rather
     than given rules of its own so the two can never drift into
     revealing at different moments. */
  :global(.copy-btn),
  :global(.edit-btn) {
    opacity: 0;
  }

  .row:hover :global(.copy-btn),
  .row:focus-within :global(.copy-btn),
  .row:hover :global(.edit-btn),
  .row:focus-within :global(.edit-btn) {
    opacity: 1;
  }

  @media (prefers-reduced-motion: no-preference) {
    :global(.copy-btn),
    :global(.edit-btn) {
      transition: opacity 0.12s ease;
    }
  }
</style>
