// SPDX-License-Identifier: AGPL-3.0-only
//
// The Admin group's pages, driven in a real browser. #548 made Users,
// Tokens, Fleet and Entities pages instead of overlays; #490 then
// removed Users, Tokens and Detectors outright, absorbing them into the
// engine room. What survives from both changes, and needs a real
// browser rather than a unit test:
//
//  - Every remaining Admin destination is actually reachable from the
//    account chip's menu (#616: the operate pages live there), and no
//    modal renders anywhere in the group -- a unit test of AccountMenu
//    alone cannot see whether App.svelte still mounts a retired
//    component.
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

/** opens a deck destination -- goTo() itself waits for that card to be
 * centred (round 30/#697/#700 draws no page heading to wait for
 * separately -- see live-viewer-surfaces.mjs's own comment on the
 * point), so there is nothing further to wait for here. */
async function openAndCheck(label) {
  await goTo(page, label)
  check(true, `the account menu's ${label} row opens the ${label} page`)
}

// --- Admin: each page is reachable, the overlays are genuinely gone -----

await openAndCheck('Settings')
await openAndCheck('Fleet')
await openAndCheck('Entities')

check((await page.$$('.modal')).length === 0, 'no modal of any kind renders anywhere in the Admin group')

// --- The absorbed pages left nothing behind (#490) ----------------------
// Removals here are wholesale: no destination, no alias, no stub.
// Checking the menu's own labels is the honest test -- a `:has-text()` click that
// finds nothing would just time out and say "timeout", not "the row is
// correctly absent".
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
check(
  adminLabels.some((l) => l.includes('Settings')),
  'Settings -- the engine room -- is what replaced them',
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
await page.keyboard.press('Escape') // the wizard modal owns Escape; close it before reading the menu
await wizard.waitFor({ state: 'detached', timeout: 5000 })
await openAccountMenu(page)
const stillCurrent = await page.$$eval('.account .menu button.row.on', (els) => els.map((e) => e.textContent.trim()))
await page.keyboard.press('Escape')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
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
// Through the engine room's people group now (round 32/#767 mounted it
// directly in the Settings card; #490 first absorbed the account page
// into the engine room).

const VIEWER_USER = 'live-viewer-548'
const VIEWER_PASS = 'live-viewer-548-password'
const PEOPLE = '#people'

await openAndCheck('Settings')
await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`)
check(true, `the viewer account "${VIEWER_USER}" is created from the people group`)

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
check(viewerLabels.includes('Fleet'), 'Fleet -- an Admin-group row with no admin gate -- is still there')
check(
  viewerLabels.some((l) => l.includes('Settings')),
  'Settings is in a viewer\'s menu too -- the one Admin destination that is deliberately viewer-readable',
)

// A disabled stub would satisfy "absent" in spirit while breaking the
// letter of it -- prove nothing in the viewer's menu is disabled either.
const viewerDisabled = await viewerPage.$$eval('.account .menu button.row', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(viewerDisabled.length === 0, `no menu row is disabled for a viewer -- got ${JSON.stringify(viewerDisabled)}`)
check((await viewerPage.$$('.modal')).length === 0, 'no modal renders for a viewer either')

// goTo() itself waits for the Fleet card to be centred -- see
// openAndCheck's own comment above.
await goTo(viewerPage, 'Fleet')
check(true, 'Fleet renders for a viewer')

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

await openAndCheck('Settings')
// Arm-then-confirm (round 28's gesture): a click arms remove, a second
// click on the same button confirms it.
const remove = page.locator(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .remove`)
await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
