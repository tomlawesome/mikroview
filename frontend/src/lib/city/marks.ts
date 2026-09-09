// SPDX-License-Identifier: AGPL-3.0-only
//
// The mark a flagged or watched building wears (#981, round 46, ratified
// 2026-09-06). Ported from docs/design/concepts/round-46/marks.html --
// `hullPointsFor`, `convexHull`, `offsetPoly` and the mark's own
// arithmetic -- so the drawing and the product agree by construction.
//
// The rules, in the record's own words:
//
//   - flagged: the device symbol's shapes take `--alarm` as fill at
//     `min(0.9, 0.45 + 0.15*(flags-1))`, stamped over the ordinary
//     district-ink symbol, and its silhouette strokes once in the same
//     ink at `min(3, 1 + 0.5*(flags-1))`.
//   - watched: the silhouette strokes once in the watcher's ink at
//     `min(4, 1.5 + 0.75*(watch-1))`, pushed outward from the flag rim
//     by `rimW/2 + watchW/2 + 0.6` so the two lines sit side by side
//     rather than on top of one another.
//   - a mark exists only while there is something behind it, in both
//     views, and there is no toggle (owner, 2026-09-08): "something
//     that's always there is easy to ignore; if it's not always there
//     you know it's there for a reason."
//
// No discs, rings, numbers, glyphs or tally, at any stop -- the owner
// on the round's first crops: "I said to remove these rings with
// numbers. Get rid of them completely." Counts exist as words on the
// click card and nowhere else.
//
// Why ONE silhouette: a symbol is several boxes with no boolean union
// available to fuse them, and the first attempt stroked every
// constituent face -- "These overlapping planes are weird ... I can't
// even tell what you're trying to show me." Every part contributes its
// corners and Andrew's monotone chain turns them into one polygon,
// stroked once. A hull cannot follow a concave silhouette, but one
// clean line that slightly rounds the shape beats a cage of true edges.
import { symbolFor, type SymbolPath } from './blocks'
import { R2, type Pt } from './project'
import type { BuildingKind } from './types'

/**
 * Andrew's monotone chain: the convex hull of a set of 2D points. The
 * same hull City.svelte's borough ring already draws, kept here as the
 * one implementation both a silhouette and a ring can share.
 */
export function convexHull(pts: readonly Pt[]): Pt[] {
  const sorted = pts.slice().sort((a, b) => a[0] - b[0] || a[1] - b[1])
  const cross = (o: Pt, a: Pt, b: Pt) => (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0])
  const lower: Pt[] = []
  const upper: Pt[] = []
  for (const p of sorted) {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) lower.pop()
    lower.push(p)
  }
  for (let i = sorted.length - 1; i >= 0; i--) {
    const p = sorted[i]
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) upper.pop()
    upper.push(p)
  }
  return lower.slice(0, -1).concat(upper.slice(0, -1))
}

/**
 * A hull pushed outward from its own centroid by a fixed distance --
 * how the watch mark sits "a touch proud" of the flag rim beneath it
 * without the two strokes landing on top of each other. An offset in
 * stroke widths rather than a scale factor: a scale moves a big
 * symbol's line further than a small one's, where this puts the two
 * lines side by side at every size.
 */
export function offsetPoly(hull: readonly Pt[], d: number): Pt[] {
  if (!d) return hull.slice()
  const cx = hull.reduce((s, p) => s + p[0], 0) / hull.length
  const cy = hull.reduce((s, p) => s + p[1], 0) / hull.length
  return hull.map((p): Pt => {
    const dx = p[0] - cx
    const dy = p[1] - cy
    const m = Math.hypot(dx, dy) || 1
    return [p[0] + (dx / m) * d, p[1] + (dy / m) * d]
  })
}

/** A hull as one closed path. */
export function hullPath(hull: readonly Pt[]): string {
  return 'M' + hull.map((p) => R2(p[0]) + ' ' + R2(p[1])).join('L') + 'Z'
}

/**
 * Every corner a symbol's parts contribute, in the symbol's own local
 * pixels. blocks.ts builds each face as an `M x y L x y ... Z` polygon
 * (lbox/lfaces), so every coordinate pair in a face's path data is a
 * corner and reading the numbers back out needs no shape-by-shape
 * knowledge -- a symbol built from something other than boxes still
 * contributes its points without this function being taught about it.
 */
export function hullPointsFor(paths: readonly SymbolPath[]): Pt[] {
  const pts: Pt[] = []
  for (const p of paths) {
    const nums = p.d.match(/-?\d*\.?\d+(?:e[-+]?\d+)?/gi)
    if (!nums) continue
    for (let i = 0; i + 1 < nums.length; i += 2) pts.push([Number(nums[i]), Number(nums[i + 1])])
  }
  return pts
}

const HULLS = new Map<BuildingKind, Pt[]>()

/** One building kind's silhouette, computed once per kind. */
export function hullFor(kind: BuildingKind): Pt[] {
  let h = HULLS.get(kind)
  if (!h) HULLS.set(kind, (h = convexHull(hullPointsFor(symbolFor(kind).paths))))
  return h
}

/** What a marked building draws, ready for the template. */
export interface BuildingMark {
  flags: number
  watch: number
  spike: boolean
  /** The alarm re-stamp's opacity, climbing with the flag count. */
  fillOpacity: number
  /** The silhouette, and the flag rim's width on it. */
  rimD: string
  rimWidth: number
  /** The blurred copy of the rim an activity spike breathes; the CSS
   * animation drives the width, so this is only its resting figure. */
  glowWidth: number
  /** The silhouette again, pushed clear of the flag rim when there is
   * one, and the watch line's width on it. */
  watchD: string
  watchWidth: number
}

/**
 * The mark for one building, or null when nothing is behind it --
 * `flags === 0 && watch === 0` draws nothing at all, which is the whole
 * point of there being no toggle.
 */
export function markFor(kind: BuildingKind, m: { flags: number; watch: number; spike: boolean }): BuildingMark | null {
  const flags = Math.max(0, m.flags)
  const watch = Math.max(0, m.watch)
  if (flags === 0 && watch === 0) return null
  const hull = hullFor(kind)
  const rimWidth = flags > 0 ? R2(Math.min(3, 1 + 0.5 * (flags - 1))) : 0
  const watchWidth = watch > 0 ? R2(Math.min(4, 1.5 + 0.75 * (watch - 1))) : 0
  const proud = rimWidth && watchWidth ? R2(rimWidth / 2 + watchWidth / 2 + 0.6) : 0
  return {
    flags,
    watch,
    // The spike only ever pulses a flagged mark: the animation moves
    // opacity and width, never hue, so a breathing mark is a red mark.
    spike: flags > 0 && m.spike,
    fillOpacity: flags > 0 ? Math.round(Math.min(0.9, 0.45 + 0.15 * (flags - 1)) * 1000) / 1000 : 0,
    rimD: hullPath(hull),
    rimWidth,
    glowWidth: R2(rimWidth * 1.8),
    watchD: hullPath(offsetPoly(hull, proud)),
    watchWidth,
  }
}
