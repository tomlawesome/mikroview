// SPDX-License-Identifier: AGPL-3.0-only
//
// Mounts Topography alone against a small hand-built estate (#979's
// default-stop review: what the 2D map looks like at its own top
// stop). Reached only through dev/topography-preview.html on the dev
// server; that page is not an input to `vite build` (index.html is),
// so nothing here ships.
//
// appState.devices stays empty on purpose -- the same convention
// Topography.svelte.test.ts documents: Topography's own $effect only
// reaches the network (zonesState.refresh etc.) once devices is
// non-empty, and this harness has no backend to answer it. Zones and
// traffic are driven directly instead, the same shape
// live-city-stops.mjs feeds a real instance (four lanes, one dark).
import { mount } from 'svelte'
import '../app.css'
import Topography from '../components/Topography.svelte'
import { appState } from '../lib/state.svelte'
import { zonesState } from '../lib/zones.svelte'
import { altitudeStopState } from '../lib/altitudeStop.svelte'
import { ALTITUDE_LABELS, type AltitudeLabel } from '../lib/altitude'
import type { ClientEvent } from '../lib/types'

appState.devices = []

zonesState.pushed = [
  { address: '10.10.0.1/24', network: '10.10.0.0', interface: 'bridge-lan', comment: 'LAN' },
  { address: '10.20.0.1/24', network: '10.20.0.0', interface: 'vlan-srv', comment: 'Servers' },
  { address: '10.30.0.1/24', network: '10.30.0.0', interface: 'vlan-iot', comment: 'IoT' },
  { address: '10.40.0.1/24', network: '10.40.0.0', interface: 'vlan-guest', comment: 'Guest' },
]

let nextId = 1
function event(overrides: Partial<ClientEvent>): ClientEvent {
  return {
    id: nextId++,
    time: '2026-09-06T12:00:00Z',
    deviceId: 'router1',
    sourceIp: '10.10.0.10',
    action: 'accept',
    ruleLabel: 'lan-out',
    chain: 'forward',
    raw: '',
    receivedAt: Date.now(),
    ...overrides,
  }
}

const LANES: [string, string, string, number][] = [
  ['bridge-lan', '10.10.0.', 'lan', 6],
  ['vlan-srv', '10.20.0.', 'srv', 4],
  ['vlan-iot', '10.30.0.', 'iot', 5],
  ['vlan-guest', '10.40.0.', 'guest', 1],
]
const events: ClientEvent[] = []
for (const [iface, base, prefix, n] of LANES) {
  for (let i = 0; i < n; i++) {
    events.push(
      event({
        inInterface: iface,
        outInterface: 'ether1',
        sourceIp: `${base}${20 + i}`,
        srcIp: `${base}${20 + i}`,
        srcHostName: `${prefix}-${i + 1}`,
        ruleLabel: `${prefix}-out`,
      }),
    )
  }
}
appState.events = events

const params = new URLSearchParams(location.search)
const requested = params.get('stop')
const stop: AltitudeLabel = (ALTITUDE_LABELS as readonly string[]).includes(requested ?? '') ? (requested as AltitudeLabel) : 'zones'
altitudeStopState.set(stop)

export default mount(Topography, {
  target: document.getElementById('stage')!,
})
