// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Deck itself makes no requests, but mounting a card's scene pulls in
// whatever that scene needs -- this unions what Fall, Topography and
// Docket's own component tests already needed to render under jsdom
// without reaching for the network (see those files' own comments).
vi.mock('../lib/api', () => ({
  fetchEventsWindow: vi.fn(async () => []),
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearAllFlags: vi.fn(),
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchFlagEpisode: vi.fn(),
  fetchWatchlistEntries: vi.fn(async () => ({ entries: [], coverage: {} })),
  createWatchlistEntry: vi.fn(),
  updateWatchlistEntry: vi.fn(),
  deleteWatchlistEntry: vi.fn(),
  fetchWatchlistMatches: vi.fn(),
  fetchRecentMatches: vi.fn(async () => []),
  promoteWatchlistDestinations: vi.fn(),
  setWatchlistObserving: vi.fn(),
  fetchSuggestions: vi.fn(async () => []),
  acceptSuggestion: vi.fn(),
  hideSuggestion: vi.fn(),
  unhideSuggestion: vi.fn(),
  resetSuggestions: vi.fn(),
  fetchAuditLog: vi.fn(async () => ({ entries: [], hasMore: false })),
  fetchStatsTops: vi.fn(async () => []),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { auditState } from '../lib/audit.svelte'

// jsdom has neither matchMedia nor ResizeObserver nor
// IntersectionObserver -- the deck's own scroll/visibility observers
// and several scenes' viewport-aware bits need at least stand-ins to
// render at all. The IntersectionObserver stub is deliberately capable
// (tracks its callback+elements) so tests below can simulate a card
// scrolling into and out of view instead of only ever seeing the
// activeIndex-only baseline.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

if (typeof ResizeObserver === 'undefined') {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

type IOCallback = (entries: Partial<IntersectionObserverEntry>[]) => void

class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []
  callback: IOCallback
  options: IntersectionObserverInit | undefined
  elements: Element[] = []

  constructor(callback: IOCallback, options?: IntersectionObserverInit) {
    this.callback = callback
    // The deck builds two observers over the same cards and they are
    // not interchangeable: the 0.6 one decides where you have arrived,
    // the 0-threshold one only decides what mounts. Kept so a test can
    // ask for the one it means rather than counting instances.
    this.options = options
    FakeIntersectionObserver.instances.push(this)
  }

  observe(el: Element) {
    this.elements.push(el)
  }

  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)

const { default: Deck } = await import('./Deck.svelte')

// #690: Deck.svelte used to mount a card's scene as soon as it became a
// neighbour of the active card (within one index, always), not when it
// was actually visited. These render the real component tree (not just
// the extracted rule in lib/deckMount.test.ts) to prove the wiring
// itself, not just the rule in isolation.
describe('Deck scene mounting (#690)', () => {
  beforeEach(() => {
    FakeIntersectionObserver.instances = []
    authState.state = 'authenticated'
    authState.role = 'viewer'
    authState.username = 'kai'
    appState.view = 'topography'
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    auditState.list = []
    auditState.hasMore = false
  })

  it('does not mount a neighbouring card (the docket) merely because it sits next to the active one', async () => {
    // The card order is fall, topography, metrics, live, docket,
    // engineroom for a viewer -- with topography active, live (index 3)
    // is two away and docket (index 4) is three away, so neither
    // qualifies as a rolling neighbour; this exercises the same "never
    // adjacent enough alone" rule metrics (index 2, one away) tests
    // more directly below.
    render(Deck)
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('[data-card="docket"]')?.querySelector('.card-body')).toBeNull()
  })

  it('mounts the one-away neighbour only once the deck reports it actually visible, not merely for sitting next to the active card', async () => {
    render(Deck)
    await Promise.resolve()
    flushSync()

    // metrics is index 2, one away from topography's index 1 -- at
    // rest (nothing reported visible), its scene must stay unmounted.
    expect(document.querySelector('[data-card="metrics"]')?.querySelector('.card-body')).toBeNull()

    // Once the deck's own low-threshold observer reports metrics as
    // physically on screen (the roll carrying it into view), its scene
    // mounts -- this is what keeps the roll from blanking mid-transit.
    const mountObserver = FakeIntersectionObserver.instances.at(-1)
    const metricsSection = document.querySelector('[data-card="metrics"]') as Element
    mountObserver?.callback([{ target: metricsSection, isIntersecting: true }])
    flushSync()
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('[data-card="metrics"]')?.querySelector('.card-body')?.childElementCount).toBeGreaterThan(0)
  })
})

// #1033: the map is loaded on demand so its weight leaves the entry
// bundle. The card must still never read as blank -- it opens on the
// chrome's ghost rows and swaps the map in when the chunk lands.
describe('the map loads on demand (#1033)', () => {
  beforeEach(() => {
    FakeIntersectionObserver.instances = []
    authState.state = 'authenticated'
    authState.role = 'viewer'
    authState.username = 'kai'
    appState.view = 'topography'
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    auditState.list = []
    auditState.hasMore = false
  })

  it('shows the ghost rows while the map chunk is in flight, then draws the map', async () => {
    render(Deck)
    flushSync()

    const body = () => document.querySelector('[data-card="topography"]')?.querySelector('.card-body')

    // Mounted, not blank: the placeholder stands in for the map rather
    // than the card body being empty until the import resolves.
    expect(body()).not.toBeNull()
    expect(body()?.querySelector('.ghost-rows')).not.toBeNull()

    await vi.waitFor(
      () => {
        flushSync()
        expect(body()?.querySelector('.ghost-rows')).toBeNull()
      },
      { timeout: 20_000 },
    )

    expect(body()?.childElementCount).toBeGreaterThan(0)
  }, 30_000)
})

// #1049: the deck's "you have arrived" observer used to be fenced off
// from its own programmatic roll by a 700ms timer. Both halves of that
// guess fail on a loaded host -- see each test for which -- and the
// consequence is the same either way: a card the roll was only passing
// becomes appState.view, the roll effect obediently rolls back to it,
// and the deck parks a card or two short of where the operator clicked.
// On the gate that was `goTo("Stream") timed out ... offsetFromDeckTop:
// -687`, three times, with the Stream card present and mounted.
describe('a roll the deck is only passing through never becomes the view (#1049)', () => {
  const CARD_H = 720

  beforeEach(() => {
    FakeIntersectionObserver.instances = []
    authState.state = 'authenticated'
    authState.role = 'viewer'
    authState.username = 'kai'
    appState.view = 'topography'
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    auditState.list = []
    auditState.hasMore = false
  })

  /**
   * jsdom lays nothing out, so the deck is given the geometry a real one
   * has: full-height cards stacked in a scroller whose scrollTop the
   * test moves by hand. scrollTo records the roll and deliberately does
   * NOT move the deck -- a roll is in flight until the test says the
   * scroll ended, which is the whole subject here.
   */
  function layOutDeck() {
    const deck = document.querySelector('.deck') as HTMLElement
    const sections = [...document.querySelectorAll('.card')] as HTMLElement[]
    let scrollTop = 0
    const rolls: number[] = []
    Object.defineProperty(deck, 'scrollTop', {
      configurable: true,
      get: () => scrollTop,
      set: (v: number) => (scrollTop = v),
    })
    deck.getBoundingClientRect = () => ({ top: 0, height: CARD_H }) as DOMRect
    sections.forEach((el, i) => {
      el.getBoundingClientRect = () => ({ top: i * CARD_H - scrollTop, height: CARD_H }) as DOMRect
    })
    deck.scrollTo = ((opts: ScrollToOptions) => rolls.push(opts.top ?? 0)) as HTMLElement['scrollTo']
    return {
      el: deck,
      rolls,
      sections,
      card: (key: string) => sections.find((el) => el.dataset.card === key) as HTMLElement,
      indexOf: (key: string) => sections.findIndex((el) => el.dataset.card === key),
      park: (key: string) => {
        scrollTop = sections.findIndex((el) => el.dataset.card === key) * CARD_H
      },
    }
  }

  /** The 0.6 observer -- the one whose entries become appState.view. */
  function arrivalObserver() {
    return FakeIntersectionObserver.instances.filter((o) => o.options?.threshold === 0.6).at(-1)
  }

  /**
   * The rail click an operator makes, and the roll it starts. Returns
   * the moment the roll was under way, for dating an observation to it.
   */
  async function clickRail(name: string) {
    const button = [...document.querySelectorAll('.roll-rail button.rail-name')].find(
      (b) => b.textContent?.trim() === name,
    ) as HTMLElement
    await fireEvent.click(button)
    flushSync()
    return performance.now()
  }

  /**
   * Long enough that the guard this replaced (a flat 700ms timer) has
   * certainly expired, and far short of the backstop that now stands
   * behind scrollend. Not a wait for anything to settle: the point of
   * each test below is what happens *after* that guess has run out.
   */
  const pastTheOldGuess = () => new Promise((r) => setTimeout(r, 750))

  it('ignores an observation the roll took on its way past, however late it is delivered', async () => {
    render(Deck)
    flushSync()
    const deck = layOutDeck()
    deck.park('topography')

    // The operator clicks Stream: two cards on, so the roll passes
    // metrics on the way.
    const duringTheRoll = await clickRail('Stream')
    expect(appState.view).toBe('live')
    expect(deck.rolls.at(-1)).toBe(deck.indexOf('live') * CARD_H)

    // The roll finishes and the deck comes to rest on Stream.
    await pastTheOldGuess()
    deck.el.scrollTop = deck.indexOf('live') * CARD_H
    deck.el.dispatchEvent(new Event('scrollend'))

    // Only now does the browser deliver what it saw 30ms into the roll:
    // metrics, briefly more than 60% of the way across the deck. This
    // is the gate's failure -- the sample is old, the callback is not.
    arrivalObserver()?.callback([
      { target: deck.card('metrics'), isIntersecting: true, time: duringTheRoll },
    ])
    flushSync()

    expect(appState.view).toBe('live')
    // ...and no second roll was started to chase it.
    expect(deck.rolls).toEqual([deck.indexOf('live') * CARD_H])
  })

  it('keeps ignoring transits when the roll itself outlasts the old 700ms guess', async () => {
    render(Deck)
    flushSync()
    const deck = layOutDeck()
    deck.park('topography')

    await clickRail('Stream')
    expect(appState.view).toBe('live')

    // No scrollend: the deck is still moving, as it is on a host whose
    // main thread stalled mid-roll and stretched it in wall-clock time.
    await pastTheOldGuess()
    arrivalObserver()?.callback([
      { target: deck.card('metrics'), isIntersecting: true, time: performance.now() },
    ])
    flushSync()

    expect(appState.view).toBe('live')
    expect(deck.rolls).toEqual([deck.indexOf('live') * CARD_H])
  })

  it('still follows an ordinary scroll once the roll is over', async () => {
    render(Deck)
    flushSync()
    const deck = layOutDeck()
    deck.park('topography')

    await clickRail('Stream')
    await pastTheOldGuess()
    deck.el.scrollTop = deck.indexOf('live') * CARD_H
    deck.el.dispatchEvent(new Event('scrollend'))

    // The operator wheels back up to metrics: sampled after the roll
    // ended, so it is theirs, not the deck's own.
    deck.el.scrollTop = deck.indexOf('metrics') * CARD_H
    arrivalObserver()?.callback([
      { target: deck.card('metrics'), isIntersecting: true, time: performance.now() },
    ])
    flushSync()

    expect(appState.view).toBe('metrics')
  })
})
