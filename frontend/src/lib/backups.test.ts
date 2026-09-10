// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { cadencePhrase, isGone, newestGeneration, oldestArrival, receiptLine } from './backups'
import { formatDayMonth, formatDurationShort, formatHM } from './format'
import type { RouterBackupRouter } from './types'

// Round 44's data story (build.py): rb5009 pushes nightly at 03:00 and
// has its ten; hap-ax2 has four, the newest 30 Aug, and three pushes
// have not arrived.
function router(over: Partial<RouterBackupRouter> = {}): RouterBackupRouter {
  return {
    device: 'rb5009',
    generations: [],
    intervalKnown: false,
    missed: 0,
    ...over,
  }
}

function gen(id: string, backupArrivedAt?: string, rscArrivedAt?: string) {
  return { id, backupArrivedAt, rscArrivedAt }
}

describe('cadencePhrase', () => {
  it('says a daily push as "nightly at" its own clock time', () => {
    const at = '2026-08-30T03:00:00Z'
    expect(cadencePhrase(86400, at)).toBe(`nightly at ${formatHM(at)}`)
  })

  it('stays close-to-a-day tolerant, not exact-to-the-second', () => {
    const at = '2026-08-30T03:00:00Z'
    expect(cadencePhrase(86400 - 1800, at)).toBe(`nightly at ${formatHM(at)}`)
  })

  it('falls back to a plain duration once the interval is not close to a day', () => {
    const at = '2026-08-30T03:00:00Z'
    expect(cadencePhrase(3600 * 6, at)).toBe(`every ${formatDurationShort(3600 * 6)}`)
  })
})

describe('receiptLine', () => {
  it('reads "10 of 10 kept" once a router is at the cap', () => {
    const r = router({
      generations: Array.from({ length: 10 }, (_, i) => gen(`g${i}`, '2026-08-24T03:00:00Z', '2026-08-24T03:00:05Z')),
      intervalKnown: true,
      intervalSeconds: 86400,
      lastArrival: '2026-09-02T03:00:00Z',
    })
    const receipt = receiptLine(r, '2026-08-24T03:00:00Z')
    expect(receipt.amber).toBe(false)
    expect(receipt.text).toBe(`10 of 10 kept · nightly at ${formatHM('2026-09-02T03:00:00Z')} · the oldest ${formatDayMonth('2026-08-24T03:00:00Z')}`)
  })

  it('states just the count below the cap, with no interval yet on a single push', () => {
    const r = router({ generations: [gen('g0', '2026-08-27T03:00:00Z', '2026-08-27T03:00:05Z')] })
    expect(receiptLine(r, '2026-08-27T03:00:00Z')).toEqual({ text: '1 kept', amber: false })
  })

  it('goes amber once a push has been missed, keeping the interval but replacing the oldest date', () => {
    const r = router({
      device: 'hap-ax2',
      generations: Array.from({ length: 4 }, (_, i) => gen(`g${i}`, '2026-08-27T03:00:00Z', '2026-08-27T03:00:05Z')),
      intervalKnown: true,
      intervalSeconds: 86400,
      lastArrival: '2026-08-30T03:00:00Z',
      missed: 3,
    })
    const receipt = receiptLine(r, '2026-08-27T03:00:00Z')
    expect(receipt.amber).toBe(true)
    expect(receipt.text).toBe(
      `4 kept · nightly at ${formatHM('2026-08-30T03:00:00Z')} · none since ${formatDayMonth('2026-08-30T03:00:00Z')} — 3 missed`,
    )
  })

  it('says "1 missed" in the singular', () => {
    const r = router({ intervalKnown: true, intervalSeconds: 86400, lastArrival: '2026-08-30T03:00:00Z', missed: 1 })
    expect(receiptLine(r, null).text).toContain('1 missed')
    expect(receiptLine(r, null).text).not.toContain('1 misseds')
  })
})

describe('isGone', () => {
  it('is offered only once a push has actually been missed', () => {
    expect(isGone(router({ missed: 0 }))).toBe(false)
    expect(isGone(router({ missed: 1 }))).toBe(true)
  })
})

describe('oldestArrival / newestGeneration', () => {
  it('reads the earliest and latest of a router generations list, oldest first', () => {
    const r = router({
      generations: [
        gen('g0', '2026-08-24T03:00:00Z', '2026-08-24T03:00:05Z'),
        gen('g1', '2026-08-25T03:00:00Z', '2026-08-25T03:00:05Z'),
      ],
    })
    expect(oldestArrival(r)).toBe('2026-08-24T03:00:00Z')
    expect(newestGeneration(r)?.id).toBe('g1')
  })

  it('is null/null with nothing kept', () => {
    const r = router()
    expect(oldestArrival(r)).toBeNull()
    expect(newestGeneration(r)).toBeNull()
  })
})
