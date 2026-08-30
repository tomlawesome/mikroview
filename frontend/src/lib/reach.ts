// SPDX-License-Identifier: AGPL-3.0-only
//
// The reach's strand model (#626/#485: recentre on a node -- its
// connections, devices/IPs, ports, per direction). Derived from the
// observed event buffer alone: under the Traffic lens an accepted flow
// is a strand that passes the membrane, a dropped one dies at it, and
// the refusing rule is the event's own rule label -- what arrived,
// never what was elicited.
import type { FirewallEvent } from './types'

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
  /** Top destination ports knocked on this strand. */
  ports: number[]
  count: number
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
}

const ACCEPTED = new Set(['accept', 'nat', 'log'])

export function reachFor(ip: string, wanInterface: string | null, events: FirewallEvent[]): ReachSummary {
  const groups = new Map<string, ReachStrand & { peerCounts: Map<string, number>; portCounts: Map<number, number> }>()

  for (const e of events) {
    const out = e.srcIp === ip
    const inn = e.dstIp === ip
    if (!out && !inn) continue
    // The far boundary is the interface on the other side of the host:
    // where the traffic left toward (out) or arrived from (in).
    const farIface = out ? e.outInterface : e.inInterface
    if (!farIface) continue
    const counterpart = farIface === wanInterface ? 'internet' : farIface
    const outcome: ReachStrand['outcome'] = ACCEPTED.has(e.action) ? 'accepted' : 'blocked'
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
        ports: [],
        count: 0,
        peerCounts: new Map(),
        portCounts: new Map(),
      }
      groups.set(key, g)
    }
    g.count++
    const peer = out ? (e.dstHostName ?? e.dstIp) : (e.srcHostName ?? e.srcIp)
    if (peer) g.peerCounts.set(peer, (g.peerCounts.get(peer) ?? 0) + 1)
    if (e.dstPort) g.portCounts.set(e.dstPort, (g.portCounts.get(e.dstPort) ?? 0) + 1)
    if (outcome === 'blocked' && e.ruleLabel && !g.refusedBy) g.refusedBy = e.ruleLabel
  }

  const strands = [...groups.values()]
    .map((g) => ({
      key: g.key,
      counterpart: g.counterpart,
      outcome: g.outcome,
      direction: g.direction,
      peers: [...g.peerCounts.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p),
      ports: [...g.portCounts.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p),
      count: g.count,
      refusedBy: g.refusedBy,
    }))
    .sort((a, b) => b.count - a.count)

  const reaches = new Set(strands.filter((s) => s.direction === 'out' && s.outcome === 'accepted').map((s) => s.counterpart)).size
  const reachedBy = new Set(strands.filter((s) => s.direction === 'in' && s.outcome === 'accepted').map((s) => s.counterpart)).size
  const topBlocked = strands.find((s) => s.outcome === 'blocked') ?? null

  return { strands, reaches, reachedBy, topBlocked }
}
