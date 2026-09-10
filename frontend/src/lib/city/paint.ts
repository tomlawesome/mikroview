// SPDX-License-Identifier: AGPL-3.0-only
//
// The ground-paint shape City.svelte's scene builder emits, and the
// curve helpers shared by the road painter and the river (#866). Pulled
// out of City.svelte so the river's own geometry (river.ts) can be built
// and tested without mounting a component -- everything here is plain
// data and pure functions, never markup: City.svelte renders a Paint as
// ordinary <path>/<ellipse> elements (guards/injection-sinks.test.ts
// bans building SVG as markup strings).
import { R2, X, Y, type Cam, type Pt } from './project'
import { segsOf } from './roads'

export interface Paint {
  d?: string
  cx?: number
  cy?: number
  rx?: number
  ry?: number
  fill?: string
  fo?: number
  stroke?: string
  so?: number
  sw?: number
  cls?: string
  dash?: string
}

export const P = (c: Cam, p: Pt, z = 0): string => R2(X(c, p[0])) + ' ' + R2(Y(c, p[1], z))

/** A Catmull-Rom curve through ground points as a projected path. */
export function curvePath(c: Cam, pts: Pt[]): string {
  let d = ''
  segsOf(pts).forEach((s, i) => {
    d += (i ? '' : 'M' + P(c, s[0])) + 'C' + P(c, s[1]) + ' ' + P(c, s[2]) + ' ' + P(c, s[3])
  })
  return d
}

/** The filled area between two curves (a forward, b reversed). */
export function between(c: Cam, a: Pt[], b: Pt[]): string {
  const rev = b.slice().reverse()
  return curvePath(c, a) + 'L' + P(c, rev[0]) + curvePath(c, rev).replace(/^M[^C]*/, '') + 'Z'
}
