// SPDX-License-Identifier: AGPL-3.0-only
//
// #805: JourneyTour.svelte's measure() reads each ring's `selector`
// straight off the live render (getBoundingClientRect(), every frame)
// and falls back to a hand-placed percentage the moment the selector
// matches nothing -- the exact defect #750 group A set out to remove.
// Nothing repeatable checked that the selectors in lib/tourHighlights.ts
// still match anything, so a renamed class degrades silently.
//
// jsdom cannot prove a ring measures the *right* box (no layout, so
// getBoundingClientRect() is always zero) -- the issue's own option 1,
// a live-check scenario that drives a freshly provisioned instance so
// the tour actually fires, is the only way to prove that, and needs a
// harness piece (a zero-account instance) that does not exist yet. This
// takes option 2 instead, the cheap guard the issue recommends trying
// first: prove every selector TOUR_HIGHLIGHTS carries resolves to
// exactly one real element on the card it names, scoped exactly the way
// the tour scopes it (tourHighlights.ts's own comment: a bare
// `span.switch` matches both metrics and the docket, so every selector
// is scoped to its own `.card[data-card="..."]`). That catches a
// renamed selector -- the failure that actually degrades a ring -- even
// though it would not catch a ring measuring the wrong-but-present
// element.
//
// The selector list is read from TOUR_HIGHLIGHTS itself, never copied
// into this file, so a new tour step is covered the moment it's added.
//
// Each card is mounted into a target div carrying the same
// `.card[data-card]` wrapper Deck.svelte's own <section> carries, using
// whichever of Deck's real pieces (SceneBar and/or the card's own scene
// component) actually renders the highlighted element -- not the whole
// deck -- reusing each piece's own component test file's mocks so the
// render needs nothing the real thing wouldn't have.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { RouterFilterRule } from '../lib/api'
import type { Entity, RuleUsage } from '../lib/types'

// Union of what Fall.svelte.test.ts, Entities.svelte.test.ts,
// EngineRoom.svelte.test.ts and Docket.svelte.test.ts each already
// mock to render their own piece under jsdom without reaching the
// network. Topography, Metrics' SceneBar switcher, and the stream's
// own Whisper/FilterBar/LiveTable read only their stores directly and
// need nothing here (see those components' own test files).
vi.mock('../lib/api', () => ({
  fetchDevices: vi.fn(async () => []),
  fetchRouterRules: vi.fn(async () => ({ available: false, rules: [] })),
  fetchEventsWindow: vi.fn(async () => ({
    events: [],
    hasMore: false,
    windowStart: new Date(Date.now() - 15 * 60 * 1000).toISOString(),
    serverTime: new Date().toISOString(),
  })),
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  fetchStatsTops: vi.fn(async () => []),
  fetchEntities: vi.fn(async (): Promise<Entity[]> => []),
  upsertEntity: vi.fn(async (): Promise<string | null> => null),
  deleteEntity: vi.fn(),
  fetchDeviceMACs: vi.fn(async () => []),
  fetchRouterAddresses: vi.fn(async () => ({ available: false, rules: [] })),
  fetchRules: vi.fn(async (): Promise<RuleUsage[]> => []),
  fetchSetupStatus: vi.fn(async () => ({
    instance: { tlsEnabled: true, syslogPort: ':6514', hosts: [] },
    sources: [],
    devices: [],
    pushKinds: [],
  })),
  fetchSetupCommands: vi.fn(async () => ({
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: { commands: '', note: '' },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
  })),
  fetchDefinitions: vi.fn(async () => ({ definitions: [], coverageEvidence: { complete: true } })),
  updateDefinition: vi.fn(),
  fetchUsers: vi.fn(async () => []),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchTokens: vi.fn(async () => []),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  signOutEverywhere: vi.fn(async () => null),
  fetchPersistence: vi.fn(async () => ({ backend: 'file', dir: '/var/lib/mikroview' })),
  fetchHistorySettings: vi.fn(async () => ({
    keyed: true,
    enabled: true,
    days: 30,
    maxBytes: 1024 * 1024 * 1024,
    held: { days: 0, oldest: '', newest: '', bytes: 0 },
    capped: false,
    bytesPerDay: 0,
  })),
  setHistorySettings: vi.fn(),
  clearAllFlags: vi.fn(),
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchFlagEpisode: vi.fn(),
  fetchExpectations: vi.fn(async () => []),
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
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { fallState, type FallBoundary } from '../lib/fall.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { zonesState } from '../lib/zones.svelte'
import { tunnelsState } from '../lib/tunnels.svelte'
import { policyState } from '../lib/policy.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { altitudeStopState } from '../lib/altitudeStop.svelte'
import { entitiesState } from '../lib/entities.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { auditState } from '../lib/audit.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { usersState } from '../lib/users.svelte'
import { tokensState } from '../lib/tokens.svelte'
import { persistenceState } from '../lib/persistence.svelte'
import { emptyFilters } from '../lib/types'
import { TOUR_HIGHLIGHTS } from '../lib/tourHighlights'

// jsdom has neither matchMedia nor ResizeObserver -- SceneBar mounts
// AccountMenu (ThemeMenu pulls in lib/viewport.svelte.ts, whose
// ViewportState singleton calls matchMedia at module-load time) and
// Fall/Metrics measure their own box with `bind:clientWidth`, compiled
// to a ResizeObserver. Both polyfilled before the dynamic imports below
// -- static imports are hoisted ahead of any plain statement in this
// file, so a polyfill after a static import of these components would
// already be too late (the same fix Fall.svelte.test.ts, Whisper.svelte
// .test.ts and Docket.svelte.test.ts each needed on their own terms).
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

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

const { default: Fall } = await import('./Fall.svelte')
const { default: Topography } = await import('./Topography.svelte')
const { default: Metrics } = await import('./Metrics.svelte')
const { default: SceneBar } = await import('./SceneBar.svelte')
const { default: Whisper } = await import('./Whisper.svelte')
const { default: FilterBar } = await import('./FilterBar.svelte')
const { default: LiveTable } = await import('./LiveTable.svelte')
const { default: Docket } = await import('./Docket.svelte')
const { default: Entities } = await import('./Entities.svelte')
const { default: EngineRoom } = await import('./EngineRoom.svelte')

function boundary(overrides: Partial<FallBoundary> = {}): FallBoundary {
  return {
    key: 'forward|iot|bridge1',
    chain: 'forward',
    inInterface: 'iot',
    outInterface: 'bridge1',
    srcAddressList: 'iot',
    label: 'iot → bridge1',
    coverage: 'observed',
    epithet: '',
    ...overrides,
  }
}

// A target div standing in for Deck.svelte's own
// `<section class="card" data-card={card.key}>` -- carrying the same
// class and attribute a real card section carries, so a selector scoped
// to it (tourHighlights.ts's `.card[data-card="..."] ...`) finds the
// same element the real tour would. Appended to the body so Testing
// Library's own cleanup (registered against document.body) still tears
// it down between tests.
function cardTarget(key: string): HTMLElement {
  const el = document.createElement('section')
  el.className = 'card'
  el.dataset.card = key
  document.body.appendChild(el)
  return el
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

// Mounts exactly the piece(s) of Deck.svelte's own render that carry
// the card's highlighted element, into a `.card[data-card]` target --
// not the whole deck, since most of what Deck otherwise pulls in
// (the roll rail, the scroll/visibility observers) has nothing to do
// with what a ring measures.
const CARD_MOUNTERS: Record<string, (target: HTMLElement) => void> = {
  fall: (target) => {
    fallState.boundaries = [boundary()]
    fallState.loading = false
    fallState.error = null
    render(Fall, { target })
  },
  topography: (target) => {
    render(Topography, { target })
  },
  metrics: (target) => {
    render(SceneBar, { target, props: { scene: 'metrics' } })
    render(Metrics, { target })
  },
  live: (target) => {
    render(Whisper, { target })
    render(FilterBar, { target })
    render(LiveTable, { target })
  },
  docket: (target) => {
    render(SceneBar, { target, props: { scene: 'flags' } })
    render(Docket, { target })
  },
  entities: (target) => {
    render(Entities, { target })
  },
  engineroom: (target) => {
    render(EngineRoom, { target })
  },
}

beforeEach(() => {
  vi.clearAllMocks()

  appState.devices = []
  appState.events = []
  appState.filters = emptyFilters()
  appState.view = 'fall'
  appState.stats = {
    total: 0,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 0,
    capacity: 100000,
    count: 0,
    oldestHeld: null,
    windowSeconds: 72 * 3600,
    connectedClients: 0,
  }

  authState.role = 'admin'
  authState.state = 'authenticated'

  fallState.boundaries = []
  fallState.loading = true
  fallState.error = null

  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  zonesState.pushed = []
  tunnelsState.byDevice = new Map()
  policyState.edges = []
  policyState.byDevice = {}
  policyState.anyPushed = false
  coverageState.declarations = []
  topologyNavState.pendingFlagId = null
  topologyNavState.pendingWatchId = null
  topologyNavState.pendingDescend = null
  wizardState.open = false
  altitudeStopState.stop = 'city'

  entitiesState.list = []
  suggestState.candidates = []
  matchesState.reset()
  auditState.list = []
  auditState.hasMore = false

  detectorSettingsState.list = []
  usersState.list = []
  tokensState.list = []
  tokensState.justCreated = null
  persistenceState.info = null
})

// Every card TOUR_HIGHLIGHTS names, read off the tour's own definition
// rather than copied here -- a new tour step with a `selector` is
// covered the moment it's added to lib/tourHighlights.ts.
describe('JourneyTour rings resolve to a real element (#805)', () => {
  for (const [cardKey, highlights] of Object.entries(TOUR_HIGHLIGHTS)) {
    const selectors = highlights.filter((h) => h.selector).map((h) => h.selector as string)
    if (selectors.length === 0) continue

    describe(`the ${cardKey} card`, () => {
      it.each(selectors)('"%s" matches exactly one element', async (selector) => {
        const mount = CARD_MOUNTERS[cardKey]
        expect(mount, `no CARD_MOUNTERS entry for tour card "${cardKey}" -- add one alongside its TOUR_HIGHLIGHTS entry`).toBeDefined()

        const target = cardTarget(cardKey)
        mount(target)
        await settle()

        expect(document.querySelectorAll(selector)).toHaveLength(1)
      })
    })
  }
})
