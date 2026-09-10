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

import { session, check, responsive, openAccountMenu, done, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// --- The chip names the account, and opens the menu -----------------------
const chip = page.locator('.card[aria-hidden="false"] .account button.chip')
const chipText = (await chip.textContent())?.trim()
check(
  chipText?.startsWith(process.env.MV_USER ?? '') && chipText?.includes('(admin)'),
  `the chip carries the username and the admin marker -- got ${JSON.stringify(chipText)}`,
)

check(!chipText?.includes('read-only'), `an admin's chip claims no read-only -- got ${JSON.stringify(chipText)}`)

await openAccountMenu(page)
const rows = await page.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
// startsWith, not equality: the About row carries the build line after
// its label since #804 (version · licence · uptime), so its textContent
// is no longer just the label.
for (const expected of ['Run setup…', 'Change password', 'Sign out', 'About & licence']) {
  check(
    rows.some((r) => r.startsWith(expected)),
    `an admin's menu offers ${expected} -- got ${JSON.stringify(rows)}`,
  )
}
for (const retired of ['Settings', 'Fleet', 'Entities', 'Audit log']) {
  check(!rows.includes(retired), `${retired} left the menu for the deck (#647) -- got ${JSON.stringify(rows)}`)
}

// --- The foot's build line (#804, round 37) --------------------------------
// Uptime's home is here, beside the version and the licence. Days and
// hours only -- "a ticking second is a clock, not a fact" -- so this
// asserts the shape rather than watching it advance, and cross-checks
// the two units against the server's own number below.
const verText = (await page.locator('.account .menu .ver').textContent())?.trim()
check(/AGPL-3\.0/.test(verText ?? ''), `the foot names the licence -- got ${JSON.stringify(verText)}`)
// The whole line, spaced as drawn: "<version> · AGPL-3.0 · up N d N h".
// Asserted as a shape because the first build of it lost the space in
// front of AGPL-3.0 -- Svelte trims literal trailing whitespace before
// a block's close, and every looser assertion here passed anyway.
check(
  /^\S+ · AGPL-3\.0 · up \d+ d \d+ h$/.test(verText ?? ''),
  `the foot reads "<version> · AGPL-3.0 · up N d N h", middots spaced -- got ${JSON.stringify(verText)}`,
)
const upMatch = /up (\d+) d (\d+) h/.exec(verText ?? '')
check(upMatch !== null, `the foot carries uptime as days and hours -- got ${JSON.stringify(verText)}`)
check(
  !/\d+\s*m\s+\d+\s*s/.test(verText ?? ''),
  `uptime shows no minutes or seconds -- got ${JSON.stringify(verText)}`,
)

// The rendered figure and the server's own number must agree -- this is
// what the old ticking assertion was really proving.
const healthz = await page.evaluate(async () => {
  const res = await fetch('/api/healthz')
  return res.json()
})
check(
  Number.isInteger(healthz.uptimeSeconds) && healthz.uptimeSeconds >= 0,
  `healthz reports uptimeSeconds (got ${healthz.uptimeSeconds})`,
)
if (upMatch) {
  const shownSeconds = Number(upMatch[1]) * 86400 + Number(upMatch[2]) * 3600
  const flooredServer = Math.floor(healthz.uptimeSeconds / 3600) * 3600
  check(
    shownSeconds === flooredServer,
    `the foot's uptime is the server's own, floored to the hour (shown ${shownSeconds}s, server ${healthz.uptimeSeconds}s)`,
  )
}

// --- Escape closes it ------------------------------------------------------
await page.keyboard.press('Escape')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
check(true, 'Escape closes the menu')

// --- Click-away closes it too ----------------------------------------------
await openAccountMenu(page)
// The wordmark is inert -- clicking it can only exercise the menu's own
// click-away listener, not open something else underneath.
//
// It used to be `.scene-bar h1`, the scene title. Round 30 replaced that
// title with the wordmark (SceneBar.svelte's `.wm`), so this step had
// been timing out for 30s and killing the scenario before its own last
// third ever ran -- including every assertion about a viewer's session.
// The RESULT line it never printed is why the run recorded a silent
// death rather than a failed check.
await page.click('.card[aria-hidden="false"] .scene-bar .wm')
await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
check(true, 'clicking away closes the menu')

// --- About & licence opens, and the licence is really in it ----------------
await openAccountMenu(page)
await page.click('.account .menu button.row:has-text("About & licence")')
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

const browser = await launchBrowser()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

// The read-only viewer, declared once (#804, round 37): the chip is the
// one place every screen already says who you are, so it is the one
// place read-only is said -- with a real viewer session rather than a
// mocked role, which is the whole reason this runs in a browser.
const viewerChip = (
  await viewerPage.locator('.card[aria-hidden="false"] .account button.chip').textContent()
)?.replace(/\s+/g, ' ').trim()
check(
  viewerChip === `${VIEWER_USER} (viewer) · read-only`,
  `a viewer's chip declares the tier and read-only, once -- got ${JSON.stringify(viewerChip)}`,
)

await openAccountMenu(viewerPage)
const viewerRows = await viewerPage.$$eval('.account .menu button.row', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…']) {
  check(!viewerRows.includes(absent), `${absent} is absent from a viewer's menu -- got ${JSON.stringify(viewerRows)}`)
}
check(viewerRows.includes('Sign out'), 'a viewer can still sign out')
check(
  viewerRows.some((r) => r.startsWith('About & licence')),
  'and still reach the licence',
)
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
