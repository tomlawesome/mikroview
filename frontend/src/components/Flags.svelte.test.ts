// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// Flags.svelte itself makes no requests directly -- these stop the
// store it pulls in (flagsState) from reaching for the network when it
// initialises under jsdom.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchExclusions: vi.fn(async () => []),
  removeExclusion: vi.fn(),
  fetchFlagEpisode: vi.fn(async () => ({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })),
  // The learning shelf (#642) reads baseline warm-up through
  // detectorSettingsState, whose refresh() calls these two.
  fetchDefinitions: vi.fn(async () => ({ definitions: [], coverageEvidence: { complete: true } })),
  updateDefinition: vi.fn(),
}))

import {
  clearFlag,
  clearFlagPermanent,
  deleteFlagVerdict,
  fetchDefinitions,
  fetchExclusions,
  fetchFlagEpisode,
  removeExclusion,
  setFlagVerdict,
} from '../lib/api'
import { flagsState } from '../lib/flags.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { authState } from '../lib/auth.svelte'
import { appState } from '../lib/state.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import type { Flag } from '../lib/types'

// jsdom has no window.matchMedia -- polyfilled before the dynamic import
// below (a static import would already have run this file's top-level
// code too late to matter), the same fix LiveTable.svelte.test.ts needs.
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

function testFlag(overrides: Partial<Flag> = {}): Flag {
  return {
    id: 'f1',
    type: 'port_scan',
    target: '203.0.113.9',
    detail: 'd',
    count: 1,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    cleared: false,
    ...overrides,
  }
}

// #688: round 29 (`#s7`) ratified a *table* -- flag · where · evidence ·
// count · age -- one row per open flag, each opening as a drawer beneath
// itself. What shipped until now was a card grid with those five words
// dressed as sort heads standing over it, which is why the ratified
// language #678 added landed inside the wrong container. These
// assertions replace the card-grid ones: they pin the structure the
// record draws, so the same substitution cannot happen again quietly.
describe('the ratified flags table (#688, round 29)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [testFlag({ target: '198.51.100.77', detail: '20 distinct ports in 40 s', count: 20 })]
  })

  it('draws the five ratified columns in order, plus CALL IT (#780)', () => {
    render(Flags)
    flushSync()

    const heads = Array.from(document.querySelectorAll('table.ftable thead tr:first-child th')).map((th) =>
      th.textContent?.replace(/[▲▼]/g, '').trim(),
    )
    expect(heads).toEqual(['flag', 'where', 'evidence', 'count', 'age', 'call it'])
  })

  it('renders one row per open flag, carrying flag, where, evidence, count and age', () => {
    render(Flags)
    flushSync()

    const rows = document.querySelectorAll('tr.frow')
    expect(rows).toHaveLength(1)
    const cells = Array.from(rows[0].querySelectorAll('td')).map((td) => td.textContent?.trim())
    expect(cells[0]).toBe('✱ Port scan')
    expect(cells[1]).toBe('198.51.100.77')
    expect(cells[2]).toBe('20 distinct ports in 40 s')
    expect(cells[3]).toBe('20×')
    expect(cells[4]).toBeTruthy()
  })

  it('wears the flag type’s own family ink, as stripe and mark, on the row and its drawer', async () => {
    render(Flags)
    flushSync()

    // port_scan is the scan family (lib/flagPalette.ts, the record's own
    // six hexes) -- the ink rides as --ft, which the CSS turns into the
    // left stripe and the mark's colour on both row and drawer.
    const row = document.querySelector('tr.frow') as HTMLElement
    expect(row.getAttribute('style')).toContain('#ff9e64')
    expect(row.querySelector('.fmark')?.textContent?.trim().startsWith('✱')).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()
    expect((document.querySelector('tr.drawer') as HTMLElement).getAttribute('style')).toContain('#ff9e64')
  })

  it('opens a drawer as the very next row beneath its own row, not a panel inside a card', async () => {
    render(Flags)
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()

    const row = document.querySelector('tr.frow') as HTMLElement
    expect(row.classList.contains('open')).toBe(true)
    expect(row.nextElementSibling?.classList.contains('drawer')).toBe(true)
    expect(row.nextElementSibling?.querySelector('td')?.getAttribute('colspan')).toBe('6')
  })

  it('opens the drawer from a click anywhere on the row, as the record has it', async () => {
    render(Flags)
    flushSync()

    await fireEvent.click(document.querySelector('tr.frow') as HTMLElement)
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeTruthy()
  })

  it('leaves no card grid behind', () => {
    render(Flags)
    flushSync()

    expect(document.querySelector('.card')).toBeNull()
    expect(document.querySelector('.card-grid')).toBeNull()
  })
})

// A custom detector's type is a string the sixteen-entry label table
// cannot know. familyOf/labelFor fall back rather than index blindly;
// indexing FLAG_FAMILIES directly used to crash the render on the first
// such flag, and because the deck mounts every scene, that one flag took
// down every scene at once. This pins the render surviving.
describe('a custom detection type renders without crashing', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    authState.state = 'authenticated'
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

  it('shows the row with the author-named type as its label', () => {
    render(Flags)
    flushSync()
    expect(screen.getByText('▲ live-custom-detection watch')).toBeTruthy()
  })
})

// #649, kept in the shape round 29 draws it: the column head *is* the
// sort control, and the quiet dashed row beneath the heads is the
// filter.
describe('Flags table sort and filter (#649)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [
      testFlag({
        id: 'f1',
        type: 'port_scan',
        target: '198.51.100.1',
        detail: 'twenty ports',
        count: 20,
        firstSeen: '2026-01-01T00:00:00Z',
        lastSeen: '2026-01-01T00:00:00Z',
      }),
      testFlag({
        id: 'f2',
        type: 'outbound_anomaly',
        target: '198.51.100.2',
        detail: 'mail server',
        count: 3,
        firstSeen: '2026-01-01T00:10:00Z',
        lastSeen: '2026-01-01T00:10:00Z',
      }),
    ]
  })

  // Was `.card .target`; the wheres now sit in the table's own second
  // column, so this reads them from there. Same flags, same order
  // expectations -- only the container changed.
  function rowWheres() {
    return Array.from(document.querySelectorAll('tr.frow td.k')).map((el) => el.textContent?.trim())
  }

  it('defaults to newest first (age ascending), matching the order this replaces', () => {
    render(Flags)
    flushSync()
    expect(rowWheres()).toEqual(['198.51.100.2', '198.51.100.1'])
  })

  it('clicking a sort head (count) sorts by it, and again reverses', async () => {
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /^count/ }))
    flushSync()
    expect(rowWheres()).toEqual(['198.51.100.2', '198.51.100.1'])

    await fireEvent.click(screen.getByRole('button', { name: /^count/ }))
    flushSync()
    expect(rowWheres()).toEqual(['198.51.100.1', '198.51.100.2'])
  })

  it('a filter on evidence narrows the table to matching flags only', async () => {
    render(Flags)
    flushSync()

    await fireEvent.input(screen.getByLabelText('Filter by evidence'), { target: { value: 'mail' } })
    flushSync()

    expect(rowWheres()).toEqual(['198.51.100.2'])
  })

  it('says plainly when nothing matches the filters, rather than the "nothing open" empty state', async () => {
    render(Flags)
    flushSync()

    await fireEvent.input(screen.getByLabelText('Filter by where'), { target: { value: 'nobody-home' } })
    flushSync()

    expect(screen.getByText('No flags match these filters.')).toBeTruthy()
    expect(screen.queryByText('Nothing open.')).toBeNull()
  })
})

// #653's tiers, on the ratified surface: clearing a flag is a normal
// operational action (user tier), not an owner-level one -- only a
// viewer, the tier below user, must see none of it. Hidden, never
// disabled. The verdict row this describe used to also cover is not on
// this surface at all any more (#688's gap list); flagsState's own
// judge/undo tests are untouched in lib/flags.svelte.test.ts.
describe('Flags tiers (#653)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    flagsState.list = [testFlag()]
    authState.username = 'kai'
  })

  it('a viewer gets no clear action in the drawer', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('button', { name: 'clear with a note' })).toBeNull()
  })

  it('a user gets the clear action in the drawer', async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()

    expect(screen.getByRole('button', { name: 'clear with a note' })).toBeTruthy()
  })
})

// #678: the drawer's headline, story and episode shape -- generated per
// flag type from the evidence the flag already carries (flagNarrative.ts,
// episodeShape.ts each have their own direct unit tests; these confirm
// the component actually wires them in, not just that the functions work
// in isolation). Unchanged by #688 except for the container they now
// live in: the drawer row beneath the flag's own row.
describe('the drawer: headline, story and episode shape (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [
      testFlag({
        type: 'port_scan',
        target: '198.51.100.77',
        evidence: { ports: [1000, 1001, 1002] },
        firstSeen: '2026-01-01T13:41:00Z',
        lastSeen: '2026-01-01T13:41:40Z',
      }),
    ]
  })

  async function openDrawer() {
    render(Flags)
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()
  }

  it('shows the generated headline and story, not just the raw evidence line', async () => {
    await openDrawer()

    expect(document.querySelector('.headline')?.textContent).toBe('One source, three doors.')
    expect(document.querySelector('.story')?.textContent).toContain('198.51.100.77')
  })

  it('shows a non-empty episode shape derived from the flag before the episode fetch resolves richer data', async () => {
    await openDrawer()

    const shape = document.querySelector('.side .span')?.textContent ?? ''
    expect(shape.length).toBeGreaterThan(0)
  })

  it('once the real episode resolves, the shape reflects its actual per-event timestamps', async () => {
    // Five events inside a forty-second window -- port_scan's own
    // shape (see episodeShape.test.ts): short burst, second precision.
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [0, 8, 16, 24, 40].map((s, i) => ({
        id: i,
        time: `2026-01-01T13:41:${String(s).padStart(2, '0')}Z`,
        deviceId: 'd1',
        sourceIp: '198.51.100.77',
        action: 'drop' as const,
        ruleLabel: 'wan-in-drop',
        chain: 'input',
        raw: '',
      })),
      hasMore: false,
      windowStart: '2026-01-01T13:40:00Z',
      serverTime: '2026-01-01T13:42:00Z',
    })
    await openDrawer()
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('.side .span')?.textContent).toMatch(/→/)
  })

  it('draws the matched log lines verbatim in the drawer', async () => {
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [
        {
          id: 1,
          time: '2026-01-01T13:41:42Z',
          deviceId: 'd1',
          sourceIp: '198.51.100.77',
          action: 'drop' as const,
          ruleLabel: 'wan-in-drop',
          chain: 'input',
          raw: '',
          srcIp: '198.51.100.77',
          dstIp: '203.0.113.7',
          dstPort: 1019,
        },
      ],
      hasMore: false,
      windowStart: '2026-01-01T13:40:00Z',
      serverTime: '2026-01-01T13:42:00Z',
    })
    await openDrawer()
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('.lines')?.textContent).toContain('198.51.100.77->203.0.113.7:1019')
  })
})

// #678's third item: "where" is a link into the topography at its
// sensible level, not the live stream -- filterToTarget/appState.view =
// 'live' (still used by "open in stream" in the drawer) is no longer
// what the where value itself does.
describe('"where" links into the topography, not the stream (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [testFlag({ target: '198.51.100.77' })]
    appState.view = 'flags'
  })

  it('clicking the where value opens the topography rather than the live stream', async () => {
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: '198.51.100.77' }))
    flushSync()

    expect(appState.view).toBe('topography')
  })

  it('does not toggle the drawer on its way past the row', async () => {
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: '198.51.100.77' }))
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeNull()
  })
})

// #678/#679: the note is the reason the clear happened, and rides along
// on the same clearFlag call the audit log now records it from (see
// internal/api's handleFlagsClear). The action wears the record's own
// wording, "clear with a note", rather than the bare "Clear" the card
// carried.
describe('clear with a note (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })
    authState.state = 'authenticated'
    authState.role = 'user'
    flagsState.list = [testFlag()]
  })

  async function openDrawer() {
    render(Flags)
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()
  }

  it('clicking "clear with a note" opens a note field instead of clearing immediately', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'clear with a note' }))
    flushSync()

    expect(screen.getByLabelText('Note for clearing this flag')).toBeTruthy()
    expect(clearFlag).not.toHaveBeenCalled()
  })

  it('typing a note and confirming clears with that note', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'clear with a note' }))
    flushSync()
    await fireEvent.input(screen.getByLabelText('Note for clearing this flag'), {
      target: { value: 'expected, speed test' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await Promise.resolve()
    flushSync()

    expect(clearFlag).toHaveBeenCalledWith('f1', 'expected, speed test')
  })

  it('confirming with an empty note still clears, with no note recorded', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'clear with a note' }))
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await Promise.resolve()
    flushSync()

    expect(clearFlag).toHaveBeenCalledWith('f1', undefined)
  })

  it('Cancel discards the note and leaves the flag open, uncleared', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'clear with a note' }))
    flushSync()
    await fireEvent.input(screen.getByLabelText('Note for clearing this flag'), { target: { value: 'nope' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    flushSync()

    expect(clearFlag).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'clear with a note' })).toBeTruthy()
  })
})

// Round 26/29's honest empty state, drawn as `.caempty`: zero open is a
// fact with a history, not a blank.
describe('the empty state (round 26/29)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    authState.state = 'authenticated'
    authState.role = 'admin'
  })

  it('says nothing has been flagged yet when no flag has ever cleared', () => {
    flagsState.list = []
    render(Flags)
    flushSync()

    expect(screen.getByText('Nothing open.')).toBeTruthy()
    expect(screen.getByText(/Nothing has been flagged yet/)).toBeTruthy()
    expect(document.querySelector('table.ftable')).toBeNull()
  })

  it('says when the last clear happened, and offers the audit log to an admin', () => {
    flagsState.list = [testFlag({ cleared: true, clearedAt: '2026-01-01T13:58:00Z' })]
    render(Flags)
    flushSync()

    expect(screen.getByText('Nothing open.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'audit log' })).toBeTruthy()
  })
})

// #724's second click: a dial panel row on the topography hands off which
// flag it was through topologyNavState.pendingFlagId (topologyNav.svelte.ts)
// rather than just switching the view -- this is the tab side that
// consumes it.
describe('opening a flag drawer from the topography dial (#724)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    authState.state = 'authenticated'
    authState.role = 'admin'
    flagsState.list = [testFlag({ id: 'f1', target: '198.51.100.1' }), testFlag({ id: 'f2', target: '198.51.100.2' })]
    topologyNavState.pendingFlagId = null
  })

  it("opens that flag's drawer on arrival, through the same episode-fetch path a click on the row would use, and clears the pending selection", async () => {
    topologyNavState.pendingFlagId = 'f2'
    render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()

    const rows = document.querySelectorAll('tr.frow')
    expect(rows[1].classList.contains('open')).toBe(true)
    expect(rows[0].classList.contains('open')).toBe(false)
    expect(rows[1].nextElementSibling?.classList.contains('drawer')).toBe(true)
    expect(fetchFlagEpisode).toHaveBeenCalledWith(expect.objectContaining({ ip: '198.51.100.2' }))
    expect(topologyNavState.pendingFlagId).toBeNull()
  })

  it('a pending id that matches nothing lands on the tab with no drawer open and no error', () => {
    topologyNavState.pendingFlagId = 'no-such-flag'
    render(Flags)
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeNull()
    expect(topologyNavState.pendingFlagId).toBeNull()
  })

  // The part that rots if it's missed (#724's own Care note): once the
  // dial's row has been followed and its drawer opened, a later, ordinary
  // visit to this tab -- not through the dial -- must not silently reopen
  // it. Simulated here the same way the deck's own keep-alive card
  // lifecycle would produce it: the tab's component is torn down when the
  // docket scrolls out of view, and freshly built again on a later visit.
  it('does not reopen a drawer on a later, unrelated visit to the tab', async () => {
    topologyNavState.pendingFlagId = 'f1'
    const { unmount } = render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()
    expect(document.querySelector('tr.drawer')).toBeTruthy()
    expect(topologyNavState.pendingFlagId).toBeNull()

    unmount()

    // A plain, later visit to the flags tab -- nothing pending this time.
    render(Flags)
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeNull()
  })
})

// #654's evidence panel is not tested here any more, and that is
// deliberate rather than an oversight. Round 30's drawer shows the
// generated narrative, the episode's shape and the matched lines -- not
// the raw evidence the old panel listed -- so the host:port pairs #654
// added have no surface in this component to assert against. The
// backend work is untouched and the grouping logic keeps its own tests
// in lib/evidencePairs.test.ts. The missing home is recorded on #691.

// The learning shelf (#642): provisional flags -- raised while their
// baseline was still warming -- live in their own bounded section below
// the settled table, hatched AND worded (#616: never meaning by colour
// alone), absent when empty and nothing is warming. See the issue body's
// ruling for why it is not a fourth docket tab and not interleaved.
describe('the learning shelf (#642)', () => {
  // One warming detection, as GET /api/definitions would serve it --
  // driven through detectorSettingsState.refresh() (the component's own
  // path) rather than poked into the store, so the projection is
  // exercised too.
  const warmingDefinitions = {
    definitions: [
      {
        id: 'port_scan',
        name: 'Port scan',
        intent: 'detection',
        available: true,
        enabled: true,
        scope: {},
        learning: { floor: { minDurationSeconds: 1209600 }, keys: 3, ready: 1 },
      },
    ],
    coverageEvidence: { complete: true },
  }

  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchDefinitions).mockResolvedValue({ definitions: [], coverageEvidence: { complete: true } } as never)
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    authState.state = 'authenticated'
    authState.role = 'user'
    authState.username = 'kai'
    detectorSettingsState.list = []
    flagsState.list = []
  })

  it('a provisional flag renders on the shelf, not in the settled table', () => {
    flagsState.list = [
      testFlag({ id: 's1', target: '198.51.100.1' }),
      testFlag({ id: 'p1', target: '198.51.100.2', provisional: true }),
    ]
    render(Flags)
    flushSync()

    const settled = document.querySelector('section[aria-label^="Active flags"]') as HTMLElement
    const shelf = document.querySelector('section[aria-label^="Learning shelf"]') as HTMLElement
    expect(shelf).toBeTruthy()
    expect(settled.querySelectorAll('tr.frow')).toHaveLength(1)
    expect(settled.textContent).toContain('198.51.100.1')
    expect(shelf.querySelectorAll('tr.frow')).toHaveLength(1)
    expect(shelf.textContent).toContain('198.51.100.2')
  })

  it('is absent when nothing is provisional and no baseline is warming', () => {
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    expect(document.querySelector('section[aria-label^="Learning shelf"]')).toBeNull()
  })

  it('wears the word "provisional" on the row and the hatch class, never colour alone', () => {
    flagsState.list = [testFlag({ id: 'p1', provisional: true })]
    render(Flags)
    flushSync()

    const row = document.querySelector('section[aria-label^="Learning shelf"] tr.frow') as HTMLElement
    expect(row.classList.contains('provisional')).toBe(true)
    expect(row.querySelector('.ptag')?.textContent?.trim()).toBe('provisional')
  })

  it("the shelf's own heading carries its number", () => {
    flagsState.list = [
      testFlag({ id: 'p1', target: '198.51.100.2', provisional: true }),
      testFlag({ id: 'p2', target: '198.51.100.3', provisional: true }),
    ]
    render(Flags)
    flushSync()

    expect(screen.getByText(/2 provisional/)).toBeTruthy()
  })

  it('a warming baseline with no provisional flags states the case in words', async () => {
    vi.mocked(fetchDefinitions).mockResolvedValue(warmingDefinitions as never)
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    expect(await screen.findByText(/still warming/)).toBeTruthy()
    // Worded, not a table of nothing.
    expect(document.querySelector('section[aria-label^="Learning shelf"] table')).toBeNull()
  })

  it('a viewer never gets the warming line -- the signal is user-tier, so it degrades by absence', async () => {
    authState.role = 'viewer'
    vi.mocked(fetchDefinitions).mockResolvedValue(warmingDefinitions as never)
    // Even a stale store left over from a more-privileged session must
    // not leak the claim to a viewer.
    detectorSettingsState.list = [
      {
        name: 'port_scan',
        label: 'Port scan',
        enabled: true,
        scope: {},
        learning: { floor: { minDurationSeconds: 1209600 }, keys: 3, ready: 1 },
      },
    ]
    flagsState.list = []
    render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('section[aria-label^="Learning shelf"]')).toBeNull()
    expect(fetchDefinitions).not.toHaveBeenCalled()
  })

  it('the settled empty state points at an occupied shelf instead of claiming nothing was flagged', () => {
    flagsState.list = [testFlag({ id: 'p1', provisional: true })]
    render(Flags)
    flushSync()

    expect(screen.getByText('Nothing open.')).toBeTruthy()
    expect(screen.getByText(/learning shelf below/)).toBeTruthy()
    expect(screen.queryByText(/Nothing has been flagged yet/)).toBeNull()
  })

  // #780 moved the trio out of the drawer and onto the row itself (a
  // row's verb, not a drawer's), so a shelf row's chips are reached
  // directly -- no need to open the drawer first any more.
  it("a shelf row's trio calls noise, stamps it in place, and offers undo -- no drawer involved", async () => {
    const judged = testFlag({
      id: 'p1',
      provisional: true,
      cleared: true,
      clearedAt: '2026-01-01T00:01:00Z',
      verdict: 'noise' as const,
      verdictBy: 'kai',
      verdictAt: '2026-01-01T00:01:00Z',
    })
    vi.mocked(setFlagVerdict).mockResolvedValue(judged as never)
    flagsState.list = [testFlag({ id: 'p1', provisional: true })]
    render(Flags)
    flushSync()

    const shelf = document.querySelector('section[aria-label^="Learning shelf"]') as HTMLElement
    await fireEvent.click(within(shelf).getByRole('button', { name: /noise/ }))
    await Promise.resolve()
    flushSync()

    expect(setFlagVerdict).toHaveBeenCalledWith('p1', 'noise')
    expect(within(shelf).getByText('noise', { selector: '.stamp' })).toBeTruthy()
    expect(within(shelf).getByRole('button', { name: 'undo' })).toBeTruthy()
    // The row stays -- pinned in place, dimmed -- rather than vanishing
    // the instant the server marks it cleared (the recently-cleared
    // list, in place).
    expect(shelf.querySelector('tr.frow.fdone')).toBeTruthy()
  })

  it('a viewer gets no verdict chips on a shelf row', () => {
    authState.role = 'viewer'
    flagsState.list = [testFlag({ id: 'p1', provisional: true })]
    render(Flags)
    flushSync()

    const shelf = document.querySelector('section[aria-label^="Learning shelf"]') as HTMLElement
    expect(within(shelf).queryByRole('button', { name: /noise/ })).toBeNull()
    expect(within(shelf).queryByRole('button', { name: /expected/ })).toBeNull()
    expect(within(shelf).queryByRole('button', { name: /real/ })).toBeNull()
  })
})

// #780 (rounds 34-35): the verdict trio moves from the drawer -- gated
// on `provisional` -- onto every open row's own CALL IT column, gated
// on canEdit alone, since the backend takes a verdict on any flag
// (store.go:915). This closes #688's own recorded gap for the settled
// table.
describe('CALL IT: verdicts as a row\'s verb, not a drawer\'s (#780)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchFlagEpisode).mockResolvedValue({
      events: [],
      hasMore: false,
      windowStart: '2026-01-01T00:00:00Z',
      serverTime: '2026-01-01T00:00:00Z',
    })
    vi.mocked(fetchExclusions).mockResolvedValue([])
    authState.state = 'authenticated'
    authState.role = 'admin'
    authState.username = 'tom'
  })

  it('the settled table carries the trio for canEdit, with no drawer open', () => {
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    const settled = document.querySelector('section[aria-label^="Active flags"]') as HTMLElement
    expect(document.querySelector('tr.drawer')).toBeNull()
    expect(within(settled).getByRole('button', { name: /expected/ })).toBeTruthy()
    expect(within(settled).getByRole('button', { name: /noise/ })).toBeTruthy()
    expect(within(settled).getByRole('button', { name: /real/ })).toBeTruthy()
  })

  it('a viewer gets no chips, only the caret, in the settled table', () => {
    authState.role = 'viewer'
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    const settled = document.querySelector('section[aria-label^="Active flags"]') as HTMLElement
    expect(within(settled).queryByRole('button', { name: /expected/ })).toBeNull()
    expect(within(settled).getByRole('button', { name: /the drawer for this flag/ })).toBeTruthy()
  })

  it('a chip click never toggles the drawer on its way past the row', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(testFlag({ id: 's1', cleared: true, verdict: 'noise' }) as never)
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /noise/ }))
    await Promise.resolve()
    flushSync()

    expect(document.querySelector('tr.drawer')).toBeNull()
  })

  it('calling noise stamps the row, flashes/dims it, and offers undo, which restores the trio', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ id: 's1', cleared: true, verdict: 'noise', verdictBy: 'tom', verdictAt: '2026-01-01T00:01:00Z' }) as never,
    )
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /noise/ }))
    await Promise.resolve()
    flushSync()

    expect(setFlagVerdict).toHaveBeenCalledWith('s1', 'noise')
    const row = document.querySelector('tr.frow') as HTMLElement
    expect(row.classList.contains('struck')).toBe(true)
    expect(row.classList.contains('fdone')).toBe(true)
    expect(row.querySelector('.stamp.noise')?.textContent).toBe('noise')
    // The caret is gone from a resolved, non-real row (round 35's
    // close(r)) -- only the stamp and its undo remain.
    expect(row.querySelector('.openc')).toBeNull()

    vi.mocked(deleteFlagVerdict).mockResolvedValue(testFlag({ id: 's1', cleared: false }) as never)
    await fireEvent.click(screen.getByRole('button', { name: 'undo' }))
    await Promise.resolve()
    flushSync()

    expect(deleteFlagVerdict).toHaveBeenCalledWith('s1')
    expect(document.querySelector('.stamp')).toBeNull()
    expect(screen.getByRole('button', { name: /noise/ })).toBeTruthy()
  })

  it('calling real stamps REAL in the CALL IT column and keeps the row open, with the caret still there', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ id: 's1', verdict: 'real', verdictBy: 'tom', verdictAt: '2026-01-01T00:01:00Z' }) as never,
    )
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /real/ }))
    await Promise.resolve()
    flushSync()

    expect(setFlagVerdict).toHaveBeenCalledWith('s1', 'real')
    const row = document.querySelector('tr.frow') as HTMLElement
    expect(row.classList.contains('isreal')).toBe(true)
    // Real never sets `cleared`, so the row is not dimmed away.
    expect(row.classList.contains('fdone')).toBe(false)
    expect(row.querySelector('.stamp.real')?.textContent).toBe('real')
    expect(row.querySelector('.openc')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'undo' })).toBeTruthy()

    // The story leads with the "called real" line.
    await fireEvent.click(row)
    flushSync()
    expect(document.querySelector('.story .called')?.textContent).toContain('Called real at')
  })

  it('never again arms on the first click, then calls clearPermanent and lists the pair in the exclusions body', async () => {
    vi.mocked(clearFlagPermanent).mockResolvedValue(undefined as never)
    vi.mocked(fetchExclusions)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([{ id: 'x1', type: 'port_scan', target: '198.51.100.1' }])
    flagsState.list = [testFlag({ id: 's1', target: '198.51.100.1' })]
    render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    await Promise.resolve()
    flushSync()

    const never = screen.getByRole('button', { name: 'never again' })
    await fireEvent.click(never)
    flushSync()
    expect(screen.getByRole('button', { name: /confirm — Port scan never fires again for 198.51.100.1/ })).toBeTruthy()
    expect(clearFlagPermanent).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /confirm —/ }))
    await Promise.resolve()
    flushSync()

    expect(clearFlagPermanent).toHaveBeenCalledWith('s1')
    const row = document.querySelector('tr.frow') as HTMLElement
    expect(row.querySelector('.stamp')?.textContent).toBe('never again')
    expect(row.querySelector('.olink')).toBeNull() // no undo on a never-again row

    expect(document.querySelector('.excl-label')?.textContent).toContain('never again ·')
    await fireEvent.click(screen.getByRole('button', { name: 'show them' }))
    flushSync()
    expect(document.querySelector('tr.frow.fx .wl-plain')?.textContent).toBe('198.51.100.1')
  })

  it('the exclusions body renders only for an admin', async () => {
    authState.role = 'user'
    vi.mocked(fetchExclusions).mockResolvedValue([{ id: 'x1', type: 'port_scan', target: '198.51.100.1' }])
    flagsState.list = [testFlag({ id: 's1' })]
    render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()

    expect(fetchExclusions).not.toHaveBeenCalled()
    expect(document.querySelector('.excl-label')).toBeNull()
    // Nor the never-again verb itself, in the drawer.
    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()
    expect(screen.queryByRole('button', { name: 'never again' })).toBeNull()
  })

  it('letting an exclusion fire again removes it from the body', async () => {
    vi.mocked(fetchExclusions).mockResolvedValue([{ id: 'x1', type: 'port_scan', target: '198.51.100.1' }])
    vi.mocked(removeExclusion).mockResolvedValue(undefined as never)
    flagsState.list = []
    render(Flags)
    flushSync()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'show them' }))
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this exclusion/ }))
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: 'let it fire again' }))
    await Promise.resolve()
    flushSync()

    expect(removeExclusion).toHaveBeenCalledWith('x1')
    expect(screen.queryByText('198.51.100.1')).toBeNull()
  })
})
