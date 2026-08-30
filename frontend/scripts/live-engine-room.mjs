// SPDX-License-Identifier: AGPL-3.0-only
//
// The engine room (#490), driven in a real browser. Settings stopped
// being filed by noun: one page draws mikroview's own signal path --
// door, store, watchers, flags desk, heralds -- with two side doors
// beside it, per docs/design/screens/settings/DESIGN.md.
//
// Three claims in that record are only true if the running app makes
// them true, and none of them is visible from the code or from a unit
// test with a mocked store:
//
//  1. "Every number on the room is arrived traffic" -- a component test
//     renders whatever number it was handed, so it cannot tell a live
//     figure from a placeholder. Feeding real syslog and watching the
//     store's count climb can.
//  2. "Opening a station zooms, not navigates" -- the page must still be
//     the engine room afterwards, with the other stations collapsed
//     rather than unmounted. A router-level test would pass either way.
//  3. The viewer grammar: chip declared once, affordances absent rather
//     than disabled, the people door absent entirely -- and, the part no
//     DOM assertion covers, a viewer's session never even *asks* for the
//     account list. GET /api/auth/users is admin-only by the owner's
//     ruling of 2026-08-24, so a viewer issuing it would be a page that
//     loads and immediately 403s.

import { chromium } from 'playwright'
import { session, feedSyslog, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session({ waitForEvents: 40 })

const PEOPLE = '.door:has-text("Who may look in")'
const MACHINES = '.door:has-text("Which machines may speak")'

await goTo(page, 'Settings')
await page.waitForFunction(
  () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
  null,
  { timeout: 5000 },
)

// --- The room is the path, in order, with both doors beside it ----------

const stationNames = await page.$$eval('.path .station .nm', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(stationNames) ===
    JSON.stringify(['The door', 'The store', 'The watchers', 'The flags desk', 'The heralds']),
  `the five stations render top to bottom in signal order -- got ${JSON.stringify(stationNames)}`,
)
const doorNames = await page.$$eval('.doors .door .dname', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(doorNames) === JSON.stringify(['Who may look in', 'Which machines may speak']),
  `both side doors render for an admin -- got ${JSON.stringify(doorNames)}`,
)

// --- Claim 1: every number is arrived traffic ---------------------------
// The store's count is the honest one to pin: it is a whole number the
// server publishes, so a placeholder or a stale render is visible as a
// number that does not move when 200 more events land.

const storeCount = () =>
  page.$eval('.path .station:has-text("The store") .live', (el) => {
    const m = el.textContent.replace(/,/g, '').match(/(\d+)/)
    return m ? Number(m[1]) : null
  })

const before = await storeCount()
check(before !== null && before > 0, `the store says how many events it holds (got ${before})`)

feedSyslog(60, 'live-engine-room')
const climbed = await page
  .waitForFunction(
    (was) => {
      // Deliberately no fallback to "the first .live on the page": that
      // is the door's events/s, which moves on its own, so a fallback
      // would let this check pass without ever reading the store.
      const station = [...document.querySelectorAll('.path .station')].find((s) =>
        s.querySelector('.nm')?.textContent.trim() === 'The store',
      )
      const text = station?.querySelector('.live')?.textContent
      if (text === undefined || text === null) return false
      const m = text.replace(/,/g, '').match(/(\d+)/)
      return m ? Number(m[1]) > was : false
    },
    before,
    { timeout: 20000 },
  )
  .then(() => true, () => false)
check(climbed, `the store's count rises as events arrive -- it is live traffic, not a placeholder (was ${before})`)

const doorLive = await page.textContent('.path .station:has-text("The door") .live')
check(
  /\d/.test(doorLive ?? ''),
  `the door states a real events/s rate rather than an em-dash placeholder (got "${(doorLive ?? '').trim()}")`,
)

// The watchers station's "N of M running" has to agree with the server's
// own definitions list, whatever an earlier scenario left toggled.
const watchersLive = (await page.textContent('.path .station:has-text("The watchers") .live'))?.trim() ?? ''
const defs = await page.request
  .get(`${URL_BASE}/api/definitions`)
  .then(async (r) => (await r.json()).definitions ?? [])
const running = defs.filter((d) => d.enabled).length
check(
  watchersLive.includes(`${running} of ${defs.length} running`),
  `the watchers station counts what the server actually runs (ui "${watchersLive}", api ${running} of ${defs.length})`,
)

// --- Claim 2: opening a station zooms, it does not navigate -------------

await page.click('.path .station:has-text("The watchers") .shead')
await page.waitForSelector('.path .station.st-open')

const opened = await page.$eval('.path .station.st-open .nm', (el) => el.textContent.trim())
check(opened === 'The watchers', `the station clicked is the one that opens (got "${opened}")`)
check(
  (await page.$$('.path .station.st-collapsed')).length === 4,
  'the other four stations collapse to slim bars rather than unmounting',
)
check(
  (await page.textContent('.page-header h2'))?.trim() === 'Settings',
  'the page is still the engine room -- the station unfolded in place, it did not navigate away',
)
check(
  await page
    .locator('.st-open .bench .row')
    .first()
    .waitFor({ timeout: 5000 })
    .then(() => true, () => false),
  'the open watchers station shows the detector bench',
)
check(
  (await page.getAttribute('.path .station:has-text("The watchers") .shead', 'aria-expanded')) === 'true',
  'the open station says so to a screen reader',
)

await page.click('.path .station:has-text("The watchers") .shead')
await page.waitForSelector('.path .station.st-open', { state: 'detached' })
check(
  (await page.$$('.path .station.st-rest')).length === 5,
  'clicking the open station again returns the whole room to rest',
)

// --- Claim 3: the viewer grammar ----------------------------------------

const VIEWER_USER = 'live-viewer-490'
const VIEWER_PASS = 'live-viewer-490-password'

// A key for the viewer to read at the machines door. Minted through the
// API rather than the door's own form on purpose: whether the form works
// is live-token-ui.mjs's question, and this scenario must not depend on
// the list happening to be non-empty -- it is not. Every scenario that
// mints one also revokes it, and this one runs before all of them, so
// without this the door is legitimately empty and the check below would
// be asserting on leftovers.
const minted = await page.request
  .post(`${URL_BASE}/api/tokens`, {
    // The same header the app's own writes send -- the server's
    // cross-origin guard refuses a state-changing request without it.
    headers: { 'X-Requested-With': 'mikroview' },
    data: { name: 'engine-room-door-read', kind: 'api' },
  })
  .then((r) => (r.ok() ? r.json() : null))
check(minted !== null, 'a key exists for the viewer to read at the machines door')

await page.click(`${PEOPLE} .footer-action`)
await page.waitForSelector(`${PEOPLE} .inline-form`)
await page.fill(`${PEOPLE} .inline-form input[type="text"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .inline-form input[type="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .inline-form .save`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`)

const browser = await chromium.launch()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()

// Attached before the first navigation, so it sees every request the
// viewer's session makes from sign-in onwards -- not only the ones after
// the room opens.
const viewerRequests = []
viewerPage.on('request', (r) => viewerRequests.push(r.url()))

await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

await goTo(viewerPage, 'Settings')
await viewerPage.waitForFunction(
  () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
  null,
  { timeout: 5000 },
)
check(true, 'a viewer can open the engine room -- the one Admin-group page that is readable')

const chips = await viewerPage.$$eval('.page-header .chip', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(chips) === JSON.stringify(['READ-ONLY — ADMINS EDIT']),
  `read-only is declared exactly once, in the page header -- got ${JSON.stringify(chips)}`,
)

const viewerDoors = await viewerPage.$$eval('.doors .door .dname', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(viewerDoors) === JSON.stringify(['Which machines may speak']),
  `the people door is absent for a viewer, not read-only and not empty -- got ${JSON.stringify(viewerDoors)}`,
)

check(
  !viewerRequests.some((u) => u.includes('/api/auth/users')),
  'a viewer never even asks for the account list -- the request that would 403 is not issued at all',
)

// Absent, never disabled: the letter of the grammar. A greyed-out Revoke
// would satisfy "cannot edit" while breaking the rule the record is
// actually about.
check(
  (await viewerPage.$$(`${MACHINES} .verb`)).length === 0,
  'Mint and Revoke are absent at the machines door for a viewer',
)
const viewerDisabled = await viewerPage.$$eval('.page button, .page input', (els) =>
  els.filter((e) => e.disabled).length,
)
check(viewerDisabled === 0, `nothing in the room is rendered disabled for a viewer -- got ${viewerDisabled}`)

// The facts survive without the handles: a viewer still reads which
// machines may speak and what every watcher is doing, in words.
check(
  await viewerPage
    .locator(`${MACHINES} .row:has-text("engine-room-door-read")`)
    .waitFor({ timeout: 10000 })
    .then(() => true, () => false),
  'a viewer still reads which machines may speak -- the key is named, with its verbs gone',
)
await viewerPage.click('.path .station:has-text("The watchers") .shead')
await viewerPage.waitForSelector('.st-open .bench .row')
check((await viewerPage.$$('.st-open .bench .cbx')).length === 0, 'the run/pause checkboxes are absent for a viewer')
check((await viewerPage.$$('.st-open .bench .scope-knob')).length === 0, 'the scope knobs are absent for a viewer')
const states = await viewerPage.$$eval('.st-open .bench .state', (els) => els.map((e) => e.textContent.trim()))
check(
  states.length > 0 && states.every((s) => s === 'running' || s === 'paused'),
  `every watcher's state survives as a word for a viewer -- got ${JSON.stringify(states.slice(0, 4))}`,
)
const scopeFacts = await viewerPage.$$eval('.st-open .bench .scope-fact', (els) => els.length)
check(scopeFacts > 0, 'a scope reads as a sentence for a viewer rather than vanishing with its knob')

const viewerConsole = []
viewerPage.on('console', (m) => m.type() === 'error' && viewerConsole.push(m.text()))
await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

if (minted?.id) {
  await page.request.delete(`${URL_BASE}/api/tokens/${minted.id}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
}

page.on('dialog', (d) => d.accept())
await page.click(`${PEOPLE} .row:has-text("${VIEWER_USER}") .verb`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
