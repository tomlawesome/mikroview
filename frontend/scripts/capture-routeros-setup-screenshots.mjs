// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and
// `make live-check`) never picks it up: it produces images, not
// pass/fail checks, and doesn't fit the check()/done() contract the
// other scripts share. Same shape as
// capture-engine-room-screenshots.mjs, which it borrows its login and
// framing mechanics from.
//
// Produces the two images docs/routeros-setup.md needs to show both
// ways a router is added (#1284's "docs show both paths with
// screenshots"):
//
//  - docs/screenshots/setup-wizard-ledger.png: the wizard's seven-step
//    ledger with step 1 open, which is the path that enrols the FIRST
//    router (account menu ▸ Run setup…).
//  - docs/screenshots/entities-add-a-router.png: the Entities routers
//    row with its "+ add a router" berth, which is the path that adds
//    every LATER one.
//
// The wizard shot is clipped to the modal from the step list down
// rather than taken whole. The block above it is the address field,
// and its "This machine also answers on ..." line prints the capture
// host's own LAN and docker-bridge addresses -- real addresses of
// whoever ran this, published into a public repo, and nothing the
// documentation needs. The clip starts below it; everything the
// marker asked for (the ledger, the open step, the footer) is inside.
//
// deviceScaleFactor 2, matching engine-room-people-door.png: both are
// element-sized crops rather than full 1600x900 views, so they are
// rendered at 2x to stay legible when a browser scales them down to
// the width of a docs column.
//
// Usage:
//   eval "$(scripts/live-env.sh up)"       # MV_DEMO_DEVICES=1, so the
//                                          # routers row has routers in it
//   scripts/seed-demo.py entities, then feed &   # live rates on the cards
//   cd frontend && node scripts/capture-routeros-setup-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)

import { chromium } from 'playwright'
import { fileURLToPath } from 'url'
import path from 'path'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

const SHOTS = path.join(REPO, 'docs', 'screenshots')

const browser = await chromium.launch()

async function signedInPage() {
  const context = await browser.newContext({
    viewport: { width: 1600, height: 900 },
    deviceScaleFactor: 2,
    colorScheme: 'dark',
    ignoreHTTPSErrors: true,
  })
  const page = await context.newPage()
  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
  await page.waitForTimeout(2000)
  return { context, page }
}

// --- The ledger: the path that enrols the first router ------------------
{
  const { context, page } = await signedInPage()
  await page.locator('.deck .card .account button').first().click()
  await page.waitForSelector('.account .menu')
  await page.click('.account .menu button.row:text-is("Run setup…")')
  await page.waitForSelector('.setup-wizard', { state: 'visible', timeout: 10000 })
  // The step list's receipts ("466 of 466 events carry an action") come
  // from /api/setup/status, which resolves after the modal paints.
  // Wait for the "rules" step's own receipt to actually say so, rather
  // than a fixed sleep and hoping the API won by then -- capturing too
  // early leaves the ledger reading "nothing has arrived yet" against an
  // instance that has heard plenty.
  await page.waitForFunction(
    () => {
      const receipt = document.querySelector('.setup-wizard .steps li:nth-child(4) .step-receipt')
      return !!receipt && /events carry an action/.test(receipt.textContent ?? '')
    },
    null,
    { timeout: 15000 },
  )
  const title = (await page.locator('#setup-wizard-title').textContent())?.trim()
  if (title !== 'Trust the certificate') {
    throw new Error(`wizard opened at ${JSON.stringify(title)}, not step 1 -- the shot is meant to show the ledger at its first step`)
  }
  const modal = await page.locator('.setup-wizard').boundingBox()
  const middle = await page.locator('.setup-wizard .middle').boundingBox()
  await page.screenshot({
    path: path.join(SHOTS, 'setup-wizard-ledger.png'),
    clip: {
      x: modal.x,
      y: middle.y,
      width: modal.width,
      height: modal.y + modal.height - middle.y,
    },
  })
  console.log('captured setup-wizard-ledger.png')
  await context.close()
}

// --- The berth: the path that adds every later router -------------------
{
  const { context, page } = await signedInPage()
  await page.click('.roll-rail button.rail-name:text-is("Entities")')
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector('.card[data-card="entities"]')
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
    null,
    { timeout: 15000 },
  )
  // Each router card's live rate arrives with the first stats poll after
  // the card mounts; wait for an actual positive rate on at least one
  // live card rather than a fixed sleep, or the row reads as a fleet
  // that has never said anything.
  await page.waitForFunction(
    () => {
      const rows = Array.from(document.querySelectorAll('.card[data-card="entities"] .fcard.live .frow'))
      return rows.some((el) => {
        const m = /([\d.]+) events\/s now/.exec(el.textContent ?? '')
        return m !== null && parseFloat(m[1]) > 0
      })
    },
    null,
    { timeout: 15000 },
  )
  const row = page.locator('.card[data-card="entities"] .og').first()
  const berth = await row.locator('.fcard.berth').count()
  if (berth !== 1) {
    throw new Error(`found ${berth} "+ add a router" berths, want exactly 1 -- sign in as an admin`)
  }
  const box = await row.boundingBox()
  const pad = 12
  await page.screenshot({
    path: path.join(SHOTS, 'entities-add-a-router.png'),
    clip: { x: box.x - pad, y: box.y - pad, width: box.width + pad * 2, height: box.height + pad * 2 },
  })
  console.log('captured entities-add-a-router.png')
  await context.close()
}

await browser.close()
