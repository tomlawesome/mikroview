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
  drawnRect,
  drawnRects,
  fitViewBox,
  grace,
  isDrawn,
  leaderEnd,
  mapRect,
  parseAspect,
  parseViewBox,
  placeCard,
  stageRect,
  unitMapper,
  userToBox,
  watchCardSize,
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

describe('watchCardSize — the card that grows under its own placement (#1028)', () => {
  // One fake observer, so a test can report a size change the way a
  // browser would. jsdom has none of its own.
  const fire: (() => void)[] = []
  let disconnected = 0
  class FakeResizeObserver {
    constructor(private readonly cb: () => void) {}
    observe() {
      fire.push(this.cb)
    }
    unobserve() {}
    disconnect() {
      disconnected++
    }
  }

  const el = (h: number) => ({ offsetWidth: 288, offsetHeight: h }) as HTMLElement

  afterEach(() => {
    fire.length = 0
    disconnected = 0
    vi.unstubAllGlobals()
  })

  it('reports the card’s new size when the declare form makes it taller', () => {
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    const card = el(180)
    const seen: { w: number; h: number }[] = []
    watchCardSize(card, (s) => seen.push(s))

    fire[0]()
    ;(card as { offsetHeight: number }).offsetHeight = 420
    fire[0]()

    expect(seen).toEqual([
      { w: 288, h: 180 },
      { w: 288, h: 420 },
    ])
  })

  // The recompute must not be able to chase its own tail. Moving a card
  // cannot resize it, so a browser has nothing new to report -- but
  // ResizeObserver does re-report an unchanged box when the layout
  // around it churns, and answering that with another placement is how
  // a card ends up walking across the map.
  it('says nothing when the size has not actually changed, so a move cannot start another', () => {
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    const card = el(420)
    const seen: { w: number; h: number }[] = []
    watchCardSize(card, (s) => seen.push(s))

    fire[0]()
    fire[0]()
    fire[0]()

    expect(seen).toEqual([{ w: 288, h: 420 }])
  })

  it('stops watching when the card goes', () => {
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    watchCardSize(el(180), () => {})()
    expect(disconnected).toBe(1)
  })

  it('watches nothing rather than guessing where there is no ResizeObserver', () => {
    vi.stubGlobal('ResizeObserver', undefined)
    let called = 0
    const stop = watchCardSize(el(180), () => called++)
    stop()
    expect(called).toBe(0)
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

// #1028: a subject the card cannot escape along the side's own axis.
// `pushClear` moves a left-side card sideways and never upwards, so a
// wide subject left every seeded side overlapping and the card settled
// for the least-bad one -- on the plate its own title named, with clear
// ground above it the whole time.
describe('placeCard where no side clears the subject on its own axis (#1028)', () => {
  // A band right across the map, as the lane row is across the foot.
  const band: Rect = { x: 0, y: 480, w: 1400, h: 110 }

  it('finds the clear ground above a subject that spans the map', () => {
    const tall = { w: 288, h: 420 }
    const p = placeCard({ anchor: { x: 700, y: 520 }, card: tall, stage: STAGE, avoid: [band] })
    const box = { x: p.left, y: p.top, w: tall.w, h: tall.h }
    expect(overlap(box, band), 'the card came down on a subject it could have cleared').toBe(0)
  })

  it('still keeps the card on the stage while it escapes', () => {
    const tall = { w: 288, h: 420 }
    const p = placeCard({ anchor: { x: 40, y: 520 }, card: tall, stage: STAGE, avoid: [band] })
    expect(p.left).toBeGreaterThanOrEqual(0)
    expect(p.top).toBeGreaterThanOrEqual(0)
    expect(p.left + tall.w).toBeLessThanOrEqual(STAGE.w)
    expect(p.top + tall.h).toBeLessThanOrEqual(STAGE.h)
  })

  it('leaves a placement that already clears its subject exactly where it was', () => {
    // The seeds work here, so the wider search must not run and must not
    // move the card: this is what stops the fix disturbing the surfaces
    // that place correctly today.
    // Left side, seeded to the anchor's right and then pushed clear of
    // the subject's right edge: 740 + the 22 gap.
    const req = { anchor: { x: 700, y: 300 }, card: CARD, stage: STAGE, avoid: [{ x: 660, y: 280, w: 80, h: 40 }] }
    const p = placeCard(req)
    expect({ left: p.left, top: p.top }).toEqual({ left: 762, top: 300 - LEAD_INSET })
  })

  it('takes the least-covering placement where the subject really cannot be cleared', () => {
    // A subject bigger than the stage: there is nowhere clear, and the
    // card must still be placed and still be joined to it.
    const everywhere: Rect = { x: -100, y: -100, w: 2000, h: 1000 }
    const p = placeCard({ anchor: { x: 700, y: 350 }, card: CARD, stage: STAGE, avoid: [everywhere] })
    expect(Number.isFinite(p.left)).toBe(true)
    expect(Number.isFinite(p.top)).toBe(true)
    expect(p.from).toEqual({ x: 700, y: 350 })
  })
})

// #1028: the avoidance set has to be what is on screen. The 2D map draws
// a zone twice -- a lane card and a ground-plan card -- and swaps them
// with `opacity: 0`, so the hidden one still answers with real numbers
// and avoiding it is avoiding nothing.
describe('isDrawn / drawnRect — only what a reader can see', () => {
  const withBox = (el: HTMLElement, box: { x: number; y: number; w: number; h: number }) => {
    el.getBoundingClientRect = () =>
      ({ left: box.x, top: box.y, width: box.w, height: box.h, right: box.x + box.w, bottom: box.y + box.h, x: box.x, y: box.y }) as DOMRect
    return el
  }
  const scene = () => {
    const host = withBox(document.createElement('div'), { x: 10, y: 20, w: 800, h: 600 })
    const layer = document.createElement('div')
    const plate = withBox(document.createElement('div'), { x: 110, y: 120, w: 60, h: 40 })
    layer.append(plate)
    host.append(layer)
    document.body.append(host)
    return { host, layer, plate }
  }
  afterEach(() => {
    document.body.replaceChildren()
  })

  it('measures a drawn plate relative to the container, not the page', () => {
    const { host, plate } = scene()
    expect(drawnRect(plate, host)).toEqual({ x: 100, y: 100, w: 60, h: 40 })
    expect(isDrawn(plate)).toBe(true)
  })

  it('refuses a plate its layer has faded out, which is how the two zone layers swap', () => {
    const { host, layer, plate } = scene()
    layer.style.opacity = '0'
    expect(isDrawn(plate)).toBe(false)
    expect(drawnRect(plate, host)).toBeNull()
  })

  it('refuses a plate hidden by display, visibility or the hidden attribute', () => {
    for (const hide of [
      (l: HTMLElement) => (l.style.display = 'none'),
      (l: HTMLElement) => (l.style.visibility = 'hidden'),
      (l: HTMLElement) => l.setAttribute('hidden', ''),
    ]) {
      const { host, layer, plate } = scene()
      hide(layer)
      expect(drawnRect(plate, host)).toBeNull()
      document.body.replaceChildren()
    }
  })

  it('refuses a plate with no size at all, rather than avoiding the corner', () => {
    const { host, plate } = scene()
    withBox(plate, { x: 110, y: 120, w: 0, h: 0 })
    expect(drawnRect(plate, host)).toBeNull()
  })

  it('keeps only the drawn ones, so a hidden layer contributes nothing', () => {
    const { host, layer, plate } = scene()
    const second = withBox(document.createElement('div'), { x: 210, y: 220, w: 30, h: 30 })
    layer.append(second)
    second.style.opacity = '0'
    expect(drawnRects([plate, second], host)).toEqual([{ x: 100, y: 100, w: 60, h: 40 }])
  })
})

function overlap(a: Rect, b: Rect): number {
  const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
  const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
  return w > 0 && h > 0 ? w * h : 0
}
