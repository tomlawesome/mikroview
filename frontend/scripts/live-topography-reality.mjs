// SPDX-License-Identifier: AGPL-3.0-only
//
// #629, map layer 3: the Traffic lens judges observed traffic against
// the pushed intent. Feeds all three deltas through the real listeners:
// a planned flow (an accepting rule anticipated it), a held one (drops
// on a refusing pair -- policy doing its job, calm), an unplanned one
// (accepted traffic where the table only refuses -- the alarm), and an
// accepting rule nothing exercises, drawn as a ghost.
//
// Runs after live-topography-policy.mjs by filename order and leans on
// the tables it pushed (bridge1→ether1 accepts, ether1→bridge1 drop);
// pushes one extra lane and one extra never-exercised rule of its own.

import { session, check, done, feedRaw, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

syslog(2, 'topo-reality-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-reality', kind: 'ingest', device: DEVICE },
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

// A second lane whose accepting rule nothing will exercise: the ghost.
// A push replaces its kind's whole table, so the LAN's own address
// rides along -- dropping it would strip the name an earlier scenario
// gave the bridge1 lane.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' },
      { address: '10.9.0.1/24', network: '10.9.0.0', interface: 'ether5', comment: 'The quiet lane' },
    ],
  })) === 200,
  'a second lane is pushed for the ghost to anchor on',
)
// The complete intended table, pushed whole (a RouterOS push always
// carries the full table): bridge1→ether1 accepts, ether1→bridge1
// refusal, and the quiet lane's accepting rule nothing will exercise.
// Self-contained on purpose -- a standalone run judges identically.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 0, comment: 'LAN out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443, protocol: 'tcp' },
      { ordinal: 1, comment: 'Nothing unsolicited comes in', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|forward-drop|', log: true, inInterface: 'ether1', outInterface: 'bridge1' },
      { ordinal: 2, comment: 'quiet lane ssh out', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'ether5', outInterface: 'ether1', dstPort: 22, protocol: 'tcp' },
      { ordinal: 3, comment: 'the quiet lane may not reach the LAN', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|quiet-block|', log: true, inInterface: 'ether5', outInterface: 'bridge1' },
    ],
  })) === 200,
  'the intended table is pushed whole, quiet-lane rules included',
)

// The three realities, through the real syslog listener: planned
// (bridge1→ether1 accepts, intended), held (ether1→bridge1 drops on the
// refusing rule -- policy doing its job), and unplanned (an accepted
// flow into the quiet lane no rule anywhere anticipates).
for (let i = 0; i < 6; i++) {
  feedRaw(`firewall,info A|planned-web| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.${20 + i}:5${100 + i}->203.0.113.9:443, len 60`)
  feedRaw(`firewall,info D|forward-drop| forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 198.51.100.${30 + i}:4${400 + i}->192.168.1.10:23, len 60`)
}
feedRaw('firewall,info A|mystery-accept| forward: in:ether1 out:ether5, connection-state:new, proto TCP (SYN), 203.0.113.66:41000->10.9.0.20:8443, len 60')
// The held pair rides its own boundary, ether5→bridge1, which no other
// scenario feeds: on a shared suite instance the ether1→bridge1 pair
// has accepts from five earlier scenarios, so its verdict is unplanned
// and its badge can never say held -- the refused-share check below
// starved on exactly that. accepts stays 0 on this pair, so it reads
// holding in the suite and standalone alike.
for (let i = 0; i < 4; i++) {
  feedRaw(`firewall,info D|quiet-block| forward: in:ether5 out:bridge1, connection-state:new, proto TCP (SYN), 10.9.0.${40 + i}:3${300 + i}->192.168.1.10:445, len 60`)
}

// Reload so the freshly pushed tables are re-fetched alongside the
// freshly arrived events (same honest path the policy scenario takes).
await new Promise((r) => setTimeout(r, 1200))
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .redge', { timeout: 10000 })

// SVG geometry-box visibility lies for lines (live-check skill), so
// presence and text carry the assertions.
const alarmCount = await page.locator('[data-card="topography"] .redge.alarm').count()
check(alarmCount >= 1, `the unplanned flow spends the saturated colour (${alarmCount} alarm edge)`)

const badges = await page.locator('[data-card="topography"] [class*="edge-badge"]').allTextContents()
check(
  badges.some((b) => b.includes('unplanned')),
  `the unplanned flow says so in words (${JSON.stringify(badges)})`,
)
check(
  badges.some((b) => b.includes('held') || b.includes('dropped')),
  'the refused share is counted where it dies',
)

const ghostCount = await page.locator('[data-card="topography"] .gedge').count()
check(ghostCount >= 1, `intent nothing exercised draws as a ghost (${ghostCount})`)
check(
  badges.some((b) => b.includes('never exercised')),
  'the ghost is named, not just faint',
)

// Click-through from a reality edge lands on the filtered stream. The
// worst unplanned pair -- the internet-side one this check is about --
// draws as the escalated card and no longer as a pill (#897 item 2), so
// click the card when it is there; the first alarm pill otherwise.
const escalated = page.locator('[data-card="topography"] .unplanned-card')
if ((await escalated.count()) > 0) {
  await escalated.first().click()
} else {
  await page.click('[data-card="topography"] text.edge-badge.alarm-t >> nth=0')
}
await page.waitForFunction(() => location.search.includes('Query=') || location.search.includes('Scope='), null, { timeout: 5000 })
check(
  decodeURIComponent(page.url()).includes('srcScope=external'),
  `the unplanned internet-side flow rides scope into the stream's filters (${page.url()})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
