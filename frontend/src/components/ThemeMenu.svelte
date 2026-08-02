<script lang="ts">
  import { COLORWAYS, colorwayState } from '../lib/colorway.svelte'

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

  const current = $derived(COLORWAYS.find((c) => c.id === colorwayState.pref) ?? COLORWAYS[0])
</script>

<div class="theme-menu" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Choose a color theme"
  >
    <span class="swatch" style="background: {current.swatch}"></span>
    Theme
  </button>

  {#if open}
    <div class="menu" role="menu">
      {#each COLORWAYS as c (c.id)}
        <button
          class="option"
          class:active={c.id === colorwayState.pref}
          role="menuitemradio"
          aria-checked={c.id === colorwayState.pref}
          onclick={() => {
            colorwayState.set(c.id)
            open = false
          }}
        >
          <span class="swatch" style="background: {c.swatch}"></span>
          {c.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .theme-menu {
    position: relative;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .swatch {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex: none;
  }

  .menu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    min-width: 150px;
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

  .option {
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    border: none;
    color: var(--fg-muted);
    padding: 7px 9px;
    border-radius: 5px;
    font-size: 13px;
    text-align: left;
    width: 100%;
  }

  .option:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .option.active {
    color: var(--fg);
    font-weight: 600;
  }
</style>
