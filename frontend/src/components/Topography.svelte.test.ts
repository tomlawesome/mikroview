// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { zonesState } from '../lib/zones.svelte'
import { tunnelsState, type TunnelInterface } from '../lib/tunnels.svelte'
import { policyState } from '../lib/policy.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { altitudeStopState } from '../lib/altitudeStop.svelte'
import type { RouterFilterRule, RouterIPAddress } from '../lib/api'
import { emptyFilters, type ClientEvent, type Device, type Flag, type FlagType, type WatchlistEntry } from '../lib/types'
import Topography from './Topography.svelte'
// Vite's own `?raw` import (typed by vite/client, already in this
// project's tsconfig) -- not a Node fs read -- so the handful of
// assertions below that care about a raw CSS value or a stylesheet's own
// token can read the component's source text without pulling `node:fs`
// into a file svelte-check type-checks under the browser-only app
// tsconfig (no `node` types there).
import componentSource from './Topography.svelte?raw'

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

function address(overrides: Partial<RouterIPAddress> = {}): RouterIPAddress {
  return { address: '10.0.0.1/24', network: '10.0.0.0', interface: 'bridge1', comment: '', ...overrides }
}

// The internet island's own `.n-cidr` text node -- found by the card
// whose name is "Internet" rather than by document order -- holding
// the sibling `.cidr-v` / `.cidr-deg` tspans (the-whole.html:977).
function internetCardCidr(container: HTMLElement): Element | null {
  const name = [...container.querySelectorAll('.n-name')].find((n) => n.textContent?.trim() === 'Internet')
  return name?.parentElement?.querySelector('.n-cidr') ?? null
}

// A pushed tunnel interface (#874's tables, as tunnelsState holds
// them). The 2D map's node is WireGuard only, so `kind` matters.
function tunnel(overrides: Partial<TunnelInterface> = {}): TunnelInterface {
  return { iface: 'wg0', routerId: 'router1', kind: 'wg', apiState: 'up', peers: [], ...overrides }
}

// The tunnel node's own card, found by its name rather than document
// order -- there are two upper cards now.
function tunnelCard(container: HTMLElement): Element | null {
  const name = [...container.querySelectorAll('.n-name')].find((n) => n.textContent?.trim() === 'WireGuard')
  return name?.parentElement ?? null
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
  tunnelsState.byDevice = new Map()
  policyState.edges = []
  policyState.byDevice = {}
  // Assigning byDevice directly does not recompute anyPushed, so
  // without this a test that pushed a table leaves every later test
  // judging traffic against a table it never pushed.
  policyState.anyPushed = false
  coverageState.declarations = []
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  topologyNavState.pendingFlagId = null
  topologyNavState.pendingWatchId = null
  topologyNavState.pendingDescend = null
  wizardState.open = false
  // Every test starts on a fresh slider: altitudeStopState is a
  // module-level singleton (like cityImportanceState), so a test that
  // moves the slider would otherwise leak its last stop into whichever
  // test runs next in this file.
  altitudeStopState.stop = 'city'
  localStorage.removeItem('mikroview:topography-altitude')
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

  // #724 replaces "one click leaves for the docket" with a two-step
  // control: this used to assert dials[0].click() went straight to
  // appState.view === 'flags'. That is now the *old* behaviour -- see
  // "the dials' condensed panel (#724)" below for what a click does
  // instead (expands a panel) and how a row gets you to the docket.
  it('clicking a dial no longer leaves the scene by itself -- it only expands the panel', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    watchlistState.entries = [watchEntry()]
    const { container } = render(Topography)
    flushSync()

    const dials = container.querySelectorAll<HTMLButtonElement>('.dial')
    expect(dials.length).toBe(2)

    dials[0].click()
    flushSync()
    expect(appState.view).toBe('topography')
    expect(dials[0].getAttribute('aria-expanded')).toBe('true')

    // Clicking the *other* dial while a panel is open counts as
    // "somewhere else on the page" for the click-away rule: it closes
    // the flags panel rather than also opening the watchers one, since
    // the click-away click must not also trigger whatever was under the
    // pointer (#724's own "Care" note).
    dials[1].click()
    flushSync()
    expect(appState.view).toBe('topography')
    expect(dials[0].getAttribute('aria-expanded')).toBe('false')
    expect(dials[1].getAttribute('aria-expanded')).toBe('false')

    // A second, real click on the watchers dial does open its own panel.
    dials[1].click()
    flushSync()
    expect(appState.view).toBe('topography')
    expect(dials[1].getAttribute('aria-expanded')).toBe('true')
  })
})

describe("the dials' condensed panel (#724)", () => {
  it('first click opens the panel, worst first, capped at five rows plus "and N more"', () => {
    flagsState.list = [
      flag('activity_spike', '203.0.113.1', { lastSeen: '2026-01-01T01:00:00Z' }), // advisory
      flag('critical_port', '203.0.113.2', { lastSeen: '2026-01-01T02:00:00Z' }), // alarm, older
      flag('critical_port', '203.0.113.3', { lastSeen: '2026-01-01T03:00:00Z' }), // alarm, newest
      flag('critical_port', '203.0.113.4'),
      flag('critical_port', '203.0.113.5'),
      flag('critical_port', '203.0.113.6'),
    ]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const panel = container.querySelector('#' + flagsDial.getAttribute('aria-controls'))
    expect(panel).not.toBeNull()
    const rows = panel!.querySelectorAll('.dp-row')
    // 5 flag rows + the "and N more" row
    expect(rows.length).toBe(6)
    expect(rows[rows.length - 1].textContent).toContain('and 1 more')

    // Worst first: the two alarm (✱) critical_port flags sort ahead of
    // the advisory activity_spike, and the newer alarm sorts ahead of
    // the older one.
    expect(rows[0].getAttribute('aria-label')).toContain('Alarm')
    expect(rows[0].getAttribute('aria-label')).toContain('203.0.113.3')
    expect(rows[1].getAttribute('aria-label')).toContain('203.0.113.2')
  })

  it('a row click navigates to that flag/watch\'s tab; the dial itself never navigates once open', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()
    expect(appState.view).toBe('topography')

    const row = container.querySelector<HTMLButtonElement>('.dp-row')!
    row.click()
    flushSync()
    expect(appState.view).toBe('flags')
  })

  // #724: "the second click takes you to the thing you clicked on, in the
  // expansion" -- a flag row's click has to hand off *which* flag, not
  // just which tab, so Flags.svelte can open that flag's own drawer
  // (topologyNav.svelte.ts's pendingFlagId).
  it('a flag row click stashes that flag\'s id for the flags tab to open', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5'), flag('critical_port', '203.0.113.6')]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const rows = container.querySelectorAll<HTMLButtonElement>('.dp-row')
    rows[1].click()
    flushSync()
    expect(topologyNavState.pendingFlagId).toBe('f2')
  })

  it('a watch row click navigates to the watchlist tab', () => {
    watchlistState.entries = [watchEntry({ source: { ip: '192.168.1.9' } })]
    const { container } = render(Topography)
    flushSync()

    const watchDial = container.querySelectorAll<HTMLButtonElement>('.dial')[1]
    watchDial.click()
    flushSync()

    const row = container.querySelector<HTMLButtonElement>('.dp-row')!
    row.click()
    flushSync()
    expect(appState.view).toBe('watchlist')
  })

  // Same handoff as the flag row above, mirrored for pendingWatchId.
  it('a watch row click stashes that watch\'s id for the watchlist tab to open', () => {
    const entries = [watchEntry({ source: { ip: '192.168.1.9' } }), watchEntry({ source: { ip: '192.168.1.10' } })]
    watchlistState.entries = entries
    const { container } = render(Topography)
    flushSync()

    const watchDial = container.querySelectorAll<HTMLButtonElement>('.dial')[1]
    watchDial.click()
    flushSync()

    const rows = container.querySelectorAll<HTMLButtonElement>('.dp-row')
    rows[1].click()
    flushSync()
    expect(topologyNavState.pendingWatchId).toBe(entries[1].id)
  })

  it('the "and N more" row opens the tab with filters reset rather than any one row', () => {
    flagsState.list = Array.from({ length: 7 }, (_, i) => flag('critical_port', `203.0.113.${i}`))
    appState.setFilter('interface', 'bridge1')
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const more = container.querySelector<HTMLButtonElement>('.dp-more')!
    more.click()
    flushSync()
    expect(appState.view).toBe('flags')
    expect(appState.filters.interface).toBe('')
  })

  // Owner's ruling (#724): "the 'and N more' row opens the tab itself,
  // with nothing selected" -- it must never stash a pending id, or the
  // flags tab would open some arbitrary row's drawer instead of none.
  it('the "and N more" row leaves no pending selection behind', () => {
    flagsState.list = Array.from({ length: 7 }, (_, i) => flag('critical_port', `203.0.113.${i}`))
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const more = container.querySelector<HTMLButtonElement>('.dp-more')!
    more.click()
    flushSync()
    expect(topologyNavState.pendingFlagId).toBeNull()
  })

  it('clicking the dial again collapses the panel', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()
    expect(container.querySelector('.dial-panel')).not.toBeNull()

    flagsDial.click()
    flushSync()
    expect(container.querySelector('.dial-panel')).toBeNull()
    expect(flagsDial.getAttribute('aria-expanded')).toBe('false')
  })

  it('a click anywhere else on the page dismisses the panel without triggering what was under the pointer', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()
    expect(container.querySelector('.dial-panel')).not.toBeNull()

    let elsewhereClicked = 0
    const elsewhere = document.createElement('button')
    elsewhere.addEventListener('click', () => elsewhereClicked++)
    document.body.appendChild(elsewhere)

    elsewhere.click()
    flushSync()

    expect(container.querySelector('.dial-panel')).toBeNull()
    // The click was spent on dismissal -- it never reached elsewhere's
    // own handler.
    expect(elsewhereClicked).toBe(0)

    document.body.removeChild(elsewhere)
  })

  it('Escape closes the panel and returns focus to the dial that opened it', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5')]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const row = container.querySelector<HTMLButtonElement>('.dp-row')!
    expect(document.activeElement).toBe(row)

    row.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    flushSync()

    expect(container.querySelector('.dial-panel')).toBeNull()
    expect(document.activeElement).toBe(flagsDial)
  })

  it('at a count of zero, the panel still opens and says so in one line', () => {
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    const zero = container.querySelector('.dp-zero')
    expect(zero).not.toBeNull()
    expect(zero!.textContent).toContain('no open flags')
    expect(container.querySelectorAll('.dp-row').length).toBe(0)
  })

  it('the zero line names the last-cleared time when a flag has actually been cleared', () => {
    flagsState.list = [flag('critical_port', '203.0.113.5', { cleared: true, clearedAt: '2026-01-01T14:02:00Z' })]
    const { container } = render(Topography)
    flushSync()

    const flagsDial = container.querySelector<HTMLButtonElement>('.dial')!
    flagsDial.click()
    flushSync()

    expect(container.querySelector('.dp-zero')?.textContent).toContain('the last cleared at')
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

describe('the altitude slider (#648, named ends #682; joined to the city #869)', () => {
  it('renders one range input, seven stop symbols, and its two named ends, defaulting to the city (#869)', () => {
    const { container } = render(Topography)
    flushSync()

    const range = container.querySelector<HTMLInputElement>('.alt-range')
    expect(range).not.toBeNull()
    expect(range?.min).toBe('0')
    expect(range?.max).toBe('6') // three 2D stops, the city, then its own three (#869)
    expect(range?.value).toBe('3') // defaults to "city", centred on the axis

    const ticks = container.querySelectorAll('.tick')
    expect(ticks.length).toBe(7)
    expect(container.querySelector('.tick.diamond.on')).not.toBeNull() // the city's own atlas diamond

    // Ratified round-29: the two extremes are named, the middle stops
    // stay tick-only symbols -- never a full text label per stop.
    const ends = [...container.querySelectorAll('.alt-end')].map((n) => n.textContent?.trim())
    expect(ends).toEqual(['clients', 'street'])
  })

  it('the city stops swap the 2D stage for the city, and back (#863, #869)', () => {
    const { container } = render(Topography)
    flushSync()

    // The default is already a city stop.
    expect(container.querySelector('.city[data-stop="city"]')).not.toBeNull()
    expect(container.querySelector<HTMLElement>('.stage')?.hidden).toBe(true)
    expect(container.querySelector('.city .mini rect.viewport')).not.toBeNull()

    const range = container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = '6'
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    expect(container.querySelector('.city[data-stop="street"]')).not.toBeNull()

    range.value = '2'
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    expect(container.querySelector('.city')).toBeNull()
    expect(container.querySelector<HTMLElement>('.stage')?.hidden).toBe(false)
  })

  it('remembers the last stop across mounts, the same convention cityImportance.svelte.ts uses (#869)', () => {
    const first = render(Topography)
    flushSync()
    const range = first.container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = '0' // clients
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    first.unmount()

    expect(altitudeStopState.stop).toBe('clients')
    const second = render(Topography)
    flushSync()
    expect(second.container.querySelector<HTMLInputElement>('.alt-range')?.value).toBe('0')
  })
})

describe('crossing the altitude centre (#869)', () => {
  const oneLane: RouterIPAddress[] = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]

  function crossTo(container: HTMLElement, value: string) {
    const range = container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = value
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
  }

  it('keeps the selected lens when crossing the centre either way', () => {
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll('[aria-label="Map lenses"] button')].find((b) => b.textContent?.trim() === 'policy')!
    policyTab.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(policyTab.classList.contains('on')).toBe(true)

    crossTo(container, '2') // to zones: the 2D side
    expect(policyTab.classList.contains('on')).toBe(true)

    crossTo(container, '4') // back across, to borough
    expect(policyTab.classList.contains('on')).toBe(true)
  })

  it('hands a 2D reach across the centre to the same host, standing on it in the city', () => {
    zonesState.pushed = oneLane
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()
    crossTo(container, '2') // zones: the 2D map is the active side

    const hostLink = container.querySelector<SVGTSpanElement>('.host-link')!
    hostLink.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(container.querySelector('.membrane-layer')).not.toBeNull()

    crossTo(container, '3') // cross the centre into the city
    expect(container.querySelector('.membrane-layer')).toBeNull()
    expect(container.querySelector('.city .crumb')).not.toBeNull()
    expect(container.querySelector('.city .crumb')?.textContent).toContain('desk')
  })

  it('resets cleanly, rather than half-applying, when the reached host has no building to land on', () => {
    // MAX_BUILDINGS (lib/city/layout.ts) draws 8 hosts per plate; a 9th
    // exists in the 2D map's reach but has no building in the city.
    zonesState.pushed = oneLane
    appState.events = Array.from({ length: 9 }, (_, i) => event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `host-${i}` }))
    const { container } = render(Topography)
    flushSync()
    crossTo(container, '0') // clients: a 2D stop, so this request opens the 2D reach

    topologyNavState.pendingDescend = { zoneId: 'bridge1', host: 'host-8', ip: '10.0.1.28' }
    flushSync()
    expect(container.querySelector('.membrane-layer')).not.toBeNull()

    crossTo(container, '3') // cross the centre into the city
    expect(container.querySelector('.membrane-layer')).toBeNull()
    expect(container.querySelector('.city[data-stop="city"]')).not.toBeNull()
    expect(container.querySelector('.city .crumb')).toBeNull() // nothing to stand on -- resets, not half-applies
  })

  it('hands the city\'s own stand across the centre to a 2D reach on the same host', () => {
    zonesState.pushed = oneLane
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()
    // Already on the city (the default); stand on the host the same way
    // a flag's "where" link does.
    topologyNavState.pendingDescend = { zoneId: 'bridge1', host: 'desk', ip: '10.0.1.20' }
    flushSync()
    expect(container.querySelector('.city .crumb')).not.toBeNull()

    crossTo(container, '2') // cross the centre out to the 2D map
    expect(container.querySelector('.membrane-layer')).not.toBeNull()
    expect(container.querySelector('.here')?.textContent).toBe('desk')
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

describe('the zone card coverage badge (#682, ratified round-29)', () => {
  it('reads LOGGED BOTH WAYS in the healthy colour when both directions log', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [
      event({ inInterface: 'bridge1', srcIp: '192.168.1.50' }),
      event({ inInterface: 'wan1', srcIp: '8.8.8.8' }), // resolves wan1 as the WAN boundary
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge1|wan1', from: 'bridge1', to: 'wan1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true },
      { key: 'wan1|bridge1', from: 'wan1', to: 'bridge1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true },
    ]
    const { container } = render(Topography)
    flushSync()

    const badge = container.querySelector('.n-cov')
    expect(badge?.textContent).toBe('LOGGED BOTH WAYS')
    expect(badge?.classList.contains('cov-l')).toBe(true)
  })

  it('reads a DARK boundary in the alarm colour, not the healthy one', () => {
    zonesState.pushed = [{ address: '10.0.30.1/24', network: '10.0.30.0', interface: 'bridge2', comment: 'Guest' }]
    appState.events = [
      event({ inInterface: 'bridge2', srcIp: '10.0.30.9' }),
      event({ inInterface: 'wan1', srcIp: '8.8.8.8' }),
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge2|wan1', from: 'bridge2', to: 'wan1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
      { key: 'wan1|bridge2', from: 'wan1', to: 'bridge2', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    ]
    const { container } = render(Topography)
    flushSync()

    // Two lines, badge over detail (#682, ratified round-29) -- not one
    // sentence crammed into the badge itself.
    const badge = container.querySelector('.n-cov')
    expect(badge?.textContent).toBe('DARK BOTH WAYS')
    expect(badge?.classList.contains('cov-d')).toBe(true)
    expect(badge?.classList.contains('cov-l')).toBe(false)

    const zoneTexts = [...container.querySelectorAll('.n-sub')].map((n) => n.textContent)
    expect(zoneTexts).toContain('no log rule on this boundary')
  })
})

describe('degrading honestly without a pushed address table (#682, data gap #687; round 36 #802)', () => {
  it('never invents a subnet or a coverage verdict, and floats no note over the map', () => {
    zonesState.pushed = [] // no /ip address table pushed -- #687's data gap, not a rendering bug
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    policyState.anyPushed = false
    const { container } = render(Topography)
    flushSync()

    // Round 36 draws no note over the drawing at all: the statement
    // belongs on the router card, so round 29's floating pill is gone.
    expect(container.querySelector('.degraded')).toBeNull()

    // No fabricated subnet and no fabricated coverage badge.
    expect([...container.querySelectorAll('.n-cidr')].map((n) => n.textContent)).not.toContain('192.168.1.0/24')
    expect(container.querySelector('.n-cov')).toBeNull()
  })

  it('carries one statement on the router card, naming the missing push and the way to add it', () => {
    zonesState.pushed = []
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    const { container } = render(Topography)
    flushSync()

    const lines = [...container.querySelectorAll('.deg-t')].map((n) => n.textContent?.trim())
    expect(lines).toEqual(['no address table pushed — zones from boundaries', 'Run setup… ▸ adds it'])

    // The statement sits on the router card, not loose on the stage: it
    // is inside the waist island's own group.
    const waistCard = container.querySelector('.isl.waist')?.parentElement
    expect(waistCard?.querySelectorAll('.deg-t').length).toBe(2)

    // And the card grew to hold it rather than the text overrunning it
    // (round-36/README.md's own validation note).
    expect(container.querySelector('.isl.waist')?.getAttribute('height')).toBe('100')
  })

  it('opens the setup wizard from the statement rather than only naming it', () => {
    zonesState.pushed = []
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    wizardState.open = false
    const { container } = render(Topography)
    flushSync()

    const go = container.querySelector<SVGTSpanElement>('.deg-go')
    expect(go?.textContent).toBe('Run setup… ▸')
    go!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(wizardState.open).toBe(true)
    wizardState.open = false
  })

  it('reads "from boundaries" in every zone address slot, never a blank one', () => {
    zonesState.pushed = []
    appState.events = [
      event({ inInterface: 'bridge1', srcIp: '192.168.1.50' }),
      event({ inInterface: 'bridge2', srcIp: '192.168.2.50' }),
    ]
    const { container } = render(Topography)
    flushSync()

    // Both sibling tspans are drawn (the-whole.html:1026); `.stage.map-degraded`
    // is what picks which one shows, so the CSS toggle is asserted through
    // the root class rather than through textContent, which jsdom never
    // hides for a `display: none` descendant (vitest.config.ts leaves
    // `test.css` at its default `false`, same reason LiveTable.svelte.test.ts
    // gives for not asserting getComputedStyle here).
    expect(container.querySelector('.stage')?.classList.contains('map-degraded')).toBe(true)
    const degSlots = [...container.querySelectorAll('.zone .cidr-deg')].map((n) => n.textContent)
    expect(degSlots).toEqual(['from boundaries', 'from boundaries'])
    const vSlots = [...container.querySelectorAll('.zone .cidr-v')].map((n) => n.textContent)
    expect(vSlots).toEqual(['', ''])
  })

  it('says "no address pushed" in the wan card\'s slot, and the address once one is pushed', () => {
    // A public source makes ether1 the wan interface -- an observation,
    // not a probe (zones.svelte.ts).
    appState.events = [event({ inInterface: 'ether1', srcIp: '203.0.113.9' })]
    zonesState.pushed = []
    const degraded = render(Topography)
    flushSync()
    expect(degraded.container.querySelector('.stage')?.classList.contains('map-degraded')).toBe(true)
    const degradedCidr = internetCardCidr(degraded.container)
    expect(degradedCidr?.querySelector('.cidr-deg')?.textContent).toBe(' · no address pushed')
    expect(degradedCidr?.querySelector('.cidr-v')?.textContent).toBe(' · ')
    degraded.unmount()

    zonesState.pushed = [address({ interface: 'ether1', address: '203.0.113.7' })]
    const pushed = render(Topography)
    flushSync()
    expect(pushed.container.querySelector('.stage')?.classList.contains('map-degraded')).toBe(false)
    expect(internetCardCidr(pushed.container)?.querySelector('.cidr-v')?.textContent).toBe(' · 203.0.113.7')
  })

  it('drops the statement once an address table arrives, leaving no leftover note', () => {
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    zonesState.pushed = [address({ interface: 'bridge1', address: '192.168.1.0/24', comment: 'LAN' })]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.deg-t')).toBeNull()
    expect(container.querySelector('.stage')?.classList.contains('map-degraded')).toBe(false)
    expect(container.querySelector('.isl.waist')?.getAttribute('height')).toBe('68')
    expect([...container.querySelectorAll('.n-cidr .cidr-v')].map((n) => n.textContent)).toContain('192.168.1.0/24')
  })
})

describe('the lens selector, ported to the scene\'s own bottom-left bar (#682)', () => {
  it('renders the three lenses as .wlens2, not a top-right tab strip, and switches on click', () => {
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.lenses')).toBeNull() // the old top-right strip is gone
    const bar = container.querySelector('.wlens2')
    expect(bar).not.toBeNull()

    // Three exclusive base lenses, then the two overlays (#715 item 3).
    const tabs = [...bar!.querySelectorAll('[role="tablist"] button')].map((b) => b.textContent?.trim())
    expect(tabs).toEqual(['traffic', 'policy', 'coverage'])

    const policyTab = [...bar!.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'policy')!
    expect(policyTab.classList.contains('on')).toBe(false)
    policyTab.click()
    flushSync()
    expect(policyTab.classList.contains('on')).toBe(true)
  })
})

describe('the watcher dial\'s eye (#682, ported from the scene)', () => {
  it('draws the eye as a path and pupil, not the aggregate bar\'s "◉" text glyph', () => {
    const { container } = render(Topography)
    flushSync()

    const eye = container.querySelector('g.watch-sym')
    expect(eye).not.toBeNull()
    expect(eye?.querySelector('path')).not.toBeNull()
    expect(eye?.querySelector('circle')).not.toBeNull()
    expect(container.querySelector('text.watch-sym')).toBeNull()
  })
})

describe('the ascend control, ported inside the map\'s own flow (#682)', () => {
  it('renders inside .stage, not as a fixed pill over the whole card', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()

    container.querySelector<SVGTSpanElement>('.host-link')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    const stage = container.querySelector('.stage')
    expect(stage).not.toBeNull()
    const ascend = stage!.querySelector('.ascend')
    expect(ascend).not.toBeNull() // inside .stage, not a sibling of it
  })
})

describe('the round-30 layout (#699)', () => {
  // The lanes come from a pushed /ip address table plus observed
  // traffic, the same way the real map builds them.
  function pushLanes(n: number, hostName = (i: number) => `host-${i}`) {
    zonesState.pushed = Array.from({ length: n }, (_, i) => ({
      address: `10.0.${i + 1}.1/24`,
      network: `10.0.${i + 1}.0`,
      interface: `bridge${i + 1}`,
      comment: `Lane ${i + 1}`,
    }))
    appState.events = Array.from({ length: n }, (_, i) =>
      event({ inInterface: `bridge${i + 1}`, srcIp: `10.0.${i + 1}.20`, srcHostName: hostName(i) }),
    )
  }

  function cards(container: HTMLElement) {
    return [...container.querySelectorAll('.zone .isl')].map((r) => ({
      x: Number(r.getAttribute('x')),
      w: Number(r.getAttribute('width')),
      cx: Number(
        (r.closest('.zone')?.getAttribute('transform') ?? 'translate(0 0)').replace(/translate\(([-\d.]+).*/, '$1'),
      ),
    }))
  }

  // The defect: laneX() spread N lanes between two fixed x values, so
  // the pitch fell below the card's own width once there were five.
  // Five is also as many lanes as the map ever draws (zones.svelte.ts
  // caps the list), which is exactly the case that used to overlap.
  for (const n of [2, 3, 4, 5]) {
    it(`lays ${n} lanes out without the cards overlapping`, () => {
      pushLanes(n)
      const { container } = render(Topography)
      flushSync()

      const laid = cards(container)
      expect(laid.length).toBe(n)
      const edges = laid.map((c) => ({ l: c.cx + c.x, r: c.cx + c.x + c.w })).sort((a, b) => a.l - b.l)
      for (let i = 1; i < edges.length; i++) expect(edges[i].l).toBeGreaterThanOrEqual(edges[i - 1].r)
      // and the whole row stays on the 1400-unit stage
      expect(edges[0].l).toBeGreaterThanOrEqual(0)
      expect(edges[edges.length - 1].r).toBeLessThanOrEqual(1400)
    })
  }

  it('keeps round 30’s own pitch and card while the row fits', () => {
    pushLanes(4)
    const { container } = render(Topography)
    flushSync()

    const laid = cards(container).sort((a, b) => a.cx - b.cx)
    expect(laid.every((c) => c.w === 216)).toBe(true)
    expect(laid[1].cx - laid[0].cx).toBeCloseTo(271, 5)
    // centred on the waist, like the drawing's own 285..1116 row
    expect((laid[0].cx + laid[3].cx) / 2).toBeCloseTo(700, 5)
  })

  it('budgets the host row to the card rather than always drawing three', () => {
    pushLanes(2, () => 'a-really-long-workstation-hostname')
    zonesState.pushed = [
      { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
    ]
    appState.events = ['aa', 'bb', 'cc'].map((s, i) =>
      event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `${s}-a-really-long-workstation-hostname` }),
    )
    const { container } = render(Topography)
    flushSync()

    const row = container.querySelector('.zone .n-hosts')
    expect(row).not.toBeNull()
    // one name fits a 216-wide card at that length; the rest become +N,
    // and the clip is the backstop behind the estimate.
    expect(row?.querySelectorAll('.host-link').length).toBe(1)
    expect(row?.textContent).toContain('+2')
    expect(row?.getAttribute('clip-path')).toMatch(/^url\(#.+-hosts\)$/)
  })

  it('draws the aggregate bar flush with the card, 16 tall', () => {
    pushLanes(1)
    flagsState.list = [flag('port_scan', '10.0.1.20')]
    const { container } = render(Topography)
    flushSync()

    const bar = container.querySelector('.zone .hb')
    expect(bar).not.toBeNull()
    // fullBarPath starts at x0 + h/2 with h = 16 against a -108 edge
    expect(bar?.getAttribute('d')).toMatch(/^M -100 0 H 100 /)
    expect(bar?.getAttribute('transform')).toBe('translate(0 110)')
  })

  it('draws no floating "not drawn" or "unjudged" caption on the map', () => {
    // Both captions need their trigger present: observed pairs with no
    // rule table pushed ("unjudged"), and more pairs than the 12-edge
    // calm draws ("+N pairs not drawn").
    pushLanes(5)
    const ifaces = ['bridge1', 'bridge2', 'bridge3', 'bridge4', 'bridge5', 'ether1']
    const pairs: ClientEvent[] = []
    for (const from of ifaces) {
      for (const to of ifaces) {
        if (from === to) continue
        pairs.push(event({ inInterface: from, outInterface: to, srcIp: '10.0.1.20', dstPort: 443, action: 'accept' }))
      }
    }
    appState.events = [...appState.events, ...pairs]
    expect(policyState.anyPushed).toBe(false)
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelectorAll('.edge-g').length).toBeGreaterThan(0)

    const texts = [...container.querySelectorAll('.stage svg text')].map((t) => t.textContent ?? '')
    expect(texts.some((t) => /pairs? not drawn/.test(t))).toBe(false)
    expect(texts.some((t) => /^unjudged — push the rule table/.test(t))).toBe(false)
  })

  it('puts every edge label on a plate rather than bare on its line', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' }),
      event({ inInterface: 'ether1', outInterface: 'bridge1', srcIp: '203.0.113.9', dstPort: 445, action: 'drop' }),
    ]
    const { container } = render(Topography)
    flushSync()

    const badges = [...container.querySelectorAll('.edge-badge')]
    expect(badges.length).toBeGreaterThan(0)
    for (const b of badges) {
      const plate = b.parentElement?.querySelector('.edge-plate')
      expect(plate).not.toBeNull()
      expect(Number(plate?.getAttribute('width'))).toBeGreaterThan(0)
    }
  })

  it('draws the zones stop\'s own flat ground plan -- a card per zone with a host count, no dots, no per-host names (#852, #869)', () => {
    pushLanes(3)
    const { container } = render(Topography)
    flushSync()

    // Present at every altitude, like every other camera layer -- the
    // stylesheet is what shows it only at zones (cam-zones).
    const cards = container.querySelectorAll('.ground-flat .gf-card')
    expect(cards.length).toBe(3)
    for (const c of cards) {
      expect(c.querySelector('.gf-count')?.textContent).toMatch(/^\d+ hosts?$/)
      expect(c.querySelector('.n-hosts')).toBeNull()
      expect(c.querySelector('circle')).toBeNull()
    }
    // The full card (host names included) stays available for clients
    // and services -- "hosts appear at clients" -- so it is still drawn,
    // just hidden by the stylesheet while zones is the active stop.
    expect(container.querySelectorAll('.zone .isl-card').length).toBe(3)
    expect(container.querySelectorAll('.zone .n-hosts').length).toBe(3)
  })

  it('adds a services layer and a client tier rather than scaling the map up', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', srcHostName: 'desk', dstPort: 443, action: 'accept' }),
    ]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.camera .svc')).not.toBeNull()
    expect(container.querySelector('.camera .cli')).not.toBeNull()
    expect(container.querySelectorAll('.cli .c-label').length).toBeGreaterThan(0)
    expect(container.querySelector('.svc .svc-t')?.textContent).toContain(':443')
  })

  it('gives the internet island an aggregate bar, from public addresses only', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20' })]
    flagsState.list = [flag('port_scan', '203.0.113.77'), flag('port_scan', '10.0.1.20')]
    const { container } = render(Topography)
    flushSync()

    const island = container.querySelector('.passive .hbar-g .hb')
    expect(island).not.toBeNull()
    // one flag is public, one is inside a lane -- the island counts only
    // the public one, and never "whatever no lane claimed".
    const label = island?.closest('.hbar-g')?.getAttribute('aria-label') ?? ''
    expect(label).toMatch(/1 open flag/)
  })

  it('never lets two edge-label plates cover each other', () => {
    // Five lanes all talking to the internet put five labels in one
    // corridor: staggering alone cannot separate them, so the placement
    // pass has to.
    zonesState.pushed = Array.from({ length: 5 }, (_, i) => ({
      address: `10.0.${i + 1}.1/24`,
      network: `10.0.${i + 1}.0`,
      interface: `bridge${i + 1}`,
      comment: `Lane ${i + 1}`,
    }))
    appState.events = Array.from({ length: 5 }, (_, i) =>
      event({ inInterface: `bridge${i + 1}`, outInterface: 'ether1', srcIp: `10.0.${i + 1}.20`, dstPort: 443, action: 'accept' }),
    )
    const { container } = render(Topography)
    flushSync()

    const plates = [...container.querySelectorAll('.edge-plate')].map((p) => ({
      x: Number(p.getAttribute('x')),
      y: Number(p.getAttribute('y')),
      w: Number(p.getAttribute('width')),
      h: Number(p.getAttribute('height')),
    }))
    expect(plates.length).toBeGreaterThan(1)
    for (let a = 0; a < plates.length; a++) {
      for (let b = a + 1; b < plates.length; b++) {
        const p1 = plates[a]
        const p2 = plates[b]
        const overlaps = p1.x < p2.x + p2.w && p2.x < p1.x + p1.w && p1.y < p2.y + p2.h && p2.y < p1.y + p1.h
        expect(overlaps).toBe(false)
      }
    }
  })

  it('holds no-overlap even at the 12-edge cap with mixed accept/drop pairs converging on a shared lane', () => {
    // #715: the owner saw traffic chips stacking on the running build.
    // Stress the placement pass at its real ceiling -- five lanes,
    // several source lanes all crossing to the same destination lane
    // (the zone-side analogue of the internet corridor), and a mix of
    // accepted/dropped verdicts -- to confirm the corridor and the
    // freestanding push-down pass still hold together at scale.
    zonesState.pushed = Array.from({ length: 5 }, (_, i) => ({
      address: `10.0.${i + 1}.1/24`,
      network: `10.0.${i + 1}.0`,
      interface: `bridge${i + 1}`,
      comment: `Lane ${i + 1}`,
    }))
    appState.events = [
      event({ inInterface: 'ether1', outInterface: 'bridge1', srcIp: '203.0.113.5', dstPort: 443, action: 'accept' }),
      ...Array.from({ length: 300 }, () => event({ inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstPort: 443, action: 'accept' })),
      ...Array.from({ length: 250 }, () => event({ inInterface: 'bridge3', outInterface: 'bridge1', srcIp: '10.0.3.20', dstPort: 80, action: 'accept' })),
      ...Array.from({ length: 200 }, () => event({ inInterface: 'bridge4', outInterface: 'bridge1', srcIp: '10.0.4.20', dstPort: 22, action: 'accept' })),
      ...Array.from({ length: 150 }, () => event({ inInterface: 'bridge5', outInterface: 'bridge1', srcIp: '10.0.5.20', dstPort: 3389, action: 'drop' })),
      ...Array.from({ length: 100 }, () => event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.1.20', dstPort: 8080, action: 'accept' })),
    ]
    const { container } = render(Topography)
    flushSync()

    const plates = [...container.querySelectorAll('.edge-plate')].map((p) => ({
      x: Number(p.getAttribute('x')),
      y: Number(p.getAttribute('y')),
      w: Number(p.getAttribute('width')),
      h: Number(p.getAttribute('height')),
    }))
    expect(plates.length).toBeGreaterThan(2)
    for (let a = 0; a < plates.length; a++) {
      for (let b = a + 1; b < plates.length; b++) {
        const p1 = plates[a]
        const p2 = plates[b]
        const overlaps = p1.x < p2.x + p2.w && p2.x < p1.x + p1.w && p1.y < p2.y + p2.h && p2.y < p1.y + p1.h
        expect(overlaps).toBe(false)
      }
    }
  })

  it('colours a planned traffic edge with the lane it touches, not one shared grey (#715)', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    const line = container.querySelector('.redge')
    expect(line).not.toBeNull()
    expect(line?.getAttribute('style')).toContain('stroke: var(--lane-lan)')
  })

  it('keeps the reserved alarm colour on an unplanned traffic edge rather than a lane ink (#715)', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    policyState.anyPushed = true
    policyState.edges = [
      {
        key: 'bridge1|ether1',
        from: 'bridge1',
        to: 'ether1',
        accepted: false,
        refused: true,
        acceptPorts: [],
        refusePorts: [':445'],
        comment: '',
        ruleCount: 1,
        logged: true,
      },
    ]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 445, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    const line = container.querySelector('.redge.alarm')
    expect(line).not.toBeNull()
    expect(line?.getAttribute('style') ?? '').not.toContain('stroke:')
  })
})


describe('#723: nodes stop clashing with the scene\'s own floor at the altitude extremes', () => {
  function pushOneLaneWithHosts(n: number) {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = Array.from({ length: n }, (_, i) =>
      event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `host-${i}` }),
    )
  }

  it('keeps every client-tier dot and label, including "+n more", well clear of the 720 floor', () => {
    // Five hosts: three drawn plus "+2 more" -- the worst case, since
    // "+n more" sat lowest of everything in the tier (baseline 716
    // against a 720 floor before this fix).
    pushOneLaneWithHosts(5)
    const { container } = render(Topography)
    flushSync()

    const range = container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = '0' // "clients" -- the tier's own altitude
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()

    const labelYs = [...container.querySelectorAll('.cli .c-label')].map((n) => Number(n.getAttribute('y')))
    expect(labelYs.length).toBeGreaterThan(0)
    for (const y of labelYs) expect(y).toBeLessThanOrEqual(700)

    const dotBottoms = [...container.querySelectorAll('.cli .c-dot')].map(
      (n) => Number(n.getAttribute('cy')) + Number(n.getAttribute('r')),
    )
    expect(dotBottoms.length).toBeGreaterThan(0)
    for (const bottom of dotBottoms) expect(bottom).toBeLessThanOrEqual(700)
  })

})

describe('#723: a lane\'s port list gets a visible tie to its own card ("ports floating in the wind")', () => {
  it('draws a leader from every services-tier label down to the card it describes', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    const labels = [...container.querySelectorAll('.svc .svc-t')]
    expect(labels.length).toBeGreaterThan(0)
    const leaders = [...container.querySelectorAll('.svc .svc-leader')]
    expect(leaders.length).toBe(labels.length)

    for (const leader of leaders) {
      // The leader's far end lands right at the card's own top edge
      // (y=490 in this scene's fixed geometry), not floating mid-air.
      expect(Number(leader.getAttribute('y2'))).toBeCloseTo(489, 0)
      expect(leader.getAttribute('x1')).toBe(leader.getAttribute('x2'))
    }
  })
})

describe('#723: the dials sit just under the top bar rather than well below it', () => {
  it('keeps the dials\' own offset small enough to read as "just under", not a floating pair', () => {
    const m = componentSource.match(/\.dials\s*{[^}]*top:\s*([\d.]+)px/)
    expect(m).not.toBeNull()
    const top = Number(m![1])
    // Was 108px (the mockup's own figure, measured from a differently-
    // structured layout -- see the component's own comment). Clear of 0
    // (#721's own concern: don't crowd the bar) but nowhere near the old
    // value.
    expect(top).toBeGreaterThan(0)
    expect(top).toBeLessThanOrEqual(24)
  })
})

describe('#723: clicking (or keying into) a node opens the reach, not the stream', () => {
  it('opens the membrane when a client-tier node is activated, at the clients altitude', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()

    const range = container.querySelector<HTMLInputElement>('.alt-range')!
    range.value = '0'
    range.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()

    const dot = container.querySelector<SVGCircleElement>('.cli .c-dot')
    expect(dot).not.toBeNull()
    dot!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    expect(container.querySelector('.membrane-layer')).not.toBeNull()
    expect(appState.view).toBe('topography') // never resolved to the stream
  })

  it('sends Space on a card host-link to the same place Enter and the pointer already go', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()

    const hostLink = container.querySelector<SVGTSpanElement>('.host-link')
    expect(hostLink).not.toBeNull()
    hostLink!.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true }))
    flushSync()

    expect(container.querySelector('.membrane-layer')).not.toBeNull()
    expect(appState.view).toBe('topography')
  })
})

describe('#715 follow-up: the edge-plate reads over any lane\'s ink, not just the empty map', () => {
  it('backs the plate with an elevated surface rather than the scene background itself', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.edge-plate')).not.toBeNull()
    // The regression this guards: a plate whose fill token is the same
    // as the page's own background is not a plate at all -- it is the
    // void, and anything behind it (a coloured lane's own line, since
    // #715 gave observed edges that ink) reads straight through.
    const plateRule = componentSource.match(/\.edge-plate\s*{([^}]*)}/)
    expect(plateRule).not.toBeNull()
    expect(plateRule![1]).not.toMatch(/fill:\s*var\(--bg\);/)
    expect(plateRule![1]).toMatch(/fill:\s*var\(--bg-elevated\)/)
  })

  it('gives the aggregate flag/watch chip enough of an opaque backing that a crossing line cannot show through it', () => {
    // #715 already ordered this bar after every edge line (document
    // order = paint order); its own defect was a 10%-into-transparent
    // fill, not stacking -- 90% see-through lets whatever is underneath
    // bleed straight through the count it is meant to carry.
    const hbRule = componentSource.match(/\.hb-f\s*{([^}]*)}/)
    expect(hbRule).not.toBeNull()
    const fillLine = hbRule![1].match(/fill:\s*[^;]+;/)?.[0] ?? ''
    expect(fillLine).not.toMatch(/,\s*transparent\)/)
    expect(fillLine).toMatch(/var\(--bg-elevated\)/)
  })
})

describe('#723: lines are painted before labels in every lens, so a line can never cover one', () => {
  function linesComeBeforeEveryPlate(container: HTMLElement, lineSelector: string) {
    const lines = [...container.querySelectorAll(lineSelector)]
    const plates = [...container.querySelectorAll('.edge-plate')]
    expect(lines.length).toBeGreaterThan(1)
    expect(plates.length).toBeGreaterThan(1)
    for (const line of lines) {
      for (const plate of plates) {
        // DOCUMENT_POSITION_FOLLOWING (4): plate comes after line.
        expect(line.compareDocumentPosition(plate) & 4).toBeTruthy()
      }
    }
  }

  it('holds for the traffic lens, two crossing pairs with badges', () => {
    zonesState.pushed = [
      { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
    ]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' }),
      event({ inInterface: 'bridge2', outInterface: 'ether1', srcIp: '10.0.2.20', dstPort: 80, action: 'accept' }),
    ]
    const { container } = render(Topography)
    flushSync()

    linesComeBeforeEveryPlate(container, '.redge')
  })

  it('holds for the policy lens, two accepted pairs with port badges', () => {
    zonesState.pushed = [
      { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
    ]
    appState.events = [
      event({ inInterface: 'bridge1', srcIp: '10.0.1.20' }),
      event({ inInterface: 'bridge2', srcIp: '10.0.2.20' }),
      event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge1|ether1', from: 'bridge1', to: 'ether1', accepted: true, refused: false, acceptPorts: [':443'], refusePorts: [], comment: '', ruleCount: 1, logged: true },
      { key: 'bridge2|ether1', from: 'bridge2', to: 'ether1', accepted: true, refused: false, acceptPorts: [':80'], refusePorts: [], comment: '', ruleCount: 1, logged: true },
    ]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    linesComeBeforeEveryPlate(container, '.edge')
  })

  it('holds for the coverage lens, two dark boundary-directions', () => {
    zonesState.pushed = [
      { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
    ]
    appState.events = [
      event({ inInterface: 'bridge1', srcIp: '10.0.1.20' }),
      event({ inInterface: 'bridge2', srcIp: '10.0.2.20' }),
      event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge1|ether1', from: 'bridge1', to: 'ether1', accepted: true, refused: false, acceptPorts: [':443'], refusePorts: [], comment: '', ruleCount: 1, logged: false },
      { key: 'bridge2|ether1', from: 'bridge2', to: 'ether1', accepted: true, refused: false, acceptPorts: [':80'], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    ]
    const { container } = render(Topography)
    flushSync()

    const coverageTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'coverage')
    coverageTab!.click()
    flushSync()

    linesComeBeforeEveryPlate(container, '.cedge')
  })
})

// #726. Crossing is fine and unavoidable on a map like this; running
// along the same path is not, because neither line can then be
// followed. The difference is a sustained stretch rather than a touch,
// so the measurement is how much of one line's run lies within a few
// units of another's -- two lines that cross dip close once and part
// again; two that are smeared together never part. Comparing the `d`
// strings would pass while the map still looks like one thick line.
function samplePath(d: string, n = 61): { x: number; y: number }[] {
  const v = (d.match(/-?\d+(?:\.\d+)?/g) ?? []).map(Number)
  const pts: { x: number; y: number }[] = []
  for (let i = 0; i < n; i++) {
    const t = i / (n - 1)
    const u = 1 - t
    if (d.includes('C')) {
      const [x0, y0, x1, y1, x2, y2, x3, y3] = v
      pts.push({
        x: u * u * u * x0 + 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t * x3,
        y: u * u * u * y0 + 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t * y3,
      })
    } else {
      const [x0, y0, x1, y1, x2, y2] = v
      pts.push({ x: u * u * x0 + 2 * u * t * x1 + t * t * x2, y: u * u * y0 + 2 * u * t * y1 + t * t * y2 })
    }
  }
  return pts
}

/** The fraction of one path's run that lies within `gap` map units of
 * the other -- 0 for lines that never meet, a blip for a crossing, and
 * most of the run for two lines drawn along each other. */
function sharedRun(a: string, b: string, gap = 4): number {
  const pa = samplePath(a)
  const pb = samplePath(b)
  let near = 0
  for (const p of pa) {
    const closest = Math.min(...pb.map((q) => Math.hypot(p.x - q.x, p.y - q.y)))
    if (closest < gap) near++
  }
  return near / pa.length
}

const SMEARED = 0.15

function pathsOf(container: HTMLElement, selector: string): string[] {
  return [...container.querySelectorAll(selector)].map((p) => p.getAttribute('d') ?? '')
}

describe('#726: distinct edges are not drawn along each other', () => {
  const twoLanes: RouterIPAddress[] = [
    { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
    { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
  ]

  const seenOnBothLanes = () => [
    event({ inInterface: 'bridge1', srcIp: '10.0.1.20' }),
    event({ inInterface: 'bridge2', srcIp: '10.0.2.20' }),
    event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
  ]

  function policyEdge(from: string, to: string, ports: string[]) {
    return {
      key: `${from}|${to}`,
      from,
      to,
      accepted: true,
      refused: false,
      acceptPorts: ports,
      refusePorts: [],
      comment: '',
      ruleCount: 1,
      logged: true,
    }
  }

  it('two lanes heading for the internet do not share the waist corridor', () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [policyEdge('bridge1', 'ether1', [':443']), policyEdge('bridge2', 'ether1', [':80'])]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    const [one, two] = pathsOf(container, '.edge')
    expect(one).toBeTruthy()
    expect(two).toBeTruthy()
    expect(sharedRun(one, two)).toBeLessThan(SMEARED)
  })

  it("a lane's edge to anywhere does not lie along its own edge to the internet", () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [policyEdge('bridge1', 'ether1', [':443']), policyEdge('bridge1', '', [':53'])]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    const [toInternet, toAnywhere] = pathsOf(container, '.edge')
    expect(toInternet).toBeTruthy()
    expect(toAnywhere).toBeTruthy()
    expect(sharedRun(toInternet, toAnywhere)).toBeLessThan(SMEARED)
  })

  it('a crossing is not counted as a smear: two lanes to opposite sides stay distinct', () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [policyEdge('bridge1', 'bridge2', [':445']), policyEdge('bridge2', 'bridge1', [':22'])]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    const [there, back] = pathsOf(container, '.edge')
    expect(sharedRun(there, back)).toBeLessThan(SMEARED)
  })

  function refusedEdge(from: string, to: string, ports: string[]) {
    return {
      key: `${from}|${to}`,
      from,
      to,
      accepted: false,
      refused: true,
      acceptPorts: [],
      refusePorts: ports,
      comment: '',
      ruleCount: 1,
      logged: true,
    }
  }

  // The gate caught this on the real map when the unit cases above did
  // not: they only ever hung the "anywhere" edge off lane 1, whose slot
  // sits at one end of the fan. A lane nearer the middle ends its limb
  // close to the waist, which is where an "anywhere" edge dies, so the
  // clearance has to hold for every slot rather than the first one.
  for (const lane of [1, 2, 3]) {
    it(`lane ${lane}'s edge to anywhere clears its own limb, whichever slot the lane holds`, () => {
      zonesState.pushed = Array.from({ length: 3 }, (_, i) => ({
        address: `10.0.${i + 1}.1/24`,
        network: `10.0.${i + 1}.0`,
        interface: `bridge${i + 1}`,
        comment: `Lane ${i + 1}`,
      }))
      appState.events = [
        ...Array.from({ length: 3 }, (_, i) => event({ inInterface: `bridge${i + 1}`, outInterface: 'ether1', srcIp: `10.0.${i + 1}.20` })),
        event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
      ]
      policyState.anyPushed = true
      policyState.edges = [
        ...Array.from({ length: 3 }, (_, i) => policyEdge(`bridge${i + 1}`, 'ether1', [':443'])),
        policyEdge(`bridge${lane}`, '', [':53']),
      ]
      const { container } = render(Topography)
      flushSync()
      const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
      policyTab!.click()
      flushSync()

      const edges = pathsOf(container, '.edge')
      expect(edges.length).toBe(4)
      for (let a = 0; a < edges.length; a++) {
        for (let b = a + 1; b < edges.length; b++) {
          expect(sharedRun(edges[a], edges[b])).toBeLessThan(SMEARED)
        }
      }
    })
  }

  it('a five-lane estate bundles the corridor and fans only at the waist', () => {
    // #726's own spec: five lanes to the internet, two of them answered
    // back, and one lane's own "reaches anywhere" edge alongside -- every
    // pairwise sharedRun among the drawn edges stays under SMEARED.
    zonesState.pushed = Array.from({ length: 5 }, (_, i) => ({
      address: `10.0.${i + 1}.1/24`,
      network: `10.0.${i + 1}.0`,
      interface: `bridge${i + 1}`,
      comment: `Lane ${i + 1}`,
    }))
    appState.events = [
      ...Array.from({ length: 5 }, (_, i) => event({ inInterface: `bridge${i + 1}`, outInterface: 'ether1', srcIp: `10.0.${i + 1}.20` })),
      event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
    ]
    policyState.anyPushed = true
    policyState.edges = [
      ...Array.from({ length: 5 }, (_, i) => policyEdge(`bridge${i + 1}`, 'ether1', [':443'])),
      policyEdge('ether1', 'bridge1', [':8080']),
      policyEdge('ether1', 'bridge2', [':8443']),
      policyEdge('bridge1', '', [':53']),
    ]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    const edges = pathsOf(container, '.edge')
    expect(edges.length).toBe(8)
    for (let a = 0; a < edges.length; a++) {
      for (let b = a + 1; b < edges.length; b++) {
        expect(sharedRun(edges[a], edges[b])).toBeLessThan(SMEARED)
      }
    }
  })

  it('two inbound refusals to different lanes stop coinciding at the top of the waist', () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [refusedEdge('ether1', 'bridge1', [':3389']), refusedEdge('ether1', 'bridge2', [':22'])]
    const { container } = render(Topography)
    flushSync()
    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('.wlens2 button')].find((b) => b.textContent?.trim() === 'policy')
    policyTab!.click()
    flushSync()

    const [toBridge1, toBridge2] = pathsOf(container, '.edge')
    expect(toBridge1).toBeTruthy()
    expect(toBridge2).toBeTruthy()
    expect(sharedRun(toBridge1, toBridge2)).toBeLessThan(SMEARED)
  })
})

describe('#715 item 9: the zone card stops where round 30 stops', () => {
  it('draws no "events this window" line on a zone card', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    // Round 30's card is name, subnet, hosts, coverage badge and the
    // aggregate bar (the-whole.html:1002-1015). The build had a fifth
    // line the mockup draws nowhere. Asserted on the rendered text
    // rather than the source, so reintroducing it anywhere on the card
    // fails rather than only reintroducing this exact element.
    expect(container.querySelector('.zone')).not.toBeNull()
    const cardText = [...container.querySelectorAll('.zone .isl-card text')].map((t) => t.textContent ?? '').join(' | ')
    expect(cardText).not.toMatch(/events this window/)
  })
})

describe('#715 item 7 / #701 fact 2: the waist card says what round 30 says', () => {
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

  function rule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
    return {
      ordinal: 0,
      comment: '',
      chain: 'forward',
      action: 'drop',
      srcAddressList: '',
      logPrefix: '',
      log: true,
      ...over,
    }
  }

  function waistText(container: HTMLElement): string {
    const card = container.querySelector('.isl.waist')?.parentElement
    return card?.querySelector('.n-sub')?.textContent?.trim() ?? ''
  }

  it('draws version, the waist and the enabled rule count, in round 30\'s order', () => {
    appState.devices = [router({ routerosVersion: '7.20.1' })]
    policyState.byDevice = { router1: [rule({ ordinal: 0 }), rule({ ordinal: 1 }), rule({ ordinal: 2 })] }
    const { container } = render(Topography)
    flushSync()

    expect(waistText(container)).toBe('RouterOS 7.20.1 · the waist · 3 rules')
  })

  it('leaves the version out rather than inventing one, when the router has not reported it', () => {
    appState.devices = [router()]
    policyState.byDevice = { router1: [rule()] }
    const { container } = render(Topography)
    flushSync()

    // Singular, and no leading "RouterOS" fragment at all -- not
    // "RouterOS unknown", which would read as a version the router gave us.
    expect(waistText(container)).toBe('the waist · 1 rule')
  })

  it('says nothing about rules until the table has actually been pushed', () => {
    // The regression that matters: "0 rules" about a router with a full
    // rule set is our own silence reported as a fact about the network.
    appState.devices = [router({ routerosVersion: '7.20.1' })]
    policyState.byDevice = {}
    const { container } = render(Topography)
    flushSync()

    const text = waistText(container)
    expect(text).toBe('RouterOS 7.20.1 · the waist')
    expect(text).not.toMatch(/rule/)
  })

  it('counts a pushed but empty table as zero, because that is a real answer', () => {
    appState.devices = [router()]
    policyState.byDevice = { router1: [] }
    const { container } = render(Topography)
    flushSync()

    expect(waistText(container)).toBe('the waist · 0 rules')
  })

  it('leaves disabled rules out of the count, and counts a rule that never mentioned it', () => {
    appState.devices = [router()]
    policyState.byDevice = {
      router1: [rule({ ordinal: 0, disabled: false }), rule({ ordinal: 1, disabled: true }), rule({ ordinal: 2 })],
    }
    const { container } = render(Topography)
    flushSync()

    // Two: the explicitly enabled one and the one from a push made
    // before the field existed. Absent must not read as disabled.
    expect(waistText(container)).toBe('the waist · 2 rules')
  })

  it('counts only the primary device\'s rules, not the estate\'s', () => {
    appState.devices = [router(), router({ id: 'router2', name: 'edge', lastSeen: '2026-01-02T00:00:00Z' })]
    policyState.byDevice = { router1: [rule()], router2: [rule(), rule(), rule(), rule()] }
    const { container } = render(Topography)
    flushSync()

    // router1 is the primary: both are configured, and it has the later
    // lastSeen.
    expect(waistText(container)).toBe('the waist · 1 rule')
  })

  it('no longer draws the live events/s figure round 30 draws nowhere on this node', () => {
    appState.devices = [router({ routerosVersion: '7.20.1' })]
    appState.stats = { eventsPerSecond: 34 } as never
    policyState.byDevice = { router1: [rule()] }
    const { container } = render(Topography)
    flushSync()

    expect(waistText(container)).not.toMatch(/events\/s/)
  })
})

describe('#715 items 10 and 11: two treatments Fable ruled on, 2026-09-03', () => {
  const oneLane: RouterIPAddress[] = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]

  it('lists host names plainly, with no per-name dot and one target each', () => {
    zonesState.pushed = oneLane
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', srcHostName: 'tom-desktop', dstPort: 443 }),
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.21', srcHostName: 'phone-tom', dstPort: 443 }),
    ]
    const { container } = render(Topography)
    flushSync()

    const hosts = container.querySelector('.n-hosts')
    expect(hosts).not.toBeNull()
    // No round ever drew a text dot here; #648's "node symbols bigger"
    // was about the map's own circles.
    expect(hosts!.textContent).not.toMatch(/●/)
    expect(container.querySelector('.host-dot')).toBeNull()
    // And one focusable target per name, not two. The dot was
    // role="button" tabindex="0" aria-hidden="true" at once -- focusable
    // yet hidden from assistive tech, doubling the tab stops per host.
    const targets = hosts!.querySelectorAll('[role="button"]')
    expect(targets.length).toBe(2)
    expect([...targets].every((t) => t.getAttribute('aria-hidden') !== 'true')).toBe(true)
  })

  it('gives the fifth lane its own ink rather than the one that means watchers', () => {
    // --marked is the watch chips' fill and the watch half of every
    // aggregate bar on this same screen. A lane wearing it made one
    // colour carry two meanings.
    const inks = componentSource.match(/const LANE_INKS = \[([^\]]*)\]/)
    expect(inks).not.toBeNull()
    expect(inks![1]).not.toMatch(/--marked/)
    expect(inks![1]).toMatch(/var\(--lane-5\)/)
    expect(inks![1].split(',').length).toBe(5)
    // That the token is actually defined, and defined as the validated
    // colour, is asserted in style_guard_test.go: Vite's ?raw import
    // hands a .css file back empty, and a Go test can read both files
    // as text.
  })

  it('does not paint the fifth lane in the watch chips\' own fill', () => {
    zonesState.pushed = ['bridge1', 'bridge2', 'bridge3', 'bridge4', 'bridge5'].map((iface, i) => ({
      address: `10.0.${i + 1}.1/24`,
      network: `10.0.${i + 1}.0`,
      interface: iface,
      comment: `Lane ${i + 1}`,
    }))
    appState.events = ['bridge1', 'bridge2', 'bridge3', 'bridge4', 'bridge5'].flatMap((iface, i) =>
      Array.from({ length: 5 - i }, () => event({ inInterface: iface, outInterface: 'ether1', srcIp: `10.0.${i + 1}.20`, dstPort: 443 })),
    )
    const { container } = render(Topography)
    flushSync()

    const dots = [...container.querySelectorAll('.zone .isl-card circle')].map((c) => c.getAttribute('fill'))
    expect(dots.length).toBe(5)
    expect(dots[4]).toBe('var(--lane-5)')
    expect(dots).not.toContain('var(--marked)')
  })
})

describe('#701: the reach names its busiest pathway, and says the ranking is weighted', () => {
  // Enter the reach the way a reader does, then read the crumb.
  function openReach(events: ClientEvent[]) {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = events
    const { container } = render(Topography)
    flushSync()
    const hostLink = container.querySelector<SVGTSpanElement>('.host-link')
    hostLink!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    return container
  }

  function busiestLine(container: HTMLElement): string | null {
    const subs = [...container.querySelectorAll('.sub')].map((s) => s.textContent?.replace(/\s+/g, ' ').trim() ?? '')
    return subs.find((s) => s.startsWith('the busiest pathway')) ?? null
  }

  function talk(over: Partial<ClientEvent>): ClientEvent {
    return event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'cam-porch', receivedAt: Date.now(), ...over })
  }

  it('names the pathway, its port and its outcome, and says the ranking is weighted toward now', () => {
    const container = openReach([
      talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 445, protocol: 'tcp', action: 'drop' }),
      talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 445, protocol: 'tcp', action: 'drop' }),
    ])

    const line = busiestLine(container)
    // The owner's own constraint, made regression-proof: the sentence
    // must say the ranking is weighted, not merely imply "now".
    expect(line).toContain('weighted toward now')
    expect(line).toContain('cam-porch → nas')
    expect(line).toContain('tcp/445')
    expect(line).toContain('refused')
  })

  it('flips the arrow when the far side started it', () => {
    // The centred host is cam-porch, put on the card by the one event
    // it sent. Three inbound knocks against that one outbound, all
    // equally recent, so the inbound strand wins outright rather than
    // by a tie-break -- otherwise the arrow direction this test is
    // about would not be the thing under test.
    const inbound = (id: number) =>
      talk({
        id,
        srcIp: '10.0.9.9',
        srcHostName: 'scanner',
        dstIp: '10.0.1.20',
        dstHostName: 'cam-porch',
        inInterface: 'bridge2',
        outInterface: 'bridge1',
        dstPort: 22,
        protocol: 'tcp',
        action: 'drop',
      })
    const container = openReach([
      talk({ id: 1, outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 443, action: 'accept' }),
      inbound(2),
      inbound(3),
      inbound(4),
    ])

    const line = busiestLine(container)
    expect(line).toContain('scanner → cam-porch')
    expect(line).not.toContain('cam-porch → scanner')
    expect(line).toContain('tcp/22')
    expect(line).toContain('refused')
  })

  it('drops the port clause entirely when the traffic carried no port', () => {
    // ICMP and its kin. A dangling separator would read as a missing
    // fact rather than an absent one.
    const container = openReach([
      talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', protocol: 'icmp', action: 'accept' }),
    ])

    const line = busiestLine(container)
    expect(line).toContain('cam-porch → nas')
    expect(line).not.toMatch(/\/\d/)
    expect(line).not.toMatch(/· ·/)
    expect(line).toMatch(/nas · accepted$/)
  })

  it('says nothing at all when nothing was observed, leaving the one honest empty state', () => {
    const container = openReach([talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 443 })])
    // Re-enter with an empty buffer: the reach is open, the buffer is not.
    appState.events = []
    flushSync()

    expect(busiestLine(container)).toBeNull()
    expect(container.textContent).toContain('nothing observed this window')
  })
})

describe('#715 item 4: the worst unplanned flow gets round 30\'s own card', () => {
  const twoLanes: RouterIPAddress[] = [
    { address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' },
    { address: '10.0.2.1/24', network: '10.0.2.0', interface: 'bridge2', comment: 'Lane 2' },
  ]

  // Unplanned needs a pushed table to judge against -- with nothing
  // pushed every pair is 'unjudged' and no claim is made either way.
  function pushedButSilent() {
    policyState.anyPushed = true
    policyState.edges = []
  }

  function drops(n: number, over: Partial<ClientEvent>): ClientEvent[] {
    return Array.from({ length: n }, () => event({ action: 'drop', ruleLabel: 'default drop', dstPort: 445, protocol: 'tcp', ...over }))
  }

  function cardText(container: HTMLElement): string {
    const card = container.querySelector('.unplanned-card')
    return card ? [...card.querySelectorAll('text')].map((t) => t.textContent?.trim()).join(' | ') : ''
  }

  it('escalates exactly one pair, and leaves the runner-up as an ordinary pill', () => {
    zonesState.pushed = twoLanes
    pushedButSilent()
    appState.events = [
      ...drops(14, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9' }),
      ...drops(13, { inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.1.20', dstIp: '10.0.2.9' }),
    ]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelectorAll('.unplanned-card').length).toBe(1)
    const text = cardText(container)
    expect(text).toContain('UNPLANNED · bridge2 → bridge1')
    expect(text).toContain('tcp/445')
    expect(text).toContain('caught by default drop')
    expect(text).toContain('14×')
    expect(text).toContain('open ▸')
    // The 13× pair keeps the ordinary treatment.
    const pills = [...container.querySelectorAll('.edge-badge')].map((t) => t.textContent?.trim() ?? '')
    expect(pills.some((p) => p.includes('13'))).toBe(true)
  })

  it('opens the stream filtered to the pair, since there is no flag to open', () => {
    zonesState.pushed = twoLanes
    pushedButSilent()
    appState.events = drops(9, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9' })
    const { container } = render(Topography)
    flushSync()

    const card = container.querySelector('.unplanned-card')!
    expect(card.getAttribute('role')).toBe('button')
    card.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    // openPair sets the live view's own filters. The mockup's flag
    // drawer cannot be opened because no unplanned flag type exists.
    expect(appState.view).toBe('live')
  })

  it('says the rule caught it, or says plainly that none named itself', () => {
    zonesState.pushed = twoLanes
    pushedButSilent()
    appState.events = drops(6, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9', ruleLabel: '' })
    const { container } = render(Topography)
    flushSync()

    expect(cardText(container)).toContain('caught, no rule named')
  })

  it('draws no card at all when nothing is unplanned', () => {
    zonesState.pushed = twoLanes
    policyState.anyPushed = false
    appState.events = drops(6, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9' })
    const { container } = render(Topography)
    flushSync()

    // Nothing pushed: every pair is unjudged, so no claim about intent.
    expect(container.querySelector('.unplanned-card')).toBeNull()
  })

  it('keeps every other badge out of the card it reserved', () => {
    zonesState.pushed = twoLanes
    pushedButSilent()
    appState.events = [
      ...drops(14, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9' }),
      ...drops(13, { inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.1.20', dstIp: '10.0.2.9' }),
    ]
    const { container } = render(Topography)
    flushSync()

    const box = container.querySelector('.uc-box')!
    const cx = Number(box.getAttribute('x')) + Number(box.getAttribute('width')) / 2
    const cy = Number(box.getAttribute('y')) + Number(box.getAttribute('height')) / 2 // box centre
    const cw = Number(box.getAttribute('width'))

    // An opaque two-line card with a pill settled inside it is the
    // failure the reservation exists to prevent.
    for (const plate of container.querySelectorAll('.edge-plate')) {
      const px = Number(plate.getAttribute('x')) + Number(plate.getAttribute('width')) / 2
      const py = Number(plate.getAttribute('y')) + 7
      const overlaps = Math.abs(px - cx) < (cw + Number(plate.getAttribute("width"))) / 2 && Math.abs(py - cy) < 27
      expect(overlaps).toBe(false)
    }
  })

  // #897 item 2. The escalated pair is handed to the layout as empty
  // text on purpose -- it takes no room and the card says its piece
  // instead. The pill loop drew it anyway, so its label went out at
  // full width over a plate sized for the empty string: the gate's
  // "every edge label sits on a plate (10 labels, 1 bare)".
  it('draws no pill for the pair it escalated, so no label hangs off the end of its plate', () => {
    zonesState.pushed = twoLanes
    pushedButSilent()
    appState.events = [
      ...drops(14, { inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', dstIp: '10.0.1.9' }),
      ...drops(13, { inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.1.20', dstIp: '10.0.2.9' }),
    ]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelectorAll('.unplanned-card').length).toBe(1)

    // A plate is sized from the string it carries and nothing else:
    // the badge face is monospace at 9.5px, so 5.72 a character plus 6
    // of padding either side (plateW). A label wider than its own plate
    // is a label with nothing behind it.
    const badges = [...container.querySelectorAll('.edge-badge')]
    expect(badges.length).toBeGreaterThan(0)
    for (const b of badges) {
      const label = b.textContent?.trim() ?? ''
      const plate = b.parentElement?.querySelector('.edge-plate')
      expect(plate).not.toBeNull()
      expect(Number(plate!.getAttribute('width'))).toBeGreaterThanOrEqual(label.length * 5.72 + 12 - 0.5)
    }

    // And the card is the only place the escalated pair is spoken.
    expect(badges.filter((b) => (b.textContent ?? '').includes('14')).length).toBe(0)
  })
})

describe('#715 item 3: the flags and watch overlays', () => {
  const oneLane: RouterIPAddress[] = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]

  function overlays(container: HTMLElement) {
    return [...container.querySelectorAll('[aria-label="Map overlays"] button')]
  }

  it('draws five controls: three exclusive tabs and two independent toggles, both on', () => {
    const { container } = render(Topography)
    flushSync()

    const tabs = [...container.querySelectorAll('[aria-label="Map lenses"] button')]
    expect(tabs.map((t) => t.textContent?.trim())).toEqual(['traffic', 'policy', 'coverage'])
    // The toggles sit outside the tablist deliberately: a toggle inside
    // one breaks its semantics.
    expect(container.querySelectorAll('[aria-label="Map lenses"] [aria-pressed]').length).toBe(0)

    const ov = overlays(container)
    expect(ov.length).toBe(2)
    expect(ov.map((b) => b.getAttribute('aria-pressed'))).toEqual(['true', 'true'])
  })

  it('shows no digit when nothing is flagged, and the count when something is', () => {
    flagsState.list = []
    const { container } = render(Topography)
    flushSync()
    expect(overlays(container)[0].textContent?.trim()).toBe('flags')

    flagsState.list = [flag('port_scan', '10.0.1.20'), flag('critical_port', '10.0.1.21'), flag('repeated_drops', '10.0.1.22')]
    flushSync()
    expect(overlays(container)[0].textContent?.replace(/\s+/g, ' ').trim()).toBe('flags 3')
  })

  it('leaves the aggregate-bar counts alone: they are drawn in every round, overlay or not', () => {
    zonesState.pushed = oneLane
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    flagsState.list = [flag('port_scan', '10.0.1.20')]
    const { container } = render(Topography)
    flushSync()

    const before = container.querySelectorAll('.fchip').length
    expect(before).toBeGreaterThan(0)

    overlays(container)[0].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    overlays(container)[1].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    expect(container.querySelectorAll('.fchip').length).toBe(before)
  })

  it('never changes the base lens', () => {
    const { container } = render(Topography)
    flushSync()
    const traffic = [...container.querySelectorAll('[aria-label="Map lenses"] button')][0]
    expect(traffic.classList.contains('on')).toBe(true)

    for (const b of overlays(container)) {
      b.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()
    }

    expect(traffic.classList.contains('on')).toBe(true)
  })

  // #897 item 1. The gate read the toggle as not latching. It does --
  // this is the assertion the scenario meant to make, on a base lens
  // the reader chose rather than the default, and on the attribute a
  // screen reader announces rather than the class the styling uses.
  it('latches off on a click and back on with the next, whichever base lens is showing', () => {
    const { container } = render(Topography)
    flushSync()

    const policyTab = [...container.querySelectorAll<HTMLButtonElement>('[aria-label="Map lenses"] button')].find(
      (b) => b.textContent?.trim() === 'policy',
    )
    policyTab!.click()
    flushSync()
    expect(policyTab!.classList.contains('on')).toBe(true)

    expect(overlays(container)[0].getAttribute('aria-pressed')).toBe('true')

    overlays(container)[0].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(overlays(container)[0].getAttribute('aria-pressed')).toBe('false')
    expect(overlays(container)[0].classList.contains('on')).toBe(false)

    // A latch, not a one-way switch.
    overlays(container)[0].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(overlays(container)[0].getAttribute('aria-pressed')).toBe('true')

    // The other toggle and the base lens are untouched throughout.
    expect(overlays(container)[1].getAttribute('aria-pressed')).toBe('true')
    const lensOn = [...container.querySelectorAll('[aria-label="Map lenses"] button')].find((b) => b.classList.contains('on'))
    expect(lensOn?.textContent?.trim()).toBe('policy')
  })
})

describe('the tunnel node (#877)', () => {
  it('draws nothing until a tunnel table has been pushed', () => {
    // An interface named wg0 in the events is not a pushed tunnel.
    // #874 exists so this state comes from the router rather than from
    // a name that looks like one -- and the issue is explicit: no
    // placeholder node, no "unknown" card.
    appState.events = [event({ inInterface: 'wg0', srcIp: '10.99.0.2' })]
    const { container } = render(Topography)
    flushSync()

    expect(tunnelCard(container)).toBeNull()
    const ribs = [...container.querySelectorAll('path.rib')].map((p) => p.getAttribute('d'))
    expect(ribs).not.toContain('M1080 186 C 990 215, 880 240, 830 252')
  })

  it('draws the card with its interface and subnet, as round 30 writes them', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    zonesState.pushed = [address({ interface: 'wg0', address: '10.99.0.1/24', network: '10.99.0.0' })]
    const { container } = render(Topography)
    flushSync()

    const card = tunnelCard(container)
    expect(card).not.toBeNull()
    // `wg0 · 10.99.0.0/24` -- the network form the mockup draws, not
    // the router's own host address in that range.
    expect(card?.querySelector('.n-cidr')?.textContent?.replace(/\s+/g, ' ').trim()).toBe('wg0 · 10.99.0.0/24')
  })

  it('says QUIET when the router calls the tunnel up but nothing has crossed it', () => {
    // Exactly what round 30 draws on this card, and the reading that
    // is mikroview's own rather than the API's vocabulary.
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'up' })]]])
    const { container } = render(Topography)
    flushSync()

    const badge = tunnelCard(container)?.querySelector('.n-cov')
    expect(badge?.textContent?.trim()).toBe('QUIET')
    expect(badge?.classList.contains('cov-q')).toBe(true)
  })

  it('says UP once traffic has actually crossed it', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'up' })]]])
    appState.events = [event({ inInterface: 'wg0', srcIp: '10.99.0.2' })]
    const { container } = render(Topography)
    flushSync()

    const badge = tunnelCard(container)?.querySelector('.n-cov')
    expect(badge?.textContent?.trim()).toBe('UP')
    expect(badge?.classList.contains('cov-l')).toBe(true)
  })

  it('says DOWN in the alarm ink when the router says down', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'down' })]]])
    appState.events = [event({ inInterface: 'wg0', srcIp: '10.99.0.2' })]
    const { container } = render(Topography)
    flushSync()

    const badge = tunnelCard(container)?.querySelector('.n-cov')
    // Events on a tunnel the router calls down do not overrule it --
    // down is the pushed fact, and this card reports facts.
    expect(badge?.textContent?.trim()).toBe('DOWN')
    expect(badge?.classList.contains('cov-d')).toBe(true)
  })

  it('says the state was never pushed rather than guessing at it', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'unknown' })]]])
    const { container } = render(Topography)
    flushSync()

    const badge = tunnelCard(container)?.querySelector('.n-cov')
    expect(badge?.textContent?.trim()).toBe('state not pushed')
    expect(badge?.classList.contains('cov-q')).toBe(true)
  })

  it('says so when no address names the tunnel, rather than going blank', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    zonesState.pushed = [address({ interface: 'bridge1' })]
    const { container } = render(Topography)
    flushSync()

    const cidr = tunnelCard(container)?.querySelector('.n-cidr')
    expect(cidr?.textContent?.replace(/\s+/g, ' ').trim()).toBe('wg0 · no address pushed')
    // Not the whole-map degraded toggle, which would hide this behind
    // a class that only shows when no table was pushed at all.
    expect(cidr?.querySelector('.cidr-none')).not.toBeNull()
  })

  it('joins the tunnel to the router with its own line', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const { container } = render(Topography)
    flushSync()

    // Ported from the-whole.html:935, ending at the waist card's edge.
    const ribs = [...container.querySelectorAll('path.rib')].map((p) => p.getAttribute('d'))
    expect(ribs).toContain('M1080 186 C 990 215, 880 240, 830 252')
  })

  it('draws the ghost reference line only once traffic has reached a lane', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const bare = render(Topography)
    flushSync()
    // Nothing observed crossing it: no destination to reference, so no
    // line rather than a guessed one.
    expect(bare.container.querySelector('path.rib-ghost')).toBeNull()
    bare.unmount()

    appState.events = [
      event({ inInterface: 'wg0', outInterface: 'bridge1', srcIp: '10.99.0.2' }),
      event({ inInterface: 'bridge1', srcIp: '192.168.1.5' }),
    ]
    const { container } = render(Topography)
    flushSync()

    const ghost = container.querySelector('path.rib-ghost')
    expect(ghost).not.toBeNull()
    // One lane, so it lands on the lane row's centre -- laneX's own
    // single-lane answer, not a hard-coded 610 from the mockup's
    // four-lane scene.
    expect(ghost?.getAttribute('d')).toBe('M 1100 186 C 945 300, 795 385, 700 476')
  })

  it('gives the node a watch bar only when a pushed range can correlate one', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    watchlistState.entries = [watchEntry({ source: { ip: '10.99.0.2' } })]

    const bare = render(Topography)
    flushSync()
    // No address pushed for wg0, so nothing correlates -- the same
    // refusal a degraded lane's bar already makes.
    expect(tunnelCard(bare.container)?.querySelector('.hb-w')).toBeNull()
    bare.unmount()

    zonesState.pushed = [address({ interface: 'wg0', address: '10.99.0.1/24', network: '10.99.0.0' })]
    const { container } = render(Topography)
    flushSync()

    const card = tunnelCard(container)
    expect(card?.querySelector('.hb-w')).not.toBeNull()
    expect(card?.querySelector('.hbt.wp')?.textContent?.replace(/\s+/g, ' ').trim()).toBe('◉ 1')
  })
})
