// SPDX-License-Identifier: AGPL-3.0-only
//
// The card-anchor rules as arithmetic (#1016, #1027). Everything here
// is the geometry half of cardAnchor.ts, which is deliberately free of
// the DOM so the rules can be checked as numbers rather than through a
// rendering engine jsdom does not have.
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  CARD_GRACE_MS,
  EDGE_INSET,
  LEAD_INSET,
  boundsOf,
  cardSize,
  fitViewBox,
  grace,
  leaderEnd,
  mapRect,
  parseAspect,
  parseViewBox,
  placeCard,
  stageRect,
  unitMapper,
  userToBox,
  type Rect,
} from './cardAnchor'

const STAGE: Rect = { x: 0, y: 0, w: 1400, h: 700 }
const CARD = { w: 288, h: 180 }

describe('fitViewBox', () => {
  it('letterboxes a wide viewBox in a square container, top and bottom', () => {
    // 1400x700 is 2:1; a 1000x1000 box can only give it 1000x500, and
    // xMidYMid centres the 500 left over. This is the case a ratio
    // taken from the container alone gets wrong -- it would scale y by
    // 1000/700 and place nothing at all.
    const fit = fitViewBox({ x: 0, y: 0, w: 1400, h: 700 }, { w: 1000, h: 1000 })
    expect(fit.sx).toBeCloseTo(1000 / 1400)
    expect(fit.sy).toBeCloseTo(1000 / 1400)
    expect(fit.tx).toBe(0)
    expect(fit.ty).toBeCloseTo(250)
  })

  it('letterboxes a tall viewBox in a wide container, left and right', () => {
    const fit = fitViewBox({ x: 0, y: 0, w: 700, h: 1400 }, { w: 1000, h: 1000 })
    expect(fit.sy).toBeCloseTo(1000 / 1400)
    expect(fit.tx).toBeCloseTo(250)
    expect(fit.ty).toBe(0)
  })

  it('keeps one scale for both axes, so nothing is stretched', () => {
    const fit = fitViewBox({ x: 0, y: 0, w: 1400, h: 720 }, { w: 1200, h: 500 })
    expect(fit.sx).toBe(fit.sy)
    expect(fit.sx).toBeCloseTo(500 / 720)
  })

  it('honours xMin and xMax rather than always centring', () => {
    const vb = { x: 0, y: 0, w: 1400, h: 700 }
    expect(fitViewBox(vb, { w: 1000, h: 1000 }, 'xMinYMin').ty).toBe(0)
    expect(fitViewBox(vb, { w: 1000, h: 1000 }, 'xMinYMax').ty).toBeCloseTo(500)
  })

  it('takes the larger ratio for slice, and stretches for none', () => {
    const vb = { x: 0, y: 0, w: 1400, h: 700 }
    expect(fitViewBox(vb, { w: 1000, h: 1000 }, 'xMidYMid', 'slice').sx).toBeCloseTo(1000 / 700)
    const none = fitViewBox(vb, { w: 1000, h: 1000 }, 'none')
    expect(none.sx).toBeCloseTo(1000 / 1400)
    expect(none.sy).toBeCloseTo(1000 / 700)
  })

  it('places a viewBox origin that is not 0,0', () => {
    const vb = { x: 100, y: 50, w: 200, h: 100 }
    const fit = fitViewBox(vb, { w: 200, h: 100 })
    expect(userToBox(fit, vb, { x: 100, y: 50 })).toEqual({ x: 0, y: 0 })
    expect(userToBox(fit, vb, { x: 300, y: 150 })).toEqual({ x: 200, y: 100 })
  })
})

describe('parsing the two attributes', () => {
  it('reads a viewBox in either separator', () => {
    expect(parseViewBox('0 0 1400 700')).toEqual({ x: 0, y: 0, w: 1400, h: 700 })
    expect(parseViewBox('0,0,1400,700')).toEqual({ x: 0, y: 0, w: 1400, h: 700 })
  })

  it('refuses a viewBox it cannot use rather than guessing one', () => {
    expect(parseViewBox(null)).toBeNull()
    expect(parseViewBox('0 0 1400')).toBeNull()
    expect(parseViewBox('0 0 0 700')).toBeNull()
    expect(parseViewBox('0 0 wide 700')).toBeNull()
  })

  it('reads preserveAspectRatio, defaulting the way SVG itself does', () => {
    expect(parseAspect('xMidYMid meet')).toEqual({ align: 'xMidYMid', meetOrSlice: 'meet' })
    expect(parseAspect('xMinYMax slice')).toEqual({ align: 'xMinYMax', meetOrSlice: 'slice' })
    expect(parseAspect('none')).toEqual({ align: 'none', meetOrSlice: 'meet' })
    expect(parseAspect(null)).toEqual({ align: 'xMidYMid', meetOrSlice: 'meet' })
  })
})

describe('leaderEnd — the mockup rule at round-49/index.html:1493', () => {
  const box: Rect = { x: 500, y: 200, w: 288, h: 180 }

  it('joins the left edge when the subject is off to the left', () => {
    const { to, side } = leaderEnd({ x: 100, y: 240 }, box)
    expect(side).toBe('left')
    expect(to).toEqual({ x: 500, y: 200 + LEAD_INSET })
  })

  it('joins the right edge when the subject is off to the right', () => {
    const { to, side } = leaderEnd({ x: 1200, y: 240 }, box)
    expect(side).toBe('right')
    expect(to).toEqual({ x: 788, y: 200 + LEAD_INSET })
  })

  it('joins the top edge when the subject is above and within the card’s span', () => {
    const { to, side } = leaderEnd({ x: 640, y: 60 }, box)
    expect(side).toBe('top')
    expect(to).toEqual({ x: 644, y: 200 + EDGE_INSET })
  })

  it('joins the bottom edge when the subject is below it — the case the drawing never has', () => {
    // The drawing only ever puts a card above or beside its subject, so
    // its rule sends every horizontally-overlapping anchor to the top.
    // Sent there from below, the leader would run up through the card.
    const { to, side } = leaderEnd({ x: 640, y: 600 }, box)
    expect(side).toBe('bottom')
    expect(to).toEqual({ x: 644, y: 200 + 180 - EDGE_INSET })
  })
})

describe('placeCard', () => {
  const subject: Rect = { x: 600, y: 300, w: 200, h: 120 }

  it('sits beside the subject, with the leader horizontal from the anchor', () => {
    const p = placeCard({ anchor: { x: 800, y: 360 }, card: CARD, stage: STAGE, avoid: [subject] })
    expect(p.left).toBeGreaterThan(subject.x + subject.w)
    // The card's top is the anchor less LEAD_INSET, so the leader that
    // the mockup draws at T+40 comes out level with the subject.
    expect(p.to.y).toBeCloseTo(p.from.y)
    expect(p.side).toBe('left')
  })

  it('never covers its subject', () => {
    for (const anchor of [
      { x: 700, y: 300 },
      { x: 700, y: 420 },
      { x: 600, y: 360 },
      { x: 800, y: 360 },
    ]) {
      const p = placeCard({ anchor, card: CARD, stage: STAGE, avoid: [subject] })
      const box = { x: p.left, y: p.top, w: CARD.w, h: CARD.h }
      expect(overlap(box, subject), `anchor ${anchor.x},${anchor.y}`).toBe(0)
    }
  })

  it('covers neither end of the boundary it describes', () => {
    // The city's own failure: the card landed on the DarkLane plate,
    // one of the two districts named in its own title.
    const near: Rect = { x: 520, y: 300, w: 220, h: 140 }
    const far: Rect = { x: 900, y: 300, w: 220, h: 140 }
    const p = placeCard({ anchor: { x: 820, y: 370 }, card: CARD, stage: STAGE, avoid: [near, far] })
    const box = { x: p.left, y: p.top, w: CARD.w, h: CARD.h }
    expect(overlap(box, near)).toBe(0)
    expect(overlap(box, far)).toBe(0)
  })

  it('prefers clear ground when more than one side clears the subject', () => {
    // Both sides of the subject are free of it, but one has an
    // unrelated district sitting there. The card takes the other.
    const clutter: Rect = { x: 830, y: 300, w: 400, h: 200 }
    const p = placeCard({
      anchor: { x: 700, y: 360 },
      card: CARD,
      stage: STAGE,
      avoid: [subject],
      softAvoid: [clutter],
    })
    expect(overlap({ x: p.left, y: p.top, w: CARD.w, h: CARD.h }, clutter)).toBe(0)
  })

  it('will still cover clear-ground preferences rather than its own subject', () => {
    // softAvoid never outranks avoid: hemmed in on the only free side,
    // the card takes the clutter and leaves the subject visible.
    const walled: Rect = { x: 0, y: 0, w: 1400, h: 700 }
    const p = placeCard({
      anchor: { x: 800, y: 360 },
      card: CARD,
      stage: STAGE,
      avoid: [subject],
      softAvoid: [walled],
    })
    expect(overlap({ x: p.left, y: p.top, w: CARD.w, h: CARD.h }, subject)).toBe(0)
  })

  it('flips to the other side rather than leaving the stage', () => {
    // Hard against the right edge there is no room on the right, so the
    // card has to go left of its subject and take the right edge's
    // leader.
    const edge: Rect = { x: 1290, y: 300, w: 100, h: 120 }
    const p = placeCard({ anchor: { x: 1340, y: 360 }, card: CARD, stage: STAGE, avoid: [edge] })
    expect(p.left + CARD.w).toBeLessThanOrEqual(STAGE.w)
    expect(p.side).toBe('right')
  })

  it('stays inside the stage from every corner of it', () => {
    for (const anchor of [
      { x: 4, y: 4 },
      { x: 1396, y: 4 },
      { x: 4, y: 696 },
      { x: 1396, y: 696 },
      { x: 700, y: 350 },
    ]) {
      const p = placeCard({ anchor, card: CARD, stage: STAGE })
      expect(p.left).toBeGreaterThanOrEqual(STAGE.x)
      expect(p.top).toBeGreaterThanOrEqual(STAGE.y)
      expect(p.left + CARD.w).toBeLessThanOrEqual(STAGE.x + STAGE.w)
      expect(p.top + CARD.h).toBeLessThanOrEqual(STAGE.y + STAGE.h)
    }
  })

  it('honours a stage that does not start at the origin', () => {
    // The 2D map's card is positioned against the whole view, while the
    // stage it must stay on is only the part below the header.
    const inset: Rect = { x: 0, y: 120, w: 1400, h: 500 }
    const p = placeCard({ anchor: { x: 700, y: 140 }, card: CARD, stage: inset })
    expect(p.top).toBeGreaterThanOrEqual(inset.y)
    expect(p.top + CARD.h).toBeLessThanOrEqual(inset.y + inset.h)
  })

  it('still places a card, joined to its subject, where nothing fits', () => {
    // A subject filling the stage has no clear side. The card is placed
    // anyway rather than not at all, and the leader still points at it.
    const huge: Rect = { x: 0, y: 0, w: 1400, h: 700 }
    const p = placeCard({ anchor: { x: 700, y: 350 }, card: CARD, stage: STAGE, avoid: [huge] })
    expect(Number.isFinite(p.left)).toBe(true)
    expect(Number.isFinite(p.top)).toBe(true)
    expect(p.from).toEqual({ x: 700, y: 350 })
  })

  it('the leader always lands on the card it is drawn to', () => {
    for (const anchor of [
      { x: 100, y: 100 },
      { x: 1300, y: 650 },
      { x: 700, y: 20 },
      { x: 700, y: 680 },
    ]) {
      const p = placeCard({ anchor, card: CARD, stage: STAGE, avoid: [subject] })
      expect(p.to.x).toBeGreaterThanOrEqual(p.left)
      expect(p.to.x).toBeLessThanOrEqual(p.left + CARD.w)
      expect(p.to.y).toBeGreaterThanOrEqual(p.top)
      expect(p.to.y).toBeLessThanOrEqual(p.top + CARD.h)
    }
  })
})

describe('boundsOf', () => {
  it('boxes a set of points', () => {
    expect(boundsOf([{ x: 10, y: 4 }, { x: -2, y: 30 }, { x: 6, y: 6 }])).toEqual({ x: -2, y: 4, w: 12, h: 26 })
  })
})

/* ---------------- the measuring half, against a stub ---------------- */

/** An SVG element as far as this module is concerned: a rendered box
 * and the two attributes. jsdom lays nothing out and has no
 * getScreenCTM, which is exactly the fallback path this exercises. */
function fakeSvg(box: { left: number; top: number; width: number; height: number }, viewBox = '0 0 1400 700', par = 'xMidYMid meet') {
  return {
    getBoundingClientRect: () => ({ ...box, right: box.left + box.width, bottom: box.top + box.height }),
    getAttribute: (n: string) => (n === 'viewBox' ? viewBox : n === 'preserveAspectRatio' ? par : null),
  } as unknown as SVGSVGElement
}

function fakeContainer(box: { left: number; top: number; width: number; height: number }) {
  return {
    getBoundingClientRect: () => ({ ...box, right: box.left + box.width, bottom: box.top + box.height }),
  } as unknown as Element
}

describe('unitMapper without getScreenCTM', () => {
  it('accounts for the letterboxing in a container that is not the viewBox’s shape', () => {
    // 1400x700 rendered into 1000x1000: 1000x500 of drawing, 250px of
    // nothing above it. A ratio from the container's own height would
    // put the viewBox's top-left at y=0 and its centre at y=500 for the
    // wrong reason; both are checked here.
    const svg = fakeSvg({ left: 0, top: 0, width: 1000, height: 1000 })
    const map = unitMapper(svg, fakeContainer({ left: 0, top: 0, width: 1000, height: 1000 }))!
    expect(map).toBeTruthy()
    const topLeft = map({ x: 0, y: 0 })
    expect(topLeft.x).toBeCloseTo(0)
    expect(topLeft.y).toBeCloseTo(250)
    const centre = map({ x: 700, y: 350 })
    expect(centre.x).toBeCloseTo(500)
    expect(centre.y).toBeCloseTo(500)
    const bottomRight = map({ x: 1400, y: 700 })
    expect(bottomRight.x).toBeCloseTo(1000)
    expect(bottomRight.y).toBeCloseTo(750)
  })

  it('measures relative to the container, not the page', () => {
    // The card is positioned inside a container that is itself somewhere
    // down the page; a mapper that forgot to subtract its origin would
    // put every card off the bottom of the view.
    const svg = fakeSvg({ left: 40, top: 300, width: 1400, height: 700 })
    const map = unitMapper(svg, fakeContainer({ left: 40, top: 200, width: 1400, height: 900 }))!
    expect(map({ x: 0, y: 0 })).toEqual({ x: 0, y: 100 })
    expect(map({ x: 1400, y: 700 })).toEqual({ x: 1400, y: 800 })
  })

  it('gives up rather than guessing when there is nothing to measure', () => {
    const container = fakeContainer({ left: 0, top: 0, width: 0, height: 0 })
    expect(unitMapper(fakeSvg({ left: 0, top: 0, width: 0, height: 0 }), container)).toBeNull()
    expect(unitMapper(fakeSvg({ left: 0, top: 0, width: 100, height: 100 }, ''), container)).toBeNull()
  })

  it('prefers the browser’s own matrix where there is one', () => {
    // Everything above is the fallback. Given a CTM -- as any real
    // browser has -- that is what is used, so a CSS transform above the
    // SVG is carried too.
    const svg = fakeSvg({ left: 0, top: 0, width: 1000, height: 1000 })
    ;(svg as unknown as { getScreenCTM: () => DOMMatrix }).getScreenCTM = () =>
      ({ a: 2, b: 0, c: 0, d: 2, e: 30, f: 70 }) as DOMMatrix
    const map = unitMapper(svg, fakeContainer({ left: 10, top: 20, width: 1000, height: 1000 }))!
    expect(map({ x: 100, y: 50 })).toEqual({ x: 2 * 100 + 30 - 10, y: 2 * 50 + 70 - 20 })
  })

  it('agrees with the browser’s own matrix in a container that is not the viewBox’s shape', () => {
    // The two routes have to give the same answer, and a non-square
    // container is the only place a wrong one shows: 1400x700 meets a
    // 900x900 box as 900x450 with 225px of nothing above it. The matrix
    // below is what a browser reports for exactly that, so if the
    // fallback ever loses the letterboxing the two stop agreeing here.
    const box = { left: 0, top: 0, width: 900, height: 900 }
    const s = 900 / 1400
    const withCtm = fakeSvg(box)
    ;(withCtm as unknown as { getScreenCTM: () => DOMMatrix }).getScreenCTM = () =>
      ({ a: s, b: 0, c: 0, d: s, e: 0, f: (900 - 700 * s) / 2 }) as DOMMatrix
    const container = fakeContainer(box)
    const byMatrix = unitMapper(withCtm, container)!
    const byViewBox = unitMapper(fakeSvg(box), container)!

    for (const p of [
      { x: 0, y: 0 },
      { x: 1400, y: 700 },
      { x: 700, y: 350 },
      { x: 1044, y: 517 },
    ]) {
      const a = byMatrix(p)
      const b = byViewBox(p)
      expect(b.x).toBeCloseTo(a.x, 6)
      expect(b.y).toBeCloseTo(a.y, 6)
      // And both put the drawing inside the letterboxed band, never
      // over the container's full height.
      expect(b.y).toBeGreaterThanOrEqual(225 - 1e-6)
      expect(b.y).toBeLessThanOrEqual(675 + 1e-6)
    }
  })

  it('maps a rectangle by all four corners', () => {
    const svg = fakeSvg({ left: 0, top: 0, width: 1400, height: 700 })
    const map = unitMapper(svg, fakeContainer({ left: 0, top: 0, width: 1400, height: 700 }))!
    expect(mapRect(map, { x: 10, y: 20, w: 30, h: 40 })).toEqual({ x: 10, y: 20, w: 30, h: 40 })
  })

  it('reports the stage as the SVG’s own box within the container', () => {
    const svg = fakeSvg({ left: 40, top: 300, width: 1200, height: 500 })
    expect(stageRect(svg, fakeContainer({ left: 40, top: 200, width: 1200, height: 900 }))).toEqual({
      x: 0,
      y: 100,
      w: 1200,
      h: 500,
    })
  })
})

describe('cardSize', () => {
  it('falls back to the drawn size before anything has been laid out', () => {
    expect(cardSize(null)).toEqual({ w: 288, h: 180 })
    expect(cardSize({ offsetWidth: 300, offsetHeight: 210 } as HTMLElement)).toEqual({ w: 300, h: 210 })
  })
})

describe('grace — the card outliving the pointer (#1027)', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('closes once the pointer has been gone for the grace period', () => {
    vi.useFakeTimers()
    const g = grace()
    const close = vi.fn()
    g.release(close)
    vi.advanceTimersByTime(CARD_GRACE_MS - 1)
    expect(close).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(close).toHaveBeenCalledTimes(1)
  })

  it('does not close when the pointer arrives somewhere in time', () => {
    vi.useFakeTimers()
    const g = grace()
    const close = vi.fn()
    g.release(close)
    vi.advanceTimersByTime(CARD_GRACE_MS - 10)
    g.hold()
    vi.advanceTimersByTime(1000)
    expect(close).not.toHaveBeenCalled()
  })

  it('a second journey replaces the first, so only one close is pending', () => {
    vi.useFakeTimers()
    const g = grace()
    const first = vi.fn()
    const second = vi.fn()
    g.release(first)
    g.release(second)
    vi.advanceTimersByTime(CARD_GRACE_MS)
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })
})

function overlap(a: Rect, b: Rect): number {
  const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
  const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
  return w > 0 && h > 0 ? w * h : 0
}
