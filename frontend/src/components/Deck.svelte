<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The deck (#633, from the #634 rounds): the scenes are full-viewport
  // snap cards rolled vertically, and navigation between them is the
  // roll rail on the right edge -- the deck's names as sideways text,
  // clicking one rolls that card to centre. The ratified order is the
  // fall, topography, metrics, stream, the docket (flags · watchlist ·
  // audit as one card's tabs, rounds 17-19), then -- since #647 (round
  // 23) -- Entities and Settings as the deck's last two cards: seven
  // for the user/admin tiers, who can act on both (Entities and
  // Settings/engineroom carry deckCards.ts's `canEdit` gate). Run
  // setup… and the account's own actions are all that is left on the
  // account menu; every other page-shaped operate surface lives here.
  //
  // Fleet is the one exception. #647 folded its routers table into
  // Entities' own leading section, so it stays off the deck for the
  // user/admin tiers -- but #657's ratified matrix keeps Fleet itself
  // viewer-visible ("a stale router is why the log looks wrong") while
  // ruling Entities and Settings out of a viewer's navigation entirely.
  // A viewer therefore gets six cards, the last of them the standalone
  // Fleet page (frontend/src/components/Fleet.svelte) in place of
  // Entities and Settings -- reused rather than duplicated: it is the
  // same component the phone-width bottom bar has always reached.
  //
  // #1134 added Log every rule as the edit tier's eighth card. It used
  // to render outside the deck, which meant it had no navigation on it
  // at all -- the owner's ruling is the same shell as every other page,
  // and this is that shell.
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
  import GhostRows from './GhostRows.svelte'
  import Entities from './Entities.svelte'
  import EngineRoom from './EngineRoom.svelte'
  import Fleet from './Fleet.svelte'
  import LogEveryRule from './LogEveryRule.svelte'

  // The card table lives in lib/deckCards.ts, shared with the Settings
  // shelf; the order is the operator's own (#633 rounds 23-25, drag to
  // reorder there), applied here so the deck rolls in the kept order.
  const cards = $derived(deckOrderState.apply(deckCards(authState.isAdmin, authState.canEdit)))

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

  // #1033: Topography -- and the City it draws once the altitude slider
  // is past the city stop -- is far the heaviest scene on the deck, and
  // nothing the app opens with needs a byte of it. So it is imported
  // dynamically: Rollup emits it as its own chunk, and first paint stops
  // paying for a map most sessions never roll to. Nothing else imports
  // the component statically, which is what keeps it out of the entry
  // chunk; a static import anywhere reachable from main.ts would pull it
  // straight back in and this would buy nothing.
  //
  // The fetch is driven by the same mount rule as every other scene
  // above, so the deck's own 25% lookahead margin normally has the chunk
  // in hand before the card reaches the screen.
  type MapModule = typeof import('./Topography.svelte')
  let mapModule = $state<MapModule | undefined>(undefined)
  let mapFailed = $state(false)
  let mapLoading = false

  async function loadMap() {
    if (mapModule || mapLoading) return
    mapLoading = true
    mapFailed = false
    try {
      mapModule = await import('./Topography.svelte')
    } catch {
      // A chunk that never arrives -- offline, or a service worker
      // holding a precache that no longer matches the deployed build --
      // must say so and offer another go, not leave the card blank.
      mapFailed = true
    } finally {
      mapLoading = false
    }
  }

  $effect(() => {
    if (cards.some((c, i) => c.key === 'topography' && mounted(i))) void loadMap()
  })

  let deckEl: HTMLElement | undefined
  let cardEls: Record<string, HTMLElement> = {}
  // While a programmatic roll is in flight the observer sees every card
  // it passes; this flag keeps those transits from writing appState.view.
  let rolling = false
  // When the last roll finished. An observer entry carries `time` --
  // the moment its geometry was *sampled* -- and that is not the moment
  // its callback runs. Under load (four gate shards sharing one host) a
  // sample taken 30ms into a roll was delivered nearly a second later,
  // by which point the fixed 700ms timer this used to rely on had
  // already dropped `rolling`: a card the roll was merely passing
  // became the view, the effect below dutifully rolled back to it, and
  // the deck parked there -- a click on Stream leaving the operator on
  // Entities, and staying there. So a sample is judged on when it was
  // taken, never on when it arrived (#1049).
  let rollEndedAt = 0
  let rollTimer: ReturnType<typeof setTimeout> | undefined

  // The roll's own length is the browser's business: a stalled main
  // thread stretches it in wall-clock time, so a timer set to a guess
  // at the animation's length expires with the deck still moving. The
  // deck's scrollend is the event that actually says the roll is over,
  // and the operator's own wheel or finger says they have taken it over
  // -- either ends the roll. The timer is left only as a backstop for
  // an engine that fires no scrollend: latched forever, the deck would
  // stop following a wheel at all.
  const ROLL_BACKSTOP_MS = 3000

  function endRoll() {
    clearTimeout(rollTimer)
    if (!rolling) return
    rolling = false
    rollEndedAt = performance.now()
  }

  // What ends a roll, attached here rather than as onscrollend/onwheel
  // on the element itself: Svelte's a11y rule reads a touchstart
  // handler on a plain <div> as an unlabelled control, and the deck is
  // a scroller rather than a widget. Passive, since none of them does
  // anything but note that the roll is over.
  $effect(() => {
    const el = deckEl
    if (!el) return
    for (const type of ['scrollend', 'wheel', 'touchstart'])
      el.addEventListener(type, endRoll, { passive: true })
    return () => {
      for (const type of ['scrollend', 'wheel', 'touchstart']) el.removeEventListener(type, endRoll)
    }
  })

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
    rollTimer = setTimeout(endRoll, ROLL_BACKSTOP_MS)
  })

  // Wheel/touch scrolling marks the centred card as the view, so the
  // rail, deep links and the operate pages all agree on where you are.
  $effect(() => {
    void cards
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          // `rolling` drops what is sampled while a roll is in flight;
          // `rollEndedAt` drops what was sampled during one and only
          // delivered afterwards. Both are the same rule -- a card the
          // deck was rolling past is not a card anybody arrived at --
          // and it has to be applied per entry, since one batch can
          // carry samples from either side of the roll's end.
          if (rolling || entry.time < rollEndedAt) continue
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

<!-- The deck and its rail are two columns of one row (#1042): the rail
     used to be `position: fixed`, floating over every scene, which is
     why each surface then had to reserve its own clearance to stay out
     from under it -- and why an ingest-loss drawer, which is in flow,
     was drawn under the rail instead of pushing it down. In flow, the
     content column ends where the rail's begins and everything above
     the deck pushes both. -->
<div class="deck-shell">
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
                {#if mapModule}
                  {@const Topography = mapModule.default}
                  <Topography />
                {:else if mapFailed}
                  <div class="map-failed">
                    <p role="alert">The map could not be loaded.</p>
                    <button onclick={loadMap}>Try again</button>
                  </div>
                {:else}
                  <GhostRows label="Loading the map…" rows={4} />
                {/if}
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
              {:else if card.key === 'fleet'}
                <Fleet />
              {:else if card.key === 'log-every-rule'}
                <LogEveryRule />
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
</div>

<style>
  .deck-shell {
    flex: 1;
    display: flex;
    min-height: 0;
  }

  .deck {
    flex: 1;
    min-width: 0;
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
    /* An ordinary, even inset now the rail has a column of its own
       (#1042). The right side used to carry --deck-rail-gutter instead
       -- one reserved strip standing in for the space the fixed rail
       took out of every scene (#721) -- which the column makes
       unnecessary here and everywhere else that copied it. */
    padding: 0 14px 14px;
    min-height: 0;
  }

  /* #1033: the map chunk failing to arrive is the one state the deck
     cannot draw its way out of, so it gets a plain centred message and
     a retry rather than an empty card. */
  .map-failed {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--fg-muted);
    font-size: 13px;
  }

  .roll-rail {
    /* The deck's right-hand column, centred on the deck's own height
       rather than the window's -- so a banner or the ingest-loss drawer
       above the deck moves the rail down with the scenes instead of
       being covered by it.

       The width is fixed at the width this box already had, rather than
       left to its contents: the in-view name grows to 12.5px over
       0.35s, so a content-sized column would breathe by 2px through
       every roll and take the scene's whole layout with it. Nothing
       about the rail's own drawing changes -- 8 + 18 + 4 is the
       padding, the widest (in-view) name and the padding again. */
    flex: none;
    align-self: center;
    width: 30px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 18px;
    padding: 10px 4px 10px 8px;
    /* Its own stacking context, so the browser hit-tests the rail
       first and stops there. The pointer rests on the rail after every
       click on it, and the browser re-runs a hit test under the pointer
       each time the stream re-lays out; without this the test walks the
       whole deck first -- every row's hidden buttons and sticky time
       cells are separate paint layers -- at 12-20 ms a time (#1102), which
       turned each stream update into a long frame. The fixed rail this
       column replaced had its own layer for free. */
    isolation: isolate;
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
