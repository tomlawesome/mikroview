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
  /** Every (destination port, protocol) pair the strand carried, with how
   * often each was seen -- what `reachLineSummary` needs and `portHits`
   * cannot answer, since that keeps one row per port with the first
   * protocol seen asking.
   *
   * `port` is null where the event named no destination port (ICMP and
   * friends), so these entries total `count` and a per-protocol split
   * does not quietly drop portless traffic. `proto` is '' where the event
   * named no protocol: `portHits` falls back to 'tcp' for display, but
   * counting an unnamed protocol as TCP would invent the fact a tcp/udp
   * split is asked for.
   *
   * Optional only so the existing hand-built strand fixtures elsewhere
   * still compile; `reachFor` always fills it. */
  protoHits?: { port: number | null; proto: string; n: number }[]
  count: number
  /** The same traffic with recent events counting for more, decaying by
   * RECENCY_HALF_LIFE_MS. Ranks "busiest right now"; `count` still ranks
   * "busiest over the whole buffer", and the two are different questions.
   * Not a number to show anyone -- it has no unit. */
  weight: number
  /** The rule that refused a blocked strand, from the events themselves.
   * Taken from the most recent blocked event on the strand, not the
   * first: a rule table can be edited (a named rule removed, replaced by
   * nothing or by a different one), and naming a rule the current table
   * no longer has would be exactly the guessing this field exists to
   * refuse (#967). */
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
// refused.
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
      protoCounts: Map<string, { port: number | null; proto: string; n: number }>
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
        protoCounts: new Map(),
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
    // The same traffic counted per (port, protocol) instead of per port,
    // for the hovered line's own summary. Every event is counted, port or
    // not, so these entries total `count`.
    const hitPort = e.dstPort ? e.dstPort : null
    const hitProto = e.protocol ? e.protocol.toLowerCase() : ''
    const hitKey = `${hitPort ?? ''}|${hitProto}`
    const hit = g.protoCounts.get(hitKey)
    if (hit) hit.n++
    else g.protoCounts.set(hitKey, { port: hitPort, proto: hitProto, n: 1 })
    // The latest drop wins, not the first: events arrive oldest-first, so
    // this is "what the most recent refusal said", including reverting
    // to unnamed when the newest drop carries no label even though an
    // older one did -- the same stale-name defect #966 fixed in
    // reality.ts (#967).
    if (outcome === 'blocked') g.refusedBy = e.ruleLabel || undefined
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
      protoHits: [...g.protoCounts.values()].sort((a, b) => b.n - a.n || (a.port ?? Infinity) - (b.port ?? Infinity) || (a.proto < b.proto ? -1 : a.proto > b.proto ? 1 : 0)),
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

/** One destination port on a hovered line: what was tried there, and
 * whether it landed. `proto` is '' where the events named no protocol --
 * unknown, not assumed TCP. */
export interface ReachLinePort {
  port: number
  proto: 'tcp' | 'udp' | string
  accepted: number
  dropped: number
}

/** What a hovered reach line says about itself (#1016). */
export interface ReachLineSummary {
  counterpart: string
  /** Busiest first by total events, ties broken by port number. */
  ports: ReachLinePort[]
  /** Events on the line by protocol. Everything that is neither TCP nor
   * UDP -- ICMP, an unnamed protocol -- lands in `other`, so the three
   * total `accepted + dropped`. */
  tcp: number
  udp: number
  other: number
  /** Events on the line by outcome, portless traffic included. */
  accepted: number
  dropped: number
  /** The rule that refused this line, from its busiest dropped strand.
   * Undefined when nothing on the line was refused, or when the refusal
   * carried no rule label -- the same refusal to name a rule the events
   * did not name that `ReachStrand.refusedBy` makes (#967). */
  refusedBy?: string
}

/** What the reach view's hover card reads: one drawn line, not one strand
 * (#1016).
 *
 * A line is a counterpart pair, but `reachFor` groups by counterpart *and*
 * direction *and* outcome, so accepted and dropped traffic between the
 * same two ends arrive as separate strands. The owner's question -- "which
 * ports are being attempted here, and was each dropped or accepted" --
 * is about the pair, so this merges every strand on the counterpart back
 * into one reading. Direction is deliberately ignored: which way the
 * packets went is what the drawn arrow already says.
 *
 * A counterpart the buffer never saw is not an error, just an empty
 * summary -- an absence of ours is never reported as a fact about the
 * network. */
export function reachLineSummary(strands: ReachStrand[], counterpart: string): ReachLineSummary {
  const ports = new Map<string, ReachLinePort>()
  let tcp = 0
  let udp = 0
  let other = 0
  let accepted = 0
  let dropped = 0
  let refusedBy: string | undefined
  // The busiest refusal wins where a line was refused in both directions,
  // so the card names one rule and always the same one.
  let refusedFrom = -1

  for (const s of strands) {
    if (s.counterpart !== counterpart) continue
    const blocked = s.outcome === 'blocked'
    if (blocked) {
      dropped += s.count
      if (s.refusedBy !== undefined && s.count > refusedFrom) {
        refusedBy = s.refusedBy
        refusedFrom = s.count
      }
    } else {
      accepted += s.count
    }

    for (const h of s.protoHits ?? []) {
      if (h.proto === 'tcp') tcp += h.n
      else if (h.proto === 'udp') udp += h.n
      else other += h.n
      // Portless traffic counts toward the protocol split but has no port
      // row to sit in.
      if (h.port === null) continue
      const key = `${h.port}|${h.proto}`
      let p = ports.get(key)
      if (!p) {
        p = { port: h.port, proto: h.proto, accepted: 0, dropped: 0 }
        ports.set(key, p)
      }
      if (blocked) p.dropped += h.n
      else p.accepted += h.n
    }
  }

  const ranked = [...ports.values()].sort(
    (a, b) =>
      b.accepted + b.dropped - (a.accepted + a.dropped) ||
      a.port - b.port ||
      (a.proto < b.proto ? -1 : a.proto > b.proto ? 1 : 0),
  )

  return { counterpart, ports: ranked, tcp, udp, other, accepted, dropped, refusedBy }
}

/** The top few ports a strand carries, as the reach's own short form --
 * shared by the 2D membrane view and the city's standing view (#868) so
 * neither invents its own port wording. */
export function portsLine(ports: number[]): string {
  return ports
    .slice(0, 3)
    .map((p) => `:${p}`)
    .join(' ')
}
