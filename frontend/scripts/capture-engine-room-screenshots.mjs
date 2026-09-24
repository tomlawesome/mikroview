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
// #1249 (this branch) gave the people door an "authenticator app" pill
// and a "clear authenticator app" button, both conditional on
// user.hasTOTP (EngineRoom.svelte) -- so a jenny with no factor set up
// shows neither, and a screenshot taken that way would be freshly
// captured but still not depict what it went stale over. The block
// below signs jenny in and drives a real enrolment through the API
// directly (enrol -> compute a code -> confirm), the same RFC 6238 port
// capture-authenticator-app-screenshots.mjs carries at length in its
// own header -- see that file for why it's a port rather than a call
// into the Go binary or a third-party library. Done through the API,
// not the browser, because nothing here needs to screenshot enrolling;
// only the end state (jenny has a factor) has to be real before the
// people-door shot is taken.
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
import crypto from 'crypto'
import { completeSecondFactor } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

// totpCode is the same RFC 6238/4226 port capture-authenticator-app-
// screenshots.mjs carries (see that file's header): 30s step, 6 digits,
// HMAC-SHA1 dynamic truncation over a base32-decoded secret.
function base32Decode(input) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const ch of input.toUpperCase().replace(/=+$/, '')) {
    const val = alphabet.indexOf(ch)
    if (val === -1) throw new Error(`totp: invalid base32 character ${JSON.stringify(ch)}`)
    bits += val.toString(2).padStart(5, '0')
  }
  const bytes = []
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(bytes)
}
function totpCode(base32Secret, atMs = Date.now()) {
  const counter = Math.floor(atMs / 1000 / 30)
  const counterBuf = Buffer.alloc(8)
  counterBuf.writeBigUInt64BE(BigInt(counter))
  const hmac = crypto.createHmac('sha1', base32Decode(base32Secret)).update(counterBuf).digest()
  const offset = hmac[hmac.length - 1] & 0x0f
  const code = ((hmac[offset] & 0x7f) << 24) | (hmac[offset + 1] << 16) | (hmac[offset + 2] << 8) | hmac[offset + 3]
  return String(code % 1_000_000).padStart(6, '0')
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

// #767 (round 32) moved "who may look in" out of the retired
// EngineRoomDoors door and into the account group's own row grammar --
// it is now the account panel's #people section (EngineRoom.svelte),
// not a .door.
const PEOPLE = '#people'

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
  // Every local account now needs a second factor (#1253); live-env.sh
  // enrols one for the admin and exports MV_TOTP_SECRET, and this
  // finishes the login the same way live-browser.mjs's own session()
  // does, rather than reinventing that step here.
  await completeSecondFactor(page)
  await page.waitForSelector('#main-content', { timeout: 15000 })
  // Settings is one of the deck's own cards since #647 (round 23), reached via the roll rail rather than the
  // account chip's menu (#616's deck retired the rail, but #647 moved Settings and Entities off the menu and onto
  // the deck itself -- see live-browser.mjs's SCENES table). Standalone here rather than importing
  // live-browser.mjs's goTo(): this capture tool deliberately stays outside the scenario contract. Waits for the
  // engineroom card to actually centre -- .page-header h2 used to be the landing proof, but #700 unmounted
  // PageHeader from EngineRoom.svelte entirely, so that selector no longer exists anywhere on the page (#667 group
  // E).
  await page.click('.roll-rail button.rail-name:text-is("Settings")')
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector('.card[data-card="engineroom"]')
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
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
  const present = await page.locator(`${PEOPLE} .prow:has-text("${EXTRA_USER}")`).count()
  if (present === 0) {
    await page.click(`${PEOPLE} .ogfoot button:has-text("let someone in")`)
    await page.waitForSelector(`${PEOPLE} .pform`)
    await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, EXTRA_USER)
    await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, EXTRA_PASS)
    await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
    await page.waitForSelector(`${PEOPLE} .prow:has-text("${EXTRA_USER}")`)
    console.log(`created "${EXTRA_USER}" for the capture`)
  }
  await context.close()
}

// --- Give jenny an authenticator app, so the pill and the clear button
//     have something real to show (see the header comment) -----------
{
  const jctx = await browser.newContext({ ignoreHTTPSErrors: true })
  const jpage = await jctx.newPage()
  const jsonHeaders = { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' }

  const login = await jpage.request.fetch(`${URL_BASE}/api/auth/login`, {
    method: 'POST',
    headers: jsonHeaders,
    data: { username: EXTRA_USER, password: EXTRA_PASS },
  })
  if (!login.ok()) throw new Error(`jenny could not sign in to enrol a factor (${login.status()})`)

  const enrol = await jpage.request.fetch(`${URL_BASE}/api/auth/totp/enrol`, {
    method: 'POST',
    headers: jsonHeaders,
  })
  if (!enrol.ok()) throw new Error(`jenny's TOTP enrol failed (${enrol.status()})`)
  const secret = new URL((await enrol.json()).uri).searchParams.get('secret')
  if (!secret) throw new Error("jenny's enrol response carried no secret")

  const confirm = await jpage.request.fetch(`${URL_BASE}/api/auth/totp/confirm`, {
    method: 'POST',
    headers: jsonHeaders,
    data: { code: totpCode(secret) },
  })
  if (!confirm.ok()) throw new Error(`jenny's TOTP confirm failed (${confirm.status()})`)
  console.log(`gave "${EXTRA_USER}" an authenticator app for the capture`)

  await jctx.close()
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
  const row = page.locator(`${PEOPLE} .prow:has-text("${EXTRA_USER}")`)
  if ((await row.count()) > 0) {
    // Round 32's arm-then-confirm gesture (EngineRoom.svelte's
    // onRemoveClick), not a native confirm() dialog -- the first click
    // arms the button, the second (now reading "confirm...") removes.
    await row.locator('button.remove').click()
    await row.locator('button.remove').click()
    await row.waitFor({ state: 'detached' })
    console.log(`removed "${EXTRA_USER}"`)
  }
  await context.close()
}

await browser.close()
