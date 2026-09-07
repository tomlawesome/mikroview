// SPDX-License-Identifier: AGPL-3.0-only
//
// Where a floating card sits, and where its leader line joins it
// (round 49, #1016; #1027). One module, both map surfaces.
//
// Round 49 draws every card beside the thing it describes, joined to it
// by a thin leader with an accent dot at the subject's end
// (round-49/index.html's `leader`, line 1112, and `chrome`, line 1472).
// The drawing places each card by hand, per scene. The app cannot: the
// subject moves under a pan, a stop change and a window resize, and it
// must never end up under its own card -- on the city that put the
// boundary card on top of the DarkLane plate, one of the two ends of
// the boundary the card was describing.
//
// It lives here rather than in either component because DESIGN.md
// already records the reason, about `worstUnplannedOf`: "Two
// implementations of one rule agree the day they are written and
// diverge on the first one-line change to either."
//
// The file is in two halves, and they do not mix:
//
//   1. GEOMETRY -- pure functions over numbers. No DOM, no globals,
//      unit-testable as arithmetic, and that is where the rules live.
//   2. MEASUREMENT -- the thin layer that asks the browser how big
//      things actually are and hands the answers to half 1.

/* ================================================================
   1. GEOMETRY
   ================================================================ */

export interface Point {
  x: number
  y: number
}

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export interface Size {
  w: number
  h: number
}

/** Which edge of the card the leader joins. Named for the card's edge,
 * not the subject's: `left` means the leader arrives at the card's left
 * side, so the subject is off to the card's left. */
export type CardSide = 'left' | 'right' | 'top' | 'bottom'

/** How far down the card's side the leader lands, and how far in from
 * its top or bottom. Both are the mockup's own numbers
 * (round-49/index.html:1493 -- `T + 40`, and `T + 10` for the top
 * edge). Placing the card at `anchor.y - LEAD_INSET` then makes the
 * common leader exactly horizontal, which is what the drawing shows. */
export const LEAD_INSET = 40
export const EDGE_INSET = 10

/** Clearance between the card and anything it is keeping off. */
export const CARD_GAP = 22

/** How long a card outlives the pointer leaving it (#1027).
 *
 * The card opens on hover and the pointer then has to travel from the
 * subject to the card to reach the pin. Crossing the gap between them
 * fires `pointerleave` on the subject, and without a grace period the
 * card unmounts mid-journey -- the pin could never be clicked at all.
 *
 * A delay, rather than a tolerance corridor or open-on-click: the
 * corridor needs the pointer's velocity and a hit-test of its own on
 * every move, and open-on-click contradicts DESIGN.md "Cards", which
 * ratifies hover to open on both surfaces. A delay is one timer, the
 * same on both surfaces, and it is testable without a rendering engine.
 *
 * 180ms is long enough to cross CARD_GAP at a hand's speed and short
 * enough that a card left behind does not feel stuck to the pointer. */
export const CARD_GRACE_MS = 180

/** Sizes to fall back on where nothing has been measured yet -- the
 * card's own CSS width, and a height near the middle of what the
 * boundary card actually renders at. Only ever used before the first
 * measurement lands, so the first frame is placed sensibly rather than
 * at 0,0. */
export const CARD_FALLBACK: Size = { w: 288, h: 180 }

const overlaps = (a: Rect, b: Rect): boolean =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

const overlapArea = (a: Rect, b: Rect): number => {
  const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
  const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
  return w > 0 && h > 0 ? w * h : 0
}

const clamp = (n: number, lo: number, hi: number): number => (lo > hi ? lo : Math.min(Math.max(n, lo), hi))

export const grow = (r: Rect, by: number): Rect => ({ x: r.x - by, y: r.y - by, w: r.w + 2 * by, h: r.h + 2 * by })

/** The bounding box of some points. */
export function boundsOf(points: readonly Point[]): Rect {
  const xs = points.map((p) => p.x)
  const ys = points.map((p) => p.y)
  const x = Math.min(...xs)
  const y = Math.min(...ys)
  return { x, y, w: Math.max(...xs) - x, h: Math.max(...ys) - y }
}

export interface PlaceRequest {
  /** The point on the subject the leader starts from, in the same
   * pixel space as everything else here. */
  anchor: Point
  /** How big the card is. */
  card: Size
  /** The visible area the card has to stay inside. */
  stage: Rect
  /** What the card must not cover: its own subject, and -- for a
   * boundary -- the other end of it, because a card sitting on either
   * end hides half of what it is describing. */
  avoid?: readonly Rect[]
  /** What the card would rather not cover: everything else drawn that
   * a reader might want to see. This only chooses between placements
   * that already clear `avoid`, so wanting clear ground can never cost
   * the card the one thing it must not sit on. */
  softAvoid?: readonly Rect[]
  /** Clearance from `avoid` and from the stage edge. */
  gap?: number
  /** Sides to try, best first. */
  prefer?: readonly CardSide[]
}

export interface Placement {
  /** Card top-left. */
  left: number
  top: number
  /** The leader: `from` is the accent dot on the subject, `to` is where
   * it meets the card. */
  from: Point
  to: Point
  /** Which edge of the card `to` lies on. */
  side: CardSide
}

const DEFAULT_PREFER: readonly CardSide[] = ['left', 'right', 'top', 'bottom']

/** Where a card wants to start out on a given side of its anchor.
 * `side` names the card's own edge that will face the anchor, so
 * `left` puts the card to the anchor's right. */
function seedFor(side: CardSide, anchor: Point, card: Size, gap: number): Point {
  switch (side) {
    case 'left':
      return { x: anchor.x + gap, y: anchor.y - LEAD_INSET }
    case 'right':
      return { x: anchor.x - gap - card.w, y: anchor.y - LEAD_INSET }
    case 'top':
      return { x: anchor.x - card.w / 2, y: anchor.y + gap }
    case 'bottom':
      return { x: anchor.x - card.w / 2, y: anchor.y - gap - card.h }
  }
}

/** Push a card clear of everything it must not cover, in the one
 * direction its side allows. Each obstacle is escaped once; a second
 * pass catches the case where escaping one lands on another. */
function pushClear(at: Point, side: CardSide, card: Size, avoid: readonly Rect[], gap: number): Point {
  let { x, y } = at
  for (let pass = 0; pass < 2; pass++) {
    for (const r of avoid) {
      const box = { x, y, w: card.w, h: card.h }
      if (!overlaps(box, grow(r, gap))) continue
      if (side === 'left') x = r.x + r.w + gap
      else if (side === 'right') x = r.x - gap - card.w
      else if (side === 'top') y = r.y + r.h + gap
      else y = r.y - gap - card.h
    }
  }
  return { x, y }
}

/** Where the leader meets the card, given where the card ended up.
 *
 * This is the mockup's rule at round-49/index.html:1493, unchanged for
 * every case the drawing draws: left of the card and it joins the left
 * edge, right of it and the right edge, and otherwise the top. The one
 * addition is the case the drawing never has -- an anchor below a card
 * it sits directly under -- which takes the bottom edge by the same
 * reasoning, rather than running the leader up through the card. */
export function leaderEnd(anchor: Point, box: Rect): { to: Point; side: CardSide } {
  const { x: L, y: T, w: W, h: H } = box
  if (anchor.x < L) return { to: { x: L, y: T + LEAD_INSET }, side: 'left' }
  if (anchor.x > L + W) return { to: { x: L + W, y: T + LEAD_INSET }, side: 'right' }
  if (anchor.y <= T + H / 2) return { to: { x: L + W / 2, y: T + EDGE_INSET }, side: 'top' }
  return { to: { x: L + W / 2, y: T + H - EDGE_INSET }, side: 'bottom' }
}

/**
 * Place a card beside its subject and work out its leader.
 *
 * Each preferred side is tried in turn: seed the card on that side of
 * the anchor, push it clear of anything it would cover, then clamp it
 * into the stage. Candidates are ranked on how much of `avoid` they
 * cover first and how much of `softAvoid` second, so the card never
 * trades its own subject for a tidier corner. If every side covers
 * something -- a subject with no room round it at all -- the
 * least-covering one still wins, so the card is always placed and
 * always joined to its subject.
 */
export function placeCard(req: PlaceRequest): Placement {
  const { anchor, card, stage } = req
  const avoid = req.avoid ?? []
  const softAvoid = req.softAvoid ?? []
  const gap = req.gap ?? CARD_GAP
  const prefer = req.prefer && req.prefer.length > 0 ? req.prefer : DEFAULT_PREFER

  const covered = (box: Rect, rects: readonly Rect[]): number => rects.reduce((sum, r) => sum + overlapArea(box, r), 0)

  let best: { at: Point; hard: number; soft: number } | null = null
  for (const side of prefer) {
    const pushed = pushClear(seedFor(side, anchor, card, gap), side, card, avoid, gap)
    const at = {
      x: clamp(pushed.x, stage.x + gap, stage.x + stage.w - card.w - gap),
      y: clamp(pushed.y, stage.y + gap, stage.y + stage.h - card.h - gap),
    }
    const box = { x: at.x, y: at.y, w: card.w, h: card.h }
    const hard = covered(box, avoid)
    const soft = covered(box, softAvoid)
    if (hard === 0 && soft === 0) {
      best = { at, hard, soft }
      break
    }
    if (best === null || hard < best.hard || (hard === best.hard && soft < best.soft)) best = { at, hard, soft }
  }

  // A side that clears everything wins outright, and the four seeded
  // sides are the drawing's own arrangement, so they are tried first and
  // kept whenever one of them works.
  //
  // When none of them does, the seeds are not the last word (#1028).
  // Each side only ever escapes along its own axis -- `pushClear` moves
  // a left-side card sideways and never upwards -- so a subject that
  // spans the map in that direction leaves every seed overlapping, and
  // the old code then shrugged and took the least-bad one. That is how
  // the card ended up on the plate its own title named while there was
  // clear ground above it the whole time.
  //
  // So: if the best seed still covers a named subject, widen the search
  // to the positions where the card sits just clear of each subject's
  // own edges, on both axes. Those are the only places a rectangle can
  // change from overlapping to not, so this finds a clear placement
  // whenever one exists at all. It runs only when the seeds have already
  // failed, which is why nothing that places correctly today moves.
  if (best!.hard > 0) {
    const wider = escapePlacements(anchor, card, stage, avoid, softAvoid, gap)
    if (wider !== null && wider.hard < best!.hard) best = wider
  }

  const at = best!.at
  const box = { x: at.x, y: at.y, w: card.w, h: card.h }
  const { to, side } = leaderEnd(anchor, box)
  return { left: at.x, top: at.y, from: anchor, to, side }
}

/**
 * The best placement among the positions that sit flush outside the
 * subjects' own edges.
 *
 * A card only stops overlapping a rectangle by clearing one of its four
 * sides, so the x it can take are the seeds' own plus, for each subject,
 * "just left of it" and "just right of it" -- and likewise for y. Every
 * combination is scored, which is a few hundred at the sizes these
 * avoidance sets actually reach.
 *
 * Ties are settled by how far the card's centre is from the anchor, so
 * the card stays beside the thing it describes and the leader stays
 * short rather than reaching across the map.
 */
function escapePlacements(
  anchor: Point,
  card: Size,
  stage: Rect,
  avoid: readonly Rect[],
  softAvoid: readonly Rect[],
  gap: number,
): { at: Point; hard: number; soft: number } | null {
  const lox = stage.x + gap
  const hix = stage.x + stage.w - card.w - gap
  const loy = stage.y + gap
  const hiy = stage.y + stage.h - card.h - gap

  const xs = new Set<number>([anchor.x + gap, anchor.x - gap - card.w, anchor.x - card.w / 2])
  const ys = new Set<number>([anchor.y - LEAD_INSET, anchor.y + gap, anchor.y - gap - card.h])
  for (const r of avoid) {
    xs.add(r.x - gap - card.w)
    xs.add(r.x + r.w + gap)
    ys.add(r.y - gap - card.h)
    ys.add(r.y + r.h + gap)
  }

  const covered = (box: Rect, rects: readonly Rect[]): number => rects.reduce((sum, r) => sum + overlapArea(box, r), 0)
  let best: { at: Point; hard: number; soft: number; near: number } | null = null
  for (const rawX of xs) {
    const x = clamp(rawX, lox, hix)
    for (const rawY of ys) {
      const y = clamp(rawY, loy, hiy)
      const box = { x, y, w: card.w, h: card.h }
      const hard = covered(box, avoid)
      const soft = covered(box, softAvoid)
      const near = Math.hypot(x + card.w / 2 - anchor.x, y + card.h / 2 - anchor.y)
      if (best === null || hard < best.hard || (hard === best.hard && (soft < best.soft || (soft === best.soft && near < best.near)))) {
        best = { at: { x, y }, hard, soft, near }
      }
    }
  }
  return best === null ? null : { at: best.at, hard: best.hard, soft: best.soft }
}

/* ---------------- viewBox arithmetic ---------------- */

export interface ViewBox {
  x: number
  y: number
  w: number
  h: number
}

/** A viewBox's mapping into a rendered box: scale per axis, then the
 * offset that centres (or aligns) what is left over. */
export interface Fit {
  sx: number
  sy: number
  tx: number
  ty: number
}

const ALIGN_FRACTION: Record<string, number> = { Min: 0, Mid: 0.5, Max: 1 }

/**
 * How `preserveAspectRatio` maps a viewBox onto the box it is rendered
 * into.
 *
 * The part a hand-rolled ratio gets wrong is `meet`: it scales both
 * axes by the *smaller* ratio and letterboxes the leftover on the other
 * axis. Take `viewBox="0 0 1400 700"` in a 1000x1000 container -- the
 * drawing is 1000x500 with 250px of empty space above and below it. A
 * ratio taken from the container's own height is out by a factor of two
 * and the offset is missing entirely, which is only ever visible in a
 * container that is not the viewBox's own shape.
 */
export function fitViewBox(vb: ViewBox, box: Size, align = 'xMidYMid', meetOrSlice: 'meet' | 'slice' = 'meet'): Fit {
  if (vb.w <= 0 || vb.h <= 0) return { sx: 1, sy: 1, tx: 0, ty: 0 }
  if (align === 'none') return { sx: box.w / vb.w, sy: box.h / vb.h, tx: 0, ty: 0 }
  const rx = box.w / vb.w
  const ry = box.h / vb.h
  const s = meetOrSlice === 'slice' ? Math.max(rx, ry) : Math.min(rx, ry)
  const fx = ALIGN_FRACTION[align.slice(1, 4)] ?? 0.5
  const fy = ALIGN_FRACTION[align.slice(5, 8)] ?? 0.5
  return { sx: s, sy: s, tx: (box.w - vb.w * s) * fx, ty: (box.h - vb.h * s) * fy }
}

/** A point in viewBox user units, in the rendered box's own pixels. */
export function userToBox(fit: Fit, vb: ViewBox, p: Point): Point {
  return { x: fit.tx + (p.x - vb.x) * fit.sx, y: fit.ty + (p.y - vb.y) * fit.sy }
}

/** `viewBox="0 0 1400 700"` -> numbers. Null for anything unparseable,
 * so a caller falls back rather than placing a card by guesswork. */
export function parseViewBox(attr: string | null): ViewBox | null {
  if (!attr) return null
  const n = attr.trim().split(/[\s,]+/).map(Number)
  if (n.length !== 4 || n.some((v) => !Number.isFinite(v))) return null
  if (n[2] <= 0 || n[3] <= 0) return null
  return { x: n[0], y: n[1], w: n[2], h: n[3] }
}

/** `preserveAspectRatio="xMidYMid meet"` -> its two parts. */
export function parseAspect(attr: string | null): { align: string; meetOrSlice: 'meet' | 'slice' } {
  const parts = (attr ?? '').trim().split(/\s+/).filter(Boolean)
  const align = parts.find((p) => p === 'none' || p.startsWith('x')) ?? 'xMidYMid'
  const meetOrSlice = parts.includes('slice') ? 'slice' : 'meet'
  return { align, meetOrSlice }
}

/* ================================================================
   2. MEASUREMENT
   ================================================================
   Everything below touches the DOM, and nothing below decides
   anything: it turns elements into the numbers half 1 takes. */

/** Turns points in an SVG's user units into pixels relative to
 * `container`'s top-left corner. */
export type UnitMapper = (p: Point) => Point

/**
 * Build that mapper for one SVG, or null while the element has no
 * rendered size to measure.
 *
 * `getScreenCTM()` is the route to trust: it is the browser's own
 * answer, so it carries the viewBox fit, any transform on an ancestor,
 * and page zoom, none of which we would otherwise see. Where it is
 * missing -- jsdom has no SVG layout at all -- the same mapping is
 * rebuilt from the viewBox and the rendered box through `fitViewBox`,
 * letterboxing included.
 */
export function unitMapper(svg: SVGSVGElement, container: Element): UnitMapper | null {
  const origin = container.getBoundingClientRect()
  const ctm = typeof svg.getScreenCTM === 'function' ? svg.getScreenCTM() : null
  if (ctm) {
    return (p) => ({
      x: ctm.a * p.x + ctm.c * p.y + ctm.e - origin.left,
      y: ctm.b * p.x + ctm.d * p.y + ctm.f - origin.top,
    })
  }
  const box = svg.getBoundingClientRect()
  if (!(box.width > 0) || !(box.height > 0)) return null
  const vb = parseViewBox(svg.getAttribute('viewBox'))
  if (!vb) return null
  const { align, meetOrSlice } = parseAspect(svg.getAttribute('preserveAspectRatio'))
  const fit = fitViewBox(vb, { w: box.width, h: box.height }, align, meetOrSlice)
  const dx = box.left - origin.left
  const dy = box.top - origin.top
  return (p) => {
    const q = userToBox(fit, vb, p)
    return { x: dx + q.x, y: dy + q.y }
  }
}

/**
 * Is this element one a reader can actually see? (#1028)
 *
 * A card must keep off what is drawn, and a surface can draw the same
 * subject through more than one layer -- the 2D map draws a zone as a
 * lane card along the foot at some stops and as a ground-plan card
 * somewhere else entirely at others, switching between them with
 * `opacity: 0` rather than by removing either from the DOM. Both layers
 * are therefore always measurable, and an avoidance set built from the
 * wrong one keeps the card off rectangles nobody can see while it comes
 * down on the plate they can. That is #1028, and it is why this asks the
 * browser what is on screen instead of trusting a second copy of the
 * layout kept in the placement code.
 *
 * `opacity` is the one that matters here and the one a hit-test would
 * miss: the hidden layer still occupies its box and still answers
 * `getBoundingClientRect()` with real numbers.
 */
export function isDrawn(el: Element): boolean {
  if (typeof getComputedStyle !== 'function') return false
  for (let n: Element | null = el; n !== null; n = n.parentElement) {
    if (n.hasAttribute('hidden')) return false
    const cs = getComputedStyle(n)
    if (cs.display === 'none') return false
    if (cs.visibility === 'hidden' || cs.visibility === 'collapse') return false
    if (Number.parseFloat(cs.opacity) < 0.05) return false
  }
  return true
}

/**
 * An element's own rendered rectangle, in `container`'s pixels -- the
 * space `placeCard` works in -- or null where it is not drawn.
 *
 * Null rather than a zero rectangle, so a caller drops it from the
 * avoidance set instead of avoiding the container's top-left corner. In
 * jsdom, where nothing is laid out, every element is null and the
 * avoidance set is simply empty, which is what the surfaces already do
 * there.
 */
export function drawnRect(el: Element, container: Element): Rect | null {
  const r = el.getBoundingClientRect()
  if (!(r.width > 0) || !(r.height > 0)) return null
  if (!isDrawn(el)) return null
  const origin = container.getBoundingClientRect()
  return { x: r.left - origin.left, y: r.top - origin.top, w: r.width, h: r.height }
}

/** Every one of `els` that is actually drawn, as rectangles in
 * `container`'s pixels. */
export function drawnRects(els: Iterable<Element>, container: Element): Rect[] {
  const out: Rect[] = []
  for (const el of els) {
    const r = drawnRect(el, container)
    if (r !== null) out.push(r)
  }
  return out
}

/** A rectangle in user units, as its bounding box in container pixels.
 * All four corners are mapped, not two, because the mapper is a matrix
 * and need not be axis-aligned. */
export function mapRect(map: UnitMapper, r: Rect): Rect {
  return boundsOf([
    map({ x: r.x, y: r.y }),
    map({ x: r.x + r.w, y: r.y }),
    map({ x: r.x, y: r.y + r.h }),
    map({ x: r.x + r.w, y: r.y + r.h }),
  ])
}

/** The area a card is allowed to sit in: where the SVG actually renders,
 * in the container's own pixels. */
export function stageRect(svg: SVGSVGElement, container: Element): Rect | null {
  const origin = container.getBoundingClientRect()
  const box = svg.getBoundingClientRect()
  if (!(box.width > 0) || !(box.height > 0)) return null
  return { x: box.left - origin.left, y: box.top - origin.top, w: box.width, h: box.height }
}

/** How big a card has rendered, falling back to its drawn size before
 * anything has been laid out (which in jsdom is always). */
export function cardSize(el: HTMLElement | null | undefined): Size {
  const w = el?.offsetWidth ?? 0
  const h = el?.offsetHeight ?? 0
  return { w: w > 0 ? w : CARD_FALLBACK.w, h: h > 0 ? h : CARD_FALLBACK.h }
}

/**
 * Tell a surface when its card's own rendered size changes (#1028).
 *
 * Placement was worked out once, from the size the card happened to be
 * when it opened. The pin then reveals the declare form, the card gets
 * taller, and the rule that says "cover neither end of your own
 * boundary" was left holding a box that is no longer the box being
 * drawn -- so the grown card came down on the very zone plate its own
 * title names. The card's height is an input to `placeCard` like any
 * other, and every other input already re-places it.
 *
 * `onChange` should re-run the surface's ordinary placement, not a
 * placement of its own: the whole point of this module is that there is
 * one path, and both surfaces call this the same way.
 *
 * It cannot chase its own tail, for two independent reasons:
 *
 *  1. A placement only ever writes `left` and `top`. The card is
 *     absolutely positioned at a fixed width with its height set by its
 *     content, so moving it cannot change how big it is, and there is
 *     no second report to answer.
 *  2. Even so, a report that measures the same size as the one last
 *     passed on is dropped here. ResizeObserver does re-report a box
 *     unchanged when the layout around it churns, and `cardSize` reads
 *     whole pixels, so sub-pixel jitter cannot get through either.
 *
 * Returns the function that stops watching. Where there is no
 * ResizeObserver -- jsdom has none -- nothing is watched and the card
 * keeps the placement it was given, rather than taking a wrong one.
 */
export function watchCardSize(el: HTMLElement, onChange: (size: Size) => void): () => void {
  if (typeof ResizeObserver === 'undefined') return () => {}
  let last: Size | null = null
  const ro = new ResizeObserver(() => {
    const now = cardSize(el)
    if (last !== null && last.w === now.w && last.h === now.h) return
    last = now
    onChange(now)
  })
  ro.observe(el)
  return () => ro.disconnect()
}

/**
 * The card's grace period, as one small object per card (#1027).
 *
 * `hold()` while the pointer is on the subject or on the card, and
 * `release(close)` when it leaves either. Whoever the pointer arrives
 * at next calls `hold()` and the close never happens; if it arrives
 * nowhere, the card closes CARD_GRACE_MS later.
 *
 * Both surfaces use this one object, so the two cannot drift apart on
 * how long the journey is allowed to take.
 */
export interface Grace {
  hold(): void
  release(close: () => void): void
}

export function grace(ms: number = CARD_GRACE_MS): Grace {
  let timer: ReturnType<typeof setTimeout> | null = null
  const hold = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }
  return {
    hold,
    release(close: () => void) {
      hold()
      timer = setTimeout(() => {
        timer = null
        close()
      }, ms)
    },
  }
}
