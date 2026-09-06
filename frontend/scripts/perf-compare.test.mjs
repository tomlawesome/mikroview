// SPDX-License-Identifier: AGPL-3.0-only
//
// Unit tests for perf-compare.mjs's comparison rule: a metric regresses
// only when it is worse than baseline by more than 30% AND more than
// 300ms (frame p95: 30% and 8ms), improvements never fail, and a metric
// missing from either file is 'new'/'missing' rather than a failure.

import { describe, expect, it } from 'vitest'
import { compareMetrics, regressed } from './perf-compare.mjs'

const DEFAULT = { pct: 0.3, absMs: 300 }
const FRAME_P95 = { pct: 0.3, absMs: 8 }

describe('regressed', () => {
  it('is false when current is at or below baseline (improvement never fails)', () => {
    expect(regressed(1000, 900, DEFAULT)).toBe(false)
    expect(regressed(1000, 1000, DEFAULT)).toBe(false)
  })

  it('is false when only the percentage threshold is crossed', () => {
    // +40% but only +40ms absolute -- under the 300ms floor.
    expect(regressed(100, 140, DEFAULT)).toBe(false)
  })

  it('is false when only the absolute threshold is crossed', () => {
    // +301ms on a large baseline is a small percentage.
    expect(regressed(100000, 100301, DEFAULT)).toBe(false)
  })

  it('is true when both thresholds are crossed', () => {
    // +50% and +500ms on a 1000ms baseline.
    expect(regressed(1000, 1500, DEFAULT)).toBe(true)
  })

  it('uses the tighter frame-p95 thresholds', () => {
    // +40% and +4ms -- fails the frame-p95 absolute floor of 8ms even
    // though it would pass the default 300ms floor.
    expect(regressed(10, 14, FRAME_P95)).toBe(false)
    // +40% and +10ms -- crosses both frame-p95 thresholds.
    expect(regressed(10, 20, FRAME_P95)).toBe(true)
  })

  it('treats a zero baseline as an infinite percentage, so only the absolute floor decides', () => {
    // 0 -> 5ms is technically an infinite percentage increase, but 5ms
    // never crosses the 300ms absolute floor -- still not a regression.
    expect(regressed(0, 5, DEFAULT)).toBe(false)
    // 0 -> 400ms crosses it.
    expect(regressed(0, 400, DEFAULT)).toBe(true)
    expect(regressed(0, 0, DEFAULT)).toBe(false)
  })
})

describe('compareMetrics', () => {
  const baseline = {
    schema: 1,
    commit: 'abc123',
    decks: { 'The fall': { wallAvgMs: 1000 }, Topography: { wallAvgMs: 500 } },
    longTasks: { rolls: { totalMs: 200 } },
    docketScroll: { frameP95Ms: 10 },
  }

  it('passes and marks everything ok/improved when nothing regresses', () => {
    const current = structuredClone(baseline)
    current.commit = 'def456' // metadata changes are never compared
    current.decks['The fall'].wallAvgMs = 950 // improved
    const { rows, failed } = compareMetrics(baseline, current)
    expect(failed).toBe(false)
    expect(rows.find((r) => r.name === 'decks.The fall.wallAvgMs').verdict).toBe('improved')
    expect(rows.find((r) => r.name === 'decks.Topography.wallAvgMs').verdict).toBe('ok')
    expect(rows.some((r) => r.name === 'schema' || r.name === 'commit')).toBe(false)
  })

  it('fails when one metric regresses past both thresholds', () => {
    const current = structuredClone(baseline)
    current.decks['The fall'].wallAvgMs = 1500 // +50%, +500ms
    const { rows, failed } = compareMetrics(baseline, current)
    expect(failed).toBe(true)
    expect(rows.find((r) => r.name === 'decks.The fall.wallAvgMs').verdict).toBe('regressed')
  })

  it('applies the tighter frame-p95 thresholds', () => {
    const current = structuredClone(baseline)
    current.docketScroll.frameP95Ms = 20 // +100%, +10ms -- fails p95's 8ms floor
    const { failed } = compareMetrics(baseline, current)
    expect(failed).toBe(true)
  })

  it('reports a metric missing from current as missing, not a failure', () => {
    const current = structuredClone(baseline)
    delete current.longTasks.rolls
    const { rows, failed } = compareMetrics(baseline, current)
    expect(failed).toBe(false)
    expect(rows.find((r) => r.name === 'longTasks.rolls.totalMs').verdict).toBe('missing')
  })

  it('reports a metric present only in current as new, not a failure', () => {
    const current = structuredClone(baseline)
    current.longTasks.idle = { totalMs: 42 }
    const { rows, failed } = compareMetrics(baseline, current)
    expect(failed).toBe(false)
    expect(rows.find((r) => r.name === 'longTasks.idle.totalMs').verdict).toBe('new')
  })
})
