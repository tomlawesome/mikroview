<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Saved filters (round 37, `#s5` `.fsaved`/`.fpmenu`): "saved filters
  // are the box's business". The trigger reads `saved ▾` and sits at the
  // filter box's own right end -- inside it, not beside it -- because a
  // saved filter is a filter, and the box is where the filter lives. It
  // opens a short list under the box in the account menu's dress: each
  // saved filter with an `×` to forget it, and, when there is a filter
  // set to save, `save this filter as…` at the foot.
  //
  // This component existed before round 30 and was never mounted: it
  // wore a bordered "Presets" button and a left-anchored menu of its
  // own, which round 29's ratified filter row drew nowhere, so #683
  // left it unmounted rather than invent a home for it. Round 37 drew
  // the home, and this is that -- the same presetState underneath, in
  // the drawn dress.
  import { appState } from '../lib/state.svelte'
  import { presetState } from '../lib/presets.svelte'
  import type { Filters } from '../lib/types'

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()
  let triggerEl: HTMLButtonElement | undefined = $state()

  // A click anywhere else closes it, matching AccountMenu. Bound only
  // while open, so a closed menu costs nothing. It never sees a click
  // inside this component at all -- the root's `contain` below stops
  // those before they reach the document, which is also what keeps them
  // from reaching the filter box's own "open the strip" handler.
  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  function onDocKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    open = false
    triggerEl?.focus()
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    document.addEventListener('keydown', onDocKeydown)
    return () => {
      document.removeEventListener('click', onDocClick)
      document.removeEventListener('keydown', onDocKeydown)
    }
  })

  // Every click in here is stopped from reaching the filter box's own
  // "click inside opens the strip" handler (FilterBar.svelte): reaching
  // for a saved filter is not reaching for the fields, and unfolding the
  // whole strip under the menu the moment it is opened reads as the
  // interface flinching.
  function contain(e: MouseEvent) {
    e.stopPropagation()
  }

  function toggle(e: MouseEvent) {
    contain(e)
    open = !open
  }

  function apply(filters: Filters) {
    appState.filters = { ...filters }
    open = false
  }

  function saveCurrent() {
    const name = window.prompt('Save this filter as:')
    if (name) presetState.save(name, appState.filters)
    open = false
  }

  function remove(e: MouseEvent, name: string) {
    e.stopPropagation()
    presetState.remove(name)
  }
</script>

<div class="saved" bind:this={rootEl} onclick={contain} role="presentation">
  <button
    type="button"
    class="fsaved"
    class:on={open}
    bind:this={triggerEl}
    onclick={toggle}
    aria-haspopup="menu"
    aria-expanded={open}
    title="Saved filters">saved ▾</button
  >

  {#if open}
    <div class="fpmenu" role="menu" aria-label="Saved filters">
      {#if presetState.presets.length > 0}
        <div class="mg">
          {#each presetState.presets as p (p.name)}
            <!-- The `×` is a real button nested in the row's own button,
                 which no browser allows -- so the row is the button and
                 the `×` is a sibling laid over its right end, exactly
                 what the drawing shows and the only shape that keeps
                 both reachable by keyboard. -->
            <div class="fprow">
              <button type="button" class="fpname" role="menuitem" onclick={() => apply(p.filters)}>
                {p.name}
              </button>
              <button
                type="button"
                class="fpx"
                onclick={(e) => remove(e, p.name)}
                title="forget this one"
                aria-label="Forget the saved filter {p.name}">×</button
              >
            </div>
          {/each}
        </div>
      {:else}
        <div class="mg">
          <p class="fpnone">No saved filters yet.</p>
        </div>
      {/if}

      <!-- Only with a filter set: "save this filter as…" with no filter
           to save is an offer the app cannot keep, and a disabled item
           would say the same thing more quietly. -->
      {#if appState.hasActiveFilters}
        <div class="mg">
          <button type="button" class="fpsave" role="menuitem" onclick={saveCurrent}>
            save this filter as…
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Anchors the menu to the box's right end, where the trigger sits. */
  .saved {
    position: relative;
    margin-left: auto;
    flex: none;
  }

  .fsaved {
    background: transparent;
    border: none;
    padding: 0;
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
    cursor: pointer;
    white-space: nowrap;
  }

  .fsaved:hover,
  .fsaved.on {
    color: var(--fg);
  }

  .fsaved:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* The account menu's dress (round 37: "a short list under it in the
     account menu's dress") -- the same elevated panel, border, radius,
     shadow and z-index AccountMenu.svelte uses, so the two read as one
     kind of thing rather than two menus that happen to be on one page. */
  .fpmenu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    min-width: 220px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 2px 0;
    z-index: 40;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
    cursor: default;
  }

  .mg {
    padding: 6px 0;
  }

  .mg + .mg {
    border-top: 1px solid var(--border);
  }

  .fprow {
    display: flex;
    align-items: baseline;
    gap: 18px;
  }

  .fpname,
  .fpsave {
    background: none;
    border: 0;
    text-align: left;
    cursor: pointer;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
    padding: 6px 14px;
  }

  .fpname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fpsave {
    width: 100%;
    font-family: var(--font-sans);
    font-size: 12px;
    color: var(--accent);
  }

  .fpname:hover,
  .fpsave:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .fpx {
    background: none;
    border: 0;
    cursor: pointer;
    font: 12px var(--font-mono);
    color: var(--fg-dim);
    padding: 6px 14px 6px 0;
  }

  .fpx:hover {
    color: var(--alarm);
  }

  .fpname:focus-visible,
  .fpsave:focus-visible,
  .fpx:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  /* An empty list still says what it is, rather than opening onto a
     bare "save this filter as…" with no explanation of what is missing. */
  .fpnone {
    margin: 0;
    padding: 6px 14px;
    font: 12px var(--font-mono);
    color: var(--fg-dim);
  }
</style>
