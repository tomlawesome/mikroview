// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import type { Stats } from './lib/types'

const FAKE_STATS: Stats = {
  total: 0,
  byAction: {},
  topRules: [],
  timeSeries: [],
  eventsPerSecond: 0,
  capacity: 0,
  count: 0,
  windowSeconds: 0,
  oldestHeld: null,
  connectedClients: 0,
}

// App.svelte's mount effect used to gate the watchlist coverage refresh
// (both the immediate first-paint call and the interval beside it) on
// authState.role === 'admin' -- stale twice over (#756): the Watchlist
// row it feeds is canEdit-visible (navGroups.ts's edit: true), not
// admin-only, and GET /api/definitions is accessViewer (#653), not
// admin-only either. This mounts the real App with a user-tier session
// and proves watchlistState actually ends up populated, not merely that
// nothing throws.
//
// This union of stubs mirrors Deck.svelte.test.ts's own comment: App
// mounts Deck (and everything Deck mounts) for whichever card is active,
// so it needs the same coverage that test already established, plus
// fetchAuthSession/fetchEvents/fetchDevices/fetchStats for App's own
// mount effect (authState.check(), appState.loadInitial()).
vi.mock('./lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./lib/api')>()
  return {
    ...actual,
    fetchAuthSession: vi.fn(async () => ({
      authenticated: true,
      setupRequired: false,
      ssoAvailable: false,
      username: 'watcher',
      role: 'user',
    })),
    fetchEvents: vi.fn(async () => ({ events: [] })),
    fetchDevices: vi.fn(async () => []),
    fetchStats: vi.fn(async () => ({})),
    fetchEventsWindow: vi.fn(async () => []),
    fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
      clearAllFlags: vi.fn(),
      setFlagVerdict: vi.fn(),
    deleteFlagVerdict: vi.fn(),
    fetchFlagEpisode: vi.fn(),
        fetchWatchlistEntries: vi.fn(async () => ({
      entries: [{ id: 'e1', name: 'watch 1', enabled: true, createdAt: '2026-01-01T00:00:00Z' }],
      coverage: { e1: 'covered' },
    })),
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
  }
})

// App.svelte connects a real WebSocket on mount -- jsdom has no server
// to answer it, so the module is replaced wholesale rather than let it
// spin up doomed connection attempts during the test.
// onChange returns its own unregister function, same as the real one --
// App.svelte calls it on teardown, and a stand-in returning undefined
// turns a clean unmount into a TypeError.
vi.mock('./lib/ws', () => ({
  liveSocket: { connect: vi.fn(), disconnect: vi.fn(), onChange: vi.fn(() => vi.fn()) },
}))

// jsdom has neither matchMedia nor ResizeObserver nor
// IntersectionObserver -- Deck (mounted by App for whichever card is
// active) and several scenes' viewport-aware bits need stand-ins to
// render at all. Mirrors Deck.svelte.test.ts's own stubs.
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

if (typeof IntersectionObserver === 'undefined') {
  class FakeIntersectionObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
}

const { default: App } = await import('./App.svelte')
const { authState } = await import('./lib/auth.svelte')
const { watchlistState } = await import('./lib/watchlist.svelte')
const { appState } = await import('./lib/state.svelte')
const { fetchWatchlistEntries, fetchStats, fetchFlags, ApiError } = await import('./lib/api')

describe('App mount effect: watchlist coverage refresh gate (#756)', () => {
  beforeEach(() => {
    vi.mocked(fetchWatchlistEntries).mockClear()
    authState.state = 'loading'
    authState.role = ''
    watchlistState.entries = []
    watchlistState.coverage = {}
  })

  it('populates the ring for a user-tier session, not just admin', async () => {
    render(App)

    // authState.check() (fetchAuthSession) and watchlistState.refresh()
    // (fetchWatchlistEntries) both resolve asynchronously off the mount
    // effects above -- wait for the session to land as 'user' before
    // asserting on what it triggered.
    await vi.waitFor(() => {
      expect(authState.state).toBe('authenticated')
      expect(authState.role).toBe('user')
    })

    await vi.waitFor(() => {
      expect(fetchWatchlistEntries).toHaveBeenCalled()
    })

    await vi.waitFor(() => {
      expect(watchlistState.entries.length).toBeGreaterThan(0)
      expect(watchlistState.coverage.e1).toBe('covered')
    })
  })
})

// #1089: a background poll failing used to be swallowed entirely unless it
// was a 401 -- a backend outage left stale numbers on screen with no
// indication. App.svelte's handleApiError now tags every non-401 poll
// failure with what it was refreshing and stores it as appState.refreshError
// (ConnectionBanner.svelte.test.ts covers the banner this feeds); the next
// successful poll of any kind clears it back to null.
describe('App mount effect: background poll failure surfaces refreshError (#1089)', () => {
  beforeEach(() => {
    authState.state = 'loading'
    authState.role = ''
    appState.refreshError = null
    vi.mocked(fetchStats).mockReset().mockResolvedValue(FAKE_STATS)
    vi.mocked(fetchFlags).mockReset().mockResolvedValue({ flags: [], timeSeries: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('sets refreshError on a 500 and clears it on the next successful poll', async () => {
    vi.useFakeTimers()
    render(App)

    // Flush the mount effect's own microtasks (authState.check() and
    // appState.loadInitial() both resolve off mocked, non-delayed
    // promises) without a real setTimeout-based waitFor, which fake
    // timers would otherwise stall.
    await vi.advanceTimersByTimeAsync(0)
    expect(authState.state).toBe('authenticated')

    // Both stats and flags fail on the same tick (rather than just one)
    // so the assertion below can't land on a false negative from racing
    // against the other poll's same-tick success clearing the error --
    // see pollStats/pollFlags's shared "any success clears it" rule in
    // App.svelte.
    vi.mocked(fetchStats).mockRejectedValueOnce(new ApiError('fetchStats: 500', 500))
    vi.mocked(fetchFlags).mockRejectedValueOnce(new ApiError('fetchFlags: 500', 500))

    // STATS_REFRESH_MS in App.svelte -- the interval driving both polls.
    await vi.advanceTimersByTimeAsync(5000)

    expect(appState.refreshError).toMatch(/^(stats|flags): /)

    // Next tick: both mocks are back to their default resolved value.
    await vi.advanceTimersByTimeAsync(5000)

    expect(appState.refreshError).toBeNull()
  })

  it('routes a 401 to the login bounce instead of refreshError', async () => {
    vi.useFakeTimers()
    render(App)
    await vi.advanceTimersByTimeAsync(0)
    expect(authState.state).toBe('authenticated')

    vi.mocked(fetchStats).mockRejectedValueOnce(new ApiError('fetchStats: 401', 401))

    await vi.advanceTimersByTimeAsync(5000)

    expect(appState.refreshError).toBeNull()
    expect(authState.state).toBe('unauthenticated')
  })
})
