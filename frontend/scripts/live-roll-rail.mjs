// SPDX-License-Identifier: AGPL-3.0-only
//
// #616: the roll rail is the deck's jump control -- the scenes' names as
// sideways text on the right edge, replacing #544's left rail wholesale.
// What needs a real browser rather than a unit test:
//
// - The names are rendered from Deck.svelte's own card table, gated on
//   the real signed-in role: a viewer's deck simply has no Watchlist
//   card and no Watchlist name (#490's grammar: absent, never
//   disabled), proved end to end with a real second account rather than
//   a mocked authState.role.
// - Clicking a name has to actually roll the card to centre and move
//   the active state -- appState.view, the snap scroll, and the
//   IntersectionObserver writing the view back all have to agree, which
//   is exactly the wiring a mocked store hides.
// - The retired chrome (toolbar, left rail, atlas overlay) must be
//   genuinely gone from the rendered document, not merely unreferenced.
//
// Sorted here (after live-newest-first, before live-router-lookup)
// rather than at the old live-nav-rail slot: it pushes no filter table
// (live-router-lookup's own "nothing pushed yet" baseline runs later
// and is not disturbed) and leaves nothing behind but its syslog batch
// and a viewer account it deletes again.

import { chromium } from 'playwright'
import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

feedSyslog(120, 'roll-rail')
// landing: 'fall' -- stay on the real landing page so the landing
// assertion below actually observes it, rather than session()'s own
// default navigation to Stream hiding what the app opens on.
const { page, consoleErrors } = await session({ landing: 'fall' })

// --- The deck's names, in the ratified order ------------------------------
const names = await page.$$eval('.roll-rail .rail-name', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(names) === JSON.stringify(['The fall', 'Metrics', 'Stream', 'Flags', 'Watchlist']),
  `an admin's roll rail carries the five scenes in deck order -- got ${JSON.stringify(names)}`,
)

// Absent, never disabled -- a disabled name would satisfy a presence
// check while breaking the grammar.
const disabled = await page.$$eval('.roll-rail .rail-name', (els) =>
  els.filter((e) => e.disabled).map((e) => e.textContent.trim()),
)
check(disabled.length === 0, `no rail name is disabled -- got ${JSON.stringify(disabled)}`)

// --- The retired chrome is really gone ------------------------------------
check(
  (await page.$$('.toolbar, .rail, .atlas, .nav-menu, .hamburger')).length === 0,
  'the toolbar, the left rail, the atlas overlay and the hamburger no longer render',
)

// --- The fall is the landing, and the active state is single --------------
const current = async () =>
  (await page.$$eval('.roll-rail .rail-name[aria-current="page"]', (els) => els.map((e) => e.textContent.trim())))[0]

check((await current()) === 'The fall', `The fall is the landing (#616) -- got ${await current()}`)
check(
  (await page.$$eval('.roll-rail .rail-name.on', (els) => els.length)) === 1,
  'exactly one rail name is active at a time',
)

// --- Clicking a name rolls its card to centre and moves the state ---------
await page.click('.roll-rail .rail-name:text-is("Stream")')
await page.waitForFunction(
  () => document.querySelector('.roll-rail .rail-name[aria-current="page"]')?.textContent.trim() === 'Stream',
  null,
  { timeout: 5000 },
)
check((await current()) === 'Stream', 'clicking Stream moves aria-current to it')
// The card itself must have rolled to centre -- the state moving while
// the deck stays put is exactly the regression a snap-scroll rebuild
// invites.
await page.waitForFunction(
  () => {
    const deck = document.querySelector('.deck')
    const el = deck?.querySelector('.card[data-view="live"]')
    if (!el) return false
    return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
  },
  null,
  { timeout: 10000 },
)
check(true, "and the Stream card is the one at the deck's scroll position")
await page.waitForFunction(() => document.querySelectorAll('.grid .row').length >= 1, null, { timeout: 20000 })
check(true, 'with the live table rendering real events')

await page.click('.roll-rail .rail-name:text-is("Metrics")')
await page.waitForFunction(
  () => document.querySelector('.roll-rail .rail-name[aria-current="page"]')?.textContent.trim() === 'Metrics',
  null,
  { timeout: 5000 },
)
check((await current()) === 'Metrics', 'clicking Metrics moves aria-current to it')
check(
  (await page.$$eval('.roll-rail .rail-name.on', (els) => els.length)) === 1,
  'still exactly one active name after switching',
)
check(
  await page.$eval('.roll-rail .rail-name.on', (el) => el.getAttribute('aria-current') === 'page'),
  'the .on class and aria-current="page" agree on which name is active',
)

await page.click('.roll-rail .rail-name:text-is("Stream")')
await page.waitForFunction(
  () => document.querySelector('.roll-rail .rail-name[aria-current="page"]')?.textContent.trim() === 'Stream',
  null,
  { timeout: 5000 },
)
check((await current()) === 'Stream', 'and back to Stream')

// --- The skip-link is real, and first -------------------------------------
// A brand-new page in the same signed-in context, not blur() and not
// reload(): blur() leaves Chromium's sequential focus starting point on
// the last clicked element, and reload() restores the deck's scroll
// position and anchors the starting point mid-deck with it -- both read
// exactly like a missing skip-link. Only a document with no history of
// its own starts tabbing from the top.
// A separate browser carrying the same session cookie, because
// session()'s page owns an implicit context Playwright refuses a second
// newPage() on directly -- the same constraint live-ws-revocation.mjs
// documents for its own second tab.
const skipBrowser = await chromium.launch()
const skipCtx = await skipBrowser.newContext({ ignoreHTTPSErrors: true })
await skipCtx.addCookies(await page.context().cookies())
const freshPage = await skipCtx.newPage()
await freshPage.goto(process.env.MV_URL, { waitUntil: 'networkidle' })
await freshPage.waitForSelector('.roll-rail .rail-name', { timeout: 10000 })
await freshPage.keyboard.press('Tab')
const focused = await freshPage.evaluate(() => {
  const el = document.activeElement
  return { cls: el?.className ?? '', text: el?.textContent?.trim() ?? '', visible: el?.getBoundingClientRect().left >= 0 }
})
check(focused.cls.includes('skip-link'), `first Tab lands on the skip-link -- got "${focused.text}"`)
check(focused.visible && focused.cls.includes('skip-link'), 'the skip-link becomes visible once focused')
await skipBrowser.close()

// --- A viewer's deck has no Watchlist card and no Watchlist name ----------
const VIEWER_USER = 'live-viewer-rail'
const VIEWER_PASS = 'live-viewer-rail-password'
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
await viewerPage.waitForSelector('.roll-rail .rail-name', { timeout: 15000 })

const viewerNames = await viewerPage.$$eval('.roll-rail .rail-name', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(viewerNames) === JSON.stringify(['The fall', 'Metrics', 'Stream', 'Flags']),
  `a viewer's rail has four names and no Watchlist -- got ${JSON.stringify(viewerNames)}`,
)
check(
  (await viewerPage.locator('.card[data-view="watchlist"]').count()) === 0,
  'and no Watchlist card exists anywhere in the viewer deck -- absent, not hidden',
)
await browser.close()

// --- Clean up: the account should not outlive the scenario ----------------
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
