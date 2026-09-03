import { describe, expect, it } from 'vitest'
import { bezAt, bezRange, bezTangent, bulge, dm, gateToward, roadPieces, routeRound, segsOf, GATE_STANDOFF } from './roads'
import type { Pt } from './project'
import type { District, Road } from './types'

const plate = (id: string, u: number, v: number, r: number): District => ({
  id,
  name: id,
  cidr: null,
  u,
  v,
  r,
  ink: 0,
  routerId: 'r',
  dark: false,
  buildings: [],
  more: 0,
  gates: [],
  rulesPushed: true,
})

describe('city roads: curves', () => {
  const pts: Pt[] = [
    [0, 0],
    [10, 4],
    [18, 14],
    [30, 16],
  ]

  it('passes through every waypoint and joins without an elbow', () => {
    const segs = segsOf(pts)
    expect(segs).toHaveLength(3)
    segs.forEach((s, i) => {
      expect(s[0]).toEqual(pts[i])
      expect(s[3]).toEqual(pts[i + 1])
    })
    for (let i = 1; i < segs.length; i++) {
      const out = bezTangent(segs[i - 1], 1)
      const into = bezTangent(segs[i], 0)
      expect(out[0]).toBeCloseTo(into[0], 9)
      expect(out[1]).toBeCloseTo(into[1], 9)
    }
  })

  it('cuts a sub-range that lies on the same curve', () => {
    const s = segsOf(pts)[1]
    const q = bezRange(s, 0.25, 0.75)
    const a = bezAt(s, 0.25)
    const b = bezAt(s, 0.75)
    expect(q[0][0]).toBeCloseTo(a[0])
    expect(q[0][1]).toBeCloseTo(a[1])
    expect(q[3][0]).toBeCloseTo(b[0])
    expect(q[3][1]).toBeCloseTo(b[1])
    const m = bezAt(q, 0.5)
    const m2 = bezAt(s, 0.5)
    expect(m[0]).toBeCloseTo(m2[0])
    expect(m[1]).toBeCloseTo(m2[1])
  })
})

describe('city roads: gates', () => {
  it('puts the gate on the plate edge, square-on to the far end', () => {
    const d = plate('lan', -76, 36, 21)
    const g = gateToward(d, [-10, 10])
    expect(dm(g.p, [d.u, d.v])).toBeCloseTo(d.r)
    expect(dm(g.out, [d.u, d.v])).toBeCloseTo(d.r + GATE_STANDOFF)
    // The outward normal points at the target.
    expect(Math.sign(g.n1[0])).toBe(1)
    expect(Math.sign(g.n1[1])).toBe(-1)
  })
})

describe('city roads: routing', () => {
  it('never leaves a road straight', () => {
    const b = bulge(
      [
        [0, 0],
        [40, 0],
      ],
      'x',
    )
    expect(b).toHaveLength(3)
    expect(Math.abs(b[1][1])).toBeGreaterThan(2)
  })

  it('bends a run round a plate in its way', () => {
    const wall = plate('srv', 20, 0, 12)
    const pts: Pt[] = [
      [-30, 0],
      [70, 0],
    ]
    const out = routeRound(pts, [wall], { exempt: new Set() })
    expect(out.length).toBeGreaterThan(2)
    const via = out[1]
    expect(dm(via, [wall.u, wall.v])).toBeGreaterThan(wall.r)
    const segs = segsOf(out)
    for (const s of segs) for (let t = 0; t <= 1; t += 0.05) expect(dm(bezAt(s, t), [wall.u, wall.v])).toBeGreaterThan(wall.r - 0.5)
  })

  it('leaves an exempt plate alone', () => {
    const wall = plate('srv', 20, 0, 12)
    const pts: Pt[] = [
      [-30, 0],
      [70, 0],
    ]
    expect(routeRound(pts, [wall], { exempt: new Set(['srv']) })).toEqual(pts)
  })
})

describe('city roads: pieces', () => {
  const road: Road = {
    id: 'r',
    pts: [
      [0, 0],
      [20, 6],
      [44, 10],
    ],
    w: 2,
    k: 'a',
    from: 'a',
    to: 'b',
    label: 'a to b',
  }
  const ents = new Map([
    ['a', { u: 0, v: 0, R: 5 }],
    ['b', { u: 44, v: 10, R: 4 }],
  ])

  it('trims the road at each footprint and butts the pieces end to end', () => {
    const pieces = roadPieces(road, ents)
    expect(pieces.length).toBeGreaterThan(3)
    expect(dm(pieces[0].q[0], [0, 0])).toBeGreaterThan(5)
    expect(dm(pieces[pieces.length - 1].q[3], [44, 10])).toBeGreaterThan(4)
    for (let i = 1; i < pieces.length; i++) {
      expect(pieces[i].q[0][0]).toBeCloseTo(pieces[i - 1].q[3][0])
      expect(pieces[i].q[0][1]).toBeCloseTo(pieces[i - 1].q[3][1])
    }
    for (const p of pieces) expect(p.v).toBeCloseTo((p.q[0][1] + p.q[3][1]) / 2)
  })
})
