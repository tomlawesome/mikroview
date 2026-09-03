// SPDX-License-Identifier: AGPL-3.0-only
//
// What the city is built from (#863): the same zones, hosts and edges
// Topography.svelte already derives for the 2D stops, reduced to plain
// data so layout.ts stays pure and testable without the stores.
import { addressInCidr, parseCidr } from '../addressMatch'
import type { PolicyEdge } from '../policy.svelte'
import type { RealityEdge } from '../reality'
import type { Device, FirewallEvent } from '../types'
import type { ZoneInfo } from '../zones.svelte'

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
  /** Nothing logs on this boundary (a rule table is pushed and no rule
   * on it logs): the plate and its buildings dim. */
  dark: boolean
}

export interface CityEdge {
  key: string
  from: string
  to: string
  events: number
  verdict: RealityEdge['verdict']
}

export interface CityInput {
  routers: CityRouter[]
  zones: CityZone[]
  edges: CityEdge[]
  /** The WAN boundary interface, or null while degraded. */
  wan: string | null
  /** Tunnel interfaces seen on events: each gets a footbridge. */
  tunnels: string[]
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
): CityInput {
  const primary = primaryId ?? devices[0]?.id ?? ''
  const routers: CityRouter[] = devices.map((d) => ({ id: d.id, name: d.name, primary: d.id === primary, sourceIp: d.sourceIp }))
  if (routers.length === 0) routers.push({ id: 'router', name: 'router', primary: true, sourceIp: '' })

  // Who logs on which interface.
  const byIface = new Map<string, Map<string, number>>()
  const tunnels = new Set<string>()
  for (const e of events) {
    for (const iface of [e.inInterface, e.outInterface]) {
      if (!iface) continue
      if (isTunnel(iface)) tunnels.add(iface)
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

  const cityZones: CityZone[] = zones
    .filter((z) => !isTunnel(z.id))
    .map((z) => ({
      id: z.id,
      name: z.name,
      cidr: z.cidr,
      hosts: z.hosts,
      hostCount: z.hostCount,
      eventCount: z.eventCount,
      routerId: routerOf(z.id),
      dark: anyPushed && !logged.has(z.id),
    }))

  return {
    routers,
    zones: cityZones,
    edges: edges.map((e) => ({ key: e.key, from: e.from, to: e.to, events: e.events, verdict: e.verdict })),
    wan,
    tunnels: [...tunnels].sort(),
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
