// SPDX-License-Identifier: AGPL-3.0-only
//
// The one fit rule both maps use (#890). The city had it first
// (`cityFitS`, lib/city/project.ts): fit the content into the stage with
// a margin of air on every edge, and cap the answer so the fit only ever
// pulls back -- a small estate opens at the stop's own height rather than
// zoomed in past it. Round 51/52 gave the 2D topography the same rule, so
// it lives here rather than being written twice: `cityFitS` now calls
// `fitScale`, and Topography.svelte's frame calls `growFrame`.
//
// Pure: no DOM, no store, no clock.

/** An axis-aligned rectangle in whatever units the caller is working in. */
export interface Box {
  x: number
  y: number
  w: number
  h: number
}

/**
 * The scale that puts `contentW × contentH` inside `stageW × stageH` with
 * `pad` of air on every edge, never above `cap`.
 *
 * A zero-sized content box would divide by zero, so it falls back to 1
 * unit of extent -- the same guard `minimapCam` has always had.
 */
export function fitScale(
  contentW: number,
  contentH: number,
  stageW: number,
  stageH: number,
  pad = 40,
  cap = Number.POSITIVE_INFINITY,
): number {
  const s = Math.min((stageW - 2 * pad) / (contentW || 1), (stageH - 2 * pad) / (contentH || 1))
  return Math.min(cap, s)
}

/**
 * The smallest box containing `frame` and every one of `boxes` grown by
 * `pad` on each edge. The frame is the floor, so this only ever grows:
 * content that already fits leaves the frame exactly as it was, which is
 * round 51's "never closer than 1:1" written as a rectangle rather than
 * as a scale.
 */
export function growFrame(frame: Box, boxes: Box[], pad = 40): Box {
  let x0 = frame.x
  let y0 = frame.y
  let x1 = frame.x + frame.w
  let y1 = frame.y + frame.h
  for (const b of boxes) {
    x0 = Math.min(x0, b.x - pad)
    y0 = Math.min(y0, b.y - pad)
    x1 = Math.max(x1, b.x + b.w + pad)
    y1 = Math.max(y1, b.y + b.h + pad)
  }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
}

/** Whether two boxes share any area, `gap` being the clearance each is
 * required to keep from the other. */
export function boxesOverlap(a: Box, b: Box, gap = 0): boolean {
  return a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap
}

/** Whether a point falls inside a box grown by `gap`. */
export function boxHasPoint(b: Box, x: number, y: number, gap = 0): boolean {
  return x >= b.x - gap && x <= b.x + b.w + gap && y >= b.y - gap && y <= b.y + b.h + gap
}
