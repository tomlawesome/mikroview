// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Docket itself makes no requests directly, but its default pane is
// always Flags (appState.view defaults to 'fall', which the tab
// derivation falls back from) and its watchlist/audit panes are also
// pulled in statically -- these stop every store reached by any of the
// three tabs (flagsState, watchlistState, suggestState,
// matchesState, auditState) from reaching for the network when they
// initialise under jsdom. Same list Flags.svelte.test.ts and
// Watchlist.svelte.test.ts already needed, unioned.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearAllFlags: vi.fn(),
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchFlagEpisode: vi.fn(),
  // Flags.svelte's learning shelf (#640) fetches the expectations
  // ledger unconditionally on mount -- Docket embeds Flags, so this
  // mock needs it too even though these tests never look at the shelf.
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

import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { auditState } from '../lib/audit.svelte'
import { appState } from '../lib/state.svelte'
import { formatHM } from '../lib/format'
import type { Flag, Stats } from '../lib/types'

// jsdom has no window.matchMedia -- the default pane is always Flags
// (see the vi.mock comment above), which pulls in lib/viewport.svelte.ts
// and calls it at module-load time. Polyfilled before the dynamic import
// below (a static import would already have run this file's top-level
// code too late to matter) -- same fix Flags.svelte.test.ts needed.
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

const { default: Docket } = await import('./Docket.svelte')

function testFlag(overrides: Partial<Flag> = {}): Flag {
  return {
    id: 'f1',
    type: 'port_scan',
    target: '203.0.113.9',
    detail: 'd',
    count: 1,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    cleared: false,
    ...overrides,
  }
}

// #653's three tiers, as they land on the docket's own tab row: the
// watchlist tab is user-tier and above, the audit log tab stays
// admin-only, and the clear-all bubble (round 29) rides along with the
// watchlist gate since clearing is the same user-tier action.
describe('Docket tiers (#653)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    auditState.list = []
    auditState.hasMore = false
    appState.view = 'fall'
    authState.state = 'authenticated'
    authState.username = 'kai'
  })

  it('a viewer sees no watchlist tab and no audit log tab', async () => {
    authState.role = 'viewer'
    render(Docket)
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('tab', { name: /watchlist/ })).toBeNull()
    expect(screen.queryByRole('tab', { name: /audit log/ })).toBeNull()
  })

  // #700: the tab row moved to the scene bar, so the docket draws no
  // tabs of its own at any tier. The tier rule those two tests pinned
  // now lives in SceneBar.svelte.test.ts, where the tabs do.
  it('draws no tab row of its own, at any tier', async () => {
    for (const role of ['viewer', 'user', 'admin'] as const) {
      authState.role = role
      const { unmount } = render(Docket)
      await Promise.resolve()
      flushSync()

      expect(screen.queryAllByRole('tab')).toHaveLength(0)
      unmount()
    }
  })

  it('a viewer with open flags sees no clear-all bubble', async () => {
    authState.role = 'viewer'
    flagsState.list = [testFlag()]
    render(Docket)
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('button', { name: /clear all/ })).toBeNull()
  })

  it('a user with open flags sees the clear-all bubble', async () => {
    authState.role = 'user'
    flagsState.list = [testFlag(), testFlag({ id: 'f2' })]
    render(Docket)
    await Promise.resolve()
    flushSync()

    expect(screen.getByRole('button', { name: 'clear all' })).toBeTruthy()
  })

  it('an admin with open flags also sees the clear-all bubble', async () => {
    authState.role = 'admin'
    flagsState.list = [testFlag()]
    render(Docket)
    await Promise.resolve()
    flushSync()

    expect(screen.getByRole('button', { name: 'clear all' })).toBeTruthy()
  })

  it('a user with no open flags sees no clear-all bubble -- the bubble needs an active count, not just edit rights', async () => {
    authState.role = 'user'
    flagsState.list = []
    render(Docket)
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('button', { name: /clear all/ })).toBeNull()
  })
})

// ── The restart statement (#795, design round 41, scene #s7) ────────────
//
// The same sentence the metrics hourline says, as a dim chip in the
// clear-all row -- the fall's `<span class="att dim">` "statement, not a
// control" idiom (#801, round 36 item 6.1). The wording and the
// sixty-minute clearing boundary are pinned in lib/provenance.test.ts,
// which both surfaces read. What is the docket's own business, and
// pinned here, is that the chip is drawn, that it is drawn on *every*
// tab (every tab's contents came out of the same restarted process), and
// that it is a span among the buttons rather than one of them.
describe('the restart statement (#795)', () => {
  const minutesAgo = (n: number) => new Date(Date.now() - n * 60 * 1000).toISOString()

  function statsWith(extra: Partial<Stats>): Stats {
    return {
      total: 0,
      byAction: {},
      topRules: [],
      timeSeries: [],
      eventsPerSecond: 0,
      capacity: 100000,
      count: 0,
      windowSeconds: 3600,
      oldestHeld: null,
      connectedClients: 1,
      ...extra,
    }
  }

  beforeEach(() => {
    vi.resetAllMocks()
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    auditState.list = []
    auditState.hasMore = false
    appState.view = 'fall'
    appState.stats = null
    authState.state = 'authenticated'
    authState.username = 'kai'
    authState.role = 'admin'
  })

  afterEach(() => {
    // The store is a module singleton; leaving a warm restart in it would
    // put a chip in every other test's markup.
    appState.stats = null
  })

  async function draw() {
    const rendered = render(Docket)
    await Promise.resolve()
    flushSync()
    return rendered
  }

  const chip = (container: HTMLElement) => container.querySelector('.clear-row .att.dim')

  it('names both times after a warm restart', async () => {
    const liveSince = minutesAgo(34)
    const restoredTo = minutesAgo(38)
    appState.stats = statsWith({ liveSince, restoredTo })
    const { container } = await draw()

    expect(chip(container)?.textContent?.trim()).toBe(
      `restored to ${formatHM(restoredTo)} · live since ${formatHM(liveSince)}`,
    )
  })

  it('says nothing came before after a cold start', async () => {
    appState.stats = statsWith({ liveSince: minutesAgo(4) })
    const { container } = await draw()

    expect(chip(container)?.textContent?.trim()).toMatch(/^counting since \d\d:\d\d — nothing before$/)
  })

  it('is a statement, not a control', async () => {
    appState.stats = statsWith({ liveSince: minutesAgo(4), restoredTo: minutesAgo(8) })
    const { container } = await draw()

    const el = chip(container)!
    // There is nowhere for it to lead, so it is a span among the buttons.
    expect(el.tagName.toLowerCase()).toBe('span')
    expect(el.closest('button')).toBeNull()
    // The idiom's hollow circle, drawn by the empty <i> the fall uses.
    expect(el.querySelector('i')).not.toBeNull()
  })

  it('is drawn on every tab', async () => {
    appState.stats = statsWith({ liveSince: minutesAgo(4), restoredTo: minutesAgo(8) })
    for (const view of ['fall', 'watchlist', 'audit'] as const) {
      appState.view = view
      const { container, unmount } = await draw()
      expect(chip(container), `no statement on the ${view} tab`).not.toBeNull()
      unmount()
    }
  })

  it('has cleared an hour after the process came up', async () => {
    appState.stats = statsWith({ liveSince: minutesAgo(61), restoredTo: minutesAgo(65) })
    const { container } = await draw()

    expect(chip(container)).toBeNull()
    expect(container.textContent).not.toContain('restored to')
  })

  it('says nothing at all when the server sends no liveSince', async () => {
    appState.stats = statsWith({})
    const { container } = await draw()
    expect(chip(container)).toBeNull()
  })
})
