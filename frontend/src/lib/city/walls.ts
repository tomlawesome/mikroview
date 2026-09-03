// SPDX-License-Identifier: AGPL-3.0-only
//
// A district's wall (#865): the low isometric prism round a plate's
// edge, in the same two visible faces a building's plinth already wears
// (project.ts's wallFace) -- the camera looking down the +v axis never
// sees the back two edges of a diamond, so neither a building's plinth
// nor a plate's wall draws them. A gate breaks the wall open at the
// point a road actually crosses it (roads.ts's gateToward); everything
// here just carves that break out of the two faces in ground-parametric
// terms (0..1 along a face), so City.svelte only ever turns a Cam and a
// district into paths -- no SVG string built in the component itself.
import { R2, X, Y, ZK, type Cam, type Pt } from './project'

export type WallSide = 'l' | 'r'

export interface WallBreak {
  side: WallSide
  /** Position along that face, 0 (near corner) to 1 (far corner). */
  t: number
}

export interface WallSegment {
  side: WallSide
  t0: number
  t1: number
}

/** How low the wall stands -- the record's own word, well under even a
 * host's shortest plinth. */
export const WALL_H = 1.5

/** Half the ground width a gate opens in the wall. */
export const GATE_HALF_WIDTH = 3

/**
 * faceOf classifies a point on a plate's diamond edge (gateToward's own
 * `p`) onto one of the two visible faces and where along it -- or null
 * when the point falls on one of the two edges the camera cannot see,
 * the same silence a hidden building face keeps. Purely geometric: it
 * makes no claim about whether a gate belongs there, only where it
 * would draw if it does.
 */
export function faceOf(d: { u: number; v: number; r: number }, p: Pt): WallBreak | null {
  if (d.r <= 0) return null
  const du = p[0] - d.u
  const dv = p[1] - d.v
  if (du <= 0 && dv >= 0) return { side: 'l', t: dv / d.r }
  if (du >= 0 && dv >= 0) return { side: 'r', t: du / d.r }
  return null
}

/**
 * wallSegments carves a district's two visible faces into the pieces
 * still standing once every gate's break is cut out of them -- merging
 * overlapping breaks first, so two gates close together open one gap
 * rather than leaving a sliver of wall between them nobody could
 * actually walk through.
 */
export function wallSegments(d: { r: number }, breaks: WallBreak[], halfWidth = GATE_HALF_WIDTH): WallSegment[] {
  const out: WallSegment[] = []
  const dt = d.r > 0 ? halfWidth / d.r : 0
  for (const side of ['l', 'r'] as const) {
    const gaps = breaks
      .filter((b) => b.side === side)
      .map((b): [number, number] => [Math.max(0, b.t - dt), Math.min(1, b.t + dt)])
      .sort((a, b) => a[0] - b[0])
    const merged: [number, number][] = []
    for (const g of gaps) {
      const last = merged[merged.length - 1]
      if (last && g[0] <= last[1]) last[1] = Math.max(last[1], g[1])
      else merged.push([g[0], g[1]])
    }
    let cursor = 0
    for (const [g0, g1] of merged) {
      if (g0 > cursor + 1e-9) out.push({ side, t0: cursor, t1: g0 })
      cursor = Math.max(cursor, g1)
    }
    if (cursor < 1 - 1e-9) out.push({ side, t0: cursor, t1: 1 })
  }
  return out
}

/** The ground point at parameter t along one of a plate's two visible
 * faces -- faceOf's inverse. */
export function facePoint(d: { u: number; v: number; r: number }, side: WallSide, t: number): Pt {
  return side === 'l' ? [d.u + d.r * (t - 1), d.v + d.r * t] : [d.u + d.r * t, d.v + d.r * (1 - t)]
}

/** One wall piece's path: the same two-face prism a building's plinth
 * wears (project.ts's wallFace), generalised from a whole corner-to-
 * corner edge to the t-range one of wallSegments handed back. */
export function wallPiece(c: Cam, d: { u: number; v: number; r: number }, seg: WallSegment, h = WALL_H, z0 = 0): string {
  const a = facePoint(d, seg.side, seg.t0)
  const b = facePoint(d, seg.side, seg.t1)
  const ax = R2(X(c, a[0]))
  const bx = R2(X(c, b[0]))
  const ay = Y(c, a[1], z0)
  const by = Y(c, b[1], z0)
  const rise = h * ZK * c.S
  return 'M' + ax + ' ' + R2(ay) + 'L' + bx + ' ' + R2(by) + 'L' + bx + ' ' + R2(by - rise) + 'L' + ax + ' ' + R2(ay - rise) + 'Z'
}
