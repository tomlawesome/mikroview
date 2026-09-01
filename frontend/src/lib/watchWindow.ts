// SPDX-License-Identifier: AGPL-3.0-only
//
// Rendering for a watch's window and its seven nights of memory (#680).
// Two sentences, both of which have to be exactly true:
//
//   - the row's "window" cell, which read a flat "always" before there
//     was any window to read, and still does for an entry without one;
//   - the drawer's nightly summary, "five kept nights · two empty",
//     which grows a third clause when a night could not be observed and
//     drops it again when every night could.
//
// Nothing here invents a fact. An entry with no nights recorded yet gets
// no sentence at all rather than a zeroed one, and a night mikroview did
// not observe is never counted as empty -- that is the one rule the whole
// feature exists to keep.
import type { WatchlistEntry, WatchNight, WatchWindow } from './types'

// One through seven, which is every count a seven-night history can
// produce. Deliberately local and short rather than reaching into
// flagNarrative's own list: that one runs to twenty for prose about flag
// counts, and this sentence can never need an eighth word.
const NIGHT_WORDS = ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven']

function nightWord(n: number): string {
  return NIGHT_WORDS[n] ?? String(n)
}

const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

// 00:00 is the zero value of a clock time, so the server omits it -- an
// absent start or end means midnight, never "unset". See WatchWindow.
function clockOf(v: string | undefined): string {
  return v ?? '00:00'
}

// A window is present only when its two clock times differ: a zero-length
// window is the absence of one. Mirrors watchlist.Window.Defined.
export function hasWindow(w: WatchWindow | undefined): w is WatchWindow {
  if (!w) return false
  return clockOf(w.start) !== clockOf(w.end)
}

// windowLabel is the row's "window" cell. "always" for an entry with no
// window, which is what every row read before windows existed and is
// still the honest answer for one that has none.
//
// The zone is always named, including when it is UTC, because the whole
// point of storing it is that "00:00-06:00" means a particular six hours,
// and an operator cannot check that against a label that hides which.
export function windowLabel(e: Pick<WatchlistEntry, 'window'>): string {
  const w = e.window
  if (!hasWindow(w)) return 'always'
  const range = `${clockOf(w.start)}–${clockOf(w.end)} ${w.zone || 'UTC'}`
  const days = (w.days ?? []).map((d) => DAY_NAMES[d]).filter(Boolean)
  return days.length > 0 ? `${days.join(', ')} ${range}` : range
}

// crossesMidnight is true when the window runs into the following date --
// end <= start, the normal case rather than the edge.
export function crossesMidnight(w: WatchWindow | undefined): boolean {
  if (!hasWindow(w)) return false
  return clockOf(w.end) <= clockOf(w.start)
}

export type NightCounts = { kept: number; empty: number; unobserved: number; total: number }

export function countNights(nights: WatchNight[] | undefined): NightCounts {
  const out = { kept: 0, empty: 0, unobserved: 0, total: 0 }
  for (const n of nights ?? []) {
    out.total++
    if (n.state === 'kept') out.kept++
    else if (n.state === 'empty') out.empty++
    else out.unobserved++
  }
  return out
}

// nightlySummary is the drawer's sentence about the last seven nights, or
// null when there is nothing recorded to summarise -- a watch added an
// hour ago has no nights yet, and "no kept nights" would read as a
// finding rather than as the absence of one.
//
// The ratified copy is "seven kept nights" and "five kept nights · two
// empty". The third clause is the wording the owner ratified on top of
// it: it appears only when a night could not be observed, and disappears
// again when every night could.
//
// When nothing at all could be observed there is no kept/empty split to
// lead with, so the whole sentence becomes "seven nights not observed" --
// leading with "no kept nights" there would be a claim about the network
// made out of mikroview's own blind spot.
export function nightlySummary(nights: WatchNight[] | undefined): string | null {
  const c = countNights(nights)
  if (c.total === 0) return null
  if (c.kept === 0 && c.empty === 0) {
    return `${nightWord(c.unobserved)} ${c.unobserved === 1 ? 'night' : 'nights'} not observed`
  }
  const clauses = [`${nightWord(c.kept)} kept ${c.kept === 1 ? 'night' : 'nights'}`]
  if (c.empty > 0) clauses.push(`${nightWord(c.empty)} empty`)
  if (c.unobserved > 0) clauses.push(`${nightWord(c.unobserved)} not observed`)
  return clauses.join(' · ')
}
