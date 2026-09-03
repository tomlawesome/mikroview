// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'

import { appState } from './state.svelte'
import { zonesState } from './zones.svelte'
import { tunnelsState, type TunnelInterface } from './tunnels.svelte'
import type { ClientEvent } from './types'

// A buffered event, as appState holds one -- receivedAt is the client
// stamp appendLive adds, so a bare FirewallEvent is not one.
function event(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00Z',
    receivedAt: 0,
    deviceId: 'router1',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'r',
    chain: 'forward',
    raw: '',
    ...overrides,
  }
}

// A pushed tunnel interface, as tunnelsState holds one per device.
function tunnel(iface: string, kind: 'wg' | 'ppp' = 'wg', apiState: TunnelInterface['apiState'] = 'up'): TunnelInterface {
  return { iface, routerId: 'router1', kind, apiState, peers: [] }
}

beforeEach(() => {
  appState.events = []
  zonesState.pushed = []
  // The lane row now consults the tunnel tables (#877), so a leftover
  // tunnel would quietly drop a lane from an unrelated test.
  tunnelsState.byDevice = new Map()
})

describe('zonesState per-device WAN (#850)', () => {
  it('excludes neither device\'s WAN when two routers carry different WAN names', () => {
    // router1's WAN is ether1, router2's is sfp-sfpplus1 -- the seed-
    // demo shape the issue was filed from. Pooling every device's
    // events together, as the old single wanInterface did, would let
    // whichever name has more hits win and draw the other as a lane.
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.10' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.11' }),
      event({ id: 4, deviceId: 'router1', inInterface: 'bridge1', srcIp: '192.168.1.5' }),
      event({ id: 5, deviceId: 'router2', inInterface: 'sfp-sfpplus1', srcIp: '198.51.100.7' }),
      event({ id: 6, deviceId: 'router2', inInterface: 'bridge1', srcIp: '192.168.2.5' }),
    ]

    expect(zonesState.wanInterfaces).toEqual(new Set(['ether1', 'sfp-sfpplus1']))
    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).not.toContain('ether1')
    expect(laneIds).not.toContain('sfp-sfpplus1')
    // bridge1 is a real lane on both devices and still renders -- the
    // two devices' traffic on it just merges under one name, which is
    // existing behaviour (#852 is what would change that).
    expect(laneIds).toContain('bridge1')
  })

  it('gives a device with no public inbound yet no WAN, and does not suppress a real lane', () => {
    appState.events = [
      // router1 has a real WAN.
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      // router2 has only internal traffic so far -- nothing public
      // inbound, so it contributes no WAN (an observation, not a guess).
      event({ id: 2, deviceId: 'router2', inInterface: 'bridge5', srcIp: '192.168.9.5' }),
    ]

    expect(zonesState.wanInterfaces).toEqual(new Set(['ether1']))
    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).not.toContain('ether1')
    expect(laneIds).toContain('bridge5')
  })

  it('keeps single-device behaviour unchanged: one WAN, excluded from the lane row', () => {
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.10' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'bridge1', srcIp: '192.168.1.5' }),
    ]

    expect(zonesState.wanInterface).toBe('ether1')
    expect(zonesState.wanInterfaces).toEqual(new Set(['ether1']))
    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).not.toContain('ether1')
    expect(laneIds).toContain('bridge1')
  })

  it('picks the busiest device\'s WAN as the single wanInterface, for callers that still want one', () => {
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      event({ id: 2, deviceId: 'router2', inInterface: 'sfp-sfpplus1', srcIp: '198.51.100.7' }),
      event({ id: 3, deviceId: 'router2', inInterface: 'sfp-sfpplus1', srcIp: '198.51.100.8' }),
      event({ id: 4, deviceId: 'router2', inInterface: 'sfp-sfpplus1', srcIp: '198.51.100.9' }),
    ]

    expect(zonesState.wanInterface).toBe('sfp-sfpplus1')
  })
})

describe('the lane row is ordered by how busy each lane is', () => {
  it('puts the busier lane first', () => {
    // #715 item 9 took the "N events this window" line off the zone
    // card, because round 30 draws no such line. The count behind it is
    // not decorative and does not go with it: it is what orders the
    // lane row, and the row's order is what the reader takes from left
    // to right. Asserted here so removing the field breaks a test that
    // says why it exists.
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'bridge2', srcIp: '192.168.2.5' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'bridge2', srcIp: '192.168.2.6' }),
      event({ id: 4, deviceId: 'router1', inInterface: 'bridge2', srcIp: '192.168.2.7' }),
      event({ id: 5, deviceId: 'router1', inInterface: 'bridge1', srcIp: '192.168.1.5' }),
    ]

    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).toContain('bridge1')
    expect(laneIds).toContain('bridge2')
    expect(laneIds.indexOf('bridge2')).toBeLessThan(laneIds.indexOf('bridge1'))
  })
})

describe('the drawn tunnel leaves the lane row (#877)', () => {
  it('keeps a pushed WireGuard interface out of the lanes and names it as the node', () => {
    // Before this, a WireGuard interface could only ever be a lane:
    // zones swallowed every observed non-WAN boundary. Round 30 draws
    // it as a second upper node instead.
    tunnelsState.byDevice = new Map([['router1', [tunnel('wg0')]]])
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'ether1', srcIp: '203.0.113.9' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.2' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'bridge1', srcIp: '192.168.1.5' }),
    ]

    expect(zonesState.tunnelInterface).toBe('wg0')
    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).not.toContain('wg0')
    expect(laneIds).toContain('bridge1')
  })

  it('does not spend a lane slot on the tunnel', () => {
    // The cap is five. With five real lanes plus a tunnel, the tunnel
    // taking a slot would push the quietest lane off the map -- which
    // is the "does not consume one of the five lane slots" half of the
    // issue, and invisible from the previous test alone.
    tunnelsState.byDevice = new Map([['router1', [tunnel('wg0')]]])
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.2' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.3' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.4' }),
      ...['bridge1', 'bridge2', 'bridge3', 'bridge4', 'bridge5'].map((iface, i) =>
        event({ id: 10 + i, deviceId: 'router1', inInterface: iface, srcIp: `192.168.${i + 1}.5` }),
      ),
    ]

    const laneIds = zonesState.zones.map((z) => z.id)
    expect(laneIds).toHaveLength(5)
    expect(laneIds).not.toContain('wg0')
    expect(new Set(laneIds)).toEqual(new Set(['bridge1', 'bridge2', 'bridge3', 'bridge4', 'bridge5']))
  })

  it('leaves a PPP session in the lane row', () => {
    // Round 30 draws no PPP node, so excluding one from the lanes
    // would delete it from the map altogether rather than move it.
    tunnelsState.byDevice = new Map([['router1', [tunnel('l2tp-in1', 'ppp')]]])
    appState.events = [event({ id: 1, deviceId: 'router1', inInterface: 'l2tp-in1', srcIp: '10.90.0.2' })]

    expect(zonesState.tunnelInterface).toBeNull()
    expect(zonesState.zones.map((z) => z.id)).toContain('l2tp-in1')
  })

  it('gives the node to the busiest tunnel and leaves the others as lanes', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel('wg0'), tunnel('wg1')]]])
    appState.events = [
      event({ id: 1, deviceId: 'router1', inInterface: 'wg1', srcIp: '10.99.1.2' }),
      event({ id: 2, deviceId: 'router1', inInterface: 'wg1', srcIp: '10.99.1.3' }),
      event({ id: 3, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.2' }),
    ]

    expect(zonesState.tunnelInterface).toBe('wg1')
    // wg0 stays visible as a lane rather than vanishing with nowhere
    // to be drawn -- a second tunnel node is a design question.
    expect(zonesState.zones.map((z) => z.id)).toContain('wg0')
  })

  it('picks alphabetically when nothing has been observed on any tunnel', () => {
    // Otherwise the node hops between tunnels as the buffer turns over,
    // with every count still zero.
    tunnelsState.byDevice = new Map([['router1', [tunnel('wg9'), tunnel('wg0')]]])
    expect(zonesState.tunnelInterface).toBe('wg0')
  })

  it('draws no tunnel node until a tunnel table has been pushed', () => {
    // An interface merely named wg0 in the events is not a pushed
    // tunnel: #874 exists so this state comes from the router, not
    // from a name that looks like one.
    appState.events = [event({ id: 1, deviceId: 'router1', inInterface: 'wg0', srcIp: '10.99.0.2' })]

    expect(zonesState.tunnelInterface).toBeNull()
    expect(zonesState.zones.map((z) => z.id)).toContain('wg0')
  })

  it('keeps the tunnel out of the lanes even when only the address table names it', () => {
    // The pushed-table half of zones is a separate exclusion from the
    // observed-events half, and misses it independently.
    tunnelsState.byDevice = new Map([['router1', [tunnel('wg0')]]])
    zonesState.pushed = [{ address: '10.99.0.1/24', network: '10.99.0.0', interface: 'wg0', comment: '' }]

    expect(zonesState.zones.map((z) => z.id)).not.toContain('wg0')
  })
})
