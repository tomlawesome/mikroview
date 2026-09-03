// SPDX-License-Identifier: AGPL-3.0-only
//
// The city's projection and cameras (#863), ported from round 40's
// isometric.html (docs/design/concepts/round-40). Ground space is
// (u, v); a ground point at height z projects to
//
//     x = ox + u·IK·S ,  y = oy + v·VK·S − z·ZK·S
//
// with IK/VK = 2:1, the ordinary game isometric. A square tile turned 45°
// therefore draws as a diamond, which is why every solid in the town
// (plates, plinths, decks, blocks) is built on the two diagonal axes
// d1 = (1,1) and d2 = (1,−1): those project to the 2:1 screen axes.
//
// One map; every stop is a camera onto it. The stop sets the height S,
// pan sets the centre, and nothing here knows what it is projecting.
export const IK = 1.02
export const VK = 0.5
export const ZK = 0.72
/** The scale a device symbol is drawn at in local pixels (blocks.ts). */
export const SREF = 10

/** The stage the mockup's scenes were framed on, and the viewBox
 * City.svelte draws into. */
export const STAGE_W = 1400
export const STAGE_H = 700

export type Pt = [number, number]

export interface Cam {
  S: number
  ox: number
  oy: number
}

/** The four city stops, right of the slider's diamond. */
export const STOPS = ['city', 'borough', 'district', 'street'] as const
export type Stop = (typeof STOPS)[number]

/** Camera height per stop: the mockup's own scene scales -- survey
 * (5.9) for the whole estate, estate (8.0) for a borough, pan/walls
 * (11) for a district, street (17) for the buildings up close. */
export const STOP_HEIGHT: Record<Stop, number> = { city: 5.9, borough: 8.0, district: 11, street: 17 }

/** cam frames ground point (cu, cv) at the stage's centre at height S. */
export function cam(cu: number, cv: number, S: number, w = STAGE_W, h = STAGE_H): Cam {
  return { S, ox: w / 2 - cu * IK * S, oy: h / 2 - cv * VK * S }
}

export const X = (c: Cam, u: number): number => c.ox + u * IK * c.S
export const Y = (c: Cam, v: number, z = 0): number => c.oy + v * VK * c.S - z * ZK * c.S

/** One decimal place: what the mockup writes into every path. */
export const R2 = (n: number): number => Math.round(n * 10) / 10

/** The ground point under a stage pixel (z = 0). */
export function groundAt(c: Cam, x: number, y: number): Pt {
  return [(x - c.ox) / (IK * c.S), (y - c.oy) / (VK * c.S)]
}

/** The ground point the camera is centred on. */
export function centreOf(c: Cam, w = STAGE_W, h = STAGE_H): Pt {
  return groundAt(c, w / 2, h / 2)
}

/** A camera moved by a screen delta: free pan, the same at every stop. */
export function panBy(c: Cam, dx: number, dy: number): Cam {
  return { S: c.S, ox: c.ox + dx, oy: c.oy + dy }
}

export interface GroundRect {
  u0: number
  u1: number
  v0: number
  v1: number
}

/** The ground the stage shows: the minimap's viewport rectangle. */
export function viewportRect(c: Cam, w = STAGE_W, h = STAGE_H): GroundRect {
  const [u0, v0] = groundAt(c, 0, 0)
  const [u1, v1] = groundAt(c, w, h)
  return { u0, u1, v0, v1 }
}

/**
 * clampCentre keeps a pan inside the estate: the centre may reach the
 * bounds' edge but never leave it, so the town can always be found
 * again from wherever a drag ended.
 */
export function clampCentre(p: Pt, bounds: GroundRect): Pt {
  return [Math.max(bounds.u0, Math.min(bounds.u1, p[0])), Math.max(bounds.v0, Math.min(bounds.v1, p[1]))]
}

/**
 * minimapCam fits the estate's bounds into a W×H panel, the mockup's own
 * fit (isometric.html minimap()): the same projection, so a plate on
 * the minimap is the same diamond smaller.
 */
export function minimapCam(bounds: GroundRect, W: number, H: number, pad = 6): Cam {
  const s = Math.min((W - 2 * pad) / ((bounds.u1 - bounds.u0) * IK || 1), (H - 2 * pad) / ((bounds.v1 - bounds.v0) * VK || 1))
  return {
    S: s,
    ox: W / 2 - ((bounds.u0 + bounds.u1) / 2) * IK * s,
    oy: H / 2 - ((bounds.v0 + bounds.v1) / 2) * VK * s,
  }
}

/** The mockup's easing for a camera move, sampled: 0..1 in, 0..1 out. */
export function ease(t: number): number {
  // cubic-bezier(0.45, 0.02, 0.2, 1), close enough by a smoothstep on
  // its slow start: what matters is that reduced motion never calls it.
  const x = Math.max(0, Math.min(1, t))
  return 1 - Math.pow(1 - x, 3)
}

/** A camera part-way between two, for the move between stops. */
export function lerpCam(a: Cam, b: Cam, t: number): Cam {
  return { S: a.S + (b.S - a.S) * t, ox: a.ox + (b.ox - a.ox) * t, oy: a.oy + (b.oy - a.oy) * t }
}

/* ---- the diamond and box geometry every solid is built from ---- */

/** A diamond footprint (plate, plinth top, plinth base) as a path. */
export function diamond(c: Cam, u: number, v: number, R: number, z = 0): string {
  return (
    'M' + R2(X(c, u)) + ' ' + R2(Y(c, v - R, z)) +
    'L' + R2(X(c, u + R)) + ' ' + R2(Y(c, v, z)) +
    'L' + R2(X(c, u)) + ' ' + R2(Y(c, v + R, z)) +
    'L' + R2(X(c, u - R)) + ' ' + R2(Y(c, v, z)) + 'Z'
  )
}

/** One of the two faces of a diamond prism the camera can see. */
export function wallFace(c: Cam, u: number, v: number, R: number, h: number, side: 'l' | 'r', z0 = 0): string {
  const a: Pt = side === 'l' ? [u - R, v] : [u, v + R]
  const b: Pt = side === 'l' ? [u, v + R] : [u + R, v]
  const ax = R2(X(c, a[0]))
  const bx = R2(X(c, b[0]))
  const ay = Y(c, a[1], z0)
  const by = Y(c, b[1], z0)
  const rise = h * ZK * c.S
  return 'M' + ax + ' ' + R2(ay) + 'L' + bx + ' ' + R2(by) + 'L' + bx + ' ' + R2(by - rise) + 'L' + ax + ' ' + R2(ay - rise) + 'Z'
}

/** The four ground corners of a box on the diagonal axes: half-extent A
 * along d1 (screen: right and down), B along d2 (right and up). */
export function boxPts(u: number, v: number, A: number, B: number): { L: Pt; F: Pt; R: Pt; K: Pt } {
  return {
    L: [u - A - B, v - A + B],
    F: [u + A - B, v + A + B],
    R: [u + A + B, v + A - B],
    K: [u - A + B, v - A - B],
  }
}

export interface BoxFaces {
  top: string
  right: string
  left: string
}

/** A box's three visible faces, base z0, height h. */
export function gbox(c: Cam, u: number, v: number, A: number, B: number, z0: number, h: number): BoxFaces {
  const p = boxPts(u, v, A, B)
  const zt = z0 + h
  const P = (k: keyof typeof p, z: number) => R2(X(c, p[k][0])) + ' ' + R2(Y(c, p[k][1], z))
  return {
    top: 'M' + P('K', zt) + 'L' + P('R', zt) + 'L' + P('F', zt) + 'L' + P('L', zt) + 'Z',
    right: 'M' + P('F', zt) + 'L' + P('R', zt) + 'L' + P('R', z0) + 'L' + P('F', z0) + 'Z',
    left: 'M' + P('L', zt) + 'L' + P('F', zt) + 'L' + P('F', z0) + 'L' + P('L', z0) + 'Z',
  }
}
