// SPDX-License-Identifier: AGPL-3.0-only
//
// #460, the decommission offer and the ghost segment, against a running
// instance: offer -> ghost -> retire.
//
// The unit tests prove the sentences on fixtures (lib/decommission.ts)
// and the Go tests prove the record and the API. This walks the real
// thing end to end, which is the only place the three meet: a push that
// stops carrying a range has to produce an offer with a receipt drawn
// from the events actually in the ring, saying yes has to leave a ghost
// lane where the segment was, and force-remove has to state its whole
// contract before it takes the ghost off the map.
//
// Retirement by clean window is deliberately not driven here. The
// shortest window the server will accept is an hour
// (decommission.MinCleanWindow), so a scenario cannot wait one out --
// and force-remove is the retirement round 55 actually draws a card for
// (its `flat-retire` scene is this confirm state). The silent
// six-quiet-hours path and its undo are covered by the Go tests, which
// can move the clock.

import { session, check, done, feedAndSettle } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// The retired segment, kept off every other scenario's ranges so the
// offer this one asserts on is the offer this one caused.
const GHOST_CIDR = '10.0.70.0/24'
const GHOST_IFACE = 'vlan-garage'
const STRAGGLER = '10.0.70.14'

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
  data: { name: 'live-decommission', kind: 'ingest', device: DEVICE },
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

const LIVE_LANES = [
  { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge-lan', comment: 'LAN' },
  { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'vlan-srv', comment: 'Servers' },
]
const GARAGE = { address: '10.0.70.1/24', network: '10.0.70.0', interface: GHOST_IFACE, comment: 'Garage' }

const addressCycle = (records) => ({ kind: 'ip-address', page: 1, pages: 1, routerosVersion: '7.23.3 (stable)', records })

// --- The range is carried, and something on it talks --------------------
//
// Two complete cycles are what makes a departure a departure
// (routerstate/departures.go): the first is the baseline, and only a
// later complete cycle that no longer carries the range says it went.

check((await push(addressCycle([...LIVE_LANES, GARAGE]))) === 200, 'the router pushes an address table that carries the garage range')

// A lease for the address, so the offer's enrichment has a name to
// freeze -- and so the receipt can say who it would have caught rather
// than only how many.
check(
  (await push({
    kind: 'dhcp-lease',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: STRAGGLER, mac: '64:d1:54:00:70:14', hostname: 'garage-cam' }],
  })) === 200,
  'the router pushes the lease that names the address',
)

// A rule that logs the range, so the watch can honestly claim quiet
// later. Without one the watch is broken by design and never retires:
// "the range went silent" is not a claim mikroview will make when
// nothing was ever able to hear it (engine/decommission_coverage.go).
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 0,
        comment: 'garage to servers',
        chain: 'forward',
        action: 'accept',
        srcAddress: GHOST_CIDR,
        srcAddressList: '',
        logPrefix: 'A|decomm|',
        log: true,
        inInterface: GHOST_IFACE,
        outInterface: 'vlan-srv',
      },
    ],
  })) === 200,
  'a logging rule covers the range, so a watch on it could see a straggler at all',
)

// Three lines from inside the range, before it is retired: this is what
// the replay has to find and what the receipt has to report.
const before = []
for (let i = 0; i < 3; i++) {
  before.push(
    `firewall,info A|decomm| forward: in:${GHOST_IFACE} out:vlan-srv, connection-state:new, proto TCP (SYN), ${STRAGGLER}:5${400 + i}->10.0.40.10:554, len 60`,
  )
}
await feedAndSettle(page, ...before)

// --- The push stops carrying it: the offer, with its receipt ------------

check((await push(addressCycle(LIVE_LANES))) === 200, 'the next complete cycle no longer carries the garage range')

const offers = await page.request.get(`${URL_BASE}/api/decommission`)
const offered = offers.ok() ? await offers.json() : { offers: [] }
const mine = (offered.offers ?? []).find((o) => o.cidr === GHOST_CIDR)
check(!!mine, `the departure is offered for the retired range (${GHOST_CIDR})`)
check(mine?.receipt?.emissionCount >= 3, `the offer carries a receipt counting what a watch would have caught (${mine?.receipt?.emissionCount})`)
check(
  (mine?.receipt?.addresses ?? []).some((a) => a.address === STRAGGLER && a.name === 'garage-cam'),
  'the receipt names who it would have caught, from the lease the router pushed',
)

async function toMap() {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  await new Promise((r) => setTimeout(r, 900))
}

await toMap()

const ghostLane = page.locator(`[data-card="topography"] [data-ghost="${GHOST_CIDR}"]`)
check((await ghostLane.count()) > 0, 'the retired segment keeps its place on the map as a ghost lane')
check(
  ((await ghostLane.first().getAttribute('aria-label')) ?? '').includes('watch none'),
  'before it is answered the ghost says no watch is running yet',
)

const offerCard = page.locator('[data-card="topography"] [data-decomm="offer"]')
check((await offerCard.count()) > 0, 'the offer card is on the map without being asked for -- the offer is the moment')
const offerText = (await offerCard.first().textContent()) ?? ''
check(offerText.includes('has left the router'), `the card leads with what left (${JSON.stringify(offerText.slice(0, 80))})`)
check(offerText.includes('would have caught'), 'the card makes its argument with the replay receipt')
check(offerText.includes('garage-cam'), 'and names the device the receipt is about')
check(offerText.includes('Watch the dead range for stragglers?'), 'and asks the question in #460 own words')

// --- Yes: the ghost stays, painted by its watch --------------------------

await offerCard.locator('button.go').click()
// `attached`, not `visible`: Playwright reads an SVG group's geometry
// box, which for a `<g>` of strokes and text resolves as hidden however
// clearly it is drawn (the live-check skill's own note).
await page.waitForSelector(`[data-card="topography"] [data-ghost="${GHOST_CIDR}"][aria-label*="watch holding"]`, {
  state: 'attached',
  timeout: 10000,
})
check(true, 'saying yes leaves the segment on the map as a ghost with its watch holding')

const laneText = (await ghostLane.first().textContent()) ?? ''
check(laneText.includes('holding'), `the ghost lane counts its quiet against the clean window (${JSON.stringify(laneText.slice(0, 120))})`)
check(laneText.includes('last known'), 'and keeps the names the range last had')

const legend = (await page.locator('[data-card="topography"] .map-legend').textContent()) ?? ''
check(legend.includes('ghost · watch holding'), `the legend explains the ghost's ink while a ghost is drawn (${JSON.stringify(legend)})`)

// --- Retire: force-remove states the whole contract first ----------------

// dispatchEvent rather than click: the lane is an SVG `<g>`, which
// Playwright resolves as hidden whatever it looks like on screen, and
// even a forced click is refused on that reading.
await ghostLane.first().dispatchEvent('click')
const ghostCard = page.locator('[data-card="topography"] [data-decomm="ghost"]')
await ghostCard.first().waitFor({ state: 'visible', timeout: 10000 })
const ghostText = (await ghostCard.first().textContent()) ?? ''
check(ghostText.includes('WATCHLIST'), 'the ghost card shows the watchlist row, so what would stay behind is visible before it does')

await ghostCard.locator('button.hot').first().click()
const confirmText = (await ghostCard.first().textContent()) ?? ''
check(confirmText.includes('The ghost leaves the map now'), 'force-remove states its contract before it is taken')
check(confirmText.includes('can only be forgotten from there'), 'including that the watch outlives the ghost')

const go = ghostCard.locator('button.go')
check(await go.first().isDisabled(), 'and refuses to act until the override has a reason recorded with it')

await ghostCard.locator('input').first().fill('the rack is gone; the range is not coming back')
await go.first().click()
await page.waitForSelector(`[data-card="topography"] [data-ghost="${GHOST_CIDR}"]`, { state: 'detached', timeout: 10000 })
check(true, 'the ghost leaves the map at once')

const after = await page.request.get(`${URL_BASE}/api/decommission`)
const watches = after.ok() ? ((await after.json()).watches ?? []) : []
const kept = watches.find((w) => w.cidr === GHOST_CIDR)
check(!!kept && kept.detached === true && !kept.retiredAt, 'and the watch goes on, detached, exactly as the contract said')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
