import { describe, expect, it } from 'vitest'
import { buildingDepth, paintOrder, pieceDepth, type Solid } from './depth'
import { mockupEstate } from './fixture'
import { layoutGround } from './layout'
import { X, Y, cam, type Cam } from './project'
import { roadPieces, type Entity, type RoadPiece } from './roads'
import type { Building } from './types'

const ground = layoutGround(mockupEstate())
const buildings: Building[] = [...ground.nodes, ...ground.districts.flatMap((d) => d.buildings)]
const ents = new Map<string, Entity>(buildings.map((b) => [b.id, { u: b.u, v: b.v, R: b.R }]))
const pieces = ground.roads.flatMap((r) => roadPieces(r, ents).map((piece) => ({ road: r.id, piece })))

/** A building's screen box: footprint corners on the ground and at its top. */
function buildingBox(c: Cam, b: Building) {
  const xs = [X(c, b.u - b.R), X(c, b.u + b.R)]
  const ys = [Y(c, b.v - b.R, b.h + 2), Y(c, b.v + b.R, 0)]
  return { x0: Math.min(...xs), x1: Math.max(...xs), y0: Math.min(...ys), y1: Math.max(...ys) }
}

function pieceBox(c: Cam, p: RoadPiece) {
  const xs = p.q.map((q) => X(c, q[0]))
  const ys = p.q.map((q) => Y(c, q[1]))
  return { x0: Math.min(...xs), x1: Math.max(...xs), y0: Math.min(...ys), y1: Math.max(...ys) }
}

const overlaps = (a: ReturnType<typeof pieceBox>, b: ReturnType<typeof pieceBox>) => a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1

describe('city depth: one painter\'s order', () => {
  const solids: Solid[] = [
    ...pieces.map((p): Solid => ({ kind: 'piece', v: pieceDepth(p.piece), road: p.road, piece: p.piece })),
    ...buildings.map((b): Solid => ({ kind: 'building', v: buildingDepth(b), building: b })),
  ]
  const order = paintOrder(solids)
  const index = new Map(order.map((s, i) => [s, i]))

  it('cuts roads into pieces short enough to interleave', () => {
    for (const p of pieces) expect(Math.abs(p.piece.q[3][1] - p.piece.q[0][1])).toBeLessThanOrEqual(8)
  })

  it('paints far to near', () => {
    for (let i = 1; i < order.length; i++) expect(order[i].v).toBeGreaterThanOrEqual(order[i - 1].v)
  })

  it('never paints a road piece over a building nearer the camera', () => {
    const c = cam(0, 40, 11)
    let checked = 0
    for (const s of order) {
      if (s.kind !== 'building') continue
      const b = s.building
      const bb = buildingBox(c, b)
      for (const p of order) {
        if (p.kind !== 'piece') continue
        // Nearer building: its near corner is closer to the camera than
        // the piece's middle, and the two overlap on screen. Pieces are
        // short, so a piece straddling the corner is a sliver either way.
        const mid = (p.piece.q[0][1] + p.piece.q[3][1]) / 2
        if (mid > b.v + b.R) continue
        if (!overlaps(pieceBox(c, p.piece), bb)) continue
        checked++
        expect(index.get(p) as number, p.road + ' over ' + b.id).toBeLessThan(index.get(s) as number)
      }
    }
    expect(checked).toBeGreaterThan(20)
  })

  it('paints a piece in front of a building after it', () => {
    for (const s of order) {
      if (s.kind !== 'building') continue
      for (const p of order) {
        if (p.kind !== 'piece') continue
        const near = Math.max(p.piece.q[0][1], p.piece.q[3][1])
        if (near <= s.v) continue
        if (Math.min(p.piece.q[0][1], p.piece.q[3][1]) <= s.v) continue
        expect(index.get(p) as number).toBeGreaterThan(index.get(s) as number)
      }
    }
  })

  it('keeps the given order at a tie, road piece first', () => {
    const a = { kind: 'building', v: 5, id: 'a' }
    const b = { kind: 'piece', v: 5, id: 'b' }
    const c = { kind: 'other', v: 5, id: 'c' }
    expect(paintOrder([a, b, c]).map((s) => s.id)).toEqual(['b', 'a', 'c'])
  })
})
