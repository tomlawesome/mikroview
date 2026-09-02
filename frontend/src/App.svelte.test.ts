// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'

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
vi.mock('./lib/ws', () => ({
  liveSocket: { connect: vi.fn(), disconnect: vi.fn() },
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
const { fetchWatchlistEntries } = await import('./lib/api')

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
