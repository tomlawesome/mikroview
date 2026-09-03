// SPDX-License-Identifier: AGPL-3.0-only
//
// What a surface says while the process is not yet fully live (#795,
// design round 41, owner-ratified 2026-09-03).
//
// A restart used to be invisible: the hour simply started again, and
// nothing on screen distinguished "quiet" from "this process has only
// been counting for four minutes". Two surfaces now say so in one
// sentence -- the metrics hourline's last fact and a dim chip in the
// docket's clear-all row -- and both say it in absolute local times,
// which is the voice those surfaces already speak (`the brink · 14:02`).
//
// This module is the *only* place that sentence is built. Two surfaces
// deriving the same fact separately is this repo's recurring failure
// (#750 is the worked example: the hourline had drifted from
// Topography's own "null, not zero, before the fetch lands" rule), so
// the wording, the clearing rule and the clock all live here and both
// components read the answer rather than the inputs.

import { formatHM } from './format'
import type { Stats } from './types'

/**
 * How long after `liveSince` both surfaces keep saying it.
 *
 * An hour, because the hourline holds an hour: once every minute the
 * seismograph draws was observed by this process, the restart is no
 * longer something the operator has to know about to read the page.
 * Exactly one constant, read by both surfaces, because "both clear at
 * the same moment" is the ratified behaviour and two timers would drift.
 */
export const STATEMENT_WINDOW_MS = 60 * 60 * 1000

/**
 * startStatement is the sentence, or null when there is nothing to say.
 *
 *  - warm restart (`restoredTo` present): `restored to 13:14 · live since 13:18`
 *  - cold start (`restoredTo` absent):    `counting since 13:18 — nothing before`
 *  - `STATEMENT_WINDOW_MS` after `liveSince`, and from then on:  null
 *
 * `now` is a parameter rather than a call to `new Date()` inside, so the
 * clearing boundary is testable without faking time, and so both
 * surfaces can be handed the same instant.
 *
 * Null is also the answer for a server that sends no `liveSince` at all
 * (an older build, or a test fixture): the surface then says nothing,
 * which is what it did before this existed. It is deliberately not a
 * guess from some other timestamp -- a statement about provenance that
 * was inferred is worse than no statement.
 */
export function startStatement(stats: Stats | null | undefined, now: Date): string | null {
  if (!stats?.liveSince) return null

  const live = new Date(stats.liveSince)
  if (Number.isNaN(live.getTime())) return null
  if (now.getTime() - live.getTime() >= STATEMENT_WINDOW_MS) return null

  // Presence, not truthiness of a parsed value, is the warm/cold test --
  // but an unparseable stamp falls through to the cold wording rather
  // than printing a raw ISO string where a clock time belongs. formatHM
  // returns its input unchanged when it cannot parse it, which is right
  // for a minute label and wrong for this sentence.
  const restoredTo = stats.restoredTo
  if (restoredTo && !Number.isNaN(new Date(restoredTo).getTime())) {
    return `restored to ${formatHM(restoredTo)} · live since ${formatHM(stats.liveSince)}`
  }
  return `counting since ${formatHM(stats.liveSince)} — nothing before`
}
