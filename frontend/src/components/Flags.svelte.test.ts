// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Flags.svelte itself makes no requests directly -- these stop the
// stores it pulls in (flagsState, exclusionsState) from reaching for the
// network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  fetchExclusions: vi.fn(async () => []),
  removeExclusion: vi.fn(),
}))

import { fetchExclusions } from '../lib/api'
import { flagsState } from '../lib/flags.svelte'
import { exclusionsState } from '../lib/exclusions.svelte'
import { authState } from '../lib/auth.svelte'
import type { Exclusion } from '../lib/types'

// jsdom has no window.matchMedia -- Flags.svelte pulls in
// lib/viewport.svelte.ts, whose ViewportState singleton calls it at
// module-load time. Polyfilled before the dynamic import below (a
// static import would already have run this file's top-level code too
// late to matter) -- same fix LiveTable.svelte.test.ts already needed.
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

const { default: Flags } = await import('./Flags.svelte')

function exclusion(id: string, target: string): Exclusion {
  return { id, type: 'port_scan', target }
}

// #547: Exclusions folded into Flags as a tab, admin-only (GET/DELETE
// /api/flags/exclusions both 403 a non-admin caller). What this proves
// that a plain unit test of exclusionsState alone cannot: the tab is
// absent -- not merely unusable -- for a viewer, and the count it
// carries for an admin is the quiet, outlined kind the record reserves
// for in-page badges, never the rail's single alarm-filled count.
describe('Flags Exclusions tab (#547)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    flagsState.list = []
    exclusionsState.list = []
  })

  it('renders no tablist at all for a viewer -- a single-tab tablist is not the pattern', () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(Flags)
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.queryByRole('tab', { name: /Exclusions/ })).toBeNull()
  })

  it('carries no count on the Exclusions tab while the list is empty -- omitted, not shown as a permanent 0', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(Flags)
    await Promise.resolve()
    flushSync()
    const tab = screen.getByRole('tab', { name: 'Exclusions' })
    expect(tab.querySelector('.count')).toBeNull()
  })

  it('renders a tablist with an Exclusions tab for an admin', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(Flags)
    await Promise.resolve()
    flushSync()
    expect(screen.getByRole('tablist', { name: 'Flags views' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Flags' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /Exclusions/ })).toBeTruthy()
  })

  it('carries the current exclusion count on the tab, and it is not the rail badge markup', async () => {
    vi.mocked(fetchExclusions).mockResolvedValue([exclusion('e1', '198.51.100.1'), exclusion('e2', '198.51.100.2')])
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(Flags)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    const tab = screen.getByRole('tab', { name: 'Exclusions 2' })
    expect(tab.querySelector('.count')?.textContent).toBe('2')
  })

  it('switching to the Exclusions tab shows its panel and hides the Flags panel', async () => {
    vi.mocked(fetchExclusions).mockResolvedValue([exclusion('e1', '198.51.100.1')])
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(Flags)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    const flagsPanel = document.getElementById('panel-flags')
    const exclusionsPanel = document.getElementById('panel-exclusions')
    expect(flagsPanel?.hasAttribute('hidden')).toBe(false)
    expect(exclusionsPanel?.hasAttribute('hidden')).toBe(true)

    await fireEvent.click(screen.getByRole('tab', { name: /Exclusions/ }))
    flushSync()

    expect(flagsPanel?.hasAttribute('hidden')).toBe(true)
    expect(exclusionsPanel?.hasAttribute('hidden')).toBe(false)
    expect(screen.getByText('198.51.100.1')).toBeTruthy()
  })
})

// A custom detection's flag carries the definition's own name as its
// type -- a string the sixteen-entry palette and label tables cannot
// know. Indexing them directly crashed the render on the first custom
// flag, and because the deck mounts every card, that one flag took down
// every scene at once (caught by the whole live-check suite timing out
// from live-definitions onward). familyOf/labelFor are the fix; this
// pins the render surviving.
describe('a custom detection type renders without crashing', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    exclusionsState.list = []
    authState.role = 'admin'
    flagsState.list = [
      {
        id: 'custom:198.51.100.9',
        type: 'live-custom-detection watch' as never,
        target: '198.51.100.9',
        detail: 'an operator-authored detection fired',
        count: 3,
        firstSeen: new Date().toISOString(),
        lastSeen: new Date().toISOString(),
        cleared: false,
      },
    ]
  })

  it('shows the card with the author-named type as its label', () => {
    render(Flags)
    flushSync()
    // Two homes, both honest: the card's own type line (wearing the
    // custom family's advisory mark) and the by-type breakdown.
    expect(screen.getAllByText(/live-custom-detection watch/).length).toBeGreaterThan(0)
    expect(screen.getByText('▲ live-custom-detection watch')).toBeTruthy()
  })
})
