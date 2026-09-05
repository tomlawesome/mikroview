// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "router backups" group (#394, round 44,
// docs/design/concepts/round-44/backups.html). The arithmetic behind a
// router's receipt line -- how many pushes it has missed, and the
// interval it is judged against -- is the server's
// (backupvault.Vault.Missed, GET /api/router-backups): this file only
// turns those numbers, and the generation list beside them, into the
// sentence the drawing writes.

import type { RouterBackupRouter } from './types'
import { formatDayMonth, formatDurationShort, formatHM } from './format'

/** MAX_GENERATIONS mirrors backupvault.MaxGenerations -- ten kept per
 * router, oldest dropped first (owner decision, #394). Not imported
 * (the frontend has no access to the Go constant); round 44's own
 * receipt line ("10 of 10 kept") is what pins the two together. */
export const MAX_GENERATIONS = 10

/**
 * cadencePhrase names how often a router pushes, from the interval the
 * server learned from its own arrivals -- never the scheduler line the
 * wizard printed, which an admin can change on the router without
 * mikroview knowing (owner, 2026-09-05, issue note 10572). A gap close
 * to a day is said the way round 44 writes it, "nightly at 03:00", read
 * off the last arrival's own clock time since that is when the push
 * actually lands; anything else falls back to a plain duration.
 */
export function cadencePhrase(intervalSeconds: number, lastArrivalIso: string): string {
  const oneDay = 86400
  if (Math.abs(intervalSeconds - oneDay) < 3600) {
    return `nightly at ${formatHM(lastArrivalIso)}`
  }
  return `every ${formatDurationShort(intervalSeconds)}`
}

/** A receipt line, and whether round 44's amber ink applies to it. */
export interface Receipt {
  text: string
  amber: boolean
}

/**
 * receiptLine is round 44's per-router receipt. At rest:
 *
 *   "10 of 10 kept · nightly at 03:00 · the oldest 24 Aug"
 *
 * Once a push has been missed, the interval clause stays -- it is still
 * what mikroview expects -- and what would have been "the oldest ..."
 * is replaced by how long the silence has run:
 *
 *   "4 kept · nightly at 03:00 · none since 30 Aug — 3 missed"
 *
 * A router with a single push has no interval yet (the server's own
 * "one push carries neither" rule, #394's build note) and so states
 * only what is kept.
 */
export function receiptLine(router: RouterBackupRouter, oldestArrivalIso: string | null): Receipt {
  const kept = router.generations.length
  const keptPhrase = kept >= MAX_GENERATIONS ? `${kept} of ${MAX_GENERATIONS} kept` : `${kept} kept`
  const cadence =
    router.intervalKnown && router.intervalSeconds !== undefined && router.lastArrival
      ? cadencePhrase(router.intervalSeconds, router.lastArrival)
      : null

  if (router.missed > 0) {
    const since = router.lastArrival ? formatDayMonth(router.lastArrival) : '—'
    const missedWord = router.missed === 1 ? '1 missed' : `${router.missed} missed`
    const tail = `none since ${since} — ${missedWord}`
    return { text: cadence ? `${keptPhrase} · ${cadence} · ${tail}` : `${keptPhrase} · ${tail}`, amber: true }
  }

  if (cadence && oldestArrivalIso) {
    return { text: `${keptPhrase} · ${cadence} · the oldest ${formatDayMonth(oldestArrivalIso)}`, amber: false }
  }
  if (cadence) return { text: `${keptPhrase} · ${cadence}`, amber: false }
  return { text: keptPhrase, amber: false }
}

/** isGone is round 44's "is it gone?" link: offered only once a push has
 * actually been missed -- a router that has never missed one has
 * nothing to ask about. */
export function isGone(router: RouterBackupRouter): boolean {
  return router.missed > 0
}

/** oldestArrival is the earliest of a router's kept generations' own
 * arrival times -- whichever of its .backup/.rsc landed first, since a
 * generation the .rsc alone opened still has an arrival worth dating.
 * Null when nothing has arrived at all. */
export function oldestArrival(router: RouterBackupRouter): string | null {
  let oldest: string | null = null
  for (const g of router.generations) {
    for (const at of [g.backupArrivedAt, g.rscArrivedAt]) {
      if (!at) continue
      if (oldest === null || at < oldest) oldest = at
    }
  }
  return oldest
}

/** newestGeneration is a router's most recently arrived pair -- round
 * 44's "newest pair" line reads from this one. Generations arrive
 * oldest first (the server's own ordering), so the newest is the last
 * entry. */
export function newestGeneration(router: RouterBackupRouter) {
  return router.generations.length > 0 ? router.generations[router.generations.length - 1] : null
}
