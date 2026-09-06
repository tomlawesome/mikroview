// SPDX-License-Identifier: AGPL-3.0-only
//
// #806: WATCH BROKEN needs a per-boundary notion of a broken watch.
// live-waterfall.mjs already proves the fall draws WATCHED against real
// cadence and a dark boundary honestly; this scenario is the other half
// the issue's done-when names explicitly -- a watcher genuinely scoped to
// a boundary (the new Boundary{Chain, InInterface, OutInterface} model),
// its ring broken by a real window that closed with nothing in it, reads
// WATCH BROKEN on that band, while a plain logged boundary next to it
// still reads WATCHED.
//
// Deliberately its own file rather than added to live-waterfall.mjs: that
// file is under active diagnosis for #1007 (an unrelated flake flipping
// red/green against unchanged code), and adding coverage there would move
// the flake's own signature mid-investigation. Nothing here shares state
// with it -- own device/token, own filter-rule push on two boundaries
// (ether20/bridge20, ether21/bridge21) no other scenario in this suite
// names, own watchlist entry, and a full cleanup (entry deleted, table
// reset to the bare non-logging rule the watchlist-*.mjs scenarios
// already leave the shared instance in) at the end -- so wherever this
// sorts among the filename-ordered suite, nothing downstream inherits a
// table or entry it did not expect.
//
// Not covered here: widening a broken watch's window back out and
// proving the ring heals (the build plan's own closing step). This
// scenario's job, per the ask that added it, is the two captions against
// a real broken watcher; healing is exercised at the unit level
// (internal/watchlist's nights.go tests) and left as a follow-up here if
// a live proof is ever wanted.

import { session, feedSyslog, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

feedSyslog(3, 'fall-watch-broken-probe')
const { page, consoleErrors } = await session({ landing: 'fall' })

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// The ingest token must be scoped to exactly the device feedSyslog's
// events carry, or the pushed filter table attaches to a different
// device and both boundaries below report 'unknown' rather than
// 'observed' -- same lookup live-waterfall.mjs uses for the same reason.
let device
for (let i = 0; i < 40 && !device; i++) {
  await new Promise((r) => setTimeout(r, 250))
  device = (await api('GET', '/api/devices')).body?.devices?.[0]?.id
}
check(!!device, `the instance reports the device events arrive from (${device})`)

const tokenRes = await api('POST', '/api/tokens', { name: 'fall-watch-broken', kind: 'ingest', device })
check(tokenRes.status === 201, `an ingest token is issued (${tokenRes.status})`)
const token = tokenRes.body?.value

async function push(records) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'filter-rule', page: 1, pages: 1, records }),
  })
  return res.status
}

// Two boundaries, neither interface pair used by any other scenario in
// this suite: both log, so both read 'observed' rather than 'dark' --
// the only thing that will tell them apart on the fall is which one
// carries a broken watch.
check(
  (await push([
    { ordinal: 0, comment: 'watch-broken probe', chain: 'forward', action: 'drop', log: true, inInterface: 'ether20', outInterface: 'bridge20' },
    { ordinal: 1, comment: 'watch-broken probe (healthy)', chain: 'forward', action: 'drop', log: true, inInterface: 'ether21', outInterface: 'bridge21' },
  ])) === 200,
  'the two-boundary filter table is accepted',
)

// A watch window that opens a couple of minutes from now and closes a
// minute later, computed from this process's own clock rather than the
// server's -- the live-check harness runs both in the same container, so
// clock skew is not a real risk, and the couple of minutes' buffer
// absorbs the both being a few seconds off. Refuses outright rather than
// silently building a wrong window within a few minutes of UTC midnight,
// where "3 minutes from now" would wrap onto a date whose clock-only
// (Days-empty) window reads as a different, already-past occurrence.
function computeWindow(bufferMin, lengthMin) {
  const now = new Date()
  const mins = now.getUTCHours() * 60 + now.getUTCMinutes()
  if (mins + bufferMin + lengthMin >= 1440) {
    throw new Error('too close to UTC midnight to safely compute a watch window for this scenario -- rerun in a few minutes')
  }
  const fmt = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`
  return {
    start: fmt(mins + bufferMin),
    end: fmt(mins + bufferMin + lengthMin),
    closesAt: new Date(now.getTime() + (bufferMin + lengthMin) * 60000),
  }
}

const PORT = 47001
const win = computeWindow(2, 1)

const entryRes = await api('POST', '/api/definitions', {
  name: 'fall-watch-broken sentinel',
  intent: 'expectation',
  kind: 'declarative',
  expectation: {
    ports: [PORT],
    boundary: { chain: 'forward', inInterface: 'ether20', outInterface: 'bridge20' },
    window: { start: win.start, end: win.end },
  },
})
check(entryRes.status === 201, `a boundary-scoped watch is created (${entryRes.status})`)
const id = entryRes.body?.id

async function ringFor(entryId) {
  const got = await api('GET', '/api/definitions')
  const d = (got.body?.definitions ?? []).find((d) => d.id === entryId)
  return { ring: d?.expectation?.ring, coverage: d?.coverage }
}

// Wait out the window itself (real time -- FillWatchNights is lazy, not a
// timer, so nothing records the empty occurrence until it has actually
// closed), then poll the read path (which runs FillWatchNights on every
// call) until the ring agrees. A generous ceiling above the window's own
// close, not a tight one -- this is proving a real close happened, not
// this scenario's patience.
const waitMs = win.closesAt.getTime() - Date.now() + 5000
if (waitMs > 0) await new Promise((r) => setTimeout(r, waitMs))

let settled = null
const deadline = Date.now() + 60000
while (Date.now() < deadline) {
  settled = await ringFor(id)
  if (settled.ring?.broken === true) break
  await new Promise((r) => setTimeout(r, 2000))
}
check(settled?.ring?.broken === true, `the server records the ring broken -- last saw ${JSON.stringify(settled)}`)
check(settled?.coverage === 'covered', `the entry's own boundary is covered by the rule pushed on it -- got ${settled?.coverage}`)

// --- The fall itself: WATCH BROKEN on the scoped boundary, WATCHED on
// the plain one ------------------------------------------------------------
await page.waitForFunction(
  () =>
    [...document.querySelectorAll('.fall .band .band-label')].some((e) => e.textContent.includes('ether20') && e.textContent.includes('bridge20')),
  null,
  { timeout: 25000 },
)

const brokenBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether20 → bridge20")') })
const healthyBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether21 → bridge21")') })
await healthyBand.waitFor({ timeout: 10000 })

check(
  (await brokenBand.locator('.band-caption').first().textContent())?.trim() === 'WATCH BROKEN',
  'the boundary carrying the broken watch reads WATCH BROKEN',
)
check(
  (await brokenBand.locator('.band-caption.bad').count()) > 0,
  'WATCH BROKEN renders in the alarm ink, not colour alone',
)
check(
  (await healthyBand.locator('.band-caption').first().textContent())?.trim() === 'WATCHED',
  'the plain logged boundary next to it still reads WATCHED',
)
check(
  !(await brokenBand.evaluate((el) => el.classList.contains('dark'))),
  'the broken boundary is logged, not dark -- WATCH BROKEN and DARK are different claims',
)

// --- Cleanup: leave no state a later scenario inherits ---------------------
await api('DELETE', `/api/definitions/${id}`)
check(
  (await push([{ ordinal: 0, chain: 'forward', action: 'drop' }])) === 200,
  'the table is reset to non-logging for whatever runs next',
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
