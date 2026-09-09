// SPDX-License-Identifier: AGPL-3.0-only
//
// Brightness for the reach's own lanes (round 49, #1016).
//
// `baselineRoads.rollUpRoads` deliberately rolls district-pair roads
// only, and says why: "A lane is one building's own street to its
// district edge ... the reach, which is the surface that draws per line,
// owns that story and is built in its own slice." This is that slice.
//
// The difference from a pair road is what a lane *is*. A pair road joins
// two districts, so a line lands on it when its two ends sit in those two
// districts. A lane joins one building to its own district's edge, so a
// line lands on it when that building is one of its ends -- an address
// match, not a zone match, and no ZoneIndex is needed at all.
import type { OffBaseline, OffBaselineLine } from '../baseline'
import type { RoadRing } from './baselineRoads'

/** One lane the standing host's reach draws, and what it carries. */
export interface ReachLaneEntry {
  /** Today's off-baseline lines on this lane; never empty. */
  lines: OffBaselineLine[]
  /** The end the traffic arrived at, for the throbbing arrival mark. */
  ring: RoadRing
  /** The ink the lane takes: the verdict of what it carries. A lane is
   * laid down in unjudged ink (`layout.ts` draws it `k: 'q'`) because
   * outside the reach it is scenery. Inside the reach it is a drawn
   * line, and colour is the verdict -- so a lane carrying anything
   * accepted reads accept, and one carrying only drops reads drop. */
  kind: 'a' | 'd'
}

/** Lane road id -> what it carries. Absence means "everything
 * established", exactly as `RoadBaseline` means it. */
export type ReachLaneBaseline = ReadonlyMap<string, ReachLaneEntry>

/** What the reach draws before any off-baseline document has landed. */
export const EMPTY_REACH_LANES: ReachLaneBaseline = new Map()

/** A lane in the reach: the road that draws it and the host it belongs
 * to. The caller resolves both, because which lanes the reach lights is
 * the drawing's business (`City.svelte`'s `reachOverlay`), not this
 * module's. */
export interface LaneSubject {
  roadId: string
  ip: string
}

/**
 * Roll today's off-baseline lines onto the reach's lanes.
 *
 * The mark goes at the end the traffic arrived at, the same rule the
 * pair roads follow. A lane's `pts[0]` is the building itself and its
 * last point is the district's own gate, so:
 *
 * - a line *to* this host arrived here, and marks the lane's start --
 *   which is this host's own building, the one case where the reach
 *   resolves the arrival to a single host (#1057);
 * - a line *from* this host arrived somewhere else, and marks whatever
 *   draws that somewhere -- the peer's own lane or the pair road between
 *   the districts. Marking the gate instead would put it at a place no
 *   traffic arrived at, which is the guess this refuses.
 *
 * A host that is both ends of a line (it talked to itself) marks the
 * start, because it did arrive there.
 */
export function rollUpLanes(off: OffBaseline, lanes: readonly LaneSubject[]): ReachLaneBaseline {
  const out = new Map<string, ReachLaneEntry>()
  if (off.lines.length === 0 || lanes.length === 0) return out

  // One pass over the lines per lane would be O(lanes x lines); the
  // reach lights a lane per lit peer, so index the lanes by address
  // instead and make it one pass over the lines. Several lanes can share
  // an address only if the ground drew the same host twice, and both
  // then light, which is what the drawing shows.
  const byIp = new Map<string, LaneSubject[]>()
  for (const l of lanes) {
    if (!l.ip) continue
    const had = byIp.get(l.ip)
    if (had) had.push(l)
    else byIp.set(l.ip, [l])
  }

  const land = (subject: LaneSubject, line: OffBaselineLine, arrived: boolean) => {
    let e = out.get(subject.roadId)
    if (!e) {
      e = { lines: [], ring: { start: false, end: false }, kind: 'd' }
      out.set(subject.roadId, e)
    }
    // The same line reaches a lane once even when the host is both of
    // its ends, so the card never lists it twice.
    if (!e.lines.some((x) => x.key === line.key)) e.lines.push(line)
    if (arrived) e.ring.start = true
    if (line.outcome === 'accept') e.kind = 'a'
  }

  for (const line of off.lines) {
    for (const subject of byIp.get(line.dstIp) ?? []) land(subject, line, true)
    for (const subject of byIp.get(line.srcIp) ?? []) land(subject, line, line.dstIp === line.srcIp)
  }

  // Busiest first, so a card reading these leads with the line that
  // happened most -- the same order `rollUpRoads` leaves its own in.
  for (const e of out.values()) e.lines.sort((a, b) => b.count - a.count)
  return out
}
