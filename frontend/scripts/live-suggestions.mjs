// SPDX-License-Identifier: AGPL-3.0-only
//
// Suggestions (#243 slice 5) against a real running mikroview: real
// router data pushed through the real ingest endpoint, generating real
// candidates, accepted/hidden/unhidden through the real UI (not the API
// directly, unlike live-watchlist-entries.mjs) -- this tab's whole point
// is the click-through review workflow, so that's what's worth actually
// driving in a browser. The nuke button gets the same treatment,
// including its confirm() dialog.
//
// #547 folded this from its own page into an admin-only "Suggestions"
// tab of Watchlist (#544 had already dropped its rail row; #547 removed
// the standalone route wholesale -- no alias, no redirect). Unlike
// Flags' Exclusions tab, there is no admin/read-only split to prove
// *inside* this page: Watchlist itself only ever mounts for an admin
// (navGroups.ts's `admin: true` on the row), so the split is that a
// viewer never reaches Watchlist -- or Suggestions with it -- at all,
// checked below the same way live-admin-pages.mjs checks the Admin
// group's own admin-only rows.

import { session, check, done, goTo, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

async function createToken(body) {
  const res = await page.request.post(`${URL_BASE}/api/tokens`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

async function push(token, payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

async function openWatchlist() {
  // goTo(page, 'Watchlist') rolls the deck to the docket scene and clicks
  // its Watchlist tab on the scene bar (#700) -- no manual label matching
  // needed here, that lives in live-browser.mjs's SCENES table.
  await goTo(page, 'Watchlist')
  await page.waitForSelector('#panel-watchlist', { timeout: 10000 })
}

async function openSuggestionsTab() {
  await page.click('[role="tab"]:has-text("Suggestions")')
  await page.waitForSelector('#panel-suggestions', { timeout: 10000 })
}

// --- Seed real router data, then force an immediate regeneration ---------
// RunPeriodicSync's own interval (5 minutes) is far too coarse for a
// live-check run -- /api/suggestions/reset regenerates synchronously as
// part of what it already does (see internal/api's
// handleSuggestionsReset), so it doubles as the fastest real path to
// fresh candidates without a test-only knob added just for this.

const ingest = await createToken({ name: 'live-suggestions', kind: 'ingest', device: 'live-suggest-router' })
check(ingest.status === 201, `an ingest token is issued (${ingest.status})`)

check(
  (await push(ingest.body.value, {
    kind: 'dhcp-lease',
    page: 1,
    pages: 1,
    records: [{ hostname: 'live-camera', mac: 'aa:bb:cc:dd:ee:99', address: '192.168.50.10' }],
  })) === 200,
  'a named DHCP lease is pushed',
)

check(
  (await push(ingest.body.value, {
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    records: [
      {
        ordinal: 1,
        chain: 'forward',
        action: 'drop',
        dstPort: 3390,
        protocol: 'tcp',
        comment: 'live-suggestions test rule',
      },
    ],
  })) === 200,
  'a drop rule with a dst-port is pushed',
)

const reset0 = await page.request.post(`${URL_BASE}/api/suggestions/reset`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { confirm: true },
})
check(reset0.status() === 200, `the initial reset regenerates candidates (${reset0.status()})`)

// --- Accept the device candidate through the real UI ----------------------

await openWatchlist()
check(
  (await page.$$('[role="tablist"][aria-label="Watchlist views"]')).length === 1,
  'the Watchlist tablist renders, with a Suggestions tab, for an admin',
)
await openSuggestionsTab()
await page.waitForSelector('#panel-suggestions .card', { timeout: 15000 })

check(await page.isVisible('#panel-suggestions .card:has-text("live-camera")'), 'the device suggestion appears')
check(await page.isVisible('#panel-suggestions .card:has-text("port 3390")'), 'the port suggestion appears')
check(await page.isVisible('#panel-suggestions .filter:has-text("Undecided") >> text=2'), 'the Undecided filter counts both')

await page.click('#panel-suggestions .card:has-text("live-camera") button:has-text("Accept")')
await page.waitForFunction(
  () => !document.getElementById('panel-suggestions')?.textContent?.includes('live-camera'),
  { timeout: 10000 },
)
check(true, 'accepting the device suggestion removes it from the Undecided view')

await page.click('#panel-suggestions .filter:has-text("Accepted")')
await page.waitForSelector('#panel-suggestions .card:has-text("live-camera")', { timeout: 10000 })
check(await page.isVisible('#panel-suggestions .card:has-text("live-camera")'), 'the accepted candidate shows under the Accepted filter')

// Confirm it actually created a real, inverted, observing watchlist entry --
// not just flipped a status locally.
await page.click('[role="tablist"][aria-label="Watchlist views"] [role="tab"]:has-text("Watchlist")')
await page.waitForSelector('#panel-watchlist .card', { timeout: 15000 })
check(await page.isVisible('#panel-watchlist .card:has-text("live-camera")'), 'accepting created a real watchlist entry')
check(
  await page.isVisible('#panel-watchlist .card:has-text("live-camera") .badge.observing'),
  'the created entry starts observing (safe, empty-permitted default)',
)

// --- Hide, then unhide, the port candidate --------------------------------

await openSuggestionsTab()
// Suggestions.svelte stays mounted (just hidden) once you switch to
// Watchlist and back, unlike the separate page it used to be -- so its
// own "Accepted" filter chip, clicked above, is still selected rather
// than resetting to Undecided the way a remount used to guarantee.
// Explicit here rather than assumed.
await page.click('#panel-suggestions .filter:has-text("Undecided")')
await page.waitForSelector('#panel-suggestions .card', { timeout: 15000 })
await page.click('#panel-suggestions .card:has-text("port 3390") button:has-text("Hide")')
await page.waitForFunction(
  () => !document.getElementById('panel-suggestions')?.textContent?.includes('port 3390'),
  { timeout: 10000 },
)
check(true, 'hiding the port suggestion removes it from the Undecided view')

await page.click('#panel-suggestions .filter:has-text("Hidden")')
await page.waitForSelector('#panel-suggestions .card:has-text("port 3390")', { timeout: 10000 })
check(await page.isVisible('#panel-suggestions .card:has-text("port 3390")'), 'the hidden candidate shows under the Hidden filter')

await page.click('#panel-suggestions .card:has-text("port 3390") button:has-text("Unhide")')
await page.waitForFunction(
  () => !document.getElementById('panel-suggestions')?.textContent?.includes('port 3390'),
  { timeout: 10000 },
)
await page.click('#panel-suggestions .filter:has-text("Undecided")')
await page.waitForSelector('#panel-suggestions .card:has-text("port 3390")', { timeout: 10000 })
check(await page.isVisible('#panel-suggestions .card:has-text("port 3390")'), 'unhiding returns the candidate to the Undecided view')

// --- The nuke button, confirm() dialog and all ----------------------------

page.once('dialog', (d) => d.accept())
await page.click('#panel-suggestions button:has-text("Reset everything")')
await page.waitForTimeout(1500)

await page.click('#panel-suggestions .filter:has-text("Accepted")')
check(!(await page.isVisible('#panel-suggestions >> text=live-camera')), 'the nuke removed the previously-accepted candidate entirely')

await page.click('[role="tablist"][aria-label="Watchlist views"] [role="tab"]:has-text("Watchlist")')
await page.waitForTimeout(500)
check(!(await page.isVisible('#panel-watchlist >> text=live-camera')), 'the nuke deleted the real watchlist entry it had created')

await openSuggestionsTab()
await page.click('#panel-suggestions .filter:has-text("Undecided")')
await page.waitForSelector('#panel-suggestions .card', { timeout: 10000 })
check(
  await page.isVisible('#panel-suggestions .card:has-text("live-camera")'),
  'the nuke immediately regenerated a fresh Off candidate from the same router data',
)

// --- Unauthenticated access is refused, same as every admin surface ------

const anonRes = await fetch(`${URL_BASE}/api/suggestions`)
check(anonRes.status === 401, `an unauthenticated request to /api/suggestions is refused (${anonRes.status})`)

// --- The admin/read-only split #547 names explicitly ----------------------
// Watchlist (and Suggestions within it) is admin-only end to end: the
// name is absent from a viewer's roll rail entirely, never a page that
// loads and then fails.

const VIEWER_USER = 'live-viewer-547-suggestions'
const VIEWER_PASS = 'live-viewer-547-suggestions-password'

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
await viewerPage.waitForSelector('.roll-rail .rail-name', { timeout: 15000 })

const viewerLabels = await viewerPage.$$eval('.roll-rail .rail-name', (els) => els.map((e) => e.textContent.trim()))
check(
  !viewerLabels.includes('Watchlist'),
  `Watchlist -- and Suggestions with it -- is absent from a viewer's roll rail, not disabled -- the rail shows ${JSON.stringify(viewerLabels)}`,
)

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------
const usersRes = await page.request.get(`${URL_BASE}/api/auth/users`)
const users = usersRes.status() < 400 ? await usersRes.json() : []
const viewerAccount = (Array.isArray(users) ? users : []).find((u) => u.username === VIEWER_USER)
if (viewerAccount) {
  const del = await page.request.delete(`${URL_BASE}/api/auth/users/${encodeURIComponent(viewerAccount.id)}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
  check(del.status() < 300, `the viewer account "${VIEWER_USER}" is removed again (${del.status()})`)
} else {
  check(false, `could not find the viewer account "${VIEWER_USER}" to clean it up`)
}

done()
