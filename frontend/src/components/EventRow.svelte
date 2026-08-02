<script lang="ts">
  import type { FirewallEvent } from '../lib/types'
  import { countryFlag, formatAddr, formatTime } from '../lib/format'
  import { appState } from '../lib/state.svelte'
  import ActionBadge from './ActionBadge.svelte'

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
    <button
      class="cell addr cell-btn"
      title="Filter to IP: {event.srcIp}"
      onclick={() => appState.setFilter('ip', event.srcIp ?? '')}
    >
      {srcFlag ? `${srcFlag} ` : ''}{formatAddr(event.srcIp, event.srcPort)}
    </button>
  {:else}
    <span class="cell addr">—</span>
  {/if}

  {#if event.dstIp}
    <button
      class="cell addr cell-btn"
      title="Filter to IP: {event.dstIp}"
      onclick={() => appState.setFilter('ip', event.dstIp ?? '')}
    >
      {dstFlag ? `${dstFlag} ` : ''}{formatAddr(event.dstIp, event.dstPort)}
    </button>
  {:else}
    <span class="cell addr">—</span>
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

  .time,
  .addr,
  .proto {
    font-family: var(--font-mono);
    color: var(--fg-muted);
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
</style>
