<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Single instance mounted once at the app root (see App.svelte), driven
  // entirely by lib/ipLookup.svelte.ts's shared singleton -- fixed-
  // positioned from the trigger's own screen coordinates so it always
  // renders above LiveTable's scrolling body instead of getting clipped
  // by that container's overflow, which an absolutely-positioned popover
  // anchored inside the table would be.
  import { ipLookupState } from '../lib/ipLookup.svelte'
  import ReputationDetails from './ReputationDetails.svelte'

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
      <ReputationDetails result={ipLookupState.result} />
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
</style>
