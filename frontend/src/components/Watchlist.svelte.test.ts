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

import {
  createWatchlistEntry,
  fetchRecentMatches,
  fetchSuggestions,
  fetchWatchlistEntries,
  setWatchlistEnabled,
} from '../lib/api'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { appState } from '../lib/state.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import type { Suggestion, WatchNight, WatchlistCoverage, WatchlistEntry, WatchlistMatch } from '../lib/types'
import Watchlist from './Watchlist.svelte'

// #547 gave Suggestions a tab on the house tablist; round 30 (#700,
// #691) draws no sub-tab row at all on the docket's WATCHLIST screen --
// one flat watch table, nothing else (docs/design/concepts/round-30/
// shots/docket-watchlist.png, the-whole.html #s7's `#p-watch`). The tab
// row and the Suggestions panel it switched to are real, shipped
// capability, unmounted rather than deleted -- see the
// WATCHLIST_SUBTABS_ENABLED comment in Watchlist.svelte. Restoring it is
// tracked on #691; the switching behaviour this block used to cover
// (which candidates render, accept/hide/reset) lives in Suggestions.svelte
// itself and is untouched by this round.
describe('Watchlist Suggestions tab (#547)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
  })

  it('draws no tablist and no Suggestions panel -- round 30 fidelity, unmounted behind WATCHLIST_SUBTABS_ENABLED (#691)', () => {
    render(Watchlist)
    expect(screen.queryByRole('tablist', { name: 'Watchlist views' })).toBeNull()
    expect(screen.queryByRole('tab', { name: 'Suggestions' })).toBeNull()
    expect(document.getElementById('panel-suggestions')).toBeNull()
    // The flat watch table renders in its place, unconditionally.
    expect(document.querySelector('.watch-table-section')).toBeTruthy()
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
  const result = render(Watchlist)
  await settle()
  return result
}

// Several rounds of microtasks, then a flush. More than looks necessary
// on purpose: opening Matches chains two fetches (entries, then the
// matches that are named from them), so the rendered result is a few
// hops further down the queue than a single await would reach.
async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  flushSync()
}

// #584 gave Matches a tab of its own. Round 30 (#700, #691) draws no
// sub-tab row at all -- see the WATCHLIST_SUBTABS_ENABLED comment on
// Watchlist.svelte -- so the tab, its panel, and the "open in Matches"
// path from a watch's drawer are all unreachable through this component
// while the flag is off. The row-by-row rendering, the three empty-state
// sentences, the entry-name-removed guard and "load older" this block
// used to exercise all live in MatchesTab.svelte, untouched by this
// round; they are not re-tested here because there is no way to reach
// that component from Watchlist.svelte with the tab row gone.
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

  it('draws no Matches tab and no matches panel -- round 30 fidelity, unmounted behind WATCHLIST_SUBTABS_ENABLED (#691)', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')])
    expect(screen.queryAllByRole('tab').length).toBe(0)
    expect(document.getElementById('panel-matches')).toBeNull()
  })
})

// Issue #649 gave the "Entries" card list (record/edit/remove, the
// add-entry form above it) its own sort/filter toolbar. Round 30 (#700,
// #691) draws none of that second surface -- one flat watch table only
// -- so it is unmounted behind ADD_ENTRY_FORM_ENABLED/
// ENTRIES_TABLE_ENABLED (see Watchlist.svelte's flags comment). The
// ratified table's own sort and filter -- the thing the mockup actually
// draws -- is covered in "The ratified watch table (#676)" and "The
// watch window and its nightly memory (#680)" below, not here.
describe('Watchlist Entries sort and filter (#649)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    suggestState.candidates = []
    matchesState.reset()
  })

  it('draws no "Entries" card list and no add-entry form -- round 30 fidelity, unmounted behind ENTRIES_TABLE_ENABLED/ADD_ENTRY_FORM_ENABLED (#691)', async () => {
    await renderWatchlist([entry('e1', 'zebra watch'), entry('e2', 'alpha watch')])
    expect(document.getElementById('entries-section')).toBeNull()
    expect(document.querySelector('#panel-watchlist .card')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Add entry' })).toBeNull()
    // The ratified table is what's left in its place.
    expect(document.querySelector('.watch-table-section')).toBeTruthy()
  })
})

// Round 30 (#700, #691): the docket's WATCHLIST screen draws one flat
// watch table -- watch/boundary/window/state/last event, one header row,
// one filter row directly beneath it -- and nothing else (docs/design/
// concepts/round-30/shots/docket-watchlist.png, the-whole.html #s7's
// `#p-watch`). No page heading, no strap, anywhere (owner, 2026-08-31).
describe('Round-30 fidelity: the docket watchlist matches the mockup', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    suggestState.candidates = []
    matchesState.reset()
  })

  it('draws no page heading and no "Manage entries" strap', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')])
    expect(screen.queryByText('Watches')).toBeNull()
    expect(screen.queryByText('Manage entries')).toBeNull()
    expect(screen.queryByText(/Watch attempts against specific ports/)).toBeNull()
  })

  it('has exactly one header row and one filter row on the watch table -- not a standalone sort/filter toolbar above a second, duplicate header', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')])
    const table = document.querySelector('.watch-table-section table') as HTMLTableElement
    expect(table).toBeTruthy()
    expect(table.tHead?.rows.length).toBe(2)
    expect(table.tHead?.rows[0].querySelectorAll('th').length).toBe(6)
    expect(table.tHead?.rows[1].classList.contains('filters')).toBe(true)
    expect(document.querySelector('.watch-table-section > .sortbar')).toBeNull()
    expect(document.querySelector('.watch-table-section > .filterbar')).toBeNull()
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

  it('renders watch, boundary, window and state for every entry -- window reads "always" without one', async () => {
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
    // None of these three carries a window, so "always" is the honest
    // answer for each -- see the #680 block at the end of this file for
    // what a row with one reads instead.
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

// #680: the window and the seven nights of memory behind it. Two
// surfaces, and one rule that outranks both -- a night mikroview did not
// observe is never reported as empty.
describe('The watch window and its nightly memory (#680)', () => {
  function watchTable(): HTMLElement {
    return document.querySelector('.watch-table-section') as HTMLElement
  }

  function nights(...states: WatchNight[]['0']['state'][]): WatchNight[] {
    return states.map((state, i) => ({
      opened: `2026-08-${String(17 + i).padStart(2, '0')}T21:00:00Z`,
      state,
    }))
  }

  const quietHours = { start: '22:00', end: '06:00', zone: 'Europe/London' }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    suggestState.candidates = []
    matchesState.reset()
    appState.now = new Date('2026-08-24T10:05:00Z').getTime()
  })

  it("renders the entry's window in the window column, zone and all", async () => {
    await renderWatchlist([entry('e1', 'the nas', { window: quietHours })])
    expect(watchTable().textContent).toContain('22:00–06:00 Europe/London')
    expect(watchTable().textContent).not.toContain('always')
  })

  it('still reads "always" for an entry with no window', async () => {
    await renderWatchlist([entry('e1', 'the nas')])
    expect(watchTable().textContent).toContain('always')
  })

  it('filters and sorts on the rendered window', async () => {
    await renderWatchlist([
      entry('e1', 'the nas', { window: quietHours }),
      entry('e2', 'the camera' ),
    ])
    await fireEvent.input(within(watchTable()).getByLabelText('Filter watches by window'), {
      target: { value: '22:00' },
    })
    flushSync()
    const names = Array.from(watchTable().querySelectorAll('tbody tr.wt-row td.k')).map((el) =>
      el.textContent?.trim(),
    )
    expect(names).toEqual(['the nas'])
  })

  it("shows the drawer's nightly summary in the ratified wording", async () => {
    await renderWatchlist([
      entry('e1', 'the nas', {
        window: quietHours,
        nights: nights('kept', 'kept', 'kept', 'kept', 'kept', 'empty', 'empty'),
      }),
    ])
    await fireEvent.click(watchTable().querySelector('tr.wt-row') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('the last seven nights')
    expect(drawer.textContent).toContain('five kept nights · two empty')
  })

  it('grows the third clause only when a night could not be observed', async () => {
    await renderWatchlist([
      entry('e1', 'the nas', {
        window: quietHours,
        nights: nights('kept', 'kept', 'kept', 'kept', 'kept', 'empty', 'not observed'),
      }),
    ])
    await fireEvent.click(watchTable().querySelector('tr.wt-row') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('five kept nights · one empty · one not observed')
  })

  it('never words an unobserved night as empty', async () => {
    await renderWatchlist([
      entry('e1', 'the nas', {
        window: quietHours,
        nights: nights('not observed', 'not observed', 'not observed'),
      }),
    ])
    await fireEvent.click(watchTable().querySelector('tr.wt-row') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('three nights not observed')
    expect(drawer.textContent).not.toContain('empty')
  })

  it('shows no nightly line at all for a watch with no nights recorded yet', async () => {
    await renderWatchlist([entry('e1', 'the nas', { window: quietHours })])
    await fireEvent.click(watchTable().querySelector('tr.wt-row') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).not.toContain('the last seven nights')
  })

  it('shows a recorded ring break as ring broken, with the window as the reason', async () => {
    await renderWatchlist([
      entry('e1', 'the nas', {
        window: quietHours,
        nights: nights('kept', 'empty', 'empty'),
        ring: { broken: true, since: '2026-08-23T05:00:00Z', reason: 'no-match-in-window' },
      }),
    ])
    expect(watchTable().textContent).toContain('○ ring broken — nothing in the window')

    await fireEvent.click(watchTable().querySelector('tr.wt-row') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('The ring is broken.')
    expect(drawer.textContent).toContain("Nothing has matched inside this watch's window")
    // The recorded break knows which window closed empty, which is why it
    // is written down at the break rather than worked out on read.
    expect(drawer.textContent).toContain('since 1d ago')
    expect(drawer.textContent).toContain('Nights mikroview could not watch are not counted against it.')
  })

  // paused > no logging visible > ring broken > watching. A watch no rule
  // logs cannot be judged on nightly presence at all, so coverage wins
  // over the recorded ring; a paused watch wins over both.
  it('ranks no-logging above a recorded ring break', async () => {
    await renderWatchlist(
      [
        entry('e1', 'dark watch', {
          window: quietHours,
          ring: { broken: true, since: '2026-08-23T05:00:00Z', reason: 'no-match-in-window' },
        }),
      ],
      { e1: 'no-logging' },
    )
    expect(watchTable().textContent).toContain('○ ring broken — no logging visible')
    expect(watchTable().textContent).not.toContain('nothing in the window')
  })

  it('ranks paused above everything', async () => {
    await renderWatchlist(
      [
        entry('e1', 'paused watch', {
          enabled: false,
          window: quietHours,
          ring: { broken: true, since: '2026-08-23T05:00:00Z', reason: 'no-match-in-window' },
        }),
      ],
      { e1: 'no-logging' },
    )
    expect(watchTable().textContent).toContain('‖ paused')
    expect(watchTable().textContent).not.toContain('ring broken')
  })
})

// #724's second click: a dial panel row on the topography hands off which
// watch it was through topologyNavState.pendingWatchId
// (topologyNav.svelte.ts) rather than just switching the view -- this is
// the tab side that consumes it.
describe('opening a watch drawer from the topography dial (#724)', () => {
  function watchTable(): HTMLElement {
    return document.querySelector('.watch-table-section') as HTMLElement
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    suggestState.candidates = []
    matchesState.reset()
    topologyNavState.pendingWatchId = null
  })

  it("opens that watch's drawer on arrival and clears the pending selection", async () => {
    const entries = [
      entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } }),
      entry('e2', 'NAS watch', { source: { mac: '11:22:33:44:55:66' } }),
    ]
    // In real use, watchlistState.entries is already populated by
    // App.svelte's own app-wide poll well before a dial row exists to be
    // clicked -- Watchlist.svelte's own onMount refresh only ever
    // confirms/replaces that with the same data, never starts the tab
    // from an empty list. Set directly so the test's timing matches.
    watchlistState.entries = entries
    topologyNavState.pendingWatchId = 'e2'
    await renderWatchlist(entries)

    const drawers = watchTable().querySelectorAll('.wt-drawer')
    expect(drawers.length).toBe(1)
    // The drawer sits directly beneath the row it belongs to -- the NAS
    // watch's row, not the SSH watch's.
    const rows = watchTable().querySelectorAll('tbody tr')
    const nasRowIndex = Array.from(rows).findIndex((r) => r.textContent?.includes('NAS watch'))
    expect(rows[nasRowIndex + 1]?.classList.contains('wt-drawer')).toBe(true)
    expect(topologyNavState.pendingWatchId).toBeNull()
  })

  it('a pending id that matches nothing lands on the tab with no drawer open and no error', async () => {
    const entries = [entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })]
    watchlistState.entries = entries
    topologyNavState.pendingWatchId = 'no-such-watch'
    await renderWatchlist(entries)

    expect(watchTable().querySelector('.wt-drawer')).toBeNull()
    expect(topologyNavState.pendingWatchId).toBeNull()
  })

  // The part that rots if it's missed (#724's own Care note): once the
  // dial's row has been followed and its drawer opened, a later, ordinary
  // visit to this tab -- not through the dial -- must not silently reopen
  // it. Simulated the same way the deck's own keep-alive card lifecycle
  // would produce it: the tab's component is torn down when the docket
  // scrolls out of view, and freshly built again on a later visit.
  it('does not reopen a drawer on a later, unrelated visit to the tab', async () => {
    const entries = [entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })]
    watchlistState.entries = entries
    topologyNavState.pendingWatchId = 'e1'
    const { unmount } = await renderWatchlist(entries)
    expect(watchTable().querySelector('.wt-drawer')).toBeTruthy()
    expect(topologyNavState.pendingWatchId).toBeNull()

    unmount()

    // A plain, later visit to the watchlist tab -- nothing pending this
    // time. entries set directly again first, same reasoning as above:
    // real app state doesn't start this second mount from empty either.
    watchlistState.entries = entries
    await renderWatchlist(entries)

    expect(watchTable().querySelector('.wt-drawer')).toBeNull()
  })
})

// #641: a flag's "watch for this" opens *this* form, prefilled, and the
// operator lands back in the inbox whichever way they leave it. The
// issue is explicit that the round trip has to be seamless -- taking a
// watcher must never cost a manual switch back.
describe('a watcher offered by a resolved flag (#641)', () => {
  const fromFlag = {
    who: '192.168.1.50',
    toward: '192.168.1.10 · :445',
    mode: 'expect' as const,
    provenance: 'From the last firing window, 6 of at least 14 pairs — IP-bound, so it stops matching if this device gets a new address.',
    returnTo: 'flags' as const,
  }

  function draftRow(): HTMLElement {
    return document.querySelector('.wt-drawer.wt-draft') as HTMLElement
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    vi.mocked(createWatchlistEntry).mockResolvedValue(undefined as never)
    suggestState.candidates = []
    matchesState.reset()
    watchlistState.entries = []
    topologyNavState.pendingWatchDraft = null
    appState.view = 'watchlist'
  })

  it('opens the existing entry form prefilled from the flag, and says where the values came from', async () => {
    topologyNavState.pendingWatchDraft = { ...fromFlag }
    await renderWatchlist([])

    const draft = draftRow()
    expect(draft).toBeTruthy()
    expect((within(draft).getByLabelText('Who this watch scopes to') as HTMLInputElement).value).toBe('192.168.1.50')
    expect((within(draft).getByLabelText('Toward') as HTMLInputElement).value).toBe('192.168.1.10 · :445')
    expect(draft.querySelector('.wf-prov')?.textContent).toContain('6 of at least 14 pairs')
    expect(topologyNavState.pendingWatchDraft).toBeNull()
  })

  it('returns to the flags inbox when the watch is saved', async () => {
    topologyNavState.pendingWatchDraft = { ...fromFlag }
    await renderWatchlist([])

    await fireEvent.click(within(draftRow()).getByRole('button', { name: /start watching/ }))
    await settle()

    expect(createWatchlistEntry).toHaveBeenCalled()
    expect(appState.view).toBe('flags')
    expect(document.querySelector('.wt-draft')).toBeNull()
  })

  // Declining is the operator's to make, and it costs them nothing: the
  // flag stays resolved and they are put back where they were.
  it('returns to the flags inbox when the watch is discarded', async () => {
    topologyNavState.pendingWatchDraft = { ...fromFlag }
    await renderWatchlist([])

    await fireEvent.click(within(draftRow()).getByRole('button', { name: /discard/ }))
    await settle()

    expect(createWatchlistEntry).not.toHaveBeenCalled()
    expect(appState.view).toBe('flags')
    expect(document.querySelector('.wt-draft')).toBeNull()
  })

  // A failed save keeps the operator on the form with the error, rather
  // than bouncing them back to the inbox as though it had worked.
  it('stays put when the server refuses the watch', async () => {
    vi.mocked(createWatchlistEntry).mockResolvedValue('a non-inverted entry must watch at least one port' as never)
    topologyNavState.pendingWatchDraft = { ...fromFlag }
    await renderWatchlist([])

    await fireEvent.click(within(draftRow()).getByRole('button', { name: /start watching/ }))
    await settle()

    expect(appState.view).toBe('watchlist')
    expect(draftRow().querySelector('.error')?.textContent).toContain('at least one port')
  })

  // A draft the operator opened here has no provenance and nowhere to be
  // sent back to -- it closes where it stands, as it always did.
  it('leaves a draft opened on this page where it is', async () => {
    topologyNavState.pendingNewWatch = {}
    await renderWatchlist([])

    expect(draftRow().querySelector('.wf-prov')).toBeNull()
    await fireEvent.click(within(draftRow()).getByRole('button', { name: /discard/ }))
    await settle()

    expect(appState.view).toBe('watchlist')
  })
})
