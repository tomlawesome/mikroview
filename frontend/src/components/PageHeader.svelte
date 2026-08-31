<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // #548's page-header chip: the ratified viewer grammar
  // (docs/design/screens/navigation/DESIGN.md, "States of the chrome" ->
  // Viewer) says read-only is declared *once*, in words, in the page
  // header -- not by disabling every control on the page, and not
  // repeated anywhere else. It names no tier since #653: with three of
  // them, "ADMINS EDIT" was wrong in both directions -- a user edits the
  // watchers station on Settings, and an admin edits everything -- and
  // the chip only ever renders for someone who cannot edit the page
  // anyway, so who can is not the useful half of the sentence.
  // `readOnly` is passed in by the page rather
  // than derived here from authState, so a page with nothing to gate
  // (Fleet -- see its own comment) can simply never pass it, instead of
  // this component showing a chip that names a distinction that page
  // doesn't have.
  //
  // `children` is the header slot the ratified metrics record (#488)
  // needs -- "three views of one data set, chosen in the page header".
  // Optional, so every page that already uses this renders exactly the
  // header it rendered before.
  import type { Snippet } from 'svelte'

  let {
    title,
    readOnly = false,
    children,
  }: { title: string; readOnly?: boolean; children?: Snippet } = $props()
</script>

<header class="page-header">
  <h2>{title}</h2>
  {#if readOnly}
    <span class="chip">READ-ONLY</span>
  {/if}
  {#if children}
    {@render children()}
  {/if}
</header>

<style>
  .page-header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .page-header h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--fg);
  }

  .chip {
    font-size: 10.5px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-muted);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 2px 9px;
  }
</style>
