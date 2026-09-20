// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  cadencePhrase,
  isGone,
  newestGeneration,
  oldestArrival,
  previousGeneration,
  readableGenerations,
  receiptLine,
  vaultGated,
} from './backups'
import type { VaultLock } from './types'
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

describe('vaultGated', () => {
  const lock = (over: Partial<VaultLock> = {}): VaultLock =>
    ({ passphraseSet: false, locked: false, unlockedForYou: false, ...over }) as VaultLock

  it('gates when a passphrase is set and this session does not hold the unlock', () => {
    expect(vaultGated(lock({ passphraseSet: true, locked: true }))).toBe(true)
    // Another of the admin's own sign-ins holds it: still not this one.
    expect(vaultGated(lock({ passphraseSet: true, locked: false }))).toBe(true)
  })

  it('does not gate when this session holds the unlock, or when there is no passphrase', () => {
    expect(vaultGated(lock({ passphraseSet: true, unlockedForYou: true }))).toBe(false)
    expect(vaultGated(lock())).toBe(false)
  })

  // The wizard reads the lock off a response it may not have yet. Not
  // loaded must not read as gated, or the tab is sent to whatever the
  // server answers a locked vault with.
  it('does not gate a lock that is not there yet', () => {
    expect(vaultGated(null)).toBe(false)
    expect(vaultGated(undefined)).toBe(false)
  })
})

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

describe('previousGeneration (#895)', () => {
  // Keeping a backup moves it into `protected` without moving it in the
  // router's own history, so "the one before" has to be read across
  // both lists in arrival order -- not off whichever list the row was
  // drawn from.
  const r = router({
    generations: [
      gen('g1', '2026-08-25T03:00:00Z', '2026-08-25T03:00:05Z'),
      gen('g2', '2026-08-26T03:00:00Z', '2026-08-26T03:00:05Z'),
    ],
    protected: [gen('kept0', '2026-08-24T03:00:00Z', '2026-08-24T03:00:05Z')],
  })

  it('reads the two lists as one history, oldest first', () => {
    expect(readableGenerations(r).map((g) => g.id)).toEqual(['kept0', 'g1', 'g2'])
  })

  it('finds the generation before, across the kept pool', () => {
    expect(previousGeneration(r, 'g2')?.id).toBe('g1')
    expect(previousGeneration(r, 'g1')?.id).toBe('kept0')
  })

  it('is null for the oldest one held, and for one it has never heard of', () => {
    expect(previousGeneration(r, 'kept0')).toBeNull()
    expect(previousGeneration(r, 'nope')).toBeNull()
  })

  it('skips a generation whose export never arrived -- there is nothing to compare', () => {
    const half = router({
      generations: [gen('g0', '2026-08-24T03:00:00Z'), gen('g1', '2026-08-25T03:00:00Z', '2026-08-25T03:00:05Z')],
    })
    expect(readableGenerations(half).map((g) => g.id)).toEqual(['g1'])
    expect(previousGeneration(half, 'g1')).toBeNull()
  })
})
