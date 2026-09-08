// SPDX-License-Identifier: AGPL-3.0-only
//
// #630, map layer 4, as round 49 (#1016) redrew it: coverage is the
// material, always on. Every boundary-direction is painted by what it
// logs -- solid ink where a rule logs, grey dashed where nothing does,
// white where the operator declared the gap -- and it is drawn, never
// omitted.
//
// Round 49 retired the Coverage lens and the LOGGED / DARK / QUIET
// words on plaques and lane captions (DESIGN.md "Superseded"): the
// material carries it, and nobody should have to click a tab to learn a
// boundary is unlogged. So this scenario reads the drawing itself --
// `.cedge.dark` against a plain `.cedge` -- and reaches the declare
// path the way an operator now does: hover the dark material, pin its
// card, fill the form.
//
// Self-provisioned on purpose: this sorts BEFORE the other topography
// scenarios, and the tables an earlier scenario happens to leave
// (live-router-lookup's, on a full suite run) carry rules whose shape
// this scenario has nothing to say about. A push replaces its kind's whole
// table, so pushing unconditionally is deterministic in both the suite
// and a standalone run -- the third suite run failed exactly here, on
// an inherited table that drew no coverage edges at all.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Standalone runs need the table the reality scenario normally leaves
// behind; a fed instance already has it.
syslog(2, 'topo-coverage-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-coverage', kind: 'ingest', device: DEVICE },
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

// The zones the captions read, then the rules the paint judges -- both
// whole tables, matching what the reality scenario pushes later so the
// lane names stay stable across the suite.
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
  'the zone table is pushed whole',
)
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 0, comment: 'LAN out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443, protocol: 'tcp' },
      { ordinal: 1, comment: 'Nothing unsolicited comes in', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|forward-drop|', log: true, inInterface: 'ether1', outInterface: 'bridge1' },
    ],
  })) === 200,
  'the rule table is pushed whole',
)
await page.reload()

/**
 * Playwright aims at an element's bounding-box centre, and for an SVG
 * line the centre of the box is usually not on the line at all, so the
 * stage swallows the event and the hover never arrives. Sample the box
 * and ask the browser which node is on top. Same helper as
 * live-topography-card-placement.mjs, which drives the same shapes.
 */
async function hoverShape(locator) {
  const handle = await locator.elementHandle()
  if (!handle) return null
  const point = await page.evaluate((el) => {
    const r = el.getBoundingClientRect()
    if (!(r.width > 0) || !(r.height > 0)) return null
    for (let i = 1; i <= 15; i++) {
      for (let j = 1; j <= 15; j++) {
        const x = r.left + (r.width * i) / 16
        const y = r.top + (r.height * j) / 16
        const top = document.elementFromPoint(x, y)
        if (top !== null && (top === el || el.contains(top))) return { x, y }
      }
    }
    return null
  }, handle)
  await handle.dispose()
  if (!point) return null
  await page.mouse.move(point.x, point.y)
  return point
}

const CARD = '[data-card="topography"] .card[role="dialog"]'

await page.click('.rail-name >> text=Topography')
// #869: the altitude now defaults to the city, its centre, which hides
// the 2D map stage the coverage material draws into
// (`hidden={cityStop !== null}` in Topography.svelte). Off the default
// and onto services -- a 2D stop that draws the lane row and its
// boundaries at a size the pointer can reach -- before waiting on
// anything the map draws.
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 10000 })
await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('1')
await page.waitForSelector('[data-card="topography"] .cedge', { timeout: 10000 })
await new Promise((r) => setTimeout(r, 900))

// Round 49's material rule, read straight off the drawing.
//
// `drawnCoverage` (Topography.svelte ~1080) keeps only the directions
// that are dark or quiet: where a rule logs there is nothing for the
// material to say, and the traffic picture carries that boundary in
// verdict ink instead. So the invariant is not "a logged edge draws
// solid coverage" -- it is that coverage material is drawn *only* where
// nothing logs, and the logging side is drawn as traffic.
const darkCount = await page.locator('[data-card="topography"] .cedge.dark').count()
check(darkCount >= 1, `a boundary-direction nothing logs draws dark, not omitted (${darkCount})`)

const dressed = await page.locator('[data-card="topography"] .cedge:not(.dark):not(.quiet)').count()
check(dressed === 0, `no logging boundary is dressed as coverage material -- the traffic picture carries it (${dressed})`)

const traffic = await page.locator('[data-card="topography"] .redge').count()
check(traffic >= 1, `the logging side of the estate is drawn, as its own traffic (${traffic} lines)`)

// The dark boundary is dashed: the material itself, not merely a class
// name that happens to be attached. This is what replaced the words --
// if the stylesheet stopped dashing dark, the map would read as logged
// where nothing logs, and no other check here would notice.
const darkInk = await page.evaluate(
  () => getComputedStyle(document.querySelector('[data-card="topography"] .cedge.dark')).strokeDasharray,
)
check(/[1-9]/.test(darkInk ?? ''), `the dark boundary is drawn dashed (stroke-dasharray ${darkInk})`)

// --- Declare-a-gap (#392), by round 49's interaction (#1016) ---------
//
// The per-edge badge is gone with the lens row. The way in is the card:
// hover the dark material, pin it, and the declare form grows into the
// same card (DESIGN.md "Cards").

const darkEdge = page.locator('[data-card="topography"] .cov-g:has(.cedge.dark)').first()
check((await hoverShape(darkEdge)) !== null, 'the dark material can be pointed at')
await page.waitForSelector(CARD, { timeout: 5000 })
await page.click(`${CARD} .pin`)
await page.waitForSelector(`${CARD} .form`, { timeout: 5000 })

await page.fill(`${CARD} .form input:not([type="checkbox"])`, 'DNS to my own resolver is not logged, on purpose.')
await page.click(`${CARD} .form .go`)

// The edge repaints quiet -- white, not grey dashed -- without a
// reload: the acknowledgement is immediate.
await page.waitForSelector('[data-card="topography"] .cedge.quiet', { timeout: 5000 })
check(true, 'declaring the gap repaints the boundary, without a reload')

// White solid, not grey dashed: the two gaps are different facts and
// the material has to tell them apart on its own, now that the words
// are gone. "Nobody logs this" and "we decided not to" reading the same
// is exactly what round 49's table forbids.
const quietInk = await page.evaluate(
  () => getComputedStyle(document.querySelector('[data-card="topography"] .cedge.quiet')).strokeDasharray,
)
check(
  !/[1-9]/.test(quietInk ?? ''),
  `a declared gap draws solid where dark draws dashed (quiet ${quietInk}, dark ${darkInk})`,
)

// And it is on the record server-side, with who and when.
const decls = await (await page.request.get(`${URL_BASE}/api/coverage/declarations`)).json()
check(
  decls.declarations?.some((d) => d.reason.includes('on purpose') && d.declaredBy),
  'the declaration is stored with its reason and author',
)

// The quiet boundary's own card quotes the reason back and offers the
// way out -- which is where the words the plaque no longer carries now
// live (DESIGN.md "Cards", the quiet card).
await page.mouse.move(4, 4)
await new Promise((r) => setTimeout(r, 300))
const quietEdge = page.locator('[data-card="topography"] .cov-g:has(.cedge.quiet)').first()
check((await hoverShape(quietEdge)) !== null, 'the declared boundary can be pointed at')
await page.waitForSelector(CARD, { timeout: 5000 })
const quietCard = (await page.locator(CARD).textContent()) ?? ''
check(
  quietCard.includes('DNS to my own resolver'),
  `the declared gap's reason is quoted on its own card (${JSON.stringify(quietCard.slice(0, 200))})`,
)

// Removing it sends the boundary honestly back to dark.
await page.click(`${CARD} .acts .hot`)
await page.waitForSelector('[data-card="topography"] .cedge.quiet', { state: 'detached', timeout: 5000 })
const quietLeft = await page.locator('[data-card="topography"] .cedge.quiet').count()
check(quietLeft === 0, 'removing the declaration returns the boundary to dark')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
