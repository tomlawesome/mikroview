// SPDX-License-Identifier: AGPL-3.0-only
//
// The fall, pinned to its round-30 mockup (#700, #616):
// docs/design/concepts/round-30/shots/fall.png and the-whole.html's
// #s2. Twelve faults were found comparing the build to that mockup;
// this file covers the ones that are real behavioural claims rather
// than pure CSS (a grid the mockup never draws, apparatus removed
// behind typed consts the same way LiveTable's RESIZE_HANDLES_ENABLED
// works, the attention chips' real defect, and the label-collision
// fixes). Visual-only differences (exact colours, the (i) button's
// pixel position) aren't practical to pin from jsdom and are covered by
// eye instead.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, waitFor } from '@testing-library/svelte'
import { fireEvent } from '@testing-library/dom'
import { flushSync } from 'svelte'
import type { ClientEvent, Flag, FlagType } from '../lib/types'

// Fall.svelte reaches the network in three ways: fallState.refresh()
// (fetchDevices/fetchRouterRules) on mount, and its own loadWindow()
// poll (fetchEventsWindow/fetchFlags). AccountMenu (mounted inside the
// bar) needs fetchAuthSession/login/logout/register mocked the same way
// AccountMenu.svelte.test.ts already does, or its own mount reaches for
// the network too.
vi.mock('../lib/api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(async () => null),
  register: vi.fn(),
  fetchDevices: vi.fn(async () => []),
  fetchRouterRules: vi.fn(async () => ({ available: false, rules: [] })),
  fetchEventsWindow: vi.fn(async () => ({ events: [], hasMore: false })),
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
}))

import { fetchEventsWindow, fetchFlags } from '../lib/api'
import { fallState, type FallBoundary } from '../lib/fall.svelte'
import { flagsState } from '../lib/flags.svelte'

// jsdom has no window.matchMedia -- AccountMenu mounts ThemeMenu, which
// pulls in lib/viewport.svelte.ts; its ViewportState singleton calls
// matchMedia at module-load time, so this has to land before the
// dynamic import below (same fix AccountMenu.svelte.test.ts needed).
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

// jsdom implements no ResizeObserver, and `bind:clientWidth` on `.rig`
// (#722 -- the sizing policy needs to know its own real pixel width) is
// compiled to one. A no-op stub is all this needs: jsdom reports every
// box as zero-sized regardless, so the rig falls back to its own
// DEFAULT_FRAME_W (1600, this issue's own verification width) in every
// test below.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

const { default: Fall } = await import('./Fall.svelte')

let nextId = 1

function makeEvent(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: nextId++,
    time: new Date(Date.now() - 60_000).toISOString(),
    deviceId: 'router1',
    sourceIp: '203.0.113.10',
    action: 'accept',
    ruleLabel: 'test-rule',
    chain: 'forward',
    raw: '',
    receivedAt: Date.now(),
    ...overrides,
  }
}

function makeFlag(type: FlagType, target: string, overrides: Partial<Flag> = {}): Flag {
  return {
    id: `f${nextId++}`,
    type,
    target,
    detail: '',
    count: 1,
    firstSeen: new Date(Date.now() - 60_000).toISOString(),
    lastSeen: new Date(Date.now() - 60_000).toISOString(),
    cleared: false,
    ...overrides,
  }
}

function boundary(overrides: Partial<FallBoundary> = {}): FallBoundary {
  return {
    key: 'forward|iot|bridge1',
    chain: 'forward',
    inInterface: 'iot',
    outInterface: 'bridge1',
    srcAddressList: 'iot',
    label: 'iot → bridge1',
    coverage: 'observed',
    epithet: '',
    ...overrides,
  }
}

// n distinct, uniquely-keyed boundaries -- enough to drive the band
// sizing policy (#722) at whatever count a test wants, without every
// boundary colliding on the same (chain, inInterface, outInterface).
function makeBoundaries(n: number): FallBoundary[] {
  return Array.from({ length: n }, (_, i) =>
    boundary({
      key: `forward|b${i}|x`,
      inInterface: `b${i}`,
      outInterface: 'x',
      label: `b${i} → x`,
    }),
  )
}

// Renders Fall with the given boundaries/events/flags already resolved,
// bypassing fallState's own fetchDevices/fetchRouterRules pipeline
// (mocked to resolve empty) the same way LiveTable.svelte.test.ts seeds
// fallState.boundaries directly -- the reactive read is what these
// tests are about, not the rule-table fetch.
async function renderFall(opts: { boundaries: FallBoundary[]; events?: ClientEvent[]; flags?: Flag[] }) {
  vi.mocked(fetchEventsWindow).mockResolvedValue({
    events: opts.events ?? [],
    hasMore: false,
    windowStart: new Date(Date.now() - 15 * 60 * 1000).toISOString(),
    serverTime: new Date().toISOString(),
  })
  vi.mocked(fetchFlags).mockResolvedValue({ flags: opts.flags ?? [], timeSeries: [] })
  const result = render(Fall)
  await waitFor(() => expect(fallState.loading).toBe(false))
  fallState.boundaries = opts.boundaries
  flushSync()
  await waitFor(() => {
    expect(result.container.querySelector('.rig svg')).toBeTruthy()
  })
  return result
}

beforeEach(() => {
  vi.clearAllMocks()
  fallState.boundaries = []
  fallState.loading = true
  fallState.error = null
  flagsState.list = []
})

describe('the grid the build added is gone (#700 fault 1)', () => {
  it('draws no vertical band separators or horizontal gridlines', async () => {
    const { container } = await renderFall({ boundaries: [boundary()] })
    expect(container.querySelectorAll('.bandline').length).toBe(0)
    expect(container.querySelectorAll('.gridline').length).toBe(0)
  })
})

describe('the (i) button replaces "key ▸" (#700 fault 2)', () => {
  it('renders a small (i) button and no key toggle or legend', async () => {
    const { container } = await renderFall({ boundaries: [boundary()] })
    const ibtn = container.querySelector('button.ibtn')
    expect(ibtn).toBeTruthy()
    expect(ibtn?.textContent?.trim()).toBe('i')
    expect(container.querySelector('.key-toggle')).toBeNull()
    expect(container.querySelector('.legend')).toBeNull()
    expect(container.textContent).not.toContain('key ▸')
  })
})

describe('the bottom-right timestamp is unmounted, not deleted (#700 fault 3)', () => {
  it('prints no window-range caption by default', async () => {
    const { container } = await renderFall({
      boundaries: [boundary()],
      events: [makeEvent()],
    })
    expect(container.textContent).not.toContain('newest at the top')
    expect(container.querySelector('.window-caption')).toBeNull()
  })
})

describe('the rig draws no inline width cap of its own (#700 faults 4 and 10)', () => {
  // #722 gave a single boundary its own reason not to fill the frame
  // (capped at MAX_PITCH -- see the band width policy tests below), so
  // this no longer claims the rig always spans edge to edge; it still
  // guards against a regression of the original #700 fault, an inline
  // style cap fighting the sizing policy's own width/height attributes.
  it('draws the rig svg with no inline max-width style', async () => {
    const { container } = await renderFall({ boundaries: [boundary()] })
    const svg = container.querySelector('.rig svg')
    expect(svg?.getAttribute('style') ?? '').not.toContain('max-width')
  })
})

describe('empty-band and quieter captions are unmounted, not deleted (#700 faults 5 and 6)', () => {
  it('prints nothing in a band with zero traffic this window', async () => {
    const { container } = await renderFall({ boundaries: [boundary()], events: [] })
    expect(container.textContent).not.toContain('no traffic in this window')
  })

  it('folds quiet carriers without printing a "+n quieter" row', async () => {
    // Nine distinct ports on one boundary: one more than MAX_CARRIERS
    // (8), so the build's own logic would have a non-zero quieterCount.
    const events = Array.from({ length: 9 }, (_, i) => makeEvent({ dstPort: 20000 + i }))
    const { container } = await renderFall({ boundaries: [boundary()], events })
    expect(container.textContent).not.toContain('quieter')
  })
})

describe('band status vocabulary matches the mockup (#700 fault 9)', () => {
  it('reads WATCH HOLDING ✓ for a quiet-but-covered band, never QUIET', async () => {
    const { container } = await renderFall({ boundaries: [boundary()], events: [] })
    expect(container.textContent).toContain('WATCH HOLDING ✓')
    expect(container.textContent).not.toContain('QUIET</text>')
    // The literal word "QUIET" alone (not as part of "WATCH HOLDING" or
    // "QUIETER") must not appear as a standalone band caption.
    const captions = [...container.querySelectorAll('.band-caption')].map((n) => n.textContent)
    expect(captions).not.toContain('QUIET')
  })

  it('names the flag type on an alarmed band ("✱ TYPE"), not "ALARM FIRED"', async () => {
    const target = '198.51.100.20'
    const events = [makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', srcIp: target, dstPort: 445 })]
    const flags = [makeFlag('new_device', target)]
    const { container } = await renderFall({ boundaries: [boundary()], events, flags })
    expect(container.textContent).toContain('✱ NEW DEVICE')
    expect(container.textContent).not.toContain('ALARM FIRED')
  })
})

describe('the attention chips read every open flag, not just the ones in view (#700 fault 11)', () => {
  it('shows a chip for an uncleared flag whose firstSeen is well outside the visible span', async () => {
    // The core defect: firstSeen four hours ago, well past the default
    // 15-minute span, and no window event carries this flag's target --
    // the old flagChips (derived from flagHorizons, which is windowed)
    // would show nothing here even though the flag is still open.
    const oldFlag = makeFlag('new_device', '198.51.100.30', {
      firstSeen: new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString(),
    })
    const { container } = await renderFall({ boundaries: [boundary()], events: [], flags: [oldFlag] })
    expect(container.textContent).toContain('NEW DEVICE')
  })

  it('never counts a cleared flag', async () => {
    const cleared = makeFlag('new_device', '198.51.100.31', { cleared: true })
    const { container } = await renderFall({ boundaries: [boundary()], events: [], flags: [cleared] })
    expect(container.textContent).not.toContain('NEW DEVICE')
  })

  it('appends the boundary label once the flagged IP is resolvable in the window', async () => {
    const target = '198.51.100.40'
    const events = [makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', srcIp: target })]
    const f = makeFlag('new_device', target)
    const { container } = await renderFall({ boundaries: [boundary()], events, flags: [f] })
    expect(container.textContent).toContain('· iot → bridge1')
  })
})

describe('port labels along the foot cover every band (#700 fault 8)', () => {
  it('never lets one band’s carrier suppress a different band’s label', async () => {
    const boundaries = [
      boundary({ key: 'forward|a|x', chain: 'forward', inInterface: 'a', outInterface: 'x', label: 'a → x' }),
      boundary({ key: 'forward|b|x', chain: 'forward', inInterface: 'b', outInterface: 'x', label: 'b → x' }),
      boundary({ key: 'forward|c|x', chain: 'forward', inInterface: 'c', outInterface: 'x', label: 'c → x' }),
    ]
    const events = [
      makeEvent({ chain: 'forward', inInterface: 'a', outInterface: 'x', dstPort: 22 }),
      makeEvent({ chain: 'forward', inInterface: 'b', outInterface: 'x', dstPort: 8291 }),
      makeEvent({ chain: 'forward', inInterface: 'c', outInterface: 'x', dstPort: 51820 }),
    ]
    const { container } = await renderFall({ boundaries, events })
    const text = container.textContent ?? ''
    expect(text).toContain(':22')
    expect(text).toContain(':8291')
    expect(text).toContain(':51820')
  })
})

// The band sizing policy (#722): an ideal width, elastic within limits,
// and pages beyond them. The ResizeObserver stub above means `.rig`
// always reports a 0 clientWidth under jsdom, so every case here runs
// against Fall.svelte's own DEFAULT_FRAME_W (1600px) fallback -- the same
// 1600×1000 frame the issue's own verification uses. With that width,
// RAIL (76) and the rig's 4px margin fixed, the bands actually have
// 1520px to share (`bandsAreaW`), and MIN_PITCH (150) caps a single page
// at floor(1520/150) = 10 boundaries before the policy paginates.
function headHitBoxes(container: HTMLElement): { x: number; width: number }[] {
  return [...container.querySelectorAll('.head-hit')].map((el) => ({
    x: Number(el.getAttribute('x')),
    width: Number(el.getAttribute('width')),
  }))
}

function rigSvgWidth(container: HTMLElement): number {
  return Number(container.querySelector('.rig svg')?.getAttribute('width') ?? NaN)
}

describe('band width policy (#722): ideal, elastic within limits, paginated beyond them', () => {
  it('stretches a lone boundary, but caps it at MAX_PITCH rather than filling the frame', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(1) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(1)
    // MAX_PITCH (340) minus the 10px gutter -- capped, not the ~1520px
    // a single "divide available width by count" column would draw.
    expect(boxes[0].width).toBe(330)
    expect(rigSvgWidth(container)).toBe(76 + 1 * 340 + 4)
    expect(container.querySelector('.pager')).toBeNull()
  })

  it('caps two boundaries at the same MAX_PITCH -- not two comically wide columns', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(2) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(2)
    for (const b of boxes) expect(b.width).toBe(330)
    // Capped well short of the 1520px bands area: plenty of empty space
    // either side, per the owner's "not comically large with few".
    expect(rigSvgWidth(container)).toBeLessThan(900)
    expect(container.querySelector('.pager')).toBeNull()
  })

  it('draws close to IDEAL_PITCH near the mockup’s own comfortable count', async () => {
    // 1520 / 8 = 190px/band -- a mild stretch just past the 171px ideal,
    // the same "comfortable" region the round-30 mockup itself draws
    // (nine bands filling the frame).
    const { container } = await renderFall({ boundaries: makeBoundaries(8) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    for (const b of boxes) expect(b.width).toBe(180) // 190 - GUTTER(10)
    expect(container.querySelector('.pager')).toBeNull()
  })

  it('shrinks by only a small amount when slightly over the ideal count, still on one page', async () => {
    // 10 boundaries is exactly this frame's per-page ceiling
    // (floor(1520/150) = 10): 1520/10 = 152px/band, just above the
    // MIN_PITCH (150) floor -- a small shrink, not a crush, and no
    // pagination yet.
    const { container } = await renderFall({ boundaries: makeBoundaries(10) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(10)
    for (const b of boxes) expect(b.width).toBe(142) // 152 - GUTTER(10)
    expect(container.querySelector('.pager')).toBeNull()
  })

  it('stops shrinking and paginates once the count exceeds what MIN_PITCH can fit', async () => {
    // 16 boundaries: the exact count #709's seeding took the live fall
    // to, and the overlap this issue was filed over. floor(1520/150) is
    // 10, so 16 must paginate: ceil(16/10) = 2 pages, spread evenly as
    // ceil(16/2) = 8 + 8 -- never a lopsided 15-and-1 split.
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    expect(container.querySelector('.pager')).toBeTruthy()
    expect(container.querySelector('.pgnum')?.textContent?.trim()).toBe('1 / 2')

    let boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    // Every page renders bands at the same pitch (derived from the
    // 8-per-page count, not each page's own size), so flipping pages
    // never jumps band width.
    for (const b of boxes) expect(b.width).toBe(180) // 190 (1520/8) - GUTTER(10)
    expect(container.textContent).toContain('b0 → x')
    expect(container.textContent).not.toContain('b8 → x')

    const nextBtn = container.querySelector<HTMLButtonElement>('.pgbtn[aria-label="Next page of boundaries"]')!
    const prevBtn = container.querySelector<HTMLButtonElement>('.pgbtn[aria-label="Previous page of boundaries"]')!
    expect(prevBtn.disabled).toBe(true)
    expect(nextBtn.disabled).toBe(false)

    await fireEvent.click(nextBtn)
    flushSync()
    expect(container.querySelector('.pgnum')?.textContent?.trim()).toBe('2 / 2')
    boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    expect(container.textContent).toContain('b8 → x')
    expect(container.textContent).not.toContain('b0 → x')
    expect(nextBtn.disabled).toBe(true)
  })

  it('distributes an uneven remainder within one boundary of even, never lopsided', async () => {
    // 17 boundaries: ceil(17/10) = 2 pages, ceil(17/2) = 9 per page --
    // 9 then 8, differing by one, not 16-and-1 or similar.
    const { container } = await renderFall({ boundaries: makeBoundaries(17) })
    expect(container.querySelector('.pgnum')?.textContent?.trim()).toBe('1 / 2')
    expect(headHitBoxes(container)).toHaveLength(9)

    const nextBtn = container.querySelector<HTMLButtonElement>('.pgbtn[aria-label="Next page of boundaries"]')!
    await fireEvent.click(nextBtn)
    flushSync()
    expect(headHitBoxes(container)).toHaveLength(8)
  })

  it('never renders a pager when everything fits on one page', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(10) })
    expect(container.querySelector('.pager')).toBeNull()
    expect(container.querySelector('.pgnum')).toBeNull()
  })
})
