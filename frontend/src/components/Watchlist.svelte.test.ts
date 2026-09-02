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
  acceptSuggestion,
  fetchRecentMatches,
  fetchSuggestions,
  fetchWatchlistEntries,
  fetchWatchlistMatches,
  hideSuggestion,
  resetSuggestions,
  setWatchlistEnabled,
  unhideSuggestion,
} from '../lib/api'
import { watchlistState } from '../lib/watchlist.svelte'
import { suggestState } from '../lib/suggest.svelte'
import { matchesState } from '../lib/matches.svelte'
import { appState } from '../lib/state.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import type { Suggestion, WatchNight, WatchlistCoverage, WatchlistEntry, WatchlistMatch } from '../lib/types'
import Watchlist from './Watchlist.svelte'

// #547/#584 gave Suggestions and Matches tabs of their own on the house
// tablist. Round 33 (#771) goes further than round 30's "no sub-tab row"
// fidelity check ever did: Suggestions.svelte and MatchesTab.svelte are
// deleted outright (AGENTS.md's "removals are wholesale"), and what they
// carried is redrawn as two things directly inside this table -- a match
// list in each watch's own drawer, and a suggestion body underneath the
// watches. #712 records the tab-era coverage (empty states, load-older
// paging, entry-name linking) as lost when the old components' tests went
// with them; the two describes below restore it against the surface it
// actually lives in now.
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

// A suggestion candidate fixture, in round 33's "off" default -- see
// Suggestion's own doc comment (lib/types.ts) for what each field means.
function suggestion(
  id: string,
  kind: 'device' | 'port' | 'addressList',
  overrides: Partial<Suggestion> = {},
): Suggestion {
  return {
    id,
    kind,
    status: 'off',
    name: id,
    justification: `${id} justification`,
    routerDevice: 'rb5009',
    firstSeen: '2026-08-24T09:00:00Z',
    updatedAt: '2026-08-24T09:00:00Z',
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
// on purpose: opening a drawer or accepting/resetting a suggestion chains
// two or more fetches (entries, suggestions, and whatever the mutation
// itself refreshes), so the rendered result is a few hops further down
// the queue than a single await would reach.
async function settle() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  flushSync()
}

// Round 33's suggestion body: a second tbody (#sugg) hung directly off
// the watch table, under the watches, in the same row grammar -- "a
// suggestion is a watch that has not been said yes to" (the component's
// own section comment). No sub-tab reaches it; it is just there.
describe('The suggestion body under the watches (#771)', () => {
  function watchTable(): HTMLElement {
    return document.querySelector('.watch-table-section') as HTMLElement
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    appState.now = new Date('2026-08-24T10:05:00Z').getTime()
  })

  // Round 30's fidelity check (no tab strip at all) still holds under
  // round 33 -- worth locking in on its own, since #712's other gaps are
  // about the body this describe otherwise exercises.
  it('draws no tablist and no Suggestions panel', async () => {
    await renderWatchlist([])
    expect(screen.queryByRole('tablist', { name: 'Watchlist views' })).toBeNull()
    expect(screen.queryByRole('tab', { name: 'Suggestions' })).toBeNull()
    expect(document.getElementById('panel-suggestions')).toBeNull()
  })

  it('counts only the open candidates in its heading, and names the routers that pushed any of them', async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([
      suggestion('s1', 'device', { status: 'off', routerDevice: 'rb5009' }),
      suggestion('s2', 'port', { status: 'off', routerDevice: 'hap-ax2' }),
      // Set aside, not open -- and the router it named still counts
      // toward "from what X and Y pushed" even though it does not count
      // toward the open number.
      suggestion('s3', 'device', { status: 'hide', routerDevice: 'rb5009' }),
    ])
    await renderWatchlist([])

    const heading = watchTable().querySelector('.sdiv .sdl') as HTMLElement
    expect(heading.textContent).toContain('mikroview suggests · from what rb5009 and hap-ax2 pushed')
    expect(heading.querySelector('b')?.textContent).toBe('2')
  })

  it('keeps set-aside suggestions out of the list until "show them" is clicked, and the pill then reads "hide them"', async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([
      suggestion('s1', 'device', { status: 'off' }),
      suggestion('s2', 'port', { status: 'hide' }),
    ])
    await renderWatchlist([])

    expect(document.getElementById('suggestion-s1')).toBeTruthy()
    expect(document.getElementById('suggestion-s2')).toBeNull()

    const toggle = within(watchTable()).getByRole('button', { name: /set aside/ })
    expect(toggle.textContent).toContain('show them')

    await fireEvent.click(toggle)
    flushSync()

    expect(document.getElementById('suggestion-s2')).toBeTruthy()
    expect(toggle.textContent).toContain('hide them')
  })

  it("a device suggestion's drawer offers to watch it and learn first", async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([suggestion('s1', 'device', { status: 'off' })])
    await renderWatchlist([])

    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement
    expect(within(drawer).getByRole('button', { name: 'watch it — it learns first' })).toBeTruthy()
  })

  it("a port suggestion's drawer offers to watch it as every attempt being a match", async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([suggestion('s1', 'port', { status: 'off', ports: [23] })])
    await renderWatchlist([])

    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement
    expect(within(drawer).getByRole('button', { name: 'watch it — every attempt is a match' })).toBeTruthy()
  })

  it('a stale suggestion leads with "let it go", keeping the accept verb quiet beside it', async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([suggestion('s1', 'device', { status: 'off', stale: true })])
    await renderWatchlist([])

    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement
    expect(within(drawer).getByRole('button', { name: 'let it go' })).toBeTruthy()
    expect(within(drawer).getByRole('button', { name: 'watch it anyway' })).toBeTruthy()
  })

  it("a set-aside suggestion's drawer offers to bring it back, and calls unhideSuggestion", async () => {
    const s1 = suggestion('s1', 'device', { status: 'hide' })
    vi.mocked(fetchSuggestions).mockResolvedValue([s1])
    vi.mocked(unhideSuggestion).mockResolvedValue({ ...s1, status: 'off' })
    await renderWatchlist([])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /set aside/ }))
    flushSync()
    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement

    await fireEvent.click(within(drawer).getByRole('button', { name: 'bring it back' }))
    await settle()
    expect(unhideSuggestion).toHaveBeenCalledWith('s1')
  })

  it('"not this" hides an open suggestion by calling hideSuggestion', async () => {
    const s1 = suggestion('s1', 'port', { status: 'off' })
    vi.mocked(fetchSuggestions).mockResolvedValue([s1])
    vi.mocked(hideSuggestion).mockResolvedValue({ ...s1, status: 'hide' })
    await renderWatchlist([])

    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement

    await fireEvent.click(within(drawer).getByRole('button', { name: 'not this' }))
    await settle()
    expect(hideSuggestion).toHaveBeenCalledWith('s1')
  })

  it('accepting a suggestion calls acceptSuggestion and refreshes the watch list so the new entry shows up', async () => {
    const s1 = suggestion('s1', 'port', { status: 'off' })
    const newEntry = entry('e-new', 'new watch')
    vi.mocked(fetchSuggestions).mockResolvedValue([s1])
    vi.mocked(acceptSuggestion).mockResolvedValue({ candidate: { ...s1, status: 'on', entryId: 'e-new' }, entry: newEntry })
    await renderWatchlist([])
    // Accept really does create a server-side entry -- suggestState only
    // reloads the candidate pool, so watchlistState has to be refetched
    // separately for the accepted row to appear among the watches at all
    // (acceptOne's own comment on Watchlist.svelte).
    vi.mocked(fetchWatchlistEntries).mockResolvedValue({ entries: [newEntry], coverage: {} })

    await fireEvent.click(document.getElementById('suggestion-s1') as HTMLElement)
    flushSync()
    const drawer = watchTable().querySelector('#sugg .wt-drawer') as HTMLElement
    await fireEvent.click(within(drawer).getByRole('button', { name: 'watch it — every attempt is a match' }))
    await settle()

    expect(acceptSuggestion).toHaveBeenCalledWith('s1')
    expect(vi.mocked(fetchWatchlistEntries)).toHaveBeenCalledTimes(2)
  })

  it('start over is arm-then-confirm: the first click only arms it, and "Started over." appears once the second click succeeds', async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([suggestion('s1', 'device', { status: 'off' })])
    vi.mocked(resetSuggestions).mockResolvedValue([])
    await renderWatchlist([entry('e1', 'SSH watch')])

    const btn = within(watchTable()).getByRole('button', { name: /wipe every watch/ })
    await fireEvent.click(btn)
    flushSync()
    expect(btn.textContent).toContain('confirm — every watch goes, and it suggests afresh')
    expect(resetSuggestions).not.toHaveBeenCalled()

    // The entries really are gone server-side once reset succeeds, so the
    // refetch that follows has to answer empty for "Started over." to
    // show in place of the ordinary empty row (see Watchlist.svelte's own
    // comment on startedOverAt).
    vi.mocked(fetchWatchlistEntries).mockResolvedValue({ entries: [], coverage: {} })
    await fireEvent.click(btn)
    await settle()

    expect(resetSuggestions).toHaveBeenCalledTimes(1)
    expect(watchTable().textContent).toContain('Started over.')
  })

  it("suggestions are not affected by the watch table's own sort and filter boxes", async () => {
    vi.mocked(fetchSuggestions).mockResolvedValue([suggestion('s1', 'device', { status: 'off', name: 'iot lease' })])
    await renderWatchlist([entry('e1', 'SSH watch')])

    await fireEvent.input(within(watchTable()).getByLabelText('Filter watches by watch name'), {
      target: { value: 'nothing matches this' },
    })
    flushSync()

    expect(watchTable().textContent).toContain('No watches match these filters.')
    expect(document.getElementById('suggestion-s1')).toBeTruthy()
  })
})

// Round 33's match list: what a watch's own drawer shows in place of the
// verbatim raw log line round 30 drew there. Fed by matchesState's bulk
// feed for the newest matches, and `older ▸` for anything further back --
// see loadOlderMatches's own comment on Watchlist.svelte for why the
// per-entry filtering has to happen client-side.
describe('The match list in a watch drawer (#771)', () => {
  function watchTable(): HTMLElement {
    return document.querySelector('.watch-table-section') as HTMLElement
  }

  // Same shape as recordFor, but lets each record carry its own count and
  // rule label without having to restate every other event field inline
  // at every call site.
  function matchRecord(id: string, entryId: string, lastSeen: string, count: number, ruleLabel: string): WatchlistMatch {
    return recordFor(id, entryId, {
      lastSeen,
      count,
      event: {
        id: 1,
        time: lastSeen,
        deviceId: 'router-1',
        sourceIp: '192.168.1.1',
        action: 'drop',
        ruleLabel,
        chain: 'forward',
        raw: 'raw line',
      },
    })
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchSuggestions).mockResolvedValue([])
    vi.mocked(fetchRecentMatches).mockResolvedValue([])
    watchlistState.entries = []
    watchlistState.coverage = {}
    suggestState.candidates = []
    matchesState.reset()
    appState.now = new Date('2026-08-24T10:05:00Z').getTime()
  })

  // Round 30's fidelity check, still true: no Matches tab, no matches
  // panel. #712's own named gaps (empty states, load-older paging,
  // entry-name linking) are what the rest of this describe restores.
  it('draws no Matches tab and no matches panel', async () => {
    await renderWatchlist([entry('e1', 'SSH watch')])
    expect(screen.queryAllByRole('tab').length).toBe(0)
    expect(document.getElementById('panel-matches')).toBeNull()
  })

  it('shows the honest empty state, and offers no older ▸ even though the entry is scoped', async () => {
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.textContent).toContain('Nothing in the recent log yet.')
    expect(within(drawer).queryByRole('button', { name: /older/ })).toBeNull()
  })

  it('renders at most three match lines, newest first, each with its own count and rule label, and the caption reads "last 3 of N"', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      matchRecord('m1', 'e1', '2026-08-24T10:00:00Z', 1, 'r1'),
      matchRecord('m2', 'e1', '2026-08-24T10:01:00Z', 2, 'r2'),
      matchRecord('m3', 'e1', '2026-08-24T10:02:00Z', 3, 'r3'),
      matchRecord('m4', 'e1', '2026-08-24T10:03:00Z', 4, 'r4'),
    ])
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.querySelector('.matches .lab')?.textContent).toContain('last 3 of 4')

    const items = Array.from(drawer.querySelectorAll('ul.mlist li'))
    expect(items.length).toBe(3)
    expect(items[0].textContent).toContain('4×')
    expect(items[0].textContent).toContain('r4')
    expect(items[1].textContent).toContain('3×')
    expect(items[2].textContent).toContain('2×')
    // The oldest of the four is the one that doesn't fit -- proof this is
    // "newest three", not just "first three, whatever order they arrived
    // in".
    expect(drawer.textContent).not.toContain('r1')
  })

  it("older ▸ asks for this entry's own mac with limit 20, and keeps only the records that are actually this entry's", async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([matchRecord('m1', 'e1', '2026-08-24T10:00:00Z', 1, 'r1')])
    vi.mocked(fetchWatchlistMatches).mockResolvedValue([
      matchRecord('m2', 'e1', '2026-08-24T09:00:00Z', 1, 'r2'),
      // The backend has no per-entry filter (loadOlderMatches's own
      // comment on Watchlist.svelte, tracked against #691) -- it answers
      // by mac/ip, which can carry another entry's record along for the
      // ride. Filtering that out is this component's job, not the
      // server's, so a foreign record in the same page must never render.
      matchRecord('m3', 'other-entry', '2026-08-24T08:00:00Z', 1, 'r3'),
    ])
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    let drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    await fireEvent.click(within(drawer).getByRole('button', { name: 'older ▸' }))
    await settle()

    expect(fetchWatchlistMatches).toHaveBeenCalledWith(
      expect.objectContaining({ mac: 'aa:bb:cc:dd:ee:ff', limit: 20 }),
    )
    drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.querySelectorAll('ul.mlist li').length).toBe(2)
    expect(drawer.textContent).toContain('r2')
    expect(drawer.textContent).not.toContain('r3')
  })

  it('a full page carrying none of this entry\'s records does not mark it exhausted -- the pager keeps walking back for one that does (#819)', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([matchRecord('m1', 'e1', '2026-08-24T10:00:00Z', 1, 'r1')])
    // A full OLDER_PAGE page that happens to be entirely another watch
    // sharing this mac, then a full page that is all this entry's own --
    // the exact shape loadOlderMatches's own comment on Watchlist.svelte
    // says a shared identity can produce.
    const otherPage = Array.from({ length: 20 }, (_, i) =>
      matchRecord(`other-${i}`, 'other-entry', `2026-08-24T09:${String(40 - i).padStart(2, '0')}:00Z`, 1, 'ro'),
    )
    const minePage = Array.from({ length: 20 }, (_, i) =>
      matchRecord(`mine-${i}`, 'e1', `2026-08-24T08:${String(40 - i).padStart(2, '0')}:00Z`, 1, 'rm'),
    )
    vi.mocked(fetchWatchlistMatches).mockResolvedValueOnce(otherPage).mockResolvedValueOnce(minePage)
    await renderWatchlist([entry('e1', 'SSH watch', { source: { mac: 'aa:bb:cc:dd:ee:ff' } })])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    let drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    await fireEvent.click(within(drawer).getByRole('button', { name: 'older ▸' }))
    await settle()

    // One click did not stop at the first, entirely-foreign page -- it
    // asked again and found this entry's own records further back.
    expect(fetchWatchlistMatches).toHaveBeenCalledTimes(2)
    drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(drawer.querySelector('.matches .lab')?.textContent).toContain('last 3 of 21')
    // A full page that happened to carry none of this entry's records
    // must never read as "nothing older": the control stays offered.
    expect(within(drawer).getByRole('button', { name: 'older ▸' })).toBeTruthy()
  })

  it('offers no older ▸ for an entry with neither a mac nor an ip -- there is nothing to query the match log by', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([matchRecord('m1', 'e1', '2026-08-24T10:00:00Z', 1, 'r1')])
    await renderWatchlist([entry('e1', 'unscoped watch', {})])

    await fireEvent.click(within(watchTable()).getByRole('button', { name: /Open the drawer/ }))
    flushSync()
    const drawer = watchTable().querySelector('.wt-drawer') as HTMLElement
    expect(within(drawer).queryByRole('button', { name: /older/ })).toBeNull()
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

  it("opening a row's drawer shows the story headline, the pathway detail, and its newest match line", async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      recordFor('m1', 'e1', {
        lastSeen: '2026-08-24T10:00:00Z',
        count: 3,
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
    expect(drawer.textContent).toContain('ports 22')
    // Round 33 replaced the verbatim raw log line with a short match
    // list -- the identity shown is the matching event's own
    // (tuple.source.mac here), never the entry's possibly-unscoped
    // Source (see matchSource's own comment on Watchlist.svelte).
    const mlist = drawer.querySelector('ul.mlist') as HTMLElement
    expect(mlist.textContent).toContain('aa:bb:cc:dd:ee:ff → 10.0.0.9')
    expect(mlist.textContent).toContain(':22')
    expect(mlist.textContent).toContain('3× · ssh-watch')
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
