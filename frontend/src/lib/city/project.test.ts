import { describe, expect, it } from 'vitest'
import {
  STAGE_H,
  STAGE_W,
  STOPS,
  STOP_HEIGHT,
  X,
  Y,
  cam,
  centreOf,
  clampCentre,
  clampRingX,
  clearDropLabels,
  diamond,
  ease,
  gbox,
  groundAt,
  lerpCam,
  minimapCam,
  panBy,
  ringHalfW,
  viewportRect,
} from './project'

describe('city projection', () => {
  it('frames the asked-for ground point at the stage centre', () => {
    const c = cam(-10, 40, 8)
    expect(X(c, -10)).toBeCloseTo(STAGE_W / 2)
    expect(Y(c, 40)).toBeCloseTo(STAGE_H / 2)
    expect(centreOf(c)[0]).toBeCloseTo(-10)
    expect(centreOf(c)[1]).toBeCloseTo(40)
  })

  it('inverts the projection on the ground', () => {
    const c = cam(3, 7, 11)
    const [u, v] = groundAt(c, X(c, 22), Y(c, -5))
    expect(u).toBeCloseTo(22)
    expect(v).toBeCloseTo(-5)
  })

  it('lifts a point by its height and projects 2:1', () => {
    const c = cam(0, 0, 10)
    expect(Y(c, 0, 1)).toBeLessThan(Y(c, 0, 0))
    expect(X(c, 1) - X(c, 0)).toBeCloseTo(1.02 * 10)
    expect(Y(c, 1) - Y(c, 0)).toBeCloseTo(0.5 * 10)
  })

  it('has four stops, each higher than the last', () => {
    expect(STOPS).toEqual(['city', 'borough', 'district', 'street'])
    for (let i = 1; i < STOPS.length; i++) expect(STOP_HEIGHT[STOPS[i]]).toBeGreaterThan(STOP_HEIGHT[STOPS[i - 1]])
  })

  it('pans freely and reports the ground the stage shows', () => {
    const c = cam(0, 0, 10)
    const moved = panBy(c, 102, 50)
    expect(centreOf(moved)[0]).toBeCloseTo(-10)
    expect(centreOf(moved)[1]).toBeCloseTo(-10)
    const r = viewportRect(c)
    expect(r.u0).toBeLessThan(0)
    expect(r.u1).toBeGreaterThan(0)
    expect(r.u1 - r.u0).toBeCloseTo(STAGE_W / 10.2)
    expect(r.v1 - r.v0).toBeCloseTo(STAGE_H / 5)
  })

  it('clamps a centre to the estate', () => {
    const b = { u0: -100, u1: 100, v0: -50, v1: 150 }
    expect(clampCentre([-500, 20], b)).toEqual([-100, 20])
    expect(clampCentre([20, 900], b)).toEqual([20, 150])
    expect(clampCentre([0, 0], b)).toEqual([0, 0])
  })

  it('fits the estate into the minimap', () => {
    const b = { u0: -104, u1: 96, v0: -96, v1: 140 }
    const m = minimapCam(b, 214, 132)
    for (const [u, v] of [
      [b.u0, b.v0],
      [b.u1, b.v1],
    ]) {
      expect(X(m, u)).toBeGreaterThanOrEqual(0)
      expect(X(m, u)).toBeLessThanOrEqual(214)
      expect(Y(m, v)).toBeGreaterThanOrEqual(0)
      expect(Y(m, v)).toBeLessThanOrEqual(132)
    }
  })

  it('moves between cameras with an ease that starts and ends exactly', () => {
    const a = cam(0, 0, 5.9)
    const b = cam(30, 40, 17)
    expect(lerpCam(a, b, 0)).toEqual(a)
    expect(lerpCam(a, b, 1)).toEqual(b)
    expect(ease(0)).toBe(0)
    expect(ease(1)).toBe(1)
    expect(ease(0.5)).toBeGreaterThan(0.5)
  })

  it('draws a diamond and a box as closed paths', () => {
    const c = cam(0, 0, 10)
    expect(diamond(c, 0, 0, 4)).toMatch(/^M[\d. -]+L[\d. -]+L[\d. -]+L[\d. -]+Z$/)
    const b = gbox(c, 0, 0, 2, 3, 0, 4)
    expect(b.top.endsWith('Z')).toBe(true)
    expect(b.left.endsWith('Z')).toBe(true)
    expect(b.right.endsWith('Z')).toBe(true)
  })

  it('pushes a drop-mark callout clear of a borough label it would otherwise print over (#982)', () => {
    const dropLabels = [{ x: 400, y: 100, text: 'caught by iot-egress-drop' }]
    const rings = [{ x: 410, y: 100, label: 'HAP-AX3 BOROUGH · 3 DISTRICT' }]
    clearDropLabels(dropLabels, rings)
    expect(Math.abs(dropLabels[0].y - rings[0].y)).toBeGreaterThanOrEqual(14)
  })

  it('moves the callout below when there is no room above (near the top of the map)', () => {
    const dropLabels = [{ x: 400, y: 8, text: 'caught by iot-egress-drop' }]
    const rings = [{ x: 410, y: 8, label: 'HAP-AX3 BOROUGH · 3 DISTRICT' }]
    clearDropLabels(dropLabels, rings)
    expect(dropLabels[0].y).toBeGreaterThan(8)
  })

  it('leaves a callout untouched when it never overlaps a label', () => {
    const dropLabels = [{ x: 0, y: 0, text: 'caught by iot-egress-drop' }]
    const rings = [{ x: 500, y: 500, label: 'HAP-AX3 BOROUGH · 3 DISTRICT' }]
    clearDropLabels(dropLabels, rings)
    expect(dropLabels[0]).toEqual({ x: 0, y: 0, text: 'caught by iot-egress-drop' })
  })

  it('keeps a borough label inside the stage rather than letting it run off (#1139)', () => {
    const label = '172.23.0.1 BOROUGH · 5 DISTRICTS'
    const half = ringHalfW(label)
    // What the operator saw: at the borough stop (scale 1) the label's
    // own centre landed at 1392 on a 1400-wide stage, so all but its
    // first few characters were off the edge.
    const off = clampRingX(621, label, 771.4, 1)
    expect(771.4 + off + half).toBeLessThanOrEqual(STAGE_W)
    expect(771.4 + off - half).toBeGreaterThanOrEqual(0)

    // The same at the far edge, and at a scale where the text shrinks
    // with the drawing.
    const left = clampRingX(-900, label, 100, 0.7)
    expect(100 + left * 0.7 - half * 0.7).toBeGreaterThanOrEqual(0)

    // A label already well inside is not moved.
    expect(clampRingX(0, label, 700, 1)).toBeCloseTo(0, 5)
  })
})
