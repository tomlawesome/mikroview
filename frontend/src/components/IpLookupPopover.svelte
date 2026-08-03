<script lang="ts">
  // Single instance mounted once at the app root (see App.svelte), driven
  // entirely by lib/ipLookup.svelte.ts's shared singleton -- fixed-
  // positioned from the trigger's own screen coordinates so it always
  // renders above LiveTable's scrolling body instead of getting clipped
  // by that container's overflow, which an absolutely-positioned popover
  // anchored inside the table would be.
  import { ipLookupState } from '../lib/ipLookup.svelte'

  const POPOVER_WIDTH = 260

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) ipLookupState.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') ipLookupState.close()
  }

  $effect(() => {
    if (!ipLookupState.anchor) return
    // Deferred past the current click's bubble phase -- the click that
    // opened this (on IpInvestigateButton, a totally separate DOM subtree
    // from this popover) would otherwise still be bubbling up to
    // `document` when this listener attaches, immediately closing what it
    // just opened.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  const style = $derived.by(() => {
    const a = ipLookupState.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 60)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })

  const hasIntel = $derived.by(() => {
    const r = ipLookupState.result
    if (!r) return false
    return (
      r.abuseScore != null ||
      r.totalReports != null ||
      !!r.isp ||
      !!r.countryCode ||
      !!r.ports?.length ||
      !!r.hostnames?.length ||
      !!r.vulns?.length
    )
  })
</script>

<svelte:window onkeydown={onKeydown} />

{#if ipLookupState.anchor}
  <div
    bind:this={popoverEl}
    class="popover"
    {style}
    role="dialog"
    aria-label="IP investigation: {ipLookupState.anchor.ip}"
  >
    <div class="header">
      <span class="ip">{ipLookupState.anchor.ip}</span>
      <button class="close" onclick={() => ipLookupState.close()} aria-label="Close">✕</button>
    </div>

    {#if ipLookupState.loading}
      <div class="status">Looking up…</div>
    {:else if ipLookupState.error}
      <div class="status error">{ipLookupState.error}</div>
    {:else if ipLookupState.result}
      {@const r = ipLookupState.result}
      {#if !hasIntel}
        <div class="status">No intel found for this IP</div>
      {:else}
        <div class="rows">
          {#if r.abuseScore != null}
            <div class="row">
              <span class="label">Abuse score</span>
              <span class="value" class:high={r.abuseScore >= 50}>{r.abuseScore}/100</span>
            </div>
          {/if}
          {#if r.totalReports != null}
            <div class="row">
              <span class="label">Reports</span>
              <span class="value">{r.totalReports}</span>
            </div>
          {/if}
          {#if r.isp}
            <div class="row">
              <span class="label">ISP</span>
              <span class="value">{r.isp}</span>
            </div>
          {/if}
          {#if r.countryCode}
            <div class="row">
              <span class="label">Country</span>
              <span class="value">{r.countryCode}</span>
            </div>
          {/if}
          {#if r.ports?.length}
            <div class="row">
              <span class="label">Open ports</span>
              <span class="value">{r.ports.join(', ')}</span>
            </div>
          {/if}
          {#if r.hostnames?.length}
            <div class="row">
              <span class="label">Hostnames</span>
              <span class="value">{r.hostnames.join(', ')}</span>
            </div>
          {/if}
          {#if r.vulns?.length}
            <div class="row">
              <span class="label">Vulns</span>
              <span class="value">{r.vulns.join(', ')}</span>
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .popover {
    position: fixed;
    width: 260px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 10px 12px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 40;
    font-size: 13px;
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }

  .ip {
    font-family: var(--font-mono);
    font-weight: 600;
    color: var(--fg);
  }

  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 20px;
    height: 20px;
    font-size: 11px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .status {
    color: var(--fg-dim);
    padding: 4px 0;
  }

  .status.error {
    color: var(--reject);
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }

  .label {
    color: var(--fg-muted);
    flex: none;
  }

  .value {
    color: var(--fg);
    text-align: right;
    overflow-wrap: anywhere;
  }

  .value.high {
    color: var(--reject);
    font-weight: 600;
  }
</style>
