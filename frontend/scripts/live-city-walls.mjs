// SPDX-License-Identifier: AGPL-3.0-only
//
// #865, walls and gates from the rule set, against a running instance.
// The unit tests prove the derivations on fixtures (gates.ts, walls.ts,
// escalate.ts); this walks the real thing: before any rule table is
// pushed the walls carry no gates and say why, then a real filter-rule
// push opens real gates, the policy lens lights every one with its rule
// number while the traffic lens leaves the wall quiet, and a drop road
// ends at the wall carrying the refusing rule's own name.

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

async function toDistrictStop() {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
  await slider.fill('6') // the district stop -- where a wall's gates are drawn
  await new Promise((r) => setTimeout(r, 900))
}

function clickLens(name) {
  return page.click(`[data-card="topography"] [aria-label="Map lenses"] >> text=${name}`)
}

// --- Before any push: a boundary-derived district, no gates, and said why --

for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info A|walls-pre| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
}
await new Promise((r) => setTimeout(r, 900))
await toDistrictStop()

const preRules = await page.request.get(`${URL_BASE}/api/routeros/${DEVICE}/rules`)
const prePushed = preRules.ok() && (await preRules.json()).available
if (!prePushed) {
  const preText = await page.locator('[data-card="topography"] .city').textContent()
  check(preText.includes('NO RULES PUSHED'), 'before any push, a district plaque says plainly that no rule table has been pushed yet')
  check((await page.locator('[data-card="topography"] .city .gate-n').count()) === 0, 'a router with no pushed rule table draws no gates at all')
  const plate = page.locator('[data-card="topography"] .city .plate').first()
  check((await plate.getAttribute('aria-label'))?.includes('no rule table has been pushed yet') ?? false, 'the district itself says why, not just the plaque')
} else {
  check(true, 'an earlier scenario already pushed a rule table -- the pre-push honesty state is asserted on standalone runs')
}

// --- Push lanes, a rule table with two gates and one drop, and traffic ----

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-city-walls', kind: 'ingest', device: DEVICE },
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

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge-lan', comment: 'LAN' },
      { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'vlan-srv', comment: 'Servers' },
      { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'vlan-guest', comment: 'Guest' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'vlan-iot', comment: 'IoT' },
    ],
  })) === 200,
  'four lane ranges are pushed',
)

check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      // A gate: lan -> srv, and it logs -- lit.
      { ordinal: 0, comment: 'lan to servers', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: 'A|walls|', log: true, inInterface: 'bridge-lan', outInterface: 'vlan-srv' },
      // No accept rule at all the other way: that wall stands with no gate.
      // Guest is refused outright, and the refusal names itself.
      {
        ordinal: 1,
        comment: 'guest cannot reach the lan',
        chain: 'forward',
        action: 'drop',
        srcAddressList: '',
        logPrefix: 'D|guest-isolation|',
        log: true,
        inInterface: 'vlan-guest',
        outInterface: 'bridge-lan',
      },
    ],
  })) === 200,
  'the filter-rule table is pushed, with one gate and one refusal',
)

// Traffic: the gate carries real traffic; the guest boundary is refused
// by its own named rule; a third, unrelated pair (lan -> iot) crosses
// with no rule anticipating it at all and no rule label on the drop, so
// the escalated callout must say "no rule named" rather than inventing
// one. Kept off the guest pair deliberately: a road folds both
// directions of the same pair together (layout.ts), so a rule label on
// one direction would otherwise paper over the other's silence.
for (let i = 0; i < 6; i++) {
  feedRaw(`firewall,info A|walls| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
  feedRaw(`firewall,info D|guest-isolation| forward: in:vlan-guest out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.30.2${i}:5${200 + i}->10.0.10.10:445, len 60`)
}
for (let i = 0; i < 9; i++) {
  feedRaw(`firewall,info D|| forward: in:bridge-lan out:vlan-iot, connection-state:new, proto TCP (SYN), 10.0.10.3${i % 9}:5${300 + i}->10.0.20.20:22, len 60`)
}

await toDistrictStop()

// --- Traffic lens: the wall stays quiet, no rule numbers -------------------

await clickLens('Traffic')
await new Promise((r) => setTimeout(r, 400))
check((await page.locator('[data-card="topography"] .city .gate-n').count()) === 0, 'the traffic lens leaves every gate quiet -- no rule numbers lit')

// --- Policy lens: every gate lights with its rule count ---------------------

await clickLens('Policy')
await new Promise((r) => setTimeout(r, 400))
const gateNumbers = await page.locator('[data-card="topography"] .city .gate-n').allTextContents()
check(gateNumbers.length > 0, `the policy lens lights every gate with its rule number (${JSON.stringify(gateNumbers)})`)
check(
  gateNumbers.every((t) => /^\d+$/.test(t)),
  `every lit gate carries a plain rule count, not invented text (${JSON.stringify(gateNumbers)})`,
)

const cityText = await page.locator('[data-card="topography"] .city').textContent()
check(cityText.includes('caught by guest-isolation'), 'the refused boundary names its own rule beside the mark, from the event itself')
check(cityText.includes('caught, no rule named'), 'the escalated pair with no rule label says so plainly rather than guessing one')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
