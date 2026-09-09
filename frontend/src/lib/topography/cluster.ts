// SPDX-License-Identifier: AGPL-3.0-only
//
// The tunnel cluster (#890, round 52 -- docs/design/concepts/round-52).
//
// Round 30 drew one tunnel node and #877 built it; a second tunnel was
// left as a design question, and rounds 50-52 answered it. The ratified
// rule, verbatim from the round-52 README:
//
//   Tunnels are one group where wg0 is, the space right of the router.
//   They fill it the way a town fills in around a crossroads: staggered,
//   no two in a line, busiest nearest the router, taking the free space
//   beside wg0 ... before anything spills past the frame. Each gets its
//   own strand to the router's right shoulder.
//
// So placement is a packing rule, not a table of positions: the mockup's
// eight are one solution of this, not the spec. The busiest tunnel keeps
// round 30's literal seat at (1128, 132) and its literal rib; every other
// takes the nearest free 188x56 pocket to the router that collides with
// nothing already drawn -- card, strand, lane card or rib -- preferring
// pockets that need no more frame than the map already has.
//
// Everything here is pure: geometry in, geometry out. No DOM, no store,
// no clock, so Topography.svelte's arithmetic is unit-testable on its own
// (cluster.test.ts).
import { boxHasPoint, boxesOverlap, fitScale, growFrame, type Box } from '../fit'

/** The card round 52 packs: round 30's small node, to the pixel. It is
 * drawn asymmetrically about its own point -- -84 to +104 -- which is
 * why these are two numbers rather than one half-width. */
export const CARD_W = 188
export const CARD_H = 56
export const CARD_LEFT = 84
export const CARD_RIGHT = 104
export const CARD_HALF_H = 28
/** How far the aggregate bar hangs below a card that has one. */
export const CARD_BAR = 20

/** Round 30's seat for the first tunnel (the-whole.html:986). The
 * busiest tunnel keeps it, so with one tunnel nothing moves. */
export const WG0 = { x: 1128, y: 132 }
/** Round 30's literal rib from that seat to the waist. */
export const WG0_STRAND = 'M1080 186 C 990 215, 880 240, 830 252'

/** The nominal frame round 49 drew the whole scene on. */
export const NOMINAL_FRAME: Box = { x: 0, y: 0, w: 1400, h: 720 }
/** The stage the viewBox is presented on, and what a zoom percentage is
 * measured against -- the city's own (project.ts STAGE_W/STAGE_H). */
export const STAGE_W = 1400
export const STAGE_H = 700

/** The router card's right edge and the shoulder the strands land on. */
const ROUTER_RIGHT = 828
const SHOULDER_TOP = 268
const SHOULDER_STEP = 12
const SHOULDER_BOTTOM = 320
/** The router's top edge, where a strand from a pocket left of wg0 lands
 * instead -- beside the Internet rib, as the mockup's `gre` does. */
const ROUTER_TOP_EDGE = { x: 790, y: 234 }

/** The lattice the pockets are cut from: half a card-plus-gap across,
 * a card-plus-air down, and only the squares whose (col + row) is even,
 * which is what makes the group staggered rather than a grid. Two
 * pockets on this lattice can never overlap. */
const COL_PITCH = 106
const ROW_PITCH = 94
/** Clearance every pocket keeps from anything already drawn. */
const GAP = 20
/** No pocket may sit left of this, or the group would stop being one
 * group in the space right of the router. */
const CLUSTER_LEFT = 830

export interface Spot {
  x: number
  y: number
}

/** A placed tunnel: where its card sits and the strand that joins it. */
export interface Placed extends Spot {
  /** Round 30's literal seat, drawn with its literal rib. */
  literal: boolean
  strand: string
  /** Where the strand leaves the card -- what an edge anchors to. */
  anchor: Spot
}

/** The card box for a pocket, with the aggregate bar where it has one. */
export function cardBox(p: Spot, bar = false): Box {
  return {
    x: p.x - CARD_LEFT,
    y: p.y - CARD_HALF_H,
    w: CARD_W,
    h: CARD_H + (bar ? CARD_BAR : 0),
  }
}

/* ---------------- curves ---------------- */

type Pt = [number, number]

function bez(pts: [Pt, Pt, Pt, Pt], t: number): Pt {
  const u = 1 - t
  const a = u * u * u
  const b = 3 * u * u * t
  const c = 3 * u * t * t
  const d = t * t * t
  return [
    a * pts[0][0] + b * pts[1][0] + c * pts[2][0] + d * pts[3][0],
    a * pts[0][1] + b * pts[1][1] + c * pts[2][1] + d * pts[3][1],
  ]
}

const r1 = (n: number): number => Math.round(n * 10) / 10

function pathOf(p: [Pt, Pt, Pt, Pt]): string {
  return `M${r1(p[0][0])} ${r1(p[0][1])} C ${r1(p[1][0])} ${r1(p[1][1])}, ${r1(p[2][0])} ${r1(p[2][1])}, ${r1(p[3][0])} ${r1(p[3][1])}`
}

/** Points along a curve, for testing it against a box. 24 samples on a
 * strand this length puts them under 20 px apart -- finer than the
 * clearance the packing already keeps. */
export function samplePath(p: [Pt, Pt, Pt, Pt], n = 24): Pt[] {
  const out: Pt[] = []
  for (let i = 0; i <= n; i++) out.push(bez(p, i / n))
  return out
}

/** The literal wg0 rib, as points -- it is a string constant rather than
 * control points, so its geometry is repeated here once. */
const WG0_STRAND_PTS: [Pt, Pt, Pt, Pt] = [
  [1080, 186],
  [990, 215],
  [880, 240],
  [830, 252],
]

/* ---------------- strands ---------------- */

/**
 * A strand's shape, mirroring the mockup's own four:
 *
 * - a card above the router and left of wg0 leaves its bottom edge and
 *   lands on the router's top edge (round 52's `gre`);
 * - a card deep in the bottom band leaves its top edge and climbs
 *   (`ovpn`);
 * - everything else leaves its left edge for the right shoulder
 *   (`wg1`, `l2tp`, `wg3`).
 */
function strandEnds(p: Spot, topEdge: boolean): { start: Pt; kind: 'top' | 'under' | 'left' } {
  if (topEdge) return { start: [p.x, p.y + CARD_HALF_H], kind: 'top' }
  // A card in the bottom band, or one sitting almost directly under the
  // shoulder, climbs out of its top edge: leaving the left edge there
  // would draw a vertical line hugging the router's own side.
  const overTheShoulder = p.x - CARD_LEFT < ROUTER_RIGHT + 100
  if (p.y > 520 || (p.y > 340 && overTheShoulder)) return { start: [p.x, p.y - CARD_HALF_H], kind: 'under' }
  return { start: [p.x - CARD_LEFT, p.y], kind: 'left' }
}

function directCurve(p: Spot, shoulder: Spot, topEdge: boolean): [Pt, Pt, Pt, Pt] {
  const { start, kind } = strandEnds(p, topEdge)
  const end: Pt = [shoulder.x, shoulder.y]
  const dx = start[0] - end[0]
  const dy = end[1] - start[1]
  if (kind === 'top') {
    return [start, [start[0] - dx * 0.15, start[1] + dy * 0.45], [start[0] - dx * 0.6, start[1] + dy * 0.85], end]
  }
  if (kind === 'under') {
    return [start, [start[0] + 10, start[1] + dy * 0.35], [start[0] - dx * 0.1, start[1] + dy * 0.7], end]
  }
  return [start, [start[0] - dx * 0.3, start[1] + dy * 0.3], [start[0] - dx * 0.7, start[1] + dy * 0.7], end]
}

/** The alternative the mockup's `wg2` draws: leave the edge, rise or
 * fall to the shoulder's own height first, then run in flat -- which is
 * how a strand threads between cards instead of under them. */
function threadedCurve(p: Spot, shoulder: Spot, topEdge: boolean): [Pt, Pt, Pt, Pt] {
  const { start, kind } = strandEnds(p, topEdge)
  const end: Pt = [shoulder.x, shoulder.y]
  if (kind === 'left') {
    const dx = start[0] - end[0]
    const dy = end[1] - start[1]
    return [start, [start[0] - 20, start[1] + dy * 0.8], [start[0] - dx * 0.75, end[1] - dy * 0.1], end]
  }
  return directCurve(p, shoulder, topEdge)
}

function crossesAny(pts: Pt[], boxes: Box[]): boolean {
  for (const [x, y] of pts) for (const b of boxes) if (boxHasPoint(b, x, y)) return true
  return false
}

/**
 * The strand for one pocket. It threads between cards where it can and
 * runs under a nearer card where it must, exactly as the Guest rib runs
 * under the Guest card (round 52 README) -- strands are drawn before the
 * cards, so the card covers what passes beneath it.
 */
export function strandFor(p: Spot, shoulder: Spot, topEdge: boolean, cards: Box[]): { d: string; anchor: Spot; points: Pt[] } {
  const direct = directCurve(p, shoulder, topEdge)
  const chosen = crossesAny(samplePath(direct), cards) ? threadedCurve(p, shoulder, topEdge) : direct
  return { d: pathOf(chosen), anchor: { x: chosen[0][0], y: chosen[0][1] }, points: samplePath(chosen) }
}

/* ---------------- the packing rule ---------------- */

export interface Obstacles {
  /** Cards already on the map that a pocket must not touch: the
   * Internet island, the waist, and every lane card. */
  boxes: Box[]
  /** Curves already on the map that a pocket must not sit on: the
   * trunk, the lane ribs, and the strands of tunnels placed before it. */
  curves: Pt[][]
}

/** Whether a pocket is clear of everything already drawn. */
function pocketFree(box: Box, obs: Obstacles): boolean {
  for (const b of obs.boxes) if (boxesOverlap(box, b, GAP)) return false
  for (const c of obs.curves) for (const [x, y] of c) if (boxHasPoint(box, x, y, GAP)) return false
  return true
}

/** How much of the map's scale a pocket outside the frame costs: the
 * frame it would force, read as the fit chip reads it. Zero for a pocket
 * that needs no more frame at all. */
function frameCost(box: Box): number {
  return 100 - zoomPercent(growFrame(NOMINAL_FRAME, [box]))
}

/** Whether a pocket needs no more frame than the map already has. */
function insideFrame(box: Box, pad = 40): boolean {
  return (
    box.x - pad >= NOMINAL_FRAME.x &&
    box.y - pad >= NOMINAL_FRAME.y &&
    box.x + box.w + pad <= NOMINAL_FRAME.x + NOMINAL_FRAME.w &&
    box.y + box.h + pad <= NOMINAL_FRAME.y + NOMINAL_FRAME.h
  )
}

/**
 * Every pocket the lattice offers, nearest the router first -- and every
 * pocket that keeps the frame as it is before any that would grow it,
 * which is the README's "before anything spills past the frame".
 */
function candidates(): Spot[] {
  const near: { p: Spot; d: number; cost: number }[] = []
  const far: { p: Spot; d: number; cost: number }[] = []
  for (let i = -2; i <= 6; i++) {
    for (let j = -4; j <= 10; j++) {
      if ((i + j) % 2 !== 0) continue
      const p = { x: WG0.x + i * COL_PITCH, y: WG0.y + j * ROW_PITCH }
      const box = cardBox(p)
      if (box.x < CLUSTER_LEFT) continue
      // Distance from the router's right shoulder to the card's centre:
      // "nearest free pocket to the router", measured once, the same way
      // for every candidate.
      const d = Math.hypot(p.x + (CARD_RIGHT - CARD_LEFT) / 2 - ROUTER_RIGHT, p.y - (SHOULDER_TOP + SHOULDER_BOTTOM) / 2)
      const c = { p, d, cost: frameCost(box) }
      ;(insideFrame(box) ? near : far).push(c)
    }
  }
  type C = { p: Spot; d: number; cost: number }
  // Inside the frame: nearest the router first. Outside it: whichever
  // costs the map least scale first, and among equals the nearest --
  // the frame grows only as the group does, and as little as it can.
  const byNear = (a: C, b: C) => a.d - b.d || a.p.y - b.p.y || a.p.x - b.p.x
  const byCost = (a: C, b: C) => a.cost - b.cost || byNear(a, b)
  return [...near.sort(byNear), ...far.sort(byCost)].map((c) => c.p)
}

const CANDIDATES = candidates()

/** Which shoulder a card's strand lands on: the top edge for a pocket
 * above the router and left of wg0, the right edge otherwise. */
function usesTopEdge(p: Spot): boolean {
  return p.y + CARD_HALF_H < ROUTER_TOP_EDGE.y && p.x < WG0.x - 40
}

export interface PackInput {
  /** How many tunnels are drawn, busiest first. */
  count: number
  /** Whether each tunnel carries an aggregate bar (in the same order). */
  bars?: boolean[]
  /** The furniture already on the map, in map units. */
  obstacles: Obstacles
}

/**
 * The packing rule. The first (busiest) tunnel keeps round 30's literal
 * seat and rib; each of the rest takes the nearest free pocket to the
 * router, and its own strand then becomes an obstacle for the ones after
 * it -- so the group is deterministic, and adding a quieter tunnel never
 * moves a busier one.
 */
export function packCluster(input: PackInput): Placed[] {
  const out: Placed[] = []
  if (input.count <= 0) return out

  const cards: Box[] = [...input.obstacles.boxes]
  const curves: Pt[][] = [...input.obstacles.curves]
  // Only the tunnel cards block a strand: every strand lands on the
  // router by design, and the lane cards sit past where any of them go.
  const tunnelCards: Box[] = []

  const bar0 = input.bars?.[0] ?? false
  out.push({ ...WG0, literal: true, strand: WG0_STRAND, anchor: { x: 1080, y: 186 } })
  cards.push(cardBox(WG0, bar0))
  tunnelCards.push(cardBox(WG0, bar0))
  curves.push(samplePath(WG0_STRAND_PTS))

  const taken = new Set<string>([`${WG0.x},${WG0.y}`])
  let shoulder = 0
  let topLandings = 0
  for (let n = 1; n < input.count; n++) {
    const bar = input.bars?.[n] ?? false
    let spot: Spot | null = null
    for (const c of CANDIDATES) {
      if (taken.has(`${c.x},${c.y}`)) continue
      if (!pocketFree(cardBox(c, bar), { boxes: cards, curves })) continue
      spot = c
      break
    }
    // Every lattice pocket taken or blocked: rather than drop a tunnel
    // off the map -- nothing hidden, nothing invented -- it goes on past
    // the last column, and the frame grows to hold it.
    if (!spot) spot = { x: WG0.x + (7 + n) * COL_PITCH, y: WG0.y + (n % 2) * ROW_PITCH }
    taken.add(`${spot.x},${spot.y}`)

    const topEdge = usesTopEdge(spot)
    // The shoulder points march down the router's right edge from wg0's
    // own 252; a top-edge landing steps left along the top instead, so
    // two strands never arrive at the same point.
    const land = topEdge
      ? { x: Math.max(730, ROUTER_TOP_EDGE.x - 14 * topLandings++), y: ROUTER_TOP_EDGE.y }
      : { x: ROUTER_RIGHT, y: Math.min(SHOULDER_BOTTOM, SHOULDER_TOP + SHOULDER_STEP * shoulder++) }
    const { d, anchor, points } = strandFor(spot, land, topEdge, tunnelCards)
    out.push({ ...spot, literal: false, strand: d, anchor })
    cards.push(cardBox(spot, bar))
    tunnelCards.push(cardBox(spot, bar))
    curves.push(points)
  }
  return out
}

/* ---------------- the frame ---------------- */

/**
 * Round 51/52's fit: the union of the nominal frame and every drawn
 * node's bounds plus 40 px of air. The frame is the floor, so it only
 * ever grows -- a map that fits opens exactly as round 49 drew it.
 */
export function topographyFrame(nodes: Box[], pad = 40): Box {
  return growFrame(NOMINAL_FRAME, nodes, pad)
}

/** How far out a viewBox is, as the fit chip reads it: the scale this
 * box is drawn at against the scale the nominal frame is drawn at. */
export function zoomPercent(box: Box): number {
  // The same fit the city uses, with no pad: the 40 px of air is already
  // inside the box by the time it gets here.
  const scale = (b: Box) => fitScale(b.w, b.h, STAGE_W, STAGE_H, 0)
  return Math.round((scale(box) / scale(NOMINAL_FRAME)) * 100)
}

/** The viewBox attribute for a box. */
export function viewBoxOf(b: Box): string {
  return `${r1(b.x)} ${r1(b.y)} ${r1(b.w)} ${r1(b.h)}`
}
