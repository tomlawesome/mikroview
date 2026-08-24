// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
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
import type { Flag } from '../lib/types'
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

beforeEach(() => {
  flagsState.list = []
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
    render(BottomBar)
    screen.getByRole('button', { name: 'Live' }).click()
    flushSync()
    expect(appState.view).toBe('live')
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
    render(BottomBar)
    screen.getByRole('button', { name: 'Investigate' }).click()
    flushSync()
    screen.getByRole('button', { name: 'Detect' }).click()
    flushSync()

    expect(screen.getAllByRole('dialog').length).toBe(1)
    expect(screen.getByRole('button', { name: 'Flags' })).toBeTruthy()
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
