// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import type { WatchlistCoverage, WatchlistEntry } from './types'

// watchlistState only ever reaches the network through the functions
// api.ts exports (see watchlist.svelte.ts) -- brokenCount is pure logic
// over whatever's already sitting in .entries/.coverage, so no network
// mocking is needed here, mirroring flags.svelte.test.ts's approach for
// FlagsState.groupedBySource.
import { watchlistState } from './watchlist.svelte'

let nextId = 1

function entry(overrides: Partial<WatchlistEntry> = {}): WatchlistEntry {
  const id = `e${nextId++}`
  return {
    id,
    name: `watch ${id}`,
    enabled: true,
    createdAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// watchlistState is a module-level singleton (see watchlist.svelte.ts),
// so every test shares the same instance -- reset entries/coverage by
// hand between tests, mirroring flags.svelte.test.ts's approach for
// flagsState.list.
beforeEach(() => {
  watchlistState.entries = []
  watchlistState.coverage = {}
  nextId = 1
})

// #546's "Ratified: what puts something in a broken state" -- an enabled
// expectation whose coverage is exactly 'no-logging', and nothing else.
// The predicate itself is what this file tests; whether it agrees with a
// real server's coverage answer is frontend/scripts/live-nav-broken-ring.mjs's
// job.
describe('WatchlistState.brokenCount', () => {
  it('is zero with no entries', () => {
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('counts an enabled expectation whose coverage is no-logging', () => {
    const e = entry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    expect(watchlistState.brokenCount).toBe(1)
  })

  it('never rings on unknown -- mikroview has no answer, so it must not assert a problem', () => {
    const e = entry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'unknown' }
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('never rings on out-of-scope -- a scoping fact, not a failure', () => {
    const e = entry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'out-of-scope' }
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('never rings on covered', () => {
    const e = entry()
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'covered' }
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('excludes a disabled entry even if its coverage is no-logging -- switching a watch off is not a promise mikroview can see it', () => {
    const e = entry({ enabled: false })
    watchlistState.entries = [e]
    watchlistState.coverage = { [e.id]: 'no-logging' }
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('does not count an entry with no coverage answer at all', () => {
    const e = entry()
    watchlistState.entries = [e]
    watchlistState.coverage = {}
    expect(watchlistState.brokenCount).toBe(0)
  })

  it('counts every enabled no-logging entry, not just the first', () => {
    const states: WatchlistCoverage[] = ['no-logging', 'no-logging', 'covered', 'unknown']
    const entries = states.map(() => entry())
    watchlistState.entries = entries
    watchlistState.coverage = Object.fromEntries(entries.map((e, i) => [e.id, states[i]]))
    expect(watchlistState.brokenCount).toBe(2)
  })

  it('mixes enabled and disabled correctly across several entries', () => {
    const broken = entry({ enabled: true })
    const offButBroken = entry({ enabled: false })
    const enabledButFine = entry({ enabled: true })
    watchlistState.entries = [broken, offButBroken, enabledButFine]
    watchlistState.coverage = {
      [broken.id]: 'no-logging',
      [offButBroken.id]: 'no-logging',
      [enabledButFine.id]: 'covered',
    }
    expect(watchlistState.brokenCount).toBe(1)
  })
})
