// SPDX-License-Identifier: AGPL-3.0-only
//
// Brightness by baseline, rolled up onto the city's roads (#1016, round
// 49). DESIGN.md's second always-on rule: "colour is the verdict;
// brightness is the baseline". A road is drawn as bright as the
// brightest line it carries -- one off-baseline line among a thousand
// lights the one place it happened.
//
// Pure by design, like baseline.ts next door: no Svelte state, no
// fetching, no markup. City.svelte holds the $derived that calls
// rollUpRoads and the drawing that reads its answer.
//
// Two things this module exists to get right.
//
// **Attribution.** An OffBaselineLine carries a source and a destination
// address; a road joins two districts. layout.ts already keys every
// district-pair road by `[from, to].sort().join('|')` -- the road's own
// id *is* the sorted pair key -- so resolving each address to a district
// and sorting the pair yields the road id directly, with no search over
// roads. Addresses resolve by drawn building first (an exact map), then
// by district CIDR (so a host the bounded plate did not draw still lands
// in its own district, which `District.more` says exists), then, failing
// both, as the WAN. Nothing is guessed: an address that resolves to no
// district and has no WAN to fall back on attributes to nothing, and its
// line simply lights no road.
//
// **Cost.** The scene is rebuilt on every camera frame, so nothing here
// may run per frame. rollUpRoads is O(lines + roads) once per change of
// ground or of the off-baseline document -- both rare -- and hands back
// a Map the per-road drawing reads with one O(1) get. The line list is
// proportional to novelty rather than to traffic (baseline.ts says why),
// so it is small by construction even on a busy network.
import { addressInCidr, parseCidr, type ParsedCidr } from '../addressMatch'
import type { OffBaseline, OffBaselineLine } from '../baseline'
import type { District, Ground, Road } from './types'

/**
 * Which end of a road off-baseline traffic arrived at.
 *
 * Both can be true: one road folds both directions (layout.ts), so a
 * road can carry a new line each way, and each end that received one
 * gets its own ring. Neither is true when the road carries off-baseline
 * lines whose destination is not either of its ends -- possible only if
 * the ground and the register disagree, and drawn as no ring rather than
 * as a guessed one.
 */
export interface RoadRing {
  start: boolean
  end: boolean
}

/** What one road carries off the baseline today. */
export interface RoadBaselineEntry {
  /** Today's off-baseline lines on this road; never empty. */
  lines: OffBaselineLine[]
  /** The end(s) the traffic arrived at, for the throbbing ring. */
  ring: RoadRing
  /** Entity id at `pts[0]`, and at the last point. */
  ends: { start: string; end: string }
}

/** Road id -> what it carries. Absence means "everything established". */
export type RoadBaseline = ReadonlyMap<string, RoadBaselineEntry>

/** What the map draws before the first fetch lands. */
export const EMPTY_ROAD_BASELINE: RoadBaseline = new Map()

/**
 * Address -> district, prepared once so each line costs one map lookup
 * plus at most one pass over the district CIDRs.
 */
export interface ZoneIndex {
  /** Exact address of a drawn building -> its district id. */
  byAddress: ReadonlyMap<string, string>
  /** District CIDRs, most specific first. */
  nets: readonly { id: string; net: ParsedCidr }[]
  /** The WAN interface id, when there is one: where anything outside
   * every district belongs. */
  wanId: string | null
}

/**
 * Build the address index for one ground.
 *
 * Only buildings that stand in a district are indexed by address: a
 * router or a bridge head stands in none (`districtId` is null) and its
 * "address" may be a phrase like `wan bridge` rather than an address at
 * all.
 */
export function zoneIndex(ground: Ground, wanId: string | null): ZoneIndex {
  const byAddress = new Map<string, string>()
  const nets: { id: string; net: ParsedCidr }[] = []
  for (const d of ground.districts) {
    for (const b of d.buildings) {
      // First writer wins: two buildings sharing an address is a
      // contradiction in the ground, not something to resolve by
      // preferring whichever came last.
      if (b.ip && !byAddress.has(b.ip)) byAddress.set(b.ip, d.id)
    }
    const net = d.cidr ? parseCidr(d.cidr) : null
    if (net) nets.push({ id: d.id, net })
  }
  // Most specific first, so a district carved out of a wider one wins
  // its own addresses.
  nets.sort((a, b) => b.net.prefix - a.net.prefix)
  return { byAddress, nets, wanId }
}

/**
 * Which district an address belongs to, or null when nothing can be
 * said.
 *
 * The WAN fallback is the honest reading of "in none of this estate's
 * subnets": the city draws exactly one outside, and a line to an address
 * beyond every district arrived across that boundary. With no WAN
 * interface known there is no outside to name, so the answer is null and
 * the line lights nothing.
 */
export function zoneOfAddress(ix: ZoneIndex, address: string): string | null {
  const exact = ix.byAddress.get(address)
  if (exact !== undefined) return exact
  for (const n of ix.nets) if (addressInCidr(address, n.net)) return n.id
  return ix.wanId
}

/**
 * The entity id at each end of a district-pair road, in drawing order.
 *
 * The road's id is the sorted pair key, which says *which two* entities
 * it joins but not which is drawn first; `Road.from`/`to` only name an
 * end that is a node, so a district-to-district road names neither. The
 * end a district sits at is recovered by distance: layout.ts puts
 * `pts[0]` on that district's own wall (`gateToward`), so the district
 * named in the id whose centre is nearer `pts[0]` than the far point is
 * the start.
 *
 * Returns null for a lane (`lane:<building>`, not a pair key) and for
 * any id that does not name a district this ground draws.
 */
export function roadEnds(road: Road, districts: readonly District[]): { start: string; end: string } | null {
  const bar = road.id.indexOf('|')
  if (bar < 0) return null
  const p = road.id.slice(0, bar)
  const q = road.id.slice(bar + 1)
  if (!p || !q) return null
  const first = road.pts[0]
  const last = road.pts[road.pts.length - 1]
  if (!first || !last) return null
  const centreOf = (id: string) => {
    const d = districts.find((x) => x.id === id)
    return d ? ([d.u, d.v] as const) : null
  }
  const near = (c: readonly [number, number], at: readonly number[]) => Math.hypot(c[0] - at[0], c[1] - at[1])
  const cp = centreOf(p)
  const cq = centreOf(q)
  // One district end is enough to orient the road: whichever end that
  // district is nearer to is its own, and the other id takes the far
  // end. That covers the WAN road, whose far end is a router node.
  if (cp) return near(cp, first) <= near(cp, last) ? { start: p, end: q } : { start: q, end: p }
  if (cq) return near(cq, first) <= near(cq, last) ? { start: q, end: p } : { start: p, end: q }
  return null
}

/**
 * Roll today's off-baseline lines onto the roads that carry them.
 *
 * Only district-pair roads take part. A lane (`Road.lane`) is one
 * building's own street to its district edge and is drawn at the street
 * stop in unjudged ink; the reach, which is the surface that draws per
 * line, owns that story and is built in its own slice.
 *
 * Roads of every verdict are rolled up, not only accepted ones, because
 * the card lists what a road carries whatever colour it is. Which of
 * them *change how they are drawn* is the drawing's decision, not this
 * one: refused and escalated roads are unchanged by the baseline.
 */
export function rollUpRoads(ground: Ground, off: OffBaseline, wanId: string | null): RoadBaseline {
  const out = new Map<string, RoadBaselineEntry>()
  if (off.lines.length === 0) return out
  const ix = zoneIndex(ground, wanId)
  // Only roads this ground actually drew: a pair whose boundary nothing
  // logs draws no road at all (round 49), and lighting a road that is
  // not there is not possible -- the line is simply not shown here.
  const pairRoads = new Map<string, Road>()
  for (const r of ground.roads) if (!r.lane && !pairRoads.has(r.id)) pairRoads.set(r.id, r)

  for (const l of off.lines) {
    const src = zoneOfAddress(ix, l.srcIp)
    const dst = zoneOfAddress(ix, l.dstIp)
    // A line inside one district crosses no boundary and so runs along
    // no road; there is nothing to light.
    if (!src || !dst || src === dst) continue
    const id = [src, dst].sort().join('|')
    const road = pairRoads.get(id)
    if (!road) continue
    let had = out.get(id)
    if (!had) {
      const ends = roadEnds(road, ground.districts)
      if (!ends) continue
      had = { lines: [], ring: { start: false, end: false }, ends }
      out.set(id, had)
    }
    had.lines.push(l)
    // The ring sits at the end the traffic arrived at -- the
    // destination's own end of this road.
    if (dst === had.ends.start) had.ring.start = true
    else if (dst === had.ends.end) had.ring.end = true
  }
  // Busiest first, so the card's table leads with the line that happened
  // most; ties keep the register's own order, which is stable.
  for (const e of out.values()) e.lines.sort((a, b) => b.count - a.count)
  return out
}

/**
 * The verdict, in the plain words the card says it in.
 *
 * The mockup's line reads "the router accepted it (rule #12); nothing
 * decided it was wanted". OffBaselineLine carries no rule label, so the
 * rule is not named here rather than invented -- the same choice the
 * road's drop mark makes when no event on the pair carried one (#865).
 */
export function verdictWords(outcome: OffBaselineLine['outcome']): string {
  return outcome === 'accept'
    ? 'the router accepted it; nothing decided it was wanted'
    : 'the router dropped it; nothing decided it was wanted'
}

/**
 * How a line's two ends read in the card's LINE column: the building's
 * own name where this ground draws one at that address, the address
 * itself otherwise.
 */
export function addressName(ground: Ground, address: string): string {
  for (const d of ground.districts) {
    for (const b of d.buildings) if (b.ip === address) return b.name
  }
  for (const n of ground.nodes) if (n.ip === address) return n.name
  return address
}

/**
 * How the card names one end of a road: the district's own name where
 * this ground draws that district, the raw id otherwise (the WAN
 * interface, which is a node rather than a district).
 */
export function endName(ground: Ground, id: string): string {
  const d = ground.districts.find((x) => x.id === id)
  if (d) return d.name
  const n = ground.nodes.find((x) => x.id === id || x.name === id)
  return n ? n.name : id
}

/** `5001/tcp`, or the bare protocol when the event carried no port. */
export function portWords(line: OffBaselineLine): string {
  return line.port === null ? line.proto : `${line.port}/${line.proto}`
}
