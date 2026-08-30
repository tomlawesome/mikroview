// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// BottomBar itself makes no requests -- this only stops the stores it
// pulls in from reaching for the network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  logout: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { flagsState } from '../lib/flags.svelte'
import { authState } from '../lib/auth.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { navGroups, type NavItem } from '../lib/navGroups'
import type { Flag, WatchlistEntry } from '../lib/types'
import BottomBar from './BottomBar.svelte'

function flag(id: string, cleared: boolean): Flag {
  return {
    id,
    type: 'port_scan',
    target: `198.51.100.${id}`,
    detail: 'a port scan',
    count: 1,
    firstSeen: '2026-08-23T20:00:00Z',
    lastSeen: '2026-08-23T20:00:00Z',
    cleared,
  }
}

let nextEntryId = 1

function watchlistEntry(overrides: Partial<WatchlistEntry> = {}): WatchlistEntry {
  const id = `e${nextEntryId++}`
  return { id, name: `watch ${id}`, enabled: true, createdAt: '2026-01-01T00:00:00Z', ...overrides }
}

beforeEach(() => {
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  nextEntryId = 1
  appState.view = 'live'
  authState.state = 'authenticated'
  authState.role = 'admin'
  // jsdom does not implement session-history navigation (`back()`/
  // `forward()` are documented no-ops that log "Not implemented" to the
  // console) -- closeSheet()'s call to it is exercised for real by
  // frontend/scripts/live-nav-small-screen.mjs against a real browser.
  // Stubbed here so a unit test isn't asserting jsdom's own limitation.
  vi.spyOn(window.history, 'back').mockImplementation(() => {})
  window.history.replaceState(null, '', '/')
})

// The bar itself: one button per visible group, in the ratified order,
// same reserved-slot table lib/navGroups.ts shares with NavRail.
describe('BottomBar groups', () => {
  it('renders exactly the five groups for an admin, in order', () => {
    render(BottomBar)
    const names = screen.getAllByRole('button').map((b) => b.textContent?.trim())
    expect(names).toEqual(['Live', 'Investigate', 'Detect', 'Expect', 'Admin'])
  })

  it('never renders dock or density controls -- pointer-width-only affordances', () => {
    const { container } = render(BottomBar)
    expect(container.querySelector('[aria-label^="Dock"]')).toBeNull()
    expect(container.querySelector('[aria-label^="Show icons"]')).toBeNull()
  })
})

// Group-to-sheet routing: the record's "tapping a group with more than
// one page raises a half-sheet ... single-page groups go straight to the
// page".
describe('BottomBar single-page shortcut', () => {
  it('navigates straight to the page for a single-page group, no sheet', () => {
    // Detect is single-page since #490 folded Detectors into the engine
    // room (see lib/navGroups.ts) -- Live no longer is, since #616 gave
    // it a second row (The fall, then Stream), so it moved to its own
    // half-sheet case below instead of standing in for this one.
    render(BottomBar)
    screen.getByRole('button', { name: 'Detect' }).click()
    flushSync()
    expect(appState.view).toBe('flags')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('collapses to a direct navigation once admin filtering leaves a group with one page', () => {
    // Investigate is Metrics + admin-only Audit log -- a viewer sees only
    // Metrics, so tapping the group must go straight there rather than
    // raising a one-item sheet.
    authState.role = 'user'
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()
    expect(appState.view).toBe('metrics')
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('BottomBar half-sheet', () => {
  it('raises a half-sheet for Live now that it carries two rows (#616)', () => {
    render(BottomBar)
    screen.getByRole('button', { name: 'Live' }).click()
    flushSync()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeTruthy()
    expect(screen.getByRole('button', { name: 'The fall' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Stream' })).toBeTruthy()

    screen.getByRole('button', { name: 'Stream' }).click()
    flushSync()
    expect(appState.view).toBe('live')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('raises a half-sheet listing the pages of a multi-page group', () => {
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Metrics' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Audit log' })).toBeTruthy()
    // The bar's own group button is a distinct control from the sheet's.
    expect(appState.view).toBe('live')
  })

  it('selecting a page in the sheet navigates and closes it', () => {
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()

    screen.getByRole('button', { name: 'Metrics' }).click()
    flushSync()

    expect(appState.view).toBe('metrics')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('Esc closes the sheet without navigating', () => {
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()
    expect(screen.getByRole('dialog')).toBeTruthy()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    flushSync()

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(appState.view).toBe('live')
  })

  it('the close button closes the sheet', () => {
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()

    screen.getByRole('button', { name: 'Close' }).click()
    flushSync()

    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('switching groups while a sheet is open replaces its contents rather than stacking', () => {
    // Detect is a single-page group since #490 folded Detectors into the
    // engine room (see lib/navGroups.ts), so it no longer raises a sheet
    // at all -- Admin (Engine room/Fleet/Entities/Run setup…) is the
    // stand-in multi-page group here instead.
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()
    screen.getByRole('button', { name: 'Admin' }).click()
    flushSync()

    expect(screen.getAllByRole('dialog').length).toBe(1)
    expect(screen.getByRole('button', { name: 'Fleet' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Metrics' })).toBeNull()
  })

  it('marks the current page inside the sheet', () => {
    appState.view = 'audit'
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()

    expect(screen.getByRole('button', { name: 'Audit log' }).getAttribute('aria-current')).toBe('page')
    expect(screen.getByRole('button', { name: 'Metrics' }).getAttribute('aria-current')).toBeNull()
  })
})

// #490's absent-never-disabled grammar, carried forward from NavRail.
describe('BottomBar admin gating (#490)', () => {
  it('keeps admin-only groups/pages absent for a viewer rather than disabled', () => {
    authState.role = 'user'
    render(BottomBar)

    // Investigate's only viewer-visible page is Metrics (Audit log is
    // admin-only), so it too collapses to a single-page group.
    expect(screen.queryByRole('button', { name: 'Audit log' })).toBeNull()

    const buttons = screen.getAllByRole('button')
    expect(buttons.some((b) => b.hasAttribute('disabled'))).toBe(false)
  })

  it('drops a group entirely once every one of its pages is admin-only', () => {
    // Expect holds only Watchlist (admin: true) -- a viewer loses the
    // whole group rather than seeing an empty heading or a disabled row.
    authState.role = 'user'
    render(BottomBar)
    expect(screen.queryByRole('button', { name: 'Expect' })).toBeNull()
  })
})

// The flag badge -- the one piece of chrome the record says must survive
// onto the bar ("badge intact").
describe('BottomBar flag badge', () => {
  it('shows no badge on Detect when nothing is open', () => {
    render(BottomBar)
    expect(screen.getByRole('button', { name: 'Detect' })).toBeTruthy()
  })

  it('shows the open count on Detect, spoken in its aria-label', () => {
    flagsState.list = [flag('1', false), flag('2', false)]
    render(BottomBar)
    const btn = screen.getByRole('button', { name: 'Detect — 2 open' })
    expect(btn.textContent).toContain('2')
  })

  it('does not put a badge on an unrelated group', () => {
    flagsState.list = [flag('1', false)]
    render(BottomBar)
    expect(screen.getByRole('button', { name: 'Live' })).toBeTruthy()
  })
})


// #583's ring on the bar. The group's claim is one level up from the
// rail's -- "an answer behind this group cannot be trusted" -- and the
// record allows it only because the next tap resolves it, so the group
// ring and the page ring are read from the same source here rather than
// computed twice. Whether the outline is really 2px/3px and hugs the icon
// rather than the label is a layout fact only a real browser can answer:
// frontend/scripts/live-nav-bottom-bar.mjs covers that at a small
// viewport, against a real server's coverage answer.
describe('BottomBar broken ring on the group', () => {
  function ringOn(name: string): boolean {
    const slot = screen.getByRole('button', { name }).querySelector('.icon-slot')
    return slot?.className.includes('broken') ?? false
  }

  function breakOne() {
    const e = watchlistEntry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    return e
  }

  it('wears no ring when nothing is broken', () => {
    render(BottomBar)
    expect(ringOn('Expect')).toBe(false)
    expect(screen.getByRole('button', { name: 'Expect' }).getAttribute('aria-label')).toBeNull()
  })

  it('wears no ring for unknown, out-of-scope or covered coverage', () => {
    const a = watchlistEntry()
    const b = watchlistEntry()
    const c = watchlistEntry()
    watchlistState.entries = [a, b, c]
    watchlistState.coverage = { [a.id]: 'unknown', [b.id]: 'out-of-scope', [c.id]: 'covered' }
    render(BottomBar)
    expect(ringOn('Expect')).toBe(false)
  })

  it('rings Expect and speaks the reason, singular, for exactly one broken watch', () => {
    breakOne()
    render(BottomBar)
    const name = "Expect — 1 watch can't be checked: the firewall rules it needs aren't being logged"
    expect(ringOn(name)).toBe(true)
  })

  it('pluralises the reason for more than one broken watch', () => {
    const a = watchlistEntry()
    const b = watchlistEntry()
    watchlistState.entries = [a, b]
    watchlistState.coverage = { [a.id]: 'no-logging', [b.id]: 'no-logging' }
    render(BottomBar)
    const name = "Expect — 2 watches can't be checked: the firewall rules they need aren't being logged"
    expect(ringOn(name)).toBe(true)
  })

  it('excludes a disabled entry from the ring', () => {
    const e = watchlistEntry({ enabled: false })
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    render(BottomBar)
    expect(ringOn('Expect')).toBe(false)
  })

  it('rings no other group -- the claim names the one group the break is behind', () => {
    breakOne()
    render(BottomBar)
    for (const name of ['Live', 'Investigate', 'Detect', 'Admin']) expect(ringOn(name)).toBe(false)
  })

  it('clears the ring once coverage recovers -- a live reading, not a record', () => {
    const e = breakOne()
    render(BottomBar)
    expect(screen.getByRole('button', { name: /can't be checked/ })).toBeTruthy()

    watchlistState.coverage = { [e.id]: 'covered' }
    flushSync()

    expect(screen.getByRole('button', { name: 'Expect' })).toBeTruthy()
    expect(ringOn('Expect')).toBe(false)
  })

  // The record's "two alarm-red marks on one bar is allowed": Flags'
  // filled count on Detect and Expect's outline ring at the same time.
  // Different marks, different groups -- recorded so it is not later
  // "fixed" as a clash.
  it('shows the flag count and the ring at once, on their own groups', () => {
    breakOne()
    flagsState.list = [flag('1', false), flag('2', false)]
    render(BottomBar)
    expect(screen.getByRole('button', { name: 'Detect — 2 open' })).toBeTruthy()
    expect(
      ringOn("Expect — 1 watch can't be checked: the firewall rules it needs aren't being logged"),
    ).toBe(true)
  })

  it('shows no ring to a viewer, who has no Watchlist page for it to point at', () => {
    breakOne()
    authState.role = 'user'
    const { container } = render(BottomBar)
    expect(screen.queryByRole('button', { name: 'Expect' })).toBeNull()
    expect(container.querySelector('.icon-slot.broken')).toBeNull()
  })
})

// The half-sheet's page row. Expect holds one page today, so tapping it
// navigates straight to Watchlist and no sheet is ever raised for the one
// group that can ring -- which leaves the sheet's own rendering of the
// ring unreachable through the shipped table. A second page is added to
// Expect for this block alone, since what is under test is the sheet's
// rendering rule ("a group ring the sheet does not resolve is a dead end,
// and is not shown"), not today's geography.
describe('BottomBar broken ring in the half-sheet', () => {
  const expectGroup = navGroups.find((g) => g.name === 'Expect')!
  const secondPage: NavItem = {
    label: 'Coverage',
    view: 'metrics',
    icon: 'watchlist',
    title: 'Stand-in second page, so Expect raises a sheet at all',
  }

  beforeEach(() => {
    expectGroup.items = [...expectGroup.items, secondPage]
    const e = watchlistEntry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
  })

  afterEach(() => {
    expectGroup.items = expectGroup.items.filter((i) => i !== secondPage)
  })

  it('rings the page that carries the break, and names it, inside the sheet', () => {
    render(BottomBar)
    screen.getByRole('button', { name: /^Expect/ }).click()
    flushSync()

    const row = screen.getByRole('button', {
      name: "Watchlist — 1 watch can't be checked: the firewall rules it needs aren't being logged",
    })
    expect(row.className).toContain('broken')
  })

  it('leaves the group\'s other pages unringed -- the sheet is what resolves the group ring', () => {
    render(BottomBar)
    screen.getByRole('button', { name: /^Expect/ }).click()
    flushSync()

    const other = screen.getByRole('button', { name: 'Coverage' })
    expect(other.className).not.toContain('broken')
    expect(other.getAttribute('aria-label')).toBeNull()
  })

  it('resolves the group ring: the group rings only while some page in the sheet does', () => {
    render(BottomBar)
    const group = screen.getByRole('button', { name: /^Expect/ })
    expect(group.querySelector('.icon-slot')?.className).toContain('broken')

    watchlistState.coverage = {}
    flushSync()

    screen.getByRole('button', { name: 'Expect' }).click()
    flushSync()
    expect(screen.getByRole('button', { name: 'Watchlist' }).className).not.toContain('broken')
  })
})
