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
}))

import { deleteFlagVerdict, fetchExclusions, setFlagVerdict } from '../lib/api'
import { flagsState } from '../lib/flags.svelte'
import { exclusionsState } from '../lib/exclusions.svelte'
import { authState } from '../lib/auth.svelte'
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

// #654: the drawer's evidence panel groups pairs by host and states a
// truncation cap rather than letting a capped list read as complete --
// the owner-recorded design decisions this issue implements. The pure
// grouping/truncation logic itself is unit-tested directly in
// lib/evidencePairs.test.ts; these two prove Flags.svelte actually wires
// that logic into what a reviewer sees when they open the drawer.
describe('Flags evidence panel (#654)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchExclusions).mockResolvedValue([])
    exclusionsState.list = []
    authState.state = 'authenticated'
    authState.role = 'user'
  })

  it('groups host:port pairs by host, one row per host, never a flat list', async () => {
    flagsState.list = [
      testFlag({
        type: 'critical_port',
        evidence: {
          pairs: [
            { host: '192.168.1.10', port: 22 },
            { host: '192.168.1.11', port: 23 },
            { host: '192.168.1.10', port: 443 },
          ],
        },
      }),
    ]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()

    // One row per host, each carrying only the ports actually seen with
    // it -- .10 shows 22 and 443 together, .11 shows only 23, and
    // nothing implies .11 was ever tried on 22 or 443.
    expect(screen.getByText('192.168.1.10')).toBeTruthy()
    expect(screen.getByText('22, 443')).toBeTruthy()
    expect(screen.getByText('192.168.1.11')).toBeTruthy()
    expect(screen.getByText('23')).toBeTruthy()
  })

  it('states the exact truncation count instead of showing a short list that reads as complete', async () => {
    const pairs = Array.from({ length: 50 }, (_, i) => ({ host: `10.0.0.${i}`, port: 22 }))
    flagsState.list = [testFlag({ type: 'critical_port', evidence: { pairs, pairsTotal: 214 } })]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()

    expect(screen.getByText('(showing 50 of 214)')).toBeTruthy()
  })

  // #654's owner correction: pairsTotal is itself bounded
  // (internal/engine's maxEvidencePairsTracked), so once the backend
  // reports pairsTotalIsFloor the panel must say "at least this many"
  // rather than a flat number that would look exactly as precise as the
  // exact-count case above. Pinned as its own test, distinctly from it,
  // so a regression that dropped the "+" suffix (or applied it to the
  // exact case) would show up as a text-content mismatch, not just a
  // missing element.
  it('marks a floor total with a "+", distinctly from an exact total', async () => {
    const pairs = Array.from({ length: 50 }, (_, i) => ({ host: `10.0.0.${i}`, port: 22 }))
    flagsState.list = [
      testFlag({ type: 'critical_port', evidence: { pairs, pairsTotal: 200, pairsTotalIsFloor: true } }),
    ]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()

    expect(screen.getByText('(showing 50 of 200+)')).toBeTruthy()
    expect(screen.queryByText('(showing 50 of 200)')).toBeNull()
  })

  it('shows no truncation notice when the pair list is already complete', async () => {
    flagsState.list = [
      testFlag({
        type: 'critical_port',
        evidence: { pairs: [{ host: '192.168.1.10', port: 22 }] },
      }),
    ]
    render(Flags)
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: /the drawer for this flag/ }))
    flushSync()

    expect(screen.queryByText(/showing \d+ of \d+/)).toBeNull()
  })
})
