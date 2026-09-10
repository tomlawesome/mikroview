// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { cam } from './project'
import { riverScene, wobbleBank } from './river'
import type { River } from './types'

const river: River = {
  bankN: [
    [-170, 12],
    [-120, -8],
    [-80, -26],
    [-30, -42],
    [20, -52],
    [70, -58],
    [140, -68],
  ],
  bankF: [
    [-170, -12],
    [-120, -32],
    [-80, -50],
    [-30, -66],
    [20, -76],
    [70, -82],
    [140, -92],
  ],
  width: 24,
}

describe('the river reads as water, not a road', () => {
  const c = cam(0, -30, 6)
  const scene = riverScene(c, river)

  it('carries no dash of any kind -- no stroke-dasharray, no dash-classed stroke', () => {
    for (const p of scene) {
      expect(p.dash).toBeUndefined()
      expect(p.cls ?? '').not.toMatch(/current|flow/)
    }
  })

  it('draws two bank lines and a ripple texture, never an empty scene', () => {
    const strokes = scene.filter((p) => p.stroke)
    expect(strokes.length).toBeGreaterThan(2)
    const ripples = scene.filter((p) => p.cls === 'ripple')
    expect(ripples.length).toBeGreaterThan(0)
    // Ripples read as texture, not roads: low contrast.
    for (const r of ripples) expect(r.so ?? 1).toBeLessThan(0.3)
  })

  it('gives the bank a hand-drawn, uneven edge rather than the plain curve', () => {
    const drawn = wobbleBank(river.bankN, 0.9, 1.4, 0)
    expect(drawn).toHaveLength(river.bankN.length)
    let anyMoved = false
    for (let i = 0; i < drawn.length; i++) {
      if (Math.abs(drawn[i][0] - river.bankN[i][0]) > 1e-6 || Math.abs(drawn[i][1] - river.bankN[i][1]) > 1e-6) anyMoved = true
    }
    expect(anyMoved).toBe(true)
  })

  it('is deterministic: the same bank wobbles the same way every time', () => {
    const a = wobbleBank(river.bankN, 0.9, 1.4, 0)
    const b = wobbleBank(river.bankN, 0.9, 1.4, 0)
    expect(a).toEqual(b)
  })

  it('draws the two banks differently, not as mirror images', () => {
    const n = wobbleBank(river.bankN, 0.9, 1.4, 0)
    const f = wobbleBank(river.bankF, 0.9, 1.4, 2.1)
    // Same amplitude and frequency, different phase: the offsets at a
    // shared index should not match.
    let anyDiffer = false
    for (let i = 1; i < n.length - 1; i++) {
      const dn = n[i][1] - river.bankN[i][1]
      const df = f[i][1] - river.bankF[i][1]
      if (Math.abs(dn - df) > 1e-6) anyDiffer = true
    }
    expect(anyDiffer).toBe(true)
  })
})
