<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The deck (#633, from the #634 rounds): the scenes are full-viewport
  // snap cards rolled vertically, and navigation between them is the
  // roll rail on the right edge -- the deck's names as sideways text,
  // clicking one rolls that card to centre. The ratified default order
  // is login -> the fall -> topography -> metrics -> stream; topography
  // is unbuilt, so today's deck is the fall, metrics, stream, then the
  // docket (flags · watchlist · audit as one card's tabs, rounds 17-19).
  // Operate pages (settings, fleet, entities) are not cards: they live
  // on the account menu and render as pages over the deck.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { deckCards, type DeckCard } from '../lib/deckCards'
  import { deckOrderState } from '../lib/deckOrder.svelte'
  import SceneBar from './SceneBar.svelte'
  import Fall from './Fall.svelte'
  import Metrics from './Metrics.svelte'
  import FilterBar from './FilterBar.svelte'
  import LiveTable from './LiveTable.svelte'
  import Docket from './Docket.svelte'
  import Topography from './Topography.svelte'

  // The card table lives in lib/deckCards.ts, shared with the Settings
  // shelf; the order is the operator's own (#633 rounds 23-25, drag to
  // reorder there), applied here so the deck rolls in the kept order.
  const cards = $derived(deckOrderState.apply(deckCards(authState.role === 'admin')))

  const activeIndex = $derived(cards.findIndex((c) => c.views.includes(appState.view)))

  // Only the centred card and its neighbours mount their scene: the
  // scenes were built for single-mount (Metrics polls, LiveTable
  // renders the buffer, the fall animates), and several running
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

  function rollTo(card: DeckCard) {
    if (!card.views.includes(appState.view)) appState.view = card.views[0]
  }

  // One effect owns the scroll position: any view change -- the rail,
  // the scene bar's flag badge, a deep link like openBoundaryInStream --
  // rolls its card to centre. The observer below is the other direction.
  $effect(() => {
    const card = cards[activeIndex]
    const el = card && cardEls[card.key]
    if (!el || !deckEl) return
    // Scroll the deck alone, never the window: scrollIntoView walks
    // every scrollable ancestor, and during load (a banner briefly
    // holding height) that dragged the document itself down, clipping
    // the top bar once the banner collapsed.
    const top = el.getBoundingClientRect().top - deckEl.getBoundingClientRect().top + deckEl.scrollTop
    if (Math.abs(top - deckEl.scrollTop) < 2) return
    rolling = true
    clearTimeout(rollTimer)
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
    deckEl.scrollTo({ top, behavior: reduced ? 'auto' : 'smooth' })
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
          const key = (entry.target as HTMLElement).dataset.card
          const card = cards.find((c) => c.key === key)
          if (card && !card.views.includes(appState.view)) appState.view = card.views[0]
        }
      },
      { root: deckEl, threshold: 0.6 },
    )
    for (const el of Object.values(cardEls)) if (el) observer.observe(el)
    return () => observer.disconnect()
  })
</script>

<div class="deck" bind:this={deckEl}>
  {#each cards as card, i (card.key)}
    <section
      class="card"
      data-card={card.key}
      bind:this={cardEls[card.key]}
      aria-label={card.name}
      aria-hidden={i !== activeIndex}
    >
      {#if near(i)}
        {#if card.key === 'fall'}
          <Fall />
        {:else}
          <SceneBar scene={card.views.includes(appState.view) ? appState.view : card.views[0]} />
          <div class="card-body">
            {#if card.key === 'topography'}
              <Topography />
            {:else if card.key === 'metrics'}
              <Metrics />
            {:else if card.key === 'live'}
              <FilterBar />
              <LiveTable />
            {:else if card.key === 'docket'}
              <Docket />
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
  {#each cards as card, i (card.key)}
    <button
      class="rail-name"
      class:on={i === activeIndex}
      onclick={() => rollTo(card)}
      aria-current={i === activeIndex ? 'page' : undefined}
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
