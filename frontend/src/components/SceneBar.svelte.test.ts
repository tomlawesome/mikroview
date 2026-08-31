// SPDX-License-Identifier: AGPL-3.0-only
//
// The scene bar to round 29's ratified content (#683):
// MIKROVIEW · <page> · <strap> · <page's own controls> · LIVE · rate ·
// ⚑ N · ◉ held ○ broken · account. Covers what #683 changed: the strap
// per page, the merged live+rate reading, the always-shown flag/watch
// markers, the stream's filter chips, and the retired toolbar's
// controls (moved here, unchanged) -- not the account menu's own
// content, which AccountMenu.svelte.test.ts already covers.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(async () => null),
  register: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { emptyFilters } from '../lib/types'

// jsdom has no window.matchMedia -- AccountMenu (mounted by SceneBar)
// pulls in ThemeMenu -> lib/viewport.svelte.ts, whose ViewportState
// singleton calls matchMedia at module-load time (same fix
// AccountMenu.svelte.test.ts and Flags.svelte.test.ts already needed).
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

const { default: SceneBar } = await import('./SceneBar.svelte')

beforeEach(() => {
  authState.username = 'tom'
  authState.role = 'admin'
  authState.hasLocalPassword = true
  authState.ssoAvailable = false

  appState.filters = emptyFilters()
  appState.devices = []
  appState.stats = null
  appState.connState = 'open'
  appState.autoscroll = true
  appState.paused = false

  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
})

describe('SceneBar (#683, ratified round 29)', () => {
  it("carries each page's own strap, in the page's voice", () => {
    render(SceneBar, { scene: 'topography' })
    expect(screen.getByText('Topography')).toBeTruthy()
    expect(screen.getByText('aggregates — click a card to descend')).toBeTruthy()
  })

  it('carries the docket strap for all three of its tabs', () => {
    for (const scene of ['flags', 'watchlist', 'audit'] as const) {
      const { unmount } = render(SceneBar, { scene })
      expect(screen.getByText('what was flagged · what you watch · what changed')).toBeTruthy()
      unmount()
    }
  })

  it('shows LIVE merged with the arriving rate as one reading, not two', () => {
    appState.stats = {
      count: 100,
      capacity: 10000,
      eventsPerSecond: 34,
      syslog: null,
    } as never
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByText('LIVE · 34/s')).toBeTruthy()
  })

  it('shows a non-open connection state in its own words, not as LIVE', () => {
    appState.connState = 'connecting'
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByText('Connecting…')).toBeTruthy()
    expect(screen.queryByText(/LIVE/)).toBeNull()
  })

  it('shows the open-flag marker even at zero -- "nothing open" is a state, not an absence', () => {
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByTitle('no open flags')).toBeTruthy()
    expect(screen.getByText('⚑ 0')).toBeTruthy()
  })

  it('shows the flag count and links to the docket', () => {
    flagsState.list = [
      { id: '1', cleared: false } as never,
      { id: '2', cleared: false } as never,
      { id: '3', cleared: true } as never,
    ]
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByTitle('2 open flags')).toBeTruthy()
  })

  it('shows watchers held, and only shows a broken count when one exists', () => {
    watchlistState.entries = [
      { id: 'a', enabled: true } as never,
      { id: 'b', enabled: true } as never,
    ]
    watchlistState.coverage = { a: 'ok' as never, b: 'ok' as never }
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByTitle('2 watchers held')).toBeTruthy()
    expect(screen.queryByText(/○/)).toBeNull()
  })

  it('shows the broken-ring count once a watch stops holding', () => {
    watchlistState.entries = [{ id: 'a', enabled: true } as never]
    watchlistState.coverage = { a: 'no-logging' as never }
    render(SceneBar, { scene: 'metrics' })
    expect(screen.getByText('○1')).toBeTruthy()
  })

  it('does not draw the retired uptime counter or per-router chips', () => {
    appState.devices = [
      { id: 'r1', name: 'border', sourceIp: '10.0.0.1', configured: true, firstSeen: '', lastSeen: '', eventCount: 0, status: 'live' } as never,
    ]
    render(SceneBar, { scene: 'metrics' })
    expect(screen.queryByText('border')).toBeNull()
    expect(screen.queryByText(/\d+d \d+h/)).toBeNull()
  })

  // #s5's own bar draws bare "LIVE", no rate -- the whisper line right
  // below it already reads "34/s now", and round 29 does not repeat it
  // on the bar for this one scene the way it does everywhere else.
  it('shows bare LIVE on the stream, not LIVE · rate -- the whisper line already carries the rate', () => {
    appState.stats = { count: 100, capacity: 10000, eventsPerSecond: 34, syslog: null } as never
    render(SceneBar, { scene: 'live' })
    expect(screen.getByText('LIVE')).toBeTruthy()
    expect(screen.queryByText(/LIVE ·/)).toBeNull()
  })

  // The retired toolbar's eps/buffer%/max-age/Autoscroll/Pause/Group/
  // Clear are NOT drawn anywhere on #s5's own bar markup -- only the
  // filter chips and (unbuilt pending a product decision -- see the
  // gap list) a SPAN control are. Building them here would be
  // inventing a home the ratified scene does not offer.
  it('does not draw the retired toolbar as bar controls -- round 29 does not show them there', () => {
    render(SceneBar, { scene: 'live' })
    expect(screen.queryByText('Autoscroll')).toBeNull()
    expect(screen.queryByText('Pause')).toBeNull()
    expect(screen.queryByText('Group')).toBeNull()
    expect(screen.queryByText('Clear')).toBeNull()
  })

  it('shows an active filter on the bar as "label:value", with one ⌫ to clear it', () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(SceneBar, { scene: 'live' })
    const search = document.querySelector('.search')
    expect(search?.textContent?.replace(/\s+/g, ' ').trim()).toBe('action:drop ⌫')
    expect(search?.querySelector('em')?.textContent).toBe('drop')
    expect(screen.getByTitle('Clear all filters')).toBeTruthy()
  })

  it('shows no filter summary when no filter is active', () => {
    render(SceneBar, { scene: 'live' })
    expect(document.querySelector('.search')).toBeNull()
    expect(screen.queryByTitle('Clear all filters')).toBeNull()
  })
})
