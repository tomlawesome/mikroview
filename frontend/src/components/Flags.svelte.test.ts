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
