<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import { presetState } from '../lib/presets.svelte'
  import type { Filters } from '../lib/types'

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

  function apply(filters: Filters) {
    appState.filters = { ...filters }
    open = false
  }

  function saveCurrent() {
    const name = window.prompt('Save current filters as:')
    if (name) presetState.save(name, appState.filters)
  }

  function remove(e: MouseEvent, name: string) {
    e.stopPropagation()
    presetState.remove(name)
  }
</script>

<div class="presets" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Saved filter presets"
  >
    Presets
  </button>

  {#if open}
    <div class="menu" role="menu">
      <button class="save" onclick={saveCurrent} disabled={!appState.hasActiveFilters}>
        Save current filters…
      </button>

      {#if presetState.presets.length > 0}
        <div class="divider"></div>
        {#each presetState.presets as p (p.name)}
          <div class="row">
            <button class="option" role="menuitem" onclick={() => apply(p.filters)}>
              {p.name}
            </button>
            <button class="remove" onclick={(e) => remove(e, p.name)} title="Delete preset" aria-label="Delete preset {p.name}">
              ×
            </button>
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .presets {
    position: relative;
  }

  .trigger {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 14px;
    font-size: 14px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .menu {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    min-width: 220px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 5px;
    display: flex;
    flex-direction: column;
    gap: 1px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 20;
  }

  .divider {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }

  .save,
  .option {
    background: transparent;
    border: none;
    color: var(--fg-muted);
    padding: 7px 9px;
    border-radius: 5px;
    font-size: 13px;
    text-align: left;
    width: 100%;
  }

  .save:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .save:hover:not(:disabled),
  .option:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .row .option {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    padding: 4px 9px;
    font-size: 15px;
    line-height: 1;
    border-radius: 5px;
  }

  .remove:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }
</style>
