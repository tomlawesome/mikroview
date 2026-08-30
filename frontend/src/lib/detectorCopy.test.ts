// SPDX-License-Identifier: AGPL-3.0-only
//
// learningSummary (#639): the watchers bench's per-definition
// learning-window status line. Covers the five states the issue rules
// on, plus Fable's follow-up ruling (2026-08-30) on a BaselineFloor that
// binds neither dimension -- global_spike ships that way by default (see
// detectorCopy.ts's NO_FLOOR_TEXT doc comment for why).

import { describe, expect, it } from 'vitest'
import { learningSummary } from './detectorCopy'
import type { LearningState } from './types'

describe('learningSummary', () => {
  it('renders nothing for a definition with no warm-up concept at all', () => {
    expect(learningSummary(undefined)).toBeNull()
  })

  it('state 2: no traffic seen yet, worded in the floor\'s own dimension', () => {
    const daysOnly: LearningState = { floor: { minDurationSeconds: 14 * 86400 }, keys: 0, ready: 0 }
    expect(learningSummary(daysOnly)).toBe(
      'Learning — no traffic seen yet; needs 14 days of history per source',
    )

    const samplesOnly: LearningState = { floor: { minSamples: 5 }, keys: 0, ready: 0 }
    expect(learningSummary(samplesOnly)).toBe(
      'Learning — no traffic seen yet; needs 5 samples of history per source',
    )

    const both: LearningState = { floor: { minDurationSeconds: 14 * 86400, minSamples: 14 }, keys: 0, ready: 0 }
    expect(learningSummary(both)).toBe(
      'Learning — no traffic seen yet; needs 14 days, 14 samples of history per source',
    )
  })

  it('state 2 collapses into the no-floor ruling when the floor binds neither dimension', () => {
    const noFloor: LearningState = { floor: {}, keys: 0, ready: 0 }
    expect(learningSummary(noFloor)).toBe(
      'Learning — no traffic seen yet; starts evaluating from its first reading (no minimum history required)',
    )
    // Explicit zeros (rather than omitted keys) mean the same thing --
    // the wire contract's own omitempty is a serialization detail, not
    // one this function should have to know about.
    const explicitZeros: LearningState = { floor: { minDurationSeconds: 0, minSamples: 0 }, keys: 0, ready: 0 }
    expect(learningSummary(explicitZeros)).toBe(learningSummary(noFloor))
  })

  it('state 3: keys observed, none ready, single key -- "Learning: 3 of 14 days"', () => {
    const state: LearningState = {
      floor: { minDurationSeconds: 14 * 86400 },
      keys: 1,
      ready: 0,
      nearest: { observedForSeconds: 3 * 86400, samples: 3 },
    }
    expect(learningSummary(state)).toBe('Learning: 3 of 14 days')
  })

  it('state 3: keys observed, none ready, many keys -- names the nearest and the count ready', () => {
    const state: LearningState = {
      floor: { minSamples: 14 },
      keys: 7,
      ready: 0,
      nearest: { observedForSeconds: 0, samples: 3 },
    }
    expect(learningSummary(state)).toBe('Learning — nearest source 3 of 14 samples (0 of 7 sources ready)')
  })

  it('state 3: both floor dimensions bind (off_hours) -- both render, comma-joined', () => {
    const state: LearningState = {
      floor: { minDurationSeconds: 14 * 86400, minSamples: 14 },
      keys: 1,
      ready: 0,
      nearest: { observedForSeconds: 3 * 86400, samples: 3 },
    }
    expect(learningSummary(state)).toBe('Learning: 3 of 14 days, 3 of 14 samples')
  })

  it('state 3 collapses into the no-floor ruling too -- no fake "3 of X" when there is no X', () => {
    const single: LearningState = { floor: {}, keys: 1, ready: 0, nearest: { observedForSeconds: 1, samples: 1 } }
    expect(learningSummary(single)).toBe(
      'Learning — no traffic seen yet; starts evaluating from its first reading (no minimum history required)',
    )
    const many: LearningState = { floor: {}, keys: 4, ready: 0, nearest: { observedForSeconds: 1, samples: 1 } }
    expect(learningSummary(many)).toBe(learningSummary(single))
  })

  it('state 4: mixed -- ready and learning counts, never a blended state', () => {
    const state: LearningState = {
      floor: { minDurationSeconds: 14 * 86400, minSamples: 14 },
      keys: 50,
      ready: 12,
      nearest: { observedForSeconds: 3 * 86400, samples: 3 },
    }
    expect(learningSummary(state)).toBe('Ready for 12 of 50 sources; 38 still learning')
  })

  it('state 5: every observed key ready -- established, never a done-tick word', () => {
    const one: LearningState = { floor: { minSamples: 5 }, keys: 1, ready: 1 }
    expect(learningSummary(one)).toBe('Baselines established (1 source)')

    const many: LearningState = { floor: { minSamples: 5 }, keys: 50, ready: 50 }
    expect(learningSummary(many)).toBe('Baselines established (50 sources)')

    // Established renders the same whether or not the floor itself binds
    // anything -- global_spike's own steady state (#639 escalation).
    const noFloor: LearningState = { floor: {}, keys: 1, ready: 1 }
    expect(learningSummary(noFloor)).toBe('Baselines established (1 source)')
  })

  it('rounds a floor requirement up and progress toward it down, never overstating readiness', () => {
    // 90000s = 1.0417 days: the requirement must still say 2 days, not 1
    // -- a floor that has not quite finished a day is not satisfied by it.
    const state: LearningState = {
      floor: { minDurationSeconds: 90000 },
      keys: 1,
      ready: 0,
      // 89999s is one second short of a full day short of the 90000s
      // floor -- still only 1 whole day observed, not 2.
      nearest: { observedForSeconds: 89999, samples: 0 },
    }
    expect(learningSummary(state)).toBe('Learning: 1 of 2 days')
  })
})
