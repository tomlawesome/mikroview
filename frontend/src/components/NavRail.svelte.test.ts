// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// NavRail itself makes no requests -- this only stops the stores it pulls
// in from reaching for the network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  logout: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { authState } from '../lib/auth.svelte'
import type { Flag, WatchlistEntry } from '../lib/types'
import NavRail from './NavRail.svelte'

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

// The count and its wording, which is all this component decides. Where
// the number comes from is internal/flags' business (an excluded pair is
// always also cleared, so "open" is already "open unexcluded"), and
// whether the badge is drawn inside the 54px rail is a layout fact that
// only a real browser can answer -- frontend/scripts/live-nav-badge.mjs
// covers both against a running server.
describe('NavRail flag badge', () => {
  beforeEach(() => {
    flagsState.list = []
  })

  it('shows no badge when nothing is open', () => {
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags' })).toBeTruthy()
  })

  it('counts open flags and speaks the count in the label', () => {
    flagsState.list = [flag('1', false), flag('2', false)]
    render(NavRail)
    const row = screen.getByRole('button', { name: 'Flags — 2 open' })
    expect(row.textContent).toContain('2')
  })

  it('leaves cleared flags out of the count', () => {
    flagsState.list = [flag('1', false), flag('2', true), flag('3', true)]
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags — 1 open' })).toBeTruthy()
  })

  // A zero would otherwise sit permanently on the row: the record puts one
  // alarm-filled count in the chrome, and only when it has something to say.
  it('drops the badge again once everything is cleared', () => {
    flagsState.list = [flag('1', true)]
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags' })).toBeTruthy()
  })
})

let nextEntryId = 1

function watchlistEntry(overrides: Partial<WatchlistEntry> = {}): WatchlistEntry {
  const id = `e${nextEntryId++}`
  return {
    id,
    name: `watch ${id}`,
    enabled: true,
    createdAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// #546's broken ring. Only Watchlist carries `ring` (see NavRail.svelte's
// Item table), and Watchlist is admin-only, so these need an authenticated
// admin session to render at all -- unlike the Flags badge above, which
// is visible to every role. Whether the outline is actually 2px/3px and
// tightens to the icon at 54px is a layout fact only a real browser can
// answer -- frontend/scripts/live-nav-broken-ring.mjs covers that, plus
// agreement with the server's own coverage answer, against a running
// instance.
describe('NavRail broken ring', () => {
  beforeEach(() => {
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
    authState.state = 'authenticated'
    authState.role = 'admin'
    nextEntryId = 1
  })

  it('wears no ring when nothing is broken', () => {
    render(NavRail)
    const row = screen.getByRole('button', { name: 'Watchlist' })
    expect(row.className).not.toContain('broken')
  })

  it('wears no ring for unknown, out-of-scope or covered coverage', () => {
    const a = watchlistEntry()
    const b = watchlistEntry()
    const c = watchlistEntry()
    watchlistState.entries = [a, b, c]
    watchlistState.coverage = { [a.id]: 'unknown', [b.id]: 'out-of-scope', [c.id]: 'covered' }
    render(NavRail)
    const row = screen.getByRole('button', { name: 'Watchlist' })
    expect(row.className).not.toContain('broken')
  })

  it('rings and names the reason, singular, for exactly one broken watch', () => {
    const e = watchlistEntry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    render(NavRail)
    const row = screen.getByRole('button', {
      name: "Watchlist — 1 watch can't be checked: the firewall rules it needs aren't being logged",
    })
    expect(row.className).toContain('broken')
  })

  it('rings and pluralises the reason for more than one broken watch', () => {
    const a = watchlistEntry()
    const b = watchlistEntry()
    watchlistState.entries = [a, b]
    watchlistState.coverage = { [a.id]: 'no-logging', [b.id]: 'no-logging' }
    render(NavRail)
    const row = screen.getByRole('button', {
      name: "Watchlist — 2 watches can't be checked: the firewall rules they need aren't being logged",
    })
    expect(row.className).toContain('broken')
  })

  it('excludes a disabled entry from both the ring and its count', () => {
    const e = watchlistEntry({ enabled: false })
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    render(NavRail)
    const row = screen.getByRole('button', { name: 'Watchlist' })
    expect(row.className).not.toContain('broken')
  })

  it('clears the ring once coverage recovers -- a live reading, not a record, with no acknowledge step', () => {
    const e = watchlistEntry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    render(NavRail)
    expect(screen.getByRole('button', { name: /can't be checked/ })).toBeTruthy()

    watchlistState.coverage = { [e.id]: 'covered' }
    flushSync()

    expect(screen.getByRole('button', { name: 'Watchlist' })).toBeTruthy()
  })

  // Flags carries a count and Watchlist carries a ring, and neither row
  // carries the other marker today -- so this proves the two independent
  // signals coexist on their own rows without one clobbering the other's
  // label. What happens on a single row carrying *both* is
  // spokenLabel's own unit tests (lib/rail.svelte.test.ts), since no real
  // item does that yet for this to exercise end to end.
  it('shows an unrelated count and ring on their own rows at once', () => {
    const e = watchlistEntry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    flagsState.list = [flag('1', false), flag('2', false)]
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags — 2 open' })).toBeTruthy()
    expect(
      screen.getByRole('button', {
        name: "Watchlist — 1 watch can't be checked: the firewall rules it needs aren't being logged",
      }),
    ).toBeTruthy()
  })
})

// #549: the rail-head dot. Whether it actually clears the 54px icons rail
// without being clipped is a layout fact only a real browser can answer
// (frontend/scripts/live-nav-connection.mjs) -- this only proves the class
// tracks appState.connState the way the record asks: alarm on 'closed',
// quiet otherwise, and decorative either way since ConnectionBanner (not
// this dot) carries the accessible text.
describe('NavRail rail-head dot (#549)', () => {
  it('is quiet while open', () => {
    appState.connState = 'open'
    const { container } = render(NavRail)
    expect(container.querySelector('.rail-head-dot')?.className).not.toContain('alarm')
  })

  it('is quiet while merely connecting -- alarm is reserved for an actual loss', () => {
    appState.connState = 'connecting'
    const { container } = render(NavRail)
    expect(container.querySelector('.rail-head-dot')?.className).not.toContain('alarm')
  })

  it('turns alarm once the connection is lost', () => {
    appState.connState = 'closed'
    const { container } = render(NavRail)
    expect(container.querySelector('.rail-head-dot')?.className).toContain('alarm')
  })

  it('clears again once the connection recovers', () => {
    appState.connState = 'closed'
    const { container } = render(NavRail)
    expect(container.querySelector('.rail-head-dot')?.className).toContain('alarm')

    appState.connState = 'open'
    flushSync()

    expect(container.querySelector('.rail-head-dot')?.className).not.toContain('alarm')
  })

  it('is decorative -- the accessible text lives on ConnectionBanner, not here', () => {
    appState.connState = 'closed'
    const { container } = render(NavRail)
    expect(container.querySelector('.rail-head')?.getAttribute('aria-hidden')).toBe('true')
  })
})

// #548: Users, Tokens, Fleet and Entities are pages under Admin now,
// reached the same way every other row is (appState.view), not the
// act()-driven overlay toggles this replaced. The reserved-slot/
// absent-never-disabled behaviour itself is already covered generally
// by live-nav-rail.mjs; this is the unit-level proof that the two rows
// that used to be act() handlers are now ordinary view rows, and that
// admin-gating for the Admin group's other rows is unchanged.
describe('NavRail Admin group pages (#548)', () => {
  beforeEach(() => {
    flagsState.list = []
    watchlistState.entries = []
    watchlistState.coverage = {}
  })

  it('renders Users and Tokens as ordinary view rows for an admin', () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    appState.view = 'live'
    render(NavRail)

    const users = screen.getByRole('button', { name: 'Users' })
    const tokens = screen.getByRole('button', { name: 'Tokens' })
    expect(users.getAttribute('aria-current')).toBeNull()
    expect(tokens.getAttribute('aria-current')).toBeNull()

    users.click()
    flushSync()
    expect(appState.view).toBe('users')
    expect(screen.getByRole('button', { name: 'Users' }).getAttribute('aria-current')).toBe('page')

    tokens.click()
    flushSync()
    expect(appState.view).toBe('tokens')
    expect(screen.getByRole('button', { name: 'Tokens' }).getAttribute('aria-current')).toBe('page')
  })

  it('keeps Users, Tokens, Entities and Run setup… absent for a viewer, per #490s absent-never-disabled grammar', () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(NavRail)

    for (const label of ['Users', 'Tokens', 'Entities', 'Run setup…']) {
      expect(screen.queryByRole('button', { name: label })).toBeNull()
    }
    // Fleet has no admin gate -- it is the one Admin-group row a viewer
    // reaches today.
    expect(screen.getByRole('button', { name: 'Fleet' })).toBeTruthy()
  })
})
