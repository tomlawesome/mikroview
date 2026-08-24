// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Watchlist.svelte itself makes no requests directly -- these stop the
// stores it pulls in (watchlistState, suggestState) from reaching for
// the network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchWatchlistEntries: vi.fn(async () => ({ entries: [], coverage: {} })),
  createWatchlistEntry: vi.fn(),
  updateWatchlistEntry: vi.fn(),
  deleteWatchlistEntry: vi.fn(),
  fetchWatchlistMatches: vi.fn(),
  promoteWatchlistDestinations: vi.fn(),
  setWatchlistObserving: vi.fn(),
  fetchSuggestions: vi.fn(async () => []),
  acceptSuggestion: vi.fn(),
  hideSuggestion: vi.fn(),
  unhideSuggestion: vi.fn(),
  resetSuggestions: vi.fn(),
}))

import { fetchSuggestions } from '../lib/api'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import type { Suggestion } from '../lib/types'
import Watchlist from './Watchlist.svelte'

function deviceSuggestion(id: string, name: string): Suggestion {
  return {
    id,
    kind: 'device',
    status: 'off',
    name,
    justification: 'named DHCP lease',
    routerDevice: 'router-1',
    source: { mac: 'aa:bb:cc:dd:ee:ff' },
    firstSeen: '2026-08-23T20:00:00Z',
    updatedAt: '2026-08-23T20:00:00Z',
  }
}

// #547: Suggestions folded into Watchlist as a tab, on the house
// tablist. Unlike Flags' Exclusions tab, no admin-gating logic lives in
// this component -- Watchlist only ever mounts for an admin at all (see
// navGroups.ts's `admin: true` on the Watchlist row), so both tabs are
// simply always present.
describe('Watchlist Suggestions tab (#547)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
  })

  it('renders a tablist with Watchlist and Suggestions tabs', () => {
    render(Watchlist)
    expect(screen.getByRole('tablist', { name: 'Watchlist views' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Watchlist' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Suggestions' })).toBeTruthy()
  })

  it('starts on the Watchlist panel with the Suggestions panel hidden', () => {
    render(Watchlist)
    const watchlistPanel = document.getElementById('panel-watchlist')
    const suggestionsPanel = document.getElementById('panel-suggestions')
    expect(watchlistPanel?.hasAttribute('hidden')).toBe(false)
    expect(suggestionsPanel?.hasAttribute('hidden')).toBe(true)
  })

  it('switching to the Suggestions tab shows its panel and the fetched candidates', async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([deviceSuggestion('s1', 'live-camera')])
    render(Watchlist)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('tab', { name: 'Suggestions' }))
    flushSync()

    const watchlistPanel = document.getElementById('panel-watchlist')
    const suggestionsPanel = document.getElementById('panel-suggestions')
    expect(watchlistPanel?.hasAttribute('hidden')).toBe(true)
    expect(suggestionsPanel?.hasAttribute('hidden')).toBe(false)
    expect(screen.getByText('live-camera')).toBeTruthy()
  })
})
