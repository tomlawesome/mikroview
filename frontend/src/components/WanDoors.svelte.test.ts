// SPDX-License-Identifier: AGPL-3.0-only
//
// #1319: the doors panel's own composition -- one section per device,
// the two groups within it, the "absent" line before #1329's push and
// the service rows after it, both marks on a rule row, and the printed
// nmap check's own port union. The assembly of one device's answer is
// Go's business (internal/api/wanedge.go); this file only proves the
// sheet renders what it is given.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchWanDoors: vi.fn(),
}))

import { fetchWanDoors, type WanDoorsResponse } from '../lib/api'
import { wanDoorsState } from '../lib/wanDoors.svelte'
import { appState } from '../lib/state.svelte'
import type { ClientEvent } from '../lib/types'
import WanDoors from './WanDoors.svelte'

let nextId = 1
function event(overrides: Partial<ClientEvent>): ClientEvent {
  return {
    id: nextId++,
    time: '2026-09-29T12:00:00Z',
    receivedAt: Date.now(),
    deviceId: 'core',
    sourceIp: '203.0.113.1',
    action: 'accept',
    ruleLabel: 'r1',
    chain: 'forward',
    raw: '',
    ...overrides,
  }
}

// Gives zonesState.deviceWans exactly one entry per device, the same
// public-source-inbound observation zones.svelte.ts derives it from --
// this panel has no fetch of its own to drive without it.
function withDeviceWans(devices: { id: string; iface: string }[]) {
  appState.events = devices.map((d) =>
    event({ deviceId: d.id, inInterface: d.iface, sourceIp: '198.51.100.9', srcIp: '198.51.100.9' }),
  )
}

async function open() {
  render(WanDoors)
  wanDoorsState.open()
  flushSync()
  await vi.waitFor(() => expect(screen.getByText('Doors from the internet')).toBeTruthy())
}

beforeEach(() => {
  nextId = 1
  vi.mocked(fetchWanDoors).mockReset()
})

afterEach(() => {
  wanDoorsState.close()
  wanDoorsState.devices = []
  appState.events = []
  cleanup()
})

describe('WanDoors', () => {
  it('renders one section per device, in name order, with the two groups', async () => {
    withDeviceWans([
      { id: 'core', iface: 'ether1' },
      { id: 'shed', iface: 'ether5' },
    ])
    vi.mocked(fetchWanDoors).mockImplementation(async (device: string, wan: string) => {
      if (device === 'core') {
        return {
          devices: [
            {
              id: 'core',
              name: 'rb5009',
              wan,
              doors: [
                { label: '#12', ordinal: 12, dstPort: '443', proto: 'tcp', comment: 'web', to: { ip: '192.168.1.5', name: 'plex' }, lastSeen: undefined },
              ],
              services: null,
            },
          ],
        } satisfies WanDoorsResponse
      }
      return {
        devices: [{ id: 'shed', name: '', wan, doors: [], services: [] }],
      } satisfies WanDoorsResponse
    })
    await open()

    const headings = [...document.querySelectorAll('h3')].map((h) => h.textContent)
    expect(headings).toEqual(['rb5009 · ether1', 'shed · ether5'])
  })

  it('shows the absent line when services is null, and the service rows once #1329 is pushed', async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    // A door through to a host, so the device is not wholly empty --
    // group 1 (to the router itself) still has nothing of its own,
    // which is the case the absent line is about.
    const hostDoor = { label: '#12', ordinal: 12, dstPort: '443', proto: 'tcp', to: { ip: '192.168.1.5' } }
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [{ id: device, name: 'rb5009', wan, doors: [hostDoor], services: null }],
    }))
    await open()
    expect(screen.getByTestId('wd-absent-core').textContent).toContain(
      'This router does not push /ip service yet',
    )

    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [
        {
          id: device,
          name: 'rb5009',
          wan,
          doors: [hostDoor],
          services: [{ name: 'winbox', port: 8291, address: '' }],
        },
      ],
    }))
    await wanDoorsState.refresh()
    flushSync()

    expect(screen.queryByTestId('wd-absent-core')).toBeNull()
    expect(screen.getByText('services the router itself runs')).toBeTruthy()
    expect(screen.getByText('winbox · 8291/tcp · any address')).toBeTruthy()
  })

  it('carries both marks on a rule row: allowed through always, seen arriving only when something did', async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [
        {
          id: device,
          name: 'rb5009',
          wan,
          doors: [
            { label: '#5', ordinal: 5, dstPort: '22', proto: 'tcp', comment: 'admin ssh' },
            {
              label: '#12',
              ordinal: 12,
              dstPort: '443',
              proto: 'tcp',
              comment: 'web',
              to: { ip: '192.168.1.5', name: 'plex' },
              lastSeen: '2026-09-29T11:57:00Z',
            },
          ],
          services: null,
        },
      ],
    }))
    await open()

    expect(screen.getByText('#5 22/tcp → this router')).toBeTruthy()
    expect(screen.getByText('admin ssh · allowed through ○ · nothing arrived in the window')).toBeTruthy()

    expect(screen.getByText('#12 443/tcp → 192.168.1.5 · plex')).toBeTruthy()
    expect(screen.getByText(/web · allowed through ○ · seen arriving ●/)).toBeTruthy()
  })

  it("prints the device's own nmap check with the union of its door and service ports", async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [
        {
          id: device,
          name: 'rb5009',
          wan,
          publicAddress: '203.0.113.9',
          doors: [
            { label: '#5', ordinal: 5, dstPort: '22', proto: 'tcp' },
            { label: '#12', ordinal: 12, dstPort: '443', proto: 'tcp', to: { ip: '192.168.1.5' } },
          ],
          services: [{ name: 'winbox', port: 8291, address: '' }],
        },
      ],
    }))
    await open()

    expect(screen.getByText('nmap -Pn -p 22,443,8291 203.0.113.9')).toBeTruthy()
  })

  it('falls back to the literal placeholder address when none is pushed', async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [{ id: device, name: 'rb5009', wan, doors: [{ label: '#5', ordinal: 5, dstPort: '22' }], services: null }],
    }))
    await open()

    expect(screen.getByText('nmap -Pn -p 22 your-public-address')).toBeTruthy()
  })

  it('shows the fixed caveat and the empty-device line honestly', async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [{ id: device, name: 'rb5009', wan, doors: [], services: [] }],
    }))
    await open()

    expect(
      screen.getByText(
        'Nothing in the pushed tables lets the internet in, and nothing arrived in the window.',
      ),
    ).toBeTruthy()
    expect(
      screen.getByText(/Rule order, a filter at your ISP and CGNAT can all keep a listed door shut/),
    ).toBeTruthy()
  })

  it('is one dialog spoken as a region, and Esc closes it', async () => {
    withDeviceWans([{ id: 'core', iface: 'ether1' }])
    vi.mocked(fetchWanDoors).mockImplementation(async (device, wan) => ({
      devices: [{ id: device, name: 'rb5009', wan, doors: [], services: null }],
    }))
    await open()

    const sheet = screen.getByTestId('wan-doors')
    expect(sheet.getAttribute('role')).toBe('dialog')
    expect(sheet.getAttribute('aria-labelledby')).toBe('wan-doors-title')

    await fireEvent.keyDown(window, { key: 'Escape' })
    flushSync()
    expect(screen.queryByTestId('wan-doors')).toBeNull()
  })
})
