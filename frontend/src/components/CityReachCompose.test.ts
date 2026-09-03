// SPDX-License-Identifier: AGPL-3.0-only
//
// #868's own "Done when": the city's composer prints the same RouterOS
// line the 2D map's composer does for the same strand. Both mount from
// the same appState/zonesState/policyState -- the same live derivation
// pipeline a real session runs -- rather than two hand-built fixtures
// that happen to describe the same thing today. Proving this by calling
// both real rendering paths and diffing their printed text is the point:
// two independent re-implementations of one line agree the day they are
// written and diverge on the first change to either. They do not
// diverge here because there is only one implementation
// (reachComposeInput/composeCommand, lib/compose.ts) -- this test would
// catch it immediately if a future edit ever gave them two.
import { beforeEach, describe, expect, it } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { zonesState } from '../lib/zones.svelte'
import { policyState } from '../lib/policy.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import { emptyFilters, type ClientEvent, type Device } from '../lib/types'
import Topography from './Topography.svelte'
import City from './City.svelte'

const HOST_IP = '192.168.1.50'
const FAR_IP = '10.0.9.9'

function router(over: Partial<Device> = {}): Device {
  return {
    id: 'router1',
    name: 'lab-crs',
    sourceIp: '10.0.0.1',
    configured: true,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-09-03T00:00:00Z',
    eventCount: 10,
    status: 'live',
    ...over,
  }
}

let nextId = 1
function event(over: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: nextId++,
    time: '2026-09-03T12:00:00Z',
    receivedAt: Date.now(),
    deviceId: 'router1',
    sourceIp: HOST_IP,
    action: 'accept',
    ruleLabel: '',
    chain: 'forward',
    raw: '',
    ...over,
  }
}

beforeEach(() => {
  appState.devices = [router()]
  appState.filters = emptyFilters()
  authState.role = 'admin'
  zonesState.pushed = []
  policyState.byDevice = {}
  policyState.pushed = []
  topologyNavState.pendingDescend = null

  // The host on bridge1 asks vlan-iot for tcp/445 three times and is
  // refused every time, named by its own rule -- the one strand both
  // views draft a rule from.
  appState.events = Array.from({ length: 3 }, (_, i) =>
    event({
      id: i + 1,
      srcIp: HOST_IP,
      dstIp: FAR_IP,
      inInterface: 'bridge1',
      outInterface: 'vlan-iot',
      action: 'drop',
      ruleLabel: 'iot-egress-drop',
      dstPort: 445,
      protocol: 'tcp',
    }),
  )
})

describe('the composer prints the same line in the city as in 2D (#868)', () => {
  it('is byte-identical for the same blocked strand', async () => {
    // Path 1: the 2D map's own reach and composer.
    const topo = render(Topography)
    topologyNavState.pendingDescend = { zoneId: 'bridge1', host: HOST_IP, ip: HOST_IP }
    flushSync()
    const door = topo.container.querySelector('.strand-door') as HTMLElement
    expect(door).not.toBeNull()
    await fireEvent.click(door)
    flushSync()
    const cmd2D = topo.container.querySelector('.composer .cmd')?.textContent
    expect(cmd2D).toBeTruthy()

    // Path 2: standing on the same host in the city, deriving its own
    // ground from the very same stores -- no fixture stands in for it.
    const city = render(City, { props: { stop: 'street' } })
    const building = [...city.container.querySelectorAll('[data-cid]')].find((el) => el.getAttribute('aria-label')?.includes(HOST_IP))
    expect(building).not.toBeUndefined()
    await fireEvent.click(building!)
    flushSync()
    const cmdCity = city.container.querySelector('.composer .cm-code')?.textContent

    expect(cmdCity).toBeTruthy()
    expect(cmdCity).toContain(`src-address=${HOST_IP}`)
    expect(cmdCity).toContain(`dst-address=${FAR_IP}`)
    expect(cmdCity).toBe(cmd2D)
  })
})
