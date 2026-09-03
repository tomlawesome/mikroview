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
  diamond,
  ease,
  gbox,
  groundAt,
  lerpCam,
  minimapCam,
  panBy,
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
})
