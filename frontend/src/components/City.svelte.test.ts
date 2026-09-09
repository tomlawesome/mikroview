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
import { roadEnds } from '../lib/city/baselineRoads'
import { faceOf } from '../lib/city/walls'
import { appState } from '../lib/state.svelte'
import { zonesState } from '../lib/zones.svelte'
import { policyState } from '../lib/policy.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import { authState } from '../lib/auth.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { baselineState } from '../lib/baseline.svelte'
import { EMPTY_OFF_BASELINE, type OffBaselineLine } from '../lib/baseline'
import type { ClientEvent, Device, FirewallEvent } from '../lib/types'
import { emptyFilters } from '../lib/types'
import { portFilterState } from '../lib/portFilter.svelte'
import { mapTraceState } from '../lib/mapTrace.svelte'
import type { TraceResponse } from '../lib/api'
import City from './City.svelte'

// #915: twelve of these tests timed out on the GitLab runner against
// vitest's 5000ms default while GitHub CI stayed green. Measured here
// rather than guessed. One render of City into jsdom builds 1576 elements
// and costs 650-900ms; the slowest tests mount twice and re-render twice,
// so they land near 2.5s on an idle machine and 5.3s on a busy one -- same
// box, same commit, twice the time, purely from CPU contention. GitLab's
// worst was 9260ms.
//
// The time is jsdom's, not the component's. In a CPU profile of a single
// render, jsdom and its symbol-tree node store take 58% and Svelte's
// runtime 23%, while City.svelte's own compiled code is 3% and the ground
// model it renders is 9ms. The hottest single function is symbol-tree's
// `index`, which jsdom calls on every insert to find a node's position
// among its siblings; that walk is linear each time, so filling one wide
// SVG parent is quadratic. No assertion here can be sharpened to avoid it
// -- the cost is the size of the DOM the component builds, which is the
// lever #690 ("the UI is very laggy") would have to pull, not this file.
//
// 20000ms is a shade over twice the worst time observed anywhere, picked
// for the variance rather than the average: contention alone already
// doubles these numbers, so 1.5x headroom would go red again on a busier
// runner. It stays far below anything a genuinely stuck render could hide
// behind, so a real hang still fails as a hang. Set per file on purpose --
// every other suite keeps the 5000ms default, and should.
vi.setConfig({ testTimeout: 20000 })

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

  it('starts from an explicit initial camera rather than the stop\'s own default, and reports every camera change back (#869)', () => {
    // Topography saves what onCameraChange reports here across this
    // component's own mount/unmount, so a slider crossing back into the
    // city can hand the exact pan back in as initialS/initialCentre --
    // "the pan position carries where the two views share coordinates".
    const reported: { s: number; centre: [number, number] }[] = []
    render(City, {
      props: {
        stop: 'district',
        ground,
        initialS: 30,
        initialCentre: [12, 34],
        onCameraChange: (s: number, centre: [number, number]) => reported.push({ s, centre }),
      },
    })
    flushSync()
    expect(reported[0]).toEqual({ s: 30, centre: [12, 34] })
  })

  it('says plainly when no router has ever pushed a rule table, and carries no coverage word at all', () => {
    const unpushed = layoutGround({ ...mockupEstate(), rulesPushed: false, gates: [] })
    const { container } = render(City, { props: { stop: 'district', ground: unpushed } })
    // Round 49 (#1016): the plaque says name and subnet, and this one
    // dim line -- never a coverage word, because the wall is what says
    // the coverage now, and a plaque that ignored declarations said the
    // wrong thing anyway (#1014). "no rule table pushed" stays, because
    // it is a different fact from dark: there, a table exists and
    // nothing on it logs.
    expect(container.textContent).toContain('no rule table pushed')
    expect(container.textContent).not.toContain('NO RULES PUSHED')
    expect(container.textContent).not.toContain('DARK')
    expect(container.textContent).not.toContain('LOGGED')
    for (const p of container.querySelectorAll('.plate')) expect(p.getAttribute('aria-label')).toMatch(/no rule table has been pushed yet/)
  })

  // ---- Round 49 (#1016): coverage is the material, always on, and the
  // wall is where the declare path starts.

  it('draws each wall edge in its own boundary’s state, and never a coverage badge', () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const walls = [...container.querySelectorAll('[data-wall]')]
    expect(walls.length).toBeGreaterThan(0)
    const labels = walls.map((w) => w.getAttribute('aria-label'))
    // bridge-lan -> vlan-srv logs but the way back does not, so the LAN's
    // edge toward Servers is dark: the worse of the two, because a wall
    // has no direction.
    expect(labels.some((l) => l?.includes('dark'))).toBe(true)
    // An ungated edge keeps the district's own ink, so it is not dark.
    expect(labels.some((l) => l?.includes('logged'))).toBe(true)
  })

  it('lamps a gate only when the whole boundary logs, never one direction of it', () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    // The workshop's boundary onto the LAN logs both ways in the
    // fixture; nothing else in the estate does, so the lamps that exist
    // belong to boundaries that log, and there is at least one.
    expect(container.querySelectorAll('circle.lamp').length).toBeGreaterThan(0)
  })

  it('hooks every gate post with `data-gate`, so gates can be counted (#1022)', () => {
    // #1022's own hook was on the policy lens's gate pill, which round
    // 49 deleted with the lens; before that the gate posts were pushed
    // into the drawing as anonymous geometry, so "how many gates" could
    // not be asked of the DOM at all and live-city-walls.mjs had to drop
    // the question. The hook is on the posts themselves now, which is
    // where it survives a redraw of anything else.
    const { container } = render(City, { props: { stop: 'district', ground } })
    const posts = [...container.querySelectorAll('[data-gate]')]
    // A gate on one of the two back edges the camera cannot see draws
    // nothing, the same silence a hidden building face keeps, so the
    // count is of the gates actually facing the reader.
    const drawn = ground.districts.flatMap((d) => d.gates.filter((gt) => faceOf(d, gt.p) !== null).map((gt) => `${d.id}:${gt.key}`))
    expect(drawn.length).toBeGreaterThan(0)
    // Two posts stand either side of each break, so the count of posts
    // is twice the count of gates and the distinct hooks are the gates.
    expect(new Set(posts.map((p) => p.getAttribute('data-gate')))).toEqual(new Set(drawn))
    expect(posts.length).toBe(drawn.length * 2)
    // A district with no pushed rule table has no gates, so no hooks.
    const unpushed = render(City, { props: { stop: 'district', ground: layoutGround({ ...mockupEstate(), rulesPushed: false, gates: [] }) } })
    expect(unpushed.container.querySelectorAll('[data-gate]').length).toBe(0)
    unpushed.unmount()
  })

  it('names the gate’s rule number and name on its own card, never on the drawing (#1016)', async () => {
    // Owner, 2026-09-08 on #1016: a gate is one opening in the wall and
    // one firewall rule, and its card says which -- `rule 4 · nas
    // access`, numbered as RouterOS numbers it. The drawing itself
    // stays wordless, the same rule as everywhere else on this surface.
    const { container } = render(City, { props: { stop: 'district', ground } })
    const post = container.querySelector('[data-gate^="bridge-lan:"][data-gate$="vlan-srv"]') as Element
    expect(post).not.toBeNull()
    fireEvent.pointerEnter(post)
    flushSync()
    await tick()
    const card = container.querySelector('.bcard') as HTMLElement
    expect(card).not.toBeNull()
    // The LAN/Servers break folds two rules' directions into one gate;
    // the card names the lower-numbered of them and that rule's own
    // comment, never a name borrowed from the other.
    expect(card.querySelector('[data-gate-rule]')?.textContent?.trim()).toBe('rule 4 · nas access')
    expect(card.textContent).not.toContain('rule 9')
    // Nothing is written on the drawing.
    for (const t of container.querySelectorAll('.city svg text')) expect(t.textContent ?? '').not.toMatch(/^rule \d/)
  })

  it('names a gate whose rule carries no comment by its number alone, never an invented name', async () => {
    // wlan-wsh → bridge-lan is the workshop's own gate; its lower
    // direction (`vlan-srv → bridge-lan`, rule 9) carries no comment at
    // all in the fixture, and neither does an estate whose operator
    // never commented a rule. A gate with no name is shown with none.
    const bare = mockupEstate()
    bare.gates = bare.gates.map((g) => ({ ...g, comment: '' }))
    const { container } = render(City, { props: { stop: 'district', ground: layoutGround(bare) } })
    const post = container.querySelector('[data-gate]') as Element
    fireEvent.pointerEnter(post)
    flushSync()
    await tick()
    const line = container.querySelector('.bcard [data-gate-rule]')
    expect(line?.textContent?.trim()).toMatch(/^rule \d+$/)
  })

  it('opens the boundary card from the wall, listing both directions, with declare behind the pin', async () => {
    authState.role = 'admin'
    authState.username = 'tom'
    const { container } = render(City, { props: { stop: 'district', ground } })
    const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
    expect(wall).toBeTruthy()
    await fireEvent.pointerEnter(wall!)
    flushSync()
    const card = container.querySelector('.bcard') as HTMLElement
    expect(card).toBeTruthy()
    // Both directions, because a wall has no direction of its own.
    expect(card.textContent).toContain('bridge-lan → vlan-srv')
    expect(card.textContent).toContain('vlan-srv → bridge-lan')
    expect(card.textContent).toContain('nothing drawn across it is a fact; nothing is known')
    // The three actions the ratified card offers, and no form until it
    // is pinned.
    expect(card.textContent).toContain('declare quiet on purpose ▸')
    expect(card.textContent).toContain('rules ▸')
    expect(card.textContent).toContain('stream ▸')
    expect(card.querySelector('.form')).toBeNull()

    await fireEvent.click(card.querySelector('.pin') as HTMLElement)
    flushSync()
    const pinned = container.querySelector('.bcard') as HTMLElement
    expect(pinned.classList.contains('pinned')).toBe(true)
    expect(pinned.textContent).toContain('QUIET ON PURPOSE — WHY?')
    expect(pinned.textContent).toContain('both directions')
    expect(pinned.textContent).toContain('as tom')
    // Both directions is checked by default: one declared and the other
    // still dark would leave the wall grey and the card explaining why.
    expect((pinned.querySelector('.both input[type="checkbox"]') as HTMLInputElement).checked).toBe(true)
    // A reason is required -- Declare stays refused until there is one.
    expect((pinned.querySelector('.go') as HTMLButtonElement).disabled).toBe(true)
    authState.role = ''
    authState.username = ''
  })

  it('declares through the existing coverage API, one key per direction (#392)', async () => {
    authState.role = 'admin'
    authState.username = 'tom'
    const declare = vi.spyOn(coverageState, 'declare').mockResolvedValue(true)
    const { container } = render(City, { props: { stop: 'district', ground } })
    const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
    await fireEvent.pointerEnter(wall!)
    flushSync()
    await fireEvent.click(container.querySelector('.bcard .pin') as HTMLElement)
    flushSync()
    const input = container.querySelector('.bcard .form input:not([type])') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'the servers answer, they never call back' } })
    flushSync()
    await fireEvent.click(container.querySelector('.bcard .go') as HTMLElement)
    await tick()
    expect(declare.mock.calls.map((c) => c[0])).toEqual(['bridge-lan|vlan-srv', 'vlan-srv|bridge-lan'])
    expect(declare.mock.calls[0][1]).toBe('the servers answer, they never call back')
    authState.role = ''
    authState.username = ''
  })

  it('shows a declared boundary’s reason, who and when, and offers undeclare', async () => {
    authState.role = 'admin'
    coverageState.declarations = [
      { key: 'bridge-lan|vlan-srv', reason: 'the servers answer, they never call back', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
      { key: 'vlan-srv|bridge-lan', reason: 'the servers answer, they never call back', declaredBy: 'tom', declaredAt: '2026-09-01T14:20:00Z' },
    ]
    const { container } = render(City, { props: { stop: 'district', ground } })
    const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
    await fireEvent.pointerEnter(wall!)
    flushSync()
    const card = container.querySelector('.bcard') as HTMLElement
    expect(card.querySelector('.quote')?.textContent).toBe('the servers answer, they never call back')
    expect(card.textContent).toContain('tom')
    expect(card.textContent).toContain('undeclare ▸')
    expect(card.textContent).not.toContain('declare quiet on purpose ▸')
    coverageState.declarations = []
    authState.role = ''
  })

  // #1027: the card opened on hover and the pointer then had to travel
  // to it to reach the pin. Setting off fired pointerleave on the wall,
  // which nulled hoverWall, which unmounted the card -- so the pin could
  // never be clicked, and the card's own pointerenter ran
  // openWallCard(id, openWall!.side) against an openWall already null.
  //
  // Floating the card beside its wall shortens that journey but does not
  // remove it, so this is about the journey itself, not the distance.
  it('keeps the boundary card up while the pointer travels from the wall to it (#1027)', async () => {
    vi.useFakeTimers()
    try {
      authState.role = 'admin'
      authState.username = 'tom'
      const { container } = render(City, { props: { stop: 'district', ground } })
      const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
      await fireEvent.pointerEnter(wall!)
      flushSync()
      expect(container.querySelector('.bcard')).toBeTruthy()

      // The pointer sets off for the card. Leaving the wall is reported
      // before it has arrived anywhere.
      await fireEvent.pointerLeave(wall!)
      flushSync()
      const card = container.querySelector('.bcard') as HTMLElement
      expect(card, 'the card was gone before the pointer could reach it').toBeTruthy()

      // It arrives, and the card stays for as long as it is there.
      await fireEvent.pointerEnter(card)
      flushSync()
      vi.advanceTimersByTime(5000)
      flushSync()
      expect(container.querySelector('.bcard'), 'the card closed while the pointer was on it').toBeTruthy()

      // The pin is reachable, which is the whole of #1027.
      await fireEvent.click(container.querySelector('.bcard .pin') as HTMLElement)
      flushSync()
      expect(container.querySelector('.bcard')?.classList.contains('pinned')).toBe(true)
      authState.role = ''
      authState.username = ''
    } finally {
      vi.useRealTimers()
    }
  })

  it('lets the card go once the pointer has arrived at neither the wall nor the card', async () => {
    vi.useFakeTimers()
    try {
      const { container } = render(City, { props: { stop: 'district', ground } })
      const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
      await fireEvent.pointerEnter(wall!)
      flushSync()
      expect(container.querySelector('.bcard')).toBeTruthy()
      await fireEvent.pointerLeave(wall!)
      flushSync()
      // The grace period is a delay, not a card that never closes.
      vi.advanceTimersByTime(5000)
      flushSync()
      expect(container.querySelector('.bcard')).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps the open wall marked, pinned as well as hovered', async () => {
    // With ten grey dashed boundaries on screen, a card naming two of
    // them in words alone does not say which is being described.
    authState.role = 'admin'
    const { container } = render(City, { props: { stop: 'district', ground } })
    const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))!
    expect(wall.classList.contains('on')).toBe(false)
    await fireEvent.pointerEnter(wall)
    flushSync()
    expect(wall.classList.contains('on'), 'the hovered wall is not marked').toBe(true)
    await fireEvent.click(container.querySelector('.bcard .pin') as HTMLElement)
    flushSync()
    const still = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))!
    expect(still.classList.contains('on'), 'the pinned wall stopped being marked').toBe(true)
    authState.role = ''
  })

  // The whole chain, in the shape that catches a wrong pixel mapping: a
  // container that is not the viewBox's own 2:1. jsdom lays nothing out
  // and has no getScreenCTM, so both are supplied here exactly as a
  // browser would report them for a 900x900 box -- 1400x700 meets it as
  // 900x450, centred, so the drawing occupies y 225..675 and nothing
  // else. A mapping that took its ratio from the container's height
  // would spread the same drawing over the whole 0..900 and put the
  // leader's dot somewhere the wall is not.
  it('places the card and its leader correctly in a container that is not the viewBox’s shape', async () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const host = container.querySelector('.city') as HTMLElement
    const svg = container.querySelector('.city > svg') as SVGSVGElement
    const box = { left: 0, top: 0, width: 900, height: 900, right: 900, bottom: 900, x: 0, y: 0 }
    host.getBoundingClientRect = () => box as DOMRect
    svg.getBoundingClientRect = () => box as DOMRect
    const s = 900 / 1400
    ;(svg as unknown as { getScreenCTM: () => DOMMatrix }).getScreenCTM = () =>
      ({ a: s, b: 0, c: 0, d: s, e: 0, f: (900 - 700 * s) / 2 }) as DOMMatrix

    const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
    await fireEvent.pointerEnter(wall!)
    flushSync()

    const card = container.querySelector('.bcard') as HTMLElement
    expect(card.classList.contains('placed'), 'the card never got a measured position').toBe(true)
    const left = parseFloat(card.style.left)
    const top = parseFloat(card.style.top)
    expect(Number.isFinite(left) && Number.isFinite(top)).toBe(true)
    // On the stage, card and all.
    expect(left).toBeGreaterThanOrEqual(0)
    expect(top).toBeGreaterThanOrEqual(0)
    expect(left + 288).toBeLessThanOrEqual(900)
    expect(top + 180).toBeLessThanOrEqual(900)

    // The leader's dot is on the wall, which is inside the letterboxed
    // band -- not spread over the container's full height.
    const dot = container.querySelector('.leader circle') as SVGCircleElement
    expect(dot, 'no leader was drawn').toBeTruthy()
    const cy = parseFloat(dot.getAttribute('cy')!)
    expect(cy).toBeGreaterThanOrEqual(225)
    expect(cy).toBeLessThanOrEqual(675)

    // And the leader actually joins the card it belongs to.
    const d = container.querySelector('.leader path')!.getAttribute('d')!
    const [, , ex, ey] = d.match(/M([-\d.]+) ([-\d.]+)L([-\d.]+) ([-\d.]+)/)!.slice(1).map(Number)
    expect(ex).toBeGreaterThanOrEqual(left)
    expect(ex).toBeLessThanOrEqual(left + 288)
    expect(ey).toBeGreaterThanOrEqual(top)
    expect(ey).toBeLessThanOrEqual(top + 180)
  })

  it('marks a refused road with the plain word `dropped`, never the refusing rule’s name (#1036)', () => {
    // DESIGN.md's metaphor table: "the plain mark only, reading
    // `dropped`; the refusing rule's name is NOT written on the
    // drawing -- it lives in the card". Nothing is written on a road or
    // a strand, and that is one rule, not two: if it names something,
    // it is in a card. The rule's name is still on the ground model as
    // `Road.refusedBy`, and the line card and the composer both read it.
    //
    // The aggregate names no *source* either, which is the part #991
    // settled: only a standing host's own strands resolve to one
    // building.
    const { container } = render(City, { props: { stop: 'district', ground } })
    const labels = [...container.querySelectorAll('.drop-t')].map((e) => e.textContent ?? '')
    expect(labels.length).toBeGreaterThan(0)
    const dropped = ground.roads.filter((r) => r.stop === 'drop')
    expect(dropped.length).toBeGreaterThan(0)
    // At least one of these fixtures names a rule, so the test would go
    // quiet rather than red if the fixture ever stopped carrying one.
    expect(dropped.some((r) => r.refusedBy)).toBe(true)
    for (const t of labels) expect(t).toBe('dropped')
    for (const r of dropped) if (r.refusedBy) expect(labels.join(' ')).not.toContain(r.refusedBy)
  })

  it('lamps a logged gate whose break falls on a face the camera cannot see (#1034)', () => {
    // Nearly every logging accept rule in a real table -- and every one
    // in the demo estate -- crosses toward the WAN, so its gate aims at
    // the bridge post north of the town and the break lands on one of
    // the two edges the camera never sees. The break itself cannot be
    // drawn there. The lamp still must: the metaphor table makes a lamp
    // on a gate the one thing that says the rule logs, so a gate with no
    // lamp reads exactly like a dark boundary.
    const est = mockupEstate()
    // Round 49 (#1016) folds a boundary's two directions into one break
    // in the wall, so a gate now carries both readings and the fixture
    // has to state both: the reading this test is about is the folded
    // one, and a gate given only its outbound half reads dark whatever
    // its own direction logs.
    const wanGate = {
      key: 'forward|bridge-lan|ether1',
      chain: 'forward',
      inInterface: 'bridge-lan',
      outInterface: 'ether1',
      logged: true,
      ruleCount: 1,
      ordinal: 1,
      comment: 'lan to wan',
      edgeKey: 'bridge-lan|ether1',
      reverseEdgeKey: 'ether1|bridge-lan',
      coverage: 'logged' as const,
      reverseCoverage: 'logged' as const,
    }
    // Nothing else on the map may lamp, so every circle.lamp counted
    // below is this gate's: the WAN deck reads dark (round 49 took that
    // from `wanCoverage`, not from `wanLogged`, which now only sets the
    // deck's up/unknown state) and there are no tunnel footbridges to
    // lamp either.
    const quiet = { ...est, wanLogged: false, wanCoverage: 'dark' as const, tunnels: [] }
    const litGround = layoutGround({ ...quiet, gates: [wanGate] })
    const lan = litGround.districts.find((d) => d.id === 'bridge-lan')!
    expect(lan.gates[0].lamp).toBe(true)
    expect(faceOf(lan, lan.gates[0].p)).toBeNull()

    const lit = render(City, { props: { stop: 'district', ground: litGround } })
    expect(lit.container.querySelectorAll('circle.lamp').length).toBeGreaterThan(0)
    lit.unmount()

    // And the same gate with nothing logging on it draws no lamp, so
    // this cannot pass by lighting every gate regardless.
    const darkGround = layoutGround({ ...quiet, gates: [{ ...wanGate, logged: false, coverage: 'dark' as const, reverseCoverage: 'dark' as const }] })
    const dark = render(City, { props: { stop: 'district', ground: darkGround } })
    expect(dark.container.querySelectorAll('circle.lamp').length).toBe(0)
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

  /** An off-baseline document naming one line, so the road that carries
   *  it is bright. Round 49 makes the flow dashes part of the bright
   *  treatment rather than a mark of ownership, so a reach test that
   *  wants to read direction off the flow has to say what is off the
   *  pattern first. */
  const offLine = (srcIp: string, dstIp: string) => ({
    config: { days: 3, of: 14 },
    generatedAt: 1,
    count: 1,
    lines: [{ key: 'reach-line', srcIp, dstIp, port: 990, proto: 'tcp', count: 40, firstSeenToday: Date.now(), outcome: 'accept' as const }],
  })

  beforeEach(() => {
    matchMedia(true) // reduced motion: every camera move lands at once
    appState.events = []
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
  })

  afterEach(() => {
    appState.events = []
    baselineState.off = EMPTY_OFF_BASELINE
  })

  it('standing drops the camera at once under reduced motion, and animates otherwise', () => {
    const raf = vi.fn(() => 1)
    window.requestAnimationFrame = raf as unknown as typeof window.requestAnimationFrame
    window.cancelAnimationFrame = () => {}

    matchMedia(true)
    const quiet = render(City, { props: { stop: 'district', ground } })
    fireEvent.click(quiet.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(raf).not.toHaveBeenCalled()
    expect(quiet.container.querySelector('.city')?.getAttribute('data-stop')).toBe('street')
    quiet.unmount()

    matchMedia(false)
    const lively = render(City, { props: { stop: 'district', ground } })
    fireEvent.click(lively.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(raf).toHaveBeenCalled()
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

  it('the crumb itself surfaces, restoring the exact camera, same as Escape', async () => {
    const { container } = render(City, { props: { stop: 'district', ground } })
    const before = container.querySelector('.mini rect.viewport')?.getAttribute('x')
    await fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(container.querySelector('.city')?.getAttribute('data-stop')).toBe('street')
    await fireEvent.click(container.querySelector('.crumb') as Element)
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
    // The line is off today's pattern, so its road is bright and flows.
    baselineState.off = offLine('10.10.0.10', '10.20.0.10')
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

    // #991: the road no longer carries its own port chip while standing
    // on a building ("gone from the street stop") -- the ports moved to
    // the building's card and the gate's card instead.
    expect(container.querySelector('.port-t')).toBeNull()
  })

  it('shows dashes moving toward the host when it was spoken to, not away', () => {
    appState.events = [event({ srcIp: '10.20.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-srv', outInterface: 'bridge-lan', action: 'accept' })]
    baselineState.off = offLine('10.20.0.10', '10.10.0.10')
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect(container.querySelector('[data-road="bridge-lan|vlan-srv"].flow.flow-rev')).not.toBeNull()
  })

  it('marks the standing building’s own refused attempt `dropped`, with no rule name on the drawing (#1036)', () => {
    // Direction 'out': lan-1 is the source, so the drop is on the
    // building you are standing on and the mark never names it back to
    // itself -- #991's rule about the *source*, which is unchanged.
    // The refusing rule's name is the card's to carry, never the
    // strand's (#1036, DESIGN.md "The reach").
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.10', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', ruleLabel: 'no-cross-router-cams' }),
    ]
    const named = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(named.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const namedLabels = [...named.container.querySelectorAll('.drop-t')].map((e) => e.textContent)
    expect(namedLabels).toContain('dropped')
    for (const t of namedLabels) expect(t).not.toContain('no-cross-router-cams')
    // No source: it would be naming lan-1 back to itself.
    for (const t of namedLabels) expect(t).not.toMatch(/lan-1/)
    named.unmount()

    // A refusal no event named a rule for is still said plainly.
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.10', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', ruleLabel: '' }),
    ]
    const unnamed = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(unnamed.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    expect([...unnamed.container.querySelectorAll('.drop-t')].map((e) => e.textContent)).toContain('dropped')
  })

  it('names the source only when the drop is not on the building you are standing on (#991)', () => {
    // Direction 'in': cam-porch tried to reach lan-1 (the standing
    // building) and was refused at lan-1's own wall -- the drop is not
    // "at" lan-1, so the mark names the source. The source is the one
    // thing the mark says beyond the plain word: the refusing rule is
    // the card's (#1036).
    appState.events = [
      event({ srcIp: '10.60.0.10', srcHostName: 'cam-porch', dstIp: '10.10.0.10', inInterface: 'wlan-cams', outInterface: 'bridge-lan', action: 'drop', ruleLabel: 'no-cross-router-cams' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const labels = [...container.querySelectorAll('.drop-t')].map((e) => e.textContent)
    expect(labels).toContain('cam-porch · dropped')
    for (const t of labels) expect(t).not.toContain('no-cross-router-cams')
  })

  it('the composer opens from the refused line’s card, drafted, never run, with what it has been asking for and the count', () => {
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
    // Round 49: the composer is behind the refused line's own
    // `draft the rule ▸`, not open the moment you stand on a building.
    expect(container.querySelector('.composer')).toBeNull()
    // The lan -> cams boundary logs nothing, so no road is drawn across
    // it -- one there would claim a log line nobody wrote. The mark at
    // the wall where the line stopped carries its card instead.
    const road = container.querySelector('[data-road-hot="mark:wlan-cams"]') as Element
    expect(road).not.toBeNull()
    fireEvent.pointerEnter(road)
    flushSync()
    const draft = container.querySelector('[data-draft-rule]') as HTMLElement
    expect(draft).not.toBeNull()
    expect(draft.textContent).toContain('draft the rule ▸')
    fireEvent.click(draft)
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

  it('the host card offers `draft the rule ▸` for the standing host’s refused strand (#1035)', async () => {
    // #1035: the composer was unreachable. Its only door was the line
    // card's `draft the rule ▸`, and a line card needs a road or a mark
    // to hover -- but a strand whose district pair already ends in a
    // drop draws neither of its own (the aggregate road is drawn, and
    // the strand's mark is suppressed so the bollards are not doubled),
    // and that aggregate is not a line the reach owns, so it has no
    // card either. Standing on such a host, nothing on screen could
    // open the composer at all.
    //
    // The door belongs on the host card, which is open the moment you
    // are standing on the building anyway: guest-1 → lan-1 is refused,
    // and the guest/LAN pair already ends in a drop of its own.
    appState.events = [
      event({ srcIp: '10.40.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-guest', outInterface: 'bridge-lan', action: 'drop', ruleLabel: 'guest-isolation', dstPort: 445, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    const guest = container.querySelector('[data-cid="vlan-guest/10.40.0.10"]') as Element
    fireEvent.click(guest)
    flushSync()
    // Nothing else on the drawing could have opened it: no road of this
    // strand's own carries a card, and no mark was drawn for it.
    expect(container.querySelector('[data-road-hot="mark:bridge-lan"]')).toBeNull()
    // Standing puts the pointer on the building, so its card is open.
    fireEvent.pointerEnter(container.querySelector('[data-cid="vlan-guest/10.40.0.10"]') as Element)
    flushSync()
    await tick()
    const card = container.querySelector('.bcard.hcard') as HTMLElement
    expect(card).not.toBeNull()
    const draft = card.querySelector('[data-draft-rule]') as HTMLElement
    expect(draft).not.toBeNull()
    expect(draft.textContent).toContain('draft the rule ▸')
    expect(container.querySelector('.composer')).toBeNull()
    fireEvent.click(draft)
    flushSync()
    const composer = container.querySelector('.composer') as HTMLElement
    expect(composer).not.toBeNull()
    expect(composer.textContent).toContain('tcp/445')
    expect(composer.textContent).toContain('caught by guest-isolation')
    expect(composer.textContent).toContain('drafted · never run')
  })

  it('offers no `draft the rule ▸` on a host card with nothing refused, or on one you are not standing on (#1035)', async () => {
    // The composer is about the standing host's own busiest refused
    // strand, so the door only belongs on that host's card: on any
    // other card it would draft a rule for a building the reader is not
    // looking at.
    appState.events = [
      event({ srcIp: '10.40.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-guest', outInterface: 'bridge-lan', action: 'drop', ruleLabel: 'guest-isolation', dstPort: 445, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    // Nobody is standing yet: hovering a host offers nothing.
    fireEvent.pointerEnter(container.querySelector('[data-cid="vlan-guest/10.40.0.10"]') as Element)
    flushSync()
    await tick()
    expect(container.querySelector('.bcard.hcard [data-draft-rule]')).toBeNull()

    fireEvent.click(container.querySelector('[data-cid="vlan-guest/10.40.0.10"]') as Element)
    flushSync()
    // Standing on guest-1, but hovering lan-1: lan-1 refused nothing of
    // its own, and the guest strand is not lan-1's to draft.
    fireEvent.pointerEnter(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    await tick()
    const card = container.querySelector('.bcard.hcard') as HTMLElement
    expect(card.textContent).toContain('lan-1')
    expect(card.querySelector('[data-draft-rule]')).toBeNull()
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

  it('the crumb counts refused counterparts too, and offers Esc surfaces ▸ (round 49)', () => {
    appState.events = [
      // Two accepted counterparts out, and two refused ones -- refused
      // is counted by distinct counterpart, the same way `reaches` and
      // `reached by` are, so two strands to one counterpart still count
      // once.
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.10', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', dstPort: 445, protocol: 'tcp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.60.0.11', inInterface: 'bridge-lan', outInterface: 'wlan-cams', action: 'drop', dstPort: 22, protocol: 'tcp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.40.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-guest', action: 'drop', dstPort: 22, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const crumb = container.querySelector('.crumb') as HTMLElement
    const spans = [...crumb.querySelectorAll('span')].map((s) => s.textContent?.replace(/\s+/g, ' ').trim())
    expect(spans).toContain('refused 2')
    expect(spans).toContain('Esc surfaces ▸')
  })

  it('writes nothing on a road: the ports live in the card, on either surface', () => {
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 990, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    // No port pill, chip or label anywhere on the drawing.
    expect(container.querySelector('.port-t')).toBeNull()
    const svgText = [...container.querySelectorAll('.city svg text')].map((t) => t.textContent ?? '')
    for (const t of svgText) expect(t).not.toMatch(/:\d/)
  })
})

describe('the reach’s line card (round 49, #1016)', () => {
  const LAN1 = 'bridge-lan/10.10.0.10'
  let nextId = 1
  const event = (over: Partial<ClientEvent> = {}): ClientEvent => ({
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
  })

  beforeEach(() => {
    matchMedia(true)
    appState.events = []
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
  })

  afterEach(() => {
    appState.events = []
    baselineState.off = EMPTY_OFF_BASELINE
  })

  /** Stand on lan-1 and hover the road toward Servers. */
  function openLine(events: ClientEvent[]) {
    appState.events = events
    const r = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(r.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    const road = r.container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element
    expect(road).not.toBeNull()
    fireEvent.pointerEnter(road)
    flushSync()
    return r
  }

  const cell = (row: Element) => [...row.querySelectorAll('td')].map((td) => td.textContent?.trim())

  it('lists every port the line carried, its protocol, and what each drew', () => {
    const { container } = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 53, protocol: 'udp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 22, protocol: 'tcp', action: 'drop', ruleLabel: 'default drop' }),
    ])
    const card = container.querySelector('.lcard') as HTMLElement
    expect(card).not.toBeNull()
    // The mockup's own header row.
    expect([...card.querySelectorAll('th')].map((th) => th.textContent)).toEqual(['PORT', 'PROTO', 'ACCEPTED', 'DROPPED'])
    const rows = [...card.querySelectorAll('tbody tr')]
    // Busiest first: 445 twice, then 53 and 22 once each by port order.
    expect(cell(rows[0])).toEqual(['445', 'tcp', '2', '—'])
    expect(rows.map((r) => cell(r)[0])).toEqual(['445', '22', '53'])
    // A port nothing was accepted on shows the drop, and vice versa.
    const refused = rows.find((r) => cell(r)[0] === '22')!
    expect(cell(refused)).toEqual(['22', 'tcp', '—', '1'])
  })

  it('states the tcp-versus-udp picture, with everything else in `other`', () => {
    const { container } = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 53, protocol: 'udp' }),
      // Portless: it still counts toward the split, so the three total
      // the line's own events rather than quietly dropping any.
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', protocol: 'icmp' }),
    ])
    const totals = container.querySelector('.lcard .totals') as HTMLElement
    expect(totals.textContent?.replace(/\s+/g, ' ').trim()).toBe('tcp 1 · udp 1 · other 1')
  })

  it('names the rule that refused the line, and says so plainly when no event named one', () => {
    const named = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 22, protocol: 'tcp', action: 'drop', ruleLabel: '#17 default drop' }),
    ])
    expect(named.container.querySelector('.lcard .s.alarm')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(':22 refused by #17 default drop')
    named.unmount()

    const unnamed = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 22, protocol: 'tcp', action: 'drop', ruleLabel: '' }),
    ])
    expect(unnamed.container.querySelector('.lcard .s.alarm')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(':22 refused, no rule named')
  })

  it('offers `draft the rule ▸` only where something was refused', () => {
    const clean = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
    ])
    expect(clean.container.querySelector('.lcard')).not.toBeNull()
    expect(clean.container.querySelector('.lcard [data-draft-rule]')).toBeNull()
    clean.unmount()

    const refused = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 22, protocol: 'tcp', action: 'drop', ruleLabel: 'default drop' }),
    ])
    expect(refused.container.querySelector('.lcard [data-draft-rule]')).not.toBeNull()
  })

  it('pins, like every other card, and lets go when the pin is clicked again', () => {
    const { container } = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
    ])
    const pin = container.querySelector('.lcard .pin') as HTMLElement
    expect(pin.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(pin)
    flushSync()
    expect(container.querySelector('.lcard')?.classList.contains('pinned')).toBe(true)
    // Pinned, it survives the pointer leaving the road entirely.
    fireEvent.pointerLeave(container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element)
    flushSync()
    expect(container.querySelector('.lcard')).not.toBeNull()
    fireEvent.click(container.querySelector('.lcard .pin') as HTMLElement)
    flushSync()
    expect(container.querySelector('.lcard')?.classList.contains('pinned')).toBeFalsy()
  })

  it('goes through the shared placement, and is joined to its subject by a leader', () => {
    // The decision, not the pixels: this card asks lib/cardAnchor for a
    // position like every other card rather than placing itself, and it
    // is drawn joined to the thing it describes. Where it lands is
    // cardAnchor's own tested business, and jsdom measures everything as
    // zero anyway -- so the container is given a size, and nothing here
    // asserts a coordinate.
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
    ]
    const { container } = render(City, { props: { stop: 'street', ground } })
    const host = container.querySelector('.city') as HTMLElement
    const svg = container.querySelector('.city > svg') as SVGSVGElement
    const box = { left: 0, top: 0, width: 900, height: 900, right: 900, bottom: 900, x: 0, y: 0 }
    host.getBoundingClientRect = () => box as DOMRect
    svg.getBoundingClientRect = () => box as DOMRect
    const s = 900 / 1400
    ;(svg as unknown as { getScreenCTM: () => DOMMatrix }).getScreenCTM = () =>
      ({ a: s, b: 0, c: 0, d: s, e: 0, f: (900 - 700 * s) / 2 }) as DOMMatrix

    fireEvent.click(container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    fireEvent.pointerEnter(container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element)
    flushSync()

    const card = container.querySelector('.lcard') as HTMLElement
    expect(card, 'no line card opened').not.toBeNull()
    expect(card.classList.contains('placed'), 'the line card never got a measured position').toBe(true)
    expect(Number.isFinite(parseFloat(card.style.left)) && Number.isFinite(parseFloat(card.style.top))).toBe(true)
    expect(container.querySelector('svg.leader path'), 'no leader joined the card to its road').not.toBeNull()
  })

  it('opens no line card on a road the standing building does not own', () => {
    const { container } = openLine([
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 445, protocol: 'tcp' }),
    ])
    // wlan-wsh is one boundary over and nothing in the buffer names it,
    // so it fades and takes no pointer.
    expect(container.querySelector('[data-road-hot="bridge-lan|wlan-wsh"]')).toBeNull()
  })
})

describe('the reach follows the brightness rule (round 49, #1016)', () => {
  const LAN1 = 'bridge-lan/10.10.0.10'
  let nextId = 1
  const event = (over: Partial<ClientEvent> = {}): ClientEvent => ({
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
  })

  beforeEach(() => {
    matchMedia(true)
    appState.events = [
      event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge-lan', outInterface: 'vlan-srv', dstPort: 990, protocol: 'tcp' }),
    ]
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
  })

  afterEach(() => {
    appState.events = []
    baselineState.off = EMPTY_OFF_BASELINE
  })

  const stand = () => {
    const r = render(City, { props: { stop: 'street', ground } })
    fireEvent.click(r.container.querySelector('[data-cid="' + LAN1 + '"]') as Element)
    flushSync()
    return r
  }

  const road = (c: Element) => c.querySelector('path[data-road="bridge-lan|vlan-srv"]') as SVGPathElement

  it('an established road the standing building owns recedes: thinner, dimmer and still', () => {
    // Nothing off the baseline: its own road is established, and the
    // reach does not exempt it. Ownership decides what fades, not what
    // is bright -- DESIGN.md's "the same rule as everywhere else".
    const dim = stand()
    const dimOp = Number(road(dim.container).getAttribute('stroke-opacity'))
    const dimW = Number(road(dim.container).getAttribute('stroke-width'))
    expect(dim.container.querySelector('[data-road="bridge-lan|vlan-srv"].flow')).toBeNull()
    dim.unmount()

    baselineState.off = {
      config: { days: 3, of: 14 },
      generatedAt: 1,
      count: 1,
      lines: [{ key: 'l', srcIp: '10.10.0.10', dstIp: '10.20.0.10', port: 990, proto: 'tcp', count: 40, firstSeenToday: Date.now(), outcome: 'accept' as const }],
    }
    const bright = stand()
    expect(Number(road(bright.container).getAttribute('stroke-opacity'))).toBeGreaterThan(dimOp)
    expect(Number(road(bright.container).getAttribute('stroke-width'))).toBeGreaterThan(dimW)
    // Bright brings the flow dashes and the outline at the arrival end
    // (#1057: an outline on the arrived-at building, never a circle).
    expect(bright.container.querySelector('[data-road="bridge-lan|vlan-srv"].flow')).not.toBeNull()
    expect(bright.container.querySelector('.city path.halo.arrived')).not.toBeNull()
    expect(bright.container.querySelector('.city ellipse.halo, .city circle.halo')).toBeNull()
  })

  it('the standing building’s own lane takes part in the rule too, not the scenery ink', () => {
    const dim = stand()
    const laneOf = (c: Element) => c.querySelector('path[data-road="lane:' + LAN1 + '"]') as SVGPathElement | null
    const before = laneOf(dim.container)
    expect(before).not.toBeNull()
    const dimOp = Number(before!.getAttribute('stroke-opacity'))
    dim.unmount()

    baselineState.off = {
      config: { days: 3, of: 14 },
      generatedAt: 1,
      count: 1,
      lines: [{ key: 'l', srcIp: '10.10.0.10', dstIp: '10.20.0.10', port: 990, proto: 'tcp', count: 40, firstSeenToday: Date.now(), outcome: 'accept' as const }],
    }
    const bright = stand()
    expect(Number(laneOf(bright.container)!.getAttribute('stroke-opacity'))).toBeGreaterThan(dimOp)
  })
})

describe('clicking anything opens its reach (round 49, #1016)', () => {
  beforeEach(() => {
    matchMedia(true)
    appState.events = []
  })

  const cityStop = (c: Element) => c.querySelector('.city')?.getAttribute('data-stop')

  it('a district plate opens the district’s reach, and Esc surfaces to where you came from', () => {
    const { container } = render(City, { props: { stop: 'city', ground } })
    const before = container.querySelector('.mini rect.viewport')?.getAttribute('x')
    expect(container.querySelector('.crumb')).toBeNull()
    const plate = container.querySelector('.plate[data-cid="bridge-lan"]') as Element
    expect(plate).not.toBeNull()
    fireEvent.click(plate)
    flushSync()
    expect(cityStop(container)).toBe('district')
    const crumb = container.querySelector('.crumb') as HTMLElement
    expect(crumb).not.toBeNull()
    expect(crumb.textContent).toContain('Esc surfaces ▸')

    plate.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    flushSync()
    expect(cityStop(container)).toBe('city')
    expect(container.querySelector('.crumb')).toBeNull()
    expect(container.querySelector('.mini rect.viewport')?.getAttribute('x')).toBe(before)
  })

  it('a district’s reach fades every road that is not its own', () => {
    const { container } = render(City, { props: { stop: 'city', ground } })
    fireEvent.click(container.querySelector('.plate[data-cid="bridge-lan"]') as Element)
    flushSync()
    const own = container.querySelector('path[data-road="bridge-lan|vlan-srv"]') as SVGPathElement
    const other = container.querySelector('path[data-road="ether1|vlan-iot"]') as SVGPathElement
    expect(own).not.toBeNull()
    expect(other).not.toBeNull()
    expect(Number(own.getAttribute('stroke-opacity'))).toBeGreaterThan(Number(other.getAttribute('stroke-opacity')))
  })

  it('a road opens its own reach, and the crumb surfaces from it', async () => {
    baselineState.off = {
      config: { days: 3, of: 14 },
      generatedAt: 1,
      count: 1,
      lines: [
        { key: 'l', srcIp: '10.10.0.10', dstIp: '10.20.0.10', port: 990, proto: 'tcp', count: 40, firstSeenToday: Date.now(), outcome: 'accept' as const },
      ],
    }
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
    const { container } = render(City, { props: { stop: 'district', ground } })
    const hot = container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element
    expect(hot).not.toBeNull()
    await fireEvent.click(hot)
    flushSync()
    expect(cityStop(container)).toBe('street')
    expect(container.querySelector('.crumb')).not.toBeNull()
    // Only that road stays lit.
    const own = container.querySelector('path[data-road="bridge-lan|vlan-srv"]') as SVGPathElement
    const other = container.querySelector('path[data-road="bridge-lan|wlan-wsh"]') as SVGPathElement
    expect(Number(own.getAttribute('stroke-opacity'))).toBeGreaterThan(Number(other.getAttribute('stroke-opacity')))

    await fireEvent.click(container.querySelector('.crumb') as Element)
    flushSync()
    expect(cityStop(container)).toBe('district')
    baselineState.off = EMPTY_OFF_BASELINE
  })

  /** in:bridge-lan out:vlan-srv, one host each side. */
  function crossing(over: Partial<ClientEvent> = {}): ClientEvent {
    return {
      id: 1,
      time: '2026-09-03T12:00:00Z',
      receivedAt: Date.now(),
      deviceId: 'rb5009',
      sourceIp: '10.10.0.10',
      action: 'accept',
      ruleLabel: 'r',
      chain: 'forward',
      raw: '',
      srcIp: '10.10.0.10',
      dstIp: '10.20.0.10',
      inInterface: 'bridge-lan',
      outInterface: 'vlan-srv',
      protocol: 'tcp',
      dstPort: 445,
      ...over,
    }
  }

  it("a district's crumb states its own three counts (#1016)", () => {
    // The district's subject is its boundary interface, so the crumb
    // says the same three things a host's does rather than leaving them
    // out: one counterpart it reaches, one that reached it, one refused.
    appState.events = [
      crossing({ id: 1 }),
      crossing({ id: 2, srcIp: '10.20.0.10', dstIp: '10.10.0.10', inInterface: 'vlan-srv', outInterface: 'bridge-lan', dstPort: 12345 }),
      crossing({ id: 3, outInterface: 'wlan-wsh', dstIp: '10.30.0.10', action: 'drop', ruleLabel: 'default drop' }),
      // In and out through the district's own boundary: it crossed
      // nothing, so it is not one of its pathways.
      crossing({ id: 4, outInterface: 'bridge-lan', dstIp: '10.10.0.11' }),
    ]
    const { container } = render(City, { props: { stop: 'city', ground } })
    fireEvent.click(container.querySelector('.plate[data-cid="bridge-lan"]') as Element)
    flushSync()

    const crumb = container.querySelector('.crumb')!.textContent!.replace(/\s+/g, ' ').trim()
    expect(crumb).toContain('reaches 1')
    expect(crumb).toContain('reached by 1')
    expect(crumb).toContain('refused 1')
  })

  it("a road's crumb states the rib's counts, and its card lists the line (#1016)", async () => {
    appState.events = [
      crossing({ id: 1 }),
      crossing({ id: 2 }),
      crossing({ id: 3, dstPort: 22, action: 'drop', ruleLabel: 'default drop' }),
      // A different pair: not this rib's traffic, so not on its card.
      crossing({ id: 4, outInterface: 'wlan-wsh', dstIp: '10.30.0.10', dstPort: 8080 }),
    ]
    // A road is only pointable when it has something to say. Outside a
    // reach that is an off-baseline line, which is how the reader gets
    // to stand on it in the first place.
    baselineState.off = {
      config: { days: 3, of: 14 },
      generatedAt: 1,
      count: 1,
      lines: [
        { key: 'l', srcIp: '10.10.0.10', dstIp: '10.20.0.10', port: 990, proto: 'tcp', count: 40, firstSeenToday: Date.now(), outcome: 'accept' as const },
      ],
    }
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
    const { container } = render(City, { props: { stop: 'district', ground } })
    const hot = container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element
    expect(hot).not.toBeNull()
    await fireEvent.click(hot)
    flushSync()

    // A road has no address of its own, so the crumb names the pair and
    // states the counts, and nothing between them.
    const crumb = container.querySelector('.crumb')!.textContent!.replace(/\s+/g, ' ').trim()
    expect(crumb).toContain('reaches 1')
    expect(crumb).toContain('refused 1')

    // What a rib can say that the drawing cannot: the ports it carried.
    await fireEvent.pointerEnter(container.querySelector('[data-road-hot="bridge-lan|vlan-srv"]') as Element)
    flushSync()
    const card = container.querySelector('.lcard') as HTMLElement
    expect(card).not.toBeNull()
    const rows = [...card.querySelectorAll('table.ports tbody tr')].map((r) => [...r.querySelectorAll('td')].map((td) => td.textContent?.trim()))
    expect(rows).toEqual([
      ['445', 'tcp', '2', '—'],
      ['22', 'tcp', '—', '1'],
    ])
    baselineState.off = EMPTY_OFF_BASELINE
  })

  it('works from any city stop, not just one', () => {
    for (const stop of ['city', 'borough', 'district', 'street'] as const) {
      const r = render(City, { props: { stop, ground } })
      fireEvent.click(r.container.querySelector('.plate[data-cid="bridge-lan"]') as Element)
      flushSync()
      expect(r.container.querySelector('.crumb')).not.toBeNull()
      r.unmount()
    }
  })
})

describe('no height, no plinth (#986): buildings sit flat on their district plate', () => {
  it('never draws a height/importance toggle, at any stop', () => {
    const city = render(City, { props: { stop: 'city', ground } })
    expect(city.container.querySelector('.importance')).toBeNull()
    city.unmount()

    const district = render(City, { props: { stop: 'district', ground } })
    expect(district.container.querySelector('.importance')).toBeNull()
  })
})

describe('a released drag stays put (#975)', () => {
  // Deriving `ground` from the live stores, not the fixed `ground` const
  // above -- the bug only shows up when `ground` is City's own $derived
  // over appState.events (layoutGround(cityInputFrom(...))), which hands
  // back a fresh object on every event batch, not when a stable `ground`
  // prop is passed straight through.
  function router(over: Partial<Device> = {}): Device {
    return {
      id: 'router1',
      name: 'lab-crs',
      sourceIp: '10.0.0.1',
      configured: true,
      firstSeen: '2026-01-01T00:00:00Z',
      lastSeen: '2026-09-03T00:00:00Z',
      eventCount: 1,
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
      sourceIp: '10.10.0.10',
      action: 'accept',
      ruleLabel: '',
      chain: 'forward',
      raw: '',
      ...over,
    }
  }

  beforeEach(() => {
    matchMedia(true) // reduced motion: every camera move lands at once
    appState.devices = [router()]
    appState.filters = emptyFilters()
    zonesState.pushed = []
    policyState.byDevice = {}
    policyState.pushed = []
    topologyNavState.pendingDescend = null
    appState.events = [event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge1', outInterface: 'vlan-iot' })]
    // jsdom has no pointer-capture model; City only calls it to keep
    // receiving move/up events off the same target, which this
    // dispatch-directly-on-the-svg test does not need. Assigned through a
    // loose view of the prototype: `in` against Element.prototype narrows
    // it to `never`, so the direct assignment does not type-check.
    const elementProto = Element.prototype as unknown as Record<string, unknown>
    if (!('setPointerCapture' in elementProto)) {
      elementProto.setPointerCapture = function () {}
    }
  })

  afterEach(() => {
    appState.events = []
    appState.devices = []
  })

  it('does not snap back to the stop default when new traffic redraws the ground under a finished drag', () => {
    const { container } = render(City, { props: { stop: 'district' } })
    const svg = container.querySelector('svg') as unknown as EventTarget
    const viewportX = () => container.querySelector('.mini rect.viewport')?.getAttribute('x')
    const before = viewportX()

    // bubbles: true is load-bearing -- Svelte 5 delegates pointer events
    // to a listener up the tree rather than binding one on the svg
    // itself, so a non-bubbling synthetic event never reaches onPointerDown.
    svg.dispatchEvent(new PointerEvent('pointerdown', { clientX: 0, clientY: 0, button: 0, pointerId: 1, bubbles: true }))
    svg.dispatchEvent(new PointerEvent('pointermove', { clientX: 80, clientY: 50, pointerId: 1, bubbles: true }))
    svg.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    flushSync()
    const afterDrag = viewportX()
    expect(afterDrag).not.toBe(before) // sanity: the drag actually panned

    // A new event batch is exactly what a live instance's traffic feed
    // does continuously -- it must not be read as a request to re-centre.
    appState.events = [...appState.events, event({ srcIp: '10.10.0.10', dstIp: '10.20.0.10', inInterface: 'bridge1', outInterface: 'vlan-iot' })]
    flushSync()
    expect(viewportX()).toBe(afterDrag)
  })
})

// ---------------------------------------------------------------------
// Brightness by baseline (#1016, round 49). "Colour is the verdict;
// brightness is the baseline": an accepted road carrying nothing off
// today's pattern recedes, and one off-baseline line among a thousand
// established ones brings the whole road up.
//
// These test the decisions, not the pixels. jsdom lays nothing out --
// getBoundingClientRect answers zero for everything -- so there is no
// honest assertion to make here about where the card lands or how long a
// road is. What is checked is what the component decided: which opacity,
// whether a flow path exists, whether a ring exists, what the card says,
// and what the write path sends.
describe('City: brightness by baseline', () => {
  /** The first accepted road joining two districts that both draw a
   *  building, taken from the ground rather than hard-coded so the test
   *  keeps meaning something when the fixture moves. */
  function pairRoad() {
    for (const r of ground.roads) {
      if (r.lane || r.k !== 'a') continue
      const bar = r.id.indexOf('|')
      if (bar < 0) continue
      const a = ground.districts.find((d) => d.id === r.id.slice(0, bar))
      const b = ground.districts.find((d) => d.id === r.id.slice(bar + 1))
      if (a?.buildings.length && b?.buildings.length) return { r, a, b }
    }
    return null
  }

  const offDoc = (lines: OffBaselineLine[]) => ({
    config: { days: 3, of: 14 },
    generatedAt: 1,
    count: lines.length,
    lines,
  })

  const aLine = (srcIp: string, dstIp: string, o: Partial<OffBaselineLine> = {}): OffBaselineLine => ({
    key: 'line-1',
    srcIp,
    dstIp,
    port: 5001,
    proto: 'tcp',
    count: 40,
    firstSeenToday: Date.now(),
    outcome: 'accept',
    ...o,
  })

  beforeEach(() => {
    // The mount effect re-reads the register; stubbed so a stray fetch
    // cannot land after a test has set the document by hand.
    vi.spyOn(baselineState, 'refresh').mockResolvedValue(undefined)
  })

  afterEach(() => {
    baselineState.off = EMPTY_OFF_BASELINE
    baselineState.error = null
    authState.role = ''
    authState.username = ''
  })

  /** Every stroke-opacity the body of one road was drawn at. */
  const opacities = (container: Element, id: string) =>
    [...container.querySelectorAll(`path[data-road="${id}"]`)].map((p) => p.getAttribute('stroke-opacity'))

  it('draws a road bright when one line among its traffic is off the baseline, and dim when none is', () => {
    const pick = pairRoad()
    expect(pick).toBeTruthy()
    const { r, a, b } = pick!
    baselineState.off = offDoc([aLine(a.buildings[0].ip, b.buildings[0].ip)])
    const { container } = render(City, { props: { stop: 'district', ground } })

    // The one road that carried it is at full brightness.
    expect(opacities(container, r.id)).toContain('0.8')

    // Every other accepted road is established, and recedes -- it is
    // still drawn, which is the whole point: this is a sieve, not a
    // filter, and nothing is ever removed from the map.
    const others = ground.roads.filter((x) => x.k === 'a' && !x.lane && x.id !== r.id)
    expect(others.length).toBeGreaterThan(0)
    for (const o of others) {
      const ops = opacities(container, o.id)
      if (ops.length === 0) continue
      expect(ops).toContain('0.26')
      expect(ops).not.toContain('0.8')
    }
  })

  it('rolls up: the number of bright roads does not grow with the number of lines', () => {
    const pick = pairRoad()!
    // Two hundred distinct lines across the same pair of districts.
    const many = Array.from({ length: 200 }, (_, i) =>
      aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip, { key: `k${i}`, port: 5000 + i }),
    )
    baselineState.off = offDoc(many)
    const { container } = render(City, { props: { stop: 'district', ground } })
    const bright = ground.roads.filter((x) => opacities(container, x.id).includes('0.8'))
    expect(bright.map((x) => x.id)).toEqual([pick.r.id])
  })

  it('gives an off-baseline road flow dashes, and an established one none', () => {
    const pick = pairRoad()!
    baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip)])
    const { container } = render(City, { props: { stop: 'district', ground } })
    expect(container.querySelectorAll(`path.flow[data-road="${pick.r.id}"]`).length).toBeGreaterThan(0)
    const other = ground.roads.find((x) => x.k === 'a' && !x.lane && x.id !== pick.r.id)!
    expect(container.querySelectorAll(`path.flow[data-road="${other.id}"]`).length).toBe(0)
  })

  it('throbs the arrived-at building\u2019s own outline, and nothing when nothing is off the baseline', () => {
    const pick = pairRoad()!
    // Which end of this road each district sits at, asked of the same
    // function `rollUpRoads` asks, so the line below is aimed at the
    // road's `end` rather than at whichever district the fixture happens
    // to list first (#1057: a judged road with `ring.end`).
    const ends = roadEnds(pick.r, ground.districts)!
    const from = ground.districts.find((d) => d.id === ends.start)!
    const to = ground.districts.find((d) => d.id === ends.end)!

    const plain = render(City, { props: { stop: 'district', ground } })
    expect(plain.container.querySelectorAll('.halo.arrived').length).toBe(0)
    plain.unmount()

    baselineState.off = offDoc([aLine(from.buildings[0].ip, to.buildings[0].ip)])
    const { container } = render(City, { props: { stop: 'district', ground } })

    // The mark is the destination's own footprint outline, drawn inside
    // the building's group -- so it is on the building by construction,
    // not merely near it.
    const outlined = (b: { id: string }) => container.querySelector(`.blk[data-cid="${b.id}"] path.halo.arrived`)
    for (const b of to.buildings) expect(outlined(b)).not.toBeNull()
    // Nothing at the end the traffic left from.
    for (const b of from.buildings) expect(outlined(b)).toBeNull()
    // And no circle anywhere: the ring on the ground is gone, both the
    // <ellipse> the old mark drew and any <circle> standing in for it.
    expect(container.querySelector('.city ellipse.halo, .city circle.halo')).toBeNull()
    expect(container.querySelectorAll('circle.arrived, ellipse.arrived').length).toBe(0)
  })

  it('opens the off-baseline card from the road, naming the line, the port, the count and the verdict in plain words', async () => {
    authState.role = 'admin'
    authState.username = 'tom'
    const pick = pairRoad()!
    const src = pick.a.buildings[0]
    const dst = pick.b.buildings[0]
    baselineState.off = offDoc([aLine(src.ip, dst.ip)])
    const { container } = render(City, { props: { stop: 'district', ground } })

    const hot = container.querySelector(`path.road-hot[data-road-hot="${pick.r.id}"]`)
    expect(hot).toBeTruthy()
    await fireEvent.pointerEnter(hot!)
    flushSync()

    const card = container.querySelector('.bcard.rcard') as HTMLElement
    expect(card).toBeTruthy()
    expect(card.textContent).toContain('1 off the baseline today')
    // The threshold the server actually applied, not a hard-coded pair.
    expect(card.textContent).toContain('seen on 3 of the last 14 days')
    expect(card.textContent).toContain(`${src.name} → ${dst.name}`)
    expect(card.textContent).toContain('5001/tcp')
    expect(card.textContent).toContain('40')
    expect(card.textContent).toContain('the router accepted it; nothing decided it was wanted')
    expect(card.textContent).toContain('expected ▸')
    // The subject stays marked while its card is open.
    expect(hot!.classList.contains('on')).toBe(true)
    // No form until it is asked for.
    expect(card.querySelector('.form')).toBeNull()
  })

  it('will not open a card on an established road: its answer is the drawing', () => {
    const other = ground.roads.find((x) => x.k === 'a' && !x.lane)!
    const { container } = render(City, { props: { stop: 'district', ground } })
    expect(container.querySelector(`path.road-hot[data-road-hot="${other.id}"]`)).toBeNull()
    expect(container.querySelector('.bcard.rcard')).toBeNull()
  })

  it('writes `expected` through the register, with the reason required', async () => {
    authState.role = 'admin'
    authState.username = 'tom'
    const expected = vi.spyOn(baselineState, 'expected').mockResolvedValue(true)
    const pick = pairRoad()!
    baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip, { key: 'the-key' })])
    const { container } = render(City, { props: { stop: 'district', ground } })

    await fireEvent.pointerEnter(container.querySelector(`path.road-hot[data-road-hot="${pick.r.id}"]`)!)
    flushSync()
    await fireEvent.click(container.querySelector('.bcard.rcard .linkact') as HTMLElement)
    flushSync()

    const card = container.querySelector('.bcard.rcard') as HTMLElement
    expect(card.textContent).toContain('EXPECTED — WHY?')
    // Who it will be recorded as, and how far the statement reaches.
    expect(card.textContent).toContain('as tom · this line only')
    // A reason is required -- the button stays refused until there is one.
    const go = card.querySelector('.go') as HTMLButtonElement
    expect(go.disabled).toBe(true)
    await fireEvent.click(go)
    expect(expected).not.toHaveBeenCalled()

    const input = card.querySelector('.form input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'new backup job from the desktop to the nas' } })
    flushSync()
    await fireEvent.click(card.querySelector('.go') as HTMLButtonElement)
    await tick()

    expect(expected).toHaveBeenCalledWith('the-key', 'new backup job from the desktop to the nas')
  })

  it('surfaces a refused write beside the form rather than pretending it landed', async () => {
    authState.role = 'admin'
    authState.username = 'tom'
    vi.spyOn(baselineState, 'expected').mockImplementation(async () => {
      baselineState.error = 'a reason is required'
      return false
    })
    const pick = pairRoad()!
    baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip, { key: 'the-key' })])
    const { container } = render(City, { props: { stop: 'district', ground } })
    await fireEvent.pointerEnter(container.querySelector(`path.road-hot[data-road-hot="${pick.r.id}"]`)!)
    flushSync()
    await fireEvent.click(container.querySelector('.bcard.rcard .linkact') as HTMLElement)
    flushSync()
    const input = container.querySelector('.bcard.rcard .form input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'because' } })
    flushSync()
    await fireEvent.click(container.querySelector('.bcard.rcard .go') as HTMLButtonElement)
    await tick()
    flushSync()
    // The form stays open with the failure beside it.
    const card = container.querySelector('.bcard.rcard') as HTMLElement
    expect(card.textContent).toContain('a reason is required')
    expect(card.querySelector('.form')).toBeTruthy()
  })

  it('does not offer `expected` to a reader who cannot write', async () => {
    authState.role = ''
    const pick = pairRoad()!
    baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip)])
    const { container } = render(City, { props: { stop: 'district', ground } })
    await fireEvent.pointerEnter(container.querySelector(`path.road-hot[data-road-hot="${pick.r.id}"]`)!)
    flushSync()
    const card = container.querySelector('.bcard.rcard') as HTMLElement
    // The lines are still listed -- reading is not the privilege.
    expect(card.textContent).toContain('1 off the baseline today')
    expect(card.querySelector('.linkact')).toBeNull()
  })

  // Per-line is the default (owner, 2026-09-07). The same decisions the
  // 2D rib card is held to, asserted here on the city's own card,
  // because the two views are one product and this is the surface that
  // would drift.
  describe('per-line by default, the lot only on purpose (#1016)', () => {
    /** Three lines on the one road, so "one of them" is a real claim. */
    function threeOn(pick: NonNullable<ReturnType<typeof pairRoad>>) {
      const src = pick.a.buildings[0].ip
      const dst = pick.b.buildings[0].ip
      return [
        aLine(src, dst, { key: 'k1' }),
        aLine(src, dst, { key: 'k2', port: 22 }),
        aLine(src, dst, { key: 'k3', port: 445, outcome: 'drop' }),
      ]
    }

    /** A write that lands, and takes the line out of the register the
     * way the real endpoint plus its refresh does. */
    function acceptingWrites() {
      return vi.spyOn(baselineState, 'expected').mockImplementation(async (key: string) => {
        baselineState.off = offDoc(baselineState.off.lines.filter((l) => l.key !== key))
        return true
      })
    }

    async function openCard(container: Element, roadId: string) {
      await fireEvent.pointerEnter(container.querySelector(`path.road-hot[data-road-hot="${roadId}"]`)!)
      flushSync()
      return container.querySelector('.bcard.rcard') as HTMLElement
    }

    const card = (container: Element) => container.querySelector('.bcard.rcard') as HTMLElement
    const perLineActs = (c: Element) => [...c.querySelectorAll<HTMLButtonElement>('.linkact[data-expected-one]')]
    const bulkAct = (c: Element) => c.querySelector<HTMLButtonElement>('.allof .linkact.all')

    async function say(container: Element, reason: string) {
      await fireEvent.input(card(container).querySelector('.form input') as HTMLInputElement, { target: { value: reason } })
      flushSync()
      await fireEvent.click(card(container).querySelector('.form .go') as HTMLButtonElement)
      await tick()
      flushSync()
    }

    beforeEach(() => {
      authState.role = 'admin'
      authState.username = 'tom'
    })

    it('offers one expected ▸ per listed line, not one for the whole road', async () => {
      const pick = pairRoad()!
      const lines = threeOn(pick)
      baselineState.off = offDoc(lines)
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      expect(perLineActs(c).map((b) => b.dataset.expectedOne)).toEqual(['k1', 'k2', 'k3'])
    })

    it('marks only the line whose own action was clicked', async () => {
      const expected = acceptingWrites()
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      await fireEvent.click(perLineActs(c)[1])
      flushSync()
      await say(container, 'the new monitoring agent')

      expect(expected.mock.calls.map((a) => a[0])).toEqual(['k2'])
    })

    it('leaves the other lines bright, and the road bright, when one of three is spoken for', async () => {
      acceptingWrites()
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })
      expect(opacities(container, pick.r.id)).toContain('0.8')

      const c = await openCard(container, pick.r.id)
      await fireEvent.click(perLineActs(c)[0])
      flushSync()
      await say(container, 'the new backup job')

      // The two nobody has answered for are still listed, still offering
      // their own action, and the road is still lit.
      expect(perLineActs(card(container)).map((b) => b.dataset.expectedOne)).toEqual(['k2', 'k3'])
      expect(card(container).textContent).toContain('2 off the baseline today')
      expect(opacities(container, pick.r.id)).toContain('0.8')
    })

    it('lets the road go dim only once its last off-baseline line is spoken for', async () => {
      acceptingWrites()
      const pick = pairRoad()!
      baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip, { key: 'only' })])
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      await fireEvent.click(perLineActs(c)[0])
      flushSync()
      await say(container, 'the new backup job')

      expect(opacities(container, pick.r.id)).not.toContain('0.8')
      // Nothing was removed: the road is still drawn, it has just
      // stopped being lit.
      expect(container.querySelectorAll(`path[data-road="${pick.r.id}"]`).length).toBeGreaterThan(0)
    })

    it('offers the lot behind a separate control that says how many it will mark', async () => {
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      const bulk = bulkAct(await openCard(container, pick.r.id))!
      expect(bulk.textContent!.trim()).toBe('mark all 3 expected ▸')
      expect(bulk.dataset.expectedOne).toBeUndefined()
      expect(bulk.textContent!.trim()).not.toBe('expected ▸')
    })

    it('offers no bulk control when there is only one line -- there is no lot to accept', async () => {
      const pick = pairRoad()!
      baselineState.off = offDoc([aLine(pick.a.buildings[0].ip, pick.b.buildings[0].ip, { key: 'only' })])
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      expect(perLineActs(c)).toHaveLength(1)
      expect(bulkAct(c)).toBeNull()
    })

    it('writes the same reason against every line the bulk control covers', async () => {
      const expected = acceptingWrites()
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      await fireEvent.click(bulkAct(await openCard(container, pick.r.id))!)
      flushSync()
      await say(container, 'the whole road is the new replication link')

      expect(expected.mock.calls).toEqual([
        ['k1', 'the whole road is the new replication link'],
        ['k2', 'the whole road is the new replication link'],
        ['k3', 'the whole road is the new replication link'],
      ])
      expect(opacities(container, pick.r.id)).not.toContain('0.8')
    })

    it('says which it is: this line only, or all of them with the count', async () => {
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      await fireEvent.click(perLineActs(c)[0])
      flushSync()
      expect(card(container).querySelector('.form .who')!.textContent).toBe('as tom · this line only')
      expect(card(container).querySelector<HTMLInputElement>('.form input')!.placeholder).toBe('why this line is meant to be here…')

      await fireEvent.click(card(container).querySelector('.form .no') as HTMLButtonElement)
      flushSync()
      await fireEvent.click(bulkAct(card(container))!)
      flushSync()
      expect(card(container).querySelector('.form .who')!.textContent).toBe('as tom · all 3 of these lines')
      expect(card(container).querySelector<HTMLInputElement>('.form input')!.placeholder).toBe('why these 3 lines are meant to be here…')
    })

    it('opens one form at a time, under the line it belongs to', async () => {
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      await fireEvent.click(perLineActs(c)[1])
      flushSync()

      expect(card(container).querySelectorAll('.form')).toHaveLength(1)
      expect(perLineActs(card(container)).map((b) => b.dataset.expectedOne)).toEqual(['k1', 'k3'])
      expect(card(container).querySelector('tr.formrow .form')).not.toBeNull()
    })

    it('offers a reader neither control -- reading the lines is not the privilege', async () => {
      authState.role = ''
      const pick = pairRoad()!
      baselineState.off = offDoc(threeOn(pick))
      const { container } = render(City, { props: { stop: 'district', ground } })

      const c = await openCard(container, pick.r.id)
      expect(c.textContent).toContain('3 off the baseline today')
      expect(perLineActs(c)).toHaveLength(0)
      expect(bulkAct(c)).toBeNull()
    })
  })
})

describe('the drop card (#1002)', () => {
  beforeEach(() => {
    matchMedia(true)
    appState.events = []
  })

  // The fixture already draws two aggregate drop marks -- the holding
  // guest boundary and the one escalated unplanned pair. Only the guest
  // one is given a breakdown, so the same ground answers both halves of
  // the question: a mark with rules to name opens a card, and a mark
  // with none stays exactly as it was drawn.
  //
  // The counts go in deliberately out of order: "largest first" is the
  // card's own promise, so the card is what has to keep it.
  const GUEST_ROAD = 'bridge-lan|vlan-guest'
  function dropGround() {
    const input = mockupEstate()
    const guest = input.edges.find((e) => e.key === 'vlan-guest|bridge-lan')!
    guest.dropsByRule = [
      { rule: 'guest-isolation', count: 2 },
      { rule: null, count: 3 },
      { rule: 'lan-guard', count: 7 },
    ]
    return layoutGround(input)
  }

  const dropCard = (c: Element) => c.querySelector('.bcard.dcard') as HTMLElement | null

  it('opens from the aggregate drop mark, a row per refusing rule, busiest first, and a total that reconciles', async () => {
    const { container } = render(City, { props: { stop: 'district', ground: dropGround() } })
    const mark = container.querySelector(`[data-drop-hot="${GUEST_ROAD}"]`) as Element
    expect(mark).not.toBeNull()
    // The mark is the control, the way a district plate and a building
    // already are: a button in the keyboard order, not a new affordance.
    expect(mark.getAttribute('role')).toBe('button')
    expect(mark.getAttribute('tabindex')).toBe('0')
    // A mark with nothing to break down is left exactly as drawn --
    // there is no dead click on it.
    expect(container.querySelector('[data-drop-hot="bridge-lan|vlan-iot"]')).toBeNull()
    expect(dropCard(container)).toBeNull()

    // Hover opens it, as it does every other card on this surface.
    await fireEvent.pointerEnter(mark)
    flushSync()
    const card = dropCard(container) as HTMLElement
    expect(card).not.toBeNull()
    // The composer's own phrasing, so a refusal reads the same wherever
    // it is said.
    expect(card.textContent).toContain('Guest → LAN · refused at this wall')
    // One row per rule, largest first, and the composer's own words for
    // the drops that carried no rule at all.
    expect([...card.querySelectorAll('[data-drop-rule]')].map((r) => r.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      'lan-guard · 7',
      'caught, no rule named · 3',
      'guest-isolation · 2',
    ])
    // The footer reconciles with the mark: every drop the mark stands for.
    expect(card.querySelector('[data-drop-total]')?.textContent).toContain('12')

    // Every card pins (DESIGN.md "Cards").
    await fireEvent.click(card.querySelector('.pin') as HTMLElement)
    flushSync()
    expect(dropCard(container)?.classList.contains('pinned')).toBe(true)
  })

  it('takes the card down before it surfaces from standing', async () => {
    const { container } = render(City, { props: { stop: 'district', ground: dropGround() } })
    // Activating the mark pins its card, so the card is still open when
    // the next click stands somewhere.
    await fireEvent.click(container.querySelector(`[data-drop-hot="${GUEST_ROAD}"]`) as Element)
    flushSync()
    expect(dropCard(container)).not.toBeNull()

    await fireEvent.click(container.querySelector('.plate[data-cid="bridge-lan"]') as Element)
    flushSync()
    expect(container.querySelector('.crumb')).not.toBeNull()

    // First Escape is the card's; standing is untouched.
    key(document.body, 'Escape')
    expect(dropCard(container)).toBeNull()
    expect(container.querySelector('.crumb')).not.toBeNull()

    // Second Escape surfaces, exactly as it did before there was a card.
    key(document.body, 'Escape')
    expect(container.querySelector('.crumb')).toBeNull()
  })
})

// #1055 (round 54): round 53's port filter on this surface. The rule is
// round 53's own and does not change with the camera -- nothing is added
// to the map for the filter, the map dims to the answer -- so these
// mirror the flat map's own cases in Topography.svelte.test.ts and check
// the city's own three additions: the tally under a plaque, the door in
// a gate, and the one line under the city.
describe('the port filter on the city (#1055, round 54)', () => {
  beforeEach(() => {
    portFilterState.clear()
    portFilterState.proto = 'tcp'
  })
  afterEach(() => portFilterState.clear())

  /** Drives the store the way a landed fetch would, which is how every
   * other store in this file is driven. 445/tcp between LAN and Servers:
   * one host each side of it, and nothing else on the port. */
  function filterTo(ports: number[], answer: Partial<(typeof portFilterState)['answer']> = {}) {
    portFilterState.ports = ports
    portFilterState.answer = {
      generatedAt: 1,
      windowSeconds: 3600,
      candidates: [],
      events: 2,
      accepts: 2,
      drops: 0,
      lines: 2,
      ribs: [{ in: 'bridge-lan', out: 'vlan-srv', events: 2, accepts: 2, drops: 0 }],
      hosts: [
        { ip: '10.10.0.10', name: 'lan-1', events: 2, accepts: 2, drops: 0 },
        { ip: '10.20.0.10', name: 'srv-1', events: 2, accepts: 2, drops: 0 },
      ],
      doors: [],
      ...answer,
    }
    portFilterState.answeredKey = portFilterState.key
  }

  function door(overrides: Partial<(typeof portFilterState)['doors'][number]> = {}) {
    return {
      device: 'rb5009',
      label: '#12',
      ordinal: 12,
      action: 'accept',
      chain: 'forward',
      in: 'bridge-lan',
      out: 'vlan-srv',
      dstPort: '445',
      who: 'bridge-lan → vlan-srv accept',
      ...overrides,
    }
  }

  const road = (c: HTMLElement, id: string) => c.querySelector(`path[data-road="${id}"]`)
  /** A building is dim when its stamp takes the faded opacity -- the one
   * visual word this file uses for "not what matters right now". */
  const dimmed = (c: HTMLElement, id: string) => c.querySelector(`.blk[data-cid="${id}"] g[opacity="0.62"]`) !== null

  it('keeps the roads the port travelled in their verdict ink and greys the rest, removing none', () => {
    filterTo([445])
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    // Both halves of the crossing are one road here, and it keeps the
    // colour it already had.
    expect(road(container, 'bridge-lan|vlan-srv')?.getAttribute('stroke')).toBe('var(--accept)')
    // Nothing is taken off the city: the unplanned pair is still drawn,
    // grey and faint rather than gone.
    const off = road(container, 'bridge-lan|vlan-iot')
    expect(off).not.toBeNull()
    expect(off?.getAttribute('stroke')).toBe('var(--fg-dim)')
    expect(Number(off?.getAttribute('stroke-opacity'))).toBeLessThan(0.2)
  })

  it('dims the hosts off the port and leaves a district with none in outline', () => {
    filterTo([445])
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    expect(dimmed(container, 'bridge-lan/10.10.0.10')).toBe(false)
    expect(dimmed(container, 'vlan-srv/10.20.0.10')).toBe(false)
    // A lane-mate that was not on the port recedes inside a lit district.
    expect(dimmed(container, 'bridge-lan/10.10.0.11')).toBe(true)
    // A district with nothing on the port goes to outline -- every one
    // of its buildings at once, which is the same word.
    for (const b of ground.districts.find((d) => d.id === 'vlan-iot')!.buildings) {
      expect(dimmed(container, b.id)).toBe(true)
    }
  })

  it('writes the tally under each district plaque, counted over the whole subnet', () => {
    filterTo([445])
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    const chips = [...container.querySelectorAll('.flat .chip-t')].map((t) => t.textContent)
    // LAN draws six of its nine hosts (layout.ts's MAX_BUILDINGS); the
    // tally is about the district, not about the six.
    expect(chips).toContain('1 of 9 · 445/tcp')
    expect(chips).toContain('1 of 4 · 445/tcp')
    expect(chips).toContain('0 of 5 · 445/tcp')
  })

  // #1056, from #1055's own live capture: the Servers plaque read `1 of
  // 0 · 445/tcp` because the machine that received the lines had never
  // been registered, so the district drew no building for it while the
  // answer's host list still counted it.
  it('leaves a host it draws no building for out of the tally, and still lights its road', () => {
    filterTo([445], {
      hosts: [{ ip: '10.20.0.99', name: '', events: 2, accepts: 2, drops: 0 }],
    })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    const chips = [...container.querySelectorAll('.flat .chip-t')].map((t) => t.textContent)
    // Servers has four known hosts and 10.20.0.99 is none of them.
    expect(chips).toContain('0 of 4 · 445/tcp')
    expect(chips).not.toContain('1 of 4 · 445/tcp')
    expect(chips.some((t) => /^1 of 0/.test(t ?? ''))).toBe(false)
    // The road is what says the traffic was there, and it still does.
    expect(road(container, 'bridge-lan|vlan-srv')?.getAttribute('stroke')).toBe('var(--accept)')
  })

  it('stands a door in the gate the rule crosses, and on the bridge deck for a WAN rule', () => {
    filterTo([445], {
      doors: [
        door(),
        door({ label: '#23', ordinal: 23, action: 'drop', in: 'ether1', out: '', who: 'ether1 → any drop' }),
        door({ label: '#31', ordinal: 31, action: 'drop', in: 'vlan-guest', who: 'vlan-guest → vlan-srv drop' }),
      ],
    })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    const doors = [...container.querySelectorAll('.door')]
    // An accept acts on the way out, so its door is in the Servers wall
    // at the gate the LAN road enters; a refusal acts on the way in, so
    // the WAN drop stands on the bridge deck and Guest's on its own
    // wall. Sorted, because a door is painted at its own depth like
    // everything else here, not in the order the rules were read.
    expect(doors.map((d) => d.getAttribute('data-door')).sort()).toEqual([
      'bridge:ether1',
      'vlan-guest:vlan-srv',
      'vlan-srv:bridge-lan',
    ])
    // A leaf that swings open for an accept, a bar across for a refusal.
    const shut = new Map(doors.map((d) => [d.getAttribute('data-door'), d.classList.contains('shut')]))
    expect(shut.get('vlan-srv:bridge-lan')).toBe(false)
    expect(shut.get('bridge:ether1')).toBe(true)
    expect(shut.get('vlan-guest:vlan-srv')).toBe(true)
    expect([...container.querySelectorAll('.door-t')].map((t) => t.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      '#12 accept',
      '#23 drop',
      '#31 drop',
    ])

    // And it really is on that wall: the door's posts stand inside the
    // Servers plate's own footprint, not somewhere plausible nearby.
    const plate = container.querySelector('.plate[data-cid="vlan-srv"] path')!.getAttribute('d')!
    const nums = [...plate.matchAll(/-?\d+(?:\.\d+)?/g)].map((m) => Number(m[0]))
    const xs = nums.filter((_, i) => i % 2 === 0)
    const ys = nums.filter((_, i) => i % 2 === 1)
    const srvDoor = doors.find((d) => d.getAttribute('data-door') === 'vlan-srv:bridge-lan')!
    const post = srvDoor.querySelector('path')!.getAttribute('d')!
    const [, px, py] = /^M(-?[\d.]+) (-?[\d.]+)/.exec(post)!.map(Number)
    expect(px).toBeGreaterThanOrEqual(Math.min(...xs) - 4)
    expect(px).toBeLessThanOrEqual(Math.max(...xs) + 4)
    // The posts stand up out of the plate, so only the foot is inside it.
    expect(py).toBeGreaterThanOrEqual(Math.min(...ys) - 4)
    expect(py).toBeLessThanOrEqual(Math.max(...ys) + 4)
  })

  it('says nothing was seen in one line under the city, and still draws the door', () => {
    filterTo([3389], {
      events: 0,
      accepts: 0,
      drops: 0,
      lines: 0,
      ribs: [],
      hosts: [],
      doors: [door({ label: '#23', ordinal: 23, action: 'drop', in: 'ether1', out: '', who: 'ether1 → any drop' })],
    })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    expect(container.querySelector('.note-t')?.textContent).toBe(
      'no logged traffic on 3389/tcp in the window · one rule names it — #23 ether1 → any drop, the door on the ether1 side',
    )
    expect(container.querySelectorAll('.door').length).toBe(1)
    // Not an empty state: the city is still there behind the sentence.
    expect(container.querySelectorAll('.plate').length).toBe(ground.districts.length)
  })

  it('gets out of the way when the operator stands on something', async () => {
    filterTo([445])
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()
    const tallies = () => [...container.querySelectorAll('.flat .chip-t')].filter((t) => t.textContent?.includes('445/tcp'))
    expect(tallies().length).toBeGreaterThan(0)

    await fireEvent.click(container.querySelector('.plate[data-cid="bridge-lan"]') as Element)
    flushSync()
    expect(portFilterState.active).toBe(false)
    expect(tallies().length).toBe(0)
  })

  it('clears on Esc, before it surfaces from standing', () => {
    filterTo([445])
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    key(document.body, 'Escape')
    expect(portFilterState.active).toBe(false)
    expect(container.querySelector('.plate[data-cid="vlan-iot"]')).not.toBeNull()
  })
})

describe('the event trace on the city (#1050, rounds 54 & 56)', () => {
  afterEach(() => mapTraceState.clear())

  const road = (c: HTMLElement, id: string) => c.querySelector(`path[data-road="${id}"]`)

  /** Drives the store the way a landed fetch would, mirroring the port
   * filter's own `filterTo` above. */
  function openTrace(event: FirewallEvent, verdict: TraceResponse['verdict'], answer: Partial<TraceResponse> = {}) {
    mapTraceState.request = { in: event.inInterface, out: event.outInterface || undefined, port: event.dstPort, proto: event.protocol }
    mapTraceState.result = { found: true, verdict, event, like: 0, srcSeen: 0, dstReached: 0, ...answer }
  }

  /** cam-porch → tom-desktop in the mockup's own words: iot-1 → lan-1,
   * refused at the LAN wall with an out-interface named -- round 56's
   * C1 gate case. Overridable for the other two shapes. */
  function traceEvent(overrides: Partial<FirewallEvent> = {}): FirewallEvent {
    return {
      id: 1,
      time: '2026-09-09T22:04:31Z',
      deviceId: 'rb5009',
      sourceIp: '10.10.0.1',
      action: 'drop',
      ruleLabel: '17',
      ruleName: '#17 default drop',
      chain: 'forward',
      inInterface: 'vlan-iot',
      outInterface: 'bridge-lan',
      protocol: 'tcp',
      srcIp: '10.30.0.10',
      dstIp: '10.10.0.10',
      dstPort: 445,
      srcHostName: 'iot-1',
      dstHostName: 'lan-1',
      raw: '',
      ...overrides,
    }
  }

  /** The plate's own footprint, the same bounding-box technique the
   * port filter's door test above uses to prove a door stands on the
   * wall it claims to. */
  function plateBox(c: HTMLElement, id: string) {
    const d = c.querySelector(`.plate[data-cid="${id}"] path`)!.getAttribute('d')!
    const nums = [...d.matchAll(/-?\d+(?:\.\d+)?/g)].map((m) => Number(m[0]))
    const xs = nums.filter((_, i) => i % 2 === 0)
    const ys = nums.filter((_, i) => i % 2 === 1)
    return { x0: Math.min(...xs) - 4, x1: Math.max(...xs) + 4, y0: Math.min(...ys) - 4, y1: Math.max(...ys) + 4 }
  }

  function stopPoint(c: HTMLElement): { x: number; y: number } | null {
    const t = c.querySelector('.trace-stop')?.getAttribute('transform')
    if (!t) return null
    const m = /translate\((-?[\d.]+) (-?[\d.]+)\)/.exec(t)!
    return { x: Number(m[1]), y: Number(m[2]) }
  }

  it('keeps the traced pair’s own road in the verdict colour and dims the rest, removing none (refused)', () => {
    openTrace(traceEvent(), 'refused', { srcSeen: 14, dstReached: 0 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    expect(road(container, 'bridge-lan|vlan-iot')?.getAttribute('stroke')).toBe('var(--alarm)')
    // Nothing is taken off the city: an unrelated pair is still drawn,
    // grey and faint rather than gone.
    const off = road(container, 'bridge-lan|vlan-srv')
    expect(off).not.toBeNull()
    expect(off?.getAttribute('stroke')).toBe('var(--fg-dim)')
    expect(Number(off?.getAttribute('stroke-opacity'))).toBeLessThan(0.2)
  })

  it('stands the ✕ at the destination’s own gate when the log names an out-interface (C1), with the ghost on to the host', () => {
    openTrace(traceEvent(), 'refused', { srcSeen: 14, dstReached: 0 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    const stop = stopPoint(container)
    expect(stop).not.toBeNull()
    const box = plateBox(container, 'bridge-lan')
    expect(stop!.x).toBeGreaterThanOrEqual(box.x0)
    expect(stop!.x).toBeLessThanOrEqual(box.x1)

    // Somewhere left to draw a ghost to, unlike the router-door case.
    expect(container.querySelector('.trace-ghost')).not.toBeNull()
    expect(container.querySelector('.ghost-t')?.textContent).toBe('would have reached lan-1 · stopped at the LAN wall')
    // The sender's own halo only -- a refusal draws no ring at the far
    // end, which would say "never reached" a second time.
    expect(container.querySelectorAll('circle.halo').length).toBe(1)
  })

  it('stands the ✕ on the router’s own door when the log names no out-interface, with no ghost', () => {
    const event = traceEvent({
      inInterface: 'ether1',
      outInterface: '',
      srcIp: '203.0.113.7',
      dstIp: '10.10.0.1',
      srcHostName: undefined,
      dstHostName: 'rb5009',
      ruleLabel: '1',
      ruleName: '#1 input drop',
    })
    openTrace(event, 'refused', { srcSeen: 0, dstReached: 0 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    // The WAN bridge is the only drawable "in road" for a boundary with
    // no out-interface (the city draws none from a plain district
    // straight to its own router -- portOverlay's own note).
    expect(road(container, 'rb-wan')?.getAttribute('stroke')).toBe('var(--alarm)')
    expect(road(container, 'wan-span')?.getAttribute('stroke')).toBe('var(--alarm)')
    expect(container.querySelector('.trace-stop')).not.toBeNull()
    expect(container.querySelector('.trace-ghost')).toBeNull()
    expect(container.querySelector('.chip-verdict.refused')?.textContent).toContain('REFUSED')
  })

  it('keeps both roads green with flow and rings the end it reached (accepted)', () => {
    const event = traceEvent({
      inInterface: 'bridge-lan',
      outInterface: 'vlan-srv',
      srcIp: '10.10.0.10',
      dstIp: '10.20.0.10',
      srcHostName: 'lan-1',
      dstHostName: 'srv-1',
      action: 'accept',
      ruleLabel: '12',
      ruleName: '#12 accept',
    })
    openTrace(event, 'accepted', { srcSeen: 3, dstReached: 12 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    expect(road(container, 'bridge-lan|vlan-srv')?.getAttribute('stroke')).toBe('var(--accept)')
    expect(container.querySelector('[data-road="bridge-lan|vlan-srv"].flow')).not.toBeNull()
    expect(container.querySelector('.trace-stop')).toBeNull()
    expect(container.querySelector('.trace-ghost')).toBeNull()
    // The sender's halo and the ring at the end it reached.
    expect(container.querySelectorAll('circle.halo').length).toBe(2)
    expect(container.querySelector('.chip-verdict')?.classList.contains('refused')).toBe(false)
  })

  it('carries the trace’s own tallies under each end’s district plaque', () => {
    openTrace(traceEvent(), 'refused', { srcSeen: 14, dstReached: 0 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    const chips = [...container.querySelectorAll('.flat .chip-t')].map((t) => t.textContent)
    expect(chips).toContain('iot-1 · 14× in the window')
    expect(chips).toContain('lan-1 · never reached')
  })

  it('clears on Esc, the same rung the port filter takes', () => {
    openTrace(traceEvent(), 'refused', { srcSeen: 14, dstReached: 0 })
    const { container } = render(City, { props: { stop: 'district', ground } })
    flushSync()

    key(document.body, 'Escape')
    expect(mapTraceState.active).toBe(false)
    expect(container.querySelector('.plate[data-cid="vlan-iot"]')).not.toBeNull()
  })
})
