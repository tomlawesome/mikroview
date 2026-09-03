// SPDX-License-Identifier: AGPL-3.0-only
//
// Roads on the ground (#863), ported from round 40's isometric.html.
// Every road is a smooth curve through its waypoints: Catmull-Rom
// converted to cubic Béziers in ground coordinates, then projected --
// so no road has a straight run or an elbow, and a road that leaves a
// plate leaves it square-on because the plate's edge contributes the
// waypoint just outside it. Projection is affine on the ground plane,
// so projecting the four control points projects the curve exactly.
//
// A road is also cut into short pieces, each with its own depth, so
// roads and buildings interleave in one painter's order (depth.ts)
// instead of one painting over the other.
import type { Pt } from './project'
import type { District, Road } from './types'

export type Seg = [Pt, Pt, Pt, Pt]

/** Catmull-Rom through the waypoints, duplicated ends, as cubics. */
export function segsOf(pts: Pt[]): Seg[] {
  const n = pts.length
  const out: Seg[] = []
  const g = (i: number) => pts[Math.max(0, Math.min(n - 1, i))]
  for (let i = 0; i < n - 1; i++) {
    const p0 = g(i - 1)
    const p1 = g(i)
    const p2 = g(i + 1)
    const p3 = g(i + 2)
    const T = 1 / 6
    out.push([p1, [p1[0] + (p2[0] - p0[0]) * T, p1[1] + (p2[1] - p0[1]) * T], [p2[0] - (p3[0] - p1[0]) * T, p2[1] - (p3[1] - p1[1]) * T], p2])
  }
  return out
}

export function bezAt(s: Seg, t: number): Pt {
  const m = 1 - t
  return [
    m * m * m * s[0][0] + 3 * m * m * t * s[1][0] + 3 * m * t * t * s[2][0] + t * t * t * s[3][0],
    m * m * m * s[0][1] + 3 * m * m * t * s[1][1] + 3 * m * t * t * s[2][1] + t * t * t * s[3][1],
  ]
}

/** The curve's direction at t, unnormalised. */
export function bezTangent(s: Seg, t: number): Pt {
  const m = 1 - t
  return [
    3 * m * m * (s[1][0] - s[0][0]) + 6 * m * t * (s[2][0] - s[1][0]) + 3 * t * t * (s[3][0] - s[2][0]),
    3 * m * m * (s[1][1] - s[0][1]) + 6 * m * t * (s[2][1] - s[1][1]) + 3 * t * t * (s[3][1] - s[2][1]),
  ]
}

export const lerpP = (p: Pt, q: Pt, t: number): Pt => [p[0] + (q[0] - p[0]) * t, p[1] + (q[1] - p[1]) * t]

/** The sub-curve between parameters a and b (de Casteljau twice). */
export function bezRange(s: Seg, a: number, b: number): Seg {
  let x01 = lerpP(s[0], s[1], b)
  let x12 = lerpP(s[1], s[2], b)
  let x23 = lerpP(s[2], s[3], b)
  let y0 = lerpP(x01, x12, b)
  let y1 = lerpP(x12, x23, b)
  const left: Seg = [s[0], x01, y0, lerpP(y0, y1, b)]
  const t2 = b > 0 ? a / b : 0
  x01 = lerpP(left[0], left[1], t2)
  x12 = lerpP(left[1], left[2], t2)
  x23 = lerpP(left[2], left[3], t2)
  y0 = lerpP(x01, x12, t2)
  y1 = lerpP(x12, x23, t2)
  return [lerpP(y0, y1, t2), y1, x23, left[3]]
}

/** The diamond metric: the footprints are diamonds, so this is exact. */
export const dm = (p: Pt, q: Pt): number => Math.abs(p[0] - q[0]) + Math.abs(p[1] - q[1])

/** The road's own point at t (0..1 along the whole road). */
export function roadPoint(pts: Pt[], t: number): Pt {
  const segs = segsOf(pts)
  const x = Math.min(0.999, Math.max(0, t)) * segs.length
  return bezAt(segs[Math.floor(x)], x - Math.floor(x))
}

/* ---------------- gates: where a road meets a plate ---------------- */

export interface Gate {
  /** The point on the plate's edge. */
  p: Pt
  /** Outward normal, diamond-normalised. */
  n1: Pt
  /** The waypoint just outside the wall, so the crossing is square-on. */
  out: Pt
}

/** How far outside the wall a road's first free waypoint sits. */
export const GATE_STANDOFF = 5.5

/**
 * gateToward is where a road toward `target` crosses the plate's edge:
 * on the diamond's boundary in that direction, with the point just
 * outside that makes the road leave square-on (isometric.html
 * initModel's `out`).
 */
export function gateToward(d: { u: number; v: number; r: number }, target: Pt): Gate {
  const dir: Pt = [target[0] - d.u, target[1] - d.v]
  const s = Math.abs(dir[0]) + Math.abs(dir[1]) || 1
  const t = d.r / s
  const p: Pt = [d.u + dir[0] * t, d.v + dir[1] * t]
  const n1: Pt = [dir[0] / s, dir[1] / s]
  return { p, n1, out: [p[0] + n1[0] * GATE_STANDOFF, p[1] + n1[1] * GATE_STANDOFF] }
}

/* ---------------- routing: round the plates, never through ---------------- */

/** Euclidean distance from p to the segment ab, and the nearest t. */
function segDist(p: Pt, a: Pt, b: Pt): { d: number; t: number } {
  const dx = b[0] - a[0]
  const dy = b[1] - a[1]
  const L2 = dx * dx + dy * dy || 1
  const t = Math.max(0, Math.min(1, ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / L2))
  const q: Pt = [a[0] + dx * t, a[1] + dy * t]
  return { d: Math.hypot(p[0] - q[0], p[1] - q[1]), t }
}

/** The plates a road may pass through: the ones it starts or ends in. */
export interface RouteOpts {
  exempt: Set<string>
}

/**
 * routeRound bends a road's waypoints round any plate the straight run
 * between two of them would cross. A plate in the way contributes a
 * waypoint beside it, on whichever side the run already leans toward,
 * pushed clear of the plate's radius. One pass per plate is enough for
 * a town this size; the result is still only waypoints -- the curve
 * through them is what makes it a road.
 */
export function routeRound(pts: Pt[], plates: District[], opts: RouteOpts): Pt[] {
  let out = pts.slice()
  for (let pass = 0; pass < 3; pass++) {
    let bent = false
    const next: Pt[] = [out[0]]
    for (let i = 1; i < out.length; i++) {
      const a = out[i - 1]
      const b = out[i]
      let hit: { d: District; t: number; side: number } | null = null
      for (const d of plates) {
        if (opts.exempt.has(d.id)) continue
        const { d: dist, t } = segDist([d.u, d.v], a, b)
        // The plate is a diamond of radius r: its inscribed circle is
        // r/√2, so a run within that of the centre certainly crosses it.
        if (dist < d.r * 0.71 + 3 && t > 0.02 && t < 0.98) {
          const dx = b[0] - a[0]
          const dy = b[1] - a[1]
          const side = Math.sign((d.u - a[0]) * dy - (d.v - a[1]) * dx) || 1
          if (!hit || t < hit.t) hit = { d, t, side }
        }
      }
      if (hit) {
        const dx = b[0] - a[0]
        const dy = b[1] - a[1]
        const L = Math.hypot(dx, dy) || 1
        // Perpendicular away from the plate, on the run's own side of it.
        const px = (-dy / L) * -hit.side
        const py = (dx / L) * -hit.side
        const clear = hit.d.r + 9
        next.push([hit.d.u + px * clear, hit.d.v + py * clear])
        bent = true
      }
      next.push(b)
    }
    out = next
    if (!bent) break
  }
  return out
}

/**
 * bulge guarantees a road is never straight: a two-point road through
 * Catmull-Rom is a line, so every road gets a mid waypoint pushed off
 * its chord. The side alternates by a hash of the road's id so parallel
 * roads do not all lean the same way.
 */
export function bulge(pts: Pt[], id: string, amount = 0.14): Pt[] {
  if (pts.length !== 2) return pts
  const [a, b] = pts
  const dx = b[0] - a[0]
  const dy = b[1] - a[1]
  const L = Math.hypot(dx, dy) || 1
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
  const side = h % 2 === 0 ? 1 : -1
  const k = L * amount * side
  return [a, [(a[0] + b[0]) / 2 + (-dy / L) * k, (a[1] + b[1]) / 2 + (dx / L) * k], b]
}

/* ---------------- pieces: the road cut for the painter ---------------- */

export interface Entity {
  u: number
  v: number
  R: number
}

export interface RoadPiece {
  /** The sub-Bézier, in ground space. */
  q: Seg
  /** Painter's depth: the piece's mean v. */
  v: number
  /** 0..1 along the road, for the highway's fade. */
  gt: number
}

/**
 * roadPieces trims a road against the footprints it leaves and arrives
 * at, then cuts every segment into short sub-Béziers that butt exactly
 * end to end (overlapping translucent strokes would bead the road where
 * they meet). Each piece carries its own depth.
 */
export function roadPieces(r: Road, ents: Map<string, Entity>): RoadPiece[] {
  const segs = segsOf(r.pts)
  const trim = (idx: number, end: 's' | 'e', id: string | null) => {
    const e = id ? ents.get(id) : undefined
    if (!e) return
    const s = segs[idx]
    let t = -1
    for (let i = 0; i <= 40; i++) {
      const k = end === 's' ? i / 40 : 1 - i / 40
      if (dm(bezAt(s, k), [e.u, e.v]) > e.R + 0.6) {
        t = k
        break
      }
    }
    if (t < 0) return
    segs[idx] = end === 's' ? bezRange(s, t, 1) : bezRange(s, 0, t)
  }
  trim(0, 's', r.from)
  trim(segs.length - 1, 'e', r.to)

  const out: RoadPiece[] = []
  const total = segs.length
  segs.forEach((s, si) => {
    const len = dm(s[0], s[3]) + dm(s[1], s[2])
    const n = Math.max(2, Math.min(9, Math.ceil(len / 5)))
    for (let k = 0; k < n; k++) {
      const q = bezRange(s, k / n, (k + 1) / n)
      out.push({ q, v: (q[0][1] + q[3][1]) / 2, gt: (si + (k + 0.5) / n) / total })
    }
  })
  return out
}
