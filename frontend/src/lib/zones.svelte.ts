// SPDX-License-Identifier: AGPL-3.0-only
//
// The topography's zone model (#485/#627 layer 1). Until the /ip
// address table has been pushed, zones degrade honestly per the
// ratified record: boundary-derived names (the interfaces the events
// themselves carry), no CIDR, and the map states which push is missing.
// Hosts per zone come from what was actually observed on that boundary
// -- names where the events carry them, never guessed.
//
// Everything here is $derived from the live event buffer: the map draws
// itself as traffic arrives, and an empty buffer yields an empty list
// (the scene's own empty state, not an error).
import { appState } from './state.svelte'
import { isPublicIp } from './format'

export interface ZoneInfo {
  /** The boundary interface this zone stands on (bridge1, ether5, ...). */
  id: string
  /** Zone name -- the boundary name until the address push provides one. */
  name: string
  /** CIDR from the pushed /ip address table; null while degraded. */
  cidr: string | null
  /** A few observed host names (or IPs), most-talkative first. */
  hosts: string[]
  hostCount: number
  eventCount: number
}

class ZonesState {
  /**
   * The interface that faces the internet: the one whose inbound events
   * most often carry a public source. An observation, not a probe --
   * and null until anything public has actually arrived.
   */
  wanInterface = $derived.by((): string | null => {
    const publicIn = new Map<string, number>()
    for (const e of appState.events) {
      if (e.inInterface && isPublicIp(e.srcIp)) {
        publicIn.set(e.inInterface, (publicIn.get(e.inInterface) ?? 0) + 1)
      }
    }
    let best: string | null = null
    let bestN = 0
    for (const [iface, n] of publicIn) {
      if (n > bestN) {
        best = iface
        bestN = n
      }
    }
    return best
  })

  /** The lanes: every observed non-wan boundary, busiest first, capped
   * at five (the map is spare by design; a sixth lane is a design
   * question, not a rendering one). */
  zones = $derived.by((): ZoneInfo[] => {
    const wan = this.wanInterface
    const byIface = new Map<string, { count: number; hosts: Map<string, number> }>()
    for (const e of appState.events) {
      for (const iface of [e.inInterface, e.outInterface]) {
        if (!iface || iface === wan) continue
        let z = byIface.get(iface)
        if (!z) {
          z = { count: 0, hosts: new Map() }
          byIface.set(iface, z)
        }
        z.count++
        // The host that stands on this boundary is the private side of
        // the event relative to it: the source when traffic enters here.
        if (iface === e.inInterface && e.srcIp && !isPublicIp(e.srcIp)) {
          const label = e.srcHostName ?? e.srcIp
          z.hosts.set(label, (z.hosts.get(label) ?? 0) + 1)
        }
      }
    }
    return [...byIface.entries()]
      .sort((a, b) => b[1].count - a[1].count)
      .slice(0, 5)
      .map(([iface, z]) => ({
        id: iface,
        name: iface,
        cidr: null,
        hosts: [...z.hosts.entries()].sort((a, b) => b[1] - a[1]).map(([h]) => h),
        hostCount: z.hosts.size,
        eventCount: z.count,
      }))
  })

  /** True while zone naming rests on boundaries alone -- the caption
   * that names the missing push renders from this. Becomes false once
   * the pushed /ip address table feeds real names and CIDRs in. */
  degraded = $derived(this.zones.every((z) => z.cidr === null))
}

export const zonesState = new ZonesState()
