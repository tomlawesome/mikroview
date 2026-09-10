// SPDX-License-Identifier: AGPL-3.0-only
//
// The event buffer's slider (#796, round 39's `#set` memory group).
//
// The arithmetic and the sentences live here rather than in
// EngineRoom.svelte so both places that draw the control -- Settings and
// the setup wizard -- read one implementation, and so the wording can be
// asserted without standing up a browser.
//
// Every figure crossing this module's boundary is in bytes, the unit
// internal/api.StoreSettings uses, so nothing has to remember which side
// of a call it is on.

const MIB = 1024 * 1024
const GIB = 1024 * MIB

// The track's own coordinates, straight off the drawing
// (docs/design/concepts/round-39/the-whole.html, `.stmemctl`): a
// 520-unit viewBox with the rail running from x=8 to x=508.
export const TRACK_X0 = 8
export const TRACK_X1 = 508
const TRACK_W = TRACK_X1 - TRACK_X0

/**
 * fractionFor places a byte figure along the track, 0 at the minimum and
 * 1 at the maximum.
 *
 * A doubling scale, not a linear one, because the range is three orders
 * of magnitude wide: linearly, the 120 MiB default on a 3.5 GiB host
 * sits 2.5% along -- jammed against the left stop, where the useful part
 * of the range has no room to be dragged in. Round 39 draws it a quarter
 * of the way along instead, which is what a log scale gives.
 */
export function fractionFor(bytes: number, min: number, max: number): number {
  if (!(max > min) || !(bytes > 0)) return 0
  const span = Math.log2(max / min)
  if (span <= 0) return 0
  return clamp01(Math.log2(bytes / min) / span)
}

/** trackX is fractionFor in the drawing's own coordinates. */
export function trackX(bytes: number, min: number, max: number): number {
  return TRACK_X0 + TRACK_W * fractionFor(bytes, min, max)
}

/**
 * bytesAt is the inverse: what a position along the track proposes.
 *
 * The result is snapped, because a slider that lands on 119.7 MiB
 * reports a figure nobody chose and no two drags can reproduce. Steps
 * are 8 MiB below a gigabyte and 64 MiB above it -- fine enough that
 * every figure round 39 draws (32, 64, 120, 480) is reachable, coarse
 * enough that the number under the handle stops flickering while the
 * mouse moves.
 */
export function bytesAt(fraction: number, min: number, max: number): number {
  if (!(max > min)) return min
  const raw = min * Math.pow(2, clamp01(fraction) * Math.log2(max / min))
  return clampBytes(snap(raw), min, max)
}

/** bytesAtX is bytesAt taking the drawing's own x coordinate. */
export function bytesAtX(x: number, min: number, max: number): number {
  return bytesAt((x - TRACK_X0) / TRACK_W, min, max)
}

function snap(bytes: number): number {
  const step = stepFor(bytes)
  return Math.round(bytes / step) * step
}

function clampBytes(bytes: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, bytes))
}

function clamp01(v: number): number {
  return Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : 0
}

/**
 * stepBytes is one arrow key: one snap step, so every figure a drag can
 * land on is also reachable from the keyboard, and the two never
 * disagree about what is settable.
 *
 * Deliberately not a fraction of a doubling, which was the first
 * attempt: a geometric step lands on whatever the snap rounds it to and
 * skips figures in between -- from 120 MiB it steps straight over
 * 480 MiB to 504. A control whose keyboard cannot reach a figure its
 * mouse can is a control with two different sets of legal values.
 * Crossing the whole range is what pageStepBytes below is for.
 */
export function stepBytes(bytes: number, direction: 1 | -1, min: number, max: number): number {
  return clampBytes(snap(bytes) + direction * stepFor(bytes), min, max)
}

/**
 * pageStepBytes is one Page Up or Page Down: a doubling, so the whole
 * range is a handful of presses rather than a few hundred. The scale is
 * logarithmic, so a doubling is the natural big step -- it moves the
 * handle the same distance along the track wherever it starts.
 */
export function pageStepBytes(bytes: number, direction: 1 | -1, min: number, max: number): number {
  return clampBytes(snap(direction > 0 ? bytes * 2 : bytes / 2), min, max)
}

function stepFor(bytes: number): number {
  return bytes >= GIB ? 64 * MIB : 8 * MIB
}

/**
 * doublingTicks are the marks under the rail: every doubling of the
 * minimum that falls strictly inside the range. Round 39 draws six of
 * them for a 32 MiB - 3.5 GiB range (64, 128, 256, 512, 1024, 2048).
 */
export function doublingTicks(min: number, max: number): number[] {
  const out: number[] = []
  for (let v = min * 2; v < max && out.length < 24; v *= 2) out.push(v)
  return out
}

/**
 * midLabel is the one figure printed between the two ends of the track:
 * the first doubling mark at or past the halfway point.
 *
 * A mark rather than the arithmetic midpoint, so the label sits on
 * something drawn rather than floating between two things. "At or past"
 * rather than "nearest" because that is what reproduces round 39's own
 * choice -- 512 MiB on a 32 MiB - 3.5 GiB track, where the nearest mark
 * to the centre would have been 256.
 */
export function midLabel(min: number, max: number): number | null {
  const ticks = doublingTicks(min, max)
  if (ticks.length === 0) return null
  for (const t of ticks) {
    if (fractionFor(t, min, max) >= 0.5) return t
  }
  return ticks[ticks.length - 1]
}

/**
 * formatSize renders a byte figure the way round 39 writes it: whole
 * MiB below a gigabyte ("120 MiB", "480 MiB"), one decimal of GiB above
 * ("3.5 GiB"). Below a mebibyte, whole KiB ("2 KiB"): the disk group's
 * "on disk" row reads a first day of a few kilobytes, and "1 day · 0 MiB"
 * would say the day is empty when it is not (#910).
 */
export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 MiB'
  if (bytes < MIB) return `${Math.max(1, Math.round(bytes / 1024))} KiB`
  if (bytes >= GIB) {
    const gib = bytes / GIB
    // A whole number of GiB prints without the ".0": 2 GiB, not 2.0 GiB.
    return `${gib % 1 === 0 ? gib : gib.toFixed(1)} GiB`
  }
  return `${Math.round(bytes / MIB)} MiB`
}

/**
 * formatEvents renders an event count as round 39 does -- "201 000",
 * three significant figures with space-separated thousands.
 *
 * Coarsened rather than exact because the count is itself an estimate:
 * it is the budget divided by a measured typical event cost, and
 * printing 201,649 of them claims a precision the constant behind it
 * does not have. The separator is a normal space, per the drawing.
 *
 * Cut down rather than rounded, which is both what round 39 prints
 * (201 000 from 201,649; 806 000 from 806,597; 107 000 from 107,546)
 * and the honest direction for a capacity claim: an estimate that
 * rounded up would promise room the buffer does not have.
 */
export function formatEvents(count: number): string {
  if (!Number.isFinite(count) || count <= 0) return '0'
  const coarse = threeSignificantFigures(count)
  return String(coarse).replace(/\B(?=(\d{3})+(?!\d))/g, ' ')
}

function threeSignificantFigures(n: number): number {
  if (n < 1000) return Math.floor(n)
  const magnitude = Math.pow(10, Math.floor(Math.log10(n)) - 2)
  return Math.floor(n / magnitude) * magnitude
}

/**
 * formatHours renders a span the way round 39 does: "9 h", "36 h",
 * "4.8 h" -- one decimal below ten hours, none above, because the
 * difference between 36 and 36.4 hours is not one anyone acts on while
 * the difference between 4.8 and 5 is.
 *
 * Below an hour it changes unit rather than printing a fraction. Round
 * 39 never draws a span that short, so this is not a departure from it;
 * it is what stops a 32 MiB buffer on a busy instance reading "holds
 * ~0 h at today's rate", which is not a rounding of half an hour but a
 * different claim -- and the wrong one to put beside a control whose
 * whole job is showing what a figure buys. Seen on the first live run.
 */
export function formatHours(hours: number): string {
  if (!Number.isFinite(hours) || hours <= 0) return '0 s'
  if (hours < 1 / 60) return `${Math.max(1, Math.round(hours * 3600))} s`
  if (hours < 1) return `${Math.round(hours * 60)} min`
  if (hours < 10) return `${trimZero(hours.toFixed(1))} h`
  return `${Math.round(hours)} h`
}

function trimZero(s: string): string {
  return s.endsWith('.0') ? s.slice(0, -2) : s
}

/** capacityFor is the event count a budget buys, as the server derives it. */
export function capacityFor(bytes: number, bytesPerEvent: number): number {
  if (!(bytesPerEvent > 0)) return 0
  return Math.max(1, Math.floor(bytes / bytesPerEvent))
}

/**
 * reachHours is how long a capacity lasts at today's rate, or null when
 * there is no rate to reason from.
 *
 * Null rather than Infinity or a dash: an instance that has seen no
 * traffic cannot say how long its buffer will cover, and the sentences
 * below say that in words instead of printing a number nobody can act
 * on. "today's rate" is eventsPerSecond, the server's own rolling
 * average -- the same figure every other reach claim in the app uses.
 */
export function reachHours(capacity: number, eventsPerSecond: number): number | null {
  if (!(eventsPerSecond > 0) || !(capacity > 0)) return null
  return capacity / eventsPerSecond / 3600
}

/**
 * The row under the bar: "120 MiB · ~201 000 events · ~9 h at today's
 * rate", or, when `held` is given, "120 MiB · 8 412 of ~201 000 events ·
 * ~9 h at today's rate" (#842).
 *
 * The ceiling and the reach are reckonings -- the client's own arithmetic
 * on a configured budget and a measured rate. held is different: it is
 * what the server publishes right now as the ring's actual occupancy, the
 * one live number in the row, and it is what the live-check (#842) pins
 * to prove events are still arriving rather than the row having frozen.
 * It renders exact, not to three figures like the capacity beside it --
 * coarsening 8,412 to "8 410" would make a real count print a number
 * nobody could reconcile with the server's own figure.
 */
export function bufferRow(
  bytes: number,
  bytesPerEvent: number,
  eventsPerSecond: number,
  held?: number,
): string {
  const capacity = capacityFor(bytes, bytesPerEvent)
  const hours = reachHours(capacity, eventsPerSecond)
  const eventsPart =
    held === undefined
      ? `~${formatEvents(capacity)} events`
      : `${formatExactCount(held)} of ~${formatEvents(capacity)} events`
  const parts = [formatSize(bytes), eventsPart]
  parts.push(hours === null ? 'no rate to reckon from yet' : `~${formatHours(hours)} at today's rate`)
  return parts.join(' · ')
}

/**
 * formatExactCount groups a live count the same way formatEvents groups
 * its coarsened one -- space-separated thousands -- but without the
 * three-significant-figure rounding, so a small or exact figure like 3
 * or 8,412 prints as itself rather than as "0" or "8 410".
 */
function formatExactCount(count: number): string {
  if (!Number.isFinite(count) || count < 0) return '0'
  return String(Math.round(count)).replace(/\B(?=(\d{3})+(?!\d))/g, ' ')
}

/** What a proposal is: bigger, smaller, or the figure already in effect. */
export type ProposalKind = 'rest' | 'grow' | 'shrink'

export function proposalKind(proposed: number, current: number): ProposalKind {
  if (proposed > current) return 'grow'
  if (proposed < current) return 'shrink'
  return 'rest'
}

export interface Proposal {
  kind: ProposalKind
  /** The figure being proposed, in bytes. */
  proposed: number
  /** The consequence sentence, without the apply/keep links after it. */
  sentence: string
  /**
   * For a shrink that would evict: when the oldest surviving event was
   * received, as epoch milliseconds. Null when nothing would fall away
   * (or there is no rate to work it out from), which is what the bar
   * reads to decide whether to draw the cut at all.
   */
  newOldest: number | null
}

export interface ProposalInputs {
  proposed: number
  current: number
  bytesPerEvent: number
  eventsPerSecond: number
  /** How many events the ring holds right now. */
  count: number
  /** Now, as epoch milliseconds -- passed in so the sentence is testable. */
  now: number
}

/**
 * describeProposal is round 39's consequence sentence, in its own words.
 *
 * Growing:  "480 MiB would hold ~36 h at today's rate, filling over the
 *            next 27 h"
 * Shrinking: "64 MiB holds ~4.8 h at today's rate — everything before
 *            09:16 lets go"
 *
 * The two differ because the drawing says they should, and it is right
 * about why: a shrink has something to show on the hours bar (the part
 * that would go), and a grow has nothing to show yet, so the sentence
 * carries the whole consequence -- how long the extra reach takes to
 * fill. A grow that is instant would be a lie; the room is real
 * immediately, the history to put in it is not.
 */
export function describeProposal(input: ProposalInputs): Proposal {
  const { proposed, current, bytesPerEvent, eventsPerSecond, count, now } = input
  const kind = proposalKind(proposed, current)
  const capacity = capacityFor(proposed, bytesPerEvent)
  const hours = reachHours(capacity, eventsPerSecond)
  const size = formatSize(proposed)

  if (kind === 'rest') {
    return { kind, proposed, sentence: '', newOldest: null }
  }

  if (kind === 'grow') {
    const held = hours === null ? null : `would hold ~${formatHours(hours)} at today's rate`
    const room = capacity - count
    const fillHours = eventsPerSecond > 0 && room > 0 ? room / eventsPerSecond / 3600 : null
    if (held === null) {
      return {
        kind,
        proposed,
        sentence: `${size} reserves room for ~${formatEvents(capacity)} events; nothing has arrived yet to say how long that lasts`,
        newOldest: null,
      }
    }
    if (fillHours === null) {
      return { kind, proposed, sentence: `${size} ${held}`, newOldest: null }
    }
    return {
      kind,
      proposed,
      sentence: `${size} ${held}, filling over the next ${formatHours(fillHours)}`,
      newOldest: null,
    }
  }

  // Shrinking.
  const evicted = count - capacity
  if (hours === null) {
    return {
      kind,
      proposed,
      sentence: `${size} holds ~${formatEvents(capacity)} events; nothing has arrived yet to say how long that lasts`,
      newOldest: null,
    }
  }
  const holds = `${size} holds ~${formatHours(hours)} at today's rate`
  if (evicted <= 0) {
    // Honest about the common case: a buffer nowhere near full loses
    // nothing when the ceiling comes down. Saying "everything before X
    // lets go" here would invent a loss that is not happening.
    return { kind, proposed, sentence: `${holds} — nothing held falls away`, newOldest: null }
  }
  const newOldest = now - (capacity / eventsPerSecond) * 1000
  return {
    kind,
    proposed,
    sentence: `${holds} — everything before ${clockTime(newOldest)} lets go`,
    newOldest,
  }
}

/** hh:mm in the viewer's own timezone, matching the bar's other labels. */
export function clockTime(epochMs: number): string {
  const d = new Date(epochMs)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

/**
 * ceilingCaption is the track's right-hand end: "3.5 GiB — all this host
 * can spare", or, when the server could read neither a cgroup limit nor
 * the machine's RAM, a caption that says the ceiling is a default rather
 * than claiming to know what the host has. Inventing "all this host can
 * spare" over a figure nobody measured would be the app asserting
 * something it does not know.
 */
export function ceilingCaption(max: number, hostTotal: number): string {
  if (hostTotal > 0) return `${formatSize(max)} — all this host can spare`
  return `${formatSize(max)} — a safe default; this host's memory could not be read`
}
