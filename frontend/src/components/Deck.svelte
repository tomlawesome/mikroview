<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The deck (#633, from the #634 rounds): the scenes are full-viewport
  // snap cards rolled vertically, and navigation between them is the
  // roll rail on the right edge -- the deck's names as sideways text,
  // clicking one rolls that card to centre. The ratified default order
  // is login -> the fall -> topography -> metrics -> stream; topography
  // and the docket are unbuilt, so today's deck is the fall, metrics,
  // stream, then flags and watchlist holding the docket's future slot.
  // Operate pages (engine room, fleet, entities, audit) are not cards:
  // they live on the account menu and render as pages over the deck.
  import { appState, type View } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import SceneBar from './SceneBar.svelte'
  import Fall from './Fall.svelte'
  import Metrics from './Metrics.svelte'
  import FilterBar from './FilterBar.svelte'
  import LiveTable from './LiveTable.svelte'
  import Flags from './Flags.svelte'
  import Watchlist from './Watchlist.svelte'

  type Card = { view: View; name: string }
  // Watchlist is admin-only throughout (#490's grammar: absent for
  // viewers, never disabled), so a viewer's deck simply has four cards.
  const cards = $derived.by((): Card[] => {
    const deck: Card[] = [
      { view: 'fall', name: 'The fall' },
      { view: 'metrics', name: 'Metrics' },
      { view: 'live', name: 'Stream' },
      { view: 'flags', name: 'Flags' },
    ]
    if (authState.role === 'admin') deck.push({ view: 'watchlist', name: 'Watchlist' })
    return deck
  })

  const activeIndex = $derived(cards.findIndex((c) => c.view === appState.view))

  // Only the centred card and its neighbours mount their scene: the
  // scenes were built for single-mount (Metrics polls, LiveTable
  // renders the buffer, the fall animates), and five of them running
  // off-screen would multiply that cost for nothing visible.
  function near(i: number): boolean {
    return Math.abs(i - activeIndex) <= 1
  }

  let deckEl: HTMLElement | undefined
  let cardEls: Record<string, HTMLElement> = {}
  // While a programmatic roll is in flight the observer sees every card
  // it passes; this flag keeps those transits from writing appState.view.
  let rolling = false
  let rollTimer: ReturnType<typeof setTimeout> | undefined

  function rollTo(view: View) {
    appState.view = view
  }

  // One effect owns the scroll position: any view change -- the rail,
  // the scene bar's flag badge, a deep link like openBoundaryInStream --
  // rolls its card to centre. The observer below is the other direction.
  $effect(() => {
    const el = cardEls[appState.view]
    if (!el || !deckEl) return
    if (Math.abs(el.offsetTop - deckEl.scrollTop) < 2) return
    rolling = true
    clearTimeout(rollTimer)
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: reduced ? 'auto' : 'smooth', block: 'start' })
    rollTimer = setTimeout(() => (rolling = false), 700)
  })

  // Wheel/touch scrolling marks the centred card as the view, so the
  // rail, deep links and the operate pages all agree on where you are.
  $effect(() => {
    void cards
    const observer = new IntersectionObserver(
      (entries) => {
        if (rolling) return
        for (const entry of entries) {
          if (!entry.isIntersecting) continue
          const view = (entry.target as HTMLElement).dataset.view as View
          if (view && appState.view !== view) appState.view = view
        }
      },
      { root: deckEl, threshold: 0.6 },
    )
    for (const el of Object.values(cardEls)) if (el) observer.observe(el)
    return () => observer.disconnect()
  })
</script>

<div class="deck" bind:this={deckEl}>
  {#each cards as card, i (card.view)}
    <section
      class="card"
      data-view={card.view}
      bind:this={cardEls[card.view]}
      aria-label={card.name}
      aria-hidden={card.view !== appState.view}
    >
      {#if near(i)}
        {#if card.view === 'fall'}
          <Fall />
        {:else}
          <SceneBar scene={card.view} />
          <div class="card-body">
            {#if card.view === 'metrics'}
              <Metrics />
            {:else if card.view === 'live'}
              <FilterBar />
              <LiveTable />
            {:else if card.view === 'flags'}
              <Flags />
            {:else if card.view === 'watchlist'}
              <Watchlist />
            {/if}
          </div>
        {/if}
      {/if}
    </section>
  {/each}
</div>

<!-- The roll rail: the deck's names as vertical sideways text hugging
     the right edge, top of the letters to the right (owner, #634 round
     11). The in-view name grows and brightens in the same beat as the
     roll. -->
<nav class="roll-rail" aria-label="The deck">
  {#each cards as card (card.view)}
    <button
      class="rail-name"
      class:on={card.view === appState.view}
      onclick={() => rollTo(card.view)}
      aria-current={card.view === appState.view ? 'page' : undefined}
    >
      {card.name}
    </button>
  {/each}
</nav>

<style>
  .deck {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scroll-snap-type: y mandatory;
    overscroll-behavior: contain;
  }

  .card {
    height: 100%;
    scroll-snap-align: start;
    scroll-snap-stop: always;
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }

  .card-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 0 14px 14px;
    min-height: 0;
  }

  .roll-rail {
    position: fixed;
    right: 0;
    top: 50%;
    transform: translateY(-50%);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 18px;
    padding: 10px 4px 10px 8px;
    z-index: 20;
  }

  .rail-name {
    writing-mode: vertical-rl;
    background: transparent;
    border: none;
    padding: 2px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--fg-dim);
    cursor: pointer;
    transition:
      font-size 0.35s,
      color 0.35s;
  }

  .rail-name:hover {
    color: var(--fg-muted);
  }

  .rail-name.on {
    color: var(--fg);
    font-size: 12.5px;
  }

  @media (prefers-reduced-motion: reduce) {
    .deck {
      scroll-behavior: auto;
    }
    .rail-name {
      transition: none;
    }
  }
</style>
