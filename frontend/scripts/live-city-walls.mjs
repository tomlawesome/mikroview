// SPDX-License-Identifier: AGPL-3.0-only
//
// #865, walls and gates from the rule set, against a running instance.
// The unit tests prove the derivations on fixtures (gates.ts, walls.ts,
// escalate.ts); this walks the real thing: before any rule table is
// pushed the district says so on its plaque and in its own accessible
// name, and a drop road ends at the wall with the refusing rule named
// on the composer card.
//
// The gate count that used to sit here went with #1022 -- see the note
// above `preRules` for what was wrong with it and why nothing has
// replaced it yet.
//
// The no-rule-label pair (#969) is read by standing on its own host
// rather than off the city-wide escalated wall: `worstUnplannedOf`
// (reality.ts) picks the single busiest unplanned pair on the whole
// device, ratified and correct (#969, decision on the issue), and on a
// live instance shared with every other scenario in a gate run some
// earlier scenario's own traffic is routinely busier than the nine
// events this one feeds. Standing on the host that sent them reads its
// own reach overlay instead (City.svelte's dropMarks, ~947) -- drawn
// for every one of *that host's* blocked strands regardless of which
// pair the city-wide wall escalates, the same wording and the same code
// path either way.

import { session, check, done, feedRaw } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
// The one host that sends the unplanned, no-rule-label traffic below --
// its own IP, not shared with any of the accepted-traffic hosts, so
// standing on it reads a clean single strand.
const IOT_UNPLANNED_SRC = '10.0.10.39'

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
  await slider.fill('5') // the district stop; the seven-stop axis is clients 0 .. street 6 (#869)
  await new Promise((r) => setTimeout(r, 900))
}

// --- Before any push: a boundary-derived district, no gates, and said why --

for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info A|walls-pre| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
}
await new Promise((r) => setTimeout(r, 900))
await toDistrictStop()

// #1022, and why there is no gate count here any more.
//
// This scenario used to assert that a router with no pushed rule table
// draws no gates, by counting `.city .gate-n` and expecting 0.
// City.svelte has not drawn `gate-n` since #991, so the count was 0
// whatever the city did: the check could not fail, and proved nothing.
//
// It is removed rather than repaired, because the city currently offers
// nothing honest to point it at. Gate posts are pushed into the drawing
// as anonymous geometry with no class, id or data attribute of their
// own, so "how many gates" cannot be asked of the DOM directly. The one
// tell the design gives a gate is its lamp -- DESIGN.md, "Lamp on a
// gate | the rule logs | a lit post", and round 49's table, "City gate |
// logged: accent posts, one lamp" -- and on a live instance with a
// logging accept rule pushed (`lan to servers`, below) the city draws
// no `circle.lamp` at all, at any city stop, while its own walls report
// `logged`. Measured 2026-09-07 on a clean instance at 844cb48.
//
// So every available reading is either absent or constant, and a check
// written against one now would be as vacuous as the one it replaced.
//
// #1022's own fix (836fffab) put a `data-gate` hook on the policy
// lens's gate pill, which was the only named gate element there was.
// Round 49 removes the policy lens from both map surfaces, so that pill
// and its hook are gone with it, and the DOM is back to offering
// nothing countable. Repairing this needs a stable hook on the gate
// posts themselves, in City.svelte, and belongs with whoever owns that
// file. What this scenario still proves about the same honesty is
// below: the plaque and the district both say a table was never pushed.
const preRules = await page.request.get(`${URL_BASE}/api/routeros/${DEVICE}/rules`)
const prePushed = preRules.ok() && (await preRules.json()).available
if (!prePushed) {
  // Round 49 (#1016) took the shouted LOGGED / DARK / NO RULES PUSHED
  // off the plaque; `no rule table pushed` stayed, dim and lower case,
  // because it is a different fact from dark (City.svelte ~2237).
  const preText = await page.locator('[data-card="topography"] .city').textContent()
  check(preText.includes('no rule table pushed'), 'before any push, a district plaque says plainly that no rule table has been pushed yet')

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
// by its own named rule; a third, unrelated pair (lan -> iot), one host
// only, crosses with no rule anticipating it at all and no rule label on
// the drop, so standing on that host must read "no rule named" rather
// than inventing one. Kept off the guest pair deliberately: a road folds
// both directions of the same pair together (layout.ts), so a rule
// label on one direction would otherwise paper over the other's
// silence.
for (let i = 0; i < 6; i++) {
  feedRaw(`firewall,info A|walls| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
  feedRaw(`firewall,info D|guest-isolation| forward: in:vlan-guest out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.30.2${i}:5${200 + i}->10.0.10.10:445, len 60`)
}
for (let i = 0; i < 9; i++) {
  feedRaw(`firewall,info D|| forward: in:bridge-lan out:vlan-iot, connection-state:new, proto TCP (SYN), ${IOT_UNPLANNED_SRC}:5${300 + i}->10.0.20.20:22, len 60`)
}

await toDistrictStop()

// --- The no-rule-label pair, read off its own host (#969) ------------------
//
// Still at the district stop. Walk the keyboard the same way
// live-city-reach.mjs does, except this district carries more than one
// building, so every one of them is tried in turn rather than assuming
// the first is the right one.
async function standOn(cid) {
  const firstDistrict = page.locator('[data-card="topography"] .city .plate[tabindex="0"]')
  await firstDistrict.focus()
  let found = false
  for (let d = 0; d < 6 && !found; d++) {
    for (let b = 0; b < 8; b++) {
      await page.keyboard.press('ArrowRight')
      const at = await page.evaluate(() => document.activeElement?.getAttribute('data-cid') ?? null)
      if (at === cid) {
        found = true
        break
      }
    }
    if (found) break
    await page.keyboard.press('ArrowDown')
  }
  if (!found) return null
  await page.keyboard.press('Enter')
  await new Promise((r) => setTimeout(r, 900))
  return page.locator('[data-card="topography"] .city')
}

const targetCid = `bridge-lan/${IOT_UNPLANNED_SRC}`
const standCity = await standOn(targetCid)
check(standCity !== null, `the keyboard walk reaches the unplanned pair's own host (${targetCid})`)

check((await standCity.getAttribute('data-stop')) === 'street', 'standing on the host drops the camera to the street stop')
const standText = await standCity.textContent()
check(standText.includes('caught, no rule named'), "standing on its own host, the unplanned pair says so plainly rather than guessing one -- whichever pair the city-wide wall escalates")

await page.keyboard.press('Escape')
await new Promise((r) => setTimeout(r, 900))

// --- The named refusal, read off the composer card (#865, #991) -----------
//
// This used to be read at the city stop, off the road's own drop label.
// #991 cut that label back to the plain word `dropped` and moved the
// refusing rule's name into the composer card in the reach
// (City.svelte ~2299, "it's been asking · tcp/445 · 14× · caught by
// guest-isolation") -- the same fact from the same event, one surface
// along. Round 49 kept it there: nothing is written on a road.
//
// So it is read where it now lives: standing on a guest host whose
// traffic the named rule refused. The negative case above -- a drop with
// no rule label reading "caught, no rule named" -- is the same card, so
// the pair proves the card names the rule when the event carries one and
// declines to invent one when it does not.
await toDistrictStop()
const guestCid = 'vlan-guest/10.0.30.20'
const guestCity = await standOn(guestCid)
check(guestCity !== null, `the keyboard walk reaches a refused guest host (${guestCid})`)
const guestText = await guestCity.textContent()
check(
  guestText.includes('caught by guest-isolation'),
  'the refused boundary names its own rule on the composer card, from the event itself',
)

// And the road itself says only the plain word: the rule's name is the
// card's to carry, not the drawing's (#991).
await page.keyboard.press('Escape')
await new Promise((r) => setTimeout(r, 900))
await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('3') // the city stop
await new Promise((r) => setTimeout(r, 900))
const cityText = (await page.locator('[data-card="topography"] .city').textContent()) ?? ''
check(cityText.includes('dropped'), 'at the city stop the refused road carries the plain mark')
check(
  !cityText.includes('caught by'),
  `no rule name is written on the drawing itself (${JSON.stringify(cityText.slice(0, 160))})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
