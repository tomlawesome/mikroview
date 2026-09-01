// SPDX-License-Identifier: AGPL-3.0-only
//
// The drawer's episode shape (#678, round 29's third ratified item):
// "first 13:46 * last 13:52 * still arriving", "13:41:02 -> 13:41:42 *
// stopped", "13:34 * 13:36 * 13:39 * quiet since" -- derived purely from
// a flag's own timestamps, never from its type. The shape is the point:
// the same nine identical drops read as "still arriving" while they
// keep landing and "stopped" the moment they don't.
import { formatHM, formatTime } from './format'
import type { Flag, FirewallEvent } from './types'

// The line between "still arriving" and "stopped"/"quiet since": how
// long ago the last event landed. Ten minutes, not some fraction of the
// episode's own span -- a nine-event pattern spanning half an hour and a
// two-event burst thirty seconds long should use the same clock, since
// what an operator actually wants to know is "is this still happening
// right now."
const RECENT_MS = 10 * 60 * 1000

// A steady cadence ("every ~2 m since 13:28") only reads as its own
// shape once it has run long enough to look deliberate -- a two-minute
// span with regular gaps is just as easily a burst that happened to
// land evenly (port_scan's twenty-ports-in-forty-seconds is exactly
// that case, and stays a burst, not a cadence).
const REGULAR_SPAN_FLOOR_MS = 2 * 60 * 1000

// Below this span, a burst gets second-level precision
// ("13:41:02 -> 13:41:42") because minute-level would round every event
// into the same one or two minutes and say nothing. Above it, "first
// HH:MM * last HH:MM" is precise enough and reads calmer.
const SECONDS_PRECISION_SPAN_MS = 2 * 60 * 1000

// Four or fewer events are named individually rather than summarised --
// "13:34 * 13:36 * 13:39" is more honest than any aggregate a handful of
// points could support.
const ENUMERATE_MAX = 4

// A cadence needs at least this many events before "every ~N since" is
// a pattern rather than a guess from two data points.
const REGULAR_MIN_EVENTS = 4

function iso(ms: number): string {
  return new Date(ms).toISOString()
}

// "~2 m" / "~45 s" -- the cadence phrase inside "every ~N since HH:MM".
// Seconds below a minute, whole minutes above -- this codebase's
// episodes run from single-digit seconds (a fast scan) to tens of
// minutes (repeated_drops), and a decimal minute count ("~2.3 m") reads
// like false precision for a pattern that is, by definition, only
// approximately regular.
function humanizeGap(ms: number): string {
  if (ms < 60_000) return `${Math.max(1, Math.round(ms / 1000))} s`
  return `${Math.round(ms / 60_000)} m`
}

/**
 * The episode's shape, computed from a plain list of event timestamps
 * (epoch ms) and the current time -- nothing else. Which of four
 * renderings applies is decided by the timestamps themselves:
 *
 *   - a handful (<=4) of events, however spaced: each one named,
 *     "13:34 * 13:36 * 13:39 * quiet since" (or "* still arriving" if
 *     the last one just landed)
 *   - several events at a steady cadence over more than a couple of
 *     minutes: "every ~2 m since 13:28" -- repeated_drops' own
 *     signature, machine-regular rather than bursty
 *   - many events bunched into two minutes or less:
 *     "13:41:02 -> 13:41:42 * stopped", second-precision because a
 *     minute-level stamp would collapse the whole thing into one tick
 *   - many events over a longer span: "first 13:46 * last 13:52 *
 *     still arriving"
 *
 * "still arriving" vs "stopped"/"quiet since" is the same test
 * throughout: did the last event land within the last ten minutes.
 */
export function episodeShapeText(timesMs: number[], nowMs: number): string {
  const sorted = timesMs.filter((t) => !Number.isNaN(t)).sort((a, b) => a - b)
  if (sorted.length === 0) return ''

  const first = sorted[0]
  const last = sorted[sorted.length - 1]
  const recent = nowMs - last <= RECENT_MS

  if (sorted.length === 1) {
    return `${formatHM(iso(first))} · ${recent ? 'still arriving' : 'quiet since'}`
  }

  const gaps: number[] = []
  for (let i = 1; i < sorted.length; i++) gaps.push(sorted[i] - sorted[i - 1])
  const meanGap = gaps.reduce((a, b) => a + b, 0) / gaps.length
  const maxDeviation = Math.max(...gaps.map((g) => Math.abs(g - meanGap)))
  const regular = sorted.length >= REGULAR_MIN_EVENTS && maxDeviation <= meanGap * 0.4
  const span = last - first

  if (regular && span > REGULAR_SPAN_FLOOR_MS) {
    return `every ~${humanizeGap(meanGap)} since ${formatHM(iso(first))}`
  }

  if (sorted.length <= ENUMERATE_MAX) {
    return `${sorted.map((t) => formatHM(iso(t))).join(' · ')} · ${recent ? 'still arriving' : 'quiet since'}`
  }

  if (span <= SECONDS_PRECISION_SPAN_MS) {
    return `${formatTime(iso(first))} → ${formatTime(iso(last))} · ${recent ? 'still arriving' : 'stopped'}`
  }

  return `first ${formatHM(iso(first))} · last ${formatHM(iso(last))} · ${recent ? 'still arriving' : 'stopped'}`
}

/**
 * episodeShapeText applied to a flag: the richer per-event episode
 * (fetchFlagEpisode's own result) once it has resolved and found
 * something, falling back to the flag's own firstSeen/lastSeen -- which
 * are always present the instant the drawer opens, well before that
 * round-trip lands (or if it lands empty: raw events are only retained
 * in the buffer, so an old flag's episode often has nothing left to
 * show at all).
 */
export function episodeShapeFor(
  f: Pick<Flag, 'firstSeen' | 'lastSeen'>,
  episode: FirewallEvent[] | 'loading' | 'error' | undefined,
  nowMs: number,
): string {
  if (Array.isArray(episode) && episode.length > 0) {
    return episodeShapeText(
      episode.map((e) => new Date(e.time).getTime()),
      nowMs,
    )
  }
  const first = new Date(f.firstSeen).getTime()
  const last = new Date(f.lastSeen).getTime()
  if (Number.isNaN(first) || Number.isNaN(last)) return ''
  return episodeShapeText(first === last ? [first] : [first, last], nowMs)
}
