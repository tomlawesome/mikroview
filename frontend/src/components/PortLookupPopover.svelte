<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Single instance mounted once at the app root (see App.svelte) -- same
  // fixed-position-from-trigger-coordinates approach as IpLookupPopover.svelte,
  // driven by lib/portLookup.svelte.ts's shared singleton.
  import { portLookupState } from '../lib/portLookup.svelte'

  const POPOVER_WIDTH = 260

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) portLookupState.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') portLookupState.close()
  }

  $effect(() => {
    if (!portLookupState.anchor) return
    // Deferred past the current click's bubble phase, same reasoning as
    // IpLookupPopover.svelte -- the opening click would otherwise still be
    // bubbling up to `document` when this listener attaches.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  const style = $derived.by(() => {
    const a = portLookupState.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 60)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })
</script>

<svelte:window onkeydown={onKeydown} />

{#if portLookupState.anchor}
  <div
    bind:this={popoverEl}
    class="popover"
    {style}
    role="dialog"
    aria-label="Port lookup: {portLookupState.anchor.port}"
  >
    <div class="header">
      <span class="port">Port {portLookupState.anchor.port}</span>
      <button class="close" onclick={() => portLookupState.close()} aria-label="Close">✕</button>
    </div>

    {#if portLookupState.results.length === 0}
      <div class="status">No known common use for this port</div>
    {:else}
      <div class="entries">
        {#each portLookupState.results as r (r.name)}
          <div class="entry">
            <div class="entry-header">
              <span class="name">{r.name}</span>
              <span class="badge cat-{r.category}">{r.category}</span>
            </div>
            <div class="proto">{r.protocol}</div>
            <div class="desc">{r.description}</div>
          </div>
        {/each}
      </div>
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

  .port {
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

  .entries {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .entry + .entry {
    border-top: 1px solid var(--border);
    padding-top: 8px;
  }

  .entry-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }

  .name {
    color: var(--fg);
    font-weight: 600;
  }

  .badge {
    flex: none;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    padding: 1px 6px;
    border-radius: 4px;
    color: var(--fg-muted);
    border: 1px solid var(--border);
  }

  .badge.cat-suspicious {
    color: var(--reject);
    border-color: var(--reject);
  }

  .proto {
    font-family: var(--font-mono);
    color: var(--fg-dim);
    font-size: 11px;
    margin-top: 2px;
  }

  .desc {
    color: var(--fg-muted);
    margin-top: 4px;
    overflow-wrap: anywhere;
  }
</style>
