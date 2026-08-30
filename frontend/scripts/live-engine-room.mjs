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
// #653: the door creates a "can change things" account by default, so
// the read-only tier this claim is about has to be chosen explicitly --
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
// viewer's session makes from sign-in onwards -- not only the ones after
// Settings opens.
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
check(true, 'a viewer can open Settings -- the one Admin-group page that is readable')

const chips = await viewerPage.$$eval('.page-header .chip', (els) => els.map((e) => e.textContent.trim()))
check(
  // #653: the chip names no tier any more -- with three of them,
  // "ADMINS EDIT" was wrong in both directions, and it only ever renders
  // for someone who cannot edit the page anyway.
  JSON.stringify(chips) === JSON.stringify(['READ-ONLY']),
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
check(viewerDisabled === 0, `nothing on the page is rendered disabled for a viewer -- got ${viewerDisabled}`)

// The facts survive without the handles: a viewer still reads which
// machines may speak and what every detector is doing, in words.
check(
  await viewerPage
    .locator(`${MACHINES} .row:has-text("engine-room-door-read")`)
    .waitFor({ timeout: 10000 })
    .then(() => true, () => false),
  'a viewer still reads which machines may speak -- the key is named, with its verbs gone',
)
await viewerPage.click('.olink:has-text("tune")')
await viewerPage.waitForSelector('.bench .row')
check((await viewerPage.$$('.bench .cbx')).length === 0, 'the run/pause checkboxes are absent for a viewer')
check((await viewerPage.$$('.bench .scope-knob')).length === 0, 'the scope knobs are absent for a viewer')
const states = await viewerPage.$$eval('.bench .state', (els) => els.map((e) => e.textContent.trim()))
check(
  states.length > 0 && states.every((s) => s === 'running' || s === 'paused'),
  `every detector's state survives as a word for a viewer -- got ${JSON.stringify(states.slice(0, 4))}`,
)
const scopeFacts = await viewerPage.$$eval('.bench .scope-fact', (els) => els.length)
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
