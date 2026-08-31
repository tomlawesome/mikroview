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

describe('the altitude slider (#648, named ends #682)', () => {
  it('renders one range input, four stop symbols, and its two named ends', () => {
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

    // Ratified round-29: the two extremes are named, the middle stops
    // stay tick-only symbols -- never a full text label per stop.
    const ends = [...container.querySelectorAll('.alt-end')].map((n) => n.textContent?.trim())
    expect(ends).toEqual(['clients', 'survey'])
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

describe('degrading honestly without a pushed address table (#682, data gap #687)', () => {
  it('never invents a subnet or a coverage verdict, and draws no explanatory note (round 30 draws none anywhere -- unmounted behind DEGRADED_NOTE_ENABLED, gap tracked on #691)', () => {
    zonesState.pushed = [] // no /ip address table pushed -- #687's data gap, not a rendering bug
    appState.events = [event({ inInterface: 'bridge1', srcIp: '192.168.1.50' })]
    policyState.anyPushed = false
    const { container } = render(Topography)
    flushSync()

    // The boundary-derived note was chrome under round 29; round 30 draws
    // no explanatory apparatus anywhere on the topography, so it stays
    // unmounted (see the DEGRADED_NOTE_ENABLED comment in Topography.svelte).
    expect(container.querySelector('.degraded')).toBeNull()

    // The zone card itself: a boundary name only -- no fabricated
    // subnet, no fabricated coverage badge.
    expect(container.querySelector('.n-cidr')).toBeNull()
    expect(container.querySelector('.n-cov')).toBeNull()
  })
})

describe('the lens selector, ported to the scene\'s own bottom-left bar (#682)', () => {
  it('renders the three lenses as .wlens2, not a top-right tab strip, and switches on click', () => {
    const { container } = render(Topography)
    flushSync()

    expect(container.querySelector('.lenses')).toBeNull() // the old top-right strip is gone
    const bar = container.querySelector('.wlens2')
    expect(bar).not.toBeNull()

    const labels = [...bar!.querySelectorAll('button')].map((b) => b.textContent?.trim())
    expect(labels).toEqual(['traffic', 'policy', 'coverage'])

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

  it('reveals a zone dot per lane at survey instead of tilting the cards', () => {
    pushLanes(3)
    const { container } = render(Topography)
    flushSync()

    // The dots exist at every altitude; the stylesheet is what hides
    // them, so assert the structure the survey rule keys off.
    expect(container.querySelectorAll('.zone .isl-card').length).toBe(3)
    expect(container.querySelectorAll('.zone .g-dot .zone-dot').length).toBe(3)
    expect(container.querySelectorAll('.zone .g-dot .zone-label').length).toBe(3)
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

  it('restores the survey tilt\'s own perspective to round 30\'s ratified 1400px, not the drifted 900px', () => {
    // The rotateX/scale/translateY triple is unchanged from the mockup;
    // only the perspective distance had drifted smaller, which makes the
    // very same tilt read as stronger (foreshortening grows as this
    // number shrinks) -- see the .stage svg comment in the component.
    expect(componentSource).toMatch(/\.stage svg\s*{\s*perspective:\s*1400px;/)
    expect(componentSource).not.toMatch(/perspective:\s*900px/)
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
