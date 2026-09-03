// SPDX-License-Identifier: AGPL-3.0-only
//
// What stands on a plinth (#863). One symbol per building kind, drawn
// once in local pixels for a footprint radius of 1 at S = SREF, then
// stamped and scaled by R·S/SREF: local px are 1 ground u = IK·SREF
// across, 1 ground v = VK·SREF down, 1 unit of height = ZK·SREF up --
// the map's own projection, so a stamp sits in the isometric without a
// transform of its own (the mockup's DEVLIB).
//
// Today every kind is a plain block. symbolFor is the single stamping
// point the device library (#864) replaces: it returns paths, never
// markup, so City.svelte renders it with elements and never as raw HTML.
import { IK, R2, SREF, VK, ZK, boxPts } from './project'
import type { BuildingKind } from './types'

const LU = IK * SREF
const LV = VK * SREF
const LZ = ZK * SREF

export interface SymbolPath {
  d: string
  /** 'body' takes the building's ink (currentColor); 'void' is the dark
   * backing every face gets first so a block reads as solid. */
  fill: 'body' | 'void'
  fillOpacity?: number
  strokeOpacity?: number
  strokeWidth?: number
}

export interface BuildingSymbol {
  /** How tall it stands in local px, for placing a name above it. */
  top: number
  paths: SymbolPath[]
}

const lp = (u: number, v: number, z = 0) => R2(u * LU) + ' ' + R2(v * LV - z * LZ)

/** A box's three visible faces in local px (the mockup's lbox). */
export function lbox(A: number, B: number, z0: number, h: number): { top: string; right: string; left: string } {
  const p = boxPts(0, 0, A, B)
  const zt = z0 + h
  const P = (k: keyof typeof p, z: number) => lp(p[k][0], p[k][1], z)
  return {
    top: 'M' + P('K', zt) + 'L' + P('R', zt) + 'L' + P('F', zt) + 'L' + P('L', zt) + 'Z',
    right: 'M' + P('F', zt) + 'L' + P('R', zt) + 'L' + P('R', z0) + 'L' + P('F', z0) + 'Z',
    left: 'M' + P('L', zt) + 'L' + P('F', zt) + 'L' + P('F', z0) + 'L' + P('L', z0) + 'Z',
  }
}

/** The mockup's lfaces: void backing, then the three lit faces. */
export function lfaces(b: { top: string; right: string; left: string }, o: { l?: number; r?: number; t?: number } = {}): SymbolPath[] {
  return [
    { d: b.left, fill: 'void' },
    { d: b.right, fill: 'void' },
    { d: b.top, fill: 'void' },
    { d: b.left, fill: 'body', fillOpacity: o.l ?? 0.38, strokeOpacity: 0.55, strokeWidth: 0.35 },
    { d: b.right, fill: 'body', fillOpacity: o.r ?? 0.72, strokeOpacity: 0.55, strokeWidth: 0.35 },
    { d: b.top, fill: 'body', fillOpacity: o.t ?? 0.95, strokeOpacity: 0.9, strokeWidth: 0.45 },
  ]
}

const BLOCKS: Record<BuildingKind, BuildingSymbol> = {
  // A router: the flat wide chassis, no lights yet.
  router: { top: 0.34 * LZ + 4, paths: lfaces(lbox(0.5, 0.86, 0, 0.34), { t: 0.95, r: 0.74, l: 0.4 }) },
  'router-ant': { top: 0.34 * LZ + 4, paths: lfaces(lbox(0.5, 0.86, 0, 0.34), { t: 0.95, r: 0.74, l: 0.4 }) },
  // A host: a block a little taller than wide.
  host: { top: 0.7 * LZ + 3, paths: lfaces(lbox(0.5, 0.5, 0, 0.7)) },
  // A bridge head's gate post: narrow and tall.
  post: { top: 1.2 * LZ + 2, paths: lfaces(lbox(0.3, 0.3, 0, 1.2), { t: 0.9, r: 0.6, l: 0.34 }) },
}

/** symbolFor is the one place a building's look comes from. */
export function symbolFor(kind: BuildingKind): BuildingSymbol {
  return BLOCKS[kind] ?? BLOCKS.host
}
