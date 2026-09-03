// SPDX-License-Identifier: AGPL-3.0-only
//
// #648, topography's ratified furniture: the altitude slider, the
// health dials, the aggregate bar, node info cards, and that the reach
// backdrop still reads as the map (round 24), not as this scenario's
// own concern -- live-topography-reach-compose.mjs already covers the
// reach itself end to end.
//
// Runs after the other live-topography-*.mjs scenarios by filename
// order; a shared suite instance may already carry flags/watchers from
// earlier scenarios, so every count below is asserted "at least", never
// "exactly" (the same reasoning live-topography-reality.mjs gives for
// its own alarm-count check).

import { session, check, done, feedRaw, feedPortScan } from './live-browser.mjs'

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
  data: { name: 'live-topo-furniture', kind: 'ingest', device: DEVICE },
})
check(tokenRes.status() === 201, `an ingest token is issued (${tokenRes.status()})`)
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

// The LAN's own pushed range: the aggregate bar and the node card's
// address/lane both correlate against this CIDR (lib/addressMatch.ts's
// addressInCidr), so it has to be a real pushed table, not a guess.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }],
  })) === 200,
  'the LAN range is pushed',
)

// One host, 192.168.1.60, real traffic so it stands on the zone card.
for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info A|furniture-web| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.60:5${100 + i}->203.0.113.9:443, len 60`)
}

// A real flag targeting that same host -- port_scan is the "scan"
// family, which carries the ✱ alarm mark (see lib/flagPalette.ts).
feedPortScan(20, '192.168.1.60')

// A real watchlist entry (#407: an expectation definition) scoped to
// the same host, so its node card shows both a warning and a watch.
const entry = await api('POST', '/api/definitions', {
  name: 'live furniture watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { source: { ip: '192.168.1.60' }, ports: [22] },
})
check(entry.status === 201, `the watch entry is created (${entry.status})`)

await new Promise((r) => setTimeout(r, 1200))
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// --- the health dials -----------------------------------------------------

const dnums = await page.locator('[data-card="topography"] .dnum').allTextContents()
check(dnums.length === 2, `both dials print a count (${JSON.stringify(dnums)})`)
check(Number(dnums[0]) >= 1, `the flags dial counts at least the one just raised (${dnums[0]})`)
check(Number(dnums[1]) >= 1, `the watchers dial counts at least the one just created (${dnums[1]})`)
check(
  (await page.locator('[data-card="topography"] .dring.d-alarm').count()) >= 1,
  'the flags ring draws an alarm segment, not the rest state',
)

await page.click('[data-card="topography"] .dial >> nth=0')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=flags', { timeout: 5000 })
check(true, 'clicking the flags dial opens the docket on the flags tab')

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// --- the aggregate bar ------------------------------------------------------

const bars = await page.locator('[data-card="topography"] .zone .hbar-g').count()
check(bars >= 2, `the LAN island draws both halves of the aggregate bar (${bars} found)`)
check((await page.locator('[data-card="topography"] .zone .hb-div').count()) >= 1, 'a centre divider is drawn between them')

await page.click('[data-card="topography"] .zone .hbar-g[aria-label*="watcher"] >> nth=0')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=watchlist', { timeout: 5000 })
check(true, 'the purple half opens the watchlist')

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })
await page.click('[data-card="topography"] .zone .hbar-g[aria-label*="open flag"] >> nth=0')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=flags', { timeout: 5000 })
check(true, 'the red half opens the flags tab, pre-filtered to the zone')

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// --- the altitude slider -----------------------------------------------------

const range = page.locator('[data-card="topography"] .alt-range')
check((await range.getAttribute('value')) === '2', 'the altitude defaults to "zones", today\'s unchanged map')
check((await page.locator('[data-card="topography"] .tick').count()) === 8, 'eight stops (four 2D, four city), no text labels')
const altitudeText = await page.locator('[data-card="topography"] .altitude').textContent()
check((altitudeText ?? '').trim() === '', 'the stops carry no text, symbols only')

await range.fill('3')
await page.waitForSelector('[data-card="topography"] .camera.cam-survey', { timeout: 5000 })
check(true, 'moving the slider to survey applies the tilted camera')

await range.fill('2')

// --- node info cards ---------------------------------------------------------

await page.click('[data-card="topography"] .host-link >> text=192.168.1.60')
await page.waitForSelector('[data-card="topography"] .membrane-layer', { timeout: 5000 })

await page.click('[data-card="topography"] .host-node')
await page.waitForSelector('.node-card', { timeout: 5000 })
const cardText = await page.textContent('.node-card')
check(cardText.includes('192.168.1.60'), `the card names the host's own address (${cardText})`)
check(cardText.includes('The LAN'), 'the card names its lane')
check(cardText.includes('open flag'), 'the card surfaces the open flag as a warning')
check(cardText.includes('watched'), 'the card says it is watched')

await page.click('.node-card .nc-act >> text=open in stream ▸')
await page.waitForFunction(() => location.search.includes('Query='), null, { timeout: 5000 })
check(true, 'the open-in-stream action filters the live view to this address')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
