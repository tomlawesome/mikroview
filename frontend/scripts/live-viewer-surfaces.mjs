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
// Entities, Watchlist, Settings, keys and people inside it, the setup
// wizard.
// Rows go absent, never disabled, so every check below also proves that
// grammar: no menu row renders greyed out for a tier that cannot use it.

import { session, check, done, goTo, openAccountMenu, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Round 32 (#767) mounted keys and people directly in the Settings
// card (docs/design/concepts/round-32/settings-doors.html), retiring
// the two side doors this scenario used to scope against.
const PEOPLE = '#people'
const MACHINES = '#keys'

// The docket's own tab labels render lower-case, in SceneBar's switcher
// (.switch[role="tablist"] .sw -- round 30/#697 moved them off Docket.svelte
// itself); the ratified matrix and every other surface use the capitalised
// form.
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
 * (deck scenes -- gated per card by deckCards.ts's isAdmin/canEdit,
 * Entities/Settings for the user/admin tiers and Fleet standing in for
 * both at the viewer tier), the docket's tabs (flags/watchlist/audit,
 * gated by canEdit/isAdmin in SceneBar's own switcher), and the account
 * chip's menu (gated per row -- today just Run setup…, admin-only,
 * since #647 moved every page-shaped row onto the deck). Also returns
 * which menu rows render disabled, so a caller can prove
 * absent-not-disabled without a second pass through the menu.
 */
async function visibleSurfaces(p) {
  const railNames = await p.$$eval('.roll-rail button.rail-name', (els) => els.map((e) => e.textContent.trim()))
  const scenes = railNames.filter((n) => n !== 'The docket')

  await goTo(p, 'Flags')
  const tabLabels = await p.$$eval('.card[aria-hidden="false"] .switch[role="tablist"] .sw', (els) =>
    els.map((e) => e.textContent.trim()),
  )
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

// goTo() itself waits for the Settings card to be centred (round 30/#700
// draws no page heading to wait for separately -- see its own comment).
await goTo(page, 'Settings')

async function createAccount(username, password, role) {
  await page.click(`${PEOPLE} .ogfoot .olink`)
  await page.waitForSelector(`${PEOPLE} .pform`)
  await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, username)
  await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, password)
  // The form defaults to 'user' ("can change things"); only the viewer
  // tier needs the segment touched at all.
  if (role === 'viewer') await page.click(`${PEOPLE} .pform button:has-text("can only look")`)
  await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
  await page.waitForSelector(`${PEOPLE} .prow:has-text("${username}")`)
}

await createAccount(VIEWER_USER, VIEWER_PASS, 'viewer')
check(true, `the viewer account "${VIEWER_USER}" is created from the people door`)
await createAccount(EDITOR_USER, EDITOR_PASS, 'user')
check(true, `the user account "${EDITOR_USER}" is created from the people door`)

async function signIn(username, password) {
  const browser = await launchBrowser()
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
      sortedSet(['The fall', 'Topography', 'Stream', 'Metrics', 'Flags', 'Watchlist', 'Settings', 'Entities']),
    ),
  `a user's whole navigation adds Watchlist, Settings and Entities -- got ${JSON.stringify(editorNav.union)}`,
)
for (const added of ['Watchlist', 'Settings', 'Entities']) {
  check(editorNav.union.includes(added), `${added} is present in a user's navigation`)
}
// Fleet is the viewer's own stand-in for Entities/Settings (deckCards.ts's
// `fleet` key); a user gets the real Entities card, which folds Fleet's
// table into its own leading section (#647), so no separate Fleet card
// reaches the rail for this tier -- same as it never did for an admin.
for (const absent of ['Fleet', 'Audit log', 'Run setup…']) {
  check(!editorNav.union.includes(absent), `${absent} still absent from a user's navigation`)
}
check(editorNav.disabledRows.length === 0, `no menu row is disabled for a user -- got ${JSON.stringify(editorNav.disabledRows)}`)

// --- 3: a user opening Settings sees neither keys nor people -------------
// #657's own narrowing: issuing keys is a setup task, not using the
// product, so both groups are admin-only -- a user loses token
// visibility it had before this ruling. Round 32/#767 mounted keys and
// people directly in the card on that same footing, so this proves the
// groups are absent for a user, not just their old doors; the #657
// admin gate underneath is pinned directly at the API level in claim 4
// below and internal/api/tokens_test.go.

await goTo(editor.page, 'Settings')
check(!(await editor.page.locator(MACHINES).count()), 'keys is absent for a user')
check(!(await editor.page.locator(PEOPLE).count()), 'people is absent for a user')

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

// Arm-then-confirm (round 28's gesture): a click arms remove, a second
// click on the same button confirms it.
for (const u of [VIEWER_USER, EDITOR_USER]) {
  const remove = page.locator(`${PEOPLE} .prow:has-text("${u}") .remove`)
  await remove.click()
  await remove.click()
  await page.waitForSelector(`${PEOPLE} .prow:has-text("${u}")`, { state: 'detached' })
}
check(true, `the viewer and user accounts are removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
