<script lang="ts">
  import type { FirewallEvent } from '../lib/types'
  import { countryFlag, formatAddr, formatTime, isPublicIp } from '../lib/format'
  import { appState } from '../lib/state.svelte'
  import ActionBadge from './ActionBadge.svelte'
  import IpInvestigateButton from './IpInvestigateButton.svelte'
  import PortInvestigateButton from './PortInvestigateButton.svelte'
  import { lookupPort } from '../lib/commonPorts'

  let { event, deviceName }: { event: FirewallEvent; deviceName: string } = $props()

  const ifaces = $derived(
    [event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—',
  )

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const dstFlag = $derived(countryFlag(event.dstCountry))
</script>

<div class="row row-{event.action}" title={event.raw}>
  <span class="cell time">{formatTime(event.time)}</span>

  <button
    class="cell device cell-btn"
    onclick={() => appState.setFilter('device', event.deviceId)}
    title="Filter to device: {deviceName}"
  >
    {deviceName}
  </button>

  <button
    class="cell action cell-btn"
    onclick={() => appState.setFilter('action', event.action)}
    title="Filter to action: {event.action}"
  >
    <ActionBadge action={event.action} />
  </button>

  {#if event.chain}
    <button
      class="cell chain cell-btn"
      onclick={() => appState.setFilter('chain', event.chain)}
      title="Filter to chain: {event.chain}"
    >
      {event.chain}
    </button>
  {:else}
    <span class="cell chain">—</span>
  {/if}

  {#if event.srcIp}
    <span class="cell addr">
      <button
        class="cell-btn addr-btn"
        title="Filter to IP: {event.srcIp}"
        onclick={() => appState.setFilter('ip', event.srcIp ?? '')}
      >
        {srcFlag ? `${srcFlag} ` : ''}{event.srcIp}
      </button>
      {#if isPublicIp(event.srcIp)}
        <IpInvestigateButton ip={event.srcIp} />
      {/if}
    </span>
  {:else}
    <span class="cell addr">—</span>
  {/if}

  {#if event.srcPort}
    <span class="cell port">
      <button
        class="cell-btn port-btn"
        title="Filter to port: {event.srcPort}"
        onclick={() => appState.setFilter('port', String(event.srcPort))}
      >
        {event.srcPort}
      </button>
      {#if lookupPort(event.srcPort)}
        <PortInvestigateButton port={event.srcPort} />
      {/if}
    </span>
  {:else}
    <span class="cell port">—</span>
  {/if}

  {#if event.dstIp}
    <span class="cell addr">
      <button
        class="cell-btn addr-btn"
        title="Filter to IP: {event.dstIp}"
        onclick={() => appState.setFilter('ip', event.dstIp ?? '')}
      >
        {dstFlag ? `${dstFlag} ` : ''}{event.dstIp}
      </button>
      {#if isPublicIp(event.dstIp)}
        <IpInvestigateButton ip={event.dstIp} />
      {/if}
    </span>
  {:else}
    <span class="cell addr">—</span>
  {/if}

  {#if event.dstPort}
    <span class="cell port">
      <button
        class="cell-btn port-btn"
        title="Filter to port: {event.dstPort}"
        onclick={() => appState.setFilter('port', String(event.dstPort))}
      >
        {event.dstPort}
      </button>
      {#if lookupPort(event.dstPort)}
        <PortInvestigateButton port={event.dstPort} />
      {/if}
    </span>
  {:else}
    <span class="cell port">—</span>
  {/if}

  <span class="cell addr nat" class:has-value={!!event.natIp} title={event.natRaw}>
    {event.natIp ? `→ ${formatAddr(event.natIp, event.natPort)}` : '—'}
  </span>

  {#if event.protocol}
    <button
      class="cell proto cell-btn"
      onclick={() => appState.setFilter('protocol', event.protocol ?? '')}
      title="Filter to protocol: {event.protocol}"
    >
      {event.protocol}
    </button>
  {:else}
    <span class="cell proto">—</span>
  {/if}

  <span class="cell iface">{ifaces}</span>

  {#if event.ruleLabel}
    <button
      class="cell rule cell-btn"
      onclick={() => (appState.filters = { ...appState.filters, rule: event.ruleLabel, ruleRegex: false })}
      title="Filter to rule: {event.ruleLabel}"
    >
      {event.ruleLabel}
    </button>
  {:else}
    <span class="cell rule">—</span>
  {/if}
</div>

<style>
  .row {
    display: contents;
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

  .rule {
    font-family: var(--font-mono);
    color: var(--fg-muted);
  }

  .iface {
    color: var(--fg-muted);
    font-size: 13px;
  }

  /* Reset button chrome on click-to-filter cells so they read exactly
     like the plain-text cells they replace -- only a hover underline
     hints they're interactive. */
  .cell-btn {
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

  /* Address cells hold the IP filter button and (for public IPs) the
     investigate trigger side by side -- overflow/ellipsis moves from
     `.cell` (which now just lays the two out) onto the filter button
     itself, since that's the element with the actual long text. Port is
     its own column now (see .port below), not crammed in here. */
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

  /* Same side-by-side shape as .cell.addr above (filter button + optional
     investigate trigger), but right-aligned since port numbers are
     numeric. */
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
</style>
