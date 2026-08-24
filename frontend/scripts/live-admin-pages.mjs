// SPDX-License-Identifier: AGPL-3.0-only
//
// #548: Users, Tokens, Fleet and Entities are pages under Admin now, and
// the overlays that used to carry Users/Tokens (UsersOverlay.svelte,
// TokensOverlay.svelte) retired wholesale. What needs a real browser
// rather than a unit test:
//
//  - Each of the four pages is actually reachable from the rail, and no
//    modal renders anywhere in the Admin group any more -- a unit test
//    of NavRail alone cannot see whether App.svelte still mounts the
//    retired overlay components.
//  - "Run setup…" launches the existing wizard page, per #548's interim
//    call (it opens #487's modal once that ships).
//  - A viewer's rail follows #490's absent-never-disabled grammar for
//    the Admin group's admin-only rows, proved end to end with a real
//    second account rather than a mocked authState.role.
//
// What this cannot cover, and why: the "READ-ONLY — ADMINS EDIT" chip
// and the edit-affordances-absent grammar on the Users/Tokens/Entities
// pages themselves. Those three stayed admin-gated end to end in this
// pass -- GET /api/auth/users, /api/tokens and /api/entities all still
// 403 a non-admin caller (see internal/api/authz_matrix_test.go, whose
// own reasoning for GET /api/auth/users is that the account list maps
// which one is the admin, the single highest-value target in the
// system). Fleet is the only Admin-group page a viewer reaches today,
// and it has no edit affordance for anyone to gate, so its header
// carries no chip either -- see PageHeader.svelte's own comment. This is
// flagged as an open question for the owner rather than decided here;
// see the PR notes.

import { chromium } from 'playwright'
import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

/** opens a rail item and waits for the new page's header to actually land -- a plain waitForSelector would resolve
 * against the *previous* page's `.page-header h2` while the transition is still in flight, since both pages share
 * the same selector. */
async function openAndCheck(label, headerText) {
  await page.click(`.rail .item:has-text("${label}")`)
  await page.waitForFunction(
    (want) => document.querySelector('.page-header h2')?.textContent.trim() === want,
    headerText,
    { timeout: 5000 },
  )
  check(true, `the rail's ${label} row opens the ${headerText} page`)
}

// --- Admin: each page is reachable, the overlays are genuinely gone -----

await openAndCheck('Users', 'Users')
check((await page.$$('.modal[aria-label="Users"]')).length === 0, 'no Users modal renders -- it is a page')

await openAndCheck('Tokens', 'Tokens')
check((await page.$$('.modal[aria-label="API tokens"]')).length === 0, 'no Tokens modal renders -- it is a page')

await openAndCheck('Fleet', 'Fleet')
await openAndCheck('Entities', 'Entities')

check((await page.$$('.modal')).length === 0, 'no modal of any kind renders anywhere in the Admin group')

// --- Run setup… launches the existing wizard page, per #548's interim ---

await page.click('.rail .item:has-text("Run setup")')
await page.waitForFunction(
  () => document.querySelector('.setup header h2')?.textContent.trim() === 'Connect a router',
  null,
  { timeout: 5000 },
)
check(true, 'Run setup… opens the existing setup wizard page')

// --- A real viewer account, created the way an admin actually would ----

const VIEWER_USER = 'live-viewer-548'
const VIEWER_PASS = 'live-viewer-548-password'

await openAndCheck('Users', 'Users')
await page.fill('.create-form input[type="text"]', VIEWER_USER)
await page.fill('.create-form input[type="password"]', VIEWER_PASS)
await page.click('.create-form .save')
await page.waitForSelector(`.row:has-text("${VIEWER_USER}")`)
check(true, `the viewer account "${VIEWER_USER}" is created from the Users page`)

// --- Viewer: absent, never disabled -------------------------------------

const browser = await chromium.launch()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('.rail .item', { timeout: 15000 })

const viewerLabels = await viewerPage.$$eval('.rail .item', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Users', 'Tokens', 'Entities', 'Run setup…']) {
  check(
    !viewerLabels.includes(absent),
    `${absent} is absent from a viewer's rail -- rail shows ${JSON.stringify(viewerLabels)}`,
  )
}
check(viewerLabels.includes('Fleet'), 'Fleet -- the one Admin-group row with no admin gate -- is still there')

// A disabled stub would satisfy "absent" in spirit while breaking the
// letter of it -- prove nothing in the viewer's rail is disabled either.
const viewerDisabled = await viewerPage.$$eval('.rail .item', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(viewerDisabled.length === 0, `no rail item is disabled for a viewer -- got ${JSON.stringify(viewerDisabled)}`)
check((await viewerPage.$$('.modal')).length === 0, 'no modal renders for a viewer either')

await viewerPage.click('.rail .item:has-text("Fleet")')
await viewerPage.waitForFunction(() => document.querySelector('.page-header h2')?.textContent.trim() === 'Fleet', null, {
  timeout: 5000,
})
check(true, 'Fleet -- the one viewer-reachable Admin-group page -- renders for a viewer')

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------

await openAndCheck('Users', 'Users')
page.on('dialog', (d) => d.accept())
await page.click(`.row:has-text("${VIEWER_USER}") .revoke`)
await page.waitForSelector(`.row:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
