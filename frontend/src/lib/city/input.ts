// SPDX-License-Identifier: AGPL-3.0-only
//
// What the city is built from (#863): the same zones, hosts and edges
// Topography.svelte already derives for the 2D stops, reduced to plain
// data so layout.ts stays pure and testable without the stores.
import { addressInCidr, parseCidr } from '../addressMatch'
import type { RouterFilterRule } from '../api'
import { boundaryCoverage, type Coverage } from '../coverageRule'
import type { PolicyEdge } from '../policy.svelte'
import type { RealityEdge } from '../reality'
import type { TunnelInterface } from '../tunnels.svelte'
import type { Device, FirewallEvent } from '../types'
import type { ZoneInfo } from '../zones.svelte'
import { gatesFromRules, type CityGate } from './gates'
import type { CityPeer } from './types'

export type { CityGate } from './gates'

export interface CityRouter {
  id: string
  name: string
  /** The device the 2D stops centre on: most recently seen, configured. */
  primary: boolean
  sourceIp: string
}

export interface CityZone {
  id: string
  name: string
  cidr: string | null
  hosts: { label: string; ip: string }[]
  hostCount: number
  eventCount: number
  /** The router this zone stands behind. */
  routerId: string
  /** How this boundary reads under the one coverage rule
   * (lib/coverageRule.ts): `logged` when something on it logs, `quiet`
   * when every remaining direction was declared intentionally quiet
   * (#392), `dark` when neither. Only ever a claim about a pushed
   * table: with nothing pushed at all it reads `quiet`, which is what
   * `dark: false` has always said here, and `rulesPushed` is the field
   * that says why. */
  coverage: Coverage
  /** Nothing logs on this boundary and nobody declared it quiet (a rule
   * table is pushed and no rule on it logs): the plate and its
   * buildings dim. Derived from `coverage` -- #1014, where reading the
   * policy edges alone left a declared boundary dark. */
  dark: boolean
}

export interface CityEdge {
  key: string
  from: string
  to: string
  events: number
  verdict: RealityEdge['verdict']
  /** Refused crossings on this pair, from the events -- the second key
   * in the escalation tie-break (escalate.ts), matching Topography's
   * own worst-unplanned card. */
  drops?: number
  /** The rule that refused traffic on this pair, from the events' own
   * label -- never invented when absent. */
  refusedBy?: string
}

/**
 * A tunnel interface, as the city's footbridge sees it: this build's own
 * event window plus whatever issue #874's ingest has pushed about it
 * (tunnelsState -- fetched from the same per-device RouterOS endpoints
 * the WAN and address tables already come from). apiState is null when
 * no pushed table names this interface at all -- this build only knows
 * it exists because events crossed it -- which tunnelState.ts's
 * bridgeStateFor reads exactly like the API's own 'unknown': a footbridge
 * with no state, never a guessed down.
 */
export interface CityTunnel {
  iface: string
  /** Which router's own endpoint this tunnel's state came from, or the
   * event-attributed router when it has none (see routerOf below). */
  routerId: string
  apiState: 'up' | 'down' | 'unknown' | null
  /** Events on this interface in the window, for the frontend's own
   * quiet reading (bridgeStateFor). */
  events: number
  peers: CityPeer[]
}

export interface CityInput {
  routers: CityRouter[]
  zones: CityZone[]
  edges: CityEdge[]
  /** The WAN boundary interface, or null while degraded. */
  wan: string | null
  /** Whether a logging rule covers the WAN boundary: the road bridge is
   * lamped when true, unlit otherwise -- never up/down/quiet, per the
   * ratified record. */
  wanLogged: boolean
  /** Every tunnel interface this build knows about, from events and/or
   * the pushed tunnel tables: each gets a footbridge. */
  tunnels: CityTunnel[]
  /** Every gate the pushed rule tables actually open -- [] whenever
   * rulesPushed is false, so "nothing pushed" and "pushed with nothing
   * accepting" are never confused with each other downstream (#865). */
  gates: CityGate[]
  /** Whether any router has ever pushed a rule table at all -- distinct
   * from a zone being dark (a table WAS pushed and nothing on it logs).
   * The wall's own "says why" reads this. */
  rulesPushed: boolean
}

/** Interface names that are tunnels, not zones: the far side is another
 * site, so the road crosses the river. */
export const TUNNEL_RE = /^(wg|wireguard|l2tp|pptp|sstp|ovpn|ipsec|gre|eoip|zerotier|vxlan)/i

export const isTunnel = (iface: string): boolean => TUNNEL_RE.test(iface)

/**
 * cityInputFrom reduces the stores' shapes to the city's. A zone's
 * router is the device that logged most events on that interface; a
 * zone nobody logged on belongs to the primary router. The router of a
 * second borough is the device whose source address sits inside a
 * primary-borough zone's CIDR (layout.ts draws that as the link road).
 */
export function cityInputFrom(
  devices: Device[],
  zones: ZoneInfo[],
  events: FirewallEvent[],
  edges: RealityEdge[],
  policyEdges: PolicyEdge[],
  anyPushed: boolean,
  primaryId: string | null,
  wan: string | null,
  /** Issue #874's per-device tunnel tables (tunnelsState.list). Defaults
   * to none, so every existing caller and test that predates #866 still
   * reads as "nothing pushed yet" rather than needing an update just to
   * keep compiling. */
  tunnelInterfaces: TunnelInterface[] = [],
  /** The pushed filter tables across every device (policyState.pushed) --
   * #865's own gates. Defaults to none for the same reason
   * tunnelInterfaces does: every caller that predates walls-and-gates
   * still compiles and reads as "nothing pushed yet". */
  rules: RouterFilterRule[] = [],
  /** The boundary-directions an admin has declared intentionally quiet
   * (#392 -- coverageState.byKey's keys), so a district can tell a
   * declared silence from an unexplained one (#1014). Defaults to none,
   * which is also the honest reading while the store cannot be read:
   * dark stays dark. */
  quietKeys: ReadonlySet<string> = new Set(),
): CityInput {
  let primary = primaryId ?? devices[0]?.id ?? ''
  const routers: CityRouter[] = devices.map((d) => ({ id: d.id, name: d.name, primary: d.id === primary, sourceIp: d.sourceIp }))
  if (routers.length === 0) {
    // No device known yet (the empty state, or a test that only pushes
    // an address table and events): `primary` must name the fallback
    // router below, or every zone's routerOf(...) resolves to an id no
    // router actually has and placeBorough's own filter drops every
    // zone -- a real district-count-zero bug, not merely a test gap.
    primary = 'router'
    routers.push({ id: primary, name: 'router', primary: true, sourceIp: '' })
  }

  // Who logs on which interface, and how many events crossed a tunnel
  // in the window (the frontend's own half of a footbridge's state).
  const byIface = new Map<string, Map<string, number>>()
  const tunnelNames = new Set<string>()
  const tunnelEvents = new Map<string, number>()
  for (const e of events) {
    for (const iface of [e.inInterface, e.outInterface]) {
      if (!iface) continue
      if (isTunnel(iface)) {
        tunnelNames.add(iface)
        tunnelEvents.set(iface, (tunnelEvents.get(iface) ?? 0) + 1)
      }
      let m = byIface.get(iface)
      if (!m) byIface.set(iface, (m = new Map()))
      m.set(e.deviceId, (m.get(e.deviceId) ?? 0) + 1)
    }
  }
  const routerOf = (iface: string): string => {
    const m = byIface.get(iface)
    if (!m) return primary
    let best = primary
    let n = -1
    for (const [id, k] of m) if (k > n && routers.some((r) => r.id === id)) (best = id), (n = k)
    return best
  }

  const logged = new Set<string>()
  for (const p of policyEdges) if (p.logged) (logged.add(p.from), logged.add(p.to))
  const wanLogged = wan !== null && logged.has(wan)

  // A lane's three-way reading, the one rule both the city plaque and
  // the 2D zones card draw from (#1014). No table pushed at all is not
  // a claim about any boundary -- there is nothing to read as dark, and
  // rulesPushed below is what says so -- so it reads quiet rather than
  // accusing every lane of a hole.
  const coverageOfZone = (iface: string): Coverage => (anyPushed ? boundaryCoverage(iface, policyEdges, quietKeys) : 'quiet')

  // A tunnel this build knows about either from its own events or from
  // a device's pushed tunnel table -- the first API entry to name it
  // wins when two devices happen to share an interface name, the same
  // single-boundary simplification zonesState.wanInterface already
  // documents for the WAN (splitting per device is #852's territory,
  // not this build's).
  const apiByIface = new Map<string, TunnelInterface>()
  for (const t of tunnelInterfaces) if (!apiByIface.has(t.iface)) apiByIface.set(t.iface, t)
  const allTunnelIfaces = new Set<string>([...tunnelNames, ...apiByIface.keys()])
  const cityTunnels: CityTunnel[] = [...allTunnelIfaces].sort().map((iface) => {
    const api = apiByIface.get(iface) ?? null
    return {
      iface,
      routerId: api?.routerId ?? routerOf(iface),
      apiState: api?.apiState ?? null,
      events: tunnelEvents.get(iface) ?? 0,
      peers: api?.peers ?? [],
    }
  })

  const cityZones: CityZone[] = zones
    .filter((z) => !isTunnel(z.id))
    .map((z) => {
      const coverage = coverageOfZone(z.id)
      return {
        id: z.id,
        name: z.name,
        cidr: z.cidr,
        hosts: z.hosts,
        hostCount: z.hostCount,
        eventCount: z.eventCount,
        routerId: routerOf(z.id),
        coverage,
        dark: coverage === 'dark',
      }
    })

  return {
    routers,
    zones: cityZones,
    edges: edges.map((e) => ({ key: e.key, from: e.from, to: e.to, events: e.events, verdict: e.verdict, drops: e.drops, refusedBy: e.refusedBy })),
    wan,
    wanLogged,
    tunnels: cityTunnels,
    gates: anyPushed ? gatesFromRules(rules) : [],
    rulesPushed: anyPushed,
  }
}

/** The primary-borough zone whose CIDR holds the address, if any. */
export function zoneHolding(zones: CityZone[], ip: string): CityZone | null {
  for (const z of zones) {
    if (!z.cidr) continue
    const c = parseCidr(z.cidr)
    if (c && addressInCidr(ip, c)) return z
  }
  return null
}
