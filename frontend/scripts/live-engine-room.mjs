// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings as the shelf (#633, rounds 23-25), driven in a real browser.
// The five-station signal path (#490) is replaced wholesale: one page,
// five groups -- your deck, ingest, detection, memory, account -- with
// the two side doors keeping their own place below the shelf.
//
// The room's claims survive the restyle, and none of them is visible
// from the code or from a unit test with a mocked store:
//
//  1. "Every number on the page is arrived traffic" -- a component test
//     renders whatever number it was handed, so it cannot tell a live
//     figure from a placeholder. Feeding real syslog and watching the
//     memory group's buffer count climb can.
//  2. "Tuning unfolds, it does not navigate" -- the detector bench opens
//     from detection's tune row and the page must still be Settings
//     afterwards, the bench folded in place rather than routed to.
//  3. The viewer grammar: #657 gave the room `edit: true`, retiring
//     #490's viewer-readable settings page -- there is no page left for
//     a viewer to read partially, so the claim collapses to "the row is
//     truly gone from a viewer's own menu, and their session never even
//     *asks* for the account list" (GET /api/auth/users is admin-only by
//     the owner's ruling of 2026-08-24, so a viewer issuing it would be
//     a page that loads and immediately 403s). The reduced-but-signed-in
//     experience the room still offers -- both doors gone, everything
//     else fully interactive -- belongs to the `user` tier now, and is
//     covered end to end in live-viewer-surfaces.mjs.

import { chromium } from 'playwright'
import { session, feedSyslog, check, done, goTo, openAccountMenu } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session({ waitForEvents: 40 })

const PEOPLE = '.door:has-text("Who may look in")'

await goTo(page, 'Settings')
await page.waitForFunction(
  () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
  null,
  { timeout: 5000 },
)

// --- The page is the five groups, with both doors below the shelf -------

const groupNames = await page.$$eval('.og h3', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(groupNames) ===
    JSON.stringify(['your deck', 'ingest', 'detection', 'memory', 'account']),
  `the five groups render -- deck, ingest, detection, memory, account -- got ${JSON.stringify(groupNames)}`,
)
const doorNames = await page.$$eval('.doors .door .dname', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(doorNames) === JSON.stringify(['Who may look in', 'Which machines may speak']),
  `both side doors render for an admin -- got ${JSON.stringify(doorNames)}`,
)

// The shelf holds the whole deck, whatever order an earlier scenario
// left it in, and exactly one card wears the sign-in mark.
const shelfNames = await page.$$eval('.stshelf .stcard .nm', (els) => els.map((e) => e.textContent.trim()))
check(
  shelfNames.length === 5 &&
    ['The fall', 'Topography', 'Metrics', 'Stream', 'The docket'].every((n) => shelfNames.includes(n)),
  `the shelf holds all five deck cards -- got ${JSON.stringify(shelfNames)}`,
)
check(
  (await page.$$('.stshelf .stcard.first .lands')).length === 1,
  'exactly one shelf card says sign-in lands on it, and it is the first',
)

// --- Claim 1: every number is arrived traffic ---------------------------
// The memory group's buffer count is the honest one to pin: it is a
// whole number the server publishes, so a placeholder or a stale render
// is visible as a number that does not move when more events land.

const BUFFER_ROW = '.og:has(h3:text-is("memory")) .orow:has-text("event buffer") .ov'

const bufferCount = () =>
  page.$eval(BUFFER_ROW, (el) => {
    const m = el.textContent.replace(/,/g, '').match(/(\d+)/)
    return m ? Number(m[1]) : null
  })

const before = await bufferCount()
check(before !== null && before > 0, `memory says how many events the buffer holds (got ${before})`)

feedSyslog(60, 'live-engine-room')
const climbed = await page
  .waitForFunction(
    (was) => {
      // Plain DOM traversal, not BUFFER_ROW: this runs inside the page,
      // where Playwright's :has-text/:text-is pseudo-selectors do not
      // exist. And deliberately no fallback to "the first number on the
      // page": the ingest group's events/s moves on its own, so a
      // fallback would let this check pass without ever reading the
      // buffer.
      const og = [...document.querySelectorAll('.og')].find(
        (g) => g.querySelector('h3')?.textContent.trim() === 'memory',
      )
      const row = og && [...og.querySelectorAll('.orow')].find((r) => r.textContent.includes('event buffer'))
      const el = row?.querySelector('.ov')
      if (!el) return false
      const m = el.textContent.replace(/,/g, '').match(/(\d+)/)
      return m ? Number(m[1]) > was : false
    },
    before,
    { timeout: 20000 },
  )
  .then(() => true, () => false)
check(climbed, `the buffer count rises as events arrive -- it is live traffic, not a placeholder (was ${before})`)

const ingestText = (await page.textContent('.og:has(h3:text-is("ingest"))')) ?? ''
check(
  /listening/.test(ingestText),
  'ingest names the listening port -- the pathway in is a stated fact',
)
check(
  /[\d.]+\s*events\/s arriving now/.test(ingestText),
  `ingest states a real events/s rate rather than a placeholder`,
)

// The detection group's "N of M on" has to agree with the server's own
// definitions list, whatever an earlier scenario left toggled.
const detectorsRow = (await page.textContent('.og:has(h3:text-is("detection")) .orow:has-text("detectors")'))?.trim() ?? ''
const defs = await page.request
  .get(`${URL_BASE}/api/definitions`)
  .then(async (r) => (await r.json()).definitions ?? [])
const running = defs.filter((d) => d.enabled).length
check(
  detectorsRow.includes(`${running} of ${defs.length} on`),
  `detection counts what the server actually runs (ui "${detectorsRow}", api ${running} of ${defs.length})`,
)

// --- Claim 2: tuning unfolds in place, it does not navigate -------------

await page.click('.olink:has-text("tune")')
await page.waitForSelector('.bench .row')

check(
  (await page.textContent('.page-header h2'))?.trim() === 'Settings',
  'the page is still Settings -- the bench unfolded in place, it did not navigate away',
)
check(
  (await page.$$('.bench .row')).length > 0,
  'the open bench shows the detectors',
)

await page.click('.olink:has-text("close the bench")')
await page.waitForSelector('.bench', { state: 'detached' })
check(true, 'closing the bench folds it away and the page is whole again')

// --- Claim 3: the viewer grammar ----------------------------------------
// #657 retired #490's viewer-readable settings page (the room carries
// `edit: true` now, gated to the user tier), so there is no page left
// for a viewer to open here at all -- goTo(page, 'Settings') would hang
// forever waiting for a menu row that no longer exists. What is left of
// "the viewer grammar" for this room is that the row is truly gone from
// a viewer's own menu, and that their session never even asks for data
// the page would have fetched.

const VIEWER_USER = 'live-viewer-490'
const VIEWER_PASS = 'live-viewer-490-password'

await page.click(`${PEOPLE} .footer-action`)
await page.waitForSelector(`${PEOPLE} .inline-form`)
await page.fill(`${PEOPLE} .inline-form input[type="text"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .inline-form input[type="password"]`, VIEWER_PASS)
// #653: the door creates a "can change things" account by default, so
// the viewer tier this claim is about has to be chosen explicitly --
// which also drives the selector itself, since without it the viewer
// tier has no route in from the UI at all.
await page.selectOption(`${PEOPLE} .inline-form select`, 'viewer')
await page.click(`${PEOPLE} .inline-form .save`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`)
check(
  await page.isVisible(`${PEOPLE} .row:has-text("${VIEWER_USER}") .chip:has-text("view only")`),
  'the people door marks the new account as read-only',
)

const browser = await chromium.launch()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()

// Attached before the first navigation, so it sees every request the
// viewer's session makes from sign-in onwards.
const viewerRequests = []
viewerPage.on('request', (r) => viewerRequests.push(r.url()))

await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

await openAccountMenu(viewerPage)
const viewerMenu = await viewerPage.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
check(
  !viewerMenu.includes('Settings'),
  `Settings is absent from a viewer's menu (#657) -- the room exists to change things, and a viewer cannot -- got ${JSON.stringify(viewerMenu)}`,
)
await viewerPage.keyboard.press('Escape')
await viewerPage.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })

check(
  !viewerRequests.some((u) => u.includes('/api/auth/users')),
  'a viewer never even asks for the account list -- the request that would 403 is not issued at all',
)

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

page.on('dialog', (d) => d.accept())
await page.click(`${PEOPLE} .row:has-text("${VIEWER_USER}") .verb`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
