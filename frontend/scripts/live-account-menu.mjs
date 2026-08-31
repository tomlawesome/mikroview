// SPDX-License-Identifier: AGPL-3.0-only
//
// #616: the account chip's menu is where the operate pages and account
// actions live since the deck retired the toolbar, the left rail and
// the atlas overlay. What needs a real browser rather than a unit test:
//
// - The rows are gated on the real signed-in role. A viewer's menu has
//   no Run setup… row at all (#490's grammar: absent, never disabled),
//   proved with a real second account rather than a mocked
//   authState.role. Since #647 (round 23) the menu carries no page
//   links at all -- Settings, Fleet and Entities joined the deck (Fleet
//   folded into Entities' own card) and Audit log has lived on the
//   docket's tab since rounds 17-19 -- so Run setup… is the only
//   admin-gated row left to prove.
// - Escape and click-away close the menu through a real window listener
//   -- a jsdom keydown on a detached component proves nothing about
//   which element actually owns the key.
// - About & licence must stay reachable from the running app (AGPL
//   5(d)/13), so its row has to really open the overlay, not merely
//   exist.
//
// Sorted before live-admin-pages deliberately: it reads the menu and
// opens one overlay, feeds nothing, and deletes the viewer account it
// creates, so nothing downstream inherits anything from it.

import { chromium } from 'playwright'
import { session, check, responsive, openAccountMenu, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// --- The chip names the account, and opens the menu -----------------------
const chip = page.locator('.card[aria-hidden="false"] .account button.chip')
const chipText = (await chip.textContent())?.trim()
check(
  chipText?.startsWith(process.env.MV_USER ?? '') && chipText?.includes('(admin)'),
  `the chip carries the username and the admin marker -- got ${JSON.stringify(chipText)}`,
)

await openAccountMenu(page)
const rows = await page.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
for (const expected of ['Run setup…', 'Change password', 'Sign out', 'About & licence']) {
  check(rows.includes(expected), `an admin's menu offers ${expected} -- got ${JSON.stringify(rows)}`)
}
for (const retired of ['Settings', 'Fleet', 'Entities', 'Audit log']) {
  check(!rows.includes(retired), `${retired} left the menu for the deck (#647) -- got ${JSON.stringify(rows)}`)
}

// --- Escape closes it ------------------------------------------------------
await page.keyboard.press('Escape')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
check(true, 'Escape closes the menu')

// --- Click-away closes it too ----------------------------------------------
await openAccountMenu(page)
// The scene title is inert -- clicking it can only exercise the menu's
// own click-away listener, not open something else underneath.
await page.click('.card[aria-hidden="false"] .scene-bar h1')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
check(true, 'clicking away closes the menu')

// --- About & licence opens, and the licence is really in it ----------------
await openAccountMenu(page)
await page.click('.account .menu button.row:text-is("About & licence")')
const about = page.locator('[role="dialog"][aria-label="About MikroView"]')
await about.waitFor({ state: 'visible', timeout: 5000 })
check(true, 'About & licence opens the about overlay')
check(
  (await about.textContent())?.includes('AGPL'),
  'the overlay names the licence -- reachable from the running app, per AGPL 5(d)/13',
)
await page.keyboard.press('Escape')
await about.waitFor({ state: 'detached', timeout: 5000 })
check(true, 'Escape closes the overlay again')

// --- A viewer's menu: admin rows absent, never disabled --------------------
const VIEWER_USER = 'live-viewer-menu'
const VIEWER_PASS = 'live-viewer-menu-password'
const createRes = await page.request.post(`${URL_BASE}/api/auth/users`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { username: VIEWER_USER, password: VIEWER_PASS, role: 'viewer' },
})
check(createRes.status() === 201, `a viewer account is created (${createRes.status()})`)

const browser = await chromium.launch()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

await openAccountMenu(viewerPage)
const viewerRows = await viewerPage.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…']) {
  check(!viewerRows.includes(absent), `${absent} is absent from a viewer's menu -- got ${JSON.stringify(viewerRows)}`)
}
check(viewerRows.includes('Sign out'), 'a viewer can still sign out')
check(viewerRows.includes('About & licence'), 'and still reach the licence')
const viewerDisabled = await viewerPage.$$eval('.account .menu button.row', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(viewerDisabled.length === 0, `no menu row is disabled for a viewer -- got ${JSON.stringify(viewerDisabled)}`)
await browser.close()

// --- Clean up ---------------------------------------------------------------
const usersRes = await page.request.get(`${URL_BASE}/api/auth/users`)
const users = usersRes.status() < 400 ? await usersRes.json() : []
const viewerAccount = (Array.isArray(users) ? users : []).find((u) => u.username === VIEWER_USER)
if (viewerAccount) {
  const del = await page.request.delete(`${URL_BASE}/api/auth/users/${encodeURIComponent(viewerAccount.id)}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
  check(del.status() === 200 || del.status() === 204, `the viewer account is removed again (${del.status()})`)
}

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
