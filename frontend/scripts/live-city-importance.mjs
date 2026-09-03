// SPDX-License-Identifier: AGPL-3.0-only
//
// #867: height = importance, against a running instance. The unit
// tests prove the two readings and the reduced-motion snap on
// fixtures (lib/city/importance.test.ts); this walks the real thing --
// a host many other hosts talk to versus a host with nothing but a
// watchlist entry on it, at the city stop, with the toggle actually
// switching which one stands taller and doing it from the keyboard.

import { session, check, done, feedRaw } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-city-importance', kind: 'ingest', device: DEVICE },
})
const token = (await tokenRes.json()).value

async function push(payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// One lane, two named hosts: busy-host gets talked to by several other
// hosts (tallest under depended-on); watched-host gets nothing but a
// watchlist entry (tallest under watched, once toggled).
const LANE = { iface: 'bridge-lan', cidr: '10.0.10.1/24', comment: 'LAN' }
const BUSY = '10.0.10.21'
const WATCHED = '10.0.10.22'

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: LANE.cidr, network: '10.0.10.0', interface: LANE.iface, comment: LANE.comment }],
  })) === 200,
  'the lane range is pushed',
)

check(
  (await push({
    kind: 'dhcp-lease',
    page: 1,
    pages: 1,
    records: [
      { address: BUSY, mac: 'aa:bb:cc:00:00:21', hostname: 'busy-host' },
      { address: WATCHED, mac: 'aa:bb:cc:00:00:22', hostname: 'watched-host' },
    ],
  })) === 200,
  'the lease table names both hosts',
)

check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 0, comment: 'lan out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: 'A|imp|', log: true, inInterface: LANE.iface, outInterface: 'ether1' },
    ],
  })) === 200,
  'the filter-rule table is pushed',
)

// busy-host and watched-host each get one ordinary egress hit so they
// stand on the map at all -- zones.svelte.ts's own host-attribution
// counts the private side (the source) of a boundary-crossing event,
// so a host that is only ever a *destination* below would never
// appear as a building in the first place.
feedRaw(`firewall,info A|imp| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), ${BUSY}:51000->203.0.113.9:443, len 60`)
feedRaw(`firewall,info A|imp| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), ${WATCHED}:51001->203.0.113.9:443, len 60`)

// Five distinct LAN hosts land on busy-host; watched-host hears from
// nobody at all in this window.
for (let i = 0; i < 5; i++) {
  feedRaw(`firewall,info A|imp| forward: in:bridge-lan out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.10.${30 + i}:5${100 + i}->${BUSY}:445, len 60`)
}
await new Promise((r) => setTimeout(r, 900))

// A watchlist entry scoped to watched-host, enabled by default -- the
// weight the watched reading is meant to pick up (#867's own "flag and
// watch weight the operator has put on it").
const entry = await api('POST', '/api/definitions', {
  name: 'live importance watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { source: { ip: WATCHED }, ports: [22] },
})
check(entry.status === 201, `the watchlist entry on watched-host is created (${entry.status})`)

await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
await slider.fill('4') // the city stop
await new Promise((r) => setTimeout(r, 900))

async function heightOf(ip) {
  return page.evaluate((needle) => {
    const el = document.querySelector(`[data-card="topography"] .city .blk[data-cid$="${needle}"]`)
    if (!el) return null
    return el.getBoundingClientRect().height
  }, ip)
}

const readingBtn = page.locator('[data-card="topography"] .importance .reading')
check((await readingBtn.count()) === 1, 'the importance toggle is at the city stop')
check((await readingBtn.getAttribute('aria-label'))?.includes('depended-on') ?? false, 'the button states the current reading in its own aria-label')

const dependedBusy = await heightOf(BUSY)
const dependedWatched = await heightOf(WATCHED)
check(dependedBusy !== null && dependedWatched !== null, `both buildings are found (busy ${dependedBusy}, watched ${dependedWatched})`)
check(dependedBusy > dependedWatched, `depended-on: busy-host (talked to by 5 hosts) stands taller than watched-host (${dependedBusy} > ${dependedWatched})`)

// Keyboard: focus the button and activate it with Enter, no click.
await readingBtn.focus()
await page.keyboard.press('Enter')
await new Promise((r) => setTimeout(r, 900))

check((await readingBtn.getAttribute('aria-pressed')) === 'true', 'Enter on the focused button switches to the watched reading (aria-pressed)')
check((await readingBtn.getAttribute('aria-label'))?.includes('watched') ?? false, 'the aria-label now states "watched"')
check((await readingBtn.textContent())?.includes('watched') ?? false, 'the button\'s own visible text states "watched" too, not only its pressed style')

const watchedBusy = await heightOf(BUSY)
const watchedWatched = await heightOf(WATCHED)
check(watchedWatched > watchedBusy, `watched: watched-host now stands taller than busy-host (${watchedWatched} > ${watchedBusy})`)
check(watchedBusy < dependedBusy, `busy-host's own plinth actually shrank when the reading changed (${watchedBusy} < ${dependedBusy})`)

// Space toggles it back.
await page.keyboard.press('Space')
await new Promise((r) => setTimeout(r, 900))
check((await readingBtn.getAttribute('aria-pressed')) === 'false', 'Space on the focused button switches back to depended-on')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
