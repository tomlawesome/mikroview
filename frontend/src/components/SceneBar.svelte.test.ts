// SPDX-License-Identifier: AGPL-3.0-only
//
// The scene bar to round 30's ratified content (#683, #700):
// MIKROVIEW · <the page's own switcher> · <page's own controls> ·
// LIVE · rate · ⚑ N · ◉ held ○ broken · account. No page name and no
// strap -- struck on every deck and ratified in words (#697). Where
// they stood, the switchers ride: metrics' three views and the
// docket's three tabs. Covers that, the merged live+rate reading, the
// always-shown flag/watch markers and the stream's filter chips -- not
// the account menu's own content, which AccountMenu.svelte.test.ts
// already covers.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

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
import { metricsPref } from '../lib/metrics.svelte'
import { retentionState } from '../lib/retention.svelte'
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

describe('SceneBar (#683, ratified round 30)', () => {
  // #697, owner verbatim: "I meant all... No page heading, no strap."
  // Round 30 deleted the rules that drew them, so this pins that the
  // app does not draw them either -- on a scene that used to carry a
  // long strap and on one whose three tabs shared one.
  it('carries no page name and no strap on any scene', () => {
    for (const [scene, name, strap] of [
      ['topography', 'Topography', 'aggregates — click a card to descend'],
      ['live', 'Stream', 'every line, as it arrived'],
      ['flags', 'The docket', 'what was flagged · what you watch · what changed'],
    ] as const) {
      const { unmount } = render(SceneBar, { scene })
      expect(screen.queryByText(name)).toBeNull()
      expect(screen.queryByText(strap)).toBeNull()
      expect(screen.queryByRole('heading')).toBeNull()
      unmount()
    }
  })

  // Moved here from Metrics.svelte.test.ts with the switcher itself
  // (#700): round 30 rides it on the bar, beside the wordmark, where
  // the heading used to be.
  it("rides metrics' three views, with the seismograph selected by default", () => {
    render(SceneBar, { scene: 'metrics' })
    for (const name of ['seismograph', 'register', 'table']) {
      expect(screen.getByRole('button', { name })).toBeTruthy()
    }
    expect(screen.getByRole('button', { name: 'seismograph' }).getAttribute('aria-pressed')).toBe('true')
  })

  it('switches the metrics view from the bar', async () => {
    render(SceneBar, { scene: 'metrics' })
    await fireEvent.click(screen.getByRole('button', { name: 'register' }))
    flushSync()
    expect(metricsPref.view).toBe('register')
    expect(screen.getByRole('button', { name: 'register' }).getAttribute('aria-pressed')).toBe('true')
    // Persisted, so a reload applies it before first paint (#488).
    expect(localStorage.getItem('mikroview-metrics-view')).toBe('register')
  })

  // Moved here from Docket.svelte.test.ts with the tab row (#700). The
  // tier rule is unchanged: absent, never disabled (#653).
  it('rides only the docket tabs the signed-in tier can reach', async () => {
    for (const [role, watchlist, audit] of [
      ['viewer', false, false],
      ['user', true, false],
      ['admin', true, true],
    ] as const) {
      authState.role = role
      const { unmount } = render(SceneBar, { scene: 'flags' })
      flushSync()
      expect(screen.getByRole('tab', { name: 'flags' })).toBeTruthy()
      expect(screen.queryByRole('tab', { name: 'watchlist' }) !== null).toBe(watchlist)
      expect(screen.queryByRole('tab', { name: 'audit log' }) !== null).toBe(audit)
      unmount()
    }
  })

  // Round 30: "No counts on the docket's tabs" -- tried inline and
  // beneath, both called clumsy; they live in this bar's own ⚑ and eye
  // marks instead, which the marker tests below cover.
  it('puts no count under a docket tab', async () => {
    authState.role = 'admin'
    flagsState.list = [
      {
        id: 'f1',
        type: 'port_scan',
        target: '203.0.113.9',
        detail: '',
        count: 1,
        firstSeen: '2026-01-01T00:00:00Z',
        lastSeen: '2026-01-01T00:00:00Z',
        cleared: false,
      },
    ]
    render(SceneBar, { scene: 'flags' })
    flushSync()
    expect(screen.getByRole('tab', { name: 'flags' }).textContent?.trim()).toBe('flags')
  })

  // #703: the control is only honest if a span the buffer cannot cover
  // is visibly not on offer. These pin that, and that choosing one sets
  // the same display window the mobile drawer sets.
  describe("the stream's span control", () => {
    function statsHolding(oldestHeld: string | null) {
      appState.stats = {
        total: 0,
        byAction: {},
        topRules: [],
        timeSeries: [],
        eventsPerSecond: 34,
        capacity: 100000,
        count: 10,
        windowSeconds: 3600,
        oldestHeld,
        connectedClients: 1,
      }
    }

    it('offers every span the buffer reaches back far enough to answer', () => {
      statsHolding(new Date(appState.now - 2 * 86400 * 1000).toISOString())
      render(SceneBar, { scene: 'live' })
      flushSync()

      for (const label of ['15 m', '1 h', '24 h']) {
        expect(screen.getByRole('button', { name: label }).hasAttribute('disabled')).toBe(false)
      }
    })

    it('withholds a fortnight from a buffer holding nine hours, and says what it holds', () => {
      statsHolding(new Date(appState.now - 9 * 3600 * 1000).toISOString())
      render(SceneBar, { scene: 'live' })
      flushSync()

      expect(screen.getByRole('button', { name: '1 h' }).hasAttribute('disabled')).toBe(false)
      expect(screen.getByRole('button', { name: '24 h' }).hasAttribute('disabled')).toBe(true)
      expect(screen.getByRole('button', { name: '14 d' }).hasAttribute('disabled')).toBe(true)
      expect(screen.getByText('holding 9 h')).toBeTruthy()
    })

    it('offers only the shortest span while the buffer holds nothing', () => {
      statsHolding(null)
      render(SceneBar, { scene: 'live' })
      flushSync()

      expect(screen.getByRole('button', { name: '15 m' }).hasAttribute('disabled')).toBe(false)
      for (const label of ['1 h', '24 h', '14 d']) {
        expect(screen.getByRole('button', { name: label }).hasAttribute('disabled')).toBe(true)
      }
      expect(screen.getByText('nothing held yet')).toBeTruthy()
    })

    it('sets the display window when a span is chosen', async () => {
      statsHolding(new Date(appState.now - 2 * 3600 * 1000).toISOString())
      render(SceneBar, { scene: 'live' })
      flushSync()

      await fireEvent.click(screen.getByRole('button', { name: '1 h' }))
      expect(retentionState.maxAgeSeconds).toBe(3600)
      expect(screen.getByRole('button', { name: '1 h' }).getAttribute('aria-pressed')).toBe('true')
    })

    it('draws no span control away from the stream', () => {
      statsHolding(new Date(appState.now - 2 * 3600 * 1000).toISOString())
      render(SceneBar, { scene: 'metrics' })
      flushSync()

      expect(screen.queryByRole('button', { name: '15 m' })).toBeNull()
    })
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
