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

import { session, check, done, feedAndSettle } from './live-browser.mjs'

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

// Reload is load-only, kept uniformly across this helper's three call
// sites (#1061): the middle call, after lanes and the filter-rule table
// are pushed, needs a fresh mount to pick up zonesState and policyState,
// which only refetch from Topography's own mount effect (Topography.svelte)
// and not from the REST pushes themselves. The first and third calls do
// not strictly need a fresh fetch, but sharing one reloading helper is
// simpler and safer than splitting the behaviour by call site.
async function toDistrictStop() {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
  await slider.fill('5') // the district stop; the seven-stop axis is clients 0 .. street 6 (#869)
  await new Promise((r) => setTimeout(r, 900)) // the stop change is a 620ms camera tween
}

// --- Before any push: a boundary-derived district, no gates, and said why --

const preLines = []
for (let i = 0; i < 3; i++) {
  preLines.push(`firewall,info A|walls-pre| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
}
await feedAndSettle(page, ...preLines)
await toDistrictStop()

// The gate count, back after #1022 (owner decision on #1016).
//
// This scenario used to count `.city .gate-n` and expect 0 before any
// push. City.svelte had not drawn `gate-n` since #991, so the count was
// 0 whatever the city did: the check could not fail. #1022's own fix
// moved the hook to the policy lens's gate pill, and round 49 deleted
// the lens, leaving the gate posts as anonymous geometry with no class,
// id or attribute -- "how many gates" could not be asked of the DOM at
// all, so the question was dropped rather than asked vacuously.
//
// The hook is now on the posts themselves, `data-gate`, one per post
// and two posts per gate: the only thing the city draws per gate, and
// the thing that survives a redraw of everything around it. A gate on
// one of the two back edges the camera cannot see draws nothing, so
// this counts what faces the reader -- which before any push is nothing
// at all, because a router that has pushed no rule table has no gates
// to draw and the city must not invent one.
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

  const preGates = await page.locator('[data-card="topography"] .city [data-gate]').count()
  check(preGates === 0, `before any push the walls stand with no gates (${preGates} gate posts)`)
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
// All six guest refusals come from the one host this scenario stands on,
// rather than one each from six. The composer drafts from the busiest
// blocked strand the standing host has, and live-city-stops.mjs -- which
// runs just before this one on the shared instance -- leaves two
// `city-drop` refusals from the internet on 10.0.30.20 as well. One
// guest-isolation event against those two lost, and the composer named
// the sibling's rule instead of this scenario's. Six against two is this
// scenario's own traffic winning on its own terms.
const wallsLines = []
for (let i = 0; i < 6; i++) {
  wallsLines.push(`firewall,info A|walls| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->10.0.40.10:443, len 60`)
  wallsLines.push(`firewall,info D|guest-isolation| forward: in:vlan-guest out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.30.20:5${200 + i}->10.0.10.10:445, len 60`)
}
for (let i = 0; i < 9; i++) {
  wallsLines.push(`firewall,info D|| forward: in:bridge-lan out:vlan-iot, connection-state:new, proto TCP (SYN), ${IOT_UNPLANNED_SRC}:5${300 + i}->10.0.20.20:22, len 60`)
}
await feedAndSettle(page, ...wallsLines)

await toDistrictStop()

// And with a table pushed, the one accept rule on it opens a gate the
// city actually draws -- the other half of the count above, so neither
// reading can go quiet without the other going red.
const gatePosts = await page.locator('[data-card="topography"] .city [data-gate]').count()
check(gatePosts > 0, `the pushed accept rule opens a gate the city draws (${gatePosts} gate posts)`)

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
  // Standing sets City.svelte's `stand` state synchronously, and
  // `data-stop` is derived straight from it, so waiting for the
  // attribute is exact rather than a guess at how long standing takes.
  await page.waitForSelector('[data-card="topography"] .city[data-stop="street"]', { timeout: 10000 })
  return page.locator('[data-card="topography"] .city')
}

// The composer names the rule, and #1035 put its door on the standing
// host's card -- the one card that is always there to be asked, where
// the line card needs a road or a mark to hover and a strand across a
// district pair that already ends in a drop draws neither. Hovering the
// building opens its card whether or not the keyboard walk left the
// focus on it.
async function draftFrom(cid) {
  await page.locator(`[data-card="topography"] .city [data-cid="${cid}"]`).first().hover()
  const draft = page.locator('[data-card="topography"] .city .bcard.hcard [data-draft-rule]')
  await draft.first().waitFor({ state: 'visible', timeout: 5000 }).catch(() => {})
  if ((await draft.count()) === 0) return null
  await draft.first().click()
  const composer = page.locator('[data-card="topography"] .city .composer')
  await composer.first().waitFor({ state: 'visible', timeout: 5000 }).catch(() => {})
  if ((await composer.count()) === 0) return null
  return (await composer.first().textContent()) ?? ''
}

const targetCid = `bridge-lan/${IOT_UNPLANNED_SRC}`
const standCity = await standOn(targetCid)
check(standCity !== null, `the keyboard walk reaches the unplanned pair's own host (${targetCid})`)

check((await standCity.getAttribute('data-stop')) === 'street', 'standing on the host drops the camera to the street stop')
const standDraft = await draftFrom(targetCid)
check(standDraft !== null, 'the standing host card offers `draft the rule ▸`, so the composer can be reached at all (#1035)')
check(
  (standDraft ?? '').includes('caught, no rule named'),
  "standing on its own host, the unplanned pair says so plainly rather than guessing one -- whichever pair the city-wide wall escalates",
)

// No settle sleep: toDistrictStop() below reloads, which discards
// whatever state Escape left behind anyway.
await page.keyboard.press('Escape')

// --- The refused guest boundary, read at the street stop (#865, #1036) ----
//
// This used to be read at the city stop, off the road's own drop label.
// #991 cut that label back to the plain word `dropped`, and #1036 keeps
// it there on every surface: nothing is written on a road or a strand,
// so standing on a refused host must show the plain mark too, not the
// rule that refused it.
//
// The rule's own name lives in the card, and the composer is opened
// through the host card's own door to read it -- the negative case
// above, a drop with no rule label reading "caught, no rule named", is
// the same card, so the pair proves it names the rule when the event
// carries one and declines to invent one when it does not.
await toDistrictStop()
const guestCid = 'vlan-guest/10.0.30.20'
const guestCity = await standOn(guestCid)
check(guestCity !== null, `the keyboard walk reaches a refused guest host (${guestCid})`)
const guestText = (await guestCity.textContent()) ?? ''
check(guestText.includes('dropped'), 'standing on the refused guest host, its strand carries the plain mark')
check(
  !guestText.includes('caught by guest-isolation'),
  `no rule name is written on the strand either (${JSON.stringify(guestText.slice(0, 160))})`,
)
const guestDraft = await draftFrom(guestCid)
check(guestDraft !== null, 'the refused guest host offers `draft the rule ▸` on its own card (#1035)')
check(
  (guestDraft ?? '').includes('caught by guest-isolation'),
  `the composer names the rule that refused the boundary, from the event itself (${JSON.stringify((guestDraft ?? '').slice(0, 240))})`,
)
await page.keyboard.press('Escape')
// composerOpen flips synchronously (City.svelte), so waiting for the
// card to detach is exact rather than a guess at the closing tween.
await page.waitForSelector('[data-card="topography"] .city .composer', { state: 'detached', timeout: 5000 })

// And the road itself says only the plain word: the rule's name is the
// card's to carry, not the drawing's (#991, #1036).
await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('3') // the city stop
await new Promise((r) => setTimeout(r, 900)) // the stop change is a 620ms camera tween
const cityText = (await page.locator('[data-card="topography"] .city').textContent()) ?? ''
check(cityText.includes('dropped'), 'at the city stop the refused road carries the plain mark')
check(
  !cityText.includes('caught by'),
  `no rule name is written on the drawing itself (${JSON.stringify(cityText.slice(0, 160))})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
