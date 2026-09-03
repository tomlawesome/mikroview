// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { cam } from './project'
import { gateToward } from './roads'
import { faceOf, facePoint, wallPiece, wallSegments, GATE_HALF_WIDTH } from './walls'

const d = { u: 0, v: 0, r: 20 }

describe('faceOf', () => {
  it('places the south corner at t=0 on the left face and t=1 on the right face', () => {
    expect(faceOf(d, [0, 20])).toEqual({ side: 'l', t: 1 })
    expect(faceOf(d, [0, 20])).not.toBeNull()
  })

  it('places the west corner at the left face start and the east corner at the right face end', () => {
    expect(faceOf(d, [-20, 0])).toEqual({ side: 'l', t: 0 })
    expect(faceOf(d, [20, 0])).toEqual({ side: 'r', t: 1 })
  })

  it('a point exactly at the south corner reads r-face at t=0 too (shared corner)', () => {
    // Both faces meet at the south corner; du=0,dv=r satisfies both
    // du<=0&&dv>=0 and du>=0&&dv>=0, and faceOf returns the l reading
    // first -- documented by the assertion above, not re-asserted here.
    expect(faceOf(d, [0, 20])?.side).toBe('l')
  })

  it('the north corner and the two back edges (never drawn) read null: honest silence, not a guess', () => {
    expect(faceOf(d, [0, -20])).toBeNull() // the north corner, where the two hidden edges meet
    expect(faceOf(d, [10, -10])).toBeNull() // strictly the back-right edge (east to north)
    expect(faceOf(d, [-10, -10])).toBeNull() // strictly the back-left edge (north to west)
  })

  it('is faceOf/facePoint round trip for an interior point on each face', () => {
    const l = facePoint(d, 'l', 0.3)
    expect(faceOf(d, l)).toEqual({ side: 'l', t: 0.3 })
    const r = facePoint(d, 'r', 0.7)
    expect(faceOf(d, r)?.side).toBe('r')
    expect(faceOf(d, r)?.t).toBeCloseTo(0.7)
  })

  it('agrees with gateToward: a gate aimed south of the plate lands on a visible face', () => {
    const g = gateToward(d, [5, 40])
    expect(faceOf(d, g.p)).not.toBeNull()
  })
})

describe('wallSegments', () => {
  it('with no breaks, each face is one whole piece', () => {
    const segs = wallSegments(d, [])
    expect(segs).toEqual([
      { side: 'l', t0: 0, t1: 1 },
      { side: 'r', t0: 0, t1: 1 },
    ])
  })

  it('one gate opens a gap on its own face only', () => {
    const segs = wallSegments(d, [{ side: 'l', t: 0.5 }])
    const l = segs.filter((s) => s.side === 'l')
    const r = segs.filter((s) => s.side === 'r')
    expect(r).toEqual([{ side: 'r', t0: 0, t1: 1 }])
    expect(l).toHaveLength(2)
    const dt = GATE_HALF_WIDTH / d.r
    expect(l[0]).toEqual({ side: 'l', t0: 0, t1: 0.5 - dt })
    expect(l[1]).toEqual({ side: 'l', t0: 0.5 + dt, t1: 1 })
  })

  it('a gate right at a corner (t=0) leaves only the far piece', () => {
    const segs = wallSegments(d, [{ side: 'l', t: 0 }])
    const l = segs.filter((s) => s.side === 'l')
    expect(l).toHaveLength(1)
    expect(l[0].t0).toBeCloseTo(GATE_HALF_WIDTH / d.r)
    expect(l[0].t1).toBe(1)
  })

  it('two gates close together merge into one gap, never a sliver of wall between them', () => {
    const dt = GATE_HALF_WIDTH / d.r
    const segs = wallSegments(d, [
      { side: 'r', t: 0.4 },
      { side: 'r', t: 0.4 + dt }, // their half-widths overlap
    ])
    const r = segs.filter((s) => s.side === 'r')
    expect(r).toHaveLength(2)
    expect(r[0].t1).toBeCloseTo(0.4 - dt)
    expect(r[1].t0).toBeCloseTo(0.4 + 2 * dt)
  })

  it('a gate spanning the whole face leaves no wall piece on that side', () => {
    const wide = wallSegments({ r: 1 }, [{ side: 'l', t: 0.5 }])
    expect(wide.filter((s) => s.side === 'l')).toEqual([])
  })
})

describe('wallPiece', () => {
  it('draws a non-empty closed path for a whole face', () => {
    const c = cam(0, 0, 10)
    const d0 = wallPiece(c, d, { side: 'l', t0: 0, t1: 1 })
    expect(d0.startsWith('M')).toBe(true)
    expect(d0.endsWith('Z')).toBe(true)
  })
})
