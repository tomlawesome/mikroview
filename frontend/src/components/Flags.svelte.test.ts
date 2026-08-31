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
  setFlagVerdict: vi.fn(),
  deleteFlagVerdict: vi.fn(),
  fetchExclusions: vi.fn(async () => []),
  removeExclusion: vi.fn(),
  fetchFlagEpisode: vi.fn(async () => ({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })),
}))

import { clearFlag, deleteFlagVerdict, fetchExclusions, fetchFlagEpisode, setFlagVerdict } from '../lib/api'
import { flagsState } from '../lib/flags.svelte'
import { exclusionsState } from '../lib/exclusions.svelte'
import { authState } from '../lib/auth.svelte'
import { appState } from '../lib/state.svelte'
import type { Exclusion, Flag } from '../lib/types'

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

// The verdict row itself (issue #638): bare-labelled buttons, the
// undo-on-clear flow for expected/noise, and the badge that replaces the
// row (never re-presenting a judged flag as an open question) for real.
describe('Flags verdict row (#638)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    flagsState.list = []
    flagsState.undoableVerdicts = []
    exclusionsState.list = []
    authState.state = 'authenticated'
    authState.role = 'admin'
    authState.username = 'alice'
  })

  it('shows exactly three bare-labelled buttons for an unjudged flag -- no second line', async () => {
    flagsState.list = [testFlag()]
    render(Flags)
    await Promise.resolve()
    flushSync()

    const row = screen.getByRole('group', { name: 'Judge this flag' })
    const labels = Array.from(row.querySelectorAll('button')).map((b) => b.textContent?.trim())
    expect(labels).toEqual(['Expected', 'Noise', 'Real'])
  })

  it('Expected posts at once and clears the card, with an undo affordance shown', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ cleared: true, verdict: 'expected', verdictBy: 'alice', verdictAt: 't' }),
    )
    flagsState.list = [testFlag()]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Expected' }))
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    // Posted immediately, not deferred behind the undo window (issue
    // #638's rework: a deferred version of this lost the verdict
    // whenever the page was torn down inside the window).
    expect(setFlagVerdict).toHaveBeenCalledWith('f1', 'expected')
    expect(screen.queryByRole('button', { name: '203.0.113.9' })).toBeNull()
    expect(screen.getByText('Cleared as Expected')).toBeTruthy()
  })

  it('the undo affordance disappears on its own once the window lapses, without any further request', async () => {
    vi.useFakeTimers()
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ cleared: true, verdict: 'expected', verdictBy: 'alice', verdictAt: 't' }),
    )
    flagsState.list = [testFlag()]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Expected' }))
    await Promise.resolve()
    await Promise.resolve()
    flushSync()
    expect(screen.getByText('Cleared as Expected')).toBeTruthy()

    await vi.advanceTimersByTimeAsync(5000)
    flushSync()

    expect(screen.queryByText('Cleared as Expected')).toBeNull()
    expect(setFlagVerdict).toHaveBeenCalledTimes(1)
    expect(deleteFlagVerdict).not.toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('Undo sends a real DELETE and restores the card, within the window', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ cleared: true, verdict: 'noise', verdictBy: 'alice', verdictAt: 't' }),
    )
    vi.mocked(deleteFlagVerdict).mockResolvedValue(testFlag({ cleared: false }))
    flagsState.list = [testFlag()]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Noise' }))
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(deleteFlagVerdict).toHaveBeenCalledWith('f1')
    expect(screen.getByRole('button', { name: '203.0.113.9' })).toBeTruthy()
    expect(screen.queryByText('Cleared as Noise')).toBeNull()
  })

  it('Real records the verdict, keeps the card open, and replaces the button row with a badge', async () => {
    vi.mocked(setFlagVerdict).mockResolvedValue(
      testFlag({ verdict: 'real', verdictBy: 'alice', verdictAt: '2026-01-01T00:05:00Z' }),
    )
    flagsState.list = [testFlag()]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Real' }))
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(setFlagVerdict).toHaveBeenCalledWith('f1', 'real')
    expect(screen.getByRole('button', { name: '203.0.113.9' })).toBeTruthy()
    expect(screen.queryByRole('group', { name: 'Judge this flag' })).toBeNull()
    expect(document.querySelector('.verdict-badge.verdict-real')?.textContent).toBe('Real')
    expect(document.querySelector('.verdict-judged-by')?.textContent).toContain('alice')
  })
})

// Issue #649: every column on the docket's three tabs sorts (click,
// again to reverse) and filters (a quiet dashed row beneath the labels).
// Flags renders as a card grid, not a table, so the sortbar/filterbar
// stand in for column heads -- these assert the same guarantee: the
// Active list narrows and reorders on demand.
describe('Flags Active list sort and filter (#649)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    exclusionsState.list = []
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

  function cardTargets() {
    return Array.from(document.querySelectorAll('section[aria-labelledby="active-heading"] .card .target')).map(
      (el) => el.textContent?.trim(),
    )
  }

  it('defaults to newest first (age ascending), matching the order this replaces', () => {
    render(Flags)
    flushSync()
    expect(cardTargets()).toEqual(['198.51.100.2', '198.51.100.1'])
  })

  it('clicking a sort head (count) sorts by it, and again reverses', async () => {
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /^count/ }))
    flushSync()
    expect(cardTargets()).toEqual(['198.51.100.2', '198.51.100.1'])

    await fireEvent.click(screen.getByRole('button', { name: /^count/ }))
    flushSync()
    expect(cardTargets()).toEqual(['198.51.100.1', '198.51.100.2'])
  })

  it('a filter on evidence narrows the Active list to matching flags only', async () => {
    render(Flags)
    flushSync()

    await fireEvent.input(screen.getByLabelText('Filter by evidence'), { target: { value: 'mail' } })
    flushSync()

    expect(cardTargets()).toEqual(['198.51.100.2'])
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

// #653's three tiers: judging/clearing a flag is a normal operational
// action (user tier), not an owner-level one -- only a viewer, the
// tier below user, must see none of it. Hidden, never disabled (issue
// #198's own reasoning, now applied one tier lower).
describe('Flags tiers (#653)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    flagsState.list = [testFlag()]
    flagsState.undoableVerdicts = []
    exclusionsState.list = []
    authState.username = 'kai'
  })

  it('a viewer sees no verdict row, no Clear, and no Clear all', async () => {
    authState.state = 'authenticated'
    authState.role = 'viewer'
    render(Flags)
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('group', { name: 'Judge this flag' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Clear' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Clear all' })).toBeNull()
  })

  it('a user sees the verdict row, a plain Clear, and Clear all', async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(Flags)
    await Promise.resolve()
    flushSync()

    expect(screen.getByRole('group', { name: 'Judge this flag' })).toBeTruthy()

    // Clear lives in the drawer since #633; Clear all left this
    // component entirely for the docket's bubble (covered there).
    await fireEvent.click(screen.getAllByRole('button', { name: /the drawer for this flag/ })[0])
    flushSync()

    expect(screen.getByRole('button', { name: 'Clear' })).toBeTruthy()
    // The permanent-clear arrow stays admin-only -- a user gets the
    // plain Clear button with no split menu beside it.
    expect(screen.queryByRole('button', { name: 'More clear options for this flag' })).toBeNull()
  })
})

// #678: the drawer's headline, story and episode shape -- generated per
// flag type from the evidence the flag already carries (flagNarrative.ts,
// episodeShape.ts each have their own direct unit tests; these confirm
// the component actually wires them in, not just that the functions work
// in isolation).
describe('the drawer: headline, story and episode shape (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    vi.mocked(fetchFlagEpisode).mockResolvedValue({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })
    exclusionsState.list = []
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
})

// #678's third item: "where" is a link into the topography at its
// sensible level, not the live stream -- filterToTarget/appState.view =
// 'live' (still used by "open in stream" in the drawer) is no longer
// what the where value itself does.
describe('"where" links into the topography, not the stream (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    exclusionsState.list = []
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
})

// #678/#679: the note is the reason the clear happened, and rides along
// on the same clearFlag call the audit log now records it from (see
// internal/api's handleFlagsClear).
describe('clear with a note (#678)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    vi.mocked(fetchFlagEpisode).mockResolvedValue({ events: [], hasMore: false, windowStart: '2026-01-01T00:00:00Z', serverTime: '2026-01-01T00:00:00Z' })
    exclusionsState.list = []
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

  it('clicking Clear opens a note field instead of clearing immediately', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    flushSync()

    expect(screen.getByLabelText('Note for clearing this flag')).toBeTruthy()
    expect(clearFlag).not.toHaveBeenCalled()
  })

  it('typing a note and confirming clears with that note', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
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

    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    flushSync()
    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await Promise.resolve()
    flushSync()

    expect(clearFlag).toHaveBeenCalledWith('f1', undefined)
  })

  it('Cancel discards the note and leaves the flag open, uncleared', async () => {
    await openDrawer()

    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    flushSync()
    await fireEvent.input(screen.getByLabelText('Note for clearing this flag'), { target: { value: 'nope' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    flushSync()

    expect(clearFlag).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeTruthy()
  })
})
