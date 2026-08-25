<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Appearance as a standalone, always-visible control (issue #137).
  //
  // This existed before #73 consolidated the toolbar into the nav menu
  // that #544 has since retired, and
  // burying it there was a regression rather than a tidy-up: theme and
  // colorway switching is reached for often and wants to be one click
  // away, not two. Reinstated in the pre-#73 shape, now also carrying
  // the light/dark/auto mode picker that was split out separately.
  //
  // At mobile widths the dropdown becomes a bottom sheet, for the same
  // reason the retired nav menu's did (issue #85): a right-anchored dropdown assumes
  // the trigger stays at the toolbar's right edge, and a wrapped toolbar
  // can put it anywhere.
  import { COLORWAYS, colorwayState } from '../lib/colorway.svelte'
  import { themeState, type ThemePref } from '../lib/theme.svelte'
  import { viewportState } from '../lib/viewport.svelte'

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

  const modeLabels: Record<ThemePref, string> = { system: 'Auto', light: 'Light', dark: 'Dark' }
  const modeOptions: ThemePref[] = ['system', 'light', 'dark']
</script>

<div class="theme-menu" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Colour theme and light/dark mode"
  >
    <span class="swatch" style="background: {current.swatch}"></span>
    Theme
  </button>

  {#if open}
    {#if viewportState.isMobile}
      <div class="scrim" onclick={() => (open = false)} role="presentation"></div>
    {/if}
    <div class="menu" class:mobile-sheet={viewportState.isMobile} role="menu">
      {#if viewportState.isMobile}
        <div class="handle"></div>
      {/if}

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

      <div class="divider"></div>

      {#each modeOptions as m (m)}
        <button
          class="option"
          class:active={m === themeState.pref}
          role="menuitemradio"
          aria-checked={m === themeState.pref}
          onclick={() => {
            themeState.pref = m
            themeState.apply()
            open = false
          }}
        >
          {modeLabels[m]}
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

  /* Same viewport-anchored treatment, scrim and z-order as the retired
     nav menu's mobile sheet -- one visual language for phone-width panels. */
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 30;
  }

  .menu.mobile-sheet {
    position: fixed;
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    min-width: 0;
    max-height: 80vh;
    overflow-y: auto;
    border-radius: 16px 16px 0 0;
    border-bottom: none;
    padding: 10px 10px calc(14px + env(safe-area-inset-bottom));
    z-index: 31;
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 8px;
    flex: none;
  }

  .divider {
    height: 1px;
    background: var(--border);
    margin: 5px 3px;
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

  /* 44px minimum touch target (issue #85). */
  @media (max-width: 700px) {
    .trigger,
    .option {
      min-height: 44px;
    }
  }
</style>
