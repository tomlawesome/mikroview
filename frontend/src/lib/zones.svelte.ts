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
import { fetchRouterAddresses, type RouterIPAddress } from './api'
import { isPublicIp } from './format'

export interface ZoneInfo {
  /** The boundary interface this zone stands on (bridge1, ether5, ...). */
  id: string
  /** Zone name -- the boundary name until the address push provides one. */
  name: string
  /** CIDR from the pushed /ip address table; null while degraded. */
  cidr: string | null
  /** A few observed hosts, most-talkative first -- the label is the
   * friendly name where the events carry one, and the ip is what the
   * reach recentres on. */
  hosts: { label: string; ip: string }[]
  hostCount: number
  eventCount: number
}

class ZonesState {
  /** The pushed /ip address table, across every device that has pushed
   * one (#627's own new kind). Empty until a push arrives -- which is
   * exactly the degraded state below. */
  pushed = $state<RouterIPAddress[]>([])

  async refresh() {
    const all: RouterIPAddress[] = []
    for (const d of appState.devices) {
      try {
        const res = await fetchRouterAddresses(d.id)
        if (res.available) all.push(...res.rules)
      } catch {
        // Absence is the degraded state, already honest on the map.
      }
    }
    this.pushed = all
  }

  /**
   * Per device: the interface whose inbound events most often carry a
   * public source, computed from that device's own events alone -- an
   * observation, not a probe. A device with nothing public inbound yet
   * has no entry here, not a guessed one (#850: a second router's real
   * WAN used to be counted against the first's and lose).
   */
  deviceWans = $derived.by((): Map<string, { iface: string; count: number }> => {
    const perDevice = new Map<string, Map<string, number>>()
    for (const e of appState.events) {
      if (!e.inInterface || !isPublicIp(e.srcIp)) continue
      let publicIn = perDevice.get(e.deviceId)
      if (!publicIn) {
        publicIn = new Map()
        perDevice.set(e.deviceId, publicIn)
      }
      publicIn.set(e.inInterface, (publicIn.get(e.inInterface) ?? 0) + 1)
    }
    const wans = new Map<string, { iface: string; count: number }>()
    for (const [deviceId, publicIn] of perDevice) {
      let best: string | null = null
      let bestN = 0
      for (const [iface, n] of publicIn) {
        if (n > bestN) {
          best = iface
          bestN = n
        }
      }
      if (best) wans.set(deviceId, { iface: best, count: bestN })
    }
    return wans
  })

  /** Every device's WAN interface name -- excluded from the lane row
   * regardless of which device it belongs to (#850). Two devices whose
   * WAN happens to share a name still collapse to one entry here, same
   * as they always have in the lane row; keying zones by device is #852. */
  wanInterfaces = $derived.by((): Set<string> => new Set([...this.deviceWans.values()].map((w) => w.iface)))

  /**
   * The estate's internet-facing boundary, for callers that still want
   * one name: the busiest device's own WAN (most public inbound
   * events), an observation, not a probe -- and null until any device
   * has one. Existing callers (Topography.svelte's internet anchor,
   * coverage keys, the WAN card, reach) keep this single-boundary
   * notion for now; splitting it per device is #852.
   */
  wanInterface = $derived.by((): string | null => {
    let best: string | null = null
    let bestN = 0
    for (const { iface, count } of this.deviceWans.values()) {
      if (count > bestN) {
        best = iface
        bestN = count
      }
    }
    return best
  })

  /** The lanes: every observed non-wan boundary, busiest first, capped
   * at five (the map is spare by design; a sixth lane is a design
   * question, not a rendering one). */
  zones = $derived.by((): ZoneInfo[] => {
    const wans = this.wanInterfaces
    const byIface = new Map<string, { count: number; hosts: Map<string, { label: string; n: number }> }>()
    for (const e of appState.events) {
      for (const iface of [e.inInterface, e.outInterface]) {
        if (!iface || wans.has(iface)) continue
        let z = byIface.get(iface)
        if (!z) {
          z = { count: 0, hosts: new Map() }
          byIface.set(iface, z)
        }
        z.count++
        // The host that stands on this boundary is the private side of
        // the event relative to it: the source when traffic enters here.
        if (iface === e.inInterface && e.srcIp && !isPublicIp(e.srcIp)) {
          const h = z.hosts.get(e.srcIp)
          if (h) {
            h.n++
            if (e.srcHostName) h.label = e.srcHostName
          } else {
            z.hosts.set(e.srcIp, { label: e.srcHostName ?? e.srcIp, n: 1 })
          }
        }
      }
    }
    // The pushed table is authoritative for naming: every interface it
    // covers is a zone even before anything has spoken on it -- the map
    // draws config, not just traffic.
    const byPush = new Map<string, RouterIPAddress>()
    for (const a of this.pushed) {
      if (a.interface && !wans.has(a.interface)) byPush.set(a.interface, a)
    }
    const ifaces = new Set([...byPush.keys(), ...byIface.keys()])
    return [...ifaces]
      .map((iface) => {
        const ev = byIface.get(iface)
        const push = byPush.get(iface)
        return {
          id: iface,
          name: push?.comment || iface,
          cidr: push?.address ?? null,
          hosts: ev
            ? [...ev.hosts.entries()].sort((a, b) => b[1].n - a[1].n).map(([ip, h]) => ({ label: h.label, ip }))
            : [],
          hostCount: ev?.hosts.size ?? 0,
          eventCount: ev?.count ?? 0,
        }
      })
      .sort((a, b) => b.eventCount - a.eventCount)
      .slice(0, 5)
  })

  /** True while zone naming rests on boundaries alone -- the caption
   * that names the missing push renders from this. */
  degraded = $derived(this.pushed.length === 0)
}

export const zonesState = new ZonesState()
