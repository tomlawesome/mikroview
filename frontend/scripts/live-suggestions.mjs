// SPDX-License-Identifier: AGPL-3.0-only
//
// Suggestions (#243 slice 5) against a real running mikroview: real
// router data pushed through the real ingest endpoint, generating real
// candidates, accepted/hidden/unhidden through the real UI (not the API
// directly, unlike live-watchlist-entries.mjs) -- this page's whole
// point is the click-through review workflow, so that's what's worth
// actually driving in a browser. The nuke button gets the same
// treatment, including its confirm() dialog.

import { session, check, done } from './live-browser.mjs'

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

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
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

await openMenuView('Suggestions')
await page.waitForSelector('.card', { timeout: 15000 })

check(await page.isVisible('.card:has-text("live-camera")'), 'the device suggestion appears')
check(await page.isVisible('.card:has-text("port 3390")'), 'the port suggestion appears')
check(await page.isVisible('.filter:has-text("Undecided") >> text=2'), 'the Undecided filter counts both')

await page.click('.card:has-text("live-camera") button:has-text("Accept")')
await page.waitForFunction(
  () => !document.body.textContent?.includes('live-camera'),
  { timeout: 10000 },
)
check(true, 'accepting the device suggestion removes it from the Undecided view')

await page.click('.filter:has-text("Accepted")')
await page.waitForSelector('.card:has-text("live-camera")', { timeout: 10000 })
check(await page.isVisible('.card:has-text("live-camera")'), 'the accepted candidate shows under the Accepted filter')

// Confirm it actually created a real, inverted, observing watchlist entry --
// not just flipped a status locally.
await openMenuView('Watchlist')
await page.waitForSelector('.card', { timeout: 15000 })
check(await page.isVisible('.card:has-text("live-camera")'), 'accepting created a real watchlist entry')
check(await page.isVisible('.card:has-text("live-camera") .badge.observing'), 'the created entry starts observing (safe, empty-permitted default)')

// --- Hide, then unhide, the port candidate --------------------------------

await openMenuView('Suggestions')
await page.waitForSelector('.card', { timeout: 15000 })
await page.click('.card:has-text("port 3390") button:has-text("Hide")')
await page.waitForFunction(() => !document.body.textContent?.includes('port 3390'), { timeout: 10000 })
check(true, 'hiding the port suggestion removes it from the Undecided view')

await page.click('.filter:has-text("Hidden")')
await page.waitForSelector('.card:has-text("port 3390")', { timeout: 10000 })
check(await page.isVisible('.card:has-text("port 3390")'), 'the hidden candidate shows under the Hidden filter')

await page.click('.card:has-text("port 3390") button:has-text("Unhide")')
await page.waitForFunction(() => !document.body.textContent?.includes('port 3390'), { timeout: 10000 })
await page.click('.filter:has-text("Undecided")')
await page.waitForSelector('.card:has-text("port 3390")', { timeout: 10000 })
check(await page.isVisible('.card:has-text("port 3390")'), 'unhiding returns the candidate to the Undecided view')

// --- The nuke button, confirm() dialog and all ----------------------------

page.once('dialog', (d) => d.accept())
await page.click('button:has-text("Reset everything")')
await page.waitForTimeout(1500)

await page.click('.filter:has-text("Accepted")')
check(!(await page.isVisible('text=live-camera')), 'the nuke removed the previously-accepted candidate entirely')

await openMenuView('Watchlist')
await page.waitForTimeout(500)
check(!(await page.isVisible('text=live-camera')), 'the nuke deleted the real watchlist entry it had created')

await openMenuView('Suggestions')
await page.click('.filter:has-text("Undecided")')
await page.waitForSelector('.card', { timeout: 10000 })
check(await page.isVisible('.card:has-text("live-camera")'), 'the nuke immediately regenerated a fresh Off candidate from the same router data')

// --- Unauthenticated access is refused, same as every admin surface ------

const anonRes = await fetch(`${URL_BASE}/api/suggestions`)
check(anonRes.status === 401, `an unauthenticated request to /api/suggestions is refused (${anonRes.status})`)

done()
