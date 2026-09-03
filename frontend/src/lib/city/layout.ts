// SPDX-License-Identifier: AGPL-3.0-only
//
// The ground plan (#863): zones become plates, hosts become buildings on
// them, routers become nodes, a second router's zones become a borough
// down a road, the WAN is a river along the north edge with a bridge
// for every way out of town. Coordinates and radii are the round-40
// mockup's (docs/design/concepts/round-40/isometric.html), so the town
// it drew is the town this draws when given the same estate.
//
// Pure: CityInput in, Ground out. Nothing here knows about the camera.
import type { Pt } from './project'
import { bezAt, bulge, gateToward, routeRound, segsOf } from './roads'
import type { CityEdge, CityInput, CityZone } from './input'
import { zoneHolding } from './input'
import { bridgeStateFor } from './tunnelState'
import type { Borough, Bridge, Building, CityPeer, District, Ground, River, Road, RoadKind } from './types'

/** The mockup's primary router. */
const ROUTER: Pt = [-10, 10]
const ROUTER_R = 7.5
const ROUTER_H = 9
const SECOND_R = 6.2
const SECOND_H = 5
const HOST_R = 4.4
const POST_R = 3.4
const FOOT_POST_R = 3.0
/** Buildings drawn per plate; the rest is the plate's `more`. */
export const MAX_BUILDINGS = 8

/** District centres relative to the primary router: lan, srv, iot,
 * guest, and a fifth south-east where there is room. */
const PRIMARY_SLOTS: Pt[] = [
  [-66, 26],
  [-8, 62],
  [56, 20],
  [76, -24],
  [50, 66],
]
/** A borough's districts relative to its own router, in a row. */
const BOROUGH_SLOTS: Pt[] = [
  [-44, 20],
  [44, 20],
  [-88, 26],
  [88, 26],
  [0, 44],
]

/** The mockup's river, before it is moved clear of the town. */
export const BANK_N: Pt[] = [
  [-170, 12],
  [-120, -8],
  [-80, -26],
  [-30, -42],
  [20, -52],
  [70, -58],
  [140, -68],
]
export const RIVER_W = 24

/** Plate radius from how many hosts stand on it. */
export const plateRadius = (hostCount: number): number => Math.max(13, Math.min(21, 11 + 2.5 * hostCount))

/** sampleBank flattens a bank's curve so anything can ask "where is the
 * water at this u?" */
function sampleBank(pts: Pt[]): Pt[] {
  const out: Pt[] = []
  for (const s of segsOf(pts)) for (let i = 0; i <= 24; i++) out.push(bezAt(s, i / 24))
  return out
}

export function bankV(sampled: Pt[], u: number): number {
  for (let i = 1; i < sampled.length; i++) {
    if (sampled[i][0] >= u) {
      const t = (u - sampled[i - 1][0]) / (sampled[i][0] - sampled[i - 1][0] || 1)
      return sampled[i - 1][1] + (sampled[i][1] - sampled[i - 1][1]) * t
    }
  }
  return sampled[sampled.length - 1][1]
}

/** Buildings on a plate: one at the centre, otherwise evenly round a
 * ring inside the wall, busiest first from the top. */
function placeBuildings(z: CityZone, d: { id: string; u: number; v: number; r: number }, routerId: string): Building[] {
  const hosts = z.hosts.slice(0, MAX_BUILDINGS)
  const n = hosts.length
  const rho = d.r - HOST_R - 4
  return hosts.map((h, i) => {
    let du = 0
    let dv = 0
    if (n > 1) {
      const th = -Math.PI / 2 + (i / n) * Math.PI * 2
      const cx = Math.cos(th)
      const sy = Math.sin(th)
      const s = Math.abs(cx) + Math.abs(sy)
      du = (cx / s) * rho
      dv = (sy / s) * rho
    }
    const hgt = n > 1 ? 4 - 3 * (i / (n - 1)) : 3
    return {
      id: d.id + '/' + h.ip,
      name: h.label,
      ip: h.ip,
      kind: 'host',
      u: d.u + du,
      v: d.v + dv,
      R: HOST_R,
      h: hgt,
      districtId: d.id,
      routerId,
      index: i,
    }
  })
}

const VERDICT_KIND: Record<CityEdge['verdict'], RoadKind> = {
  planned: 'a',
  holding: 'd',
  unplanned: 'x',
  unjudged: 'q',
}

export const roadWidth = (events: number): number => Math.max(0.7, Math.min(2.8, 0.7 + Math.log2(1 + events) * 0.35))

/**
 * layoutGround lays the estate out. Districts are placed per borough
 * from fixed slots -- the mockup's, chosen so no two plates overlap --
 * and a borough after the first is placed south of everything before
 * it. Roads are then routed between the gates the edges name.
 */
export function layoutGround(input: CityInput): Ground {
  const primary = input.routers.find((r) => r.primary) ?? input.routers[0]
  const others = input.routers.filter((r) => r !== primary)
  const districts: District[] = []
  const nodes: Building[] = []
  const boroughs: Borough[] = []
  const primaryNode: Building = {
    id: primary.id,
    name: primary.name,
    ip: primary.sourceIp,
    kind: 'router',
    u: ROUTER[0],
    v: ROUTER[1],
    R: ROUTER_R,
    h: ROUTER_H,
    districtId: null,
    routerId: primary.id,
    index: 0,
  }
  nodes.push(primaryNode)

  let inkIndex = 0
  const placeBorough = (routerId: string, name: string, at: Pt, slots: Pt[]): Borough => {
    const zones = input.zones.filter((z) => z.routerId === routerId).slice(0, slots.length)
    const ids: string[] = []
    let u0 = at[0] - 12
    let u1 = at[0] + 12
    let v0 = at[1] - 12
    let v1 = at[1] + 12
    zones.forEach((z, i) => {
      const r = plateRadius(z.hostCount)
      const u = at[0] + slots[i][0]
      const v = at[1] + slots[i][1]
      const d: District = {
        id: z.id,
        name: z.name,
        cidr: z.cidr,
        u,
        v,
        r,
        ink: inkIndex++,
        routerId,
        dark: z.dark,
        buildings: [],
        more: Math.max(0, z.hostCount - Math.min(z.hosts.length, MAX_BUILDINGS)),
      }
      d.buildings = placeBuildings(z, d, routerId)
      districts.push(d)
      ids.push(d.id)
      u0 = Math.min(u0, u - r)
      u1 = Math.max(u1, u + r)
      v0 = Math.min(v0, v - r)
      v1 = Math.max(v1, v + r)
    })
    const b: Borough = { routerId, name, districtIds: ids, bounds: { u0, u1, v0, v1 } }
    boroughs.push(b)
    return b
  }

  placeBorough(primary.id, primary.name, ROUTER, PRIMARY_SLOTS)

  // Boroughs after the first, each south of everything so far.
  const links: { node: Building; host: District | null }[] = []
  for (const r of others) {
    const south = Math.max(...districts.map((d) => d.v + d.r), ...nodes.map((n) => n.v + n.R))
    const at: Pt = [14, south + 24]
    const node: Building = {
      id: r.id,
      name: r.name,
      ip: r.sourceIp,
      kind: 'router-ant',
      u: at[0],
      v: at[1],
      R: SECOND_R,
      h: SECOND_H,
      districtId: null,
      routerId: r.id,
      index: 0,
    }
    nodes.push(node)
    placeBorough(r.id, r.name, at, BOROUGH_SLOTS)
    const hz = zoneHolding(
      input.zones.filter((z) => z.routerId === primary.id),
      r.sourceIp,
    )
    links.push({ node, host: hz ? (districts.find((d) => d.id === hz.id) ?? null) : null })
  }

  // The river: the mockup's bank, moved north if the town's top plate
  // would otherwise sit in the water.
  let shift = 0
  const bankN0 = sampleBank(BANK_N)
  for (const d of districts) shift = Math.min(shift, d.v - d.r - 12 - bankV(bankN0, d.u))
  for (const n of nodes) shift = Math.min(shift, n.v - n.R - 12 - bankV(bankN0, n.u))
  const bankN = BANK_N.map((p): Pt => [p[0], p[1] + shift])
  const bankF = bankN.map((p): Pt => [p[0], p[1] - RIVER_W])
  const river: River = { bankN, bankF, width: RIVER_W }
  const sampledN = sampleBank(bankN)
  const sampledF = sampleBank(bankF)

  // Bridges: the WAN west of the router, tunnels east of it. Each deck
  // runs on the d1 diagonal: solve tv - d = farBank(at - d). The road
  // bridge's state is never up/down/quiet -- 'up' means lamped (a
  // logging rule covers the boundary), 'unknown' means unlit; a
  // footbridge's state is tunnelState.ts's bridgeStateFor, never guessed.
  const bridges: Bridge[] = []
  const crossings: { id: string; iface: string; at: number; w: number; kind: Bridge['kind']; postR: number; state: Bridge['state']; peers: CityPeer[] }[] = []
  if (input.wan) crossings.push({ id: 'wan', iface: input.wan, at: ROUTER[0] - 46, w: 3.4, kind: 'road', postR: POST_R, state: input.wanLogged ? 'up' : 'unknown', peers: [] })
  input.tunnels.forEach((t, i) =>
    crossings.push({ id: t.iface, iface: t.iface, at: ROUTER[0] + 18 + i * 28, w: 1.5, kind: 'foot', postR: FOOT_POST_R, state: bridgeStateFor(t.apiState, t.events), peers: t.peers }),
  )
  for (const c of crossings) {
    const tv = bankV(sampledN, c.at)
    let lo = 4
    let hi = 90
    for (let i = 0; i < 44; i++) {
      const mid = (lo + hi) / 2
      if (tv - mid < bankV(sampledF, c.at - mid)) hi = mid
      else lo = mid
    }
    const d = (lo + hi) / 2
    const t: Pt = [c.at, tv]
    const f: Pt = [c.at - d, tv - d]
    const post: Building = {
      id: 'post:' + c.iface,
      name: c.iface,
      ip: c.kind === 'road' ? 'wan bridge' : 'tunnel bridge',
      kind: 'post',
      u: t[0] + 3.4,
      v: t[1] + 3.4,
      R: c.postR,
      h: c.kind === 'road' ? 2.6 : 2.0,
      districtId: null,
      routerId: primary.id,
      index: 0,
    }
    nodes.push(post)
    bridges.push({ id: c.id, iface: c.iface, kind: c.kind, t, f, mid: [(t[0] + f[0]) / 2, (t[1] + f[1]) / 2], half: d / 2, w: c.w, post: post.id, state: c.state, peers: c.peers })
  }

  // Roads. A gate is where a road toward its far end crosses a plate's
  // edge; the point just outside makes the crossing square-on.
  const roads: Road[] = []
  const byId = new Map<string, District>()
  for (const d of districts) byId.set(d.id, d)
  const postFor = (iface: string) => nodes.find((n) => n.id === 'post:' + iface) ?? null
  const routerNode = (id: string) => nodes.find((n) => n.id === id) ?? primaryNode

  type End = { kind: 'district'; d: District } | { kind: 'node'; n: Building }
  const endOf = (iface: string): End | null => {
    const d = byId.get(iface)
    if (d) return { kind: 'district', d }
    const p = postFor(iface)
    if (p) return { kind: 'node', n: p }
    return null
  }
  const centre = (e: End): Pt => (e.kind === 'district' ? [e.d.u, e.d.v] : [e.n.u, e.n.v])

  const connect = (id: string, a: End, b: End, w: number, k: RoadKind, label: string, stop?: 'drop') => {
    const exempt = new Set<string>()
    let pts: Pt[] = []
    let from: string | null = null
    let to: string | null = null
    // Both ends first, so each gate faces the other's centre.
    const ca = centre(a)
    const cb = centre(b)
    if (a.kind === 'district') {
      const g = gateToward(a.d, cb)
      pts.push(g.p, g.out)
      exempt.add(a.d.id)
    } else {
      pts.push(ca)
      from = a.n.id
    }
    if (b.kind === 'district') {
      const g = gateToward(b.d, ca)
      pts.push(g.out, g.p)
      exempt.add(b.d.id)
    } else {
      pts.push(cb)
      to = b.n.id
    }
    // Free waypoints only: route round plates, then guarantee a bend.
    const head = a.kind === 'district' ? 2 : 1
    const tail = b.kind === 'district' ? 2 : 1
    const inner = routeRound(pts.slice(head - 1, pts.length - tail + 1), districts, { exempt })
    const bent = bulge(inner, id)
    pts = [...pts.slice(0, head - 1), ...bent, ...pts.slice(pts.length - tail + 1)]
    roads.push({ id, pts, w, k, from, to, stop, label })
  }

  // One road per pair whichever way the traffic runs: both directions
  // fold into it, and the worse verdict names it.
  const RANK: Record<CityEdge['verdict'], number> = { unjudged: 0, planned: 1, holding: 2, unplanned: 3 }
  const pairs = new Map<string, CityEdge>()
  for (const e of input.edges) {
    if (e.from === e.to) continue
    const key = [e.from, e.to].sort().join('|')
    const had = pairs.get(key)
    if (!had) pairs.set(key, { ...e, key })
    else pairs.set(key, { ...had, events: had.events + e.events, verdict: RANK[e.verdict] > RANK[had.verdict] ? e.verdict : had.verdict })
  }
  // A zone's road to the WAN runs to its router (the bridge road takes
  // it on from there); any other pair runs gate to gate.
  const isWan = (i: string) => input.wan !== null && i === input.wan
  for (const e of pairs.values()) {
    const k = VERDICT_KIND[e.verdict]
    const w = roadWidth(e.events)
    const stop = k === 'd' ? 'drop' : undefined
    const label = e.from + ' to ' + e.to + ', ' + e.events + ' events, ' + e.verdict
    if (isWan(e.from) || isWan(e.to)) {
      const end = endOf(isWan(e.from) ? e.to : e.from)
      if (!end || end.kind !== 'district') continue
      connect(e.key, end, { kind: 'node', n: routerNode(end.d.routerId) }, w, k, label, stop)
      continue
    }
    const a = endOf(e.from)
    const b = endOf(e.to)
    if (!a || !b) continue
    connect(e.key, a, b, w, k, label, stop)
  }
  // Router to each bridge head, then the crossing and what is beyond it.
  for (const b of bridges) {
    const post = nodes.find((n) => n.id === b.post)
    if (!post) continue
    const w = b.kind === 'road' ? 2.8 : 0.8
    const k: RoadKind = b.state === 'up' ? 'a' : 'q'
    connect('rb-' + b.id, { kind: 'node', n: primaryNode }, { kind: 'node', n: post }, w, k, primary.name + ' to ' + b.iface)
    const off = (du: number, dv: number): Pt => [b.f[0] + du, b.f[1] + dv]
    roads.push({
      id: b.id + '-span',
      pts: b.kind === 'road' ? [b.t, b.f, off(-26, -17), off(-58, -25), off(-98, -33)] : [b.t, b.f, off(-17, -6)],
      w: b.kind === 'road' ? 2.6 : 0.7,
      k,
      from: null,
      to: null,
      fade: true,
      label: b.iface + ' across the river',
    })
  }
  // The link road from a second router to the district that holds its
  // address, or to the primary router when no plate does.
  for (const l of links) {
    const to: End = l.host ? { kind: 'district', d: l.host } : { kind: 'node', n: primaryNode }
    connect('link-' + l.node.id, { kind: 'node', n: l.node }, to, 1.6, 'a', l.node.name + ' to ' + (l.host ? l.host.name : primary.name))
  }
  // Lanes: each building's own street to the plate's edge nearest the
  // router (drawn at the district and street stops).
  for (const d of districts) {
    if (d.buildings.length < 2) continue
    const rn = routerNode(d.routerId)
    const g = gateToward(d, [rn.u, rn.v])
    for (const b of d.buildings) {
      const dx = g.p[0] - b.u
      const dy = g.p[1] - b.v
      const L = Math.hypot(dx, dy) || 1
      const side = b.index % 2 ? 1 : -1
      const mid: Pt = [(b.u + g.p[0]) / 2 + (-dy / L) * L * 0.18 * side, (b.v + g.p[1]) / 2 + (dx / L) * L * 0.18 * side]
      roads.push({ id: 'lane:' + b.id, pts: [[b.u, b.v], mid, g.p], w: 0.6, k: 'q', from: b.id, to: null, lane: true, label: b.name + ' lane' })
    }
  }

  // Bounds: everything, with the far bank and the highway's end.
  let u0 = Infinity
  let u1 = -Infinity
  let v0 = Infinity
  let v1 = -Infinity
  const grow = (u: number, v: number, r = 0) => {
    u0 = Math.min(u0, u - r)
    u1 = Math.max(u1, u + r)
    v0 = Math.min(v0, v - r)
    v1 = Math.max(v1, v + r)
  }
  for (const d of districts) grow(d.u, d.v, d.r)
  for (const n of nodes) grow(n.u, n.v, n.R)
  for (const r of roads) for (const p of r.pts) grow(p[0], p[1])
  for (const p of bankF) grow(p[0], p[1] - 10)
  if (!isFinite(u0)) grow(0, 0, 40)

  return { districts, nodes, boroughs, roads, river, bridges, bounds: { u0, u1, v0, v1 } }
}
