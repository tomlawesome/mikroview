// SPDX-License-Identifier: AGPL-3.0-only
//
// #628, map layer 2: the Policy lens draws intended-policy edges from a
// rule table genuinely pushed through POST /api/ingest/routeros -- one
// edge per boundary pair per direction, port badges, a refused pair
// dying at the waist, and click-through landing on the stream filtered
// to the pair. Also the lens's honest empty state before any push:
// waiting for data is a state, not a fault.
//
// Depends on earlier scenarios' traffic only loosely: it feeds its own
// events so ether1 resolves as the internet-facing boundary and bridge1
// stands as a lane whichever scenarios ran before it.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// The device the events actually arrive from, asked of the instance --
// see live-router-lookup.mjs for why this is discovered, not assumed.
syslog(2, 'topo-policy-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

// Traffic so the map has islands: public sources in on ether1 make it
// the internet-facing boundary; bridge1 becomes a lane.
syslog(30, 'topo-policy-traffic')

// --- The empty state: before any rule push -------------------------------
// Scenarios share one instance in filename order, and earlier ones
// (live-router-lookup.mjs) push rule tables of their own -- so the
// empty state is only assertable when this scenario meets a fresh
// instance (as a standalone run does). Skipping on a fed one is said,
// not silent.

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] [aria-label="Map lenses"]', { timeout: 10000 })
await page.click('[data-card="topography"] [aria-label="Map lenses"] >> text=Policy')

const pre = await page.request.get(`${URL_BASE}/api/routeros/${DEVICE}/rules`)
const preAvailable = pre.ok() && (await pre.json()).available
if (!preAvailable) {
  const stage = page.locator('[data-card="topography"] .stage svg')
  await page.waitForTimeout(500)
  const emptyText = await stage.textContent()
  check(
    emptyText.includes('nothing is broken, this lens is waiting for data'),
    'before any push, the Policy lens says it is waiting for data, not broken',
  )
  check(emptyText.includes('Run setup'), 'the empty state names the way to configure the push')
} else {
  check(true, 'an earlier scenario already pushed rule tables -- the pre-push empty state is asserted on standalone runs')
}

// --- Push the tables through the real ingest endpoint --------------------

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-policy', kind: 'ingest', device: DEVICE },
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

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }],
  })) === 200,
  'the /ip address table is accepted, naming the lane',
)

check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      // The pair out: LAN may reach the internet on the named ports.
      { ordinal: 0, comment: 'LAN out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443, protocol: 'tcp' },
      { ordinal: 1, comment: '', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: '53,123', protocol: 'udp' },
      // The pair back: refused outright -- dies at the waist.
      { ordinal: 2, comment: 'Nothing unsolicited comes in', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|topo-policy|', log: true, inInterface: 'ether1', outInterface: 'bridge1' },
      // An input rule: terminates at the router, must draw no edge.
      { ordinal: 3, comment: 'router ssh', chain: 'input', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', dstPort: 22 },
    ],
  })) === 200,
  'the filter-rule table is accepted through the real ingest endpoint',
)

// --- The edges draw from the push ----------------------------------------

// The lens refreshes with the device list; a reload is the honest way a
// fresh push arrives today, same as the map's own first load.
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] [aria-label="Map lenses"]', { timeout: 10000 })
await page.click('[data-card="topography"] [aria-label="Map lenses"] >> text=Policy')
await page.waitForSelector('[data-card="topography"] .edge-g', { timeout: 10000 })

// SVG lines resolve as "hidden" to Playwright's geometry-box visibility
// (see the live-check skill), so these assert on presence and text.
const edgeCount = await page.locator('[data-card="topography"] .edge-g').count()
check(edgeCount === 2, `one edge per pair per direction: the input rule draws nothing (${edgeCount} edges)`)

const badges = await page.locator('[data-card="topography"] .edge-badge').allTextContents()
check(
  badges.some((b) => b.includes(':443') && b.includes(':53')),
  `the accepting pair carries its port-set badges (${JSON.stringify(badges)})`,
)
const refusedCount = await page.locator('[data-card="topography"] .edge.refused').count()
check(refusedCount === 1, 'the refused pair draws as dying at the waist')
check((await page.locator('[data-card="topography"] .edge-bar').count()) >= 1, 'the ⊣ bar stands where the refusal dies')

// --- Click-through: the pair, said in the stream's own filters -----------

// The busiest pair sorts first: bridge1→ether1, the accepting edge.
// Clicked via its badge: a Playwright click aims at the bounding-box
// centre, which for a curved path is empty air the svg soaks up; the
// badge text paints where it sits.
await page.click('[data-card="topography"] .edge-g >> nth=0 >> .edge-badge')
// Filters sync to the URL (App.svelte's shareable-link effect), so the
// URL is the honest witness of what the click-through set.
await page.waitForFunction(() => location.search.includes('srcQuery='), null, { timeout: 5000 })
const url = page.url()
check(
  decodeURIComponent(url).includes('srcQuery=192.168.1.1/24'),
  `the LAN side of the pair rides its pushed CIDR into the stream's filters (${url})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
