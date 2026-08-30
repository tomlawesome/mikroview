// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #207 / #547: permanent exclusions, now the admin-only
// "Exclusions" tab of Flags rather than a page of its own (#544 dropped
// the standalone route's rail row; #547 folded it into Flags as a tab
// and removed the route wholesale -- no alias, no redirect).
//
// Drives the whole path against a real instance: raise a real port_scan
// flag (not synthesized), permanently clear it from the Flags tab,
// confirm it shows up -- filterable, with the tab's own quiet outlined
// count -- on the Exclusions tab, then remove it and confirm it's gone.
// A UI reorganisation like this one has no truth beyond "the thing that
// used to be there still works, in the new place", so that's what this
// checks. It also proves the admin/read-only split #547 names
// explicitly: the tab itself is admin-only (GET/DELETE
// /api/flags/exclusions both 403 a non-admin, per Exclusions.svelte's
// own doc comment) even though Flags itself is not -- a viewer reaches
// Flags but never sees the Exclusions tab at all, absent rather than
// disabled, per #490's grammar.

import { chromium } from 'playwright'
import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario in this directory -- checked before
// choosing it, because sharing one is what #590 is about.
const SCAN_SOURCE = '198.51.100.85'

const URL_BASE = process.env.MV_URL

// A real port_scan flag: one source IP, 20 distinct destination ports
// inside the default 60s/15-port threshold.
// Its own scan address, not portscan's default (#590). This scenario
// permanently clears the flag it raises, and live-flags-clearing.mjs
// runs next and feeds the same address expecting a fresh active one --
// so on the default they collide, and the loser prints a diagnostic
// naming a "(cleared)" flag that belongs to the other scenario
// entirely. Two wrong diagnoses were built on that line.
feedPortScan(20, SCAN_SOURCE)

const { page } = await session()

async function openFlags() {
  // Matching the label, not the row: NavRail gives each row an icon and
  // moves its text into a <span class="label">, and Playwright's text
  // engine only matches an element that *directly* contains the text --
  // see live-nav-rail.mjs's own note on why `.item:text-is(...)` stopped
  // working once that landed.
  await goTo(page, 'Flags')
  await page.waitForSelector('#panel-flags', { timeout: 10000 })
}

// Confirm server-side before looking at the UI (#354). Without this the
// scenario intermittently died on a bare locator timeout that could not
// say whether the flag was missing or merely not rendered yet.
const raised = await waitForFlag(page, SCAN_SOURCE)
check(raised.ok, raised.message)

// Everything below -- clearing it, finding it on the Exclusions tab,
// filtering, removing it -- has nothing to assert against when the flag
// itself never reached the server. Running it anyway used to crash the
// scenario on the first Playwright locator timeout instead of reporting
// the real, upstream reason (#450).
if (raised.ok) {
  await openFlags()
  await page.waitForSelector('.card .type', { timeout: 15000 })

  check(
    (await page.$$('[role="tablist"][aria-label="Flags views"]')).length === 1,
    'the Flags tablist renders for an admin',
  )

  // Scoped to the port-scan card, not to whichever card happens to be
  // first. A 20-port scan also trips the rule-spike detector, so there
  // are two flags on this page and the order between them is not
  // something this scenario should depend on -- it clicked the first
  // `.split-arrow` and permanently cleared whichever flag that was,
  // which is why every assertion below then failed against the
  // container.
  // li.card, not .card: the deck's own snap-scroll sections carry class
  // "card" too (#616), and the section wrapping the whole Flags scene
  // also hasText the IP, so a bare .card resolves to it first.
  const scanCard = page.locator('li.card', { hasText: SCAN_SOURCE }).first()
  await scanCard.waitFor({ timeout: 15000 })
  check(await scanCard.locator('.split-arrow').isVisible(), 'the port scan raised a real flag with the permanent-clear action visible')

  // Permanently clear it -- this is what creates the exclusion under test.
  await scanCard.locator('.split-arrow').click()
  await page.click('.split-menu-item:has-text("Permanently clear")')

  // Wait for the effect on the tab's own count rather than a fixed
  // pause: the write goes through the persistence backend, and Postgres
  // is slower than a local file, so a 500ms sleep was enough for one and
  // not the other.
  await page.waitForSelector('[role="tab"]:has-text("Exclusions 1")', { timeout: 15000 })
  const exclusionsTab = page.locator('[role="tab"]', { hasText: 'Exclusions' })
  check(await exclusionsTab.locator('.count').innerText() === '1', 'the Exclusions tab carries a count of 1 once an exclusion exists')

  await exclusionsTab.click()
  await page.waitForSelector('#panel-exclusions .card .type', { timeout: 15000 })

  check(
    await page.isVisible('#panel-exclusions .card:has-text("Port scan")'),
    'the permanently-cleared flag shows up on the Exclusions tab',
  )
  check(
    await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`),
    'the exclusion card shows the correct target',
  )

  // Filter by detector type -- selecting anything other than Port scan
  // (or All) must hide the one exclusion under test.
  await page.selectOption('#panel-exclusions .filter select', { label: 'Port scan' })
  check(
    await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`),
    'the type filter set to a match still shows the card',
  )

  const typeOptions = await page.$$eval('#panel-exclusions .filter select option', (opts) =>
    opts.map((o) => o.textContent),
  )
  const otherType = typeOptions.find((t) => t && t !== 'All' && t !== 'Port scan')
  if (otherType) {
    await page.selectOption('#panel-exclusions .filter select', { label: otherType })
    check(
      !(await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`)),
      `the type filter set to "${otherType}" hides the non-matching card`,
    )
  } else {
    check(true, 'only one detector type has an exclusion -- the mismatch branch has nothing to test against')
  }
  await page.selectOption('#panel-exclusions .filter select', { label: 'All' })

  // Filter by target text.
  await page.fill('#panel-exclusions .filter input[type="search"]', SCAN_SOURCE)
  check(
    await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`),
    'the target filter matches the excluded IP',
  )
  await page.fill('#panel-exclusions .filter input[type="search"]', 'no-such-target-xyz')
  check(
    await page.isVisible('text=No exclusions match this filter'),
    'a non-matching target filter shows the empty-filter message, not a blank list',
  )
  await page.fill('#panel-exclusions .filter input[type="search"]', '')

  // Remove it, and confirm it's actually gone rather than just visually
  // hidden -- reload the page and check again.
  await page.click('#panel-exclusions button:has-text("Remove exclusion")')
  await page.waitForSelector('[role="tab"]:has-text("Exclusions")', { timeout: 5000 })
  check(
    !(await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`)),
    'removing the exclusion takes it off the tab immediately',
  )
  const exclusionsTabAfterRemove = page.locator('[role="tab"]', { hasText: 'Exclusions' })
  check(
    (await exclusionsTabAfterRemove.locator('.count').count()) === 0,
    'the Exclusions tab drops its count back to none once the list is empty',
  )

  await page.reload({ waitUntil: 'networkidle' })
  await openFlags()
  await page.click('[role="tab"]:has-text("Exclusions")')
  await page.waitForTimeout(500)
  check(
    !(await page.isVisible(`#panel-exclusions .card:has-text("${SCAN_SOURCE}")`)),
    'the removal persisted -- the exclusion is gone after a reload, not just optimistically hidden',
  )
} else {
  check(true, `skipped -- the exclusions flow (clear, list, filter, remove) cannot run without the port-scan flag (${raised.message})`)
}

// --- The admin/read-only split #547 names explicitly ----------------------
// Flags itself has no admin gate (navGroups.ts), but the Exclusions tab
// inside it does -- a viewer must reach the page and see their own
// flags, with the tab simply absent, never present-and-broken.

const VIEWER_USER = 'live-viewer-547'
const VIEWER_PASS = 'live-viewer-547-password'

const createRes = await page.request.post(`${URL_BASE}/api/auth/users`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { username: VIEWER_USER, password: VIEWER_PASS, role: 'user' },
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

await goTo(viewerPage, 'Flags')
await viewerPage.waitForSelector('.flags', { timeout: 10000 })
check(true, 'a viewer reaches the Flags page')
check(
  (await viewerPage.$$('[role="tablist"]')).length === 0,
  'no tablist renders for a viewer -- with only one tab visible, there is no tab chrome at all',
)
check(
  (await viewerPage.$$('[role="tab"]')).length === 0,
  'the Exclusions tab specifically is absent for a viewer, not just unusable',
)

await browser.close()

// --- Clean up: this account should not outlive the scenario -------------
const deleteRes = await page.request.get(`${URL_BASE}/api/auth/users`)
const users = deleteRes.status() < 400 ? await deleteRes.json() : []
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
