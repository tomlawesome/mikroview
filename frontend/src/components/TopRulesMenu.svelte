<script lang="ts">
  import { appState } from '../lib/state.svelte'

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })

  const topRules = $derived(appState.stats?.topRules ?? [])
  const maxCount = $derived(topRules[0]?.count ?? 0)
</script>

<div class="top-rules" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    disabled={topRules.length === 0}
    aria-haspopup="true"
    aria-expanded={open}
    title="Most-triggered rules"
  >
    Top rules
  </button>

  {#if open}
    <div class="menu" role="menu">
      {#if topRules.length === 0}
        <div class="empty">No labeled rules seen yet</div>
      {:else}
        {#each topRules as r (r.rule)}
          <div class="row" role="menuitem">
            <span class="label">{r.rule}</span>
            <span class="bar-track">
              <span class="bar" style="width: {maxCount ? (r.count / maxCount) * 100 : 0}%"></span>
            </span>
            <span class="count">{r.count}</span>
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .top-rules {
    position: relative;
  }

  .trigger {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
  }

  .trigger:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .trigger:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .menu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    min-width: 240px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 20;
  }

  .empty {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 4px 2px;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 60px auto;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  .label {
    font-family: var(--font-mono);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .bar-track {
    height: 6px;
    border-radius: 3px;
    background: var(--bg-hover);
    overflow: hidden;
  }

  .bar {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: 3px;
  }

  .count {
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
    text-align: right;
  }
</style>
