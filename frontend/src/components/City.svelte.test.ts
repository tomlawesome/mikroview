// SPDX-License-Identifier: AGPL-3.0-only
//
// #863: the city as a component. The ground model has its own tests
// under lib/city; this covers what only the component does -- names on
// every district and building, the keyboard walk, and the camera
// landing at once when the reader asked for reduced motion.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync, tick } from 'svelte'
import { mockupEstate } from '../lib/city/fixture'
import { layoutGround } from '../lib/city/layout'
import { cityImportanceState } from '../lib/cityImportance.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { appState } from '../lib/state.svelte'
import type { ClientEvent } from '../lib/types'
import City from './City.svelte'

const ground = layoutGround(mockupEstate())

function matchMedia(matches: boolean) {
  window.matchMedia = ((query: string) =>
    ({ matches, media: query, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, onchange: null, dispatchEvent: () => false }) as MediaQueryList) as typeof window.matchMedia
}

afterEach(() => {
  vi.restoreAllMocks()
  // @ts-expect-error jsdom has none; tests that set one put it back
  delete window.matchMedia
})

const key = (el: Element, k: string, shift = false) => {
  el.dispatchEvent(new KeyboardEvent('keydown', { key: k, shiftKey: shift, bubbles: true }))
  flushSync()
}

describe('City', () => {
  it('names every district and building for a screen reader', () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const plates = container.querySelectorAll('.plate[role="button"]')
    const blks = container.querySelectorAll('.blk[role="button"]')
    expect(plates.length).toBe(ground.districts.length)
    expect(blks.length).toBe(ground.districts.reduce((n, d) => n + d.buildings.length, 0) + ground.nodes.length)
    for (const el of [...plates, ...blks]) expect(el.getAttribute('aria-label')).toMatch(/\S/)
    expect(container.querySelector('.mini rect.viewport')).not.toBeNull()
  })

  it('walks buildings within a district and districts within the map', async () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const first = container.querySelector<HTMLElement>('.plate[tabindex="0"]') as HTMLElement
    expect(first.dataset.cid).toBe(ground.districts[0].id)
    key(first, 'ArrowRight')
    await tick()
    const b0 = ground.districts[0].buildings
    expect(document.activeElement?.getAttribute('data-cid')).toBe(b0[0].id)
    key(document.activeElement as Element, 'ArrowRight')
    await tick()
    expect(document.activeElement?.getAttribute('data-cid')).toBe(b0[1].id)
    key(document.activeElement as Element, 'ArrowDown')
    await tick()
    expect(document.activeElement?.getAttribute('data-cid')).toBe(ground.districts[1].id)
    key(document.activeElement as Element, 'ArrowUp')
    await tick()
    expect(document.activeElement?.getAttribute('data-cid')).toBe(ground.districts[0].id)
  })

  it('pans with Shift and the arrows, and leaves Enter and Escape alone', () => {
    // Reduced motion, so the pan lands before the next line reads it.
    matchMedia(true)
    const { container } = render(City, { props: { stop: 'district', ground } })
    const vp = () => container.querySelector('.mini rect.viewport')?.getAttribute('x')
    const before = vp()
    const first = container.querySelector('.plate[tabindex="0"]') as Element
    key(first, 'ArrowLeft', true)
    expect(vp()).not.toBe(before)
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    first.dispatchEvent(enter)
    expect(enter.defaultPrevented).toBe(false)
    const esc = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    first.dispatchEvent(esc)
    expect(esc.defaultPrevented).toBe(false)
  })

  it('moves the camera at once under reduced motion, and animates otherwise', async () => {
    // Records the ask; never runs the frame, so nothing loops.
    const raf = vi.fn(() => 1)
    window.requestAnimationFrame = raf as unknown as typeof window.requestAnimationFrame
    window.cancelAnimationFrame = () => {}

    matchMedia(true)
    const quiet = render(City, { props: { stop: 'city', ground } })
    await quiet.rerender({ stop: 'street', ground })
    flushSync()
    expect(raf).not.toHaveBeenCalled()
    expect(quiet.container.querySelector('svg > g')?.getAttribute('transform')).toMatch(/scale\(1\)$/)
    quiet.unmount()

    matchMedia(false)
    const lively = render(City, { props: { stop: 'city', ground } })
    await lively.rerender({ stop: 'street', ground })
    flushSync()
    expect(raf).toHaveBeenCalled()
  })

  it('says plainly when no router has ever pushed a rule table, rather than claiming DARK or LOGGED', () => {
    const unpushed = layoutGround({ ...mockupEstate(), rulesPushed: false, gates: [] })
    const { container } = render(City, { props: { stop: 'district', ground: unpushed } })
    expect(container.textContent).toContain('NO RULES PUSHED')
    expect(container.textContent).not.toContain('DARK')
    expect(container.textContent).not.toContain('LOGGED')
    for (const p of container.querySelectorAll('.plate')) expect(p.getAttribute('aria-label')).toMatch(/no rule table has been pushed yet/)
  })

  it('carries the refusing rule beside a drop mark, from the events themselves', () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    expect(container.textContent).toContain('caught by iot-egress-drop')
    expect(container.textContent).toContain('caught by guest-isolation')
  })

  it('the policy lens lights every gate with its rule number; the traffic lens leaves the wall quiet', () => {
    const quiet = render(City, { props: { stop: 'district', ground, lens: 'traffic' } })
    expect(quiet.container.querySelectorAll('.gate-n').length).toBe(0)
    quiet.unmount()
    const lit = render(City, { props: { stop: 'district', ground, lens: 'policy' } })
    expect(lit.container.querySelectorAll('.gate-n').length).toBeGreaterThan(0)
  })
})

describe('standing on a building (#868)', () => {
  const LAN1 = 'bridge-lan/10.10.0.10'
  const SRV1 = 'vlan-srv/10.20.0.10'

  let nextId = 1
  function event(over: Partial<ClientEvent> = {}): ClientEvent {
    return {
      id: nextId++,
      time: '2026-09-03T12:00:00Z',
      receivedAt: Date.now(),
      deviceId: 'rb5009',
      sourceIp: '10.10.0.10',
      action: 'accept',
      ruleLabel: 'r',
      chain: 'forward',
      raw: '',
      ...over,
    }
  }

  beforeEach(() => {
    matchMedia(true) // reduced motion: every camera move lands at once
    appState.events = []
  })

  afterEach(() => {
    appState.events = []
  })

  it('drops the camera to the street stop centred on the building, and Escape restores the exact stop and pan it came from', () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const before = container.querySelector('.mini rect.viewport')?.getAttribute('x')
    const lan1 = container.querySelector('[data-cid="' + LAN1 + '"]') as Element
    fireEvent.click(lan1)
    flushSync()
    expect(container.querySelector('.city')?.getAttribute('data-stop')).toBe('street')
    expect(container.querySelector('.city svg')?.getAttribute('aria-label')).toContain('Standing on lan-1')

    lan1.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    flushSync()
    expect(container.querySelector('.city')?.getAttribute('data-stop')).toBe('district')
    expect(container.querySelector('.mini rect.viewport')?.getAttribute('x')).toBe(before)
  })

  it('Enter stands on the focused building, same as a click', async () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    // districts[0] is LAN, and ArrowRight from it walks onto its first
    // building -- lan-1, the same LAN1 id the other tests click.
    const firstDistrict = container.querySelector('.plate[tabindex="0"]') as Element
    key(firstDistrict, 'ArrowRight')
    await tick()
    expect(document.activeElement?.getAttribute('data-cid')).toBe(LAN1)
    key(document.activeElement as Element, 'Enter')
    await tick()
    expect(container.querySelector('.city')?.getAttribute('data-stop')).toBe('street')
  })

  it('fades every road that is not its own, lights the accepted peer through the gate, and marks direction from the flow', () => {
    appState.events = [
      // lan-1 spoke to srv-1, accepted, through the lit lan->srv gate.
      // Nothing else in the buffer names lan-1 at all, so every other
      // road on the map -- including the fixture's own bridge-lan|wlan-wsh
      // road, one boundary over -- is unrelated to it.
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 990, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()

    // Its own lane and the lan->srv road keep full opacity and flow;
    // an unrelated road elsewhere on the map fades and carries no flow.
    const ownRoad = container.querySelector('[data-road="bridge-lan|vlan-srv"]') as SVGPathElement
    const otherRoad = container.querySelector('[data-road="bridge-lan|wlan-wsh"]') as SVGPathElement
    expect(otherRoad).not.toBeNull()
    expect(Number(ownRoad.getAttribute('stroke-opacity'))).toBeGreaterThan(Number(otherRoad.getAttribute('stroke-opacity')))
    expect(container.querySelector('[data-road="bridge-lan|wlan-wsh"].flow')).toBeNull()
    // lan-1 spoke (direction 'out'): the flow is not reversed.
    expect(container.querySelector('[data-road="bridge-lan|vlan-srv"].flow')).not.toBeNull()
    expect(container.querySelector('[data-road="bridge-lan|vlan-srv"].flow.flow-rev')).toBeNull()

    // srv-1, the accepted peer, is not dimmed; an uninvolved iot host is.
    const srv1 = container.querySelector('[data-cid="' + SRV1 + '"]')
    expect(srv1?.classList.contains('focused')).toBe(false) // sanity: it's lit, not merely keyboard-focused
    const srv1Paints = srv1?.querySelectorAll('path') ?? []
    expect([...srv1Paints].some((p) => p.getAttribute('fill-opacity') === '0.12')).toBe(false)

    // The port it asked for is drawn on the road.
    expect(container.textContent).toContain(':990')
  })

  it('shows dashes moving toward the host when it was spoken to, not away', () => {
    appState.events = [event({ srcIp: '10.20.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-srv', outInterface: 'bridge-lan', action: 'accept' })]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(container.querySelector('[data-road="bridge-lan|vlan-srv"].flow.flow-rev')).not.toBeNull()
  })

  it('a refused road ends at the wall carrying the refusing rule from the event itself, even where no aggregate road already drops', () => {
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.10', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', ruleLabel: 'no-cross-router-cams' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(container.textContent).toContain('caught by no-cross-router-cams')
  })

  it('says plainly when the refusing event carried no rule label, rather than inventing one', () => {
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.10', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', ruleLabel: '' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(container.textContent).toContain('caught, no rule named')
  })

  it('the composer pins to the wall, drafted, never run, with what it has been asking for and the count', () => {
    appState.events = Array.from({ length: 14 }, (_, i) =>
      event({
        id: i + 1,
        srcIp: '10.10.0.10',
        dstIp: '10.60.0.10',
        inInterface: 'bridge-lan',
        outInterface: 'wlan-cams',
        action: 'drop',
        ruleLabel: 'no-cross-router-cams',
        dstPort: 445,
        protocol: 'tcp',
      }),
    )
    const { container } = render(City, { props: { stop: 'street', ground } })
    expect(container.querySelector('.composer')).toBeNull()
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const composer = container.querySelector('.composer') as HTMLElement
    expect(composer).not.toBeNull()
    expect(composer.textContent).toContain("it's been asking")
    expect(composer.textContent).toContain('tcp/445')
    expect(composer.textContent).toContain('14×')
    expect(composer.textContent).toContain('caught by no-cross-router-cams')
    expect(composer.textContent).toContain('drafted · never run')
    const cmd = composer.querySelector('.cm-code')?.textContent ?? ''
    expect(cmd).toContain('src-address=10.10.0.10')
    expect(cmd).toContain('dst-address=10.60.0.10')
    expect(cmd).toContain('action=accept')
  })

  it('the crumb states name, address, reach counts and that Esc surfaces, as in 2D', () => {
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv' }),
      event({ srcIp: '10.40.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-guest', outInterface: 'bridge-lan' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    expect(container.querySelector('.crumb')).toBeNull()
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const crumb = container.querySelector('.crumb') as HTMLElement
    expect(crumb).not.toBeNull()
    expect(crumb.querySelector('b')?.textContent).toBe('lan-1')
    expect(crumb.textContent).toContain('10.10.0.10')
    expect(crumb.textContent).toContain('reaches')
    expect(crumb.textContent).toContain('reached by')
    expect(crumb.textContent).toContain('1')
    expect(crumb.textContent).toContain('Esc surfaces')
  })
})

describe('the importance toggle (#867)', () => {
  beforeEach(() => {
    cityImportanceState.set('depended-on')
    flagsState.loaded = false
    watchlistState.loaded = false
  })

  it('only appears at the city stop, as a real button so it is in the tab order and Enter/Space already activate it', () => {
    const district = render(City, { props: { stop: 'district', ground } })
    expect(district.container.querySelector('.importance .reading')).toBeNull()
    district.unmount()

    const { container } = render(City, { props: { stop: 'city', ground } })
    const btn = container.querySelector('.importance .reading')
    expect(btn?.tagName).toBe('BUTTON')
    expect(btn?.getAttribute('tabindex')).not.toBe('-1')
  })

  it('states the current reading in its own text and aria, not just a pressed style, and switches when activated', async () => {
    const { container } = render(City, { props: { stop: 'city', ground } })
    const btn = container.querySelector('.importance .reading') as HTMLButtonElement
    expect(btn.textContent).toContain('depended-on')
    expect(btn.getAttribute('aria-label')).toContain('depended-on')
    expect(btn.getAttribute('aria-pressed')).toBe('false')

    await fireEvent.click(btn)
    await tick()
    expect(cityImportanceState.reading).toBe('watched')
    expect(btn.textContent).toContain('watched')
    expect(btn.getAttribute('aria-label')).toContain('watched')
    expect(btn.getAttribute('aria-pressed')).toBe('true')

    await fireEvent.click(btn)
    await tick()
    expect(cityImportanceState.reading).toBe('depended-on')
  })

  it('says plainly that the watched reading is not yet known while the flags/watchlist stores have not loaded, and stops saying so once they have', async () => {
    cityImportanceState.set('watched')
    const { container } = render(City, { props: { stop: 'city', ground } })
    expect(container.querySelector('.importance .notice')?.textContent).toMatch(/not loaded yet/)

    flagsState.loaded = true
    watchlistState.loaded = true
    await tick()
    expect(container.querySelector('.importance .notice')).toBeNull()
  })
})
