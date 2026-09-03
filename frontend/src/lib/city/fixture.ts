// SPDX-License-Identifier: AGPL-3.0-only
//
// The mockup's estate as CityInput, for the city tests: two routers,
// six zones, the WAN and two tunnels, roads of every verdict.
import type { CityInput } from './input'

const hosts = (n: number, prefix: string, base: string) =>
  Array.from({ length: n }, (_, i) => ({ label: prefix + '-' + (i + 1), ip: base + (10 + i) }))

export function mockupEstate(): CityInput {
  return {
    routers: [
      { id: 'rb5009', name: 'rb5009', primary: true, sourceIp: '10.10.0.1' },
      { id: 'hapax3', name: 'hAP ax3', primary: false, sourceIp: '10.10.0.40' },
    ],
    zones: [
      { id: 'bridge-lan', name: 'LAN', cidr: '10.10.0.0/24', hosts: hosts(6, 'lan', '10.10.0.'), hostCount: 9, eventCount: 900, routerId: 'rb5009', dark: false },
      { id: 'vlan-srv', name: 'Servers', cidr: '10.20.0.0/24', hosts: hosts(4, 'srv', '10.20.0.'), hostCount: 4, eventCount: 600, routerId: 'rb5009', dark: false },
      { id: 'vlan-iot', name: 'IoT', cidr: '10.30.0.0/24', hosts: hosts(5, 'iot', '10.30.0.'), hostCount: 5, eventCount: 300, routerId: 'rb5009', dark: false },
      { id: 'vlan-guest', name: 'Guest', cidr: '10.40.0.0/24', hosts: hosts(1, 'guest', '10.40.0.'), hostCount: 1, eventCount: 40, routerId: 'rb5009', dark: true },
      { id: 'wlan-wsh', name: 'Workshop', cidr: '10.50.0.0/24', hosts: hosts(3, 'wsh', '10.50.0.'), hostCount: 3, eventCount: 60, routerId: 'hapax3', dark: false },
      { id: 'wlan-cams', name: 'Cameras', cidr: '10.60.0.0/24', hosts: hosts(2, 'cam', '10.60.0.'), hostCount: 2, eventCount: 20, routerId: 'hapax3', dark: false },
    ],
    edges: [
      { key: 'bridge-lan|ether1', from: 'bridge-lan', to: 'ether1', events: 500, verdict: 'planned' },
      { key: 'vlan-srv|ether1', from: 'vlan-srv', to: 'ether1', events: 200, verdict: 'planned' },
      { key: 'vlan-iot|ether1', from: 'vlan-iot', to: 'ether1', events: 80, verdict: 'planned' },
      { key: 'vlan-guest|ether1', from: 'vlan-guest', to: 'ether1', events: 30, verdict: 'planned' },
      { key: 'bridge-lan|vlan-srv', from: 'bridge-lan', to: 'vlan-srv', events: 400, verdict: 'planned' },
      { key: 'vlan-srv|bridge-lan', from: 'vlan-srv', to: 'bridge-lan', events: 100, verdict: 'planned' },
      { key: 'vlan-iot|vlan-srv', from: 'vlan-iot', to: 'vlan-srv', events: 60, verdict: 'planned' },
      { key: 'vlan-iot|bridge-lan', from: 'vlan-iot', to: 'bridge-lan', events: 12, verdict: 'unplanned' },
      { key: 'vlan-guest|bridge-lan', from: 'vlan-guest', to: 'bridge-lan', events: 5, verdict: 'holding' },
      { key: 'bridge-lan|wg0', from: 'bridge-lan', to: 'wg0', events: 3, verdict: 'unjudged' },
      { key: 'wlan-wsh|bridge-lan', from: 'wlan-wsh', to: 'bridge-lan', events: 20, verdict: 'planned' },
    ],
    wan: 'ether1',
    wanLogged: true,
    tunnels: [
      {
        iface: 'l2tp-out1',
        routerId: 'rb5009',
        apiState: 'up',
        events: 3,
        peers: [{ id: 'l2tp-out1/ppp/branch', name: 'branch-office', address: '10.90.0.2', kind: 'ppp' }],
      },
      { iface: 'wg0', routerId: 'rb5009', apiState: 'down', events: 0, peers: [] },
    ],
  }
}
