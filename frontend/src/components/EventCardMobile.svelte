<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // One event, mobile card layout (issue #85) -- the desktop LiveTable's
  // 12-column grid doesn't survive a phone-width squeeze without either
  // tiny unreadable text or horizontal scrolling per row, so below the
  // breakpoint LiveTable.svelte renders these instead. Design locked in
  // from a mockup review (see issue #85's comments): primary triage
  // info up front (time/action/NAT/rule, then the actual src->dst flow),
  // secondary detail (device/proto/interfaces) small and dim, everything
  // else pushed into EventDetailSheet.svelte on tap rather than shown
  // inline or expanded in place.
  import type { FirewallEvent } from '../lib/types'
  import { formatTime, countryFlag } from '../lib/format'

  let { event, deviceName, onOpen }: { event: FirewallEvent; deviceName: string; onOpen: () => void } =
    $props()

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const ifaces = $derived([event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—')
</script>

<button class="card row-{event.action}" onclick={onOpen} title={event.raw}>
  <div class="line1">
    <span class="time">{formatTime(event.time)}</span>
    <span class="badge badge-{event.action}">{event.action.toUpperCase()}</span>
    {#if event.natIp}
      <span class="badge badge-nat">NAT</span>
    {/if}
    {#if event.ruleName || event.ruleLabel}
      <span class="rule">{event.ruleName || event.ruleLabel}</span>
    {/if}
  </div>
  <div class="line2">
    <span class="addr">
      {#if event.srcIp}{srcFlag ? `${srcFlag} ` : ''}{event.srcHostName || event.srcIp}{#if event.srcPort}<span class="port">:{event.srcPortName || event.srcPort}</span>{/if}{:else}&mdash;{/if}
    </span>
    <span class="arrow">&rarr;</span>
    <span class="addr dst">
      {#if event.dstIp}{event.dstHostName || event.dstIp}{#if event.dstPort}<span class="port">:{event.dstPortName || event.dstPort}</span>{/if}{:else}&mdash;{/if}
    </span>
  </div>
  <div class="line3">
    <span>{deviceName}</span>
    {#if event.protocol}<span>{event.protocol}</span>{/if}
    <span>{ifaces}</span>
  </div>
</button>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 100%;
    padding: 9px 14px;
    border: none;
    border-bottom: 1px solid var(--border);
    background: none;
    text-align: left;
    font-family: inherit;
    /* 44px minimum touch target (issue #85's touch-sizing pass) --
       three short text lines don't reach it on their own. */
    min-height: 44px;
  }

  .row-accept {
    background: var(--row-accept-bg);
  }
  .row-drop {
    background: var(--row-drop-bg);
  }
  .row-reject {
    background: var(--row-reject-bg);
  }
  .row-log {
    background: var(--row-log-bg);
  }
  .row-unknown {
    background: var(--row-unknown-bg);
  }

  .line1 {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .time {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-dim);
  }

  .badge {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.03em;
    padding: 2px 7px;
    border-radius: 4px;
    line-height: 1.4;
  }

  .badge-accept {
    color: var(--accept);
    background: var(--accept-bg);
  }
  .badge-drop {
    color: var(--drop);
    background: var(--drop-bg);
  }
  .badge-reject {
    color: var(--reject);
    background: var(--reject-bg);
  }
  .badge-log {
    color: var(--log);
    background: var(--log-bg);
  }
  .badge-unknown {
    color: var(--unknown);
    background: var(--unknown-bg);
  }

  /* Same blue as the desktop NAT text/active-chrome tokens -- see
     issue #85's locked design ("same blue colour little NAT badge"). */
  .badge-nat {
    color: var(--accent);
    background: var(--accent-bg);
  }

  .rule {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
    max-width: 40%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .line2 {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: 14.5px;
    font-weight: 500;
    color: var(--fg);
  }

  .addr {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .dst {
    flex: 1;
  }

  .port {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
  }

  .arrow {
    color: var(--fg-dim);
    flex: none;
  }

  .line3 {
    display: flex;
    gap: 8px;
    font-size: 11.5px;
    font-family: var(--font-mono);
    color: var(--fg-dim);
  }
</style>
