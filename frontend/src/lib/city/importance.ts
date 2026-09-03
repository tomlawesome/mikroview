// SPDX-License-Identifier: AGPL-3.0-only
//
// Height = importance (#867): two readings for a building's plinth,
// docs/design/screens/city/DESIGN.md's "Height = importance". Both are
// pure folds over data the app already has -- pass in whatever the
// caller already holds (the event buffer, the flags list, the
// watchlist entries) and get back a normalised plinth height per
// building, so City.svelte's drawing code never does its own
// arithmetic on a raw count.
import { isAccepted } from '../reach'
import { extractSourceIp } from '../flags.svelte'
import { WALL_H } from './walls'
import type { ClientEvent, Flag, WatchlistEntry } from '../types'

export type ImportanceReading = 'depended-on' | 'watched'

export const DEFAULT_IMPORTANCE_READING: ImportanceReading = 'depended-on'

export const IMPORTANCE_READINGS: { id: ImportanceReading; label: string }[] = [
  { id: 'depended-on', label: 'depended-on' },
  { id: 'watched', label: 'watched' },
]

/** Plinth height range every reading normalises into. The floor sits
 * clear of a district's own wall (WALL_H, "well under even a host's
 * shortest plinth" -- walls.ts) rather than repeating that number, so
 * the two can never drift out of the relationship the record assumes.
 * The ceiling matches the pre-#867 town's tallest fixed plinth (the
 * primary router's), so swapping in a real reading does not itself
 * redraw the skyline's scale. */
export const IMPORTANCE_FLOOR_H = WALL_H + 0.5
export const IMPORTANCE_CEIL_H = 9

export interface Importance {
  /** Plinth height, already scaled to IMPORTANCE_FLOOR_H..IMPORTANCE_CEIL_H. */
  height: number
  /** depended-on: false when the window carries no event at all naming
   * this host (as either side of a connection) -- the floor then means
   * "no data", never "nothing depends on this". A building the window
   * *does* have events for, with zero accepted talkers, is `known: true`
   * at the floor -- that is a real answer.
   * watched: always true; see watchedNotice for the "the store hasn't
   * loaded" case, which is a fact about the reading as a whole rather
   * than about any one building. */
  known: boolean
}

export interface ImportanceBuilding {
  id: string
  ip: string
}

const FLOOR: Importance = { height: IMPORTANCE_FLOOR_H, known: true }

/** Scales raw non-negative counts so the largest becomes the ceiling
 * and everything else sits proportionally above the floor -- an empty
 * or all-zero map stays at the floor throughout. */
function scale(raw: ReadonlyMap<string, number>): Map<string, Importance> {
  let max = 0
  for (const v of raw.values()) if (v > max) max = v
  const out = new Map<string, Importance>()
  for (const [id, v] of raw) {
    const height = max <= 0 ? IMPORTANCE_FLOOR_H : IMPORTANCE_FLOOR_H + (IMPORTANCE_CEIL_H - IMPORTANCE_FLOOR_H) * (v / max)
    out.set(id, { height, known: true })
  }
  return out
}

/**
 * dependedOnImportance: how many distinct hosts talked to each building
 * in the window, from the event buffer alone. "Talked to" means an
 * accepted connection landed on it -- the same accept/blocked split
 * reach.ts's reachFor already makes (isAccepted), reused rather than
 * re-decided here, so the two never disagree about what counts as a
 * connection actually landing.
 */
export function dependedOnImportance(buildings: ImportanceBuilding[], events: ClientEvent[]): Map<string, Importance> {
  const talkersByIp = new Map<string, Set<string>>()
  const seen = new Set<string>()
  for (const e of events) {
    if (e.srcIp) seen.add(e.srcIp)
    if (e.dstIp) seen.add(e.dstIp)
    if (!e.srcIp || !e.dstIp || !isAccepted(e.action)) continue
    let talkers = talkersByIp.get(e.dstIp)
    if (!talkers) talkersByIp.set(e.dstIp, (talkers = new Set()))
    talkers.add(e.srcIp)
  }

  const raw = new Map<string, number>()
  const noData: string[] = []
  for (const b of buildings) {
    if (!seen.has(b.ip)) {
      noData.push(b.id)
      continue
    }
    raw.set(b.id, talkersByIp.get(b.ip)?.size ?? 0)
  }

  const out = scale(raw)
  for (const id of noData) out.set(id, { ...FLOOR, known: false })
  return out
}

/**
 * watchedImportance: the flag and watch weight the operator has put on
 * each building -- open (uncleared) flags, resolved to a host the same
 * way the flags store's own campaign grouping already resolves a
 * flag's target (extractSourceIp), plus enabled watchlist entries
 * scoped to that host's address. Unwatched and unflagged sits at the
 * floor; a twice-flagged host outweighs a once-flagged one.
 */
export function watchedImportance(buildings: ImportanceBuilding[], flags: Flag[], watchlist: WatchlistEntry[]): Map<string, Importance> {
  const weightByIp = new Map<string, number>()
  const bump = (ip: string | null | undefined) => {
    if (!ip) return
    weightByIp.set(ip, (weightByIp.get(ip) ?? 0) + 1)
  }
  for (const f of flags) {
    if (f.cleared) continue
    bump(extractSourceIp(f.target))
  }
  for (const w of watchlist) {
    if (!w.enabled) continue
    bump(w.source?.ip)
  }

  const raw = new Map<string, number>()
  for (const b of buildings) raw.set(b.id, weightByIp.get(b.ip) ?? 0)
  return scale(raw)
}

/**
 * Whether the watched reading can be trusted yet. Null once both the
 * flags and watchlist stores have loaded; otherwise the sentence to
 * show instead of letting a skyline that has not heard from either
 * store read as "nothing is watched" -- the app's existing "no data
 * pushed/loaded yet" wording (api.ts's RouterFilterRule doc comment,
 * routerLookup.svelte.ts's `available`), applied to these two stores.
 */
export function watchedNotice(flagsLoaded: boolean, watchlistLoaded: boolean): string | null {
  if (flagsLoaded && watchlistLoaded) return null
  if (!flagsLoaded && !watchlistLoaded) return 'flags and the watchlist have not loaded yet — watched heights are not yet known'
  if (!flagsLoaded) return 'flags have not loaded yet — watched heights are not yet known'
  return 'the watchlist has not loaded yet — watched heights are not yet known'
}

/**
 * tweenHeights: one animation step from `from` toward `target`'s own
 * heights, `t` in 0..1. `reduced` (prefers-reduced-motion) snaps
 * straight to the target regardless of `t`, matching the camera moves'
 * own reduced-motion snap (City.svelte's moveCamera / project.ts's
 * reducedMotion).
 */
export function tweenHeights(from: ReadonlyMap<string, number>, target: ReadonlyMap<string, Importance>, t: number, reduced: boolean): Map<string, number> {
  const out = new Map<string, number>()
  const clamped = Math.max(0, Math.min(1, t))
  for (const [id, imp] of target) {
    if (reduced) {
      out.set(id, imp.height)
      continue
    }
    const start = from.get(id) ?? imp.height
    out.set(id, start + (imp.height - start) * clamped)
  }
  return out
}
