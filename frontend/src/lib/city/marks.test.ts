// SPDX-License-Identifier: AGPL-3.0-only
//
// The mark's geometry and arithmetic (#981, round 46). What the
// component does with it is in components/CityHosts.test.ts.
import { describe, expect, it } from 'vitest'
import { symbolFor } from './blocks'
import { convexHull, hullFor, hullPointsFor, markFor, offsetPoly } from './marks'

const reach = (d: string) => Math.max(...(d.match(/-?\d*\.?\d+/g) ?? []).map((n) => Math.abs(Number(n))))

describe('the silhouette', () => {
  it('reads every corner back out of a symbol built from boxes', () => {
    // Three faces, four corners each, and the shared corners repeated --
    // the hull is what removes the duplicates, not this.
    expect(hullPointsFor(symbolFor('host').paths).length).toBeGreaterThan(0)
  })

  it('is one closed polygon, not a stroke per face', () => {
    const hull = hullFor('host')
    expect(hull.length).toBeGreaterThanOrEqual(3)
    // Every point of the symbol is inside it or on it: each one sits on
    // the same side of every edge, whichever way round the chain walks.
    const side = (a: number[], b: number[], p: number[]) => (b[0] - a[0]) * (p[1] - a[1]) - (b[1] - a[1]) * (p[0] - a[0])
    const turn = Math.sign(side(hull[0], hull[1], hull[2]))
    expect(turn).not.toBe(0)
    for (const p of hullPointsFor(symbolFor('host').paths)) {
      for (let i = 0; i < hull.length; i++) {
        const s = side(hull[i], hull[(i + 1) % hull.length], p) * turn
        expect(s).toBeGreaterThanOrEqual(-1e-6)
      }
    }
  })

  it('drops the interior points a cage of true edges would have kept', () => {
    expect(convexHull([
      [0, 0],
      [10, 0],
      [10, 10],
      [0, 10],
      [5, 5],
    ]).length).toBe(4)
  })

  it('pushes outward from the centroid, so both lines move by the same amount at any size', () => {
    const square: [number, number][] = [
      [-10, -10],
      [10, -10],
      [10, 10],
      [-10, 10],
    ]
    const out = offsetPoly(square, 4)
    for (const [i, p] of out.entries()) expect(Math.hypot(p[0], p[1]) - Math.hypot(square[i][0], square[i][1])).toBeCloseTo(4, 6)
  })
})

describe('what a mark is made of', () => {
  it('is nothing at all when nothing is behind it', () => {
    expect(markFor('host', { flags: 0, watch: 0, spike: false })).toBeNull()
    // Not even a pulse: the spike is drawn inside the flagged branch.
    expect(markFor('host', { flags: 0, watch: 0, spike: true })).toBeNull()
  })

  it('grows the fill and the rim with the flag count, and stops at the cap', () => {
    expect(markFor('host', { flags: 1, watch: 0, spike: false })).toMatchObject({ fillOpacity: 0.45, rimWidth: 1 })
    expect(markFor('host', { flags: 3, watch: 0, spike: false })).toMatchObject({ fillOpacity: 0.75, rimWidth: 2 })
    expect(markFor('host', { flags: 40, watch: 0, spike: false })).toMatchObject({ fillOpacity: 0.9, rimWidth: 3 })
  })

  it('grows the watch line with the watcher count, and stops at its own cap', () => {
    expect(markFor('host', { flags: 0, watch: 1, spike: false })?.watchWidth).toBe(1.5)
    expect(markFor('host', { flags: 0, watch: 2, spike: false })?.watchWidth).toBe(2.3)
    expect(markFor('host', { flags: 0, watch: 40, spike: false })?.watchWidth).toBe(4)
  })

  it('leaves the watch line on the silhouette itself when there is no rim under it', () => {
    const m = markFor('host', { flags: 0, watch: 1, spike: false })!
    expect(m.watchD).toBe(m.rimD)
  })

  it('pushes the watch line clear of the rim by half of each width plus a hair', () => {
    const m = markFor('host', { flags: 1, watch: 1, spike: false })!
    // 1/2 + 1.5/2 + 0.6 = 1.85
    expect(reach(m.watchD) - reach(m.rimD)).toBeCloseTo(1.85, 1)
  })

  it('never pulses an unflagged mark', () => {
    expect(markFor('host', { flags: 0, watch: 2, spike: true })?.spike).toBe(false)
    expect(markFor('host', { flags: 1, watch: 0, spike: true })?.spike).toBe(true)
  })
})
