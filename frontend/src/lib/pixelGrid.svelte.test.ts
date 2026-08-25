// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { snapFill, snapLine } from './pixelGrid.svelte'

// The metrics record's "sharp" clause (#488): filled marks land on a
// whole device pixel, hairlines land in the middle of one. Getting the
// second wrong is what turns a 1px rule into a 2px grey smear, and it is
// invisible in a screenshot taken at DPR 1 -- hence a test rather than
// an eyeball.
describe('the device-pixel grid', () => {
  it('snaps a filled mark to a whole device pixel', () => {
    expect(snapFill(10.4, 1)).toBe(10)
    expect(snapFill(10.6, 1)).toBe(11)
    expect(snapFill(10.4, 2)).toBe(10.5)
    expect(snapFill(10.3, 2)).toBe(10.5)
  })

  it('snaps a hairline to the middle of a device pixel', () => {
    expect(snapLine(10, 1)).toBe(10.5)
    expect(snapLine(10.4, 1)).toBe(10.5)
    expect(snapLine(10, 2)).toBe(10.25)
    expect(snapLine(10.4, 2)).toBe(10.75)
  })

  it('handles a fractional ratio without landing between pixels', () => {
    const dpr = 1.5
    const v = snapLine(37.2, dpr)
    expect(Number.isInteger(v * dpr - 0.5)).toBe(true)
    expect(Number.isInteger(snapFill(37.2, dpr) * dpr)).toBe(true)
  })

  it('falls back to a ratio of 1 rather than dividing by zero', () => {
    expect(snapFill(10.6, 0)).toBe(11)
    expect(snapLine(10, 0)).toBe(10.5)
  })
})
