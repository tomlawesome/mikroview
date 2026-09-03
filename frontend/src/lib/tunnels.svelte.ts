// SPDX-License-Identifier: AGPL-3.0-only
//
// Tunnel state for the city's footbridges (#866), fed by issue #874's
// ingest side: per-device WireGuard interface/peer state and ppp-active
// sessions. Same shape as zones.svelte.ts's own refresh() over
// fetchRouterAddresses -- these are mikroview's own pushed tables, read
// per device, never a live probe (AGENTS.md: mikroview observes; it
// never scans or connects).
import { fetchRouterPPPActive, fetchRouterWireguard, type RouterPPPSession, type RouterWireguardPeer } from './api'
import { appState } from './state.svelte'
import type { CityPeer } from './city/types'

export type TunnelPeer = CityPeer

export interface TunnelInterface {
  iface: string
  routerId: string
  /** Which pushed table this came from. The city draws both kinds as
   * footbridges, so it never had to tell them apart; the 2D map's
   * tunnel node is WireGuard only (#877, round 30 draws no PPP node),
   * and a peer's own `kind` cannot answer it -- a WireGuard interface
   * with no peers pushed has an empty list. */
  kind: 'wg' | 'ppp'
  apiState: 'up' | 'down' | 'unknown'
  peers: TunnelPeer[]
}

/** A WireGuard peer's own name: an operator's comment first, then the
 * address it is allowed to use, then a short slice of its public key --
 * always something, never blank. */
function wgPeerName(p: RouterWireguardPeer): string {
  return p.comment || p.allowedAddress[0] || p.publicKey.slice(0, 12) || 'peer'
}

function wgPeerAddress(p: RouterWireguardPeer): string {
  return p.allowedAddress[0] ?? ''
}

class TunnelsState {
  /** Every device's tunnel interfaces, keyed by device id. Empty for a
   * device that has never pushed either table -- the degraded state a
   * tunnel seen only in events is drawn from. */
  byDevice = $state<Map<string, TunnelInterface[]>>(new Map())

  /** Flattened, for cityInputFrom -- it does not need to know this is
   * per-device. */
  list = $derived.by((): TunnelInterface[] => [...this.byDevice.values()].flat())

  async refresh() {
    const next = new Map<string, TunnelInterface[]>()
    for (const d of appState.devices) {
      const list: TunnelInterface[] = []
      try {
        const wg = await fetchRouterWireguard(d.id)
        for (const iface of wg.interfaces) {
          list.push({
            iface: iface.name,
            routerId: d.id,
            kind: 'wg',
            apiState: iface.state,
            peers: iface.peers.map((p, i) => ({
              id: iface.name + '/wg/' + (p.publicKey || String(i)),
              name: wgPeerName(p),
              address: wgPeerAddress(p),
              kind: 'wg',
            })),
          })
        }
      } catch {
        // Absence is the degraded state: this device's WireGuard
        // interfaces (if any) read as unknown until events name them.
      }
      try {
        const ppp = await fetchRouterPPPActive(d.id)
        for (const sess of ppp.sessions) {
          list.push({
            iface: sess.name,
            routerId: d.id,
            kind: 'ppp',
            // A row present in /ppp/active is itself the up signal
            // (#874) -- there is no separate "down but configured"
            // reading this endpoint can offer.
            apiState: 'up',
            peers: [pppPeer(sess)],
          })
        }
      } catch {
        // Same.
      }
      next.set(d.id, list)
    }
    this.byDevice = next
  }
}

function pppPeer(sess: RouterPPPSession): TunnelPeer {
  return {
    id: sess.name + '/ppp/' + (sess.address || sess.callerId || sess.service),
    name: sess.address || sess.callerId || sess.name,
    address: sess.address,
    kind: 'ppp',
  }
}

export const tunnelsState = new TunnelsState()
