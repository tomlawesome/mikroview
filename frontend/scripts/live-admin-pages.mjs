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
//  - A user's menu (the people door's default tier -- see below) follows
//    the absent-never-disabled grammar for the owner-level rows, proved
//    end to end with a real second account rather than a mocked
//    authState.role. A genuine viewer account is covered end to end in
//    live-viewer-surfaces.mjs, which this file predates.
//
// The room's own read-only grammar is live-engine-room.mjs's job. This
// scenario stops at the menu and the group's page-level facts.

import { chromium } from 'playwright'
import { session, check, done, goTo, openAccountMenu } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

/** opens an account-menu destination and waits for the new page's header to actually land -- a plain waitForSelector would
 * resolve against the *previous* page's `.page-header h2` while the transition is still in flight, since both pages share
 * the same selector. */
async function openAndCheck(label, headerText) {
  await goTo(page, label)
  await page.waitForFunction(
    (want) => document.querySelector('.page-header h2')?.textContent.trim() === want,
    headerText,
    { timeout: 5000 },
  )
  check(true, `the account menu's ${label} row opens the ${headerText} page`)
}

// --- Admin: each page is reachable, the overlays are genuinely gone -----

await openAndCheck('Settings', 'Settings')
await openAndCheck('Fleet', 'Fleet')
await openAndCheck('Entities', 'Entities')

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

// --- A real user account, created the way an admin actually would ------
// Through the engine room's people door now, which is where adding an
// account lives since #490. No role is picked here -- the door defaults
// to "Can change things", the `user` tier -- which is exactly what this
// section tests: the owner-level rows stay gated, the user-level rows
// (Entities, Settings) do not. It is not a viewer account despite the
// name this scenario used to give it; a genuine viewer's menu is covered
// end to end in live-viewer-surfaces.mjs.

const USER_USER = 'live-user-548'
const USER_PASS = 'live-user-548-password'
const PEOPLE = '.door:has-text("Who may look in")'

await openAndCheck('Settings', 'Settings')
await page.click(`${PEOPLE} .footer-action`)
await page.waitForSelector(`${PEOPLE} .inline-form`)
await page.fill(`${PEOPLE} .inline-form input[type="text"]`, USER_USER)
await page.fill(`${PEOPLE} .inline-form input[type="password"]`, USER_PASS)
await page.click(`${PEOPLE} .inline-form .save`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${USER_USER}")`)
check(true, `the user account "${USER_USER}" is created from the engine room's people door`)

// --- A user's menu: owner-level rows absent, user-level rows present, --
// --- never disabled ------------------------------------------------------

const browser = await chromium.launch()
const userCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const userPage = await userCtx.newPage()
await userPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await userPage.fill('input[autocomplete="username"]', USER_USER)
await userPage.fill('input[autocomplete="current-password"]', USER_PASS)
await userPage.click('button[type="submit"]')
await userPage.waitForSelector('#main-content', { timeout: 15000 })

await openAccountMenu(userPage)
const userLabels = await userPage.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
// Users, Tokens and Detectors have no row of their own for anyone (#490).
// Audit log and Run setup… are the owner-level rows #657 leaves gated to
// admin.
for (const absent of ['Users', 'Tokens', 'Detectors', 'Audit log', 'Run setup…']) {
  check(
    !userLabels.some((l) => l === absent),
    `${absent} is absent from a user's menu -- the menu shows ${JSON.stringify(userLabels)}`,
  )
}
check(userLabels.includes('Fleet'), 'Fleet -- an Admin-group row with no admin gate -- is still there')
check(
  userLabels.includes('Entities'),
  `Entities is in a user's menu -- #653 widened it to the user tier -- got ${JSON.stringify(userLabels)}`,
)
check(
  userLabels.some((l) => l.includes('Settings')),
  `Settings is in a user's menu too -- #657 gave the engine room to the user tier, not a viewer -- got ${JSON.stringify(userLabels)}`,
)

// A disabled stub would satisfy "absent" in spirit while breaking the
// letter of it -- prove nothing in the user's menu is disabled either.
const userDisabled = await userPage.$$eval('.account .menu button.row', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(userDisabled.length === 0, `no menu row is disabled for a user -- got ${JSON.stringify(userDisabled)}`)
check((await userPage.$$('.modal')).length === 0, 'no modal renders for a user either')

await goTo(userPage, 'Fleet')
await userPage.waitForFunction(() => document.querySelector('.page-header h2')?.textContent.trim() === 'Fleet', null, {
  timeout: 5000,
})
check(true, 'Fleet renders for a user')

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

await openAndCheck('Settings', 'Settings')
page.on('dialog', (d) => d.accept())
await page.click(`${PEOPLE} .row:has-text("${USER_USER}") .verb`)
await page.waitForSelector(`${PEOPLE} .row:has-text("${USER_USER}")`, { state: 'detached' })
check(true, `the user account "${USER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
