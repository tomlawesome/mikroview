// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and
// `make live-check`) never picks it up: it produces images, not
// pass/fail checks, and doesn't fit the check()/done() contract the
// other scripts share. Same shape as capture-live-view-screenshots.mjs
// and capture-engine-room-screenshots.mjs, which it borrows its login
// mechanics from.
//
// Replaces capture-live-view-screenshots.mjs, whose landing-page
// assumption (a bare login lands on the stream, so waiting for '.row'
// after signing in was enough) stopped holding once #616 made the fall
// the landing view. This script drives the roll rail explicitly, so it
// captures whichever view it actually names rather than relying on
// where sign-in happens to land.
//
// Produces three shots for the README and the GitHub Pages site, against
// a seeded demo (AGENTS.md's "Seed it: a demo on bare syslog is not a
// demo" -- run scripts/seed-demo.py push/entities/accounts and capture
// while `feed` is still running, or the header rate reads 0.0/s and the
// stream rows all carry one stale timestamp instead of a live spread):
//
//  - docs/screenshots/fall-dark.png: the landing view.
//  - docs/screenshots/topography-dark.png: the Topography rail view.
//  - docs/screenshots/stream-dark.png: the Stream rail view.
//
// 1600x900: at 1440 wide the fall's band headers overlap each other.
//
// No light-mode shot -- colorway.svelte.ts: the light/system themes
// were removed wholesale in #708, so a "light" capture would just be a
// second copy of the same dark UI.
//
// Usage:
//   eval "$(scripts/live-env.sh up)"   # MV_DEMO_DEVICES=1 MV_DEMO_BUILD=1
//   scripts/seed-demo.py push / entities / accounts, then feed &
//   cd frontend && node scripts/capture-release-screenshots.mjs   # while feed is still running
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

const outDir = path.join(REPO, 'docs', 'screenshots')

async function signedInPage(browser) {
  const context = await browser.newContext({
    viewport: { width: 1600, height: 900 },
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

const browser = await chromium.launch()

// The fall -- landing view.
{
  const { context, page } = await signedInPage(browser)
  await page.screenshot({ path: path.join(outDir, 'fall-dark.png') })
  console.log('captured fall-dark.png')
  await context.close()
}

// Topography.
{
  const { context, page } = await signedInPage(browser)
  await page.click('.roll-rail button.rail-name:text-is("Topography")')
  await page.waitForTimeout(2000)
  await page.screenshot({ path: path.join(outDir, 'topography-dark.png') })
  console.log('captured topography-dark.png')
  await context.close()
}

// Stream.
{
  const { context, page } = await signedInPage(browser)
  await page.click('.roll-rail button.rail-name:text-is("Stream")')
  await page.waitForTimeout(2000)
  await page.screenshot({ path: path.join(outDir, 'stream-dark.png') })
  console.log('captured stream-dark.png')
  await context.close()
}

await browser.close()
