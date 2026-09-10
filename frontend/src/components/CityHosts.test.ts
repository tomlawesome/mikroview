// SPDX-License-Identifier: AGPL-3.0-only
//
// Living hosts in the city (round 49, #1016): hosts drawn as buildings
// at every stop, what each presence looks like, the two overlay pills on
// a building, and the host card's two marks.
//
// The ground model's own share of this is in lib/city/presence.test.ts;
// this covers only what the component does with it.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { mockupEstate } from '../lib/city/fixture'
import { layoutGround } from '../lib/city/layout'
import { bufferHost, mergeZoneHosts, type CityHost } from '../lib/city/presence'
import { CARD_GRACE_MS } from '../lib/cardAnchor'
import { hostsState } from '../lib/hosts.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { authState } from '../lib/auth.svelte'
import type { Ground } from '../lib/city/types'
import City from './City.svelte'

// The same 20000ms City.svelte.test.ts sets, and for the same reason:
// the cost is jsdom's, building the wide SVG this component renders.
vi.setConfig({ testTimeout: 20000 })

const H = 3_600_000
const NOW = Date.parse('2026-09-07T12:00:00Z')

function host(ip: string, label: string, over: Partial<CityHost> = {}): CityHost {
  return {
    ...bufferHost(label, ip),
    key: 'ether1|' + ip,
    lastSeen: new Date(NOW).toISOString(),
    firstSeen: '2026-08-03T09:00:00Z',
    events: 1204,
    ...over,
  }
}

const QUIET = host('10.10.0.10', 'tv-lounge', { presence: 'quiet', lastSeen: new Date(NOW - 26 * H).toISOString() })
const INTENDED = host('10.10.0.11', 'printer-old', { presence: 'intended', reason: 'switched off at the wall', markedBy: 'tom', markedAt: '2026-09-01T14:20:00Z' })
const LIVE = host('10.10.0.12', 'tom-desktop')

/** The mockup's estate with the LAN district's hosts replaced, so one
 * district carries one of each presence. The hosts go through
 * mergeZoneHosts, which is the path cityInputFrom takes -- so a
 * dismissed host is dropped here for the same reason it is dropped in
 * the app, rather than by this helper knowing the rule separately. */
function groundWith(hosts: CityHost[]): Ground {
  const input = mockupEstate()
  const lan = input.zones.find((z) => z.id === 'bridge-lan')!
  lan.hosts = mergeZoneHosts([], hosts)
  lan.hostCount = lan.hosts.length
  return layoutGround(input)
}

const ground = groundWith([LIVE, QUIET, INTENDED])

/** The <g class="blk"> a host was drawn into, by its name. */
function blk(container: Element, name: string): Element {
  const el = [...container.querySelectorAll('.blk[role="button"]')].find((g) => (g.getAttribute('aria-label') ?? '').startsWith(name))
  if (!el) throw new Error('no building drawn for ' + name)
  return el
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  hostsState.hosts = []
  hostsState.error = null
  flagsState.list = []
  watchlistState.entries = []
  authState.role = 'admin'
  authState.username = 'tom'
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  hostsState.hosts = []
  flagsState.list = []
  watchlistState.entries = []
  authState.role = ''
})

describe('hosts on the map', () => {
  // DESIGN.md "Living hosts": hosts are drawn at every stop, including
  // the top-level map. The city stop used to show only the big devices.
  it('draws hosts as buildings at the top-level city stop', () => {
    const { container } = render(City, { props: { stop: 'city', ground } })
    expect(blk(container, 'tom-desktop')).toBeTruthy()
    expect(blk(container, 'tv-lounge')).toBeTruthy()
  })

  it('draws them at the street stop too', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    expect(blk(container, 'tom-desktop')).toBeTruthy()
  })

  // Per-host labels are placed by camera (DESIGN.md "Labels"): the city
  // stop carries district, router and subnet names only.
  it('writes no per-host label at the city stop, and does at the street stop', () => {
    const city = render(City, { props: { stop: 'city', ground } })
    expect(city.container.querySelectorAll('.st-name')).toHaveLength(0)
    city.unmount()
    const street = render(City, { props: { stop: 'street', ground } })
    expect(street.container.querySelectorAll('.st-name').length).toBeGreaterThan(0)
  })
})

describe('presence, on a building', () => {
  it('draws a live host normally: no dashes, full ink', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    const g = blk(container, 'tom-desktop')
    expect(g.getAttribute('data-presence')).toBe('live')
    expect([...g.querySelectorAll('path')].some((p) => p.getAttribute('stroke-dasharray') === '3 3')).toBe(false)
  })

  // "It goes grey with a dashed footprint and says how long, e.g.
  // `quiet · 26 h`" -- DESIGN.md "Living hosts".
  it('draws a quiet host grey, with a dashed footprint', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    const g = blk(container, 'tv-lounge')
    expect(g.getAttribute('data-presence')).toBe('quiet')
    const footprint = [...g.querySelectorAll('path')].find((p) => p.getAttribute('stroke-dasharray') === '3 3')
    expect(footprint).toBeTruthy()
    expect(footprint!.getAttribute('stroke')).toBe('var(--fg-dim)')
  })

  it('says how long it has been quiet, in place of its address', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    const labels = [...container.querySelectorAll('.st-ip')].map((t) => t.textContent)
    expect(labels).toContain('quiet · 26 h')
  })

  // "The operator can mark it quiet on purpose, which draws it white
  // translucent" -- and white is not dashed: somebody said something, so
  // nothing is missing.
  it('draws a host marked quiet on purpose white and translucent, never dashed', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    const g = blk(container, 'printer-old')
    expect(g.getAttribute('data-presence')).toBe('intended')
    expect([...g.querySelectorAll('path')].some((p) => p.getAttribute('stroke-dasharray') === '3 3')).toBe(false)
    expect([...g.querySelectorAll('path')].some((p) => p.getAttribute('stroke') === 'var(--fg)')).toBe(true)
  })

  it('says quiet on purpose under its name', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    expect([...container.querySelectorAll('.st-ip')].map((t) => t.textContent)).toContain('quiet on purpose')
  })

  // A dismissal is the one state that removes a building. Nothing else
  // does -- a quiet host is never removed.
  it('draws no building at all for a dismissed host', () => {
    const g = groundWith([LIVE, host('10.10.0.13', 'gone-away', { presence: 'dismissed' })])
    const { container } = render(City, { props: { stop: 'street', ground: g } })
    expect(blk(container, 'tom-desktop')).toBeTruthy()
    expect(() => blk(container, 'gone-away')).toThrow()
  })

  it('tells a screen reader what a quiet building is, not only how it is drawn', () => {
    const { container } = render(City, { props: { stop: 'street', ground } })
    expect(blk(container, 'tv-lounge').getAttribute('aria-label')).toContain('quiet · 26 h')
  })
})

describe('the marks on a building (#981, round 46)', () => {
  // A mark exists only while there is something behind it, and there is
  // no toggle -- owner, 2026-09-08: "something that's always there is
  // easy to ignore; if it's not always there you know it's there for a
  // reason." So every mark here is driven by the host's own counts, and
  // an unmarked neighbour is the control in each case.
  const PLAIN = host('10.10.0.13', 'nas')
  const marked = (over: Partial<CityHost>) => groundWith([host('10.10.0.12', 'tom-desktop', over), PLAIN])

  /** The widest this path reaches from the symbol's own origin -- how a
   * silhouette pushed outward is told from the one it was pushed off. */
  const reach = (d: string) => Math.max(...(d.match(/-?\d*\.?\d+/g) ?? []).map((n) => Math.abs(Number(n))))

  it('re-stamps a flagged building in alarm ink and strokes one silhouette round it', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 1 }) } })
    const g = blk(container, 'tom-desktop')

    const fill = g.querySelector('.mk-fill')
    expect(fill).toBeTruthy()
    expect(fill!.getAttribute('opacity')).toBe('0.45')

    // ONE line round the silhouette, not a stroke per face: the owner on
    // the first attempt, "These overlapping planes are weird ... I can't
    // even tell what you're trying to show me."
    const rims = g.querySelectorAll('path.mk-rim')
    expect(rims.length).toBe(1)
    expect(rims[0].getAttribute('stroke')).toBe('var(--alarm)')
    expect(rims[0].getAttribute('stroke-width')).toBe('1')

    // And the neighbour with nothing behind it wears nothing at all.
    expect(blk(container, 'nas').querySelector('.mark')).toBeNull()
  })

  it('makes the fill more solid and the rim bolder the more flags there are', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 3 }) } })
    const g = blk(container, 'tom-desktop')
    expect(g.querySelector('.mk-fill')!.getAttribute('opacity')).toBe('0.75')
    expect(g.querySelector('path.mk-rim')!.getAttribute('stroke-width')).toBe('2')
  })

  it('strokes a watched building once in the watcher ink', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ watch: 1 }) } })
    const g = blk(container, 'tom-desktop')

    const watch = g.querySelectorAll('path.mk-watch')
    expect(watch.length).toBe(1)
    expect(watch[0].getAttribute('stroke')).toBe('var(--marked)')
    expect(watch[0].getAttribute('stroke-width')).toBe('1.5')
    // Watched is not flagged: no red anywhere on it.
    expect(g.querySelector('.mk-fill')).toBeNull()
    expect(g.querySelector('path.mk-rim')).toBeNull()
  })

  it('puts the watch line outside the flag rim when a building carries both', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 1, watch: 1 }) } })
    const g = blk(container, 'tom-desktop')

    const rim = g.querySelector('path.mk-rim')!
    const watch = g.querySelector('path.mk-watch')!
    expect(g.querySelectorAll('path.mk-rim, path.mk-watch').length).toBe(2)
    // Side by side, never on top of one another.
    expect(reach(watch.getAttribute('d')!)).toBeGreaterThan(reach(rim.getAttribute('d')!))
  })

  it('breathes the rim of an activity spike, and only of an activity spike', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 2, spike: true }) } })
    const spiking = blk(container, 'tom-desktop')
    expect(spiking.querySelector('path.mk-rim')!.classList.contains('mk-spike-rim')).toBe(true)
    // The glow is a second, blurred copy of the same line.
    expect(spiking.querySelectorAll('path.mk-spike-glow').length).toBe(1)

    const still = render(City, { props: { stop: 'street', ground: marked({ flags: 2 }) } })
    const plainRim = blk(still.container, 'tom-desktop').querySelector('path.mk-rim')!
    expect(plainRim.classList.contains('mk-spike-rim')).toBe(false)
    expect(still.container.querySelectorAll('path.mk-spike-glow').length).toBe(0)
  })

  // "I said to remove these rings with numbers. Get rid of them
  // completely" -- owner, on the round's first crops. No disc, ring,
  // number, glyph or tally on the map, at any stop.
  it('draws no disc, ring or number on a marked building', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 2, watch: 1, spike: true }) } })
    const g = blk(container, 'tom-desktop')
    expect(g.querySelector('circle')).toBeNull()
    expect(g.querySelector('text')).toBeNull()
  })

  it('tells a screen reader what the mark means, not only how it is drawn', () => {
    const { container } = render(City, { props: { stop: 'street', ground: marked({ flags: 2, watch: 1, spike: true }) } })
    const aria = blk(container, 'tom-desktop').getAttribute('aria-label') ?? ''
    expect(aria).toContain('2 flags')
    expect(aria).toContain('watched')
    expect(aria).toContain('activity spike')
  })
})

describe('the host card', () => {
  async function open(name: string, g: Ground = ground) {
    const r = render(City, { props: { stop: 'street', ground: g } })
    await fireEvent.pointerEnter(blk(r.container, name))
    flushSync()
    const card = r.container.querySelector('.hcard')
    if (!card) throw new Error('no host card opened for ' + name)
    return { ...r, card }
  }

  it('opens on hover', async () => {
    const { card } = await open('tv-lounge')
    expect(card.getAttribute('role')).toBe('dialog')
  })

  // The grace period is cardAnchor's, shared with the boundary card and
  // the 2D map (#1027), so the two surfaces cannot drift apart on how
  // long the pointer has to get from a building to its own card. The
  // placement itself cannot be asserted here: jsdom lays nothing out, so
  // stageRect finds no stage and the card stays at its unplaced
  // position, exactly as the boundary card does.
  it('survives the pointer travelling to it, and closes once it does not arrive', async () => {
    const { container } = await open('tv-lounge')
    await fireEvent.pointerLeave(blk(container, 'tv-lounge'))
    flushSync()
    expect(container.querySelector('.hcard')).toBeTruthy()
    vi.advanceTimersByTime(CARD_GRACE_MS + 20)
    flushSync()
    expect(container.querySelector('.hcard')).toBeNull()
  })

  it('stays open while the pointer is on the card itself', async () => {
    const { container } = await open('tv-lounge')
    await fireEvent.pointerLeave(blk(container, 'tv-lounge'))
    await fireEvent.pointerEnter(container.querySelector('.hcard')!)
    vi.advanceTimersByTime(CARD_GRACE_MS + 20)
    flushSync()
    expect(container.querySelector('.hcard')).toBeTruthy()
  })

  // The wording is the 2D map's, word for word (DESIGN.md "Cards").
  it('says the presence, how long, and the window it was measured against', async () => {
    const { card } = await open('tv-lounge')
    expect(card.textContent).toContain('quiet · not heard for')
    expect(card.textContent).toContain('26 h')
    expect(card.textContent).toContain('window 24 h')
  })

  it('says last seen, first seen and how many events', async () => {
    const { card } = await open('tv-lounge')
    expect(card.textContent).toContain('last seen')
    expect(card.textContent).toContain('first seen')
    expect(card.textContent).toContain('1,204')
    expect(card.textContent).toContain('events')
  })

  // Counts exist as words on the card and nowhere else (#981, round 46):
  // cam-porch's line in the drawing reads exactly this.
  it('says how many flags and watchers, as plain words in their own inks', async () => {
    const g = groundWith([host('10.10.0.12', 'tom-desktop', { flags: 2, watch: 1 })])
    const { card } = await open('tom-desktop', g)
    const line = card.querySelector('[data-marks]')
    expect(line?.textContent?.replace(/\s+/g, ' ').trim()).toBe('2 flags · 1 watch')
    expect(line!.querySelector('.fw')?.textContent).toBe('2 flags')
    expect(line!.querySelector('.ww')?.textContent).toBe('1 watch')
  })

  it('says no counts at all on a host nothing is behind', async () => {
    const { card } = await open('tv-lounge')
    expect(card.querySelector('[data-marks]')).toBeNull()
  })

  it('offers the two marks, worded as the 2D map words them', async () => {
    const { card } = await open('tv-lounge')
    const acts = [...card.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())
    expect(acts).toContain('mark quiet on purpose ▸')
    expect(acts).toContain('dismiss ▸')
  })

  it('says a quiet host comes back by itself', async () => {
    const { card } = await open('tv-lounge')
    expect(card.textContent).toContain('comes back by itself when the feed hears it again')
  })

  it('quotes the reason on a host already marked quiet on purpose, with who and when', async () => {
    const { card } = await open('printer-old')
    expect(card.querySelector('.quote')?.textContent).toBe('switched off at the wall')
    expect(card.textContent).toContain('tom')
  })

  it('offers no mark at all for a host the register has not recorded yet', async () => {
    const g = groundWith([bufferHost('newcomer', '10.10.0.20')])
    const { card } = await open('newcomer', g)
    expect(card.textContent).toContain('not in the host register yet')
    expect([...card.querySelectorAll('.acts button')].map((b) => b.textContent?.trim())).not.toContain('dismiss ▸')
  })

  // A router is not a host: the syslog feed is what hears a host, and
  // the router is the thing doing the hearing.
  it('opens no card on a router', async () => {
    const r = render(City, { props: { stop: 'street', ground } })
    const router = [...r.container.querySelectorAll('.blk[role="button"]')].find((g) => (g.getAttribute('aria-label') ?? '').includes(', router'))
    await fireEvent.pointerEnter(router!)
    flushSync()
    expect(r.container.querySelector('.hcard')).toBeNull()
  })
})

describe('the marks', () => {
  async function open(name: string) {
    const r = render(City, { props: { stop: 'street', ground } })
    await fireEvent.pointerEnter(blk(r.container, name))
    flushSync()
    return r
  }

  const act = (c: Element, label: string) => [...c.querySelectorAll('.acts button')].find((b) => b.textContent?.trim() === label)!

  // Both marks go through the register (putHostMark / deleteHostMark
  // behind it); the card invents no endpoint of its own.
  it('marks quiet on purpose with the reason the operator typed', async () => {
    const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
    const { container } = await open('tv-lounge')
    await fireEvent.click(act(container.querySelector('.hcard')!, 'mark quiet on purpose ▸'))
    flushSync()
    const input = container.querySelector<HTMLInputElement>('#city-host-reason')!
    await fireEvent.input(input, { target: { value: 'switched off for the summer' } })
    flushSync()
    await fireEvent.click([...container.querySelectorAll('.hcard .form button')].find((b) => b.textContent === 'Mark')!)
    expect(mark).toHaveBeenCalledWith('ether1|10.10.0.10', 'intended', 'switched off for the summer')
  })

  it('will not mark quiet on purpose without a reason', async () => {
    const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
    const { container } = await open('tv-lounge')
    await fireEvent.click(act(container.querySelector('.hcard')!, 'mark quiet on purpose ▸'))
    flushSync()
    const go = [...container.querySelectorAll('.hcard .form button')].find((b) => b.textContent === 'Mark') as HTMLButtonElement
    expect(go.disabled).toBe(true)
    await fireEvent.click(go)
    expect(mark).not.toHaveBeenCalled()
  })

  it('dismisses a host through the register, with no reason needed', async () => {
    const mark = vi.spyOn(hostsState, 'mark').mockResolvedValue(true)
    const { container } = await open('tv-lounge')
    await fireEvent.click(act(container.querySelector('.hcard')!, 'dismiss ▸'))
    expect(mark).toHaveBeenCalledWith('ether1|10.10.0.10', 'dismissed')
  })

  // A statement you cannot withdraw is worse than one you never made.
  it('takes a mark back through the register', async () => {
    const unmark = vi.spyOn(hostsState, 'unmark').mockResolvedValue(true)
    const { container } = await open('printer-old')
    await fireEvent.click(act(container.querySelector('.hcard')!, 'unmark ▸'))
    expect(unmark).toHaveBeenCalledWith('ether1|10.10.0.11')
  })

  it("shows the register's own failure where the mark was offered", async () => {
    vi.spyOn(hostsState, 'mark').mockImplementation(async () => {
      hostsState.error = 'forbidden'
      return false
    })
    const { container } = await open('tv-lounge')
    await fireEvent.click(act(container.querySelector('.hcard')!, 'dismiss ▸'))
    flushSync()
    expect(container.querySelector('.hcard')?.textContent).toContain('forbidden')
  })

  it('offers no mark to a reader who cannot edit', async () => {
    authState.role = 'viewer'
    const { container } = await open('tv-lounge')
    const acts = [...container.querySelectorAll('.hcard .acts button')].map((b) => b.textContent?.trim())
    expect(acts).not.toContain('dismiss ▸')
    expect(acts).toContain('stream ▸')
  })
})

describe('clicking a building', () => {
  // DESIGN.md "The reach": standing on a building drops the camera to
  // the street stop on it. This wires to the entry point that already
  // exists -- it does not rebuild the reach.
  it("opens that host's reach", async () => {
    const { container } = render(City, { props: { stop: 'city', ground } })
    await fireEvent.click(blk(container, 'tom-desktop'))
    flushSync()
    const crumb = container.querySelector('.crumb')
    expect(crumb).toBeTruthy()
    expect(crumb!.textContent).toContain('tom-desktop')
  })
})
