<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Every scene's own bar, under the Atlas identity (owner, 2026-08-29:
  // pages are the site -- no persistent chrome). Carries the wordmark,
  // the scene's name, its strap, the live+rate reading, the flag/watch
  // markers and the account chip -- round 29's ratified content (#683),
  // ported field-for-field from docs/design/concepts/round-29/the-
  // whole.html rather than approximated. On the stream, also the active
  // filter as chips: the one piece of the retired toolbar #s5 actually
  // draws on the bar. Everything else the old toolbar held (eps,
  // buffer%, the max-age selector, Autoscroll/Pause/Group/Clear) is off
  // the bar -- the mockup's own `.scenebar` markup for #s5 does not draw
  // them, so they are gaps, recorded on the issue, not homed here.
  //
  // Inside the deck every card carries its own bar, so the scene named
  // here is the card's own, passed as a prop; outside the deck (the
  // operate pages) it defaults to the current view.
  import { appState, type View } from '../lib/state.svelte'
  import { buildFilterChips } from '../lib/filterChips'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import AlarmCluster from './AlarmCluster.svelte'
  import AccountMenu from './AccountMenu.svelte'

  let { scene = null }: { scene?: View | null } = $props()
  const view = $derived(scene ?? appState.view)

  const TITLES: Record<string, string> = {
    topography: 'Topography',
    live: 'Stream',
    metrics: 'Metrics',
    audit: 'The docket',
    flags: 'The docket',
    watchlist: 'The docket',
    engineroom: 'Settings',
    fleet: 'Fleet',
    entities: 'Entities',
  }

  // The page's own strap, in the page's voice -- taken verbatim from
  // round 29's mockup (docs/design/concepts/round-29/the-whole.html),
  // one per scene. Fleet carries none: it is outside the seven ratified
  // deck scenes (#633/#647's standalone phone-width page).
  const STRAPS: Record<string, string> = {
    topography: 'aggregates — click a card to descend',
    live: 'every line, as it arrived',
    metrics: 'the deep read',
    audit: 'what was flagged · what you watch · what changed',
    flags: 'what was flagged · what you watch · what changed',
    watchlist: 'what was flagged · what you watch · what changed',
    engineroom: 'the app, arranged your way',
    entities: 'the routers that push here, and the named things behind them',
  }

  const filterChips = $derived(buildFilterChips(appState.filters, appState.devices))
</script>

<div class="scene-bar">
  <span class="wm">MIKRO<em>VIEW</em></span>
  <span class="scname">
    <h1>{TITLES[view] ?? ''}</h1>
    {#if STRAPS[view]}<span class="epi">{STRAPS[view]}</span>{/if}
  </span>

  {#if view === 'live' && filterChips.length > 0}
    <!-- The stream's own control, exactly as #s5 draws it: one search-
         style box reading "label:<em>value</em> label:<em>value</em> ⌫"
         -- ported from `.scenebar .controls .search` in round 29's
         mockup (bordered box, accented values, single trailing ⌫), not
         approximated as separate chip pills. The mockup also draws a
         SPAN control here (15 m · 1 h · 24 h · 14 d, the same pattern
         as the fall's) -- left off pending a product decision, see the
         issue's gap list: the app has no existing capability those four
         buckets clearly belong to, and guessing one would be inventing
         behaviour, not styling it. -->
    <span class="search">
      {#each filterChips as chip, i (chip.key)}{i > 0 ? ' ' : ''}{chip.label}:<em>{chip.value}</em>{/each}
      <button
        type="button"
        class="chip-clear"
        onclick={() => appState.resetFilters()}
        title="Clear all filters"
        aria-label="Clear all filters"
      >
        ⌫
      </button>
    </span>
  {/if}

  <div class="status-cluster">
    <ConnectionIndicator showRate={view !== 'live'} />
    <AlarmCluster />
    <AccountMenu />
  </div>
</div>

<style>
  .scene-bar {
    display: flex;
    align-items: baseline;
    gap: 16px;
    padding: 12px 20px 4px;
    flex-wrap: wrap;
  }
  .wm {
    font-size: 13px;
    font-weight: 800;
    letter-spacing: 0.22em;
    color: var(--fg-dim);
  }
  .wm em {
    color: var(--accent);
    font-style: normal;
  }
  h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    color: var(--fg);
  }

  .scname {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .epi {
    font-weight: 400;
    color: var(--fg-dim);
    font-size: 12px;
  }

  .status-cluster {
    margin-left: auto;
    display: flex;
    gap: 16px;
    align-items: center;
  }

  /* Ported from round 29's `.controls .search` (the-whole.html):
     a single bordered box, not separate chip pills. */
  .search {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    max-width: 320px;
    padding: 5px 12px;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
  }
  .search em {
    font-style: normal;
    color: var(--accent);
  }
  .chip-clear {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 13px;
    cursor: pointer;
    padding: 0 2px;
    margin-left: 4px;
  }
  .chip-clear:hover {
    color: var(--alarm);
  }
</style>
