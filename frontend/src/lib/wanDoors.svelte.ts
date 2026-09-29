// SPDX-License-Identifier: AGPL-3.0-only

import { fetchWanDoors, type WanDeviceDoors } from './api'
import { zonesState } from './zones.svelte'
import { appState } from './state.svelte'

// portUnion is every concrete port a device's doors and pushed services
// together name, ascending -- the printed nmap check's own port list
// (#1319's note: "Ports = the union of the device's door ports").
// Ranges are skipped, the same "a rule table is bounded, traffic is not"
// reading api/ports.go's namedPorts gives the same field: a range names
// many ports and none of them is a single figure worth printing.
export function portUnion(d: WanDeviceDoors): number[] {
  const ports = new Set<number>()
  const add = (spec: string) => {
    for (const part of spec.split(',')) {
      const t = part.trim()
      if (t === '' || t.includes('-')) continue
      const n = Number(t)
      if (Number.isInteger(n) && n >= 1 && n <= 65535) ports.add(n)
    }
  }
  for (const door of d.doors) add(door.dstPort)
  for (const svc of d.services ?? []) if (svc.port >= 1 && svc.port <= 65535) ports.add(svc.port)
  return [...ports].sort((a, b) => a - b)
}

// The doors panel's own state (#1319): a right-hand sheet modelled on
// dossier.svelte.ts's singleton-plus-trigger shape, with one thing
// dossier does not need -- a standing answer the boundary card's own
// "Doors from the internet · N" row (and the internet anchor beside it)
// can read without the sheet being open. Topography's own device-arrived
// effect calls refresh() the same way it already does for
// zonesState/policyState; open()/close() only toggle the sheet over
// whatever refresh last found, and close() deliberately does not clear
// devices the way dossier's close() clears its data -- the count must
// survive the sheet closing.
class WanDoorsState {
  isOpen = $state(false)
  devices = $state<WanDeviceDoors[]>([])
  loading = $state(false)
  error = $state<string | null>(null)

  private trigger: HTMLElement | null = null
  private requestId = 0

  /** Every door and every enabled service, across every device --
   * what the boundary card's row and the internet anchor's own label
   * both count. */
  totalDoors = $derived.by(() =>
    this.devices.reduce((n, d) => n + d.doors.length + (d.services?.length ?? 0), 0),
  )

  /** Opens the panel. device names nothing currently open here for one
   * device in particular -- both entry points (the boundary card, the
   * internet anchor) are about the internet edge as a whole -- but the
   * fetch is already per-device, so a future per-device entry point
   * costs nothing new here; it would set this and the panel would
   * scroll to that section. */
  open(device?: string, trigger?: HTMLElement | null) {
    if (!this.isOpen) this.trigger = trigger ?? null
    this.isOpen = true
    void this.refresh()
    void device // reserved; see the doc comment above
  }

  close() {
    if (!this.isOpen) return
    this.isOpen = false
    const t = this.trigger
    this.trigger = null
    t?.focus?.()
  }

  /** Reads every device's own section fresh, from whichever WAN
   * interface zonesState.deviceWans currently observes for it. A device
   * with no observed WAN yet has no section -- an absent entry, never a
   * guessed one, the same reading deviceWans itself gives (#850). */
  async refresh() {
    const wans = zonesState.deviceWans
    if (wans.size === 0) {
      this.devices = []
      this.loading = false
      return
    }
    const id = ++this.requestId
    this.loading = true
    this.error = null
    try {
      const results = await Promise.all(
        [...wans.entries()].map(([deviceId, w]) => fetchWanDoors(deviceId, w.iface)),
      )
      if (id !== this.requestId) return
      const nameOf = new Map(appState.devices.map((d) => [d.id, d.name]))
      this.devices = results
        .flatMap((r) => r.devices)
        .map((d) => ({ ...d, name: d.name || nameOf.get(d.id) || '' }))
        .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
      this.loading = false
    } catch {
      if (id !== this.requestId) return
      this.error = 'Could not read the pushed tables'
      this.loading = false
    }
  }
}

export const wanDoorsState = new WanDoorsState()
