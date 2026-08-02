<script lang="ts">
  import type { FirewallEvent } from '../lib/types'
  import { formatAddr, formatTime } from '../lib/format'
  import ActionBadge from './ActionBadge.svelte'

  let { event, deviceName }: { event: FirewallEvent; deviceName: string } = $props()

  const ifaces = $derived(
    [event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—',
  )
</script>

<div class="row row-{event.action}" title={event.raw}>
  <span class="cell time">{formatTime(event.time)}</span>
  <span class="cell device">{deviceName}</span>
  <span class="cell action"><ActionBadge action={event.action} /></span>
  <span class="cell chain">{event.chain || '—'}</span>
  <span class="cell addr">{formatAddr(event.srcIp, event.srcPort)}</span>
  <span class="cell addr">{formatAddr(event.dstIp, event.dstPort)}</span>
  <span class="cell proto">{event.protocol || '—'}</span>
  <span class="cell iface">{ifaces}</span>
  <span class="cell rule">{event.ruleLabel || '—'}</span>
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
</style>
