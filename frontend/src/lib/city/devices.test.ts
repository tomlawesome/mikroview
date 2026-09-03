// SPDX-License-Identifier: AGPL-3.0-only
//
// The library's shapes are judged by eye, not by assertion -- the
// reviewed captures are docs/design/screens/city/devices-*.png. What is
// worth holding in a test is the contract around them: that every type
// has a symbol, that no symbol carries a coordinate that would silently
// draw nothing, and that a flag only ever changes the signal ink.

import { describe, expect, it } from 'vitest'
import {
  DEVICE_KINDS,
  DEVICE_KIND_LABEL,
  DEVICE_LIBRARY,
  SIG_ALARM,
  SIG_REST,
  SREF,
  deviceScale,
  deviceStampAttrs,
  deviceSymbolId,
  deviceTop,
} from './devices'

describe('the device library', () => {
  it('has all eleven types the record names', () => {
    expect(DEVICE_KINDS).toHaveLength(11)
    expect(Object.keys(DEVICE_LIBRARY).sort()).toEqual([...DEVICE_KINDS].sort())
  })

  for (const kind of DEVICE_KINDS) {
    it(`draws ${kind} with a spoken name, a height and finite geometry`, () => {
      const symbol = DEVICE_LIBRARY[kind]
      expect(DEVICE_KIND_LABEL[kind]).toBeTruthy()
      expect(deviceTop(kind)).toBeGreaterThan(0)
      expect(symbol.parts.length).toBeGreaterThan(0)
      for (const part of symbol.parts) {
        expect(part.fill).toBeTruthy()
        if (part.shape === 'path') expect(part.d).not.toMatch(/NaN|undefined/)
        for (const n of [part.cx, part.cy, part.r, part.rx, part.ry]) {
          if (n !== undefined) expect(Number.isFinite(n)).toBe(true)
        }
      }
    })
  }

  it('gives every scene its own symbol ids, so a <use> never crosses SVG roots', () => {
    expect(deviceSymbolId('street', 'router')).toBe('street-router')
    expect(deviceSymbolId('city', 'router')).not.toBe(deviceSymbolId('street', 'router'))
  })
})

describe('stamping', () => {
  it('scales a symbol by its footprint and the camera, nothing else', () => {
    // Drawn for footprint radius 1 at S = SREF; 0.74 keeps it inside
    // the plinth. Importance never appears here (#867 moves the plinth).
    expect(deviceScale(1, SREF)).toBeCloseTo(0.74)
    expect(deviceScale(4.6, 17)).toBeCloseTo(deviceScale(4.6, 8.5) * 2)
  })

  it('carries the district ink in, so one symbol serves every VLAN', () => {
    const attrs = deviceStampAttrs('server', 's', { ink: 'var(--lane-iot)' })
    expect(attrs.href).toBe('#s-server')
    expect(attrs.style).toContain('color:var(--lane-iot)')
  })

  it('rests on the resting signal ink', () => {
    expect(deviceStampAttrs('camera', 's').style).toContain(`--sig:${SIG_REST}`)
  })

  it('glows a flagged device in the alarm ink, and changes nothing else', () => {
    const rest = deviceStampAttrs('camera', 's', { ink: 'var(--lane-iot)', scale: 3, x: 10, y: 20 })
    const lit = deviceStampAttrs('camera', 's', {
      ink: 'var(--lane-iot)',
      scale: 3,
      x: 10,
      y: 20,
      flagged: true,
    })
    expect(lit.style).toContain(`--sig:${SIG_ALARM}`)
    expect(lit.href).toBe(rest.href)
    expect(lit.transform).toBe(rest.transform)
  })

  it('lets a caller name the signal ink outright', () => {
    expect(deviceStampAttrs('post', 's', { sig: 'var(--accent)' }).style).toContain(
      '--sig:var(--accent)',
    )
  })

  it('places the device at its ground point', () => {
    expect(deviceStampAttrs('puck', 's', { x: 12.34, y: -5, scale: 2 }).transform).toBe(
      'translate(12.3 -5) scale(2)',
    )
  })
})
