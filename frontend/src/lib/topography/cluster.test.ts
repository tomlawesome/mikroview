// SPDX-License-Identifier: AGPL-3.0-only
import { describe, it, expect } from 'vitest'
import { boxesOverlap } from '../fit'
import {
  CARD_H,
  CARD_W,
  NOMINAL_FRAME,
  WG0,
  WG0_STRAND,
  cardBox,
  packCluster,
  samplePath,
  topographyFrame,
  viewBoxOf,
  zoomPercent,
  type Obstacles,
  type Placed,
} from './cluster'

// The furniture round 49 draws, in map units: the Internet island with
// its bar, the waist, and four lane cards on the row's own pitch. The
// same boxes Topography.svelte hands the packer.
function furniture(lanes = 4): Obstacles {
  const pitch = 271
  const laneX = (i: number) => 700 + (i - (lanes - 1) / 2) * pitch
  const boxes = [
    { x: 600, y: 38, w: 200, h: 80 }, // Internet card + aggregate bar
    { x: 572, y: 234, w: 256, h: 68 }, // the waist
  ]
  const curves = [
    // the trunk, internet to waist
    samplePath([
      [700, 104],
      [700, 150],
      [700, 190],
      [700, 232],
    ]),
  ]
  for (let i = 0; i < lanes; i++) {
    const x = laneX(i)
    boxes.push({ x: x - 108, y: 466, w: 216, h: 150 })
    const spread = lanes === 1 ? 0 : -55 + (110 / (lanes - 1)) * i
    curves.push(
      samplePath([
        [700 + spread, 302],
        [700 + spread * 2.2, 380],
        [x + (700 - x) * 0.25, 420],
        [x, 480],
      ]),
    )
  }
  return { boxes, curves }
}

const boxesOf = (placed: Placed[]) => placed.map((p) => cardBox(p))

describe('the tunnel cluster packing rule (#890, round 52)', () => {
  it('leaves one tunnel exactly where round 30 drew it', () => {
    // "With one tunnel nothing moves" -- the round-52 README. The seat
    // and the rib are both literal, so #877's own scenario still holds.
    const placed = packCluster({ count: 1, obstacles: furniture() })
    expect(placed).toHaveLength(1)
    expect(placed[0].x).toBe(WG0.x)
    expect(placed[0].y).toBe(WG0.y)
    expect(placed[0].strand).toBe(WG0_STRAND)
  })

  it('gives every tunnel its own 188x56 card and its own strand', () => {
    const placed = packCluster({ count: 5, obstacles: furniture() })
    expect(placed).toHaveLength(5)
    for (const p of placed) {
      const b = cardBox(p)
      expect(b.w).toBe(CARD_W)
      expect(b.h).toBe(CARD_H)
      expect(p.strand.startsWith('M')).toBe(true)
    }
    expect(new Set(placed.map((p) => p.strand)).size).toBe(5)
  })

  it('never overlaps two cards, at any count', () => {
    for (const n of [2, 3, 5, 8, 12]) {
      const boxes = boxesOf(packCluster({ count: n, obstacles: furniture() }))
      for (let i = 0; i < boxes.length; i++) {
        for (let j = i + 1; j < boxes.length; j++) {
          expect(boxesOverlap(boxes[i], boxes[j]), `cards ${i} and ${j} overlap at n=${n}`).toBe(false)
        }
      }
    }
  })

  it('never puts a card on the furniture already drawn', () => {
    const obs = furniture()
    for (const b of boxesOf(packCluster({ count: 8, obstacles: obs }))) {
      for (const f of obs.boxes) expect(boxesOverlap(b, f), `${JSON.stringify(b)} sits on ${JSON.stringify(f)}`).toBe(false)
    }
  })

  it('keeps the group in the space right of the router', () => {
    // One group where wg0 is, not a card wandering off to the left of
    // the waist -- the verdict's own words.
    for (const p of packCluster({ count: 8, obstacles: furniture() })) {
      expect(cardBox(p).x).toBeGreaterThanOrEqual(828)
    }
  })

  it('is staggered: no two cards share both a column and an adjacent row', () => {
    const placed = packCluster({ count: 8, obstacles: furniture() })
    for (let i = 0; i < placed.length; i++) {
      for (let j = i + 1; j < placed.length; j++) {
        const sameColumn = placed[i].x === placed[j].x
        const adjacentRow = Math.abs(placed[i].y - placed[j].y) < 94
        expect(sameColumn && adjacentRow).toBe(false)
      }
    }
  })

  it('places the busiest nearest the router, and adding a quieter one moves nobody', () => {
    const three = packCluster({ count: 3, obstacles: furniture() })
    const five = packCluster({ count: 5, obstacles: furniture() })
    expect(five.slice(0, 3).map((p) => [p.x, p.y])).toEqual(three.map((p) => [p.x, p.y]))
  })

  it('fills the free space before it spills past the frame', () => {
    // Two, three and five tunnels all fit the map round 49 drew, so the
    // frame does not move for any of them.
    for (const n of [2, 3, 5]) {
      const frame = topographyFrame(boxesOf(packCluster({ count: n, obstacles: furniture() })))
      expect(viewBoxOf(frame), `n=${n}`).toBe('0 0 1400 720')
    }
  })
})

describe('the frame fits the map (#890 item 3)', () => {
  it('is the nominal frame when everything already fits', () => {
    expect(topographyFrame([{ x: 600, y: 300, w: 200, h: 60 }])).toEqual(NOMINAL_FRAME)
    expect(zoomPercent(NOMINAL_FRAME)).toBe(100)
  })

  it('is the union of the nominal frame and every node plus 40 px', () => {
    const frame = topographyFrame([{ x: 1300, y: 700, w: 188, h: 56 }])
    // 1300 + 188 + 40 = 1528 wide; 700 + 56 + 40 = 796 tall.
    expect(frame).toEqual({ x: 0, y: 0, w: 1528, h: 796 })
  })

  it('grows on every edge, never inward', () => {
    const frame = topographyFrame([{ x: -100, y: -80, w: 188, h: 56 }])
    expect(frame).toEqual({ x: -140, y: -120, w: 1540, h: 840 })
  })

  it('never scales closer than 1:1 -- the frame only grows', () => {
    for (const n of [1, 2, 3, 5, 8, 12]) {
      const frame = topographyFrame(boxesOf(packCluster({ count: n, obstacles: furniture() })))
      expect(frame.w, `n=${n}`).toBeGreaterThanOrEqual(NOMINAL_FRAME.w)
      expect(frame.h, `n=${n}`).toBeGreaterThanOrEqual(NOMINAL_FRAME.h)
      expect(zoomPercent(frame), `n=${n}`).toBeLessThanOrEqual(100)
    }
  })

  it('grows once the pockets inside the frame are used up', () => {
    const frame = topographyFrame(boxesOf(packCluster({ count: 12, obstacles: furniture() })))
    expect(frame.w > NOMINAL_FRAME.w || frame.h > NOMINAL_FRAME.h).toBe(true)
    expect(zoomPercent(frame)).toBeLessThan(100)
  })

  it('holds every card it was given, whatever the count', () => {
    for (const n of [2, 5, 8, 12]) {
      const boxes = boxesOf(packCluster({ count: n, obstacles: furniture() }))
      const f = topographyFrame(boxes)
      for (const b of boxes) {
        expect(b.x - 40, `n=${n}`).toBeGreaterThanOrEqual(f.x)
        expect(b.y - 40, `n=${n}`).toBeGreaterThanOrEqual(f.y)
        expect(b.x + b.w + 40, `n=${n}`).toBeLessThanOrEqual(f.x + f.w)
        expect(b.y + b.h + 40, `n=${n}`).toBeLessThanOrEqual(f.y + f.h)
      }
    }
  })
})
