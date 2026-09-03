// SPDX-License-Identifier: AGPL-3.0-only
//
// The reach's strand model (#626/#485: recentre on a node -- its
// connections, devices/IPs, ports, per direction). Derived from the
// observed event buffer alone: under the Traffic lens an accepted flow
// is a strand that passes the membrane, a dropped one dies at it, and
// the refusing rule is the event's own rule label -- what arrived,
// never what was elicited.
import type { ClientEvent } from './types'

/** How fast an event stops counting toward "busiest right now" (#701).
 *
 * The owner chose a recency-weighted ranking over a plain
 * last-five-minutes count (2026-09-03), because a cliff edge makes a
 * pathway vanish from the sentence the moment its last event ages out,
 * while the traffic itself tapered. A half-life keeps the same
 * timescale without the cliff: five minutes ago counts half, ten
 * minutes a quarter, an hour roughly a thousandth.
 *
 * Weighted by the client's own arrival stamp rather than the router's
 * timestamp, so the ranking needs no clock agreement with the router --
 * the same reasoning #874 applies to WireGuard handshake ages. */
export const RECENCY_HALF_LIFE_MS = 5 * 60 * 1000

export interface ReachStrand {
  /** Grouping key: counterpart boundary + direction + outcome. */
  key: string
  /** The far side: a boundary interface, or 'internet'. */
  counterpart: string
  outcome: 'accepted' | 'blocked'
  /** Relative to the centred host: 'out' = it spoke, 'in' = it was spoken to. */
  direction: 'out' | 'in'
  /** Top far-side hosts (names where the events carry them). */
  peers: string[]
  /** Top far-side raw addresses, same ranking -- what a printed rule
   * targets (a name is display, never a match condition). */
  peerAddrs: string[]
  /** Top destination ports knocked on this strand. */
  ports: number[]
  /** The same ports with how often each was asked for and the protocol
   * seen asking -- the compose panel's "it's been asking · 14×". */
  portHits: { port: number; n: number; proto: string }[]
  count: number
  /** The same traffic with recent events counting for more, decaying by
   * RECENCY_HALF_LIFE_MS. Ranks "busiest right now"; `count` still ranks
   * "busiest over the whole buffer", and the two are different questions.
   * Not a number to show anyone -- it has no unit. */
  weight: number
  /** The rule that refused a blocked strand, from the events themselves. */
  refusedBy?: string
}

export interface ReachSummary {
  strands: ReachStrand[]
  /** Distinct counterparts this host spoke to, accepted. */
  reaches: number
  /** Distinct counterparts that spoke to it, accepted. */
  reachedBy: number
  /** The busiest blocked strand, for the crumb's own alarm line. */
  topBlocked: ReachStrand | null
  /** The busiest pathway weighted toward now (#701), which round 30
   * states on the reach's zone card. Null when nothing was observed --
   * the surface then says nothing rather than naming a pathway from an
   * empty buffer. */
  busiest: ReachStrand | null
}

// What counts as a connection actually landing, rather than being
// refused -- shared with lib/city/importance.ts's depended-on reading
// (#867) so the two never quietly disagree about what "reached" means.
export const ACCEPTED_ACTIONS = new Set(['accept', 'nat', 'log'])
export const isAccepted = (action: string): boolean => ACCEPTED_ACTIONS.has(action)

export function reachFor(ip: string, wanInterface: string | null, events: ClientEvent[], now: number = Date.now()): ReachSummary {
  const groups = new Map<
    string,
    ReachStrand & {
      peerCounts: Map<string, number>
      peerAddrCounts: Map<string, number>
      portCounts: Map<number, number>
      portProto: Map<number, string>
    }
  >()

  for (const e of events) {
    const out = e.srcIp === ip
    const inn = e.dstIp === ip
    if (!out && !inn) continue
    // The far boundary is the interface on the other side of the host:
    // where the traffic left toward (out) or arrived from (in).
    const farIface = out ? e.outInterface : e.inInterface
    if (!farIface) continue
    const counterpart = farIface === wanInterface ? 'internet' : farIface
    const outcome: ReachStrand['outcome'] = isAccepted(e.action) ? 'accepted' : 'blocked'
    const direction: ReachStrand['direction'] = out ? 'out' : 'in'
    const key = `${counterpart}|${direction}|${outcome}`
    let g = groups.get(key)
    if (!g) {
      g = {
        key,
        counterpart,
        outcome,
        direction,
        peers: [],
        peerAddrs: [],
        ports: [],
        portHits: [],
        count: 0,
        weight: 0,
        peerCounts: new Map(),
        peerAddrCounts: new Map(),
        portCounts: new Map(),
        portProto: new Map(),
      }
      groups.set(key, g)
    }
    g.count++
    // An event stamped in the future (a clock that moved, or a buffer
    // replayed) is clamped to full weight rather than allowed to
    // outrank everything by an unbounded amount.
    g.weight += 2 ** (-Math.max(0, now - e.receivedAt) / RECENCY_HALF_LIFE_MS)
    const peer = out ? (e.dstHostName ?? e.dstIp) : (e.srcHostName ?? e.srcIp)
    if (peer) g.peerCounts.set(peer, (g.peerCounts.get(peer) ?? 0) + 1)
    const peerAddr = out ? e.dstIp : e.srcIp
    if (peerAddr) g.peerAddrCounts.set(peerAddr, (g.peerAddrCounts.get(peerAddr) ?? 0) + 1)
    if (e.dstPort) {
      g.portCounts.set(e.dstPort, (g.portCounts.get(e.dstPort) ?? 0) + 1)
      if (e.protocol && !g.portProto.has(e.dstPort)) g.portProto.set(e.dstPort, e.protocol.toLowerCase())
    }
    if (outcome === 'blocked' && e.ruleLabel && !g.refusedBy) g.refusedBy = e.ruleLabel
  }

  const strands = [...groups.values()]
    .map((g) => ({
      key: g.key,
      counterpart: g.counterpart,
      outcome: g.outcome,
      direction: g.direction,
      peers: [...g.peerCounts.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p),
      peerAddrs: [...g.peerAddrCounts.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p),
      ports: [...g.portCounts.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p),
      portHits: [...g.portCounts.entries()]
        .sort((a, b) => b[1] - a[1])
        .map(([port, n]) => ({ port, n, proto: g.portProto.get(port) ?? 'tcp' })),
      count: g.count,
      weight: g.weight,
      refusedBy: g.refusedBy,
    }))
    .sort((a, b) => b.count - a.count)

  const reaches = new Set(strands.filter((s) => s.direction === 'out' && s.outcome === 'accepted').map((s) => s.counterpart)).size
  const reachedBy = new Set(strands.filter((s) => s.direction === 'in' && s.outcome === 'accepted').map((s) => s.counterpart)).size
  const topBlocked = strands.find((s) => s.outcome === 'blocked') ?? null
  // Deliberately not the same ordering as `strands`, which stays sorted
  // by lifetime count: that order is what the drawn strands and the
  // crumb's alarm line already read, and re-sorting it would silently
  // change both.
  const busiest = strands.reduce<ReachStrand | null>((best, s) => (best === null || s.weight > best.weight ? s : best), null)

  return { strands, reaches, reachedBy, topBlocked, busiest }
}
