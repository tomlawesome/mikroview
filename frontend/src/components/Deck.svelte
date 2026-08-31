<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The deck (#633, from the #634 rounds): the scenes are full-viewport
  // snap cards rolled vertically, and navigation between them is the
  // roll rail on the right edge -- the deck's names as sideways text,
  // clicking one rolls that card to centre. The ratified order is the
  // fall, topography, metrics, stream, the docket (flags · watchlist ·
  // audit as one card's tabs, rounds 17-19), then -- since #647 (round
  // 23) -- Entities and Settings as the deck's last two cards: seven for
  // an admin, six for a viewer (Entities keeps its own admin gate; see
  // deckCards.ts). Run setup… and the account's own actions are all
  // that is left on the account menu; every page-shaped operate surface
  // now lives here. Fleet alone stays off the deck, absorbed into the
  // Entities card (its "routers" section leads); the standalone Fleet
  // page still exists for the phone-width bottom bar, per its own file.
  import { SvelteSet } from 'svelte/reactivity'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { deckCards, type DeckCard } from '../lib/deckCards'
  import { deckOrderState } from '../lib/deckOrder.svelte'
  import { deckCardMounted } from '../lib/deckMount'
  import SceneBar from './SceneBar.svelte'
  import Fall from './Fall.svelte'
  import Metrics from './Metrics.svelte'
  import FilterBar from './FilterBar.svelte'
  import LiveTable from './LiveTable.svelte'
  import Whisper from './Whisper.svelte'
  import Docket from './Docket.svelte'
  import Topography from './Topography.svelte'
  import Entities from './Entities.svelte'
  import EngineRoom from './EngineRoom.svelte'

  // The card table lives in lib/deckCards.ts, shared with the Settings
  // shelf; the order is the operator's own (#633 rounds 23-25, drag to
  // reorder there), applied here so the deck rolls in the kept order.
  const cards = $derived(deckOrderState.apply(deckCards(authState.role === 'admin')))

  const activeIndex = $derived(cards.findIndex((c) => c.views.includes(appState.view)))

  // Only the visited card mounts its scene, plus whichever neighbour the
  // deck is physically rolling it into or out of view (#690): the
  // scenes were built for single-mount (Metrics polls, LiveTable
  // renders the buffer, the fall animates), so several mounted at once
  // multiplies that cost for nothing visible -- worst on the docket's
  // unvirtualised Flags list, which used to mount a card early and tear
  // down a card late for no reason but sitting next to the active one.
  // visibleKeys is kept by the low-threshold observer below; the rule
  // itself lives in lib/deckMount.ts so it's unit-testable without
  // mounting a component.
  let visibleKeys = new SvelteSet<string>()

  function mounted(i: number): boolean {
    return deckCardMounted(i, activeIndex, cards[i]?.key, visibleKeys)
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

  // Mount-only observer (#690): tracks which cards are actually on
  // screen, independent of the 0.6 "you've arrived" threshold above --
  // a low threshold plus a lookahead margin so a neighbour the roll is
  // carrying toward view mounts a little ahead of being visible (no
  // pop-in), but a card sitting untouched a full card away never enters
  // this set at all. Not gated on `rolling`: a programmatic roll should
  // mount whatever it's visibly passing through exactly like a wheel
  // scroll does.
  $effect(() => {
    void cards
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const key = (entry.target as HTMLElement).dataset.card
          if (!key) continue
          if (entry.isIntersecting) visibleKeys.add(key)
          else visibleKeys.delete(key)
        }
      },
      { root: deckEl, threshold: 0, rootMargin: '25% 0px' },
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
      {#if mounted(i)}
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
              <Whisper />
              <FilterBar />
              <LiveTable />
            {:else if card.key === 'docket'}
              <Docket />
            {:else if card.key === 'entities'}
              <Entities />
            {:else if card.key === 'engineroom'}
              <EngineRoom />
            {/if}
          </div>
        {/if}
      {/if}
    </section>
  {/each}
</div>

<!-- The roll rail: the deck's names as vertical sideways text hugging
     the right edge, top of the letters to the LEFT -- round 30's
     `.deckrail a { writing-mode: sideways-lr }`, ported field-for-field
     (the build had drawn `vertical-rl` here, rotating the letters the
     opposite way round). The in-view name grows and brightens in the
     same beat as the roll. -->
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
    /* #689: a positioning context, not just a clip. Without this, a
       descendant that is `position: absolute` with no offset of its own
       (an sr-only live region, say) falls back to its CSS "static
       position" -- computed from the full *unclipped* flow height of
       whatever comes before it, ignoring every overflow:hidden/auto
       ancestor on the way. With no positioned ancestor between here and
       <html>, that static position becomes real document coordinates,
       so a scene whose content wants to be much taller than the
       viewport (a chart, a long table) before it is clipped stretches
       document.scrollingElement.scrollHeight to match -- the deck's own
       rail stays fixed and visible while everything else scrolls away
       under it, exactly the "nothing but the rail" defect reported.
       This is the one wrapper every scene shares, so it is the one
       place to close the gap rather than chasing it per scene. */
    position: relative;
  }

  .card-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    /* #721: six reports across four scenes turned out to be one missing
       constraint (content crowding .roll-rail below), fixed per-scene by
       hand or not at all. Reserved here instead, once, for every card's
       content -- see app.css's --deck-rail-gutter for where its value
       comes from. Every scene's own component (Metrics*, LiveTable,
       Flags/Docket) fills this box with ordinary flow width, no
       `position: absolute` escaping to the card's own edge (see
       LiveTable.svelte's .table-wrap comment), so this padding reaches
       all of them without any of them needing their own copy. */
    padding: 0 var(--deck-rail-gutter, 36px) 14px 14px;
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
    writing-mode: sideways-lr;
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
