<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The bar's alarm pair, ratified round 29 (#683), ported field-for-
  // field from the mockup's `.fmk`/`.wmk` markup (docs/design/concepts/
  // round-29/the-whole.html): "⚑ 6" (open flags, linking to the docket)
  // and the eye-shaped watch glyph + "7 ○1" (watchers held, rings
  // broken, linking to the watchlist) -- the watch marker is the
  // mockup's own small SVG, not a text stand-in for it. Shared between
  // SceneBar.svelte and Fall.svelte -- the two places that hand-roll a
  // scene bar -- so the two never drift apart. Both markers show even
  // at zero: the mockup's own "clear all" demo (the-whole.html's
  // cabtn handler) sets ⚑ to "⚑ 0" / "no open flags" as a real,
  // ok-coloured state, not a hidden one.
  import { appState } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
</script>

<button
  type="button"
  class="fmk"
  class:zero={flagsState.activeCount === 0}
  onclick={() => (appState.view = 'flags')}
  title={flagsState.activeCount === 0 ? 'no open flags' : `${flagsState.activeCount} open flags`}
>
  ⚑ {flagsState.activeCount}
</button>
<button
  type="button"
  class="wmk"
  onclick={() => (appState.view = 'watchlist')}
  title="{watchlistState.heldCount} watcher{watchlistState.heldCount === 1 ? '' : 's'} held{watchlistState.brokenCount > 0 ? `, ${watchlistState.brokenCount} broken` : ''}"
>
  <svg viewBox="0 0 14 10" width="13" height="10" aria-hidden="true">
    <path d="M1 5 Q7 -0.5 13 5 Q7 10.5 1 5 Z" fill="none" stroke="currentColor" stroke-width="1.1" />
    <circle cx="7" cy="5" r="1.8" fill="currentColor" />
  </svg>
  {watchlistState.heldCount}
  {#if watchlistState.brokenCount > 0}<b>○{watchlistState.brokenCount}</b>{/if}
</button>

<style>
  .fmk,
  .wmk {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    background: transparent;
    border: none;
    padding: 0;
    font: 600 12px var(--font-mono);
    cursor: pointer;
  }

  .fmk {
    color: var(--alarm);
  }

  .fmk.zero {
    color: var(--ok, var(--accept-tinted));
  }

  .wmk {
    color: var(--fg-muted);
  }

  .wmk b {
    color: var(--alarm);
    font-weight: 600;
  }

  .fmk:hover,
  .wmk:hover {
    text-decoration: underline;
  }
</style>
