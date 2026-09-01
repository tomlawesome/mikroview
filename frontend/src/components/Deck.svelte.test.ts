// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Deck itself makes no requests, but mounting a card's scene pulls in
// whatever that scene needs -- this unions what Fall, Topography and
// Docket's own component tests already needed to render under jsdom
// without reaching for the network (see those files' own comments).
vi.mock('../lib/api', () => ({
  fetchEventsWindow: vi.fn(async () => []),
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchFlagEpisode: vi.fn(),
  fetchExclusions: vi.fn(async () => []),
  removeExclusion: vi.fn(),
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
import { exclusionsState } from '../lib/exclusions.svelte'
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
  elements: Element[] = []

  constructor(callback: IOCallback) {
    this.callback = callback
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
    exclusionsState.list = []
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
