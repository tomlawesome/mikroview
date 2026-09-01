// SPDX-License-Identifier: AGPL-3.0-only
//
// The Admin group's pages, driven in a real browser. #548 made Users,
// Tokens, Fleet and Entities pages instead of overlays; #490 then
// removed Users, Tokens and Detectors outright, absorbing them into the
// engine room; #647 (round 23) then slimmed the account chip's menu
// itself down to theme, Run setup…, change password, SSO linking, sign
// out and About -- Settings and Entities became deck cards (reached via
// the roll rail, live-browser.mjs's SCENES table), and Fleet became the
// one page left off the deck, reachable only from the phone-width bottom
// bar's Admin group (App.svelte's own comment). What survives from all
// three changes, and needs a real browser rather than a unit test:
//
//  - Every remaining Admin destination is actually reachable -- Settings
//    and Entities via the deck, Fleet via the bottom bar -- and no modal
//    renders anywhere in the group -- a unit test of AccountMenu alone
//    cannot see whether App.svelte still mounts a retired component.
//  - The three absorbed pages are gone with no alias: no destination, and
//    nothing that renders their old headers.
//  - "Run setup…" opens #487's setup modal over whatever page is showing,
//    and does not navigate -- #548's interim view switch to the old
//    wizard page retired with that page.
//  - A viewer's menu follows the absent-never-disabled grammar, proved
//    end to end with a real second account rather than a mocked
//    authState.role.
//
// The room's own read-only grammar is live-engine-room.mjs's job. This
// scenario stops at the menu and the group's page-level facts.

import { session, check, done, goTo, openAccountMenu, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

/** navigates to a deck destination by its visible label and confirms it landed. Used to wait for `.page-header h2`
 * to show the right title too, but #700 unmounted PageHeader from every page it drew (EngineRoom, Fleet, Metrics)
 * and none of Settings/Entities carries a heading of its own any more, so that wait could never resolve (#667 group
 * E). goTo's own wait -- SCENES in live-browser.mjs waits for the destination's own `data-card` to centre -- is what
 * proves arrival now; it is specific to the destination because each deck card carries a different `data-card`. */
async function openAndCheck(label) {
  await goTo(page, label)
  check(true, `${label} is reachable and opens`)
}

/** Fleet has no desktop destination any more -- #647 folded its content into Entities' own deck card and left the
 * standalone page reachable only from the phone-width bottom bar's Admin group (App.svelte's DECK_VIEWS comment:
 * "Fleet alone is left outside it ... reached only from the phone-width bottom bar now"). Proven the way
 * live-nav-bottom-bar.mjs proves any other bottom-bar destination: resize down, open the half-sheet, click Fleet's
 * row inside it, and read the .intro paragraph Fleet.svelte always renders (it carries no heading -- #697/#700 --
 * and no table when the fleet is empty, but the intro text is unconditional). Lands back on Detect/Flags before
 * restoring the desktop viewport, not Expect/Watchlist, because Expect is gated away from a viewer (navGroups.ts)
 * and this helper runs for both roles. */
async function checkFleetFromBottomBar(target) {
  await target.setViewportSize({ width: 390, height: 844 })
  await target.waitForSelector('.bottom-bar', { timeout: 5000 })
  await target.click('.bottom-bar .group-btn .label:text-is("Admin")')
  await target.waitForSelector('[role="dialog"]', { timeout: 5000 })
  const sheetItems = await target.$$eval('.sheet .sheet-item .label', (els) => els.map((e) => e.textContent.trim()))
  await target.click('.sheet .sheet-item .label:text-is("Fleet")')
  await target.waitForFunction(() => document.querySelector('[role="dialog"]') === null, null, { timeout: 5000 })
  await target.waitForSelector('.intro:has-text("Every RouterOS device")', { timeout: 5000 })
  await target.click('.bottom-bar .group-btn .label:text-is("Detect")')
  await target.waitForSelector('.flags-page', { timeout: 5000 })
  await target.setViewportSize({ width: 1280, height: 720 })
  return sheetItems
}

// --- Admin: each page is reachable, the overlays are genuinely gone -----

await openAndCheck('Settings')
await openAndCheck('Entities')
const adminSheetItems = await checkFleetFromBottomBar(page)
check(
  adminSheetItems.includes('Fleet'),
  `Fleet -- folded out of the deck and off the account menu by #647 -- is reachable from the phone-width bottom bar's Admin group instead, got ${JSON.stringify(adminSheetItems)}`,
)
// checkFleetFromBottomBar leaves the deck on Flags; the checks below assume the underlying page is Entities, same
// as before Fleet's check ran.
await openAndCheck('Entities')

check((await page.$$('.modal')).length === 0, 'no modal of any kind renders anywhere in the Admin group')

// --- The absorbed pages left nothing behind (#490) ----------------------
// Removals here are wholesale: no destination, no alias, no stub.
// Checking the menu's own labels is the honest test -- a `:has-text()` click that
// finds nothing would just time out and say "timeout", not "the row is
// correctly absent". Settings itself left this menu too, in #647 (it is a
// deck destination now, proved above by openAndCheck), so this block only
// checks Users/Tokens/Detectors' absence -- not for anything that
// replaced them here.
await openAccountMenu(page)
const adminLabels = await page.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
await page.keyboard.press('Escape')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
for (const gone of ['Users', 'Tokens', 'Detectors']) {
  check(
    !adminLabels.some((l) => l === gone),
    `${gone} has no menu row of its own any more -- the menu shows ${JSON.stringify(adminLabels)}`,
  )
}

// --- Run setup… opens the modal, and is not a page (#487) --------------
// The row before this one left the app on Entities, and it must still be
// there underneath: an action does not navigate. Checked against the roll
// rail's own current-scene marker rather than the account menu's
// button.row.on -- #647 emptied that menu down to Run setup… alone, which
// carries no `view` of its own (AccountMenu.svelte's operate table), so
// `.row.on` can never match any row any more and would always read as
// "nothing is current" regardless of what is actually showing.

await goTo(page, 'Run setup…')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible', timeout: 5000 })
check(true, 'Run setup… opens the setup modal')
await page.keyboard.press('Escape') // the wizard modal owns Escape; close it before reading the rail
await wizard.waitFor({ state: 'detached', timeout: 5000 })
const stillCurrent = await page
  .$eval('.roll-rail button.rail-name[aria-current="page"]', (e) => e.textContent.trim())
  .catch(() => null)
check(
  stillCurrent === 'Entities',
  `the page underneath is still the one the operator was on -- an action does not navigate (got ${JSON.stringify(stillCurrent)})`,
)
check(
  !(await page.locator('main .setup').count()),
  'no wizard page route survives -- the view was removed wholesale, not aliased',
)

// Explicit close, so the rest of this scenario is not driving the page
// through a focus trap.
await page.keyboard.press('Escape')
await wizard.waitFor({ state: 'detached', timeout: 5000 })

// --- A real viewer account, created the way an admin actually would ----
// Through the engine room's people door now, which is where adding an
// account lives since #490.

const VIEWER_USER = 'live-viewer-548'
const VIEWER_PASS = 'live-viewer-548-password'
const PEOPLE = '.door:has-text("Who may look in")'

await openAndCheck('Settings')
await page.click(`${PEOPLE} .footer-action`)
await page.waitForSelector(`${PEOPLE} .inline-form`)
await page.fill(`${PEOPLE} .inline-form input[type="text"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .inline-form input[type="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .inline-form .save`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`)
check(true, `the viewer account "${VIEWER_USER}" is created from the engine room's people door`)

// --- Viewer: absent, never disabled -------------------------------------

const browser = await launchBrowser()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

await openAccountMenu(viewerPage)
const viewerLabels = await viewerPage.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Users', 'Tokens', 'Detectors', 'Entities', 'Run setup…']) {
  check(
    !viewerLabels.some((l) => l === absent),
    `${absent} is absent from a viewer's menu -- the menu shows ${JSON.stringify(viewerLabels)}`,
  )
}

// A disabled stub would satisfy "absent" in spirit while breaking the
// letter of it -- prove nothing in the viewer's menu is disabled either,
// while it is still open from the read above.
const viewerDisabled = await viewerPage.$$eval('.account .menu button.row', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(viewerDisabled.length === 0, `no menu row is disabled for a viewer -- got ${JSON.stringify(viewerDisabled)}`)
check((await viewerPage.$$('.modal')).length === 0, 'no modal renders for a viewer either')

await viewerPage.keyboard.press('Escape')
await viewerPage.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })

// Settings and Fleet both left this menu in #647 too -- Settings is a deck destination now (SCENES), Fleet is
// bottom-bar-only -- so "still reachable for a viewer, with no admin gate" is proved the same two ways the admin
// half of this scenario proved it above, not by reading this menu.
await goTo(viewerPage, 'Settings')
check(true, "Settings is reachable for a viewer too -- the one Admin destination that is deliberately viewer-readable")

const viewerSheetItems = await checkFleetFromBottomBar(viewerPage)
check(
  viewerSheetItems.includes('Fleet'),
  `Fleet -- an Admin-group destination with no admin gate -- is still reachable for a viewer, got ${JSON.stringify(viewerSheetItems)}`,
)

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

await openAndCheck('Settings')
page.on('dialog', (d) => d.accept())
await page.click(`${PEOPLE} .row:has-text("${VIEWER_USER}") .verb`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
