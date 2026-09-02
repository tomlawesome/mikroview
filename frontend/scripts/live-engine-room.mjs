// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings as the shelf (#633, rounds 23-25), driven in a real browser.
// The five-station signal path (#490) is replaced wholesale: one page,
// groups reporting live truth. Round 32 (#767) mounted keys (under
// ingest) and people (under account) directly in the card, in the
// card's own row grammar, retiring the two side doors that used to
// carry them below the shelf.
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
//  3. The viewer grammar: affordances absent rather than disabled, the
//     people group absent entirely -- and, the part no DOM assertion
//     covers, a viewer's session never even *asks* for the account
//     list. GET /api/auth/users is admin-only (#657), so a viewer
//     issuing it would be a page that loads and immediately 403s.

import { session, feedSyslog, check, done, goTo, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session({ waitForEvents: 40 })

const PEOPLE = '#people'
const MACHINES = '#keys'

// goTo's own wait (SCENES in live-browser.mjs, waiting for the engineroom card to centre) is what proves arrival --
// this used to also wait for `.page-header h2`, but #700 unmounted PageHeader from EngineRoom.svelte entirely, so
// that selector no longer exists anywhere on the page (#667 group E).
await goTo(page, 'Settings')

// --- The page is the groups, with keys and people mounted in place ------

const groupNames = await page.$$eval('.stsection h3', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(groupNames) ===
    JSON.stringify(['ingest', 'keys', 'detection', 'memory', 'account', 'people']),
  `the groups render in order -- ingest, keys, detection, memory, account, people -- got ${JSON.stringify(groupNames)}`,
)
check(
  await page.locator(`${MACHINES} h3`).isVisible(),
  'the keys group renders for an admin',
)
check(
  await page.locator(`${PEOPLE} h3`).isVisible(),
  'the people group renders for an admin',
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

const BUFFER_ROW = '.stsection:has(h3:text-is("memory")) .orow:has-text("event buffer") .ov'

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
      const og = [...document.querySelectorAll('.stsection')].find(
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

const ingestText = (await page.textContent('.stsection:has(h3:text-is("ingest"))')) ?? ''
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
const detectorsRow = (await page.textContent('.stsection:has(h3:text-is("detection")) .orow:has-text("detectors")'))?.trim() ?? ''
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

// .page-header h2 is gone with #700 (see above); the roll rail's own current-scene marker is what proves the page
// underneath the unfolded bench is still Settings.
check(
  (await page
    .$eval('.roll-rail button.rail-name[aria-current="page"]', (e) => e.textContent.trim())
    .catch(() => null)) === 'Settings',
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
// #657 (predating round 32) narrowed GET /api/tokens to admin-only, the
// same footing GET /api/auth/users was already on -- issuing keys is a
// setup task, not using the product. So keys and people are both absent
// for a viewer, not a read-only rendering of either: this used to assert
// the machines door stayed viewer-readable with its verbs gone, which
// stopped being true the moment #657 landed. Round 32/#767 keeps both
// groups on that same admin-only footing (see EngineRoom.svelte's own
// doc comment on the point).

const VIEWER_USER = 'live-viewer-490'
const VIEWER_PASS = 'live-viewer-490-password'

await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, VIEWER_PASS)
// #653: the form defaults to a "can change things" account, so the
// read-only tier this claim is about has to be chosen explicitly --
// which also drives the selector itself, since without it the viewer
// tier has no route in from the UI at all.
await page.click(`${PEOPLE} .pform button:has-text("can only look")`)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`)
check(
  await page.isVisible(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .pr:has-text("can only look")`),
  'the people group marks the new account as read-only',
)

const browser = await launchBrowser()
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

// goTo's own wait proves arrival -- .page-header h2 is gone with #700, same as the admin half of this scenario
// above.
await goTo(viewerPage, 'Settings')
check(true, 'a viewer can open Settings -- the operational groups stay readable')

// The READ-ONLY declaration itself has nowhere to render today -- #700
// struck the page heading it lived in, and round 30 drew no replacement
// (a gap tracked on #691, not a decision that viewers stop being told).
// Pinned here as the present truth rather than the stale claim that it
// still renders once, in the header.
check(
  (await viewerPage.$$('text=READ-ONLY')).length === 0,
  'no READ-ONLY declaration renders anywhere (#691 gap, not this scenario\'s to fix)',
)

check(
  (await viewerPage.$$(`${MACHINES} h3`)).length === 0,
  'the keys group is absent for a viewer, not read-only and not empty',
)
check(
  (await viewerPage.$$(`${PEOPLE} h3`)).length === 0,
  'the people group is absent for a viewer, not read-only and not empty',
)

check(
  !viewerRequests.some((u) => u.includes('/api/auth/users') || u.includes('/api/tokens')),
  'a viewer never even asks for the account or token list -- the requests that would 403 are not issued at all',
)

const viewerDisabled = await viewerPage.$$eval('.page button, .page input', (els) =>
  els.filter((e) => e.disabled).length,
)
check(viewerDisabled === 0, `nothing on the page is rendered disabled for a viewer -- got ${viewerDisabled}`)

await viewerPage.click('.olink:has-text("tune")')
await viewerPage.waitForSelector('.bench .row')
check((await viewerPage.$$('.bench .cbx')).length === 0, 'the run/pause checkboxes are absent for a viewer')
// .row-knob since #787: the whole row line is the expander now, opening
// the editing panel the old scope knob's form grew into. The class name
// is the one that exists today -- checking for the retired `.scope-knob`
// would be an absence assertion nothing on any page could ever satisfy.
check((await viewerPage.$$('.bench .row-knob')).length === 0, 'the row expanders are absent for a viewer')
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
// Arm-then-confirm (round 28's gesture, retained rather than a confirm()
// dialog): a click arms remove, a second click on the same button
// confirms it.

const remove = page.locator(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .remove`)
await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
