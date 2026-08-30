// SPDX-License-Identifier: AGPL-3.0-only
//
// The Admin group's pages, driven in a real browser. #548 made Users,
// Tokens, Fleet and Entities pages instead of overlays; #490 then
// removed Users, Tokens and Detectors outright, absorbing them into the
// engine room. What survives from both changes, and needs a real
// browser rather than a unit test:
//
//  - Every remaining Admin destination is actually reachable from the
//    atlas, and no modal renders anywhere in the group -- a unit test
//    of AtlasNav alone cannot see whether App.svelte still mounts a
//    retired component.
//  - The three absorbed pages are gone with no alias: no destination, and
//    nothing that renders their old headers.
//  - "Run setup…" opens #487's setup modal over whatever page is showing,
//    and does not navigate -- #548's interim view switch to the old
//    wizard page retired with that page.
//  - A viewer's atlas follows the absent-never-disabled grammar, proved
//    end to end with a real second account rather than a mocked
//    authState.role.
//
// The room's own read-only grammar is live-engine-room.mjs's job. This
// scenario stops at the atlas and the group's page-level facts.

import { chromium } from 'playwright'
import { session, check, done, goTo, openAtlas } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

/** opens an atlas destination and waits for the new page's header to actually land -- a plain waitForSelector would resolve
 * against the *previous* page's `.page-header h2` while the transition is still in flight, since both pages share
 * the same selector. */
async function openAndCheck(label, headerText) {
  await goTo(page, label)
  await page.waitForFunction(
    (want) => document.querySelector('.page-header h2')?.textContent.trim() === want,
    headerText,
    { timeout: 5000 },
  )
  check(true, `the atlas's ${label} destination opens the ${headerText} page`)
}

// --- Admin: each page is reachable, the overlays are genuinely gone -----

await openAndCheck('The engine room', 'The engine room')
await openAndCheck('Fleet', 'Fleet')
await openAndCheck('Entities', 'Entities')

check((await page.$$('.modal')).length === 0, 'no modal of any kind renders anywhere in the Admin group')

// --- The absorbed pages left nothing behind (#490) ----------------------
// Removals here are wholesale: no destination, no alias, no stub.
// Checking the atlas's own labels is the honest test -- a `:has-text()` click that
// finds nothing would just time out and say "timeout", not "the row is
// correctly absent".
await openAtlas(page)
const adminLabels = await page.$$eval('.atlas .ports .port', (els) => els.map((e) => e.textContent.trim()))
await page.keyboard.press('Escape')
for (const gone of ['Users', 'Tokens', 'Detectors']) {
  check(
    !adminLabels.some((l) => l === gone),
    `${gone} has no atlas destination of its own any more -- atlas shows ${JSON.stringify(adminLabels)}`,
  )
}
check(
  adminLabels.some((l) => l.includes('The engine room')),
  'the engine room is what replaced them',
)

// --- Run setup… opens the modal, and is not a page (#487) --------------
// The row before this one left the app on Entities, and it must still be
// there underneath: an action does not navigate. Checked with the shell
// visible behind the modal rather than by reading appState, because what
// broke here before was App.svelte still mounting a retired component --
// exactly the thing only a real browser can see.

await goTo(page, 'Run setup…')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible', timeout: 5000 })
check(true, 'Run setup… opens the setup modal')
await page.keyboard.press('Escape') // the wizard modal owns Escape; close it before reading the atlas
await wizard.waitFor({ state: 'detached', timeout: 5000 })
await openAtlas(page)
const stillCurrent = await page.$$eval('.atlas .ports .port.on', (els) => els.map((e) => e.textContent.trim()))
await page.keyboard.press('Escape')
check(
  stillCurrent.length === 1 && stillCurrent[0] === 'Entities',
  `the page underneath is still the one the operator was on -- an action does not navigate (${JSON.stringify(stillCurrent)})`,
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

await openAndCheck('The engine room', 'The engine room')
await page.click(`${PEOPLE} .footer-action`)
await page.waitForSelector(`${PEOPLE} .inline-form`)
await page.fill(`${PEOPLE} .inline-form input[type="text"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .inline-form input[type="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .inline-form .save`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`)
check(true, `the viewer account "${VIEWER_USER}" is created from the engine room's people door`)

// --- Viewer: absent, never disabled -------------------------------------

const browser = await chromium.launch()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

await openAtlas(viewerPage)
const viewerLabels = await viewerPage.$$eval('.atlas .ports .port', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Users', 'Tokens', 'Detectors', 'Entities', 'Run setup…']) {
  check(
    !viewerLabels.some((l) => l === absent),
    `${absent} is absent from a viewer's atlas -- atlas shows ${JSON.stringify(viewerLabels)}`,
  )
}
check(viewerLabels.includes('Fleet'), 'Fleet -- an Admin-group row with no admin gate -- is still there')
check(
  viewerLabels.some((l) => l.includes('The engine room')),
  'the engine room is in a viewer\'s atlas too -- the one Admin destination that is deliberately viewer-readable',
)

// A disabled stub would satisfy "absent" in spirit while breaking the
// letter of it -- prove nothing in the viewer's atlas is disabled either.
const viewerDisabled = await viewerPage.$$eval('.atlas .ports .port', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(viewerDisabled.length === 0, `no atlas destination is disabled for a viewer -- got ${JSON.stringify(viewerDisabled)}`)
check((await viewerPage.$$('.modal')).length === 0, 'no modal renders for a viewer either')

await viewerPage.click('.rail .item:has-text("Fleet")')
await viewerPage.waitForFunction(() => document.querySelector('.page-header h2')?.textContent.trim() === 'Fleet', null, {
  timeout: 5000,
})
check(true, 'Fleet renders for a viewer')

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

await openAndCheck('The engine room', 'The engine room')
page.on('dialog', (d) => d.accept())
await page.click(`${PEOPLE} .row:has-text("${VIEWER_USER}") .verb`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
