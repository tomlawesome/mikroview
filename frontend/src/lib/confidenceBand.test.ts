// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { confidenceBand } from './confidenceBand'

// The boundaries are the whole of this function's behaviour (#1231), so
// they are what is pinned: 39/40 and 69/70, either side of each step.
describe('confidenceBand (#1231, round 58)', () => {
  it('bands the drawing’s three ranges: low 0-39, moderate 40-69, high 70-100', () => {
    expect(confidenceBand(0)).toBe('low')
    expect(confidenceBand(39)).toBe('low')
    expect(confidenceBand(40)).toBe('moderate')
    expect(confidenceBand(69)).toBe('moderate')
    expect(confidenceBand(70)).toBe('high')
    expect(confidenceBand(100)).toBe('high')
  })
})
