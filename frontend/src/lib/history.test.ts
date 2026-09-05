// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  DAY_TICKS,
  addDays,
  barLabel,
  bytesOfMib,
  capDays,
  capMark,
  dayLabel,
  dayX,
  daysAtX,
  heldRow,
  memoryHint,
  mibOf,
  pageStepDays,
  proposeCap,
  proposeDays,
  proposeOff,
  restartRow,
  stateRow,
  stepDays,
} from './history'
import type { HistorySettings } from './types'

const MIB = 1024 * 1024
const GIB = 1024 * MIB
const en = { locale: 'en-GB' }

// Round 42's data story: today is 2 Sep, history was turned on 7 Aug,
// 27 days on disk at 30 MiB a day, 30 days allowed under 1 GiB. The
// drawing rounds its figures loosely ("about 30 MiB a day"); these
// assert the exact rate that reproduces its cuts and dates.
function story(overrides: Partial<HistorySettings> = {}): HistorySettings {
  return {
    keyed: true,
    enabled: true,
    days: 30,
    maxBytes: GIB,
    held: { days: 27, oldest: '2026-08-07', newest: '2026-09-02', bytes: 812 * MIB },
    capped: false,
    bytesPerDay: 30 * MIB,
    ...overrides,
  }
}

describe('round 42 draws these exact positions', () => {
  it('places the handle where build.py places it', () => {
    // tx(30) = 296, tx(14) = 232, tx(90) = 389, tx(34) = 307, tx(25) = 281, tx(17) = 248
    expect(Math.round(dayX(30))).toBe(296)
    expect(Math.round(dayX(14))).toBe(232)
    expect(Math.round(dayX(90))).toBe(389)
    expect(Math.round(dayX(34))).toBe(307)
    expect(Math.round(dayX(25))).toBe(281)
    expect(Math.round(dayX(17))).toBe(248)
    expect(dayX(1)).toBe(8)
    expect(dayX(365)).toBeCloseTo(508, 9)
  })

  it('marks the same doublings', () => {
    expect(DAY_TICKS).toEqual([2, 4, 8, 16, 32, 64, 128, 256])
    expect(Math.round(dayX(2))).toBe(67)
    expect(Math.round(dayX(256))).toBe(478)
  })

  it('reads a position back as a whole day, and the ends as 1 and 365', () => {
    expect(daysAtX(296)).toBe(30)
    expect(daysAtX(8)).toBe(1)
    expect(daysAtX(508)).toBe(365)
    expect(daysAtX(-40)).toBe(1)
    expect(daysAtX(900)).toBe(365)
  })

  it('steps a day at a time by arrow and doubles by page, within 1-365', () => {
    expect(stepDays(30, 1)).toBe(31)
    expect(stepDays(1, -1)).toBe(1)
    expect(stepDays(365, 1)).toBe(365)
    expect(pageStepDays(30, 1)).toBe(60)
    expect(pageStepDays(30, -1)).toBe(15)
    expect(pageStepDays(200, 1)).toBe(365)
  })
})

describe('the cap mark', () => {
  it('is where the cap runs out at today’s rate, cut down not rounded', () => {
    expect(capDays(GIB, 30 * MIB)).toBe(34)
    expect(capDays(768 * MIB, 30 * MIB)).toBe(25)
    expect(capDays(512 * MIB, 30 * MIB)).toBe(17)
    expect(capMark(GIB, 30 * MIB)).toEqual({ days: 34, label: "~34 d — where 1 GiB runs out at today's rate" })
    expect(capMark(768 * MIB, 30 * MIB)?.label).toBe("~25 d — where 768 MiB runs out at today's rate")
  })

  it('is left off without a rate, or when it would fall past the track', () => {
    expect(capDays(GIB, 0)).toBeNull()
    expect(capMark(GIB, 0)).toBeNull()
    expect(capMark(100 * GIB, 30 * MIB)).toBeNull()
  })
})

describe('days and sizes', () => {
  it('writes a day the way the drawing writes it', () => {
    expect(dayLabel('2026-08-07', 'en-GB')).toBe('7 Aug')
    // en-GB spells September "Sept", as the rest of the app's day labels do.
    expect(dayLabel('2026-09-02', 'en-GB')).toBe('2 Sept')
    expect(dayLabel('2026-09-02', 'en-US')).toBe('Sep 2')
    expect(dayLabel('not a day')).toBe('not a day')
  })

  it('moves a day by whole days across a month end', () => {
    expect(addDays('2026-09-02', -13)).toBe('2026-08-20')
    expect(addDays('2026-09-02', -16)).toBe('2026-08-17')
    expect(addDays('2026-01-01', -1)).toBe('2025-12-31')
  })

  it('edits the cap in whole MiB', () => {
    expect(mibOf(GIB)).toBe(1024)
    expect(bytesOfMib('512')).toBe(512 * MIB)
    expect(bytesOfMib(' 768 ')).toBe(768 * MIB)
    expect(bytesOfMib('0')).toBeNull()
    expect(bytesOfMib('1.5')).toBeNull()
    expect(bytesOfMib('a lot')).toBeNull()
    expect(bytesOfMib('')).toBeNull()
  })
})

describe('the on-disk row', () => {
  it('says what is held, and that it is still filling', () => {
    expect(heldRow(story(), 'en-GB')).toBe('27 days · since 7 Aug · 812 MiB — filling')
  })

  it('says full when the cap is what decides', () => {
    const s = story({
      maxBytes: 768 * MIB,
      capped: true,
      held: { days: 25, oldest: '2026-08-09', newest: '2026-09-02', bytes: 768 * MIB },
    })
    expect(heldRow(s, 'en-GB')).toBe('25 days · since 9 Aug · 768 MiB — full')
    expect(barLabel(s, 'en-GB')).toBe('9 Aug — the oldest the 768 MiB cap keeps')
  })

  it('says neither when the days allowed are all on disk', () => {
    const s = story({ days: 27 })
    expect(heldRow(s, 'en-GB')).toBe('27 days · since 7 Aug · 812 MiB')
    expect(barLabel(s, 'en-GB')).toBe('7 Aug — the oldest day on disk')
  })

  it('has nothing to say when nothing is held', () => {
    expect(heldRow(story({ held: null }))).toBeNull()
    expect(barLabel(story({ held: null }))).toBeNull()
  })
})

describe('fewer days', () => {
  it('names the days that would go and the new oldest day, in the drawing’s words', () => {
    const p = proposeDays(story(), 14, en)!
    expect(p.kind).toBe('dshrink')
    expect(p.sentence).toBe("14 days holds ~420 MiB at today's rate — the 13 days before 20 Aug let go")
    expect(p.applyLabel).toBe('delete 13 days')
    expect(p.keepLabel).toBe('keep all 27')
    expect(p.cut).toBe(13)
    expect(p.newOldest).toBe('2026-08-20')
    expect(p.cutLabel).toBe('20 Aug — the oldest that 14 days would keep')
    expect({ enabled: p.enabled, days: p.days, maxBytes: p.maxBytes }).toEqual({ enabled: true, days: 14, maxBytes: GIB })
  })

  it('is a plain apply when fewer days would still keep everything on disk', () => {
    const p = proposeDays(story(), 28, en)!
    expect(p.sentence).toBe("28 days holds ~840 MiB at today's rate — nothing on disk lets go")
    expect(p.applyLabel).toBe('apply')
    expect(p.keepLabel).toBe('keep 30 days')
    expect(p.cut).toBeNull()
  })

  it('leaves the rate out when there is none', () => {
    const p = proposeDays(story({ bytesPerDay: 0 }), 14, en)!
    expect(p.sentence).toBe('14 days — the 13 days before 20 Aug let go')
    expect(p.applyLabel).toBe('delete 13 days')
  })

  it('speaks in the singular for one day', () => {
    const p = proposeDays(story(), 26, en)!
    expect(p.sentence).toBe("26 days holds ~780 MiB at today's rate — the day before 8 Aug lets go")
    expect(p.applyLabel).toBe('delete 1 day')
  })

  it('is no proposal at the figure in effect', () => {
    expect(proposeDays(story(), 30)).toBeNull()
  })
})

describe('more days', () => {
  it('says what they would need and how many the cap would hold', () => {
    const p = proposeDays(story(), 90, en)!
    expect(p.kind).toBe('dgrow')
    expect(p.sentence).toBe("90 days would need ~2.6 GiB at today's rate; the 1 GiB cap would hold ~34 of them")
    expect(p.applyLabel).toBe('apply')
    expect(p.keepLabel).toBe('keep 30 days')
    expect(p.cut).toBeNull()
  })

  it('says they fit when the cap would hold them all', () => {
    const p = proposeDays(story({ maxBytes: 4 * GIB }), 60, en)!
    expect(p.sentence).toBe("60 days would need ~1.8 GiB at today's rate, within the 4 GiB cap")
  })

  it('says there is no rate rather than inventing one', () => {
    const p = proposeDays(story({ bytesPerDay: 0 }), 90, en)!
    expect(p.sentence).toBe('90 days, under the 1 GiB cap; no rate yet to say how much of it that needs')
  })
})

describe('the cap', () => {
  it('estimates the bite at today’s rate, in the drawing’s words', () => {
    const p = proposeCap(story(), 512 * MIB, en)!
    expect(p.kind).toBe('dcap')
    expect(p.sentence).toBe("512 MiB holds ~17 days at today's rate — the 10 days before 17 Aug let go")
    expect(p.applyLabel).toBe('delete 10 days')
    expect(p.keepLabel).toBe('keep 1 GiB')
    expect(p.cut).toBe(10)
    expect(p.newOldest).toBe('2026-08-17')
    expect(p.cutLabel).toBe('17 Aug — the oldest that 512 MiB would keep')
    expect({ enabled: p.enabled, days: p.days, maxBytes: p.maxBytes }).toEqual({ enabled: true, days: 30, maxBytes: 512 * MIB })
  })

  it('is a plain apply when the new cap holds everything on disk', () => {
    const p = proposeCap(story(), 2 * GIB, en)!
    expect(p.sentence).toBe("2 GiB holds ~30 days at today's rate — nothing on disk lets go")
    expect(p.applyLabel).toBe('apply')
    expect(p.keepLabel).toBe('keep 1 GiB')
    expect(p.cut).toBeNull()
  })

  it('deletes at least a day when the cap is below what is held, however the rate rounds', () => {
    // 800 MiB against 812 MiB held: the rate says 26 days fit, but the
    // figure on disk says something has to go.
    const p = proposeCap(story(), 800 * MIB, en)!
    expect(p.applyLabel).toBe('delete 1 day')
    expect(p.cut).toBe(1)
  })

  it('says the cap is below what is held when there is no rate to count days with', () => {
    const p = proposeCap(story({ bytesPerDay: 0 }), 512 * MIB, en)!
    expect(p.sentence).toBe('512 MiB is less than the 812 MiB on disk — the oldest days let go until it fits')
    expect(p.applyLabel).toBe('apply')
    expect(p.cut).toBeNull()
  })

  it('is no proposal at the figure in effect, or for nonsense', () => {
    expect(proposeCap(story(), GIB)).toBeNull()
    expect(proposeCap(story(), 0)).toBeNull()
  })
})

describe('turning off', () => {
  it('names every day on disk and the link deletes them', () => {
    const p = proposeOff(story(), en)!
    expect(p.kind).toBe('doff')
    expect(p.sentence).toBe('off deletes all 27 days on disk, back to 7 Aug, and keeps nothing after')
    expect(p.applyLabel).toBe('delete 27 days')
    expect(p.keepLabel).toBe('keep them')
    expect(p.cut).toBe(27)
    expect(p.newOldest).toBeNull()
    expect(p.cutLabel).toBe('all 27 days would let go')
    expect({ enabled: p.enabled, days: p.days, maxBytes: p.maxBytes }).toEqual({ enabled: false, days: 30, maxBytes: GIB })
  })

  it('is not a proposal when nothing is on disk to delete', () => {
    expect(proposeOff(story({ held: null }))).toBeNull()
  })
})

describe('the stopped state’s line', () => {
  it('says how much memory holds at today’s rate', () => {
    expect(memoryHint(9)).toBe(
      "nothing on disk — events live in memory only, ~9 h of them at today's rate; on keeps those and every day after",
    )
  })

  it('leaves the span out when there is nothing in memory yet', () => {
    expect(memoryHint(null)).toBe('nothing on disk — events live in memory only; on keeps those and every day after')
  })
})

describe('the memory group’s on-restart row (round 43, #921)', () => {
  it('with history on, names the days that stay and the one thing that reads them', () => {
    expect(restartRow(story())).toBe('the buffer clears — the 27 days on disk stay; trying a watcher reads them')
    expect(restartRow(story({ held: { days: 1, oldest: '2026-09-02', newest: '2026-09-02', bytes: 3 * MIB } }))).toBe(
      'the buffer clears — the 1 day on disk stays; trying a watcher reads it',
    )
  })

  it('just turned on with nothing filed yet, claims no count', () => {
    expect(restartRow(story({ held: null }))).toBe('the buffer clears — what is on disk stays; trying a watcher reads it')
  })

  it('off with a key points one storey down; no key says why nothing outlives it elsewhere', () => {
    expect(restartRow(story({ enabled: false, held: null }))).toBe(
      'the buffer clears — nothing outlives it; days can be kept on disk below',
    )
    expect(restartRow(story({ keyed: false, enabled: false, held: null }))).toBe('the buffer clears — nothing outlives it')
  })

  it('claims only that the buffer clears when the disk state is unknown', () => {
    expect(restartRow(null)).toBe('the buffer clears')
  })
})

describe('the disk group’s state row', () => {
  it('names the backend and what it keeps, or nothing for a caller the GET refuses', () => {
    expect(stateRow({ backend: 'file', dir: '/var/lib/mikroview' })).toBe(
      'encrypted file store · /var/lib/mikroview — flags, definitions, watchlist, entities, tokens',
    )
    expect(stateRow({ backend: 'postgres' })).toBe('Postgres — flags, definitions, watchlist, entities, tokens')
    expect(stateRow(null)).toBeNull()
  })

  it('says memory-only when #853 has no key to persist any of it under, except the hashed stores', () => {
    expect(stateRow({ backend: 'memory' })).toBe(
      'memory only — no key configured, so flags, definitions, watchlist and entities do not survive a restart (accounts and tokens still do, as one-way hashes)',
    )
  })
})
