<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import ActionBadge from './ActionBadge.svelte'
  // Full-detail bottom sheet for one event, opened by tapping a card in
  // EventCardMobile.svelte -- chosen over expanding the card in place
  // (see issue #85's locked design decision) since it scales better if
  // more detail (e.g. the reputation/evidence data desktop's Flags view
  // already shows) needs to live here later, without redesigning the
  // card again.
  import { appState } from '../lib/state.svelte'
  import { formatTime, formatAddr, countryFlag, isPublicIp } from '../lib/format'
  import { lookupPort } from '../lib/commonPorts'
  import type { FirewallEvent } from '../lib/types'
  import IpInvestigateButton from './IpInvestigateButton.svelte'
  import PortInvestigateButton from './PortInvestigateButton.svelte'
  // #439: raw-value copy per field, mobile's answer to desktop's
  // hover-revealed CopyButton in EventRow.svelte. There's no hover here
  // (touch), so unlike EventRow's, these are just always shown -- see
  // CopyButton.svelte's own doc comment for why that's the default
  // rather than something this file has to turn back on.
  import CopyButton from './CopyButton.svelte'

  let { event, deviceName, onClose }: { event: FirewallEvent; deviceName: string; onClose: () => void } =
    $props()

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const dstFlag = $derived(countryFlag(event.dstCountry))

  // Mirrors EventRow.svelte's natFilterKey exactly -- see that file's
  // comment for why only the two dedicated NAT chains say which side was
  // translated.
  const natFilterKey = $derived(
    event.chain?.toLowerCase() === 'srcnat'
      ? 'srcQuery'
      : event.chain?.toLowerCase() === 'dstnat'
        ? 'dstQuery'
        : null,
  )

  // Same click-to-filter convention every other cell in the app uses
  // (see EventRow.svelte, Flags.svelte's target button) -- closes the
  // sheet too, since the live view behind it is about to change under
  // it anyway.
  function filterAndClose<K extends keyof typeof appState.filters>(key: K, value: (typeof appState.filters)[K]) {
    appState.setFilter(key, value)
    onClose()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="scrim" onclick={onClose} role="presentation"></div>
<div class="sheet" role="dialog" aria-modal="true" aria-label="Event detail">
  <div class="handle"></div>

  <div class="header">
    <ActionBadge action={event.action} />
    <span class="time">{formatTime(event.time)}</span>
    <button class="close" onclick={onClose} aria-label="Close">✕</button>
  </div>

  <p class="title">
    {#if srcFlag}
      <button
        class="flag-link"
        onclick={() => filterAndClose('srcCountry', event.srcCountry ?? '')}
        aria-label="Filter to source country: {event.srcCountry}"
      >{srcFlag}</button>
    {/if}
    {formatAddr(event.srcIp, event.srcPort)}
    <span class="arrow">→</span>
    {#if dstFlag}
      <button
        class="flag-link"
        onclick={() => filterAndClose('dstCountry', event.dstCountry ?? '')}
        aria-label="Filter to destination country: {event.dstCountry}"
      >{dstFlag}</button>
    {/if}
    {formatAddr(event.dstIp, event.dstPort)}
  </p>

  <div class="rows">
    <div class="row">
      <span class="k">Device</span>
      <span class="v-group">
        <button class="v link" onclick={() => filterAndClose('device', event.deviceId)}>{deviceName}</button>
        <CopyButton value={event.deviceId} label="device id" />
      </span>
    </div>
    {#if event.chain}
      <div class="row">
        <span class="k">Chain</span>
        <button class="v link" onclick={() => filterAndClose('chain', event.chain)}>{event.chain}</button>
      </div>
    {/if}
    {#if event.srcIp}
      <div class="row">
        <span class="k">Source</span>
        <span class="v-group">
          <button class="v link" onclick={() => filterAndClose('srcQuery', event.srcIp ?? '')}>
            {event.srcHostName || event.srcIp}
          </button>
          <CopyButton value={event.srcIp} label="source IP" />
          {#if isPublicIp(event.srcIp)}<IpInvestigateButton ip={event.srcIp} />{/if}
        </span>
      </div>
    {/if}
    {#if event.srcPort}
      <div class="row">
        <span class="k">Src port</span>
        <span class="v-group">
          <button class="v link" onclick={() => filterAndClose('port', String(event.srcPort))}>
            {event.srcPortName || event.srcPort}
          </button>
          <CopyButton value={String(event.srcPort)} label="source port" />
          {#if lookupPort(event.srcPort)}<PortInvestigateButton port={event.srcPort} />{/if}
        </span>
      </div>
    {/if}
    {#if event.dstIp}
      <div class="row">
        <span class="k">Destination</span>
        <span class="v-group">
          <button class="v link" onclick={() => filterAndClose('dstQuery', event.dstIp ?? '')}>
            {event.dstHostName || event.dstIp}
          </button>
          <CopyButton value={event.dstIp} label="destination IP" />
          {#if isPublicIp(event.dstIp)}<IpInvestigateButton ip={event.dstIp} />{/if}
        </span>
      </div>
    {/if}
    {#if event.dstPort}
      <div class="row">
        <span class="k">Dst port</span>
        <span class="v-group">
          <button class="v link" onclick={() => filterAndClose('port', String(event.dstPort))}>
            {event.dstPortName || event.dstPort}
          </button>
          <CopyButton value={String(event.dstPort)} label="destination port" />
          {#if lookupPort(event.dstPort)}<PortInvestigateButton port={event.dstPort} />{/if}
        </span>
      </div>
    {/if}
    {#if event.natIp}
      <div class="row">
        <span class="k">NAT</span>
        {#if natFilterKey}
          <button
            class="v accent link"
            onclick={() => {
              if (natFilterKey) filterAndClose(natFilterKey, event.natIp ?? '')
            }}
          >→ {formatAddr(event.natIp, event.natPort)}</button>
        {:else}
          <span class="v accent">→ {formatAddr(event.natIp, event.natPort)}</span>
        {/if}
      </div>
    {/if}
    {#if event.protocol}
      <div class="row">
        <span class="k">Protocol</span>
        <button class="v link" onclick={() => filterAndClose('protocol', event.protocol ?? '')}>{event.protocol}</button>
      </div>
    {/if}
    <div class="row">
      <span class="k">Interfaces</span>
      <span class="v-group">
        {#if event.inInterface}
          <button class="v link" onclick={() => filterAndClose('interface', event.inInterface ?? '')}>{event.inInterface}</button>
        {/if}
        {#if event.inInterface && event.outInterface}
          <span class="v arrow">→</span>
        {/if}
        {#if event.outInterface}
          <button class="v link" onclick={() => filterAndClose('interface', event.outInterface ?? '')}>{event.outInterface}</button>
        {/if}
        {#if !event.inInterface && !event.outInterface}<span class="v">—</span>{/if}
      </span>
    </div>
    {#if event.ruleLabel}
      <div class="row">
        <span class="k">Rule</span>
        <span class="v-group">
          <button
            class="v link"
            onclick={() => {
              appState.filters = { ...appState.filters, rule: event.ruleLabel, ruleRegex: false }
              onClose()
            }}
          >
            {event.ruleName || event.ruleLabel}
          </button>
          <CopyButton value={event.ruleLabel} label="rule label" />
        </span>
      </div>
    {/if}
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 30;
  }

  .sheet {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: 75vh;
    overflow-y: auto;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    border-radius: 16px 16px 0 0;
    padding: 10px 18px calc(22px + env(safe-area-inset-bottom));
    box-shadow: 0 -20px 50px rgba(0, 0, 0, 0.4);
    z-index: 31;
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 14px;
  }

  .header {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .time {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-dim);
  }

  .close {
    margin-left: auto;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 28px;
    height: 28px;
    font-size: 12px;
    /* 44px minimum touch target extends past the visible box via padding
       rather than growing the icon itself. */
    padding: 8px;
  }

  .title {
    margin: 10px 0 14px;
    font-family: var(--font-mono);
    font-size: 15px;
    font-weight: 600;
    color: var(--fg);
    overflow-wrap: anywhere;
  }

  .title .arrow {
    color: var(--fg-dim);
    margin: 0 2px;
  }

  /* #438: the country flag in the summary title is now a click-to-filter
     token like every other -- plain inline button, no border/background,
     so it still reads as part of the running text. */
  .flag-link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    cursor: pointer;
  }

  .flag-link:hover {
    text-decoration: underline;
  }

  .rows {
    display: flex;
    flex-direction: column;
  }

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    padding: 9px 0;
    border-bottom: 1px solid var(--border);
    font-size: 13.5px;
    /* 44px minimum touch target for the tappable rows below. */
    min-height: 26px;
  }

  .k {
    color: var(--fg-dim);
    flex: none;
  }

  .v {
    font-family: var(--font-mono);
    color: var(--fg);
    text-align: right;
    overflow-wrap: anywhere;
  }

  .v-group {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .v.accent {
    color: var(--accent);
    font-weight: 600;
  }

  button.v {
    background: none;
    border: none;
    padding: 6px 0;
    font-size: 13.5px;
  }

  button.v:hover,
  button.v:active {
    text-decoration: underline;
  }
</style>
