// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { anyBaselineWarming } from './learningShelf'
import type { LearningState } from './types'

// #642's ruling, amendment 2: "warming" means observed-but-below-floor
// (ready < keys) on an enabled detection -- exactly the state
// engine.Snapshot.ProvisionalFire can fire in. No traffic at all
// (keys 0) is not warming: nothing observed can land on the shelf, so
// claiming "a spike would show here" would be false.
describe('anyBaselineWarming (#642)', () => {
  const learning = (keys: number, ready: number): LearningState => ({
    floor: { minDurationSeconds: 1209600 },
    keys,
    ready,
  })

  it('reports true when an enabled detection has an observed key below its floor', () => {
    expect(anyBaselineWarming([{ enabled: true, learning: learning(3, 1) }])).toBe(true)
  })

  it('reports false for an empty detector list', () => {
    expect(anyBaselineWarming([])).toBe(false)
  })

  it('ignores a disabled detection, however warm its baseline', () => {
    expect(anyBaselineWarming([{ enabled: false, learning: learning(3, 0) }])).toBe(false)
  })

  it('ignores a detection with no warm-up concept at all', () => {
    expect(anyBaselineWarming([{ enabled: true }])).toBe(false)
  })

  it('does not call "no traffic seen yet" warming -- nothing observed can be provisional', () => {
    expect(anyBaselineWarming([{ enabled: true, learning: learning(0, 0) }])).toBe(false)
  })

  it('reports false once every observed key is ready', () => {
    expect(anyBaselineWarming([{ enabled: true, learning: learning(4, 4) }])).toBe(false)
  })

  it('one warming detection among settled ones is enough', () => {
    expect(
      anyBaselineWarming([
        { enabled: true, learning: learning(4, 4) },
        { enabled: true, learning: learning(2, 1) },
      ]),
    ).toBe(true)
  })
})
