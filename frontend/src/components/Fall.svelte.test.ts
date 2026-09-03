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
import { appState } from '../lib/state.svelte'

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

// Round 36 (#801) gives both of these a home. They were live logic
// behind a gate since round 30 (#700 faults 5 and 6); these tests now
// pin what the drawing actually draws instead of their absence.
describe("the empty band's quiet statement (#801, round 36 item 6.1)", () => {
  it('states "quiet, not dark" on a logged band that caught nothing, in the quiet ink', async () => {
    const { container } = await renderFall({ boundaries: [boundary()], events: [] })
    // Both halves of the drawing's own two-line plate, and the span it
    // is actually drawn over rather than the drawing's fixed "15 m".
    expect(container.textContent).toContain('nothing in these 15 m')
    expect(container.textContent).toContain('logged — quiet, not dark')
    // Never the dark red: quiet is a fact, not a fault (round 36).
    const quiet = [...container.querySelectorAll('.quiet-anno')]
    expect(quiet.length).toBe(2)
    for (const n of quiet) expect(n.classList.contains('bad-anno')).toBe(false)
  })

  it('says nothing on a band whose coverage is unknown -- the claim rests on a logging rule', async () => {
    // "logged — quiet, not dark" is a claim about a pushed rule that
    // really does log this boundary. With no rule table pushed there is
    // no such rule to point at, so the band stays silent rather than
    // guessing -- the same "only ever a definite answer" rule the
    // coverage model itself follows.
    const { container } = await renderFall({
      boundaries: [boundary({ coverage: 'unknown' })],
      events: [],
    })
    expect(container.textContent).not.toContain('quiet, not dark')
    expect(container.querySelectorAll('.quiet-anno')).toHaveLength(0)
  })

  it('says nothing on a band that does have traffic', async () => {
    const events = [makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', dstPort: 443 })]
    const { container } = await renderFall({ boundaries: [boundary()], events })
    expect(container.textContent).not.toContain('quiet, not dark')
  })
})

describe('the quieter count (#801, round 36 item 6.1)', () => {
  it('prints "+n quieter ▸" for the carriers folded past the cap', async () => {
    // Nine distinct ports on one boundary: one more than MAX_CARRIERS
    // (8), so exactly one carrier folds.
    const events = Array.from({ length: 9 }, (_, i) =>
      makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', dstPort: 20000 + i }),
    )
    const { container } = await renderFall({ boundaries: [boundary()], events })
    const quieter = container.querySelector('.quieter')
    expect(quieter?.textContent?.trim()).toBe('+1 quieter ▸')
  })

  it('sits beneath the band\'s port labels, not inside the fall', async () => {
    // The drawing puts it below the foot, under the port labels
    // (the-whole.html #s2) -- a name for the band, not a mark in its
    // traffic. PORTLAB_Y is 775 in the rig's own units.
    const events = Array.from({ length: 9 }, (_, i) =>
      makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', dstPort: 20000 + i }),
    )
    const { container } = await renderFall({ boundaries: [boundary()], events })
    const y = Number(container.querySelector('.quieter')?.getAttribute('y'))
    expect(y).toBeGreaterThan(775)
    // ...and still inside the rig, not clipped off the bottom (RIG_H 800).
    expect(y).toBeLessThan(800)
  })

  it('prints nothing when no carrier folded', async () => {
    const events = [makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', dstPort: 443 })]
    const { container } = await renderFall({ boundaries: [boundary()], events })
    expect(container.querySelector('.quieter')).toBeNull()
  })

  it('opens the stream filtered to its own boundary when activated', async () => {
    // #801's "Done when": the folded ports are reachable, not merely
    // counted. Pinned here as well as in live-waterfall.mjs because it
    // is the one claim of the four that is behaviour rather than text.
    const events = Array.from({ length: 9 }, (_, i) =>
      makeEvent({ chain: 'forward', inInterface: 'iot', outInterface: 'bridge1', dstPort: 20000 + i }),
    )
    const { container } = await renderFall({ boundaries: [boundary()], events })
    appState.view = 'fall'
    await fireEvent.click(container.querySelector('.quieter')!)
    flushSync()
    expect(appState.view).toBe('live')
    expect(appState.filters.interface).toBe('iot')
    expect(appState.filters.chain).toBe('forward')
  })
})

describe('the window-cap chip (#801, round 36 item 6.1)', () => {
  it('states the cap only when the window really does hold more', async () => {
    vi.mocked(fetchEventsWindow).mockResolvedValue({
      events: [],
      hasMore: true,
      windowStart: new Date(Date.now() - 15 * 60 * 1000).toISOString(),
      serverTime: new Date().toISOString(),
    })
    vi.mocked(fetchFlags).mockResolvedValue({ flags: [], timeSeries: [] })
    const { container } = render(Fall)
    await waitFor(() => expect(fallState.loading).toBe(false))
    fallState.boundaries = [boundary()]
    flushSync()
    await waitFor(() => {
      expect(container.textContent).toContain('this window holds more')
    })
    const chip = container.querySelector('.att.dim')
    expect(chip?.textContent).toContain('the most recent 5,000 events')
    // A statement, not a control: there is nowhere for it to lead.
    expect(chip?.tagName.toLowerCase()).toBe('span')
  })

  it('is absent when the window holds everything', async () => {
    // renderFall's own mock answers hasMore: false.
    const { container } = await renderFall({ boundaries: [boundary()], events: [] })
    expect(container.textContent).not.toContain('this window holds more')
    expect(container.querySelector('.att.dim')).toBeNull()
  })
})

describe('band status vocabulary matches the mockup (#700 fault 9, reworded by #790/#801)', () => {
  it('reads WATCHED for a quiet-but-covered band -- no tick, and never QUIET', async () => {
    const { container } = await renderFall({ boundaries: [boundary()], events: [] })
    // The owner retired round 30's wording and its tick (#790, round 36:
    // "watched in green says everything we need"). The ink carries the
    // verdict; the tick said it twice. Asserted on the caption itself
    // rather than the whole container -- AccountMenu mounts inside this
    // component and draws ticks of its own.
    const caption = container.querySelector('.band-caption')
    expect(caption?.textContent?.trim()).toBe('WATCHED')
    expect(caption?.textContent).not.toContain('✓')
    expect(container.textContent).not.toContain('WATCH HOLDING')
    // The caption keeps the accept ink it has always had.
    expect(caption?.classList.contains('ch-ok')).toBe(true)
    expect(container.textContent).not.toContain('QUIET</text>')
    // The literal word "QUIET" alone (not as part of "WATCHED" or
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

describe('a failed window load says so, not that nothing happened (#737)', () => {
  it('renders the load error, and not the empty-window wording, when fetchEventsWindow rejects', async () => {
    vi.mocked(fetchEventsWindow).mockRejectedValue(new Error('network unreachable'))
    vi.mocked(fetchFlags).mockResolvedValue({ flags: [], timeSeries: [] })
    const { container } = render(Fall)
    await waitFor(() => expect(fallState.loading).toBe(false))
    fallState.boundaries = [boundary()]
    flushSync()
    await waitFor(() => {
      expect(container.textContent).toContain('Could not load the window: network unreachable')
    })
    expect(container.textContent).not.toContain('nothing has arrived yet')
  })
})

describe('band width policy (#722): ideal, elastic within limits, paginated beyond them', () => {
  it('stretches a lone boundary, but caps it at MAX_PITCH rather than filling the frame', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(1) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(1)
    // MAX_PITCH (340) minus the 10px gutter -- capped, not the ~1520px
    // a single "divide available width by count" column would draw.
    expect(boxes[0].width).toBe(330)
    expect(rigSvgWidth(container)).toBe(76 + 1 * 340 + 4)
    expect(container.querySelector('.ovstrip')).toBeNull()
  })

  it('caps two boundaries at the same MAX_PITCH -- not two comically wide columns', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(2) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(2)
    for (const b of boxes) expect(b.width).toBe(330)
    // Capped well short of the 1520px bands area: plenty of empty space
    // either side, per the owner's "not comically large with few".
    expect(rigSvgWidth(container)).toBeLessThan(900)
    expect(container.querySelector('.ovstrip')).toBeNull()
  })

  it('draws close to IDEAL_PITCH near the mockup’s own comfortable count', async () => {
    // 1520 / 8 = 190px/band -- a mild stretch just past the 171px ideal,
    // the same "comfortable" region the round-30 mockup itself draws
    // (nine bands filling the frame).
    const { container } = await renderFall({ boundaries: makeBoundaries(8) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    for (const b of boxes) expect(b.width).toBe(180) // 190 - GUTTER(10)
    expect(container.querySelector('.ovstrip')).toBeNull()
  })

  it('shrinks by only a small amount when slightly over the ideal count, still one window', async () => {
    // 10 boundaries is exactly this frame's per-window ceiling
    // (floor(1520/150) = 10): 1520/10 = 152px/band, just above the
    // MIN_PITCH (150) floor -- a small shrink, not a crush, and no
    // second window to pan to yet.
    const { container } = await renderFall({ boundaries: makeBoundaries(10) })
    const boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(10)
    for (const b of boxes) expect(b.width).toBe(142) // 152 - GUTTER(10)
    expect(container.querySelector('.ovstrip')).toBeNull()
  })

  it('stops shrinking and windows once the count exceeds what MIN_PITCH can fit', async () => {
    // 16 boundaries: the exact count #709's seeding took the live fall
    // to, and the overlap this issue was filed over. floor(1520/150) is
    // 10, so 16 must window: ceil(16/10) = 2 windows' worth, spread
    // evenly as ceil(16/2) = 8 -- never a lopsided 15-and-1 split.
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')
    expect(strip).toBeTruthy()
    expect(strip!.getAttribute('aria-valuemax')).toBe('8') // maxStart = 16 - perPage(8)

    let boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    // Every window renders bands at the same pitch (derived from
    // perPage, not the window's own possibly-smaller tail), so panning
    // never jumps band width.
    for (const b of boxes) expect(b.width).toBe(180) // 190 (1520/8) - GUTTER(10)
    expect(container.textContent).toContain('b0 → x')
    expect(container.textContent).not.toContain('b8 → x')

    await fireEvent.keyDown(strip!, { key: 'End' })
    flushSync()
    expect(strip!.getAttribute('aria-valuenow')).toBe('8')
    boxes = headHitBoxes(container)
    expect(boxes).toHaveLength(8)
    expect(container.textContent).toContain('b15 → x')
    expect(container.textContent).not.toContain('b0 → x')
  })

  it('a freely positioned window is always the full perPage width, never a lopsided remainder', async () => {
    // 17 boundaries: ceil(17/10) = 2 windows' worth, perPage =
    // ceil(17/2) = 9. Unlike the old discrete pager (which split 17 as
    // 9-then-8, a lopsided last page), a continuously-slidable window
    // always shows exactly perPage boundaries, at the start of the
    // estate and at the far end alike.
    const { container } = await renderFall({ boundaries: makeBoundaries(17) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')!
    expect(headHitBoxes(container)).toHaveLength(9)

    await fireEvent.keyDown(strip, { key: 'End' })
    flushSync()
    expect(headHitBoxes(container)).toHaveLength(9)
    expect(container.textContent).toContain('b16 → x')
  })

  it('never renders the overview strip when everything already fits in one window', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(10) })
    expect(container.querySelector('.ovstrip')).toBeNull()
    expect(container.querySelectorAll('.ovtick')).toHaveLength(0)
  })
})

describe('the overview strip (#722 amendment, 2026-08-31): replaces the pager', () => {
  it('carries one tick per boundary in the whole estate, not just the visible slice', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    expect(container.querySelectorAll('.ovtick')).toHaveLength(16)
    // Exactly perPage (8) of them are the lit, contiguous "in window" run.
    expect(container.querySelectorAll('.ovtick.inwin')).toHaveLength(8)
  })

  it('draws no page numbers, arrows or other words -- round 30 README §5', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector('.ovstrip')!
    expect(strip.textContent?.trim()).toBe('')
  })

  it('is focusable and announces the visible range for a screen reader', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')!
    expect(strip.getAttribute('role')).toBe('slider')
    expect(strip.getAttribute('tabindex')).toBe('0')
    expect(strip.getAttribute('aria-valuetext')).toBe('showing boundaries 1 to 8 of 16')
  })

  it('pans the window with the left/right arrow keys, and Home/End reach the ends', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')!
    expect(container.textContent).toContain('b0 → x')

    await fireEvent.keyDown(strip, { key: 'ArrowRight' })
    flushSync()
    // Sliding by one boundary: the window drops b0 and picks up b8.
    expect(container.textContent).not.toContain('b0 → x')
    expect(container.textContent).toContain('b8 → x')

    await fireEvent.keyDown(strip, { key: 'ArrowLeft' })
    flushSync()
    expect(container.textContent).toContain('b0 → x')
    expect(container.textContent).not.toContain('b8 → x')

    await fireEvent.keyDown(strip, { key: 'End' })
    flushSync()
    expect(strip.getAttribute('aria-valuenow')).toBe('8')
    expect(container.textContent).toContain('b15 → x')

    await fireEvent.keyDown(strip, { key: 'Home' })
    flushSync()
    expect(strip.getAttribute('aria-valuenow')).toBe('0')
    expect(container.textContent).toContain('b0 → x')
  })

  it('clamps the window into range when the estate shrinks under it', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')!

    await fireEvent.keyDown(strip, { key: 'End' })
    flushSync()
    expect(strip.getAttribute('aria-valuenow')).toBe('8') // maxStart at 16 boundaries

    // Shrink the estate to 12: maxPerPage is still 10, so this still
    // windows (ceil(12/10) = 2 windows' worth, perPage = ceil(12/2) =
    // 6, maxStart = 12 - 6 = 6) -- windowStart (8) is now out of range
    // and must clamp to 6, not throw or render an empty slice.
    fallState.boundaries = makeBoundaries(12)
    flushSync()
    await waitFor(() => expect(headHitBoxes(container)).toHaveLength(6))
    const strip2 = container.querySelector<HTMLElement>('.ovstrip')!
    expect(strip2.getAttribute('aria-valuenow')).toBe('6')
    expect(container.textContent).toContain('b11 → x') // last of the shrunk estate, still reachable
  })

  it('moving the pointer down on the strip jumps the window to that position', async () => {
    const { container } = await renderFall({ boundaries: makeBoundaries(16) })
    const strip = container.querySelector<HTMLElement>('.ovstrip')!
    // jsdom reports every box as zero-sized (no ResizeObserver/layout),
    // so getBoundingClientRect() on the strip itself is also all zeros;
    // stub it to a real 800px track so the pointer math has something
    // to divide by.
    strip.getBoundingClientRect = () => ({ left: 0, right: 800, width: 800, top: 0, bottom: 6, height: 6, x: 0, y: 0, toJSON() {} }) as DOMRect

    await fireEvent.pointerDown(strip, { clientX: 800 }) // far right edge
    flushSync()
    expect(strip.getAttribute('aria-valuenow')).toBe('8') // clamped to maxStart
    expect(container.textContent).toContain('b15 → x')
  })
})
