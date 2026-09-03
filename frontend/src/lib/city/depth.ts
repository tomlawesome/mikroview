// SPDX-License-Identifier: AGPL-3.0-only
//
// One painter's order (#863). The camera looks down the +v axis, so
// anything with a larger v is nearer and paints later. Roads lie on the
// ground and buildings stand on it, so a road piece behind a building's
// footprint must paint before the building and a piece in front of it
// after: the mockup's own keys -- a piece's mean v, a building's far
// edge v + R -- interleave them correctly, and the test in
// depth.test.ts checks the geometry rather than the keys.
import type { Building } from './types'
import type { RoadPiece } from './roads'

export type Solid =
  | { kind: 'piece'; v: number; road: string; piece: RoadPiece }
  | { kind: 'building'; v: number; building: Building }
  | { kind: 'other'; v: number; id: string }

/** Depth key for a building: its footprint's near corner. */
export const buildingDepth = (b: { v: number; R: number }): number => b.v + b.R

/** Depth key for a road piece: its mean v. */
export const pieceDepth = (p: RoadPiece): number => p.v

/**
 * paintOrder sorts solids far to near. The sort is stable so two solids
 * at the same depth keep the order they were given in; at an exact tie
 * a road piece paints before a building, since the building stands on
 * it.
 */
export function paintOrder<T extends { kind: string; v: number }>(items: T[]): T[] {
  return items
    .map((s, i) => ({ s, i }))
    .sort((a, b) => {
      if (a.s.v !== b.s.v) return a.s.v - b.s.v
      const ra = a.s.kind === 'piece' ? 0 : 1
      const rb = b.s.kind === 'piece' ? 0 : 1
      if (ra !== rb) return ra - rb
      return a.i - b.i
    })
    .map((x) => x.s)
}
