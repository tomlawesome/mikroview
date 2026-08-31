// SPDX-License-Identifier: AGPL-3.0-only
//
// #657's ratified surface matrix (issue body), proved end to end with
// real viewer and user accounts in a real browser -- not by reading
// navGroups.test.ts, which pins visibleGroups(), the table
// BottomBar.svelte reads on a phone-width viewport. It cannot see
// whether the desktop surfaces -- the deck's roll rail, the docket's
// own tabs, and the account chip's menu -- agree with that table. They
// are three separate DOM regions, not one render of navGroups.ts, so
// this scenario reads all three and unions them rather than trusting
// that "the data table says so" means "the screen says so".
//
// The owner's test (issue body): not "may they read this" but "does it
// help them interrogate the log". A viewer keeps every operational
// read; what disappears is anything whose purpose is making a change --
// Entities, Watchlist, Settings, the doors inside it, the setup wizard.
// Rows go absent, never disabled, so every check below also proves that
// grammar: no menu row renders greyed out for a tier that cannot use it.

import { chromium } from 'playwright'
import { session, check, done, goTo, openAccountMenu } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

const PEOPLE = '.door:has-text("Who may look in")'
const MACHINES = '.door:has-text("Which machines may speak")'

// The docket's own tab labels render lower-case in the DOM (Docket.svelte);
// the ratified matrix and every other surface use the capitalised form.
const DOCKET_LABELS = { flags: 'Flags', watchlist: 'Watchlist', 'audit log': 'Audit log' }

// The account menu also carries rows with no place in navGroups.ts at
// all -- Change password, Sign out, About & licence, SSO -- which are
// account actions, not surfaces the ratified matrix gates. Restricting
// to this set is what makes the union below comparable to the matrix
// rather than polluted by rows the matrix was never about.
const OPERATE_ROWS = ['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…']

/**
 * The whole visible navigation set for whatever session `p` holds,
 * read from every real surface that account can reach: the roll rail
 * (deck scenes -- ungated, the same four scenes for every tier), the
 * docket's tabs (flags/watchlist/audit, gated by canEdit/isAdmin), and
 * the account chip's menu (gated per row). Also returns which menu rows
 * render disabled, so a caller can prove absent-not-disabled without a
 * second pass through the menu.
 */
async function visibleSurfaces(p) {
  const railNames = await p.$$eval('.roll-rail button.rail-name', (els) => els.map((e) => e.textContent.trim()))
  const scenes = railNames.filter((n) => n !== 'The docket')

  await goTo(p, 'Flags')
  const tabLabels = await p.$$eval('.docket .tab-row .tab .tlabel', (els) => els.map((e) => e.textContent.trim()))
  const docket = tabLabels.map((t) => DOCKET_LABELS[t] ?? t)

  await openAccountMenu(p)
  const menuRows = await p.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
  const disabledRows = await p.$$eval('.account .menu button.row', (els) =>
    els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
  )
  await p.keyboard.press('Escape')
  await p.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
  const operate = menuRows.filter((r) => OPERATE_ROWS.includes(r))

  return { union: [...new Set([...scenes, ...docket, ...operate])].sort(), disabledRows }
}

function sortedSet(labels) {
  return [...new Set(labels)].sort()
}

// --- A real viewer and a real user account, created the way an admin --
// actually would: through the engine room's people door.

const VIEWER_USER = 'live-viewer-657'
const VIEWER_PASS = 'live-viewer-657-password'
const EDITOR_USER = 'live-user-657'
const EDITOR_PASS = 'live-user-657-password'

await goTo(page, 'Settings')
await page.waitForFunction(() => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings', null, {
  timeout: 5000,
})

async function createAccount(username, password, role) {
  await page.click(`${PEOPLE} .footer-action`)
  await page.waitForSelector(`${PEOPLE} .inline-form`)
  await page.fill(`${PEOPLE} .inline-form input[type="text"]`, username)
  await page.fill(`${PEOPLE} .inline-form input[type="password"]`, password)
  // The door defaults to 'user' ("can change things"); only the viewer
  // tier needs the selector touched at all.
  if (role === 'viewer') await page.selectOption(`${PEOPLE} .inline-form select`, 'viewer')
  await page.click(`${PEOPLE} .inline-form .save`)
  await page.waitForSelector(`${PEOPLE} .row:has-text("${username}")`)
}

await createAccount(VIEWER_USER, VIEWER_PASS, 'viewer')
check(true, `the viewer account "${VIEWER_USER}" is created from the people door`)
await createAccount(EDITOR_USER, EDITOR_PASS, 'user')
check(true, `the user account "${EDITOR_USER}" is created from the people door`)

async function signIn(username, password) {
  const browser = await chromium.launch()
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true })
  const p = await ctx.newPage()
  await p.goto(URL_BASE, { waitUntil: 'networkidle' })
  await p.fill('input[autocomplete="username"]', username)
  await p.fill('input[autocomplete="current-password"]', password)
  await p.click('button[type="submit"]')
  await p.waitForSelector('#main-content', { timeout: 15000 })
  return { browser, page: p }
}

// --- 1: a viewer's navigation is exactly the read-only set --------------

const viewer = await signIn(VIEWER_USER, VIEWER_PASS)
const viewerNav = await visibleSurfaces(viewer.page)
check(
  JSON.stringify(viewerNav.union) ===
    JSON.stringify(sortedSet(['The fall', 'Topography', 'Stream', 'Metrics', 'Flags', 'Fleet'])),
  `a viewer's whole navigation is the fall, topography, stream, metrics, flags, fleet -- got ${JSON.stringify(viewerNav.union)}`,
)
for (const absent of ['Watchlist', 'Settings', 'Entities', 'Audit log', 'Run setup…']) {
  check(!viewerNav.union.includes(absent), `${absent} is absent from a viewer's navigation`)
}
check(viewerNav.disabledRows.length === 0, `no menu row is disabled for a viewer -- got ${JSON.stringify(viewerNav.disabledRows)}`)

// --- 2: a user's navigation adds Watchlist, Settings and Entities -------

const editor = await signIn(EDITOR_USER, EDITOR_PASS)
const editorNav = await visibleSurfaces(editor.page)
check(
  JSON.stringify(editorNav.union) ===
    JSON.stringify(
      sortedSet(['The fall', 'Topography', 'Stream', 'Metrics', 'Flags', 'Watchlist', 'Settings', 'Fleet', 'Entities']),
    ),
  `a user's whole navigation adds Watchlist, Settings and Entities -- got ${JSON.stringify(editorNav.union)}`,
)
for (const added of ['Watchlist', 'Settings', 'Entities']) {
  check(editorNav.union.includes(added), `${added} is present in a user's navigation`)
}
for (const absent of ['Audit log', 'Run setup…']) {
  check(!editorNav.union.includes(absent), `${absent} still absent from a user's navigation`)
}
check(editorNav.disabledRows.length === 0, `no menu row is disabled for a user -- got ${JSON.stringify(editorNav.disabledRows)}`)

// --- 3: a user opening Settings sees neither door ------------------------
// #657's own narrowing: issuing keys is a setup task, not using the
// product, so both doors went admin-only -- a user loses token
// visibility it had before this ruling.

await goTo(editor.page, 'Settings')
await editor.page.waitForFunction(
  () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
  null,
  { timeout: 5000 },
)
const editorDoors = await editor.page.$$eval('.doors .door .dname', (els) => els.map((e) => e.textContent.trim()))
check(editorDoors.length === 0, `neither door renders for a user -- got ${JSON.stringify(editorDoors)}`)
check(!(await editor.page.locator(MACHINES).count()), '"Which machines may speak" (tokens) is absent for a user')
check(!(await editor.page.locator(PEOPLE).count()), '"Who may look in" (users) is absent for a user')

// Absent, never disabled -- the letter of #490's grammar. A greyed-out
// control satisfies "cannot use this" while breaking the rule the record
// is actually about, and a disabled control is the shape a hurried gating
// change reaches for first.
//
// live-engine-room.mjs used to assert this against a viewer, whose route
// into the room #657 removed. It moves here rather than disappearing:
// the user tier is who can still open the page, so they are who the
// grammar has to hold for now.
const editorDisabled = await editor.page.$$eval('.page button, .page input', (els) => els.filter((e) => e.disabled).length)
check(editorDisabled === 0, `nothing on Settings is rendered disabled for a user -- got ${editorDisabled}`)

// --- 4: GET /api/tokens narrowed to admin (#657) -------------------------

const userTokensResp = await editor.page.request.get(`${URL_BASE}/api/tokens`)
check(userTokensResp.status() === 403, `GET /api/tokens is refused for a signed-in user -- got ${userTokensResp.status()}`)
const adminTokensResp = await page.request.get(`${URL_BASE}/api/tokens`)
check(adminTokensResp.status() === 200, `GET /api/tokens still answers for an admin -- got ${adminTokensResp.status()}`)

// --- 5: a viewer still reads why an empty stream is empty -----------------
// GET /api/setup/status stays viewer-readable precisely because Settings
// no longer is -- gating the page must not have taken this read with it.

const viewerStatusResp = await viewer.page.request.get(`${URL_BASE}/api/setup/status`)
check(
  viewerStatusResp.status() === 200,
  `a viewer can still read GET /api/setup/status -- got ${viewerStatusResp.status()}`,
)

// --- Clean up: neither account, nor its session, should outlive this ----

await viewer.browser.close()
await editor.browser.close()

page.on('dialog', (d) => d.accept())
for (const u of [VIEWER_USER, EDITOR_USER]) {
  await page.click(`${PEOPLE} .row:has-text("${u}") .verb`)
  await page.waitForSelector(`${PEOPLE} .row:has-text("${u}")`, { state: 'detached' })
}
check(true, `the viewer and user accounts are removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
