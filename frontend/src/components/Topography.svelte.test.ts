// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
// Only fetchTrace is stubbed (B1's own test needs to control when its
// promise resolves); every other export of the module -- fetchPorts and
// the rest -- stays real, the same way the file's own top-of-file note
// says the component's other network calls never fire because
// appState.devices stays empty throughout this file.
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchTrace: vi.fn(),
}))
import { fetchTrace, type TraceResponse } from '../lib/api'
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
import { hostsState } from '../lib/hosts.svelte'
import { baselineState } from '../lib/baseline.svelte'
import { portFilterState } from '../lib/portFilter.svelte'
import { mapTraceState } from '../lib/mapTrace.svelte'
import { EMPTY_OFF_BASELINE, type OffBaselineLine } from '../lib/baseline'
import type { Host } from '../lib/api'
import type { RouterFilterRule, RouterIPAddress } from '../lib/api'
import { emptyFilters, type ClientEvent, type Device, type Flag, type FlagType, type WatchlistEntry } from '../lib/types'
import Topography, { tunnelEventCounts } from './Topography.svelte'
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

// jsdom has no ResizeObserver at all, and the card's placement is
// re-worked on one (#1028). This stand-in records which callbacks are
// watching which element, so a test can report a size change to exactly
// the element that grew -- and report it to nothing, which is what the
// unfixed code deserves, rather than throwing.
const resizeWatchers = new Map<Element, Set<() => void>>()

class FakeResizeObserver {
  constructor(private readonly cb: () => void) {}
  observe(el: Element) {
    const set = resizeWatchers.get(el) ?? new Set<() => void>()
    set.add(this.cb)
    resizeWatchers.set(el, set)
  }
  unobserve(el: Element) {
    resizeWatchers.get(el)?.delete(this.cb)
  }
  disconnect() {
    for (const set of resizeWatchers.values()) set.delete(this.cb)
  }
}

const reportResize = (el: Element) => {
  for (const cb of [...(resizeWatchers.get(el) ?? [])]) cb()
}

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
  return { iface: 'wg0', routerId: 'router1', kind: 'wg', apiState: 'up', peers: [], lastHeard: null, ...overrides }
}

// The tunnel node's own card, found by its name rather than document
// order -- there are two upper cards now.
function tunnelCard(container: HTMLElement): Element | null {
  const name = [...container.querySelectorAll('.n-name')].find((n) => n.textContent?.trim() === 'WireGuard')
  return name?.parentElement ?? null
}

// Every tunnel card the cluster drew (#890), with the place it was
// packed into -- read off the group transform, which is where the card
// actually is on the map.
function tunnelCards(container: HTMLElement): { iface: string; x: number; y: number; card: Element }[] {
  return [...container.querySelectorAll('.n-name')]
    .filter((n) => n.textContent?.trim() === 'WireGuard')
    .map((n) => {
      const card = n.parentElement as Element
      const g = card.parentElement as Element
      const m = /translate\(([-\d.]+) ([-\d.]+)\)/.exec(g.getAttribute('transform') ?? '')
      return {
        iface: (card.querySelector('.n-cidr')?.textContent ?? '').replace(/\s+/g, ' ').trim().split(' ')[0],
        x: Number(m?.[1] ?? NaN),
        y: Number(m?.[2] ?? NaN),
        card,
      }
    })
}

function mapViewBox(container: HTMLElement): string {
  return container.querySelector('.stage > svg')?.getAttribute('viewBox') ?? ''
}

// The slider opens on the city (#869), where the 2D stage is hidden.
// Anything that reads the map as the surface in front of the operator --
// its keyboard, its wheel -- has to move to a 2D stop first.
function showTheMap(container: HTMLElement) {
  const range = container.querySelector<HTMLInputElement>('.alt-range')!
  range.value = '1' // "services"
  range.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
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
  // The host register is a module-level singleton too (#1016), so a test
  // that seeds a quiet host would otherwise leave it quiet for the next.
  hostsState.hosts = []
  hostsState.error = null
  // The baseline register is a module-level singleton too (#1016), so a
  // test that seeds an off-baseline line would otherwise leave the next
  // test's map lit by it.
  baselineState.off = EMPTY_OFF_BASELINE
  baselineState.error = null
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  topologyNavState.pendingFlagId = null
  topologyNavState.pendingWatchId = null
  topologyNavState.pendingDescend = null
  topologyNavState.pendingTrace = null
  // #1018's two filters are module-level singletons too, so a test that
  // filtered the map would otherwise leave the next one's map filtered.
  portFilterState.clear()
  portFilterState.proto = 'tcp'
  mapTraceState.clear()
  vi.mocked(fetchTrace).mockReset()
  wizardState.open = false
  // Every test starts on a fresh slider: altitudeStopState is a
  // module-level singleton that persists across reloads, so a test that
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

  // #1156: the scene bar's eye read "5 watchers held" while this dial
  // read 6 on the same session -- the dial was counting a watch the
  // operator had switched off. Both now read watchlistState.heldCount.
  it('leaves a switched-off watch off the dial, as the scene bar does', () => {
    watchlistState.entries = [watchEntry({ enabled: true }), watchEntry({ enabled: false })]
    const { container } = render(Topography)
    flushSync()

    const dnums = [...container.querySelectorAll('.dnum')].map((n) => n.textContent)
    expect(dnums).toEqual(['0', String(watchlistState.heldCount)])
    expect(dnums[1]).toBe('1')
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

  it('remembers the last stop across mounts, via altitudeStopState persistence (#869)', () => {
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

  // Nothing on the row is a control any more. Round 49 (#1016) left the
  // two overlay pills as the one piece of state the crossing carried;
  // #981 took those too, because a mark is drawn by its own data on both
  // sides and there is nothing to remember across the centre. What the
  // crossing still carries -- the reach, and the camera -- is asserted
  // below.
  // #1018's port pill was the flat map's own furniture until #1055 gave
  // the city the same filter from the same store. It is one control at
  // every altitude now, and the selection it carries survives the
  // crossing in both directions -- clearing it would throw away the
  // operator's own question for moving the slider.
  it('draws the port pill on both sides of the centre, and carries the selection across', () => {
    const { container } = render(Topography)
    flushSync()
    expect(container.querySelectorAll('.pills .pill').length).toBe(1) // ◆ city, the default
    expect(container.querySelector('.pills .pill')?.textContent?.trim()).toBe('⌕ port')

    crossTo(container, '2') // to zones: the 2D side
    expect(container.querySelectorAll('.pills .pill').length).toBe(1)

    portFilterState.ports = [445]
    portFilterState.proto = 'tcp'
    portFilterState.answer = { ...portFilterState.answer, events: 2, lines: 2 }
    portFilterState.answeredKey = portFilterState.key
    flushSync()
    expect(container.querySelector('.pill.p.on')?.textContent).toContain('445/tcp')

    crossTo(container, '4') // back across, to borough
    expect(portFilterState.active).toBe(true)
    expect(container.querySelector('.city')).not.toBeNull()
    expect(container.querySelector('.pill.p.on')?.textContent).toContain('445/tcp')
  })

  it('hands a 2D reach across the centre to the same host, standing on it in the city', () => {
    zonesState.pushed = oneLane
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    const { container } = render(Topography)
    flushSync()
    crossTo(container, '2') // zones: the 2D map is the active side

    const hostLink = container.querySelector<SVGGElement>('.hostrow .hot')!
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

    const hostLink = container.querySelector<SVGGElement>('.hostrow .hot')
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

// Round 49 (#1016) replaced the zone card's coverage badge with the
// material on the ribs themselves: the card says `name · subnet` and
// the boundary says the rest.
describe('coverage is the material, not a caption (round 49, #1016)', () => {
  const bothLogged = (): void => {
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
  }

  const bothDark = (): void => {
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
  }

  it('says name and subnet on a lane card, and nothing about coverage', () => {
    bothLogged()
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.zone .n-cov')).toBeNull()
    // The 2D stage only: the city carries its own share of round 49.
    const text = container.querySelector('.stage')?.textContent ?? ''
    for (const word of ['LOGGED', 'DARK', 'QUIET', 'COVERED']) expect(text).not.toContain(word)
  })

  it('draws a dark boundary-direction as grey dashes rather than writing "dark" anywhere', () => {
    bothDark()
    const { container } = render(Topography)
    flushSync()

    const dark = [...container.querySelectorAll('.cedge.dark')]
    expect(dark.length).toBe(2) // one half each way
    // Nothing drawn says it: the word survives only in the accessible
    // name and the hover title, which are how a reader who cannot see
    // the dashes is told the same thing.
    // Scoped to the 2D stage: the city is the other agent's surface and
    // carries its own share of round 49.
    const drawn = [...container.querySelectorAll('.stage svg text')].map((t) => t.textContent ?? '')
    expect(drawn.filter((t) => t.toLowerCase().includes('dark'))).toEqual([])
    expect(container.querySelector('.zone .n-cov')).toBeNull()
  })

  it('draws a declared boundary in white, solid, not in the dark treatment', () => {
    bothDark()
    coverageState.declarations = [
      { key: 'bridge2|wan1', reason: 'guest devices only reach the internet', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
    ]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelectorAll('.cedge.quiet').length).toBe(1)
    expect(container.querySelectorAll('.cedge.dark').length).toBe(1)
  })

  it('draws no traffic across a dark direction — a line there would claim a log line never written', () => {
    bothDark()
    // Traffic on the dark boundary-direction: the material stays, the
    // rib does not.
    appState.events.push(event({ inInterface: 'bridge2', outInterface: 'wan1', srcIp: '10.0.30.9' }))
    const { container } = render(Topography)
    flushSync()

    const drawn = [...container.querySelectorAll('.redge')]
    expect(drawn.length).toBe(0)
    expect(container.querySelectorAll('.cedge.dark').length).toBe(2)
  })

  it('keeps "no rule table pushed" on the card, because it is a different fact from dark', () => {
    zonesState.pushed = [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }]
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    policyState.anyPushed = false
    const { container } = render(Topography)
    flushSync()

    const line = container.querySelector('.n-sub.no-table')
    expect(line?.textContent).toBe('no rule table pushed')
  })
})

describe('a rib is two halves (round 49, #1016)', () => {
  // One boundary logged one way and dark the other: the pair draws one
  // rib whose halves disagree, meeting at the pair's own midpoint.
  const oneEachWay = (): void => {
    zonesState.pushed = [{ address: '10.0.30.1/24', network: '10.0.30.0', interface: 'bridge2', comment: 'IoT' }]
    appState.events = [
      event({ inInterface: 'wan1', outInterface: 'bridge2', srcIp: '8.8.8.8' }),
      event({ inInterface: 'bridge2', outInterface: 'wan1', srcIp: '10.0.30.9' }),
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'wan1|bridge2', from: 'wan1', to: 'bridge2', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true },
      { key: 'bridge2|wan1', from: 'bridge2', to: 'wan1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    ]
  }

  const endOf = (d: string): [number, number] => {
    const nums = d.match(/-?\d+(\.\d+)?/g)!.map(Number)
    return [nums[nums.length - 2], nums[nums.length - 1]]
  }
  const startOf = (d: string): [number, number] => {
    const nums = d.match(/-?\d+(\.\d+)?/g)!.map(Number)
    return [nums[0], nums[1]]
  }

  it('gives each direction its own half, and the two halves meet in the middle', () => {
    oneEachWay()
    const { container } = render(Topography)
    flushSync()

    const dark = container.querySelector('.cedge.dark')!.getAttribute('d')!
    const logged = container.querySelector('.redge')!.getAttribute('d')!
    // Each half starts at its own island and stops at the same point:
    // the midpoint of the pair's one curve.
    const [dex, dey] = endOf(dark)
    const [lex, ley] = endOf(logged)
    expect(Math.hypot(dex - lex, dey - ley)).toBeLessThan(1)
    const [dsx, dsy] = startOf(dark)
    const [lsx, lsy] = startOf(logged)
    expect(Math.hypot(dsx - lsx, dsy - lsy)).toBeGreaterThan(40)
  })

  it('colours a logged half by the verdict — green where anything was accepted', () => {
    oneEachWay()
    const { container } = render(Topography)
    flushSync()

    const style = container.querySelector('.redge')!.getAttribute('style') ?? ''
    expect(style).toContain('stroke: var(--accept)')
  })

  it('leaves the escalated unplanned pair undivided, in the alarm ink', () => {
    zonesState.pushed = [{ address: '10.0.30.1/24', network: '10.0.30.0', interface: 'bridge2', comment: 'IoT' }]
    policyState.anyPushed = true
    policyState.edges = [] // nothing in the table names this pair: unplanned
    appState.events = [
      event({ inInterface: 'bridge2', outInterface: 'wan1', srcIp: '10.0.30.9' }),
      event({ inInterface: 'wan1', srcIp: '8.8.8.8' }),
    ]
    const { container } = render(Topography)
    flushSync()

    const alarm = container.querySelector('.redge.alarm')!
    const half = container.querySelector('.redge:not(.alarm)')
    const whole = alarm.getAttribute('d')!
    // Undivided: it runs the whole way to its far island, well past the
    // midpoint any half would stop at.
    expect(whole.match(/-?\d+(\.\d+)?/g)!.length).toBeGreaterThanOrEqual(8)
    expect(half).toBeNull()
  })
})

describe('#1053: a lateral zone-to-zone rib bends without hooking', () => {
  // Four lanes so two of them land on the same side of the waist --
  // laneX(0,4) and laneX(1,4) are both left of x=700, the "lateral"
  // pair the hook was reported on (LAN <-> Servers, #1053).
  function fourLanesWithLateralPair() {
    zonesState.pushed = [1, 2, 3, 4].map((n) => ({
      address: `10.0.${n}.1/24`,
      network: `10.0.${n}.0`,
      interface: `bridge${n}`,
      comment: `Lane ${n}`,
    }))
    // Strictly decreasing counts pin the busiest-first sort: bridge1
    // lands at index 0, bridge2 at index 1 -- both the waist's own side.
    appState.events = [
      ...Array.from({ length: 40 }, () => event({ inInterface: 'bridge1', srcIp: '10.0.1.20' })),
      ...Array.from({ length: 30 }, () => event({ inInterface: 'bridge2', srcIp: '10.0.2.20' })),
      ...Array.from({ length: 20 }, () => event({ inInterface: 'bridge3', srcIp: '10.0.3.20' })),
      ...Array.from({ length: 10 }, () => event({ inInterface: 'bridge4', srcIp: '10.0.4.20' })),
      event({ inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.2.20', action: 'accept' }),
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge2|bridge1', from: 'bridge2', to: 'bridge1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true },
      { key: 'bridge1|bridge2', from: 'bridge1', to: 'bridge2', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    ]
  }

  // The curve a half actually draws -- "M x y C x1 y1, x2 y2, x3 y3" --
  // sampled the same way the map itself samples a drawn cubic (bezAt).
  function sampleCubicPath(d: string): { x: number; y: number }[] {
    const n = d.match(/-?\d+(\.\d+)?/g)!.map(Number)
    const c: [number, number][] = [
      [n[0], n[1]],
      [n[2], n[3]],
      [n[4], n[5]],
      [n[6], n[7]],
    ]
    const out: { x: number; y: number }[] = []
    for (let i = 0; i <= 20; i++) {
      const t = i / 20
      const u = 1 - t
      const w = [u * u * u, 3 * u * u * t, 3 * u * t * t, t * t * t]
      out.push({
        x: w[0] * c[0][0] + w[1] * c[1][0] + w[2] * c[2][0] + w[3] * c[3][0],
        y: w[0] * c[0][1] + w[1] * c[1][1] + w[2] * c[2][1] + w[3] * c[3][1],
      })
    }
    return out
  }

  // A hook is a reversal: the sampled curve moving back the way it came
  // along x, rather than bending smoothly on toward its own far end.
  function reversesAlongX(pts: { x: number; y: number }[]): boolean {
    const overall = Math.sign(pts[pts.length - 1].x - pts[0].x)
    if (overall === 0) return false
    return pts.some((p, i) => i > 0 && Math.sign(p.x - pts[i - 1].x) === -overall)
  }

  it('never reverses direction along x on either half of the pair', () => {
    fourLanesWithLateralPair()
    const { container } = render(Topography)
    flushSync()

    const redge = container.querySelector('.redge')!.getAttribute('d')!
    const cedge = container.querySelector('.cedge.dark')!.getAttribute('d')!
    expect(reversesAlongX(sampleCubicPath(redge))).toBe(false)
    expect(reversesAlongX(sampleCubicPath(cedge))).toBe(false)
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
    // #1165: the second line was "Run setup… ▸ adds it" -- the ellipsis
    // drew as three raised dots in this face, and "adds it" left the
    // reader to work out what "it" was. Both are named in full now.
    expect(lines).toEqual(['no address table pushed — zones from boundaries', 'Run setup ▸ adds the address table'])
    expect(lines.join(' ')).not.toContain('…')

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
    expect(go?.textContent).toBe('Run setup ▸')
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

describe('the lens row (round 49 reduced it to two pills; #981 took those; #1018 put a filter there)', () => {
  it('renders no lens tabs and no overlay pills -- only the port filter', () => {
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.lenses')).toBeNull() // the old top-right strip
    expect(container.querySelector('.wlens2')).toBeNull() // the lens bar itself
    expect(container.querySelector('[role="tablist"]')).toBeNull()

    // Owner, 2026-09-08 (#981): there is no toggle -- "something that's
    // always there is easy to ignore; if it's not always there you know
    // it's there for a reason." The one pill in this row is #1018's port
    // filter, which is not a lens: it does not switch a layer on and off,
    // it redraws the map to an answer, and it goes away with the ✕.
    const pills = [...container.querySelectorAll('.pills .pill')]
    expect(pills.length).toBe(1)
    expect(pills[0].textContent?.trim()).toBe('⌕ port')
    expect(pills[0].getAttribute('aria-pressed')).toBe('false')
  })

  it('shows the collapsed answer, with no picker bar, the moment a port is selected (#1178)', () => {
    // Selected but not yet answered: the store's own collapse has closed
    // the picker (portFilter.svelte.ts), and the pill is the answer.
    portFilterState.ports = [445]
    portFilterState.proto = 'tcp'
    portFilterState.open = false
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.pill.p.edit')).toBeNull()
    const pill = container.querySelector('.pill.p.on')
    expect(pill?.textContent).toContain('445/tcp')
    // ...and it claims nothing about the traffic until the answer is in:
    // "nothing seen · 0 doors" out of a pending fetch would be a
    // statement about the network nobody has made yet.
    expect(pill?.textContent).not.toContain('nothing seen')
    expect(container.querySelector('.pill-x')).not.toBeNull()

    portFilterState.answer = { ...portFilterState.answer, events: 2, lines: 2 }
    portFilterState.answeredKey = portFilterState.key
    flushSync()
    expect(container.querySelector('.pill.p.on')?.textContent).toContain('2 lines seen')
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

    container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
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

  // Round 49 replaced the card's list of host *names* with a row of
  // dots (#1016, DESIGN.md "Living hosts"): ten dots then `+N`, one dot
  // per host. The old width budget went with the names -- ten is the
  // ratified number, not an estimate off the card's width -- and #715
  // item 10's "no per-name dot" ruling went with them too: it struck a
  // dot decorating a name list, and there is no name list any more.
  it('draws ten host dots then +N, one dot per host', () => {
    pushLanes(1)
    appState.events = Array.from({ length: 13 }, (_, i) =>
      event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `host-${i}` }),
    )
    const { container } = render(Topography)
    flushSync()

    const row = container.querySelector('.zone .hostrow')
    expect(row).not.toBeNull()
    expect(row?.querySelectorAll('.hot').length).toBe(10)
    expect(row?.querySelector('.c-label.more')?.textContent).toBe('+3')
    // The clip is the backstop: a crowded lane stays inside its own card
    // whatever the pitch works out to.
    expect(row?.getAttribute('clip-path')).toMatch(/^url\(#.+-hosts\)$/)
  })

  it('draws every host and no +N when the lane has ten or fewer', () => {
    pushLanes(1)
    appState.events = Array.from({ length: 4 }, (_, i) =>
      event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `host-${i}` }),
    )
    const { container } = render(Topography)
    flushSync()

    const row = container.querySelector('.zone .hostrow')
    expect(row?.querySelectorAll('.hot').length).toBe(4)
    expect(row?.querySelector('.c-label.more')).toBeNull()
  })

  it('counts the lane under its dots, and says nothing about a state no host is in', () => {
    pushLanes(1)
    appState.events = Array.from({ length: 3 }, (_, i) =>
      event({ inInterface: 'bridge1', srcIp: `10.0.1.${20 + i}`, srcHostName: `host-${i}` }),
    )
    const { container } = render(Topography)
    flushSync()

    const tally = container.querySelector('.zone .hosttally')
    expect(tally?.textContent).toBe('3 hosts')
  })

  it('says so on a lane whose addresses resolved no host, rather than leaving the band blank (#1165)', () => {
    // sfp-sfpplus1 on the operator's own router: the address table names
    // the interface and nothing in the feed ever resolved to it. The
    // card drew its name and subnet and then an empty band, which reads
    // as a card that failed to finish drawing.
    pushLanes(1)
    appState.events = []
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.zone .h-dot')).toBeNull()
    expect(container.querySelector('.zone .hosttally')?.textContent).toBe('no hosts seen yet')
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

  it('gives a rib one tab stop, not two (#1180)', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' }),
      event({ inInterface: 'ether1', outInterface: 'bridge1', srcIp: '203.0.113.9', dstPort: 445, action: 'drop' }),
    ]
    const { container } = render(Topography)
    flushSync()

    // The rib and its own label plate carried the same action under the
    // same name, and both were in the tab order, so a keyboard walk of
    // the map stopped at every boundary twice.
    const ribs = [...container.querySelectorAll('.edge-g')]
    expect(ribs.length).toBeGreaterThan(0)
    for (const r of ribs) expect(r.getAttribute('tabindex')).toBe('0')

    const plates = [...container.querySelectorAll('g.detail')].filter((g) => g.querySelector('.edge-plate'))
    expect(plates.length).toBeGreaterThan(0)
    for (const p of plates) {
      expect(p.getAttribute('tabindex')).toBe('-1')
      // ...and the screen reader is not told the same rib twice either.
      expect(p.getAttribute('aria-hidden')).toBe('true')
    }
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
      expect(c.querySelector('.hostrow')).toBeNull()
      expect(c.querySelector('circle')).toBeNull()
    }
    // The full card (the host dot row included) stays available for
    // clients and services -- "hosts appear at clients" -- so it is
    // still drawn, just hidden by the stylesheet while zones is the
    // active stop.
    expect(container.querySelectorAll('.zone .isl-card').length).toBe(3)
    expect(container.querySelectorAll('.zone .hostrow').length).toBe(3)
  })

  it("stacks a district card's name above its CIDR rather than printing them over each other (#976 item 2: \"10.0.10.1/24 shows through LAN\")", () => {
    // One lane with one host gives the smallest plateRadius (lib/city/
    // layout.ts), so the card sits at its floor -- the size the owner's
    // report was actually seeing.
    pushLanes(1)
    const { container } = render(Topography)
    flushSync()

    const card = container.querySelector('.ground-flat .gf-card')!
    const name = card.querySelector('.n-name')!
    const cidr = card.querySelector('.n-cidr')!
    // Same left edge, different row -- stacked, not the name and CIDR
    // racing each other in from opposite sides of one shared line.
    expect(cidr.getAttribute('x')).toBe(name.getAttribute('x'))
    expect(Number(cidr.getAttribute('y'))).toBeGreaterThan(Number(name.getAttribute('y')))
  })

  it('never lets two district cards on the ground plan overlap, even at the smallest size (#976 item 2)', () => {
    // Five lanes on one router is PRIMARY_SLOTS' own length (lib/city/
    // layout.ts) -- the estate this map actually draws its fullest.
    pushLanes(5)
    const { container } = render(Topography)
    flushSync()

    function absoluteBox(card: Element) {
      const plate = card.querySelector('.gf-plate')!
      const tf = card.getAttribute('transform') ?? 'translate(0 0)'
      const [tx, ty] = tf
        .replace('translate(', '')
        .replace(')', '')
        .split(' ')
        .map(Number)
      return {
        x: tx + Number(plate.getAttribute('x')),
        y: ty + Number(plate.getAttribute('y')),
        w: Number(plate.getAttribute('width')),
        h: Number(plate.getAttribute('height')),
      }
    }

    const boxes = [...container.querySelectorAll('.ground-flat .gf-card')].map(absoluteBox)
    expect(boxes.length).toBe(5)
    for (let a = 0; a < boxes.length; a++) {
      for (let b = a + 1; b < boxes.length; b++) {
        const p1 = boxes[a]
        const p2 = boxes[b]
        const overlaps = p1.x < p2.x + p2.w && p2.x < p1.x + p1.w && p1.y < p2.y + p2.h && p2.y < p1.y + p1.h
        expect(overlaps).toBe(false)
      }
    }
  })

  it("keeps every line of a district card's own text inside its plate, so the roads behind it have something solid to stop against (#976 follow-up)", () => {
    // A road passing under a card is only ever hidden by the plate's
    // own opaque fill -- there is no other mechanism. If a text line
    // sits below the plate's own bottom edge, whatever is behind it
    // (a road, in a real estate) shows straight through that line
    // instead of stopping at the card, which read as "lines painted
    // through the card" even though the roads were always drawn first.
    pushLanes(1)
    const { container } = render(Topography)
    flushSync()

    const card = container.querySelector('.ground-flat .gf-card')!
    const plate = card.querySelector('.gf-plate')!
    const plateTop = Number(plate.getAttribute('y'))
    const plateBottom = plateTop + Number(plate.getAttribute('height'))
    const texts = [...card.querySelectorAll('text')]
    expect(texts.length).toBeGreaterThan(0)
    for (const t of texts) {
      const y = Number(t.getAttribute('y'))
      expect(y).toBeGreaterThan(plateTop)
      expect(y).toBeLessThan(plateBottom)
    }
  })

  it("says a dark district with the plate's own material, never with the word DARK (round 49)", () => {
    // The DARK word used to trail the host count on this card, and
    // before that sat right-anchored on the count's own row, where a
    // wide enough count printed "hostsARK" (#976 follow-up). Round 49
    // took the word away entirely: the plate is drawn dark, and the
    // card carries `name · subnet` and its count.
    pushLanes(1)
    policyState.anyPushed = true
    policyState.edges = [] // nothing logs this lane, so it reads dark
    const { container } = render(Topography)
    flushSync()

    const card = container.querySelector('.ground-flat .gf-card.dark')!
    expect(card).not.toBeNull()
    expect(card.querySelectorAll('.gf-count').length).toBe(1)
    const count = card.querySelector('.gf-count')!
    expect(count.textContent?.replace(/\s+/g, ' ').trim()).toMatch(/^\d+ hosts?$/)
    expect(card.textContent).not.toContain('DARK')
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
    appState.events = [
      // A public-sourced inbound event on ether1 is what actually makes
      // it the WAN (zonesState.wanInterface, #850) -- without one,
      // ether1 is just an undeclared boundary, which #1054's declared-
      // first ranking now correctly leaves off the map instead of
      // drawing it as a sixth lane.
      event({ inInterface: 'ether1', outInterface: 'bridge1', srcIp: '203.0.113.9', dstPort: 443, action: 'accept' }),
      ...Array.from({ length: 5 }, (_, i) =>
        event({ inInterface: `bridge${i + 1}`, outInterface: 'ether1', srcIp: `10.0.${i + 1}.20`, dstPort: 443, action: 'accept' }),
      ),
    ]
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

  it('colours a traffic edge by the verdict, never one shared grey (#715; round 49 makes it the verdict, not the lane)', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 443, action: 'accept' })]
    const { container } = render(Topography)
    flushSync()

    const line = container.querySelector('.redge')
    expect(line).not.toBeNull()
    expect(line?.getAttribute('style')).toContain('stroke: var(--accept)')
  })

  it('colours a boundary that only ever dropped in the alarm ink, at its own weight', () => {
    zonesState.pushed = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge1|ether1', from: 'bridge1', to: 'ether1', accepted: false, refused: true, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true },
    ]
    appState.events = [event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', dstPort: 22, action: 'drop' })]
    const { container } = render(Topography)
    flushSync()

    const line = container.querySelector('.redge')!
    expect(line.getAttribute('style')).toContain('stroke: var(--alarm)')
    expect(line.classList.contains('dropped')).toBe(true)
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

    const hostLink = container.querySelector<SVGGElement>('.hostrow .hot')
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

  it('holds for the material, two dark boundary-directions', () => {
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

    // The material draws itself, always: no lens to switch to (round 49).
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

  // Dark, so that every one of these boundary-directions draws its own
  // half of the material whether or not traffic was observed on it --
  // which is what gives this geometry check one drawn path per edge
  // now that the coverage lens is gone (round 49).
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
      logged: false,
    }
  }

  it('two lanes heading for the internet do not share the waist corridor', () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [policyEdge('bridge1', 'ether1', [':443']), policyEdge('bridge2', 'ether1', [':80'])]
    const { container } = render(Topography)
    flushSync()
    const [one, two] = pathsOf(container, '.cedge')
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
    const [toInternet, toAnywhere] = pathsOf(container, '.cedge')
    expect(toInternet).toBeTruthy()
    expect(toAnywhere).toBeTruthy()
    expect(sharedRun(toInternet, toAnywhere)).toBeLessThan(SMEARED)
  })

  // Round 49 makes these two the two halves of one rib: they meet at
  // the midpoint and each runs back to its own island, so they touch
  // once and part -- exactly what a crossing used to look like here.
  it('the two directions of one pair meet without lying along each other', () => {
    zonesState.pushed = twoLanes
    appState.events = seenOnBothLanes()
    policyState.anyPushed = true
    policyState.edges = [policyEdge('bridge1', 'bridge2', [':445']), policyEdge('bridge2', 'bridge1', [':22'])]
    const { container } = render(Topography)
    flushSync()
    const [there, back] = pathsOf(container, '.cedge')
    expect(sharedRun(there, back)).toBeLessThan(SMEARED)
  })

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
      const edges = pathsOf(container, '.cedge')
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
    const edges = pathsOf(container, '.cedge')
    expect(edges.length).toBe(8)
    for (let a = 0; a < edges.length; a++) {
      for (let b = a + 1; b < edges.length; b++) {
        expect(sharedRun(edges[a], edges[b])).toBeLessThan(SMEARED)
      }
    }
  })

  // A refused pair dies at the waist rather than crossing it, so its
  // death point is its own geometry -- kept on the traffic lens, the
  // only one that still draws a pair that does not cross.
  it('two inbound drops to different lanes stop coinciding at the top of the waist', () => {
    zonesState.pushed = twoLanes
    appState.events = [
      ...seenOnBothLanes(),
      event({ inInterface: 'ether1', outInterface: 'bridge1', srcIp: '203.0.113.9', dstPort: 3389, action: 'drop' }),
      event({ inInterface: 'ether1', outInterface: 'bridge2', srcIp: '203.0.113.9', dstPort: 22, action: 'drop' }),
    ]
    const { container } = render(Topography)
    flushSync()

    const [toBridge1, toBridge2] = pathsOf(container, '.redge')
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

  // #715 item 10 struck a dot that decorated a list of host *names*.
  // Round 49 replaced the list itself with a dot row (#1016), so the
  // ruling no longer has a subject -- but the tab-stop half of it does,
  // and that is what this keeps: one focusable target per host, never a
  // second one hidden from assistive tech behind it.
  it('gives each host dot one focusable target, not two', () => {
    zonesState.pushed = oneLane
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.20', srcHostName: 'tom-desktop', dstPort: 443 }),
      event({ inInterface: 'bridge1', outInterface: 'ether1', srcIp: '10.0.1.21', srcHostName: 'phone-tom', dstPort: 443 }),
    ]
    const { container } = render(Topography)
    flushSync()

    const row = container.querySelector('.zone .hostrow')
    expect(row).not.toBeNull()
    const targets = row!.querySelectorAll('[role="button"]')
    expect(targets.length).toBe(2)
    expect([...targets].every((t) => t.getAttribute('aria-hidden') !== 'true')).toBe(true)
    // Each one says which host it is and where it goes.
    expect([...targets].map((t) => t.getAttribute('aria-label'))).toEqual([
      'tom-desktop — open its reach',
      'phone-tom — open its reach',
    ])
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

    // The lane's accent dot specifically: the card also carries a row of
    // host dots now (#1016), and those wear the same ink by design.
    const dots = [...container.querySelectorAll('.zone .isl-card .lane-ink')].map((c) => c.getAttribute('fill'))
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
    const hostLink = container.querySelector<SVGGElement>('.hostrow .hot')
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

  // #976 item 3 stacked a counterpart's four pills so they stopped
  // landing on each other. Round 49 answers the same complaint by
  // removing them: "Nothing is written on a road or strand -- no pill
  // labels, on either surface; the ports live in the card" (DESIGN.md
  // "The reach", and its Superseded list). The scenario is kept exactly
  // as it was -- the four-way case that produced the overlap -- and the
  // expectation inverted, so the pills cannot come back unnoticed.
  it('writes nothing at all on a strand, whichever way the traffic ran (round 49, #1016)', () => {
    const container = openReach([
      talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 443, protocol: 'tcp', action: 'accept' }),
      talk({ outInterface: 'bridge2', dstIp: '10.0.2.9', dstHostName: 'nas', dstPort: 445, protocol: 'tcp', action: 'drop' }),
      talk({
        srcIp: '10.0.2.9',
        srcHostName: 'nas',
        dstIp: '10.0.1.20',
        dstHostName: 'cam-porch',
        inInterface: 'bridge2',
        outInterface: 'bridge1',
        dstPort: 22,
        protocol: 'tcp',
        action: 'accept',
      }),
      talk({
        srcIp: '10.0.2.9',
        srcHostName: 'nas',
        dstIp: '10.0.1.20',
        dstHostName: 'cam-porch',
        inInterface: 'bridge2',
        outInterface: 'bridge1',
        dstPort: 3389,
        protocol: 'tcp',
        action: 'drop',
      }),
    ])

    // The four strands are still drawn -- nothing is removed, only
    // dimmed -- so this is "the labels went", not "the traffic went".
    expect(container.querySelectorAll('.membrane-layer .strand').length).toBe(4)
    expect(container.querySelectorAll('.membrane-layer .chip-t').length).toBe(0)

    // Nothing is written on any of them: no text of any kind sits inside
    // a strand's own group. Asserted over every strand group rather than
    // over one class name, so re-adding a label under a new class fails
    // here too.
    for (const g of container.querySelectorAll('.membrane-layer .strand-g')) {
      expect(g.querySelector('text')).toBeNull()
    }
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

describe('#715 item 3, as #981 left it: the marks are the data, not an overlay', () => {
  const oneLane: RouterIPAddress[] = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]

  it('leaves nothing in the overlay row but the off-baseline tally and the port pill', () => {
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const buttons = [...container.querySelectorAll('[aria-label="Map overlays"] button')]
    expect(buttons.map((b) => b.textContent?.trim())).toEqual(['⌕ port'])
    expect(container.querySelector('[role="tablist"]')).toBeNull()
  })

  it('draws the aggregate-bar counts as it always did: they were never an overlay', () => {
    zonesState.pushed = oneLane
    appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'desk' })]
    flagsState.list = [flag('port_scan', '10.0.1.20')]
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelectorAll('.fchip').length).toBeGreaterThan(0)
  })
})

describe('tunnelEventCounts (#1090)', () => {
  it('counts one pass over the buffer into a per-interface map', () => {
    const counts = tunnelEventCounts([
      event({ inInterface: 'wg0' }),
      event({ outInterface: 'wg0' }),
      event({ inInterface: 'wg1', outInterface: 'bridge1' }),
      event({}),
    ])
    expect(counts.get('wg0')).toBe(2)
    expect(counts.get('wg1')).toBe(1)
    expect(counts.get('bridge1')).toBe(1)
    expect(counts.has('unused')).toBe(false)
  })

  it('counts an event once per interface even if in and out match the same tunnel', () => {
    // Mirrors tunnelEventsOf's old `e.inInterface === iface || e.outInterface === iface`
    // OR check: one matching event is one count, not two.
    const counts = tunnelEventCounts([event({ inInterface: 'wg0', outInterface: 'wg0' })])
    expect(counts.get('wg0')).toBe(1)
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

  // Round 49 (#1016): the card says `name · subnet`, and the words UP
  // and QUIET are gone with every other coverage caption -- they were
  // drawn in the coverage badge's own ink and vocabulary, saying what
  // the ribs leaving the node now say themselves.
  it('writes nothing on the card when the router calls the tunnel up but nothing has crossed it', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'up' })]]])
    const { container } = render(Topography)
    flushSync()

    expect(tunnelCard(container)?.querySelector('.n-cov')).toBeNull()
    expect(tunnelCard(container)?.textContent).not.toContain('QUIET')
  })

  it('writes nothing on the card once traffic has actually crossed it either', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel({ apiState: 'up' })]]])
    appState.events = [event({ inInterface: 'wg0', srcIp: '10.99.0.2' })]
    const { container } = render(Topography)
    flushSync()

    expect(tunnelCard(container)?.querySelector('.n-cov')).toBeNull()
  })

  // The two states that stay: a tunnel the router calls down, and one
  // whose state was never pushed. Neither is coverage, and no line on
  // the 2D map draws either of them.
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

  it('draws the tunnel node once, not a second time as a leftover survey dot (#976 items 1/2)', () => {
    // The removed survey stop (#869) used to draw every node as a plain
    // dot plus label; the tunnel's own copy of that (`.g-dot`,
    // `.zone-label`) was never wired to any camera class, so it stayed
    // on screen at every altitude, its own "WireGuard" printed straight
    // over the card's -- the owner's "both renderings show at once".
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.g-dot')).toBeNull()
    expect(container.querySelector('.zone-dot')).toBeNull()
    expect(container.querySelector('.zone-label')).toBeNull()
    expect([...container.querySelectorAll('.n-name')].filter((n) => n.textContent?.trim() === 'WireGuard').length).toBe(1)
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

// Round 52 (#890): a second tunnel is no longer a design question. Every
// pushed tunnel is drawn as its own card in one group where wg0 is, and
// the frame fits whatever that group turns out to need.
describe('the tunnel cluster (#890, round 52)', () => {
  const wg = (iface: string, overrides: Partial<TunnelInterface> = {}) => tunnel({ iface, ...overrides })

  // Busiest first is what the packing order is, so give each one a
  // different number of lines and the order is decided rather than
  // alphabetical.
  function pushTunnels(names: string[]) {
    tunnelsState.byDevice = new Map([['router1', names.map((n) => wg(n))]])
    zonesState.pushed = names.map((n, i) => address({ interface: n, address: `10.9${i}.0.1/24`, network: `10.9${i}.0.0` }))
    appState.events = [
      event({ id: 1, inInterface: 'bridge1', srcIp: '192.168.1.5' }),
      ...names.flatMap((n, i) =>
        Array.from({ length: names.length - i }, (_x, k) => event({ id: 100 + i * 20 + k, inInterface: n, srcIp: `10.9${i}.0.2` })),
      ),
    ]
  }

  it('draws a card for every pushed tunnel, not just the busiest', () => {
    for (const n of [2, 3, 5]) {
      const names = Array.from({ length: n }, (_x, i) => `wg${i}`)
      pushTunnels(names)
      const { container, unmount } = render(Topography)
      flushSync()

      const cards = tunnelCards(container)
      expect(cards.map((c) => c.iface).sort(), `at ${n} tunnels`).toEqual(names.slice().sort())
      unmount()
    }
  })

  it('leaves the busiest exactly where round 30 drew it, and its rib with it', () => {
    // "With one tunnel nothing moves" -- the round-52 README. The
    // busiest keeps the seat whatever else is packed around it.
    pushTunnels(['wg0', 'wg1', 'wg2'])
    const { container } = render(Topography)
    flushSync()

    const busiest = tunnelCards(container).find((c) => c.iface === 'wg0')
    expect([busiest?.x, busiest?.y]).toEqual([1128, 132])
    const ribs = [...container.querySelectorAll('path.rib')].map((p) => p.getAttribute('d'))
    expect(ribs).toContain('M1080 186 C 990 215, 880 240, 830 252')
  })

  it('never overlaps two cards', () => {
    pushTunnels(['wg0', 'wg1', 'wg2', 'wg3', 'wg4'])
    const { container } = render(Topography)
    flushSync()

    // The card is drawn -84..+104 about its own point, 56 tall.
    const boxes = tunnelCards(container).map((c) => ({ x: c.x - 84, y: c.y - 28, w: 188, h: 56 }))
    expect(boxes).toHaveLength(5)
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const [a, b] = [boxes[i], boxes[j]]
        expect(a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h, `cards ${i} and ${j} overlap`).toBe(false)
      }
    }
  })

  it('gives every tunnel its own strand, and no two the same', () => {
    pushTunnels(['wg0', 'wg1', 'wg2'])
    const { container } = render(Topography)
    flushSync()

    const strands = [...container.querySelectorAll('path[data-tunnel-strand]')]
    expect(strands.map((p) => p.getAttribute('data-tunnel-strand')).sort()).toEqual(['wg0', 'wg1', 'wg2'])
    // Each leaves its own card: the path starts on one of that card's
    // own edges (its left, its top or its bottom).
    for (const c of tunnelCards(container)) {
      const d = strands.find((p) => p.getAttribute('data-tunnel-strand') === c.iface)?.getAttribute('d') ?? ''
      const [sx, sy] = /^M([-\d.]+) ([-\d.]+)/.exec(d)!.slice(1).map(Number)
      const onEdge = (sx === c.x - 84 && Math.abs(sy - c.y) <= 28) || (sx === c.x && Math.abs(Math.abs(sy - c.y) - 28) < 0.01)
      // wg0 keeps round 30's literal rib, which leaves from under its card.
      expect(onEdge || d === 'M1080 186 C 990 215, 880 240, 830 252', `${c.iface}: ${d}`).toBe(true)
    }
    expect(new Set(strands.map((p) => p.getAttribute('d'))).size).toBe(3)
  })

  it('keeps every tunnel out of the lane row, not just the busiest (#890 item 5)', () => {
    // The #877 build drew one and left the rest standing in the lane
    // row, where the same interface could read as a lane and a tunnel.
    pushTunnels(['wg0', 'wg1', 'wg2'])
    const { container } = render(Topography)
    flushSync()

    const laneCidrs = [...container.querySelectorAll('.zone .n-cidr')].map((n) => n.textContent?.replace(/\s+/g, ' ').trim() ?? '')
    for (const n of ['wg0', 'wg1', 'wg2']) expect(laneCidrs.some((t) => t.startsWith(n))).toBe(false)
    // And each is on the map exactly once, as its own card.
    expect(tunnelCards(container)).toHaveLength(3)
  })

  it('draws a tunnel not heard for a while with a dashed footprint and how long', () => {
    const heard = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString()
    tunnelsState.byDevice = new Map([['router1', [wg('wg0', { apiState: 'down', lastHeard: heard })]]])
    const { container } = render(Topography)
    flushSync()

    const card = tunnelCard(container)
    expect(card?.querySelector('.isl')?.classList.contains('quiet-print')).toBe(true)
    expect(card?.querySelector('.n-quiet')?.textContent?.replace(/\s+/g, ' ').trim()).toBe('quiet · 3 d')
  })

  it('writes no span when no handshake the server could date has been pushed', () => {
    // "How long" is not a question that data answers, so the card does
    // not answer it -- the same refusal the subnet slot makes.
    tunnelsState.byDevice = new Map([['router1', [wg('wg0', { apiState: 'down', lastHeard: null })]]])
    const { container } = render(Topography)
    flushSync()

    expect(tunnelCard(container)?.querySelector('.n-quiet')).toBeNull()
    expect(tunnelCard(container)?.querySelector('.isl')?.classList.contains('quiet-print')).toBe(true)
  })
})

describe('the frame fits the map, and the operator may move inside it (#890 items 3 and 4)', () => {
  it('opens on round 49\u2019s own frame while everything fits', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const { container } = render(Topography)
    flushSync()

    expect(mapViewBox(container)).toBe('0 0 1400 720')
  })

  it('stays on that frame at two, three and five tunnels', () => {
    for (const n of [2, 3, 5]) {
      const names = Array.from({ length: n }, (_x, i) => `wg${i}`)
      tunnelsState.byDevice = new Map([['router1', names.map((iface) => tunnel({ iface }))]])
      const { container, unmount } = render(Topography)
      flushSync()
      expect(mapViewBox(container), `at ${n} tunnels`).toBe('0 0 1400 720')
      unmount()
    }
  })

  it('grows, never shrinks, once the group needs more room', () => {
    const names = Array.from({ length: 12 }, (_x, i) => `wg${i}`)
    tunnelsState.byDevice = new Map([['router1', names.map((iface) => tunnel({ iface }))]])
    const { container } = render(Topography)
    flushSync()

    const [x, y, w, h] = mapViewBox(container).split(' ').map(Number)
    expect(w).toBeGreaterThanOrEqual(1400)
    expect(h).toBeGreaterThanOrEqual(720)
    // And it holds every card it drew, with the 40 px of air.
    for (const c of tunnelCards(container)) {
      expect(c.x - 84 - 40).toBeGreaterThanOrEqual(x)
      expect(c.y - 28 - 40).toBeGreaterThanOrEqual(y)
      expect(c.x + 104 + 40).toBeLessThanOrEqual(x + w)
      expect(c.y + 28 + 40).toBeLessThanOrEqual(y + h)
    }
  })

  it('carries a fit chip reading the zoom, and reads under 100 % once the frame has grown', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const bare = render(Topography)
    flushSync()
    const chip = bare.container.querySelector('.fitchip')
    expect(chip?.textContent?.replace(/\s+/g, ' ').trim()).toBe('100%⤢ fit')
    expect(chip?.getAttribute('aria-label')).toBe('The whole map, fitted')
    bare.unmount()

    const names = Array.from({ length: 12 }, (_x, i) => `wg${i}`)
    tunnelsState.byDevice = new Map([['router1', names.map((iface) => tunnel({ iface }))]])
    const { container } = render(Topography)
    flushSync()
    const pct = Number(/(\d+)%/.exec(container.querySelector('.fitchip')?.textContent ?? '')?.[1])
    expect(pct).toBeLessThan(100)
  })

  it('pans on the arrow keys, and the fit chip comes back to the fitted frame', () => {
    // Reduced motion, so the return is immediate and the test is not
    // racing an animation frame -- and so this covers that path too.
    const original = window.matchMedia
    window.matchMedia = ((q: string) => ({ matches: q.includes('reduced-motion'), media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia
    try {
      tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
      const { container } = render(Topography)
      flushSync()
      // Off the city default and onto the 2D map: the arrow keys belong
      // to whichever surface is actually on screen.
      showTheMap(container)
      expect(mapViewBox(container)).toBe('0 0 1400 720')

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      flushSync()
      const [x] = mapViewBox(container).split(' ').map(Number)
      expect(x).toBeGreaterThan(0)
      const chip = container.querySelector('.fitchip')!
      expect(chip.classList.contains('hand')).toBe(true)
      expect(chip.getAttribute('aria-label')).toBe('Zoomed by hand — fit the whole map')

      chip.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()
      expect(mapViewBox(container)).toBe('0 0 1400 720')
    } finally {
      window.matchMedia = original
    }
  })

  it('zooms on the wheel about the pointer, and never past its own bounds', () => {
    tunnelsState.byDevice = new Map([['router1', [tunnel()]]])
    const { container } = render(Topography)
    flushSync()

    const svg = container.querySelector('.stage > svg') as SVGSVGElement
    const box = { left: 0, top: 0, width: 1400, height: 700, right: 1400, bottom: 700, x: 0, y: 0 }
    svg.getBoundingClientRect = () => box as DOMRect

    svg.dispatchEvent(new WheelEvent('wheel', { deltaY: -240, clientX: 700, clientY: 350, bubbles: true, cancelable: true }))
    flushSync()
    const zoomed = mapViewBox(container).split(' ').map(Number)
    expect(zoomed[2]).toBeLessThan(1400)

    // A wheel held down does not run away with the map.
    for (let i = 0; i < 60; i++) {
      svg.dispatchEvent(new WheelEvent('wheel', { deltaY: -240, clientX: 700, clientY: 350, bubbles: true, cancelable: true }))
    }
    flushSync()
    expect(mapViewBox(container).split(' ').map(Number)[2]).toBeGreaterThanOrEqual(1400 * 0.25 - 0.5)
  })
})

// Round 49's declare path (#1016), on top of #392's record: the card is
// the one interaction on a boundary, the pin opens the form, and "both
// directions" writes the two boundary-direction keys the API is keyed
// by. The store's own methods are stubbed here -- what is under test is
// which keys the card asks for, not the HTTP call it makes.
describe('the boundary card and the declare path (round 49, #1016)', () => {
  const settle = () => new Promise((r) => setTimeout(r, 0))

  const guestDark = (): void => {
    zonesState.pushed = [{ address: '10.0.40.1/24', network: '10.0.40.0', interface: 'bridge4', comment: 'Guest' }]
    appState.events = [
      event({ inInterface: 'bridge4', srcIp: '10.0.40.9' }),
      event({ inInterface: 'ether1', srcIp: '8.8.8.8' }), // resolves ether1 as the WAN boundary
    ]
    policyState.anyPushed = true
    policyState.edges = [
      { key: 'bridge4|ether1', from: 'bridge4', to: 'ether1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
      { key: 'ether1|bridge4', from: 'ether1', to: 'bridge4', accepted: false, refused: true, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    ]
  }

  // A full lane row with a dark boundary between two neighbouring lanes:
  // the shape #1028's screenshot was taken in. With the foot of the map
  // occupied there is nowhere below the boundary for a card to go, so a
  // card that grows has to be placed again rather than left where it was.
  const laneRowDark = (): void => {
    const names = ['Guest', 'IoT', 'LitLane', 'Staff', 'Cams']
    zonesState.pushed = names.map((comment, i) => ({
      address: `10.0.8${i}.1/24`,
      network: `10.0.8${i}.0`,
      interface: `bridge${i + 1}`,
      comment,
    }))
    appState.events = names.map((_, i) => event({ inInterface: `bridge${i + 1}`, srcIp: `10.0.8${i}.9` }))
    policyState.anyPushed = true
    const edge = (from: string, to: string) => ({
      key: `${from}|${to}`,
      from,
      to,
      accepted: true,
      refused: false,
      acceptPorts: [],
      refusePorts: [],
      comment: '',
      ruleCount: 1,
      logged: false,
    })
    policyState.edges = [edge('bridge2', 'bridge3'), edge('bridge3', 'bridge2')]
  }

  function openCard(container: HTMLElement): HTMLElement {
    const half = container.querySelector('.cov-g')!
    half.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    return container.querySelector<HTMLElement>('.card')!
  }

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('opens a card on a dark boundary saying what the rule does and what both directions are', () => {
    guestDark()
    const { container } = render(Topography)
    flushSync()

    const card = openCard(container)
    expect(card).not.toBeNull()
    const text = card.textContent?.replace(/\s+/g, ' ') ?? ''
    expect(text).toContain('dark — nothing logs this boundary')
    expect(text).toContain('without log=yes') // what the rule does
    expect(text).toContain('the internet → Guest') // the other direction, said in full, in the map's own names
    const acts = [...card.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())
    expect(acts).toEqual(['declare quiet on purpose ▸', 'rules ▸', 'stream ▸'])
    expect(card.querySelector('.form')).toBeNull() // the form is behind the pin
  })

  it('opens the declare form on the pin, with both directions checked and who it will be signed by', () => {
    guestDark()
    const { container } = render(Topography)
    flushSync()

    const card = openCard(container)
    card.querySelector<HTMLButtonElement>('.pin')!.click()
    flushSync()

    const form = container.querySelector('.card .form')!
    expect(form).not.toBeNull()
    const both = form.querySelector<HTMLInputElement>('input[type="checkbox"]')!
    expect(both.checked).toBe(true)
    expect(form.querySelector('.who')?.textContent).toContain('as ')
  })

  it('declares both boundary-directions when both directions is left checked', async () => {
    guestDark()
    const declare = vi.spyOn(coverageState, 'declare').mockResolvedValue(true)
    const { container } = render(Topography)
    flushSync()

    const card = openCard(container)
    card.querySelector<HTMLButtonElement>('.pin')!.click()
    flushSync()
    const why = container.querySelector<HTMLInputElement>('.card .form input:not([type="checkbox"])')!
    why.value = 'guest devices only reach the internet'
    why.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    container.querySelector<HTMLButtonElement>('.card .form .go')!.click()
    await settle()

    expect(declare.mock.calls.map((c) => c[0])).toEqual(['bridge4|ether1', 'ether1|bridge4'])
    expect(declare.mock.calls[0][1]).toBe('guest devices only reach the internet')
  })

  it('declares one direction only when both directions is unchecked', async () => {
    guestDark()
    const declare = vi.spyOn(coverageState, 'declare').mockResolvedValue(true)
    const { container } = render(Topography)
    flushSync()

    const card = openCard(container)
    card.querySelector<HTMLButtonElement>('.pin')!.click()
    flushSync()
    container.querySelector<HTMLInputElement>('.card .form input[type="checkbox"]')!.click()
    flushSync()
    const why = container.querySelector<HTMLInputElement>('.card .form input:not([type="checkbox"])')!
    why.value = 'outbound only'
    why.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    container.querySelector<HTMLButtonElement>('.card .form .go')!.click()
    await settle()

    expect(declare.mock.calls.map((c) => c[0])).toEqual(['bridge4|ether1'])
  })

  it('reads a quiet boundary back: the reason quoted, who and when, and undeclare', () => {
    guestDark()
    coverageState.declarations = [
      { key: 'bridge4|ether1', reason: 'peers are trusted; logging them is noise', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
    ]
    const { container } = render(Topography)
    flushSync()

    const quiet = container.querySelector('.cedge.quiet')!.closest('.cov-g')!
    quiet.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    const card = container.querySelector('.card')!
    expect(card.querySelector('.quote')?.textContent).toBe('peers are trusted; logging them is noise')
    expect(card.textContent).toContain('tom')
    const acts = [...card.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())
    expect(acts[0]).toBe('undeclare ▸')
  })

  it('undeclares both directions when both were declared', async () => {
    guestDark()
    coverageState.declarations = [
      { key: 'bridge4|ether1', reason: 'noise', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
      { key: 'ether1|bridge4', reason: 'noise', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
    ]
    const undeclare = vi.spyOn(coverageState, 'undeclare').mockResolvedValue(true)
    const { container } = render(Topography)
    flushSync()

    openCard(container)
    container.querySelector<HTMLButtonElement>('.card .acts button.hot')!.click()
    await settle()

    expect(undeclare.mock.calls.map((c) => c[0])).toEqual(['bridge4|ether1', 'ether1|bridge4'])
  })

  it('offers a viewer the card but no way to declare — absent, never disabled (#490)', () => {
    authState.role = 'viewer'
    guestDark()
    const { container } = render(Topography)
    flushSync()

    const card = openCard(container)
    const acts = [...card.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())
    expect(acts).toEqual(['stream ▸'])
    expect(card.querySelector('.form')).toBeNull()
  })

  // The card is the one interaction, the same on both surfaces
  // (DESIGN.md "Cards"), so the pointer's journey from a boundary to its
  // own card is governed by the one rule in lib/cardAnchor.ts here as
  // well as in the city -- see City.svelte.test.ts for the same journey
  // on the other surface.
  it('opens on hover and stays up while the pointer travels to it (#1027)', async () => {
    vi.useFakeTimers()
    try {
      authState.role = 'admin'
      guestDark()
      const { container } = render(Topography)
      flushSync()

      const half = container.querySelector('.cov-g')!
      half.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.card'), 'hovering a boundary did not open its card').toBeTruthy()

      half.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
      flushSync()
      const card = container.querySelector<HTMLElement>('.card')
      expect(card, 'the card was gone before the pointer could reach it').toBeTruthy()

      card!.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      vi.advanceTimersByTime(5000)
      flushSync()
      expect(container.querySelector('.card'), 'the card closed while the pointer was on it').toBeTruthy()

      container.querySelector<HTMLButtonElement>('.card .pin')!.click()
      flushSync()
      expect(container.querySelector('.card')?.classList.contains('pinned')).toBe(true)
    } finally {
      vi.useRealTimers()
      authState.role = ''
    }
  })

  it('lets the card go once the pointer has arrived at neither the boundary nor the card', async () => {
    vi.useFakeTimers()
    try {
      guestDark()
      const { container } = render(Topography)
      flushSync()
      const half = container.querySelector('.cov-g')!
      half.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.card')).toBeTruthy()
      half.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
      flushSync()
      vi.advanceTimersByTime(5000)
      flushSync()
      expect(container.querySelector('.card')).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps the open boundary marked, pinned as well as hovered', async () => {
    authState.role = 'admin'
    guestDark()
    const { container } = render(Topography)
    flushSync()
    const half = container.querySelector('.cov-g')!
    expect(half.classList.contains('on')).toBe(false)
    openCard(container)
    expect(half.classList.contains('on'), 'the open boundary is not marked').toBe(true)
    container.querySelector<HTMLButtonElement>('.card .pin')!.click()
    flushSync()
    expect(container.querySelector('.cov-g')!.classList.contains('on'), 'the pinned boundary stopped being marked').toBe(true)
    authState.role = ''
  })

  it('keeps a pinned card when the pointer brushes past another boundary', async () => {
    // Hover opens cards, so without this a pinned card would be lost to
    // the next boundary the pointer happened to cross. The city's
    // `pinnedWall ?? hoverWall` says the same thing.
    authState.role = 'admin'
    guestDark()
    const { container } = render(Topography)
    flushSync()
    const halves = [...container.querySelectorAll('.cov-g')]
    expect(halves.length).toBeGreaterThan(1)
    openCard(container)
    container.querySelector<HTMLButtonElement>('.card .pin')!.click()
    flushSync()
    const pinned = container.querySelector('.card')?.getAttribute('aria-label')
    halves[1].dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    flushSync()
    expect(container.querySelector('.card')?.getAttribute('aria-label')).toBe(pinned)
    expect(container.querySelector('.card')?.classList.contains('pinned')).toBe(true)
    authState.role = ''
  })

  // #1028: the grown card came down on the zone plate named in its own
  // title and hid the very thing it was describing.
  //
  // What this can and cannot prove is worth being exact about, because
  // the first attempt at #1028 got it wrong. jsdom lays nothing out, so
  // it cannot say where a plate really is, and a test that invents the
  // plates' coordinates and then checks the card avoids them is checking
  // its own arithmetic -- that test passed while the defect was on
  // screen and photographed. The real gate is
  // `scripts/live-topography-card-placement.mjs`, which reads every
  // rectangle out of a real browser.
  //
  // What is honestly testable here is the part that went wrong: *which*
  // rectangles the card is told to keep off. A zone is drawn twice, as a
  // lane card and as a ground-plan card, and the stop swaps them with
  // `opacity: 0` rather than by removing either. So the browser's own
  // answers are supplied at the one boundary the component reads them
  // through -- `getBoundingClientRect` and the computed opacity -- and
  // the assertion is that the card follows whichever layer is drawn.
  // That is a claim about the component's logic, not about layout, and
  // jsdom can settle it.
  it('keeps off the zone plates that are actually drawn, not the layer the stop has hidden (#1028)', () => {
    resizeWatchers.clear()
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    try {
      laneRowDark()
      const { container } = render(Topography)
      flushSync()

      // Down onto the 2D map. Every test starts at the city, where the
      // stage carries `hidden` and nothing on it is drawn at all -- and
      // a card that has no visible subject to keep off is not the case
      // under test here.
      const range = container.querySelector<HTMLInputElement>('.alt-range')!
      range.value = '1' // "services"
      range.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()

      // The map's own 1400x720 rendered at 1400x720: user units and
      // container pixels then agree, so every number below is readable.
      const host = container.querySelector('.topo') as HTMLElement
      const svg = [...container.querySelectorAll('svg')].find((s) => s.getAttribute('viewBox') === '0 0 1400 720') as SVGSVGElement
      const box = { left: 0, top: 0, width: 1400, height: 720, right: 1400, bottom: 720, x: 0, y: 0 }
      host.getBoundingClientRect = () => box as DOMRect
      svg.getBoundingClientRect = () => box as DOMRect
      ;(svg as unknown as { getScreenCTM: () => DOMMatrix }).getScreenCTM = () => ({ a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }) as DOMMatrix

      type Box = { x: number; y: number; w: number; h: number }
      const hits = (a: Box, b: Box) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

      // Give every plate the rendered box a browser would report for it,
      // taken from what the component actually drew. Without this the
      // component measures zeros -- jsdom's answer for everything -- and
      // has no rectangles to keep off at all.
      const stub = (el: Element, b: Box) => {
        el.getBoundingClientRect = () => ({ left: b.x, top: b.y, width: b.w, height: b.h, right: b.x + b.w, bottom: b.y + b.h, x: b.x, y: b.y }) as DOMRect
      }
      const boxOf = (g: Element, rectSel: string): Box => {
        const [tx, ty] = /translate\(([-\d.]+) ([-\d.]+)\)/.exec(g.getAttribute('transform') ?? '')!.slice(1).map(Number)
        const r = g.querySelector(rectSel) as SVGRectElement
        return { x: tx + Number(r.getAttribute('x')), y: ty + Number(r.getAttribute('y')), w: Number(r.getAttribute('width')), h: Number(r.getAttribute('height')) }
      }
      const layer = (groupSel: string, rectSel: string) => {
        const out = new Map<string, { g: Element; box: Box }>()
        for (const g of container.querySelectorAll(groupSel)) {
          const rect = g.querySelector(rectSel)
          if (!rect) continue
          const box = boxOf(g, rectSel)
          stub(rect, box)
          out.set(g.getAttribute('data-zone') ?? '', { g, box })
        }
        return out
      }
      const lane = layer('g.zone', 'rect.isl')
      const groundPlan = layer('g.gf-card', 'rect.gf-plate')

      // Which layer the stop is showing, done the way the stylesheet
      // does it -- the hidden one stays in the DOM, still measurable.
      const show = (which: Map<string, { g: Element; box: Box }>, on: boolean) => {
        for (const { g } of which.values()) (g as SVGElement).style.opacity = on ? '1' : '0'
      }

      expect(lane.size, 'the lane row drew no plates to keep off').toBeGreaterThan(0)
      expect(groundPlan.size, 'the ground plan drew no plates to keep off').toBeGreaterThan(0)

      const drawn = (h: number): Box => {
        const el = container.querySelector('.card') as HTMLElement
        return { x: parseFloat(el.style.left), y: parseFloat(el.style.top), w: 288, h }
      }

      // The IoT ⇄ LitLane boundary, which is what the card will name.
      const half = [...container.querySelectorAll('.cov-g')].find((g) => {
        const l = g.getAttribute('aria-label') ?? ''
        return l.includes('IoT') && l.includes('LitLane')
      })!
      half.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()

      const card = container.querySelector('.card') as HTMLElement
      expect(card, 'no card opened on the boundary').not.toBeNull()
      expect(card.classList.contains('placed'), 'the card never got a measured position').toBe(true)

      card.querySelector<HTMLButtonElement>('.pin')!.click()
      flushSync()
      expect(container.querySelector('.card .form'), 'the declare form never opened').not.toBeNull()

      // The two ends the card names, by the id both layers tag their
      // plate with.
      const idOf = (name: string) =>
        [...container.querySelectorAll('g.zone')].find((z) => z.getAttribute('aria-label')?.includes(name))!.getAttribute('data-zone')!
      const ends = [idOf('IoT'), idOf('LitLane')]

      // The form is in, so the card is taller. Growing it is what makes
      // the placement run again, so each case grows it to a size it has
      // not been -- `watchCardSize` drops a report that says nothing new.
      const open = container.querySelector('.card') as HTMLElement
      Object.defineProperty(open, 'offsetWidth', { value: 288, configurable: true })
      const growTo = (h: number) => {
        Object.defineProperty(open, 'offsetHeight', { value: h, configurable: true })
        reportResize(open)
        flushSync()
        return drawn(h)
      }

      // Clients and services: the lane row is the drawing.
      show(lane, true)
      show(groundPlan, false)
      const onLaneRow = growTo(420)
      for (const id of ends) {
        expect(hits(onLaneRow, lane.get(id)!.box), `the grown card came down on the ${id} lane plate, named in its own title`).toBe(false)
      }

      // Zones: the ground plan has replaced the lane row, in a different
      // place. This is the case the screenshot caught -- the card
      // cleared the lane row, which nobody could see, and sat on the
      // ground-plan plate, which they could.
      show(lane, false)
      show(groundPlan, true)
      const onGroundPlan = growTo(430)
      for (const id of ends) {
        expect(hits(onGroundPlan, groundPlan.get(id)!.box), `the grown card came down on the ${id} ground-plan plate, named in its own title`).toBe(false)
      }

      // And it is still a card on the stage, not one shoved off it.
      for (const after of [onLaneRow, onGroundPlan]) {
        expect(after.x).toBeGreaterThanOrEqual(0)
        expect(after.y).toBeGreaterThanOrEqual(0)
        expect(after.x + after.w).toBeLessThanOrEqual(1400)
        expect(after.y + after.h).toBeLessThanOrEqual(720)
      }
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

// ---------------------------------------------------------------------
// Living hosts on the 2D map (#1016; DESIGN.md "Living hosts").
//
// Presence comes from the server's host register, not from the event
// buffer: a machine that stops talking scrolls out of the buffer, and
// the one thing presence must not do is let it vanish. These drive
// hostsState directly, the same way the rest of this file drives
// zonesState and coverageState, rather than mocking ../lib/api.
// ---------------------------------------------------------------------
describe('living hosts on the 2D map (#1016)', () => {
  const HOUR = 60 * 60_000

  const oneLane: RouterIPAddress[] = [{ address: '10.0.1.1/24', network: '10.0.1.0', interface: 'bridge1', comment: 'Lane 1' }]

  function host(over: Partial<Host> & { ip: string }): Host {
    const now = Date.now()
    return {
      key: `bridge1|${over.ip}`,
      iface: 'bridge1',
      firstSeen: new Date(now - 30 * 24 * HOUR).toISOString(),
      lastSeen: new Date(now - 60_000).toISOString(),
      events: 1204,
      ...over,
    }
  }

  /** One lane the register has answered for, with no live traffic of its
   * own -- so what draws is the register's word and nothing else. */
  function laneOf(...hosts: Host[]) {
    zonesState.pushed = oneLane
    appState.events = []
    hostsState.hosts = hosts
  }

  function dots(container: HTMLElement) {
    return [...container.querySelectorAll('.zone .hostrow .h-dot')]
  }

  function openCard(container: HTMLElement) {
    container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    flushSync()
    return container.querySelector<HTMLDivElement>('.card[aria-label^="The host"]')
  }

  describe('presence', () => {
    it('draws a live host in its lane ink, with no footprint and nothing to explain', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      const [d] = dots(container)
      expect(d.getAttribute('fill')).toBe('var(--lane-lan)')
      expect(d.classList.contains('quiet')).toBe(false)
      expect(d.classList.contains('intended')).toBe(false)
      expect(container.querySelector('.zone .hostrow .h-foot')).toBeNull()
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host')
    })

    it('greys a host nothing has been heard from for the window, with a dashed footprint, and says how long', () => {
      // 26 h -- the owner's own example, and comfortably past the
      // ratified 24-hour window. Ten minutes of silence is not evidence
      // of anything, which is why the window is a working day.
      laneOf(host({ ip: '10.0.1.60', label: 'tv-lounge', lastSeen: new Date(Date.now() - 26 * HOUR).toISOString() }))
      const { container } = render(Topography)
      flushSync()

      const [d] = dots(container)
      expect(d.classList.contains('quiet')).toBe(true)
      // The class carries the grey, so the lane ink is not also set.
      expect(d.getAttribute('fill')).toBeNull()
      expect(container.querySelector('.zone .hostrow .h-foot')).not.toBeNull()
      expect(container.querySelector('.zone .hostrow title')?.textContent).toBe('tv-lounge · quiet · 26 h')
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host · 1 quiet')
    })

    it('leaves a host live at 23 hours: a short silence is not evidence of anything', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop', lastSeen: new Date(Date.now() - 23 * HOUR).toISOString() }))
      const { container } = render(Topography)
      flushSync()

      expect(dots(container)[0].classList.contains('quiet')).toBe(false)
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host')
    })

    it('draws a host marked quiet on purpose white translucent, and counts it as such', () => {
      laneOf(
        host({
          ip: '10.0.1.70',
          label: 'printer-old',
          lastSeen: new Date(Date.now() - 40 * HOUR).toISOString(),
          mark: { kind: 'intended', reason: 'switched off at the wall', by: 'tom', at: new Date().toISOString() },
        }),
      )
      const { container } = render(Topography)
      flushSync()

      const [d] = dots(container)
      expect(d.classList.contains('intended')).toBe(true)
      expect(d.classList.contains('quiet')).toBe(false)
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host · 1 quiet on purpose')
    })

    it('draws a host marked quiet on purpose as plainly live while it is still talking', () => {
      // The mark is a statement about a silence, not a permanent label:
      // while the machine is talking it is simply live, and the
      // explanation waits for the next silence.
      laneOf(
        host({
          ip: '10.0.1.70',
          label: 'printer-old',
          mark: { kind: 'intended', reason: 'switched off at the wall', by: 'tom', at: new Date().toISOString() },
        }),
      )
      const { container } = render(Topography)
      flushSync()

      expect(dots(container)[0].classList.contains('intended')).toBe(false)
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host')
    })

    it('takes a dismissed host off the map without touching the others', () => {
      laneOf(
        host({ ip: '10.0.1.20', label: 'tom-desktop' }),
        host({ ip: '10.0.1.99', label: 'gone', mark: { kind: 'dismissed', by: 'tom', at: new Date().toISOString() } }),
      )
      const { container } = render(Topography)
      flushSync()

      expect(dots(container).length).toBe(1)
      expect(container.querySelector('.zone .hostrow title')?.textContent).toBe('tom-desktop')
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('1 host')
    })

    it('keeps drawing a host the event buffer has forgotten', () => {
      // The whole point: zonesState derives its hosts from the buffer, so
      // with no events at all it knows of none. The register does.
      laneOf(host({ ip: '10.0.1.60', label: 'tv-lounge', lastSeen: new Date(Date.now() - 26 * HOUR).toISOString() }))
      const { container } = render(Topography)
      flushSync()

      expect(dots(container).length).toBe(1)
    })

    it('draws a host the buffer has seen but the register has not answered for yet', () => {
      zonesState.pushed = oneLane
      appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'tom-desktop' })]
      hostsState.hosts = []
      const { container } = render(Topography)
      flushSync()

      // Being in the buffer is evidence of having just been heard, so
      // live is the honest reading rather than a guess.
      expect(dots(container).length).toBe(1)
      expect(dots(container)[0].classList.contains('quiet')).toBe(false)
    })

    it('does not draw the same host twice when both the buffer and the register have it', () => {
      zonesState.pushed = oneLane
      appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'tom-desktop' })]
      hostsState.hosts = [host({ ip: '10.0.1.20', label: 'tom-desktop' })]
      const { container } = render(Topography)
      flushSync()

      expect(dots(container).length).toBe(1)
    })
  })

  describe('the dot row', () => {
    const many = (n: number) =>
      Array.from({ length: n }, (_, i) => host({ ip: `10.0.1.${20 + i}`, label: `host-${String(i).padStart(2, '0')}` }))

    it('draws ten dots then +N, and counts every host including the ones past ten', () => {
      laneOf(...many(14))
      const { container } = render(Topography)
      flushSync()

      expect(dots(container).length).toBe(10)
      expect(container.querySelector('.zone .hostrow .c-label.more')?.textContent).toBe('+4')
      expect(container.querySelector('.zone .hosttally')?.textContent).toBe('14 hosts')
    })

    it('draws no +N at exactly ten', () => {
      laneOf(...many(10))
      const { container } = render(Topography)
      flushSync()

      expect(dots(container).length).toBe(10)
      expect(container.querySelector('.zone .hostrow .c-label.more')).toBeNull()
    })

    it("spaces the dots evenly from the card's own left inset", () => {
      laneOf(...many(3))
      const { container } = render(Topography)
      flushSync()

      const xs = dots(container).map((d) => Number(d.getAttribute('cx')))
      expect(xs[1] - xs[0]).toBeCloseTo(xs[2] - xs[1], 5)
      expect(xs[1] - xs[0]).toBeGreaterThan(0)
      // Every dot on the row's own baseline, the mockup's y 56.
      expect(dots(container).every((d) => d.getAttribute('cy') === '56')).toBe(true)
    })

    // #981: no toggle. The halo is there because the host has an open
    // flag and the ring because something watches it -- there is no pill
    // in the row that could have switched either on, and none that could
    // switch them off. Take the flag and the watcher away and both marks
    // go with them, which is the whole of the rule.
    it('halos a flagged host and rings a watched one, with no pill anywhere on the page', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      flagsState.list = [flag('port_scan', '10.0.1.20')]
      watchlistState.entries = [watchEntry({ destIp: '10.0.1.20' })]
      const { container } = render(Topography)
      flushSync()
      showTheMap(container)

      // The one pill in the row is #1018's port filter, and it switches
      // neither of these marks: take the flag and the watcher away and
      // both go, whatever the filter is doing.
      expect([...container.querySelectorAll('.pills .pill')].map((p) => p.textContent?.trim())).toEqual(['⌕ port'])
      expect(container.querySelector('.zone .hostrow .h-halo')).not.toBeNull()
      expect(container.querySelector('.zone .hostrow .h-watch')).not.toBeNull()
    })

    it('draws neither mark on a host with nothing behind it', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      flagsState.list = []
      watchlistState.entries = []
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelector('.zone .hostrow .h-halo')).toBeNull()
      expect(container.querySelector('.zone .hostrow .h-watch')).toBeNull()
    })

    it('throbs the flagged halo in place rather than pulsing it outward', () => {
      // Owner, 2026-09-07: a ring that travels outward reads as
      // something moving through the network, and nothing here moved.
      // So the animation may touch opacity and weight, never the radius.
      const frames = componentSource.match(/@keyframes h-halo \{[^}]*\{[^}]*\}[^}]*\{[^}]*\}[^}]*\}/)
      expect(frames).not.toBeNull()
      expect(frames![0]).not.toMatch(/\br\s*:/)
      expect(frames![0]).toMatch(/stroke-opacity/)
      expect(frames![0]).toMatch(/stroke-width/)
    })

    it('opens the reach on a dot, the same place the old name list went', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()

      expect(container.querySelector('.membrane-layer')).not.toBeNull()
    })
  })

  describe('the host card', () => {
    it('says the presence, both timestamps and the event count, and offers the two marks', () => {
      laneOf(host({ ip: '10.0.1.60', label: 'tv-lounge', lastSeen: new Date(Date.now() - 26 * HOUR).toISOString() }))
      const { container } = render(Topography)
      flushSync()

      const card = openCard(container)
      expect(card).not.toBeNull()
      expect(card!.textContent).toContain('quiet')
      expect(card!.textContent).toContain('26 h')
      expect(card!.textContent).toContain('first seen')
      expect(card!.textContent).toMatch(/1[,. ]?204 events/)
      expect(card!.textContent).toContain('comes back by itself')
      const acts = [...card!.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())
      expect(acts).toContain('mark quiet on purpose ▸')
      expect(acts).toContain('dismiss ▸')
    })

    it('floats beside its dot on the shared card anchor, never a second placement beside it', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      expect(openCard(container)).not.toBeNull()
      // jsdom lays nothing out, so the placement itself declines rather
      // than taking a wrong one -- but the card and its re-placement are
      // lib/cardAnchor's, not a second implementation.
      expect(componentSource).toMatch(/hostCardPlace = placeCard\(/)
      expect(componentSource).toMatch(/watchCardSize\(card, \(\) => hostCardTick\+\+\)/)
    })

    it('survives the pointer leaving the dot, so it is still there when the pointer arrives', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      const dot = container.querySelector<SVGGElement>('.hostrow .hot')!
      dot.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      dot.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
      flushSync()

      // The grace period has not elapsed, so the card is still there.
      expect(container.querySelector('.card[aria-label^="The host"]')).not.toBeNull()
    })

    it('marks its own dot while the card is open, so it is clear which host is being read', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelector('.zone .hostrow .h-open')).toBeNull()
      openCard(container)
      expect(container.querySelector('.zone .hostrow .h-open')).not.toBeNull()
    })

    it('comes down when its own reach opens, rather than waiting behind it', () => {
      laneOf(host({ ip: '10.0.1.20', label: 'tom-desktop' }))
      const { container } = render(Topography)
      flushSync()

      const dot = container.querySelector<SVGGElement>('.hostrow .hot')!
      dot.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.card[aria-label^="The host"]')).not.toBeNull()

      dot.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.membrane-layer')).not.toBeNull()
      expect(container.querySelector('.card[aria-label^="The host"]')).toBeNull()
    })
  })

  describe('the marks', () => {
    function act(card: HTMLElement, label: string) {
      return [...card.querySelectorAll<HTMLButtonElement>('.acts button')].find((b) => b.textContent?.trim() === label)
    }

    const quiet = () => host({ ip: '10.0.1.60', label: 'tv-lounge', lastSeen: new Date(Date.now() - 26 * HOUR).toISOString() })

    it('asks for a reason before marking quiet on purpose -- the reason is the mark', () => {
      laneOf(quiet())
      const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
      const { container } = render(Topography)
      flushSync()

      act(openCard(container)!, 'mark quiet on purpose ▸')!.click()
      flushSync()

      expect(container.querySelector('.card .form input')).not.toBeNull()
      // Nothing is written without one: an empty statement says nothing.
      expect(container.querySelector<HTMLButtonElement>('.card .form .go')!.disabled).toBe(true)
      expect(mark).not.toHaveBeenCalled()
    })

    it('writes the intended mark through the host register, with its reason', async () => {
      laneOf(quiet())
      const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
      const { container } = render(Topography)
      flushSync()

      act(openCard(container)!, 'mark quiet on purpose ▸')!.click()
      flushSync()

      const input = container.querySelector<HTMLInputElement>('.card .form input')!
      input.value = 'switched off at the wall'
      input.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
      container.querySelector<HTMLButtonElement>('.card .form .go')!.click()
      await vi.waitFor(() => expect(mark).toHaveBeenCalled())

      expect(mark).toHaveBeenCalledWith('bridge1|10.0.1.60', 'intended', 'switched off at the wall')
    })

    it('dismisses through the same register, with no reason to give', async () => {
      laneOf(quiet())
      const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
      const { container } = render(Topography)
      flushSync()

      act(openCard(container)!, 'dismiss ▸')!.click()
      await vi.waitFor(() => expect(mark).toHaveBeenCalled())

      expect(mark).toHaveBeenCalledWith('bridge1|10.0.1.60', 'dismissed')
    })

    it('offers to take a mark back, quoting what was said, and does it through the register', async () => {
      laneOf(
        host({
          ip: '10.0.1.70',
          label: 'printer-old',
          lastSeen: new Date(Date.now() - 40 * HOUR).toISOString(),
          mark: { kind: 'intended', reason: 'switched off at the wall', by: 'tom', at: new Date().toISOString() },
        }),
      )
      const unmark = vi.spyOn(hostsState, 'unmark').mockResolvedValue(true)
      const { container } = render(Topography)
      flushSync()

      const card = openCard(container)!
      // Taking it back is an informed act, not a guess at what was said.
      expect(card.textContent).toContain('switched off at the wall')
      expect(act(card, 'mark quiet on purpose ▸')).toBeUndefined()
      act(card, 'unmark ▸')!.click()
      await vi.waitFor(() => expect(unmark).toHaveBeenCalled())

      expect(unmark).toHaveBeenCalledWith('bridge1|10.0.1.70')
    })

    it("shows the register's own failure on the card rather than pretending the mark took", async () => {
      laneOf(quiet())
      vi.spyOn(hostsState, 'mark').mockImplementation(async () => {
        hostsState.error = 'putHostMark: 403'
        return false
      })
      const { container } = render(Topography)
      flushSync()

      act(openCard(container)!, 'dismiss ▸')!.click()
      await vi.waitFor(() => expect(container.querySelector('.card .d-error')).not.toBeNull())

      expect(container.querySelector('.card .d-error')?.textContent).toContain('403')
      // Still on the map: nothing was written, so nothing is taken away.
      expect(dots(container).length).toBe(1)
    })

    it('offers no marks to a reader, and still reads the facts out', () => {
      authState.role = 'viewer'
      laneOf(quiet())
      const { container } = render(Topography)
      flushSync()

      const card = openCard(container)!
      expect(act(card, 'mark quiet on purpose ▸')).toBeUndefined()
      expect(act(card, 'dismiss ▸')).toBeUndefined()
      expect(card.textContent).toContain('quiet')
    })

    it('offers no marks for a host the register has not answered for', () => {
      // Its key is the map's own construction, not a record, and
      // internal/hosts refuses a mark on a key it does not know.
      zonesState.pushed = oneLane
      appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.1.20', srcHostName: 'tom-desktop' })]
      hostsState.hosts = []
      const { container } = render(Topography)
      flushSync()

      const card = openCard(container)!
      expect(act(card, 'mark quiet on purpose ▸')).toBeUndefined()
      expect(act(card, 'dismiss ▸')).toBeUndefined()
      expect(card.textContent).toContain('has not answered for it yet')
    })
  })
})

// Round 49's second always-on rule (#1016): colour is the verdict,
// brightness is the baseline. A line on the pattern recedes; a line off
// it is bright, with a ring where it arrived. Nothing is ever removed
// from the map by any of this -- the whole point is a sieve rather than
// a filter, so every assertion below is about how something is drawn and
// never about whether it is drawn at all.
//
// jsdom returns zeros from getBoundingClientRect and lays nothing out,
// so nothing here asserts a pixel. What it asserts is the decisions: how
// many elements light, which ones, what the card says, and what the
// `expected` action actually writes.
describe('brightness is the baseline (round 49, #1016)', () => {
  let nextOffKey = 1

  function offLine(over: Partial<OffBaselineLine> = {}): OffBaselineLine {
    return {
      key: `line${nextOffKey++}`,
      srcIp: '10.0.10.21',
      dstIp: '10.0.20.10',
      port: 5001,
      proto: 'tcp',
      count: 40,
      firstSeenToday: Date.parse('2026-09-07T21:26:00Z'),
      outcome: 'accept',
      ...over,
    }
  }

  /** What the register answered. Only today's off-baseline lines are
   * ever in it -- there is no established set to seed, which is exactly
   * the fact the roll-up has to be built on. */
  function seedOff(lines: OffBaselineLine[], count = lines.length) {
    baselineState.off = { config: { days: 3, of: 14 }, generatedAt: Date.now(), count, lines }
  }

  function loggedEdge(from: string, to: string) {
    return { key: `${from}|${to}`, from, to, accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: true }
  }

  /** Two lanes that log both ways and talk both ways: two accepted rib
   * halves, so "one lit, one not" is a claim the drawing can settle. */
  function twoLanes(extraEvents: ClientEvent[] = []) {
    zonesState.pushed = [
      { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'bridge2', comment: 'Servers' },
    ]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.10.21', dstIp: '10.0.20.10', action: 'accept', dstPort: 5001 }),
      event({ inInterface: 'bridge2', outInterface: 'bridge1', srcIp: '10.0.20.10', dstIp: '10.0.10.21', action: 'accept', dstPort: 443 }),
      ...extraEvents,
    ]
    policyState.anyPushed = true
    policyState.edges = [loggedEdge('bridge1', 'bridge2'), loggedEdge('bridge2', 'bridge1')]
  }

  describe('the roll-up', () => {
    it('lights the one half that carries an off-baseline line, and lets the other recede', () => {
      twoLanes()
      seedOff([offLine()]) // 10.0.10.21 -> 10.0.20.10 is bridge1 -> bridge2
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelectorAll('.redge.offbase').length).toBe(1)
      expect(container.querySelectorAll('.redge.established').length).toBe(1)
      // Nothing was removed: both halves are still drawn.
      expect(container.querySelectorAll('.redge').length).toBe(2)
    })

    it('draws the established half thin and the off-baseline half at full width', () => {
      twoLanes()
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      const width = (sel: string) => {
        const style = container.querySelector(sel)!.getAttribute('style') ?? ''
        return Number(/stroke-width:\s*([\d.]+)px/.exec(style)![1])
      }
      expect(width('.redge.established')).toBeLessThan(width('.redge.offbase'))
    })

    it('gives the off-baseline half flow dashes and a ring, and the established half neither', () => {
      twoLanes()
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      // Scoped to the ribs: the city draws its own roads into the same
      // document and marks each with data-road, and an unscoped count
      // here would be answering for both surfaces at once.
      const ribs = (sel: string) =>
        Array.from(container.querySelectorAll(sel)).filter((n) => n.closest('.edge-g'))
      expect(ribs('.flow').length).toBe(1)
      expect(ribs('.nb-ring').length).toBe(1)
      // The flow belongs to the lit half, not to the dim one.
      expect(container.querySelector('.redge.offbase')!.parentElement!.querySelector('.flow')).not.toBeNull()
      expect(container.querySelector('.redge.established')!.parentElement!.querySelector('.flow')).toBeNull()
    })

    it('lights one place for one off-baseline line among a thousand established ones', () => {
      // A thousand events on the same pair is a thousand established
      // lines the register never sends: the map only ever hears about
      // the one that is off the pattern.
      const busy: ClientEvent[] = []
      for (let i = 0; i < 1000; i++) {
        busy.push(event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.10.21', dstIp: '10.0.20.10', action: 'accept', dstPort: 443 }))
      }
      twoLanes(busy)
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelectorAll('.redge.offbase').length).toBe(1)
      expect(container.querySelectorAll('.nb-ring').length).toBe(1)
    })

    it('does not grow the drawing when the off-baseline set grows', () => {
      twoLanes()
      // Three hundred distinct lines, every one of them on the same
      // zone pair: one rib per pair is the promise, whatever the volume.
      seedOff(Array.from({ length: 300 }, (_, i) => offLine({ port: 5000 + i })))
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelectorAll('.redge').length).toBe(2)
      expect(container.querySelectorAll('.redge.offbase').length).toBe(1)
      expect(container.querySelectorAll('.nb-ring').length).toBe(1)
    })

    it('leaves every rib established when nothing is off the baseline', () => {
      twoLanes()
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelectorAll('.redge.established').length).toBe(2)
      expect(container.querySelector('.redge.offbase')).toBeNull()
      expect(container.querySelector('.nb-ring')).toBeNull()
    })

    it('attributes a line to no rib at all when its addresses are on no lane the map draws', () => {
      twoLanes()
      seedOff([offLine({ srcIp: '172.16.9.4', dstIp: '172.16.9.5' })])
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelector('.redge.offbase')).toBeNull()
      expect(container.querySelectorAll('.redge.established').length).toBe(2)
    })

    it('rings the host dot that carries an off-baseline line, and no other on its lane', () => {
      twoLanes()
      const seen = new Date().toISOString()
      const at = (ip: string, label: string): Host => ({
        key: `bridge1|${ip}`,
        iface: 'bridge1',
        ip,
        label,
        firstSeen: '2026-08-01T00:00:00Z',
        lastSeen: seen,
        events: 12,
      })
      hostsState.hosts = [at('10.0.10.21', 'tom-desktop'), at('10.0.10.22', 'phone-tom'), at('10.0.10.23', 'tablet')]
      seedOff([offLine()]) // tom-desktop is this line's source
      const { container } = render(Topography)
      flushSync()

      const lan = container.querySelector('g.zone[data-zone="bridge1"]')!
      expect(lan.querySelectorAll('.h-nb').length).toBe(1)
      // The other two are still drawn -- they only stopped being lit.
      expect(lan.querySelectorAll('.h-dot').length).toBe(3)
      // The ring is on tom-desktop's own dot, not on whichever came first.
      expect(lan.querySelector('.h-nb')!.parentElement!.querySelector('title')!.textContent).toContain('tom-desktop')
    })
  })

  describe('the header count', () => {
    it('counts today’s off-baseline lines, before the flags count and in the accept ink', () => {
      twoLanes()
      seedOff([offLine(), offLine({ port: 22, srcIp: '10.0.10.22' }), offLine({ port: 445, outcome: 'drop' })])
      const { container } = render(Topography)
      flushSync()

      const mark = container.querySelector('.pills .nmk')!
      expect(mark.textContent).toContain('off-baseline')
      expect(mark.textContent).toContain('3')
      // It is the whole of the row now: the ⚑ and ◉ pills it used to sit
      // ahead of went with #981, and a count is not a control.
      expect(mark.nextElementSibling).toBeNull()
      expect(componentSource).toMatch(/\.nmk\s*\{[^}]*color:\s*var\(--accept\)/)
    })

    it('reports the register’s own count, not the map’s share of it', () => {
      // A line on no lane this map draws still happened, and the header
      // is reporting the register rather than the drawing.
      twoLanes()
      seedOff([offLine({ srcIp: '172.16.9.4', dstIp: '172.16.9.5' })])
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelector('.pills .nmk')!.textContent).toContain('1')
    })

    it('says nothing at all when nothing is off the baseline', () => {
      // Zero is also what an unread register looks like, so a permanent
      // "· 0" would be an all-clear the map has not been told.
      twoLanes()
      const { container } = render(Topography)
      flushSync()

      expect(container.querySelector('.pills .nmk')).toBeNull()
    })
  })

  describe('the off-baseline card and the expected write path', () => {
    /** Hover the lit half, which is the card's one way in. */
    function openOffCard(container: HTMLElement) {
      container.querySelector('.redge.offbase')!.parentElement!.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      return container.querySelector<HTMLDivElement>('.card[aria-label^="Off the baseline"]')
    }

    /** The open card, re-read after a click has re-rendered it. */
    function offCard(container: HTMLElement) {
      return container.querySelector<HTMLDivElement>('.card[aria-label^="Off the baseline"]')!
    }

    function act(card: HTMLElement, label: string) {
      return [...card.querySelectorAll<HTMLButtonElement>('.acts button')].find((b) => b.textContent?.trim() === label)
    }

    /** One line's own `expected ▸`, the default action (#1016). Keyed by
     * the line rather than by position, so a test names which line it
     * meant rather than trusting the table's order. */
    function expectedFor(card: HTMLElement, key: string) {
      return card.querySelector<HTMLButtonElement>(`.linkact[data-expected-one="${key}"]`)
    }

    /** Every per-line `expected ▸` the card is offering. */
    function perLineActs(card: HTMLElement) {
      return [...card.querySelectorAll<HTMLButtonElement>('.linkact[data-expected-one]')]
    }

    /** The bulk control, which is the only way to mark more than one. */
    function bulkAct(card: HTMLElement) {
      return card.querySelector<HTMLButtonElement>('.allof .linkact.all')
    }

    function typeReason(card: HTMLElement, reason: string) {
      const input = card.querySelector<HTMLInputElement>('.form input')!
      input.value = reason
      input.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
    }

    /** The line keys every PUT actually wrote, in order. */
    function written(calls: { url: string; init?: RequestInit }[]) {
      return calls.filter((c) => c.init?.method === 'PUT').map((c) => /\/api\/baseline\/(.+)\/expected$/.exec(c.url)![1])
    }

    /** The register's own endpoints, answered rather than reached. The
     * assertion is what was written, so the write has to travel the real
     * path -- api.putBaselineExpected -- not a spy on the store. */
    function stubApi(seeded: OffBaselineLine[] = []) {
      const calls: { url: string; init?: RequestInit }[] = []
      // A line that has been spoken for stops being off the baseline, so
      // the refresh that follows a write answers with the rest. Marking
      // one of several has to leave the others in the register, or no
      // test here can tell per-line from bulk (#1016).
      const marked = new Set<string>()
      vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => {
          calls.push({ url, init })
          if (url.startsWith('/api/baseline/off')) {
            const lines = seeded.filter((l) => !marked.has(l.key))
            return {
              ok: true,
              status: 200,
              json: async () => ({ config: { days: 3, of: 14 }, generatedAt: 1, count: lines.length, lines, hostQuietAfterMs: 86_400_000 }),
            } as unknown as Response
          }
          const put = init?.method === 'PUT' ? /\/api\/baseline\/(.+)\/expected$/.exec(url) : null
          if (put) marked.add(put[1])
          return { ok: true, status: 200, json: async () => ({ key: put?.[1] ?? 'line1' }), text: async () => '' } as unknown as Response
        }),
      )
      return calls
    }

    afterEach(() => {
      vi.unstubAllGlobals()
    })

    it('rolls the rib up: the established word, the count, and the line spelled out', () => {
      twoLanes()
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      // Wrapped source text, so the reading is on one line of whitespace.
      const said = openOffCard(container)!.textContent!.replace(/\s+/g, ' ')
      const card = container.querySelector<HTMLDivElement>('.card[aria-label^="Off the baseline"]')!
      expect(said).toContain('established')
      // The threshold the server actually applied, not a hard-coded pair.
      expect(said).toContain('3 of the last 14 days')
      expect(said).toContain('1 off the baseline today')
      const row = card.querySelector('tbody tr')!
      expect(row.textContent).toContain('5001/tcp')
      expect(row.textContent).toContain('40')
      expect(row.textContent).toContain('today')
    })

    it('says plainly what the verdict was and what it means', () => {
      twoLanes()
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      expect(openOffCard(container)!.textContent).toContain('the router accepted it; nothing decided it was wanted')
    })

    it('says so differently when the rule stopped it', () => {
      twoLanes([
        event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.10.21', dstIp: '10.0.20.10', action: 'drop', ruleLabel: '#17 default drop' }),
      ])
      seedOff([offLine({ outcome: 'drop' })])
      const { container } = render(Topography)
      flushSync()

      expect(openOffCard(container)!.textContent).toContain('the router refused it (caught by #17 default drop)')
    })

    it('names no established line count, because the register never sends one', () => {
      twoLanes()
      seedOff([offLine()])
      const { container } = render(Topography)
      flushSync()

      // The mockup's "1,214 lines" would be invented here: only today's
      // off-baseline lines are ever fetched (lib/baseline.ts).
      expect(openOffCard(container)!.textContent).not.toMatch(/established · [\d,]+ lines/)
    })

    it('asks for a reason before writing expected -- the reason is the statement', () => {
      const line = offLine()
      const calls = stubApi([line])
      twoLanes()
      seedOff([line])
      const { container } = render(Topography)
      flushSync()

      expectedFor(openOffCard(container)!, line.key)!.click()
      flushSync()

      const card = offCard(container)
      expect(card.querySelector<HTMLButtonElement>('.form .go')!.disabled).toBe(true)
      expect(calls.some((c) => c.init?.method === 'PUT')).toBe(false)
    })

    it('writes the reason through the baseline register, for the line the card names', async () => {
      const line = offLine()
      const calls = stubApi([line])
      twoLanes()
      seedOff([line])
      const { container } = render(Topography)
      flushSync()

      expectedFor(openOffCard(container)!, line.key)!.click()
      flushSync()

      typeReason(offCard(container), 'new backup job from the desktop to the nas')
      offCard(container).querySelector<HTMLButtonElement>('.form .go')!.click()

      await vi.waitFor(() => expect(calls.some((c) => c.init?.method === 'PUT')).toBe(true))
      const put = calls.find((c) => c.init?.method === 'PUT')!
      expect(put.url).toBe(`/api/baseline/${line.key}/expected`)
      expect(JSON.parse(put.init!.body as string)).toEqual({ reason: 'new backup job from the desktop to the nas' })
      // And the register is re-read, so the line reads established from
      // then on rather than the map keeping its own stale copy.
      await vi.waitFor(() => expect(calls.some((c) => c.url.startsWith('/api/baseline/off'))).toBe(true))
    })

    it('shows who it will be recorded as, the way the declare form does', () => {
      const line = offLine()
      stubApi([line])
      authState.username = 'tom'
      twoLanes()
      seedOff([line])
      const { container } = render(Topography)
      flushSync()

      expectedFor(openOffCard(container)!, line.key)!.click()
      flushSync()

      const who = container.querySelector('.card[aria-label^="Off the baseline"] .form .who')!
      expect(who.textContent).toContain('as tom')
      expect(who.textContent).toContain('this line only')
    })

    it('offers no expected action to a viewer -- the affordance is absent, not disabled', () => {
      authState.role = 'viewer'
      twoLanes()
      seedOff([offLine(), offLine({ port: 22 })])
      const { container } = render(Topography)
      flushSync()

      const card = openOffCard(container)!
      expect(perLineActs(card)).toHaveLength(0)
      expect(bulkAct(card)).toBeNull()
      expect(act(card, 'expected ▸')).toBeUndefined()
    })

    // Per-line is the default (owner, 2026-09-07). The screen is a
    // sieve: waving a whole rib through on one click can retire
    // something nobody looked at, so one click marks one line and
    // marking several is only ever reachable from a control that says
    // how many it covers.
    //
    // jsdom lays nothing out, so nothing below asserts a pixel. What is
    // asserted is what the card decided: which keys were written, which
    // lines are still listed, and whether the rib is still drawn lit.
    describe('per-line by default, the lot only on purpose (#1016)', () => {
      /** Three lines on the one rib, so "one of them" is a real claim. */
      const three = () => [offLine(), offLine({ port: 22, srcIp: '10.0.10.22' }), offLine({ port: 445, outcome: 'drop' })]

      /** Ribs only. The city draws its own roads into the same document
       * and marks each with data-road; an unscoped count here would be
       * answering for both surfaces at once. */
      const litRibs = (container: HTMLElement) => container.querySelectorAll('.redge.offbase').length

      it('offers one expected ▸ per listed line, not one for the whole rib', () => {
        const lines = three()
        stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        const card = openOffCard(container)!
        expect(perLineActs(card).map((b) => b.dataset.expectedOne)).toEqual(lines.map((l) => l.key))
        // And the card-level action is gone: the old one reason across
        // every line is exactly what this replaced.
        expect(act(card, 'expected ▸')).toBeUndefined()
      })

      it('marks only the line whose own action was clicked', async () => {
        const lines = three()
        const calls = stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        expectedFor(openOffCard(container)!, lines[1].key)!.click()
        flushSync()
        typeReason(offCard(container), 'the new monitoring agent')
        offCard(container).querySelector<HTMLButtonElement>('.form .go')!.click()

        await vi.waitFor(() => expect(written(calls)).toEqual([lines[1].key]))
      })

      it('leaves the other lines bright, and the rib bright, when one of three is spoken for', async () => {
        const lines = three()
        const calls = stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()
        expect(litRibs(container)).toBe(1)

        expectedFor(openOffCard(container)!, lines[0].key)!.click()
        flushSync()
        typeReason(offCard(container), 'the new backup job')
        offCard(container).querySelector<HTMLButtonElement>('.form .go')!.click()

        // The register drops the line that was spoken for and keeps the
        // rest, which is what the real endpoint does.
        await vi.waitFor(() => expect(baselineState.off.lines.map((l) => l.key)).toEqual([lines[1].key, lines[2].key]))
        flushSync()

        // The two nobody has answered for are still listed, still
        // offering their own action, and the rib is still lit.
        const card = offCard(container)
        expect(perLineActs(card).map((b) => b.dataset.expectedOne)).toEqual([lines[1].key, lines[2].key])
        expect(litRibs(container)).toBe(1)
        expect(card.textContent).toContain('2 off the baseline today')
      })

      it('lets the rib go dim only once its last off-baseline line is spoken for', async () => {
        const lines = [offLine()]
        const calls = stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        expectedFor(openOffCard(container)!, lines[0].key)!.click()
        flushSync()
        typeReason(offCard(container), 'the new backup job')
        offCard(container).querySelector<HTMLButtonElement>('.form .go')!.click()

        await vi.waitFor(() => expect(written(calls)).toEqual([lines[0].key]))
        await vi.waitFor(() => expect(baselineState.off.lines).toEqual([]))
        flushSync()
        expect(litRibs(container)).toBe(0)
        // Nothing was removed from the map: the rib is still drawn, it
        // has just stopped being lit.
        expect(container.querySelectorAll('.redge').length).toBe(2)
      })

      it('offers the lot behind a separate control that says how many it will mark', () => {
        const lines = three()
        stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        const bulk = bulkAct(openOffCard(container)!)!
        expect(bulk.textContent!.trim()).toBe('mark all 3 expected ▸')
        // It cannot be mistaken for a single line's action: it is not one
        // of them, and it does not read like one.
        expect(bulk.dataset.expectedOne).toBeUndefined()
        expect(bulk.textContent!.trim()).not.toBe('expected ▸')
      })

      it('offers no bulk control when there is only one line -- there is no lot to accept', () => {
        const lines = [offLine()]
        stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        const card = openOffCard(container)!
        expect(perLineActs(card)).toHaveLength(1)
        expect(bulkAct(card)).toBeNull()
      })

      it('writes the same reason against every line the bulk control covers', async () => {
        const lines = three()
        const calls = stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        bulkAct(openOffCard(container)!)!.click()
        flushSync()
        typeReason(offCard(container), 'the whole rib is the new replication link')
        offCard(container).querySelector<HTMLButtonElement>('.form .go')!.click()

        await vi.waitFor(() => expect(written(calls)).toEqual(lines.map((l) => l.key)))
        for (const c of calls.filter((x) => x.init?.method === 'PUT')) {
          expect(JSON.parse(c.init!.body as string)).toEqual({ reason: 'the whole rib is the new replication link' })
        }
        await vi.waitFor(() => expect(baselineState.off.lines).toEqual([]))
        flushSync()
        expect(litRibs(container)).toBe(0)
      })

      it('says which it is: this line only, or all of them with the count', () => {
        const lines = three()
        stubApi(lines)
        authState.username = 'tom'
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        expectedFor(openOffCard(container)!, lines[0].key)!.click()
        flushSync()
        expect(offCard(container).querySelector('.form .who')!.textContent).toBe('as tom · this line only')
        expect(offCard(container).querySelector<HTMLInputElement>('.form input')!.placeholder).toBe('why this line is meant to be here…')

        offCard(container).querySelector<HTMLButtonElement>('.form .no')!.click()
        flushSync()
        bulkAct(offCard(container))!.click()
        flushSync()
        expect(offCard(container).querySelector('.form .who')!.textContent).toBe('as tom · all 3 of these lines')
        expect(offCard(container).querySelector<HTMLInputElement>('.form input')!.placeholder).toBe('why these 3 lines are meant to be here…')
      })

      it('opens one form at a time, under the line it belongs to', () => {
        const lines = three()
        stubApi(lines)
        twoLanes()
        seedOff(lines)
        const { container } = render(Topography)
        flushSync()

        expectedFor(openOffCard(container)!, lines[1].key)!.click()
        flushSync()

        const card = offCard(container)
        expect(card.querySelectorAll('.form')).toHaveLength(1)
        // The other two still offer their own action; the one being
        // answered has given its row over to the form.
        expect(perLineActs(card).map((b) => b.dataset.expectedOne)).toEqual([lines[0].key, lines[2].key])
        expect(card.querySelector('tr.formrow .form')).not.toBeNull()
      })
    })

    it('opens no card on an established rib -- a rib on the pattern has nothing to say', () => {
      twoLanes()
      const { container } = render(Topography)
      flushSync()

      container.querySelector('.redge.established')!.parentElement!.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.card[aria-label^="Off the baseline"]')).toBeNull()
    })

    // #1030, again, for the off-baseline card. The boundary card was
    // given its own line to keep off; this one was not, so it kept clear
    // of the two zone plates its title names and then came down on the
    // rib running between them -- the one thing it is about.
    //
    // The same honest limit as the #1028 test above: jsdom lays nothing
    // out, so this cannot say where the rib really is, and a test that
    // invents coordinates and then checks its own arithmetic proves
    // nothing. What it can settle is *which* rectangles the card is told
    // to keep off. So the rib is given a hand-built rectangle -- a band
    // through the leader's own anchor, which is where the rib is by
    // construction -- handed over at the one boundary the component
    // reads the browser through, and the assertion is that the card
    // clears it. `scripts/live-topography-card-placement.mjs` is still
    // the gate that reads real rectangles out of a real browser.
    it('keeps off the rib it is describing, not only the two plates its title names (#1030)', () => {
      resizeWatchers.clear()
      vi.stubGlobal('ResizeObserver', FakeResizeObserver)
      try {
        twoLanes()
        seedOff([offLine()])
        const { container } = render(Topography)
        flushSync()

        // Down onto the 2D map. Every test starts at the city, where the
        // stage carries `hidden` and nothing on it is drawn at all.
        const range = container.querySelector<HTMLInputElement>('.alt-range')!
        range.value = '1' // "services"
        range.dispatchEvent(new Event('input', { bubbles: true }))
        flushSync()

        // The map's own 1400x720 rendered at 1400x720: user units and
        // container pixels agree, so every number below is readable.
        const host = container.querySelector('.topo') as HTMLElement
        const svg = [...container.querySelectorAll('svg')].find((s) => s.getAttribute('viewBox') === '0 0 1400 720') as SVGSVGElement
        const box = { left: 0, top: 0, width: 1400, height: 720, right: 1400, bottom: 720, x: 0, y: 0 }
        host.getBoundingClientRect = () => box as DOMRect
        svg.getBoundingClientRect = () => box as DOMRect

        type Box = { x: number; y: number; w: number; h: number }
        const hits = (a: Box, b: Box) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
        const stub = (el: Element, b: Box) => {
          el.getBoundingClientRect = () => ({ left: b.x, top: b.y, width: b.w, height: b.h, right: b.x + b.w, bottom: b.y + b.h, x: b.x, y: b.y }) as DOMRect
        }

        const card = openOffCard(container)!
        expect(card, 'no card opened on the lit rib').not.toBeNull()
        expect(card.classList.contains('placed'), 'the card never got a measured position').toBe(true)
        card.querySelector<HTMLButtonElement>('.pin')!.click()
        flushSync()

        // Where the leader starts is where the rib is: the placement's
        // own `from` point, drawn as the accent dot. The band is that
        // point given some length and `LINE_GIRTH`'s body, which is the
        // shape `pathBoxes` gives a rib that is thin in one axis.
        // The off card's own leader, not one of the other two cards'.
        const dot = (container.querySelector('.off-card')!.previousElementSibling as Element).querySelector('circle') as SVGCircleElement
        expect(dot, 'the card drew no leader to read the anchor off').not.toBeNull()
        const ax = Number(dot.getAttribute('cx'))
        const ay = Number(dot.getAttribute('cy'))
        const rib: Box = { x: ax - 300, y: ay - 4, w: 600, h: 8 }
        stub(container.querySelector('g.edge-g.on path.redge') as Element, rib)

        // Growing the card is what makes the placement run again.
        const open = offCard(container)
        Object.defineProperty(open, 'offsetWidth', { value: 288, configurable: true })
        Object.defineProperty(open, 'offsetHeight', { value: 340, configurable: true })
        reportResize(open)
        flushSync()

        const placed = offCard(container)
        const drawn: Box = { x: parseFloat(placed.style.left), y: parseFloat(placed.style.top), w: 288, h: 340 }
        expect(hits(drawn, rib), 'the card came down on the rib it is describing').toBe(false)

        // And it is still a card on the stage, not one shoved off it.
        expect(drawn.x).toBeGreaterThanOrEqual(0)
        expect(drawn.y).toBeGreaterThanOrEqual(0)
        expect(drawn.x + drawn.w).toBeLessThanOrEqual(1400)
        expect(drawn.y + drawn.h).toBeLessThanOrEqual(720)
      } finally {
        vi.unstubAllGlobals()
      }
    })
  })
})

// The reach on the 2D map, rebuilt to round 49 (#1016) -- round 49's own
// `flat-reach` scene: nothing written on a strand, the line card
// carrying what used to be printed there, and the same brightness rule
// the ribs already follow.
//
// jsdom returns zeros from getBoundingClientRect and lays nothing out,
// so nothing here asserts a pixel -- the placement is lib/cardAnchor's
// own, tested there against real numbers. What these assert is the
// decisions: which strand is drawn which way, what the card says, what
// its actions do, and where Esc lands. Every count is scoped to
// `.membrane-layer`, because the ribs and the city draw their own
// `.flow` and `.nb-ring` into the same document.
describe('the reach, drawn to round 49 (#1016)', () => {
  let nextReachKey = 1

  function reachOffLine(over: Partial<OffBaselineLine> = {}): OffBaselineLine {
    return {
      key: `reach${nextReachKey++}`,
      srcIp: '10.0.10.21',
      dstIp: '10.0.20.10',
      port: 5001,
      proto: 'tcp',
      count: 40,
      firstSeenToday: Date.parse('2026-09-07T21:26:00Z'),
      outcome: 'accept',
      ...over,
    }
  }

  function seedOff(lines: OffBaselineLine[]) {
    baselineState.off = { config: { days: 3, of: 14 }, generatedAt: Date.now(), count: lines.length, lines }
  }

  /** tom-desktop in the LAN, talking to the Servers lane -- round 49's
   * own data story, cut to the two lanes these assertions need. */
  function standOnDesktop(extra: ClientEvent[] = []) {
    zonesState.pushed = [
      { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'bridge2', comment: 'Servers' },
    ]
    appState.events = [
      event({
        inInterface: 'bridge1',
        outInterface: 'bridge2',
        srcIp: '10.0.10.21',
        srcHostName: 'tom-desktop',
        dstIp: '10.0.20.10',
        dstHostName: 'nas',
        dstPort: 445,
        protocol: 'tcp',
        action: 'accept',
      }),
      ...extra,
    ]
    const { container } = render(Topography)
    flushSync()
    container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    return container
  }

  /** Open a strand's card the way a reader does. */
  function hoverStrand(container: HTMLElement, i = 0) {
    const g = [...container.querySelectorAll('.membrane-layer .strand-g')][i]
    g.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    flushSync()
    return container.querySelector<HTMLElement>('.line-card')
  }

  const rowFor = (card: HTMLElement, port: number) => card.querySelector(`tr[data-line-port="${port}"]`)

  describe('nothing on the strand, everything in the card', () => {
    it('opens a line card naming both ends when a strand is pointed at', () => {
      const container = standOnDesktop()
      expect(container.querySelector('.line-card')).toBeNull() // nothing until pointed at

      const card = hoverStrand(container)
      expect(card).not.toBeNull()
      expect(card!.textContent).toContain('tom-desktop')
      expect(card!.textContent).toContain('Servers')
    })

    // A hover test in jsdom proves the handler, not that a pointer can
    // reach the element -- jsdom hit-tests nothing. `.membrane-layer` is
    // `pointer-events: none` so that clicking off it surfaces, and every
    // shape in it that can be pointed at opts back in. The strand used to
    // opt in through the `.strand-door` pill that sat on it; round 49
    // moved the interaction onto the whole strand and deleted the pill,
    // and the opt-in went with it, so in a real browser no strand could be
    // hovered or clicked at all -- 0 hits over every pixel of a refused
    // strand's box, measured on a live instance. The stylesheet is the
    // only place that fact lives, so the stylesheet is what is asserted,
    // the same way the lens pills' inks are.
    it('lets a pointer reach a strand at all, which pointer-events: none on its layer does not (#1016)', () => {
      const rule = componentSource.slice(componentSource.indexOf('\n  .strand-g {'))
      expect(rule.slice(0, rule.indexOf('}'))).toContain('pointer-events: auto')
    })

    it('reads the port, proto, accepted and dropped table out of reachLineSummary', () => {
      // One port accepted and another dropped on the same pair. reachFor
      // splits those into two strands; the card is about the line, so
      // both have to land in one table.
      const container = standOnDesktop([
        event({
          inInterface: 'bridge1',
          outInterface: 'bridge2',
          srcIp: '10.0.10.21',
          dstIp: '10.0.20.10',
          dstPort: 22,
          protocol: 'tcp',
          action: 'drop',
          ruleLabel: '#17 default drop',
        }),
      ])
      const card = hoverStrand(container)!

      expect(rowFor(card, 445)!.textContent).toContain('tcp')
      expect(rowFor(card, 445)!.querySelector('td.ok')!.textContent).toContain('1')
      // Accepted and dropped are different columns, not one signed number.
      expect(rowFor(card, 22)!.querySelector('td.al')!.textContent).toContain('1')
      expect(rowFor(card, 22)!.querySelector('td.ok')).toBeNull()
    })

    it('states the totals and the tcp-versus-udp split', () => {
      const container = standOnDesktop([
        event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.10.21', dstIp: '10.0.20.10', dstPort: 53, protocol: 'udp', action: 'accept' }),
      ])
      const card = hoverStrand(container)!

      const totals = card.querySelector('[data-line-totals]')!.textContent!.replace(/\s+/g, ' ')
      expect(totals).toContain('tcp 1')
      expect(totals).toContain('udp 1')
      expect(totals).toContain('other 0')
      // The same three numbers as the picture beside them.
      expect(card.querySelectorAll('.protobar i').length).toBe(3)
    })

    it("names the rule that refused the line, in round 49's own wording", () => {
      const container = standOnDesktop([
        event({
          inInterface: 'bridge1',
          outInterface: 'bridge2',
          srcIp: '10.0.10.21',
          dstIp: '10.0.20.10',
          dstPort: 22,
          protocol: 'tcp',
          action: 'drop',
          ruleLabel: '#17 default drop',
        }),
      ])
      const said = hoverStrand(container)!.querySelector('[data-refused-by]')!.textContent!.replace(/\s+/g, ' ').trim()
      expect(said).toBe(':22 refused by #17 default drop')
    })

    it('never names a rule the drop did not carry', () => {
      const container = standOnDesktop([
        event({ inInterface: 'bridge1', outInterface: 'bridge2', srcIp: '10.0.10.21', dstIp: '10.0.20.10', dstPort: 22, action: 'drop', ruleLabel: '' }),
      ])
      const card = hoverStrand(container)!
      expect(card.querySelector('[data-refused-by]')).toBeNull()
      expect(card.textContent).toContain('the drop named no rule')
    })
  })

  describe('the composer stays a draft', () => {
    it('offers `draft the rule ▸` only on a refused line', () => {
      expect(hoverStrand(standOnDesktop())!.querySelector('[data-draft-rule]')).toBeNull()
    })

    it("opens the composer from the refused line's card, and drafts rather than runs", () => {
      const container = standOnDesktop([
        event({
          inInterface: 'bridge1',
          outInterface: 'bridge2',
          srcIp: '10.0.10.21',
          dstIp: '10.0.20.10',
          dstPort: 445,
          protocol: 'tcp',
          action: 'drop',
          ruleLabel: '#17 default drop',
        }),
      ])
      // The refused strand is the one drawn with the ✕ on it.
      const refusedAt = [...container.querySelectorAll('.membrane-layer .strand-g')].findIndex((g) => g.querySelector('.strand-x') !== null)
      expect(refusedAt).toBeGreaterThanOrEqual(0)

      const draft = hoverStrand(container, refusedAt)!.querySelector<HTMLButtonElement>('[data-draft-rule]')!
      expect(draft.textContent).toContain('draft the rule')
      draft.click()
      flushSync()

      const composer = container.querySelector('.composer')
      expect(composer).not.toBeNull()
      // The same invariant the strand pill's door carried: a printed
      // line for the operator to paste, and nothing sent to the router.
      expect(composer!.textContent).toContain('mikroview never touches the router')
    })
  })

  describe('every card pins', () => {
    it('keeps the card up when the pointer leaves, once pinned', async () => {
      const container = standOnDesktop()
      const card = hoverStrand(container)!
      card.querySelector<HTMLButtonElement>('.pin')!.click()
      flushSync()

      container.querySelector('.membrane-layer .strand-g')!.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
      card.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
      // Well past the grace period an unpinned card would close in.
      await new Promise((r) => setTimeout(r, 260))
      flushSync()

      expect(container.querySelector('.line-card.pinned')).not.toBeNull()
    })
  })

  describe('brightness is the baseline, on strands too', () => {
    /** One strand to Servers (off-baseline below) and one inside the LAN
     * (never off-baseline), so "one bright, one dim" is a claim the
     * drawing can settle. */
    const twoStrands = () => [
      event({ inInterface: 'bridge1', outInterface: 'bridge1', srcIp: '10.0.10.21', dstIp: '10.0.10.34', dstPort: 443, protocol: 'tcp', action: 'accept' }),
    ]

    it('draws an established strand thin and dim, with no flow and no ring', () => {
      seedOff([])
      const container = standOnDesktop()
      const layer = container.querySelector('.membrane-layer')!

      expect(layer.querySelector('.strand.established')).not.toBeNull()
      expect(layer.querySelector('.strand.offbase')).toBeNull()
      expect(layer.querySelector('.flow')).toBeNull()
      expect(layer.querySelector('.nb-ring')).toBeNull()
    })

    it('brings the off-baseline strand forward, with flow and a ring, and removes nothing', () => {
      seedOff([reachOffLine()])
      const layer = standOnDesktop(twoStrands()).querySelector('.membrane-layer')!

      expect(layer.querySelectorAll('.strand.offbase').length).toBe(1)
      expect(layer.querySelectorAll('.flow').length).toBe(1)
      expect(layer.querySelectorAll('.nb-ring').length).toBe(1)
      // Nothing was removed, only dimmed.
      expect(layer.querySelectorAll('.strand.established').length).toBeGreaterThan(0)
    })

    it('draws the established strand thinner than the off-baseline one', () => {
      seedOff([reachOffLine()])
      const container = standOnDesktop(twoStrands())
      const width = (sel: string) => {
        const style = container.querySelector(`.membrane-layer ${sel}`)!.getAttribute('style') ?? ''
        return Number(/stroke-width:\s*([\d.]+)px/.exec(style)![1])
      }
      expect(width('.strand.established')).toBeLessThan(width('.strand.offbase'))
    })

    it("is a strand's own question, not the whole host's: one bright line leaves its neighbour dim", () => {
      // Off-baseline toward Servers only. A host-wide roll-up would
      // light the LAN-internal strand too; a strand-level one must not.
      seedOff([reachOffLine()])
      const layer = standOnDesktop(twoStrands()).querySelector('.membrane-layer')!
      expect(layer.querySelectorAll('.strand.offbase').length).toBe(1)
      expect(layer.querySelectorAll('.strand.established').length).toBe(1)
    })

    it('draws a refused strand in alarm ink, ending in a ✕, and never as off-baseline', () => {
      const layer = standOnDesktop([
        event({
          inInterface: 'bridge1',
          outInterface: 'bridge2',
          srcIp: '10.0.10.21',
          dstIp: '10.0.20.10',
          dstPort: 22,
          protocol: 'tcp',
          action: 'drop',
          ruleLabel: '#17 default drop',
        }),
      ]).querySelector('.membrane-layer')!

      const refused = layer.querySelector('.strand.refused')!
      expect(refused.getAttribute('stroke')).toBe('var(--alarm)')
      // Colour is the verdict; brightness is the baseline. Refused is red
      // whatever the baseline says about it.
      expect(refused.classList.contains('offbase')).toBe(false)
      expect(refused.classList.contains('established')).toBe(false)
      expect(layer.querySelectorAll('.strand-x').length).toBe(1)
    })
  })

  describe("the crumb, in the city's words", () => {
    it('reads name · ip · reaches N · reached by N · refused N · Esc surfaces ▸', () => {
      const container = standOnDesktop([
        event({
          inInterface: 'bridge1',
          outInterface: 'bridge2',
          srcIp: '10.0.10.21',
          dstIp: '10.0.20.10',
          dstPort: 22,
          protocol: 'tcp',
          action: 'drop',
          ruleLabel: '#17 default drop',
        }),
      ])
      const crumb = container.querySelector('.crumb .path')!.textContent!.replace(/\s+/g, ' ').trim()
      expect(crumb).toContain('tom-desktop')
      expect(crumb).toContain('10.0.10.21')
      expect(crumb).toContain('reaches 1')
      expect(crumb).toContain('reached by 0')
      expect(crumb).toContain('refused 1')
      expect(crumb).toContain('Esc surfaces')
      // The old trail is gone, not merely restyled.
      expect(crumb).not.toContain('Network')
    })
  })

  describe('clicking anything opens its reach, and Esc surfaces where you were', () => {
    function oneLane() {
      zonesState.pushed = [{ address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' }]
      appState.events = [event({ inInterface: 'bridge1', srcIp: '10.0.10.21', srcHostName: 'tom-desktop' })]
      const { container } = render(Topography)
      flushSync()
      return container
    }

    function jumpTo(container: HTMLElement, stop: string) {
      const range = container.querySelector<HTMLInputElement>('.alt-range')!
      range.value = stop
      range.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
    }

    it("opens the router's own reach from the ground plan", () => {
      const container = oneLane()
      jumpTo(container, '2') // the zones stop, where the ground plan is drawn

      const router = container.querySelector<SVGGElement>('[data-router]')
      expect(router).not.toBeNull()
      router!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()

      expect(container.querySelector('.membrane-layer')).not.toBeNull()
      expect(appState.view).toBe('topography') // the reach, never the stream
    })

    it('surfaces to the stop it was opened from, not to the default one', () => {
      const container = oneLane()
      jumpTo(container, '1') // the services stop

      container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()
      expect(container.querySelector('.membrane-layer')).not.toBeNull()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      flushSync()

      expect(container.querySelector('.membrane-layer')).toBeNull()
      // Back where it was left: the reach is a mode of this scene, so
      // the stop it was opened from is the stop it surfaces to.
      expect(container.querySelector<HTMLInputElement>('.alt-range')!.value).toBe('1')
    })

    it("opens a zone's own reach from its lane plate (#1016)", () => {
      // Round 49 widened the click to anything, and #1016 gave a zone a
      // subject of its own: the plate is no longer a shortcut to the
      // stream, it stands on the zone the way a host dot stands on a
      // host.
      const container = standOnDesktop()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      flushSync()

      const plate = [...container.querySelectorAll<SVGGElement>('g.zone')].find((g) => g.getAttribute('data-zone') === 'bridge1')!
      plate.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()

      expect(container.querySelector('.membrane-layer')).not.toBeNull()
      expect(appState.view).toBe('topography') // the reach, never the stream
      const crumb = container.querySelector('.crumb .path')!.textContent!.replace(/\s+/g, ' ').trim()
      expect(crumb).toContain('LAN')
      // The zone's own side is bridge1, so the one accepted crossing to
      // Servers is a pathway it reaches, counted the same way a host's is.
      expect(crumb).toContain('reaches 1')
      expect(crumb).toContain('reached by 0')
    })

    it("opens a rib's own reach from the line between two zones (#1016)", () => {
      const container = standOnDesktop()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      flushSync()

      const rib = container.querySelector<SVGGElement>('.edge-g')!
      expect(rib).not.toBeNull()
      rib.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      flushSync()

      expect(container.querySelector('.membrane-layer')).not.toBeNull()
      expect(appState.view).toBe('topography')
      // A rib names the pair, and has no address of its own to print.
      const crumb = container.querySelector('.crumb .path')!.textContent!.replace(/\s+/g, ' ').trim()
      expect(crumb).toContain('LAN → Servers')
      expect(crumb).toContain('Esc surfaces')
      expect(container.querySelector('.crumb .ip')).toBeNull()
    })

    it('walks out of the card first, then the reach', () => {
      const container = standOnDesktop()
      hoverStrand(container)
      expect(container.querySelector('.line-card')).not.toBeNull()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      flushSync()
      expect(container.querySelector('.line-card')).toBeNull()
      expect(container.querySelector('.membrane-layer')).not.toBeNull() // still standing on it

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      flushSync()
      expect(container.querySelector('.membrane-layer')).toBeNull()
    })
  })
})

// ---------------------------------------------------------------------
// #1018, round 53: the two filters on the living topology. Neither is a
// new view -- both redraw this one, and the rule they are drawn to is
// "dim to the answer, remove nothing".
// ---------------------------------------------------------------------
describe('the port filter (#1018, round 53)', () => {
  const lanes: RouterIPAddress[] = [
    { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' },
    { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'ether3', comment: 'Servers' },
    { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'ether4', comment: 'IoT' },
  ]

  // Round 49's own data story, which round 53 filters: 445/tcp accepted
  // between LAN and Servers, and IoT talking on something else.
  function seedMap() {
    zonesState.pushed = lanes
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether3', srcIp: '10.0.10.21', srcHostName: 'tom-desktop', dstIp: '10.0.20.5', dstPort: 445, protocol: 'tcp' }),
      event({ inInterface: 'bridge1', outInterface: 'ether3', srcIp: '10.0.10.34', srcHostName: 'laptop-anna', dstIp: '10.0.20.5', dstPort: 445, protocol: 'tcp' }),
      event({ inInterface: 'ether4', outInterface: 'ether3', srcIp: '10.0.30.14', srcHostName: 'cam-porch', dstIp: '10.0.20.5', dstPort: 53, protocol: 'udp' }),
    ]
  }

  /** Drives the store the way a landed fetch would, which is how every
   * other store in this file is driven. */
  function filterTo(ports: number[], answer: Partial<(typeof portFilterState)['answer']> = {}) {
    portFilterState.ports = ports
    portFilterState.proto = 'tcp'
    portFilterState.answer = {
      generatedAt: 1,
      windowSeconds: 3600,
      candidates: [],
      events: 2,
      accepts: 2,
      drops: 0,
      lines: 2,
      ribs: [{ in: 'bridge1', out: 'ether3', events: 2, accepts: 2, drops: 0 }],
      hosts: [{ ip: '10.0.10.21', name: 'tom-desktop', events: 2, accepts: 2, drops: 0 }],
      doors: [],
      ...answer,
    }
    portFilterState.answeredKey = portFilterState.key
  }

  function door(overrides: Partial<(typeof portFilterState)['doors'][number]> = {}) {
    return {
      device: 'core',
      label: '#12',
      ordinal: 12,
      action: 'accept',
      chain: 'forward',
      in: 'bridge1',
      out: 'ether3',
      dstPort: '445',
      who: 'bridge1 → ether3 accept',
      ...overrides,
    }
  }

  it('opens the pill into a bar of the same shape, with no Show button in it', () => {
    seedMap()
    portFilterState.open = true
    portFilterState.answer = {
      ...portFilterState.answer,
      candidates: [
        { port: 445, proto: 'tcp', count: 16, named: true },
        { port: 3389, proto: 'tcp', count: 0, named: true },
      ],
    }
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const bar = container.querySelector('.pill.p.edit')!
    expect(bar).not.toBeNull()
    expect([...bar.querySelectorAll('.ports .chip')].map((c) => c.textContent)).toEqual(['445', '3389'])
    expect([...bar.querySelectorAll('.seg .chip')].map((c) => c.textContent)).toEqual(['tcp', 'udp'])
    // Owner, 2026-09-08: "no show button, it loads automatically".
    expect([...bar.querySelectorAll('button')].some((b) => /show/i.test(b.textContent ?? ''))).toBe(false)
    expect(bar.querySelector('input')).not.toBeNull()
  })

  it('collapses onto the answer, with the ✕ as its own control', () => {
    seedMap()
    filterTo([445], { doors: [door(), door({ label: '#23', ordinal: 23, action: 'drop' })] })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.pill.p.on')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      '⌕ 445/tcp · 2 lines seen · 2 doors',
    )
    expect(container.querySelector('.pill-x')).not.toBeNull()
  })

  it('keeps the crossed rib in its verdict ink and greys every other one, removing none', () => {
    seedMap()
    filterTo([445])
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    // Both halves of the crossing light: one packet through the router,
    // not a claim about traffic coming back.
    expect(container.querySelectorAll('.lit-half').length).toBe(2)
    expect(container.querySelector('.lit-half.refused')).toBeNull()

    // Nothing is removed -- every rib the map drew is still drawn, thin
    // and grey, with the off-filter ones at the dim opacity.
    const off = [...container.querySelectorAll('.redge.port-off')]
    expect(off.length).toBeGreaterThan(0)
    for (const r of off) {
      expect(r.getAttribute('style')).toContain('var(--fg-muted)')
    }
    expect(off.some((r) => (r.getAttribute('style') ?? '').includes('opacity: 0.4'))).toBe(true)
  })

  it('draws a refused direction in the alarm ink and lights only the half it arrived on', () => {
    seedMap()
    filterTo([445], { ribs: [{ in: 'ether4', out: '', events: 14, accepts: 0, drops: 14 }], accepts: 0, drops: 14, hosts: [] })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const lit = [...container.querySelectorAll('.lit-half')]
    expect(lit.length).toBe(1)
    expect(lit[0].classList.contains('refused')).toBe(true)
  })

  it('lights only the hosts on the port and counts the lane against itself', () => {
    seedMap()
    filterTo([445])
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const tallies = [...container.querySelectorAll('.hosttally')].map((t) => t.textContent)
    expect(tallies).toContain('1 of 2 hosts on 445/tcp')
    // A dot off the port recedes but is never taken off the card: "1 of
    // 2" is a claim about the two hosts the card draws.
    const dimmed = [...container.querySelectorAll('.zone .hostrow .dot-off')]
    expect(dimmed.length).toBeGreaterThan(0)
    expect(container.querySelectorAll('.zone .hostrow .h-dot').length).toBeGreaterThan(dimmed.length)
  })

  // #1056, found on #1055's live capture of the city: the answer's host
  // list is the log lines' own, and an address nothing has registered
  // draws no dot. Counting it anyway put `1 of 0` under a plaque; the
  // same arithmetic sits under these cards.
  it('leaves a host it draws no dot for out of the tally, and says nothing where there is no host', () => {
    zonesState.pushed = [
      ...lanes,
      { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'ether5', comment: 'Guest' },
    ]
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether3', srcIp: '10.0.10.21', srcHostName: 'tom-desktop', dstIp: '10.0.20.5', dstPort: 445, protocol: 'tcp' }),
    ]
    // The one host on the port is inside the Servers lane's subnet and
    // has never been registered, so no card draws a dot for it.
    filterTo([445], { hosts: [{ ip: '10.0.20.99', name: '', events: 2, accepts: 2, drops: 0 }] })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const tallies = [...container.querySelectorAll('.hosttally')].map((t) => t.textContent ?? '')
    expect(tallies.some((t) => t.startsWith('0 of'))).toBe(true)
    expect(tallies.some((t) => t.startsWith('1 of'))).toBe(false)
    // A lane with no known host says nothing rather than `0 of 0`: this
    // surface draws no row at all there, so there are fewer of these
    // lines than there are cards. zoneTally refuses the same fraction
    // outright (portFilter.test.ts), which is what the city needs.
    expect(tallies.some((t) => t.startsWith('0 of 0'))).toBe(false)
    expect(container.querySelectorAll('.zone').length).toBeGreaterThan(tallies.length)
    // The rib is what says the traffic was there, and it still does.
    expect(container.querySelectorAll('.lit-half').length).toBe(2)
  })

  it('dims a lane with nothing on the port whole, and keeps it on the map', () => {
    seedMap()
    filterTo([445])
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const off = [...container.querySelectorAll('.zone.lane-off')]
    expect(off.length).toBeGreaterThan(0)
    expect(container.querySelectorAll('.zone').length).toBeGreaterThan(off.length)
  })

  // "Knowing where a door is open (even if unused) is useful
  // information" (owner, 2026-09-08). A door on a rib nobody used still
  // draws, and its rib recedes less than the rest.
  // Every rib on this map converges on the router, so two doors picked
  // at the same fraction land in the same crowded place. The chooser
  // measures clearance instead; here that has to separate them.
  it('keeps two doors on converging ribs off each other', () => {
    seedMap()
    filterTo([445], {
      doors: [door(), door({ label: '#31', ordinal: 31, action: 'drop', in: 'ether4', out: 'ether3' })],
    })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const doors = [...container.querySelectorAll('.door')]
    expect(doors.length).toBe(2)
    const at = (g: Element) => {
      const d = g.querySelector('.door-post')!.getAttribute('d') ?? ''
      const [, x, y] = /^M ([-\d.]+) ([-\d.]+)/.exec(d) ?? []
      return { x: Number(x), y: Number(y) }
    }
    const [a, b] = doors.map(at)
    expect(Math.hypot(a.x - b.x, a.y - b.y)).toBeGreaterThan(20)
  })

  it('draws a door wherever a rule names the port, on a used rib and an unused one alike', () => {
    seedMap()
    filterTo([445], {
      doors: [door(), door({ label: '#31', ordinal: 31, action: 'drop', in: 'ether4', out: 'ether3' })],
    })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const doors = [...container.querySelectorAll('.door')]
    expect(doors.length).toBe(2)
    expect(doors.map((d) => d.querySelector('.door-t')?.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      '#12 accept',
      '#31 drop',
    ])
    // Accept is a leaf that swings open, a refusal is a bar across.
    expect(doors[0].classList.contains('shut')).toBe(false)
    expect(doors[1].classList.contains('shut')).toBe(true)
    // The unused rib the drop guards recedes less than the rest.
    const halves = [...container.querySelectorAll('.redge.port-off, .cedge.port-off')]
    expect(halves.some((h) => (h.getAttribute('style') ?? '').includes('opacity: 0.6'))).toBe(true)
  })

  // The fit chip keeps its own corner; the legend goes to the left of it.
  // jsdom lays nothing out, so the real overlap is caught in
  // live-topography-port-trace.mjs against measured rects -- this pins
  // the wiring: the legend's `right` is driven by the chip's own width
  // rather than by a guess that is right at 100 % and wrong at 1000 %.
  it('places its legend clear of the fit chip rather than under it', () => {
    seedMap()
    filterTo([445], { doors: [door()] })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const legend = container.querySelector<HTMLElement>('.map-legend')!
    const chip = container.querySelector<HTMLElement>('.fitchip')!
    expect(chip).not.toBeNull()
    const right = Number(/right:\s*([\d.]+)px/.exec(legend.getAttribute('style') ?? '')?.[1])
    // The chip sits 16px in and measures 0 wide under jsdom, so the
    // legend's own inset has to clear that inset by the stated gap.
    expect(right).toBeGreaterThanOrEqual(32)
    expect(componentSource).toMatch(/fitChipW = el \? el\.getBoundingClientRect\(\)\.width : 0/)
  })

  it('says nothing was seen in one line under the map, and still draws the door', () => {
    seedMap()
    filterTo([3389], {
      events: 0,
      accepts: 0,
      drops: 0,
      lines: 0,
      ribs: [],
      hosts: [],
      doors: [door({ label: '#23', ordinal: 23, action: 'drop', in: 'ether4', out: '', who: 'ether4 → any drop' })],
    })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.note-t')?.textContent).toBe(
      'no logged traffic on 3389/tcp in the window · one rule names it — #23 ether4 → any drop, the door on the ether4 side',
    )
    expect(container.querySelectorAll('.door').length).toBe(1)
    // Not an empty state: the map is still there behind the sentence.
    expect(container.querySelectorAll('.zone').length).toBe(3)
  })

  it('swaps the legend for its own entries, and takes it away with the filter', () => {
    seedMap()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelector('.map-legend')).toBeNull()

    filterTo([445], { doors: [door()] })
    flushSync()
    const legend = container.querySelector('.map-legend')!
    expect(legend.textContent?.replace(/\s+/g, ' ')).toContain('door open')
    expect(legend.textContent?.replace(/\s+/g, ' ')).toContain('off the port')
  })

  it('goes quiet about everything the filter is not about', () => {
    seedMap()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelectorAll('.edge-badge').length).toBeGreaterThan(0)

    filterTo([445])
    flushSync()
    // Two port answers on one card is the map saying two things at once.
    expect(container.querySelectorAll('.edge-badge').length).toBe(0)
    expect(container.querySelectorAll('.svc-t').length).toBe(0)
  })

  // Three answers layered on one map would stack their crumbs on each
  // other and leave nobody able to say which of them a dim rib was dim
  // because of.
  it('gets out of the way when the operator stands on something', () => {
    seedMap()
    filterTo([445])
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelector('.lit-half')).not.toBeNull()

    container.querySelector<SVGGElement>('.hostrow .hot')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    expect(portFilterState.active).toBe(false)
    expect(container.querySelector('.lit-half')).toBeNull()
  })

  it('clears on Esc', () => {
    seedMap()
    filterTo([445])
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelector('.lit-half')).not.toBeNull()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    flushSync()
    expect(portFilterState.active).toBe(false)
    expect(container.querySelector('.lit-half')).toBeNull()
  })
})

describe('the event trace (#1018, round 53)', () => {
  const lanes: RouterIPAddress[] = [
    { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' },
    { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'ether4', comment: 'IoT' },
  ]

  function seedMap() {
    zonesState.pushed = lanes
    appState.events = [
      event({ inInterface: 'bridge1', outInterface: 'ether4', srcIp: '10.0.10.21', srcHostName: 'tom-desktop', dstIp: '10.0.30.14', dstPort: 80, protocol: 'tcp' }),
      event({ inInterface: 'ether4', outInterface: 'bridge1', srcIp: '10.0.30.14', srcHostName: 'cam-porch', dstIp: '10.0.10.21', dstPort: 445, protocol: 'tcp' }),
    ]
  }

  function traceRefused(overrides: Partial<ClientEvent> = {}, resultOverrides: Partial<TraceResponse> = {}) {
    const e = event({
      id: 7,
      action: 'drop',
      ruleLabel: '#17 default drop',
      inInterface: 'ether4',
      outInterface: undefined,
      srcIp: '10.0.30.14',
      srcHostName: 'cam-porch',
      dstIp: '10.0.10.21',
      dstHostName: 'tom-desktop',
      dstPort: 445,
      protocol: 'tcp',
      ...overrides,
    })
    mapTraceState.request = { event: 7 }
    mapTraceState.result = {
      found: true,
      verdict: 'refused',
      event: e,
      like: 13,
      srcSeen: 14,
      dstReached: 0,
      sameLine: [e],
      sameMinute: [],
      sameMinuteTotal: 0,
      ...resultOverrides,
    }
  }

  it('draws the crumb, the chip and the one lit half of a refusal', () => {
    seedMap()
    traceRefused()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const crumb = container.querySelector('.trace-crumb')!.textContent!.replace(/\s+/g, ' ')
    expect(crumb).toContain('cam-porch')
    expect(crumb).toContain('tom-desktop')
    expect(crumb).toContain('445/tcp')
    expect(crumb).toContain('refused at #17 default drop')
    // The others are said, never drawn as a union.
    expect(crumb).toContain('and 13 more like it')
    expect(crumb).toContain('Esc ▸')

    const chip = container.querySelector('.trace-chip.refused')!
    expect(chip.querySelector('.chip-verdict')?.textContent).toBe('✕ REFUSED · #17 default drop')
    expect(chip.querySelector('.chip-t')?.textContent).toContain('in: ether4 → out: —')

    const lit = [...container.querySelectorAll('.lit-half')]
    expect(lit.length).toBe(1)
    expect(lit[0].classList.contains('refused')).toBe(true)
    // A refusal ends at the router.
    expect(container.querySelector('.trace-stop')).not.toBeNull()
    expect(container.querySelector('.trace-ring')).toBeNull()
  })

  it('C1 (round 56): a forward-chain refusal lights both halves and puts the ✕ at the far gate, with no ghost', () => {
    seedMap()
    // The log now names an out-interface: the router forwarded the line
    // before the LAN boundary refused it, unlike the input-chain shape
    // traceRefused() otherwise fixtures.
    traceRefused({ outInterface: 'bridge1' })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const lit = [...container.querySelectorAll('.lit-half')]
    expect(lit.length).toBe(2)
    expect(lit.every((h) => h.classList.contains('refused'))).toBe(true)

    // The ✕ still stands (at the gate now, not the router), but there is
    // nowhere left to draw a dashed rib toward.
    expect(container.querySelector('.trace-stop')).not.toBeNull()
    expect(container.querySelector('.trace-ring')).toBeNull()
    expect(container.querySelector('.trace-ghost')).toBeNull()

    // The two grey words take the ghost's place instead.
    const note = container.querySelector('.trace-note')?.textContent?.replace(/\s+/g, ' ')
    expect(note).toContain('would have reached tom-desktop')
    expect(note).toContain('stopped at the LAN boundary')

    // The chip sits by the ✕: its leader now runs to the gate, not to
    // the router's own fixed spot.
    const leader = container.querySelector('.trace-chip .trace-leader')?.getAttribute('d')
    expect(leader).not.toBe('M556 254 L 572 258')
  })

  it('dashes the rib the refused line would have taken, and says so beside it', () => {
    seedMap()
    traceRefused()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.trace-ghost')).not.toBeNull()
    expect(container.querySelector('.trace-note')?.textContent).toBe(
      'would have reached tom-desktop · never left the router',
    )
  })

  it('gives the traced ends their own one-line tallies', () => {
    seedMap()
    traceRefused()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const tallies = [...container.querySelectorAll('.hosttally')].map((t) => t.textContent)
    expect(tallies).toContain('cam-porch · 14× in the window')
    // The far end was never reached, and the number says so rather than
    // the drawing implying it.
    expect(tallies).toContain('tom-desktop · never reached')
  })

  it('lights both halves of an accepted line and rings where it arrived', () => {
    seedMap()
    mapTraceState.request = { event: 8 }
    mapTraceState.result = {
      found: true,
      verdict: 'accepted',
      event: event({
        id: 8,
        action: 'accept',
        ruleLabel: '#8 lan → iot',
        inInterface: 'bridge1',
        outInterface: 'ether4',
        srcIp: '10.0.10.21',
        srcHostName: 'tom-desktop',
        dstIp: '10.0.30.14',
        dstHostName: 'cam-porch',
        dstPort: 80,
        protocol: 'tcp',
      }),
      like: 0,
      srcSeen: 1,
      dstReached: 1,
      sameLine: [],
      sameMinute: [],
      sameMinuteTotal: 0,
    }
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelectorAll('.lit-half').length).toBe(2)
    expect(container.querySelector('.lit-half.refused')).toBeNull()
    expect(container.querySelector('.trace-stop')).toBeNull()
    expect(container.querySelector('.trace-chip')?.classList.contains('refused')).toBe(false)
    // No ghost on an accepted line: it reached its far end.
    expect(container.querySelector('.trace-ghost')).toBeNull()
  })

  // Found while building this chip, which uses the same class: `.chip-t`
  // carried no fill at all, so SVG's own black was drawing the
  // unplanned callout's second line on a black map. Asserted against
  // the stylesheet rather than a computed colour, the same way this
  // file already pins a raw CSS value it cares about.
  it('gives the class both chips write their second line in a fill, so it is not black on black', () => {
    expect(componentSource).toMatch(/\.chip-t \{[^}]*fill: var\(--fg-muted\);/)
  })

  it('says an honest miss in words rather than drawing a path that went nowhere', () => {
    seedMap()
    mapTraceState.request = { in: 'ether4', port: 3389 }
    mapTraceState.result = { found: false, like: 0, srcSeen: 0, dstReached: 0, sameLine: [], sameMinute: [], sameMinuteTotal: 0 }
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.trace-crumb')?.textContent).toContain('nothing in the window matches that line')
    expect(container.querySelector('.lit-half')).toBeNull()
    expect(container.querySelector('.trace-chip')).toBeNull()
  })

  it('opens from a stream row through the same one-shot slot the descend uses', () => {
    seedMap()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    topologyNavState.requestTrace({ event: 7 })
    flushSync()
    // Read and cleared on arrival, so a later visit does not reopen it.
    expect(topologyNavState.pendingTrace).toBeNull()
    expect(mapTraceState.request).toEqual({ event: 7 })
  })

  it('clears on Esc', () => {
    seedMap()
    traceRefused()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelector('.trace-crumb')).not.toBeNull()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    flushSync()
    expect(mapTraceState.active).toBe(false)
    expect(container.querySelector('.trace-crumb')).toBeNull()
  })

  it('Esc closes the list first (#1050, A1), a second Esc clears the trace underneath it', () => {
    seedMap()
    traceRefused()
    mapTraceState.listOpen = true
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)
    expect(container.querySelector('.picker')).not.toBeNull()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    flushSync()
    expect(mapTraceState.active).toBe(true)
    expect(mapTraceState.listOpen).toBe(false)
    expect(container.querySelector('.picker')).toBeNull()
    expect(container.querySelector('.trace-crumb')).not.toBeNull()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    flushSync()
    expect(mapTraceState.active).toBe(false)
    expect(container.querySelector('.trace-crumb')).toBeNull()
  })
})

describe("the trace's own list (#1050, round 56, A1/B1)", () => {
  const lanes: RouterIPAddress[] = [
    { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge1', comment: 'LAN' },
    { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'ether4', comment: 'IoT' },
  ]

  function seedMap() {
    zonesState.pushed = lanes
  }

  function traceRefusedWithList() {
    const traced = event({
      id: 7,
      action: 'drop',
      ruleLabel: '#17 default drop',
      inInterface: 'ether4',
      outInterface: 'bridge1',
      srcIp: '10.0.30.14',
      srcHostName: 'cam-porch',
      dstIp: '10.0.10.21',
      dstHostName: 'tom-desktop',
      dstPort: 445,
      protocol: 'tcp',
      time: '2026-09-09T22:04:31Z',
    })
    const older = event({
      id: 6,
      action: 'drop',
      ruleLabel: '#17 default drop',
      inInterface: 'ether4',
      outInterface: 'bridge1',
      srcIp: '10.0.30.14',
      srcHostName: 'cam-porch',
      dstIp: '10.0.10.21',
      dstHostName: 'tom-desktop',
      dstPort: 445,
      protocol: 'tcp',
      time: '2026-09-09T22:04:12Z',
    })
    const minuteMate = event({
      id: 9,
      action: 'accept',
      ruleLabel: '#26 accept',
      inInterface: 'ether4',
      outInterface: 'bridge1',
      srcIp: '10.0.30.14',
      srcHostName: 'cam-porch',
      dstIp: '10.0.20.5',
      dstHostName: 'nas',
      dstPort: 554,
      protocol: 'tcp',
      time: '2026-09-09T22:04:47Z',
    })
    mapTraceState.request = { event: 7 }
    mapTraceState.result = {
      found: true,
      verdict: 'refused',
      event: traced,
      like: 13,
      srcSeen: 14,
      dstReached: 0,
      sameLine: [traced, older],
      sameMinute: [minuteMate],
      sameMinuteTotal: 4,
    }
  }

  it('opens the list on the toggle, with SAME LINE and SAME MINUTE columns, the traced row marked', () => {
    seedMap()
    traceRefusedWithList()
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    expect(container.querySelector('.picker')).toBeNull()
    const toggle = [...container.querySelectorAll('.crumb-link')].find((b) => b.textContent?.includes('more like it'))!
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()

    const picker = container.querySelector('.picker')!
    expect(picker).not.toBeNull()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')

    const cols = [...picker.querySelectorAll('.col')]
    expect(cols.length).toBe(2)
    expect(cols[0].querySelector('h5')?.textContent).toContain('SAME LINE')
    expect(cols[1].querySelector('h5')?.textContent).toContain('SAME MINUTE')

    const lineRows = [...cols[0].querySelectorAll('.row')]
    expect(lineRows.length).toBe(2)
    expect(lineRows.some((r) => r.classList.contains('on') && r.textContent?.includes('TRACED'))).toBe(true)

    const minuteRows = [...cols[1].querySelectorAll('.row')]
    expect(minuteRows.length).toBe(1)
    expect(minuteRows[0].textContent).toContain('nas')
  })

  it('picking a row re-opens the trace on that event, and keeps the list open', async () => {
    seedMap()
    traceRefusedWithList()
    mapTraceState.listOpen = true
    vi.mocked(fetchTrace).mockResolvedValue({
      found: true,
      verdict: 'refused',
      event: event({ id: 6, action: 'drop', srcIp: '10.0.30.14', dstIp: '10.0.10.21', dstPort: 445, protocol: 'tcp' }),
      like: 13,
      srcSeen: 14,
      dstReached: 0,
      sameLine: [],
      sameMinute: [],
      sameMinuteTotal: 0,
    })
    const { container } = render(Topography)
    flushSync()
    showTheMap(container)

    const picker = container.querySelector('.picker')!
    const lineRows = [...picker.querySelectorAll('.col')[0].querySelectorAll('.row')]
    const olderRow = lineRows.find((r) => !r.classList.contains('on'))!
    olderRow.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    await Promise.resolve()
    await Promise.resolve()

    expect(mapTraceState.request).toEqual({ event: 6 })
    expect(mapTraceState.listOpen).toBe(true)
  })

  it('B1: the unplanned callout opens the list as soon as the trace answer lands', async () => {
    zonesState.pushed = lanes
    policyState.anyPushed = true
    policyState.edges = []
    appState.events = Array.from({ length: 14 }, () =>
      event({ action: 'drop', ruleLabel: 'default drop', inInterface: 'ether4', outInterface: 'bridge1', srcIp: '10.0.30.14', dstIp: '10.0.10.21', dstPort: 445, protocol: 'tcp' }),
    )
    vi.mocked(fetchTrace).mockResolvedValue({
      found: true,
      verdict: 'refused',
      event: event({ id: 99, action: 'drop', srcIp: '10.0.30.14', dstIp: '10.0.10.21', dstPort: 445, protocol: 'tcp' }),
      like: 13,
      srcSeen: 14,
      dstReached: 0,
      sameLine: [],
      sameMinute: [],
      sameMinuteTotal: 0,
    })
    const { container } = render(Topography)
    flushSync()

    const traceBtn = container.querySelector('.uc-trace')!
    expect(traceBtn).not.toBeNull()
    traceBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    flushSync()
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(mapTraceState.listOpen).toBe(true)
    expect(container.querySelector('.picker')).not.toBeNull()
  })
})
