// SPDX-License-Identifier: AGPL-3.0-only
//
// #981: the flag mark on a city building, driven by the data and by
// nothing else.
//
// The ruling this walks (owner, 2026-09-08): a mark exists only while
// there is something behind it, in both views, and there is no toggle --
// "something that's always there is easy to ignore; if it's not always
// there you know it's there for a reason." A unit test can render a
// building with `flags: 2` set by hand; only this can show a real
// detector raising a real flag against a real host, the building going
// red, and the mark disappearing again when the flag is judged.
//
// One address of its own, on a lane its neighbours already declare.
// This runs on an instance shared with every other city scenario, and
// both of that instance's caps have to be respected:
//
//   - a district draws at most eight buildings (MAX_BUILDINGS), so the
//     host this scenario is about could be the one that does not fit and
//     the check would fail for a reason that has nothing to do with
//     marks. A real port scan is the busiest thing on any lane here --
//     tens of events against the neighbours' handful -- and a zone
//     orders its hosts busiest-first before that cut, so this one is
//     always inside it. The address is used by no other scenario, which
//     is what keeps other traffic from marking or unmarking it.
//   - the lane row itself is capped at five, busiest first
//     (`zonesState.zones`), over the whole shared event buffer. A lane
//     of this scenario's own would sit in that row for the rest of the
//     run carrying more events than any real lane, and evict one that a
//     later scenario needs -- which is exactly what `vlan-mark` did to
//     live-city-reach's `wlan-wsh` and live-city-walls's `vlan-guest`.
//     So the scan stands on `vlan-iot`, the lane live-city-reach,
//     live-city-stops and live-city-walls all declare with this same
//     range and name, and no sixth lane is ever created.

import { session, check, done, feedRaw, waitForFlag } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
// The lane is shared with this file's neighbours on purpose (see the
// header); the address is not -- it is used by no other scenario, so
// nothing else's traffic can mark or unmark this host.
const LANE = 'vlan-iot'
const MARKED_IP = '10.0.20.44'
const PORTS = 25

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
  data: { name: 'live-city-marks', kind: 'ingest', device: DEVICE },
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
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: LANE, comment: 'IoT' },
    ],
  })) === 200,
  'the lane the marked host stands on is pushed',
)

// A real port scan, so a real detector raises the flag -- nothing here
// synthesises one. Same shape as live-env.sh's own portscan feeder, from
// a lane address so the host has a building to be marked on.
for (let i = 0; i < PORTS; i++) {
  feedRaw(
    `firewall,info D|mark-scan| forward: in:${LANE} out:ether1, connection-state:new, proto TCP (SYN), ${MARKED_IP}:${40000 + i}->203.0.113.9:${1000 + i}, len 60`,
  )
}

// Server-side first (#354): a locator timeout cannot say whether the
// scan raised nothing or merely had not rendered yet.
const raised = await waitForFlag(page, MARKED_IP)
check(raised.ok, raised.message)

async function toStreetStop() {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('6') // street
  await new Promise((r) => setTimeout(r, 1200))
}

const BUILDING = `[data-card="topography"] .city .blk[aria-label*="${MARKED_IP}"]`
const BUILDING_CID = `${LANE}/${MARKED_IP}`

/**
 * Walk the keyboard onto the marked building and leave the focus there.
 *
 * Every focus move recentres the camera (City.svelte's `focusItem`), and
 * that is the whole point of walking rather than reaching straight for
 * the element: the street stop's own camera centres on the *first*
 * district (`centreFor` with nothing in focus), and on a shared instance
 * that is whichever lane is busiest, not necessarily this one. A
 * building one district over is then off the stage entirely -- present,
 * visible and stable in the DOM, and outside the viewport, which is a
 * hover Playwright can never land however long it retries. Scrolling
 * does not help either: the stage does not scroll, the camera moves.
 *
 * The same walk live-city-walls.mjs uses, and for the same reason.
 */
async function walkTo(cid) {
  const firstDistrict = page.locator('[data-card="topography"] .city .plate[tabindex="0"]')
  await firstDistrict.focus()
  for (let d = 0; d < 8; d++) {
    for (let b = 0; b < 8; b++) {
      await page.keyboard.press('ArrowRight')
      const at = await page.evaluate(() => document.activeElement?.getAttribute('data-cid') ?? null)
      if (at === cid) return true
    }
    await page.keyboard.press('ArrowDown')
  }
  return false
}

/** What the building at MARKED_IP is wearing, straight off the DOM. */
const readMark = () =>
  page.evaluate((sel) => {
    const g = document.querySelector(sel)
    if (!g) return null
    return {
      fill: g.querySelector('.mk-fill')?.getAttribute('opacity') ?? null,
      rims: g.querySelectorAll('path.mk-rim').length,
      rimInk: g.querySelector('path.mk-rim')?.getAttribute('stroke') ?? null,
      watches: g.querySelectorAll('path.mk-watch').length,
      // "Get rid of them completely" -- no disc, ring, number or glyph
      // on the map, at any stop.
      discs: g.querySelectorAll('circle').length,
      words: g.querySelectorAll('text').length,
      aria: g.getAttribute('aria-label') ?? '',
    }
  }, BUILDING)

await toStreetStop()

// Nothing switches a mark on or off any more. The one button left on
// the row is the port pill (#1055), which is a filter, not a switch:
// it is the only control there, and it stands idle.
const overlays = page.locator('[data-card="topography"] [aria-label="Map overlays"] button')
const pills = await overlays.count()
check(pills === 1, `the only button on the overlay row is the port pill (${pills} buttons)`)
check((await overlays.first().textContent())?.trim() === '⌕ port', 'and it is the port pill, idle -- no flag or watch switch remains')

await page.waitForSelector(BUILDING, { timeout: 15000 })
const marked = await readMark()
check(marked !== null, `the flagged host has a building in the city (${MARKED_IP})`)
if (marked) {
  check(marked.fill === '0.45', `one open flag re-stamps the device in alarm ink at 0.45 (opacity ${marked.fill})`)
  check(marked.rims === 1, `exactly one silhouette is stroked round the symbol, not one per face (${marked.rims})`)
  check(marked.rimInk === 'var(--alarm)', `the silhouette is stroked in the alarm ink (${marked.rimInk})`)
  check(marked.watches === 0, `nothing watches this host, so no watch line is drawn (${marked.watches})`)
  check(marked.discs === 0 && marked.words === 0, `no disc, ring or number on the building (${marked.discs} circles, ${marked.words} texts)`)
  check(marked.aria.includes('1 flag'), `a screen reader is told what the mark means (${JSON.stringify(marked.aria)})`)
}

// The count is a word on the click card and nowhere else. Walked to
// first, so the camera is on the building rather than wherever the stop
// happened to land -- see walkTo.
const walked = await walkTo(BUILDING_CID)
check(walked, `the keyboard walk reaches the marked building (${BUILDING_CID})`)
await new Promise((r) => setTimeout(r, 900)) // the recentre is a 620ms tween
await page.hover(BUILDING)
await new Promise((r) => setTimeout(r, 400))
const counts = (await page.locator('[data-card="topography"] .city .hcard [data-marks]').textContent().catch(() => null))?.replace(/\s+/g, ' ').trim()
check(counts === '1 flag', `the click card says how many, as plain words (${JSON.stringify(counts)})`)
await page.mouse.move(10, 10)

// --- Judge the flag, and the mark must go with it -------------------------

const flagsRes = await page.request.get(`${URL_BASE}/api/flags`)
const open = ((await flagsRes.json()).flags ?? []).find((f) => f.target === MARKED_IP && !f.cleared)
check(!!open, `the open flag is readable from the API before it is judged (${open?.id})`)

if (open) {
  const judged = await page.request.post(`${URL_BASE}/api/flags/${encodeURIComponent(open.id)}/verdict`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: { verdict: 'checked' },
  })
  check(judged.ok(), `checking the flag clears it server-side (${judged.status()})`)

  await toStreetStop()
  await page.waitForSelector(BUILDING, { timeout: 15000 })
  const after = await readMark()
  check(after !== null, 'the building is still there once the flag is cleared -- only the mark goes')
  check(after?.rims === 0 && after?.fill === null, `nothing is behind it any more, so it wears nothing (${JSON.stringify(after)})`)
  check(!(after?.aria ?? '').includes('flag'), `and a screen reader is told nothing about flags either (${JSON.stringify(after?.aria)})`)
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
