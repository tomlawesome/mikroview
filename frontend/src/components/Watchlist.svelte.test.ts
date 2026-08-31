// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Watchlist.svelte itself makes no requests directly -- these stop the
// stores it pulls in (watchlistState, suggestState, matchesState) from
// reaching for the network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchWatchlistEntries: vi.fn(async () => ({ entries: [], coverage: {} })),
  createWatchlistEntry: vi.fn(),
  updateWatchlistEntry: vi.fn(),
  deleteWatchlistEntry: vi.fn(),
  fetchWatchlistMatches: vi.fn(),
  fetchRecentMatches: vi.fn(async () => []),
  promoteWatchlistDestinations: vi.fn(),
  setWatchlistObserving: vi.fn(),
  setWatchlistEnabled: vi.fn(),
  fetchSuggestions: vi.fn(async () => []),
  acceptSuggestion: vi.fn(),
  hideSuggestion: vi.fn(),
  unhideSuggestion: vi.fn(),
  resetSuggestions: vi.fn(),
}))

import { fetchRecentMatches, fetchSuggestions, fetchWatchlistEntries, setWatchlistEnabled } from '../lib/api'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { appState } from '../lib/state.svelte'
import type { Suggestion, WatchlistCoverage, WatchlistEntry, WatchlistMatch } from '../lib/types'
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

// #584: Matches folded into Watchlist as a third tab -- one merged,
// reverse-chronological list of every entry's matches, not an entry
// picker and not grouped. Same admin argument as Suggestions above: no
// gating logic lives in the component, because Watchlist only ever
// mounts for an admin.
function entry(id: string, name: string, overrides: Partial<WatchlistEntry> = {}): WatchlistEntry {
  return { id, name, enabled: true, createdAt: '2026-08-24T09:00:00Z', ...overrides }
}

function recordFor(id: string, entryId: string, overrides: Partial<WatchlistMatch> = {}): WatchlistMatch {
  return {
    id,
    entryId,
    tuple: { source: { mac: 'aa:bb:cc:dd:ee:ff' }, destIp: '198.51.100.7', port: 9999 },
    event: {
      id: 1,
      time: '2026-08-24T10:00:00Z',
      deviceId: 'router-1',
      sourceIp: '192.168.1.1',
      action: 'drop',
      ruleLabel: 'r1',
      chain: 'forward',
      raw: 'raw line',
    },
    firstSeen: '2026-08-24T10:00:00Z',
    lastSeen: '2026-08-24T10:00:00Z',
    count: 1,
    ...overrides,
  }
}

// The entries come back through the mocked API rather than being
// assigned onto watchlistState: Watchlist refreshes on mount, so
// anything written directly onto the store is overwritten by the fetch
// a moment later.
async function renderWatchlist(entries: WatchlistEntry[], coverage: Record<string, WatchlistCoverage> = {}) {
  vi.mocked(fetchWatchlistEntries).mockResolvedValue({ entries, coverage })
  render(Watchlist)
  await settle()
}

// Several rounds of microtasks, then a flush. More than looks necessary
// on purpose: opening Matches chains two fetches (entries, then the
// matches that are named from them), so the rendered result is a few
// hops further down the queue than a single await would reach.
async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  flushSync()
}

async function openMatches() {
  await fireEvent.click(screen.getByRole('tab', { name: 'Matches' }))
  await settle()
}

describe('Watchlist Matches tab (#584)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
  })

  it('renders a Matches tab alongside Watchlist and Suggestions', () => {
    render(Watchlist)
    const tabs = screen.getAllByRole('tab').map((t) => t.textContent?.trim())
    expect(tabs).toEqual(['Watchlist', 'Matches', 'Suggestions'])
  })

  it('shows one row per match, newest first, naming the entry and its mode', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      recordFor('m2', 'e2', { lastSeen: '2026-08-24T11:00:00Z', count: 4 }),
      recordFor('m1', 'e1', { tuple: { source: { ip: '192.168.1.50' }, destIp: '10.0.0.9', port: 22 } }),
    ])
    await renderWatchlist([entry('e1', 'SSH watch'), entry('e2', 'live camera', { invert: true })])
    await openMatches()

    const rows = document.querySelectorAll('#panel-matches .match')
    expect(rows.length).toBe(2)
    expect(rows[0].textContent).toContain('live camera')
    expect(rows[0].textContent).toContain('egress policy')
    expect(rows[0].textContent).toContain('4×')
    expect(rows[1].textContent).toContain('SSH watch')
    expect(rows[1].textContent).toContain('watched port')
    // The event's own identity, not the entry's scope -- an "any
    // source" entry's matches carry whichever device triggered them.
    expect(rows[1].textContent).toContain('192.168.1.50')
    expect(rows[1].textContent).toContain('10.0.0.9:22')
  })

  it('the entry name goes back to that entry, expanded, on the Watchlist tab', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([recordFor('m1', 'e1')])
    await renderWatchlist([entry('e1', 'SSH watch', { ports: [22] })])
    await openMatches()

    await fireEvent.click(screen.getByRole('button', { name: 'SSH watch' }))
    flushSync()

    expect(document.getElementById('panel-matches')?.hasAttribute('hidden')).toBe(true)
    expect(document.getElementById('panel-watchlist')?.hasAttribute('hidden')).toBe(false)
    // Expanded, not merely scrolled to: the expanded block is what
    // carries the entry's own permitted/observed/matches detail.
    expect(document.querySelector('#entry-e1 .expanded')).toBeTruthy()
  })

  it('an empty tab names an enabled entry nothing can ever feed, rather than reporting a clean sheet', async () => {
    await renderWatchlist([entry('e1', 'SSH watch'), entry('e2', 'disabled watch', { enabled: false })], {
      e1: 'no-logging',
      e2: 'no-logging',
    })
    await openMatches()

    const panel = document.getElementById('panel-matches')
    expect(panel?.textContent).toContain('Nothing has matched -- and nothing could.')
    expect(panel?.textContent).toContain('SSH watch')
    // A disabled watch is not a promise mikroview can see anything, so
    // it never counts (#546's rule, applied here).
    expect(panel?.textContent).not.toContain('disabled watch')
  })

  it('an empty tab with every enabled entry fed says nothing has broken', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')], { e1: 'covered' })
    await openMatches()

    expect(document.getElementById('panel-matches')?.textContent).toContain('Nothing has broken.')
  })

  it('an empty tab with nothing enabled says there is nothing to match yet', async () => {
    await renderWatchlist([])
    await openMatches()

    const panel = document.getElementById('panel-matches')
    expect(panel?.textContent).toContain('There is nothing to match yet.')
    expect(panel?.textContent).not.toContain('Nothing has broken.')
  })

  it('refreshes the entries on arrival, so a live entry is never named "(entry removed)"', async () => {
    // The failure this pins down was found by live-matches-tab.mjs, not
    // here: the page stays mounted, an entry created after it mounted is
    // absent from the list the rows resolve names from, and nothing
    // refreshed it until App.svelte's 60-second coverage interval. Every
    // row said "(entry removed)" about an entry that existed.
    await renderWatchlist([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([recordFor('m1', 'e1')])
    vi.mocked(fetchWatchlistEntries).mockResolvedValue({ entries: [entry('e1', 'SSH watch')], coverage: {} })

    await openMatches()

    const panel = document.getElementById('panel-matches')
    expect(panel?.textContent).toContain('SSH watch')
    expect(panel?.textContent).not.toContain('(entry removed)')
  })

  it('offers "load older" only while older matches may remain', async () => {
    // A short page is the whole log, so the control is not offered.
    vi.mocked(fetchRecentMatches).mockResolvedValue([recordFor('m1', 'e1')])
    await renderWatchlist([entry('e1', 'SSH watch')])
    await openMatches()

    expect(screen.queryByRole('button', { name: 'Load older' })).toBeNull()
    expect(document.getElementById('panel-matches')?.textContent).toContain('Nothing older')
  })
})

// Issue #649: every column on the docket's three tabs sorts (click,
// again to reverse) and filters (a quiet dashed row beneath the labels).
// Watchlist entries render as cards, not a table, so the sortbar/
// filterbar stand in for column heads.
describe('Watchlist Entries sort and filter (#649)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    suggestState.candidates = []
    matchesState.reset()
  })

  function entryNames() {
    return Array.from(document.querySelectorAll('#panel-watchlist .card .name')).map((el) => el.textContent?.trim())
  }

  it('defaults to alphabetical by watch name', async () => {
    await renderWatchlist([entry('e1', 'zebra watch'), entry('e2', 'alpha watch')])
    expect(entryNames()).toEqual(['alpha watch', 'zebra watch'])
  })

  it('clicking a sort head (watch) reverses on a second click', async () => {
    await renderWatchlist([entry('e1', 'zebra watch'), entry('e2', 'alpha watch')])

    // Scoped to the Entries section (#676 added a second "watch" sort
    // head above it, on the ratified table) -- otherwise this matches
    // both.
    const entriesSection = document.getElementById('entries-section') as HTMLElement
    await fireEvent.click(within(entriesSection).getByRole('button', { name: /^watch/ }))
    flushSync()
    expect(entryNames()).toEqual(['zebra watch', 'alpha watch'])
  })

  it('a filter on state narrows to entries in that state only', async () => {
    await renderWatchlist(
      [entry('e1', 'SSH watch'), entry('e2', 'broken watch')],
      { e2: 'no-logging' },
    )

    await fireEvent.input(screen.getByLabelText('Filter by state'), { target: { value: 'broken' } })
    flushSync()

    expect(entryNames()).toEqual(['broken watch'])
  })

  it('says plainly when nothing matches the filters', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')])

    await fireEvent.input(screen.getByLabelText('Filter by watch name'), { target: { value: 'nope' } })
    flushSync()

    expect(screen.getByText('No entries match these filters.')).toBeTruthy()
  })
})

// #676: the ratified table (round 29's docket scene), built alongside
// -- not instead of -- the Entries section above, per the issue's own
// "leave the existing capability reachable" instruction.
describe('The ratified watch table (#676)', () => {
  function watchTable(): HTMLElement {
    return document.querySelector('.watch-table-section') as HTMLElement
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    vi.mocked(setWatchlistEnabled).mockResolvedValue(entry('e1', 'SSH watch', { enabled: false }))
    suggestState.candidates = []
    matchesState.reset()
    // A fixed clock so formatRelative's "last event" column and the
    // drawer's story are deterministic rather than drifting with the
    // real wall clock.
    appState.now = new Date('2026-08-24T10:05:00Z').getTime()
  })

  it('renders watch, boundary, window and state for every entry -- window always reads "always"', async () => {
    await renderWatchlist(
      [
        entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' }, destIp: '10.0.0.9', ports: [22] }),
        entry('e2', 'broken watch', { source: { ip: '192.168.1.50' } }),
        entry('e3', 'paused watch', { enabled: false, source: { mac: '11:22:33:44:55:66' } }),
      ],
      { e2: 'no-logging' },
    )

    const table = watchTable()
    expect(table.textContent).toContain('SSH watch')
    expect(table.textContent).toContain('aa:bb:cc:dd:ee:ff → 10.0.0.9')
    expect(table.textContent).toContain('◉ watching')
    expect(table.textContent).toContain('192.168.1.50 → any destination')
    expect(table.textContent).toContain('○ ring broken')
    expect(table.textContent).toContain('paused watch')
    // No entry carries a schedule -- see Watchlist.svelte's own comment
    // on why every row honestly reads "always" rather than a fabricated
    // window.
    expect(table.querySelectorAll('td.t').length).toBeGreaterThan(0)
    expect(table.textContent?.match(/always/g)?.length).toBe(3)
  })

  it("shows the entry's most recent match as last event, from matchesState's bulk feed", async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      recordFor('m1', 'e1', { lastSeen: '2026-08-24T10:00:00Z' }),
    ])
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    expect(watchTable().textContent).toContain('5m ago')
  })

  it('an entry with no recent match reads an honest em dash, not "never"', async () => {
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])
    expect(watchTable().textContent).toContain('—')
  })

  it("opening a row's drawer shows the story headline, the verbatim last matching line and the pathway detail", async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      recordFor('m1', 'e1', {
        lastSeen: '2026-08-24T10:00:00Z',
        tuple: { source: { mac: 'aa:bb:cc:dd:ee:ff' }, destIp: '10.0.0.9', port: 22 },
        event: {
          id: 1,
          time: '2026-08-24T10:00:00Z',
          deviceId: 'router-1',
          sourceIp: '10.0.0.5',
          action: 'accept',
          ruleLabel: 'ssh-watch',
          chain: 'forward',
          raw: '10:00:00 firewall,info A|ssh-watch| forward: 10.0.0.5->10.0.0.9:22',
        },
      }),
    ])
    await renderWatchlist([
      entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' }, destIp: '10.0.0.9', ports: [22] }),
    ])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()

    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('Watching.')
    expect(drawer.textContent).toContain('aa:bb:cc:dd:ee:ff')
    expect(drawer.textContent).toContain('10:00:00 firewall,info A|ssh-watch| forward: 10.0.0.5->10.0.0.9:22')
    expect(drawer.textContent).toContain('ports 22')
  })

  it('a broken ring explains itself honestly, without inventing a window-silence reason', async () => {
    await renderWatchlist([entry('e1', 'broken watch', { source: { ip: '192.168.1.50' } })], { e1: 'no-logging' })

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()

    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('The ring is broken.')
    expect(drawer.textContent).toContain('No firewall rule mikroview can see is logging this pathway')
  })

  it('pause watch calls the enable toggle and refreshes the entry', async () => {
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    await fireEvent.click(within(watchTable()).getByRole('button', { name: 'pause watch' }))
    await settle()

    expect(setWatchlistEnabled).toHaveBeenCalledWith('e1', false)
    // watchlistState.setEnabled refreshes after a successful call, same
    // as every other mutation on this page.
    expect(vi.mocked(fetchWatchlistEntries)).toHaveBeenCalledTimes(2)
  })

  it('a paused entry offers "resume watch" instead', async () => {
    await renderWatchlist([entry('e1', 'paused watch', { enabled: false, source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()

    expect(within(watchTable()).getByRole('button', { name: 'resume watch' })).toBeTruthy()
  })

  it('open in stream is offered only for a scoped entry, and filters the live view to it', async () => {
    await renderWatchlist([
      entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } }),
      entry('e2', 'unscoped watch', {}),
    ])

    const rows = watchTable().querySelectorAll('.wt-row')
    await fireEvent.click(rows[0])
    flushSync()
    const scopedDrawer = watchTable().querySelectorAll('.wt-drawer')[0]
    const streamBtn = within(scopedDrawer as HTMLElement).getByRole('button', { name: /open in stream/ })
    await fireEvent.click(streamBtn)
    expect(appState.filters.srcQuery).toBe('aa:bb:cc:dd:ee:ff')
    expect(appState.view).toBe('live')

    await fireEvent.click(rows[0])
    await fireEvent.click(rows[1])
    flushSync()
    const unscopedDrawer = watchTable().querySelectorAll('.wt-drawer')[0]
    expect(
      within(unscopedDrawer as HTMLElement).queryByRole('button', { name: /open in stream/ }),
    ).toBeNull()
  })

  it('sorting and filtering the ratified table works independently of the Entries list below', async () => {
    await renderWatchlist([
      entry('e1', 'zebra watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } }),
      entry('e2', 'alpha watch', { source: { mac: '11:22:33:44:55:66' } }),
    ])

    const watchNames = () =>
      Array.from(watchTable().querySelectorAll('tbody tr.wt-row td.k')).map((el) => el.textContent?.trim())
    expect(watchNames()).toEqual(['alpha watch', 'zebra watch'])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /^watch/ }))
    flushSync()
    expect(watchNames()).toEqual(['zebra watch', 'alpha watch'])

    await fireEvent.input(within(watchTable()).getByLabelText('Filter watches by watch name'), {
      target: { value: 'alpha' },
    })
    flushSync()
    expect(watchNames()).toEqual(['alpha watch'])
  })
})
