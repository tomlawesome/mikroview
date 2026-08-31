// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and
// `make live-check`) never picks it up: it produces images, not
// pass/fail checks, and doesn't fit the check()/done() contract the
// other scripts share. Same shape as
// capture-live-view-screenshots.mjs, which it borrows its login and
// traffic mechanics from.
//
// Produces two things for #490:
//
//  - docs/screenshots/engine-room-people-door.png, which
//    docs/configuration.md's "Adding and removing people" section
//    points at. It replaces screenshots/users-panel.png, whose page no
//    longer exists.
//  - docs/design/screens/settings/round-2/built/ac-s1-{light,dark}.png,
//    the built room at the round-2 mockup's own viewport so the two can
//    be held side by side. The mockup shots beside them are 3200x2162,
//    i.e. 1600x1081 at 2x, so these match that rather than the
//    live-view screenshots' 1440x860.
//
// The comparison is by eye today, deliberately: automating
// mockup-vs-built fidelity is #587, and is blocked on #588.
//
// Usage:
//   eval "$(scripts/live-env.sh up)"
//   scripts/live-env.sh syslog 200
//   cd frontend && node scripts/capture-engine-room-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)

import { chromium } from 'playwright'
import { fileURLToPath } from 'url'
import path from 'path'
import fs from 'fs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

const SHOTS = path.join(REPO, 'docs', 'screenshots')
const BUILT = path.join(REPO, 'docs', 'design', 'screens', 'settings', 'round-2', 'built')
fs.mkdirSync(BUILT, { recursive: true })

// A second account so the people door depicts what the documentation
// says it depicts -- the admin and one ordinary user. Created for the
// capture and removed again at the end: a screenshot is not a reason to
// leave an account behind on the instance.
const EXTRA_USER = 'jenny'
const EXTRA_PASS = 'screenshot-only-account-pw'

const PEOPLE = '.door:has-text("Who may look in")'

const browser = await chromium.launch()

async function signedInPage(scheme) {
  const context = await browser.newContext({
    viewport: { width: 1600, height: 1081 },
    deviceScaleFactor: 2,
    colorScheme: scheme,
    ignoreHTTPSErrors: true,
  })
  const page = await context.newPage()
  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
  // The operate pages live on the scene bar's account chip since #616's
  // deck retired the rail. Standalone here rather than importing
  // live-browser.mjs's goTo(): this capture tool deliberately stays
  // outside the scenario contract.
  await page.click('.card[aria-hidden="false"] .account button.chip')
  await page.waitForSelector('.account .menu', { timeout: 5000 })
  await page.click('.account .menu button.row:text-is("Settings")')
  await page.waitForFunction(
    () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
    null,
    { timeout: 10000 },
  )
  // The room's numbers land a beat after the page does -- the setup
  // status and definitions fetches both resolve after mount. Capturing
  // without this waits produces em-dashes where the live figures belong.
  await page.waitForTimeout(1200)
  return { context, page }
}

// --- The extra account, created once through the door itself ------------
{
  const { context, page } = await signedInPage('dark')
  const present = await page.locator(`${PEOPLE} .row:has-text("${EXTRA_USER}")`).count()
  if (present === 0) {
    await page.click(`${PEOPLE} .footer-action`)
    await page.waitForSelector(`${PEOPLE} .inline-form`)
    await page.fill(`${PEOPLE} .inline-form input[type="text"]`, EXTRA_USER)
    await page.fill(`${PEOPLE} .inline-form input[type="password"]`, EXTRA_PASS)
    await page.click(`${PEOPLE} .inline-form .save`)
    await page.waitForSelector(`${PEOPLE} .row:has-text("${EXTRA_USER}")`)
    console.log(`created "${EXTRA_USER}" for the capture`)
  }
  await context.close()
}

for (const scheme of ['light', 'dark']) {
  const { context, page } = await signedInPage(scheme)

  await page.screenshot({ path: path.join(BUILT, `ac-s1-${scheme}.png`) })
  console.log(`captured built/ac-s1-${scheme}.png`)

  if (scheme === 'light') {
    await page.locator(PEOPLE).screenshot({ path: path.join(SHOTS, 'engine-room-people-door.png') })
    console.log('captured engine-room-people-door.png')
  }

  // The bench opened, which is the only place the record's
  // dashed-underline knobs are on screen -- worth a shot of its own,
  // since a closed bench cannot show them. Opened from the detection
  // group's "tune..." link since #633 rewrote this page (#661).
  await page.click('.olink:has-text("tune")')
  await page.waitForSelector('.bench .row')
  await page.waitForTimeout(400)
  await page.screenshot({ path: path.join(BUILT, `ac-s2-${scheme}.png`) })
  console.log(`captured built/ac-s2-${scheme}.png`)

  await context.close()
}

// --- Tidy up: the capture account does not outlive the capture ----------
{
  const { context, page } = await signedInPage('dark')
  page.on('dialog', (d) => d.accept())
  if ((await page.locator(`${PEOPLE} .row:has-text("${EXTRA_USER}")`).count()) > 0) {
    await page.click(`${PEOPLE} .row:has-text("${EXTRA_USER}") .verb`)
    await page.waitForSelector(`${PEOPLE} .row:has-text("${EXTRA_USER}")`, { state: 'detached' })
    console.log(`removed "${EXTRA_USER}"`)
  }
  await context.close()
}

await browser.close()
