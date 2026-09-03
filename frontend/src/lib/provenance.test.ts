// SPDX-License-Identifier: AGPL-3.0-only
//
// The restart statement (#795, design round 41). Both the metrics
// hourline and the docket's clear-all row read this one function, so the
// wording, the warm/cold branch and the clearing boundary are pinned
// once, here, rather than twice in two component tests.
//
// vitest.config.ts pins TZ=UTC before anything formats a date (#741), so
// the HH:MM strings below are the same ones CI reads.

import { describe, expect, it } from 'vitest'
import { STATEMENT_WINDOW_MS, startStatement } from './provenance'
import type { Stats } from './types'

// The round-41 data story: mikroview restarted at 13:18, and the newest
// snapshot had been taken at 13:14.
const LIVE_SINCE = '2026-09-03T13:18:00Z'
const RESTORED_TO = '2026-09-03T13:14:00Z'
const live = new Date(LIVE_SINCE)

function at(msAfterBoot: number): Date {
  return new Date(live.getTime() + msAfterBoot)
}

function stats(overrides: Partial<Stats> = {}): Stats {
  return {
    total: 0,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 0,
    capacity: 100000,
    count: 0,
    windowSeconds: 3600,
    oldestHeld: null,
    connectedClients: 1,
    ...overrides,
  }
}

describe('startStatement (#795)', () => {
  it('names both times after a warm restart', () => {
    const s = startStatement(stats({ liveSince: LIVE_SINCE, restoredTo: RESTORED_TO }), at(4 * 60 * 1000))
    expect(s).toBe('restored to 13:14 · live since 13:18')
  })

  it('says nothing came before after a cold start', () => {
    // A cold start omits restoredTo entirely -- the key's absence is the
    // answer, which is why the server sends no null (internal/store's
    // `restoredTo,omitempty`).
    const s = startStatement(stats({ liveSince: LIVE_SINCE }), at(4 * 60 * 1000))
    expect(s).toBe('counting since 13:18 — nothing before')
  })

  it('is still saying it just before the window closes', () => {
    const warm = stats({ liveSince: LIVE_SINCE, restoredTo: RESTORED_TO })
    expect(startStatement(warm, at(STATEMENT_WINDOW_MS - 1000))).toBe('restored to 13:14 · live since 13:18')
  })

  it('has cleared at exactly sixty minutes, not a moment later', () => {
    // The boundary is the whole behaviour: "clears 60 minutes after
    // liveSince" is ratified wording, so the sixtieth minute is the
    // first one with nothing to say, on both surfaces at once.
    const warm = stats({ liveSince: LIVE_SINCE, restoredTo: RESTORED_TO })
    const cold = stats({ liveSince: LIVE_SINCE })
    expect(startStatement(warm, at(STATEMENT_WINDOW_MS))).toBeNull()
    expect(startStatement(cold, at(STATEMENT_WINDOW_MS))).toBeNull()
  })

  it('has cleared well after the window', () => {
    expect(startStatement(stats({ liveSince: LIVE_SINCE, restoredTo: RESTORED_TO }), at(3 * 60 * 60 * 1000))).toBeNull()
  })

  it('says nothing when the server sends no liveSince', () => {
    // An older backend, or a fixture written before #795. The surface
    // goes back to saying nothing rather than guessing a boot time from
    // some other stamp -- an inferred claim about provenance is worse
    // than no claim.
    expect(startStatement(stats(), at(0))).toBeNull()
  })

  it('says nothing before the first fetch lands', () => {
    expect(startStatement(null, at(0))).toBeNull()
    expect(startStatement(undefined, at(0))).toBeNull()
  })

  it('says nothing rather than printing an unparseable liveSince', () => {
    expect(startStatement(stats({ liveSince: 'not a date' }), at(0))).toBeNull()
  })

  it('falls back to the cold wording when restoredTo is unparseable', () => {
    // formatHM returns its input unchanged when it cannot parse it,
    // which is right for a minute label and wrong here: "restored to
    // wat" is worse than the honest cold sentence.
    const s = startStatement(stats({ liveSince: LIVE_SINCE, restoredTo: 'wat' }), at(60 * 1000))
    expect(s).toBe('counting since 13:18 — nothing before')
  })
})
