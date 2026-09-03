// SPDX-License-Identifier: AGPL-3.0-only
//
// #863: the city as a component. The ground model has its own tests
// under lib/city; this covers what only the component does -- names on
// every district and building, the keyboard walk, and the camera
// landing at once when the reader asked for reduced motion.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync, tick } from 'svelte'
import { mockupEstate } from '../lib/city/fixture'
import { layoutGround } from '../lib/city/layout'
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
