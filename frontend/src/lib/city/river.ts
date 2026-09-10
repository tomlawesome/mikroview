// SPDX-License-Identifier: AGPL-3.0-only
//
// The river reads as water, not a road (#866): the owner's verdict on
// the mockup was blunt -- "the river reads as a road on both". What
// made it read that way was the two dashed "current" strokes down its
// middle, drawn with the same stroke-dasharray convention as a road's
// lane-flow animation. This file replaces them with banks that have a
// hand-drawn, uneven edge and a ripple texture: short arc strokes, low
// contrast, and -- the point of this file -- NO stroke-dasharray
// anywhere in what it returns. frontend/src/lib/city/river.test.ts
// checks that directly, on the Paint objects themselves, so the guard
// holds even if City.svelte's rendering changes later.
//
// The wobble and the ripple placements are deterministic functions of
// each point's index along the bank, never Math.random(): the same
// estate draws the same water on every render, which is what makes a
// screenshot and a snapshot test meaningful at all. Geometry that other
// code depends on for correctness -- the smooth bank layout.ts computes
// gates and bridges against -- is untouched; only what is drawn here is
// uneven.
//
// X and Y (project.ts) are affine in ground (u, v) at z = 0, so
// projecting a curve's control points individually and drawing the same
// curve order in screen space reproduces the ground-space curve exactly
// (Bezier curves are affine-invariant). That is what lets this file
// build ripple arcs as ground-space quadratics and project their three
// points independently, same as curvePath already does for the banks.
import { R2, X, Y, type Cam, type Pt } from './project'
import { bezAt, bezTangent, segsOf } from './roads'
import type { River } from './types'
import { between, curvePath, type Paint } from './paint'

const VOID = '#080d18'

/** Perpendicular unit normal to the run from a to b. */
function normal(a: Pt, b: Pt): Pt {
  const dx = b[0] - a[0]
  const dy = b[1] - a[1]
  const len = Math.hypot(dx, dy) || 1
  return [-dy / len, dx / len]
}

/**
 * A bank's hand-drawn edge: each control point nudged perpendicular to
 * its neighbours by a small sine wave keyed on its own index. Uneven,
 * not smooth -- but deterministic, not random.
 */
export function wobbleBank(pts: Pt[], amp: number, freq: number, phase: number): Pt[] {
  return pts.map((p, i) => {
    const [nx, ny] = normal(pts[Math.max(0, i - 1)], pts[Math.min(pts.length - 1, i + 1)])
    const off = Math.sin(i * freq + phase) * amp
    return [p[0] + nx * off, p[1] + ny * off]
  })
}

/**
 * A handful of short ripple arcs along a ground-space curve: a
 * low-contrast texture, evenly spaced by curve parameter (never
 * Math.random()). Each is a single shallow quadratic, alternating which
 * way it bows so the texture does not read as a repeating dash.
 */
function rippleArcs(c: Cam, pts: Pt[], perSeg: number, len: number, bow: number): Paint[] {
  const out: Paint[] = []
  let i = 0
  for (const s of segsOf(pts)) {
    for (let k = 0; k < perSeg; k++, i++) {
      const t = (k + 0.5) / perSeg
      const at = bezAt(s, t)
      const tan = bezTangent(s, t)
      const tl = Math.hypot(tan[0], tan[1]) || 1
      const ux = tan[0] / tl
      const uy = tan[1] / tl
      const sign = i % 2 === 0 ? 1 : -1
      const a: Pt = [at[0] - ux * len * 0.5, at[1] - uy * len * 0.5]
      const b: Pt = [at[0] + ux * len * 0.5, at[1] + uy * len * 0.5]
      const m: Pt = [at[0] - uy * bow * sign, at[1] + ux * bow * sign]
      const d = 'M' + R2(X(c, a[0])) + ' ' + R2(Y(c, a[1])) + 'Q' + R2(X(c, m[0])) + ' ' + R2(Y(c, m[1])) + ' ' + R2(X(c, b[0])) + ' ' + R2(Y(c, b[1]))
      out.push({ d, stroke: 'var(--accent)', so: 0.16, sw: 0.9, cls: 'ripple' })
    }
  }
  return out
}

/**
 * riverScene is the river's entire drawing, ground paints only (no
 * bridges -- those are City.svelte's, since they carry per-bridge
 * building/road pieces the depth painter interleaves). Pure: a Cam and
 * a River in, Paint[] out, so river.test.ts can check the "no dashes"
 * invariant without mounting a component.
 */
export function riverScene(c: Cam, river: River): Paint[] {
  const { bankN, bankF, width } = river
  const bankNDrawn = wobbleBank(bankN, 0.9, 1.4, 0)
  const bankFDrawn = wobbleBank(bankF, 0.9, 1.4, 2.1)
  const area = between(c, bankNDrawn, bankFDrawn)
  const mid = bankN.map((p): Pt => [p[0] - width * 0.5, p[1] - width * 0.5])
  const midW = bankN.map((p): Pt => [p[0] - width * 0.28, p[1] - width * 0.72])

  return [
    { d: curvePath(c, bankFDrawn.map((p): Pt => [p[0], p[1] - 58])), fill: VOID },
    { d: area, fill: '#0a1526', fo: 0.94 },
    { d: area, fill: 'var(--accent)', fo: 0.05 },
    ...rippleArcs(c, mid, 1, 9, 1.6),
    ...rippleArcs(c, midW, 1, 8, 1.3),
    { d: curvePath(c, bankNDrawn), stroke: 'var(--accent)', so: 0.4, sw: 1.2 },
    { d: curvePath(c, bankFDrawn), stroke: 'var(--fg-dim)', so: 0.5, sw: 1 },
  ]
}
