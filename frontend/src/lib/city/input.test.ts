import { describe, expect, it } from 'vitest'
import { cityInputFrom, isTunnel, zoneHolding } from './input'
import type { Device, FirewallEvent } from '../types'
import type { ZoneInfo } from '../zones.svelte'
import type { PolicyEdge } from '../policy.svelte'

const device = (id: string, sourceIp: string): Device =>
  ({ id, name: id, sourceIp, configured: true, firstSeen: '', lastSeen: '', eventCount: 0, status: 'live' }) as Device

const event = (deviceId: string, inInterface?: string, outInterface?: string): FirewallEvent =>
  ({ id: 1, time: '', deviceId, sourceIp: '', action: 'accept', ruleLabel: '', chain: 'forward', inInterface, outInterface }) as FirewallEvent

const zone = (id: string, cidr: string | null): ZoneInfo => ({ id, name: id, cidr, hosts: [], hostCount: 0, eventCount: 0 })

const policy = (from: string, to: string, logged: boolean): PolicyEdge =>
  ({ key: from + '|' + to, from, to, accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged }) as PolicyEdge

describe('city input', () => {
  it('knows a tunnel by its name', () => {
    expect(isTunnel('wg0')).toBe(true)
    expect(isTunnel('l2tp-out1')).toBe(true)
    expect(isTunnel('bridge1')).toBe(false)
    expect(isTunnel('ether1')).toBe(false)
  })

  it('gives each zone the router that logs on it, and drops tunnels', () => {
    const devices = [device('rb', '10.0.0.1'), device('hap', '10.0.0.40')]
    const zones = [zone('bridge1', '10.0.0.0/24'), zone('wlan-wsh', '10.5.0.0/24'), zone('wg0', null)]
    const events = [event('rb', 'bridge1', 'ether1'), event('rb', 'bridge1', 'ether1'), event('hap', 'wlan-wsh', 'bridge1'), event('rb', 'bridge1', 'wg0')]
    const input = cityInputFrom(devices, zones, events, [], [], false, 'rb', 'ether1')
    expect(input.routers.map((r) => [r.id, r.primary])).toEqual([
      ['rb', true],
      ['hap', false],
    ])
    expect(input.zones.map((z) => [z.id, z.routerId])).toEqual([
      ['bridge1', 'rb'],
      ['wlan-wsh', 'hap'],
    ])
    expect(input.tunnels.map((t) => t.iface)).toEqual(['wg0'])
    // No pushed tunnel table named it: seen only in events, so it reads
    // exactly like the API's own "unknown" -- never a guessed state.
    expect(input.tunnels[0].apiState).toBeNull()
    expect(input.wan).toBe('ether1')
  })

  it('lamps the WAN bridge only when a logging rule covers that boundary', () => {
    const devices = [device('rb', '10.0.0.1')]
    const zones = [zone('bridge1', null)]
    const lamped = cityInputFrom(devices, zones, [], [], [policy('bridge1', 'ether1', true)], true, 'rb', 'ether1')
    expect(lamped.wanLogged).toBe(true)
    const unlit = cityInputFrom(devices, zones, [], [], [policy('bridge1', 'ether1', false)], true, 'rb', 'ether1')
    expect(unlit.wanLogged).toBe(false)
    const none = cityInputFrom(devices, zones, [], [], [], false, 'rb', null)
    expect(none.wanLogged).toBe(false)
  })

  it('dims a zone nothing logs on once a rule table is pushed', () => {
    const devices = [device('rb', '10.0.0.1')]
    const zones = [zone('bridge1', null), zone('vlan-guest', null)]
    const lit = cityInputFrom(devices, zones, [], [], [policy('bridge1', 'ether1', true)], true, 'rb', 'ether1')
    expect(lit.zones.map((z) => z.dark)).toEqual([false, true])
    const none = cityInputFrom(devices, zones, [], [], [], false, 'rb', null)
    expect(none.zones.map((z) => z.dark)).toEqual([false, false])
  })

  it('stands in a router when no device exists yet', () => {
    const input = cityInputFrom([], [], [], [], [], false, null, null)
    expect(input.routers).toHaveLength(1)
    expect(input.routers[0].primary).toBe(true)
  })

  it('carries a pushed tunnel table state through even with no events', () => {
    const devices = [device('rb', '10.0.0.1')]
    const input = cityInputFrom(devices, [], [], [], [], false, 'rb', null, [
      {
        iface: 'wg0',
        routerId: 'rb',
        kind: 'wg',
        apiState: 'down',
        peers: [{ id: 'wg0/wg/1', name: 'phone', address: '10.9.0.2', kind: 'wg' }],
      },
    ])
    expect(input.tunnels).toEqual([{ iface: 'wg0', routerId: 'rb', apiState: 'down', events: 0, peers: [{ id: 'wg0/wg/1', name: 'phone', address: '10.9.0.2', kind: 'wg' }] }])
  })

  it('finds the zone whose CIDR holds an address', () => {
    const zones = cityInputFrom([device('rb', '1.1.1.1')], [zone('a', '10.0.0.0/24'), zone('b', '10.5.0.0/16')], [], [], [], false, 'rb', null).zones
    expect(zoneHolding(zones, '10.5.9.9')?.id).toBe('b')
    expect(zoneHolding(zones, '192.168.1.1')).toBeNull()
  })
})
