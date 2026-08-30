// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { zonesState } from '../lib/zones.svelte'
import { policyState } from '../lib/policy.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { emptyFilters, type ClientEvent, type Flag, type FlagType, type WatchlistEntry } from '../lib/types'
import Topography from './Topography.svelte'

// Topography's own $effect only reaches the network (zonesState.refresh
// etc.) once appState.devices is non-empty (see the component's doc
// comment) -- every test here keeps it empty, so the zones/policy/
// coverage stores are driven directly instead, the same way
// flags.svelte.test.ts and watchlist.svelte.test.ts drive their stores
// without mocking ../lib/api.

let nextEventId = 1
let nextFlagId = 1
let nextEntryId = 1

function event(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: nextEventId++,
    time: '2026-08-08T12:00:00Z',
    deviceId: 'router1',
    sourceIp: '192.168.1.50',
    action: 'accept',
    ruleLabel: 'test-rule',
    chain: 'forward',
    raw: '',
    receivedAt: Date.now(),
    ...overrides,
  }
}

function flag(type: FlagType, target: string, overrides: Partial<Flag> = {}): Flag {
  return {
    id: `f${nextFlagId++}`,
    type,
    target,
    detail: '',
    count: 1,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    cleared: false,
    ...overrides,
  }
}

function watchEntry(overrides: Partial<WatchlistEntry> = {}): WatchlistEntry {
  const id = `w${nextEntryId++}`
  return {
    id,
    name: `watch ${id}`,
    enabled: true,
    createdAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

beforeEach(() => {
  appState.devices = []
  appState.events = []
  appState.filters = emptyFilters()
  appState.view = 'topography'
  authState.role = 'admin'
  zonesState.pushed = []
  policyState.edges = []
  coverageState.declarations = []
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  nextEventId = 1
  nextFlagId = 1
  nextEntryId = 1
})

describe('the health dials (#648)', () => {
  it('shows a solid rest ring for both dials with nothing to report', () => {
    const { container } = render(Topography)
    flushSync()

    const rest = container.querySelectorAll('.dring.d-rest')
    expect(rest.length).toBe(2)
    const nums = [...container.querySelectorAll('.dnum')].map((n) => n.textContent)
    expect(nums).toEqual(['0', '0'])
  })

  it('splits the flags ring by alarm/advisory and counts only active flags', () => {
    flagsState.list = [
      flag('critical_port', '203.0.113.5'), // alarm (✱)
      flag('known_bad_ip', '203.0.113.6'), // alarm (✱)
      flag('activity_spike', '203.0.113.7'), // advisory (▲)
      flag('port_scan', '203.0.113.8', { cleared: true }), // cleared -- excluded
    ]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.dial .dnum')?.textContent).toBe('3')
    expect(container.querySelectorAll('.dring.d-rest').length).toBe(1) // watchers still at rest
  })

  it('splits the watchers ring by healthy/broken', () => {
    watchlistState.entries = [watchEntry({ enabled: true }), watchEntry({ enabled: true })]
    watchlistState.coverage = { [watchlistState.entries[0].id]: 'no-logging' }
    const { container } = render(Topography)
    flushSync()

    const dnums = [...container.querySelectorAll('.dnum')].map((n) => n.textContent)
    expect(dnums).toEqual(['0', '2'])
    expect(container.querySelectorAll('.dring.d-broken').length).toBe(1)
    expect(container.querySelectorAll('.dring.d-healthy').length).toBe(1)
  })

  it('clicking either dial opens the docket', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    watchlistState.entries = [watchEntry()]
    const { container } = render(Topography)
    flushSync()

    const dials = container.querySelectorAll<HTMLButtonElement>('.dial')
    expect(dials.length).toBe(2)

    dials[0].click()
    expect(appState.view).toBe('flags')

    dials[1].click()
    expect(appState.view).toBe('watchlist')
  })
})

describe('the aggregate bar (#648)', () => {
  function seedZoneWithFlagAndWatch() {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50', srcHostName: 'desk' })]
    flagsState.list = [flag('critical_port', '192.168.1.50')]
    watchlistState.entries = [watchEntry({ source: { ip: '192.168.1.50' } })]
  }

  it('is absent when nothing is open or watched on the zone', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.hbar-g')).toBeNull()
  })

  it('draws split red/purple with a centre divider when both flags and a watch touch the zone', () => {
    seedZoneWithFlagAndWatch()
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.hb-w')).not.toBeNull()
    expect(container.querySelector('.hb-f')).not.toBeNull()
    expect(container.querySelector('.hb-div')).not.toBeNull()
  })

  it('a flag outside the zone CIDR does not count toward it', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    flagsState.list = [flag('critical_port', '10.9.0.9')] // outside 192.168.1.0/24
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.hbar-g')).toBeNull()
  })

  it('clicking the flags half filters flags to the zone', () => {
    seedZoneWithFlagAndWatch()
    const { container } = render(Topography)
    flushSync()

    const flagsHalf = container.querySelector<SVGGElement>('.hbar-g[aria-label*="open flag"]')
    expect(flagsHalf).not.toBeNull()
    flagsHalf!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

    expect(appState.view).toBe('flags')
    expect(appState.filters.interface).toBe('bridge1')
  })

  it('clicking the watch half opens the watchlist', () => {
    seedZoneWithFlagAndWatch()
    const { container } = render(Topography)
    flushSync()

    const watchHalf = container.querySelector<SVGGElement>('.hbar-g[aria-label*="watcher"]')
    expect(watchHalf).not.toBeNull()
    watchHalf!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

    expect(appState.view).toBe('watchlist')
  })
})

describe('the altitude slider (#648)', () => {
  it('renders one range input and four stop symbols, no text labels', () => {
    const { container } = render(Topography)
    flushSync()

    const range = container.querySelector<HTMLInputElement>('.alt-range')
    expect(range).not.toBeNull()
    expect(range?.min).toBe('0')
    expect(range?.max).toBe('3')
    expect(range?.value).toBe('2') // defaults to "zones", today's unchanged map

    const ticks = container.querySelectorAll('.tick')
    expect(ticks.length).toBe(4)
    expect(container.querySelector('.tick.diamond')).not.toBeNull() // survey's atlas diamond
    expect(container.querySelector('.altitude')?.textContent?.trim()).toBe('')
  })

  it('moving the slider reframes the map camera, including the survey tilt', () => {
    const { container } = render(Topography)
    flushSync()

    const range = container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = '3'
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()

    expect(container.querySelector('.camera.cam-survey')).not.toBeNull()
    expect(container.querySelector('.tick.diamond.on')).not.toBeNull()
  })
})

describe('node info cards (#648)', () => {
  it("opens the reached host's own card, with its zone as lane", () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()

    const hostLink = container.querySelector<SVGTSpanElement>('.host-link')
    expect(hostLink).not.toBeNull()
    hostLink!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    expect(container.querySelector('.membrane-layer')).not.toBeNull()

    const hostNode = container.querySelector<SVGGElement>('.host-node')
    expect(hostNode).not.toBeNull()
    hostNode!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    const card = container.querySelector('.node-card')
    expect(card).not.toBeNull()
    expect(card?.textContent).toContain('desk')
    expect(card?.textContent).toContain('The LAN')
  })
})
